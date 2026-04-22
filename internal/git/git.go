// Package git handles all git operations for Cloneable.
// Uses system git binary for reliability and real progress bars.
// Falls back to go-git if system git is not available.
package git

import (
	"bufio"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/charmbracelet/lipgloss"
)

// ── Types ─────────────────────────────────────────────────────────────────────

type CloneOptions struct {
	URL         string
	DestDir     string
	OnDuplicate DuplicateAction
	ProgressFn  func(msg string)
	Auth        *Auth
}

type Auth struct {
	Username string
	Token    string
}

type DuplicateAction int

const (
	DuplicateAsk     DuplicateAction = iota
	DuplicateReplace
	DuplicateSkip
)

type CloneResult struct {
	RepoName       string
	ClonedPath     string
	AlreadyExisted bool
}

// ── Progress bar ──────────────────────────────────────────────────────────────

var (
	saffron  = lipgloss.Color("#FF8C00")
	darkGray = lipgloss.Color("#3A3A3A")
	gray     = lipgloss.Color("#888888")
	green    = lipgloss.Color("#00E676")

	styleBar     = lipgloss.NewStyle().Foreground(saffron)
	styleBarBg   = lipgloss.NewStyle().Foreground(darkGray)
	stylePercent = lipgloss.NewStyle().Foreground(saffron).Bold(true)
	styleLabel   = lipgloss.NewStyle().Foreground(gray)
	styleDone    = lipgloss.NewStyle().Foreground(green)
)

// renderBar returns a coloured progress bar string for the given percent (0-100).
func renderBar(percent int, width int) string {
	if width < 10 {
		width = 30
	}
	filled := (percent * width) / 100
	if filled > width {
		filled = width
	}
	empty := width - filled

	bar := styleBar.Render(strings.Repeat("█", filled)) +
		styleBarBg.Render(strings.Repeat("░", empty))
	pct := stylePercent.Render(fmt.Sprintf("%3d%%", percent))
	return bar + "  " + pct
}

// printProgress prints a single overwriting progress line to stdout.
func printProgress(phase string, percent int) {
	bar := renderBar(percent, 30)
	label := styleLabel.Render(phase)
	// \r overwrites the current line, \033[K clears the rest of the line
	fmt.Printf("\r  %s  %s\033[K", label, bar)
}

// printProgressDone prints the final "done" state on a new line.
func printProgressDone() {
	tick := styleDone.Render("✓")
	fmt.Printf("\r  %s  %s\n", tick, styleDone.Render("Cloned"))
}

// ── Clone (system git with progress, go-git fallback) ────────────────────────

func Clone(opts CloneOptions) (*CloneResult, error) {
	cleanURL, err := NormaliseURL(opts.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid repository URL: %w", err)
	}

	repoName := ExtractRepoName(cleanURL)
	if repoName == "" {
		return nil, fmt.Errorf("could not determine repository name from URL: %s", cleanURL)
	}

	destPath := filepath.Join(opts.DestDir, repoName)

	// Handle existing directory
	if _, err := os.Stat(destPath); err == nil {
		switch opts.OnDuplicate {
		case DuplicateSkip:
			// Ensure submodules are up to date even if skipping clone
			_ = EnsureSubmodules(destPath, opts.ProgressFn)
			return &CloneResult{RepoName: repoName, ClonedPath: destPath, AlreadyExisted: true}, nil
		case DuplicateReplace, DuplicateAsk:
			if err := os.RemoveAll(destPath); err != nil {
				return nil, fmt.Errorf("could not remove existing directory: %w", err)
			}
		}
	}

	// Try system git first — much faster, real progress, handles large repos
	if commandExists("git") {
		if err := cloneWithSystemGit(cleanURL, destPath, opts); err != nil {
			// If system git fails, try go-git as fallback
			_ = os.RemoveAll(destPath)
			if err2 := cloneWithGoGit(cleanURL, destPath, opts); err2 != nil {
				return nil, friendlyCloneError(err, cleanURL)
			}
		}
	} else {
		// No system git — use go-git
		if err := cloneWithGoGit(cleanURL, destPath, opts); err != nil {
			_ = os.RemoveAll(destPath)
			return nil, friendlyCloneError(err, cleanURL)
		}
	}

	return &CloneResult{RepoName: repoName, ClonedPath: destPath}, nil
}

