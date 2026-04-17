// Package git handles all git operations for Cloneable.
// It uses go-git (pure Go) so no system git installation is required.
package git

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
)

// CloneOptions holds everything needed to perform a clone.
type CloneOptions struct {
	// URL is the remote git URL to clone from.
	URL string

	// DestDir is the parent directory to clone into.
	// The repo will be cloned into DestDir/<RepoName>.
	DestDir string

	// OnDuplicate controls what happens if the target folder already exists.
	OnDuplicate DuplicateAction

	// ProgressFn is called with progress messages during clone.
	// Can be nil — no progress output in that case.
	ProgressFn func(msg string)

	// Auth holds optional credentials for private repos.
	Auth *Auth
}

// Auth holds credentials for private repository access.
type Auth struct {
	Username string
	Token    string // GitHub personal access token or password
}

// DuplicateAction controls what happens when the target directory already exists.
type DuplicateAction int

const (
	DuplicateAsk     DuplicateAction = iota // Ask the user (default)
	DuplicateReplace                        // Delete existing and re-clone
	DuplicateSkip                           // Skip clone, use existing directory
)

// CloneResult holds the outcome of a clone operation.
type CloneResult struct {
	// RepoName is the short name of the repository (e.g. "neovim").
	RepoName string

	// ClonedPath is the full absolute path to the cloned directory.
	ClonedPath string

	// AlreadyExisted is true if the directory existed and was reused (not re-cloned).
	AlreadyExisted bool
}

// Clone clones the repository at opts.URL into opts.DestDir/<RepoName>.
// It handles URL normalisation, duplicate detection, and progress reporting.
func Clone(opts CloneOptions) (*CloneResult, error) {
	// Normalise and validate the URL
	cleanURL, err := NormaliseURL(opts.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid repository URL: %w", err)
	}

	repoName := ExtractRepoName(cleanURL)
	if repoName == "" {
		return nil, fmt.Errorf("could not determine repository name from URL: %s", cleanURL)
	}

	destPath := filepath.Join(opts.DestDir, repoName)

	// Check if destination already exists
	if _, err := os.Stat(destPath); err == nil {
		switch opts.OnDuplicate {
		case DuplicateSkip:
			return &CloneResult{
				RepoName:       repoName,
				ClonedPath:     destPath,
				AlreadyExisted: true,
			}, nil

		case DuplicateReplace:
			if err := os.RemoveAll(destPath); err != nil {
				return nil, fmt.Errorf("could not remove existing directory %s: %w", destPath, err)
			}
			// Fall through to clone

		case DuplicateAsk:
			// Caller should have resolved this before calling Clone.
			// Default to replace if they didn't handle it.
			if err := os.RemoveAll(destPath); err != nil {
				return nil, fmt.Errorf("could not remove existing directory %s: %w", destPath, err)
			}
		}
	}

	// Build go-git clone options
	cloneOpts := &gogit.CloneOptions{
		URL:          cleanURL,
		Depth:        0,   // full clone (depth=1 breaks some build systems)
		SingleBranch: false,
		Tags:         gogit.AllTags,
	}

	// Attach auth if provided
	if opts.Auth != nil && opts.Auth.Token != "" {
		cloneOpts.Auth = &http.BasicAuth{
			Username: opts.Auth.Username,
			Password: opts.Auth.Token,
		}
	}

	// Attach progress writer if provided
	if opts.ProgressFn != nil {
		cloneOpts.Progress = &progressWriter{fn: opts.ProgressFn}
	}

	// Perform the clone
	_, err = gogit.PlainClone(destPath, false, cloneOpts)
	if err != nil {
		// Clean up partial clone on failure
		os.RemoveAll(destPath) //nolint:errcheck

		// Provide helpful error messages for common failures
		return nil, friendlyCloneError(err, cleanURL)
	}

	return &CloneResult{
		RepoName:       repoName,
		ClonedPath:     destPath,
		AlreadyExisted: false,
	}, nil
}

