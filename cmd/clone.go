package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/manansati/cloneable/internal/git"
	"github.com/manansati/cloneable/internal/ui"
	"github.com/spf13/cobra"
)

// cloneCmd handles: cloneable clone <git-url>
// Clones the repository into the current working directory.
// Phase I only — no dependency installation, no launch.
var cloneCmd = &cobra.Command{
	Use:   "clone <git-url>",
	Short: "Clone a repository without installing or launching",
	Long: `Clone a GitHub (or any git) repository into the current directory.
This performs Phase I only — no dependency installation, no launch.

Example:
  cloneable clone https://github.com/neovim/neovim
`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runClone(args[0], git.DuplicateAsk)
	},
}

// runClone is the shared clone implementation used by both cloneCmd
// and runFullFlow. It shows the header, handles duplicates, and runs
// the clone with an animated spinner.
//
// onDuplicate controls what happens if the target folder already exists.
// Returns the CloneResult so the full flow can proceed to Phase II/III.
func runClone(rawURL string, onDuplicate git.DuplicateAction) (*git.CloneResult, error) {
	// Get current working directory — clone goes here
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("could not determine current directory: %w", err)
	}

	// Extract repo name early so we can show it in the header
	repoName := git.ExtractRepoName(rawURL)

	// Print the header now that we know the repo name
	ui.PrintHeader(ui.HeaderInfo{
		OS:         sysInfo.DisplayName(),
		Distro:     string(sysInfo.Distro),
		PkgManager: pkgInfo.DisplayName(),
		RepoName:   repoName,
	})

	// Check for an existing directory and ask user what to do
	if onDuplicate == git.DuplicateAsk {
		resolved, err := resolveDuplicate(cwd, repoName)
		if err != nil {
			return nil, err
		}
		onDuplicate = resolved
	}

	// Run the clone with a spinner
	var result *git.CloneResult
	cloneErr := ui.RunWithSpinner("Cloning", func() error {
		r, err := git.Clone(git.CloneOptions{
			URL:         rawURL,
			DestDir:     cwd,
			OnDuplicate: onDuplicate,
			Auth:        gitAuthFromEnv(),
		})
		if err != nil {
			return err
		}
		result = r
		return nil
	})

	if cloneErr != nil {
		return nil, cloneErr
	}

	// Report outcome
	if result.AlreadyExisted {
		fmt.Printf("\n  %s  Using existing directory: %s\n",
			ui.Tick(), ui.SaffronBold(result.ClonedPath))
	} else {
		fmt.Printf("\n  %s  %s cloned to %s\n",
			ui.Tick(),
			ui.SaffronBold(result.RepoName),
			ui.Muted(result.ClonedPath),
		)
	}

	return result, nil
}

// resolveDuplicate checks if a directory already exists and asks the user
// what to do. Returns the resolved DuplicateAction.
func resolveDuplicate(destDir, repoName string) (git.DuplicateAction, error) {
	targetPath := filepath.Join(destDir, repoName)

	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		// Directory doesn't exist — nothing to resolve
		return git.DuplicateReplace, nil
	}

	// Directory exists — show the selector
	opts := []ui.SelectorOption{
		{
			Label:       "Replace it",
			Description: "Delete the existing folder and clone fresh",
			Value:       "replace",
		},
		{
			Label:       "Use existing",
			Description: "Skip clone and use the folder as-is",
			Value:       "skip",
		},
	}

	fmt.Printf("\n  %s  Directory %s already exists.\n\n",
		ui.Warn("!"), ui.SaffronBold(repoName))

	result, err := ui.RunSelector("What would you like to do?", opts)
	if err != nil {
		return git.DuplicateSkip, err
	}
	if result == nil {
		return git.DuplicateSkip, fmt.Errorf("cancelled")
	}

	if result.Value == "replace" {
		return git.DuplicateReplace, nil
	}
	return git.DuplicateSkip, nil
}

// gitAuthFromEnv reads GitHub credentials from environment variables.
// GITHUB_TOKEN is the standard variable used by GitHub CLI and Actions.
func gitAuthFromEnv() *git.Auth {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return nil
	}
	return &git.Auth{
		Username: "token", // GitHub accepts any non-empty username with a token
		Token:    token,
	}
}