// cloneWithSystemGit runs `git clone` as a subprocess and parses its stderr
// for progress percentages to render a live progress bar.
func cloneWithSystemGit(cloneURL, destPath string, opts CloneOptions) error {
	args := []string{"clone", "--recursive", "--shallow-submodules", "--progress", cloneURL, destPath}

	// Add token auth via URL if available
	if opts.Auth != nil && opts.Auth.Token != "" {
		u, err := url.Parse(cloneURL)
		if err == nil {
			u.User = url.UserPassword(opts.Auth.Username, opts.Auth.Token)
			cloneURL = u.String()
			args = []string{"clone", "--recursive", "--shallow-submodules", "--progress", cloneURL, destPath}
		}
	}

	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	// git sends progress to stderr
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	// Parse progress in a goroutine
	done := make(chan struct{})
	lastProgress := time.Now()
	stuckTimeout := 90 * time.Second

	go func() {
		defer close(done)
		parseGitProgress(stderrPipe, opts.ProgressFn, &lastProgress)
	}()

	// Watch for stall
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	waitCh := make(chan error, 1)
	go func() {
		<-done
		waitCh <- cmd.Wait()
	}()

	for {
		select {
		case err := <-waitCh:
			if err != nil {
				return err
			}
			printProgressDone()
			return nil
		case <-ticker.C:
			if time.Since(lastProgress) > stuckTimeout {
				_ = cmd.Process.Kill()
				return fmt.Errorf("clone timed out after %v with no progress — the repo may be very large or your connection is slow", stuckTimeout)
			}
		}
	}
}

// progressRe matches git's \"Receiving objects:  45% (123/456)\" lines.
var progressRe = regexp.MustCompile(`(\w[\w\s]+):\s+(\d+)%`)

// parseGitProgress reads git's stderr and updates the progress bar.
func parseGitProgress(r io.Reader, logFn func(string), lastProgress *time.Time) {
	scanner := bufio.NewScanner(r)
	// git uses \r to overwrite the same line — split on both \n and \r
	scanner.Split(scanLinesAndCR)

	currentPhase := "Cloning"
	currentPct := 0

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		*lastProgress = time.Now()

		if logFn != nil {
			logFn("[git] " + line)
		}

		// Try to parse a percentage
		if m := progressRe.FindStringSubmatch(line); m != nil {
			phase := strings.TrimSpace(m[1])
			pct, _ := strconv.Atoi(m[2])

			// Map git phases to friendlier names
			mappedPhase := ""
			switch {
			case strings.Contains(strings.ToLower(phase), "enumerating"):
				mappedPhase = "Enumerating"
			case strings.Contains(strings.ToLower(phase), "counting"):
				mappedPhase = "Counting"
			case strings.Contains(strings.ToLower(phase), "compressing"):
				mappedPhase = "Compressing"
			case strings.Contains(strings.ToLower(phase), "receiving"):
				mappedPhase = "Downloading"
			case strings.Contains(strings.ToLower(phase), "resolving"):
				mappedPhase = "Resolving"
			default:
				mappedPhase = phase
			}

			// If phase changed, reset percentage to show progress for the new phase
			if mappedPhase != currentPhase {
				currentPhase = mappedPhase
				currentPct = 0
			}

			if pct > currentPct {
				currentPct = pct
			}

			printProgress(currentPhase, currentPct)
		}
	}
}