// NormaliseURL cleans and validates a git URL.
// Handles:
//   - https://github.com/user/repo
//   - https://github.com/user/repo.git
//   - github.com/user/repo        (no scheme)
//   - git@github.com:user/repo    (SSH — converted to HTTPS)
func NormaliseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)

	// Convert SSH format (git@github.com:user/repo) to HTTPS
	if strings.HasPrefix(raw, "git@") {
		raw = convertSSHToHTTPS(raw)
	}

	// Add scheme if missing
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		raw = "https://" + raw
	}

	// Validate it's a real URL
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}

	if u.Host == "" {
		return "", fmt.Errorf("URL has no host: %s", raw)
	}

	// Ensure .git suffix is present for go-git compatibility
	if !strings.HasSuffix(u.Path, ".git") {
		u.Path = strings.TrimSuffix(u.Path, "/") + ".git"
	}

	return u.String(), nil
}

// convertSSHToHTTPS converts an SSH git URL to HTTPS.
// git@github.com:user/repo.git → https://github.com/user/repo.git
func convertSSHToHTTPS(ssh string) string {
	// Remove "git@" prefix
	s := strings.TrimPrefix(ssh, "git@")
	// Replace first ":" with "/"
	s = strings.Replace(s, ":", "/", 1)
	return "https://" + s
}

// ExtractRepoName returns just the repository name from a URL.
// https://github.com/neovim/neovim.git → "neovim"
// https://github.com/user/my-cool-tool  → "my-cool-tool"
func ExtractRepoName(rawURL string) string {
	rawURL = strings.TrimSuffix(rawURL, ".git")
	rawURL = strings.TrimSuffix(rawURL, "/")

	parts := strings.Split(rawURL, "/")
	if len(parts) == 0 {
		return ""
	}

	name := parts[len(parts)-1]
	name = strings.TrimSpace(name)
	return name
}

// ExtractOwnerRepo returns "owner/repo" from a GitHub URL.
// Used for API calls (stats, search).
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

// IsGitRepo returns true if the given path is a valid git repository.
func IsGitRepo(path string) bool {
	_, err := gogit.PlainOpen(path)
	return err == nil
}

// CurrentRepoName returns the name of the repository at the given path.
// Returns "" if the path is not a git repository.
func CurrentRepoName(path string) string {
	repo, err := gogit.PlainOpen(path)
	if err != nil {
		return ""
	}

	remote, err := repo.Remote("origin")
	if err != nil {
		// No origin remote — fall back to directory name
		return filepath.Base(path)
	}

	urls := remote.Config().URLs
	if len(urls) == 0 {
		return filepath.Base(path)
	}

	return ExtractRepoName(urls[0])
}

// DefaultBranch returns the default branch name of the repo at path.
// Returns "main" as a safe fallback.
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

// friendlyCloneError converts go-git errors into user-friendly messages.
func friendlyCloneError(err error, url string) error {
	msg := err.Error()

	switch {
	case err == transport.ErrAuthenticationRequired || err == transport.ErrAuthorizationFailed:
		return fmt.Errorf(
			"authentication required for %s\n\n"+
				"  To clone private repos, set your GitHub token:\n"+
				"  export GITHUB_TOKEN=your_token_here\n", url)

	case strings.Contains(msg, "repository not found") ||
		strings.Contains(msg, "not found"):
		return fmt.Errorf(
			"repository not found: %s\n\n"+
				"  Check that the URL is correct and the repo is public.", url)

	case strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "dial tcp"):
		return fmt.Errorf(
			"network error — could not reach %s\n\n"+
				"  Check your internet connection and try again.", url)

	case strings.Contains(msg, "already exists") ||
		strings.Contains(msg, "non-empty"):
		return fmt.Errorf(
			"destination directory already exists and is not empty.\n\n"+
				"  Cloneable will ask what to do next time.")

	default:
		return fmt.Errorf("clone failed: %w", err)
	}
}

// ── progressWriter ────────────────────────────────────────────────────────────

// progressWriter adapts a callback function to the io.Writer interface
// expected by go-git's Progress field.
// The raw go-git progress output is forwarded to the logger (install.logs),
// NOT to the UI — the UI only shows the spinner.
type progressWriter struct {
	fn func(msg string)
}

func (pw *progressWriter) Write(p []byte) (n int, err error) {
	if pw.fn != nil {
		pw.fn(string(p))
	}
	return len(p), nil
}
