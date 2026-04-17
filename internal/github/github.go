// Package github provides lightweight access to the GitHub REST API.
// No SDK — just net/http. Respects GITHUB_TOKEN for higher rate limits.
package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	baseURL   = "https://api.github.com"
	userAgent = "cloneable-cli/1.0"
)

// client is a minimal GitHub API client.
type client struct {
	http  *http.Client
	token string
}

// newClient creates a client, picking up GITHUB_TOKEN from the environment.
func newClient() *client {
	return &client{
		http:  &http.Client{Timeout: 8 * time.Second},
		token: os.Getenv("GITHUB_TOKEN"),
	}
}

// get performs a GET request and decodes the JSON response into dest.
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

	// Handle rate limit
	if resp.StatusCode == 403 || resp.StatusCode == 429 {
		return fmt.Errorf(
			"GitHub API rate limit reached\n\n" +
				"  Set GITHUB_TOKEN for 5,000 requests/hour:\n" +
				"  export GITHUB_TOKEN=your_token_here\n",
		)
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("GitHub API error %d", resp.StatusCode)
	}

	return json.Unmarshal(body, dest)
}

// ── Search ────────────────────────────────────────────────────────────────────

// SearchResult holds a single repository returned by the search API.
type SearchResult struct {
	FullName    string `json:"full_name"`    // e.g. "neovim/neovim"
	Description string `json:"description"`  // e.g. "Vim-fork focused on extensibility"
	HTMLURL     string `json:"html_url"`     // GitHub web URL
	CloneURL    string `json:"clone_url"`    // HTTPS clone URL
	Stars       int    `json:"stargazers_count"`
	Language    string `json:"language"`     // primary language
	UpdatedAt   string `json:"updated_at"`   // ISO 8601 timestamp
	Topics      []string `json:"topics"`
}

// searchResponse is the top-level shape of the GitHub search API response.
type searchResponse struct {
	TotalCount int            `json:"total_count"`
	Items      []SearchResult `json:"items"`
}

// SearchRepos searches GitHub for repositories matching the query.
// Returns up to 20 results sorted by stars (best match).
func SearchRepos(query string) ([]SearchResult, int, error) {
	c := newClient()

	q := url.QueryEscape(query)
	endpoint := fmt.Sprintf(
		"/search/repositories?q=%s&sort=stars&order=desc&per_page=20",
		q,
	)

	var resp searchResponse
	if err := c.get(endpoint, &resp); err != nil {
		return nil, 0, err
	}

	return resp.Items, resp.TotalCount, nil
}

// ── Language stats ────────────────────────────────────────────────────────────

// LangStats maps language name → byte count.
type LangStats map[string]int

// FetchLanguages fetches the language breakdown for owner/repo from the
// GitHub API. Does not clone the repo.
func FetchLanguages(owner, repo string) (LangStats, error) {
	c := newClient()
	var stats LangStats
	endpoint := fmt.Sprintf("/repos/%s/%s/languages", owner, repo)
	if err := c.get(endpoint, &stats); err != nil {
		return nil, err
	}
	return stats, nil
}

// ── Formatting helpers ────────────────────────────────────────────────────────

// LangBar represents a single language row in the stats display.
type LangBar struct {
	Name    string
	Bytes   int
	Percent float64
}

// ToSortedBars converts LangStats to a slice of LangBars sorted by byte count descending.
func (ls LangStats) ToSortedBars() []LangBar {
	if len(ls) == 0 {
		return nil
	}

	// Total bytes
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

	// Sort descending by bytes
	for i := 0; i < len(bars); i++ {
		for j := i + 1; j < len(bars); j++ {
			if bars[j].Bytes > bars[i].Bytes {
				bars[i], bars[j] = bars[j], bars[i]
			}
		}
	}

	return bars
}

// FormatStars returns a compact star count string.
// e.g. 1234 → "1.2k"   12345 → "12k"   1234567 → "1.2m"
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

// FormatUpdated returns a short relative time string from an ISO timestamp.
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

// TruncateDesc truncates a description to maxLen characters cleanly.
func TruncateDesc(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	// Trim at word boundary
	trimmed := s[:maxLen]
	if idx := strings.LastIndex(trimmed, " "); idx > maxLen-15 {
		trimmed = trimmed[:idx]
	}
	return trimmed + "…"
}
