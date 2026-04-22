// Package github provides lightweight access to the GitHub REST API.
package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	baseURL   = "https://api.github.com"
	userAgent = "cloneable-cli/1.0"
)

// ── Client ────────────────────────────────────────────────────────────────────

type client struct {
	http  *http.Client
	token string
}

func newClient() *client {
	return &client{
		http:  &http.Client{Timeout: 10 * time.Second},
		token: GetToken(), // reads env var or config file
	}
}

func (c *client) get(endpoint string, dest interface{}) error {
	req, err := http.NewRequest("GET", baseURL+endpoint, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// Rate limit hit
	if resp.StatusCode == 403 || resp.StatusCode == 429 {
		hasToken := c.token != ""
		if hasToken {
			return &RateLimitError{HasToken: true}
		}
		return &RateLimitError{HasToken: false}
	}

	// Not found — treat as empty, not error (e.g. repo has no releases)
	if resp.StatusCode == 404 {
		return &NotFoundError{}
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("GitHub API error %d", resp.StatusCode)
	}

	return json.Unmarshal(body, dest)
}

// ── Error types ───────────────────────────────────────────────────────────────

// RateLimitError is returned when the GitHub API rate limit is hit.
type RateLimitError struct {
	HasToken bool
}

func (e *RateLimitError) Error() string {
	if e.HasToken {
		return "rate_limit_with_token"
	}
	return "rate_limit_no_token"
}

// NotFoundError is returned when a resource is not found (404).
type NotFoundError struct{}

func (e *NotFoundError) Error() string { return "not_found" }

// IsRateLimit returns true if the error is a rate limit error.
func IsRateLimit(err error) bool {
	_, ok := err.(*RateLimitError)
	return ok
}

// IsNotFound returns true if the error is a 404.
func IsNotFound(err error) bool {
	_, ok := err.(*NotFoundError)
	return ok
}

// ── Search ────────────────────────────────────────────────────────────────────

type SearchResult struct {
	FullName    string   `json:"full_name"`
	Description string   `json:"description"`
	HTMLURL     string   `json:"html_url"`
	CloneURL    string   `json:"clone_url"`
	Stars       int      `json:"stargazers_count"`
	Language    string   `json:"language"`
	UpdatedAt   string   `json:"updated_at"`
	Topics      []string `json:"topics"`
}

type searchResponse struct {
	TotalCount int            `json:"total_count"`
	Items      []SearchResult `json:"items"`
}

// perPage returns how many results to fetch based on whether we have a token.
func perPage() int {
	if GetToken() != "" {
		return 100
	}
	return 30
}

func SearchRepos(query string, page int) ([]SearchResult, int, error) {
	c := newClient()
	
	// Improve fuzzy search for popular repos:
	// If it's a single word without special qualifiers and long enough, append a truncated OR fallback.
	runes := []rune(query)
	if !strings.Contains(query, " ") && len(runes) >= 4 && !strings.Contains(query, ":") {
		shortened := string(runes[:len(runes)-1])
		query = fmt.Sprintf("%s OR %s in:name", query, shortened)
	}

	q := url.QueryEscape(query)
	endpoint := fmt.Sprintf(
		"/search/repositories?q=%s&per_page=%d&page=%d",
		q, perPage(), page,
	)
	var resp searchResponse
	if err := c.get(endpoint, &resp); err != nil {
		return nil, 0, handleAPIError(err)
	}
	return resp.Items, resp.TotalCount, nil
}

func ExploreTrending(query string, page int) ([]SearchResult, int, error) {
	c := newClient()
	q := url.QueryEscape(query)
	endpoint := fmt.Sprintf(
		"/search/repositories?q=%s&sort=stars&order=desc&per_page=%d&page=%d",
		q, perPage(), page,
	)
	var resp searchResponse
	if err := c.get(endpoint, &resp); err != nil {
		return nil, 0, handleAPIError(err)
	}
	return resp.Items, resp.TotalCount, nil
}

// ── Language stats ────────────────────────────────────────────────────────────

type LangStats map[string]int

func FetchLanguages(owner, repo string) (LangStats, error) {
	c := newClient()
	var stats LangStats
	endpoint := fmt.Sprintf("/repos/%s/%s/languages", owner, repo)
	if err := c.get(endpoint, &stats); err != nil {
		return nil, handleAPIError(err)
	}
	return stats, nil
}

// ── Latest release ────────────────────────────────────────────────────────────

type releaseResponse struct {
	TagName string `json:"tag_name"`
	Message string `json:"message"` // GitHub error message field
}

// FetchLatestVersion returns the latest release version, or "" if no releases exist.
func FetchLatestVersion(repo string) (string, error) {
	c := newClient()
	var rel releaseResponse
	err := c.get("/repos/"+repo+"/releases/latest", &rel)
	if err != nil {
		if IsNotFound(err) {
			return "", nil // No releases yet — not an error
		}
		return "", err
	}
	return strings.TrimPrefix(rel.TagName, "v"), nil
}

// ── handleAPIError ────────────────────────────────────────────────────────────

// handleAPIError checks if it's a rate limit and if so, prompts for a token.
// If the user provides a token, it retries the original call.
// This is the central place for rate limit handling so every caller benefits.
func handleAPIError(err error) error {
	if !IsRateLimit(err) {
		return err
	}

	rl := err.(*RateLimitError)

	if rl.HasToken {
		// Already have a token and still rate limited — token exhausted
		return fmt.Errorf(
			"GitHub API rate limit reached (even with your token)\n\n" +
				"  You've made too many requests. Wait ~1 hour and try again.\n",
		)
	}

	// No token — prompt for one
	reason := "GitHub API rate limit reached (60 requests/hour without a token).\n\n" +
		"  With a free token you get 5,000 requests/hour.\n" +
		"  Without a token, please wait ~1 hour before trying again."

	token := PromptForToken(reason)
	if token == "" {
		return fmt.Errorf(
			"GitHub rate limit reached\n\n" +
				"  No token provided. Please wait ~1 hour, or add a GITHUB_TOKEN.\n",
		)
	}

	// Token provided — the caller will retry naturally on next invocation
	// (since GetToken() will now return the saved token)
	return fmt.Errorf("token saved — please run your command again")
}

// ── Formatting helpers ────────────────────────────────────────────────────────

type LangBar struct {
	Name    string
	Bytes   int
	Percent float64
}

func (ls LangStats) ToSortedBars() []LangBar {
	if len(ls) == 0 {
		return nil
	}
	total := 0
	for _, b := range ls {
		total += b
	}
	if total == 0 {
		return nil
	}
	bars := make([]LangBar, 0, len(ls))
	for name, bytes := range ls {
		bars = append(bars, LangBar{
			Name:    name,
			Bytes:   bytes,
			Percent: float64(bytes) / float64(total) * 100,
		})
	}
	for i := 0; i < len(bars); i++ {
		for j := i + 1; j < len(bars); j++ {
			if bars[j].Bytes > bars[i].Bytes {
				bars[i], bars[j] = bars[j], bars[i]
			}
		}
	}
	return bars
}

func FormatStars(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fm", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func FormatUpdated(iso string) string {
	if iso == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < 24*time.Hour:
		return "today"
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dw ago", int(d.Hours()/(24*7)))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo ago", int(d.Hours()/(24*30)))
	default:
		return fmt.Sprintf("%dy ago", int(d.Hours()/(24*365)))
	}
}

func TruncateDesc(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	trimmed := s[:maxLen]
	if idx := strings.LastIndex(trimmed, " "); idx > maxLen-15 {
		trimmed = trimmed[:idx]
	}
	return trimmed + "…"
}