// scanLinesAndCR is a bufio.SplitFunc that splits on \n or \r.
func scanLinesAndCR(data []byte, atEOF bool) (advance int, token []byte, err error) {
	for i, b := range data {
		if b == '\n' || b == '\r' {
			return i + 1, data[:i], nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// cloneWithGoGit is the fallback clone using the pure-Go git implementation.
func cloneWithGoGit(cloneURL, destPath string, opts CloneOptions) error {
	cloneOpts := &gogit.CloneOptions{
		URL:          cloneURL,
		Depth:        0,
		SingleBranch: false,
		Tags:         gogit.AllTags,
		RecurseSubmodules: gogit.DefaultSubmoduleRecursionDepth,
	}

	if opts.Auth != nil && opts.Auth.Token != "" {
		cloneOpts.Auth = &http.BasicAuth{
			Username: opts.Auth.Username,
			Password: opts.Auth.Token,
		}
	}

	if opts.ProgressFn != nil {
		cloneOpts.Progress = &progressWriter{fn: opts.ProgressFn}
	}

	_, err := gogit.PlainClone(destPath, false, cloneOpts)
	return err
}

// ── URL helpers ───────────────────────────────────────────────────────────────

func NormaliseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)

	if strings.HasPrefix(raw, "git@") {
		raw = convertSSHToHTTPS(raw)
	}

	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		raw = "https://" + raw
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}

	if u.Host == "" {
		return "", fmt.Errorf("URL has no host: %s", raw)
	}

	// Remove trailing slashes before checking for .git suffix
	u.Path = strings.TrimSuffix(u.Path, "/")
	if !strings.HasSuffix(u.Path, ".git") {
		u.Path = u.Path + ".git"
	}

	return u.String(), nil
}

func convertSSHToHTTPS(ssh string) string {
	s := strings.TrimPrefix(ssh, "git@")
	s = strings.Replace(s, ":", "/", 1)
	return "https://" + s
}

func ExtractRepoName(rawURL string) string {
	rawURL = strings.TrimSuffix(rawURL, ".git")
	rawURL = strings.TrimSuffix(rawURL, "/")
	parts := strings.Split(rawURL, "/")
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[len(parts)-1])
}

func ExtractOwnerRepo(rawURL string) (owner, repo string, err error) {
	rawURL = strings.TrimSuffix(rawURL, ".git")
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", err
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("URL does not contain owner/repo: %s", rawURL)
	}
	return parts[0], parts[1], nil
}

func IsGitRepo(path string) bool {
	_, err := gogit.PlainOpen(path)
	return err == nil
}

func CurrentRepoName(path string) string {
	repo, err := gogit.PlainOpen(path)
	if err != nil {
		return filepath.Base(path)
	}
	remote, err := repo.Remote("origin")
	if err != nil {
		return filepath.Base(path)
	}
	urls := remote.Config().URLs
	if len(urls) == 0 {
		return filepath.Base(path)
	}
	return ExtractRepoName(urls[0])
}

func DefaultBranch(path string) string {
	repo, err := gogit.PlainOpen(path)
	if err != nil {
		return "main"
	}
	head, err := repo.Head()
	if err != nil {
		return "main"
	}
	branch := head.Name().Short()
	if branch == "" {
		return "main"
	}
	return branch
}

// ── Error handling ────────────────────────────────────────────────────────────

func friendlyCloneError(err error, cloneURL string) error {
	msg := err.Error()
	switch {
	case err == transport.ErrAuthenticationRequired || err == transport.ErrAuthorizationFailed:
		return fmt.Errorf("authentication required for %s\n\n  Set GITHUB_TOKEN:\n  export GITHUB_TOKEN=your_token", cloneURL)
	case strings.Contains(msg, "repository not found") || strings.Contains(msg, "not found"):
		return fmt.Errorf("repository not found: %s\n\n  Check the URL is correct and the repo is public.", cloneURL)
	case strings.Contains(msg, "no such host") || strings.Contains(msg, "dial tcp"):
		return fmt.Errorf("network error — could not reach %s\n\n  Check your internet connection.", cloneURL)
	default:
		return fmt.Errorf("clone failed: %w", err)
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

type progressWriter struct {
	fn func(msg string)
}

func (pw *progressWriter) Write(p []byte) (n int, err error) {
	if pw.fn != nil {
		pw.fn(string(p))
	}
	return len(p), nil
}

// EnsureSubmodules makes sure all git submodules are initialized and updated.
func EnsureSubmodules(repoPath string, logFn func(string)) error {
	if !commandExists("git") {
		return nil // fallback to go-git logic if possible, but system git is preferred
	}

	if logFn != nil {
		logFn("[git] updating submodules...")
	}

	cmd := exec.Command("git", "submodule", "update", "--init", "--recursive", "--depth", "1")
	cmd.Dir = repoPath
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	// Capture output for logging if needed
	out, err := cmd.CombinedOutput()
	if err != nil && logFn != nil {
		logFn(fmt.Sprintf("[git] submodule error: %s", string(out)))
	}
	return err
}
