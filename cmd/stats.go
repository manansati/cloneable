package cmd

import (
	"fmt"
	"os"

	"github.com/manansati/cloneable/internal/git"
	gh "github.com/manansati/cloneable/internal/github"
	"github.com/spf13/cobra"
)

var statsCmd = &cobra.Command{
	Use:   "stats [git-url]",
	Short: "Show language and technology breakdown of a repository",
	Long: `Display a breakdown of languages and technologies used in a repository.

Without a URL, analyzes the current directory.
With a URL, fetches stats from GitHub without cloning the repo.

Examples:
  cloneable --stats
  cloneable stats https://github.com/neovim/neovim
`,
	Args:          cobra.MaximumNArgs(1),
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// ── Remote repo via GitHub API ─────────────────────────────────────────
		if len(args) == 1 {
			return remoteStats(args[0])
		}

		// ── Local repo via file walk ───────────────────────────────────────────
		if !isInsideGitRepo() {
			return fmt.Errorf("not inside a git repository — provide a URL or cd into a repo")
		}

		cwd, err := os.Getwd()
		if err != nil {
			return err
		}

		repoName := git.CurrentRepoName(cwd)
		fmt.Printf("\n  Scanning %s...\n", repoName)

		stats, err := gh.ScanLocalStats(cwd)
		if err != nil {
			return fmt.Errorf("could not scan repo: %w", err)
		}

		bars := stats.ToSortedBars()
		gh.PrintStats(bars, repoName)
		return nil
	},
}

// remoteStats fetches and prints stats for a remote repo using the GitHub API.
func remoteStats(rawURL string) error {
	owner, repo, err := git.ExtractOwnerRepo(rawURL)
	if err != nil {
		return fmt.Errorf("invalid GitHub URL: %w", err)
	}

	fmt.Printf("\n  Fetching stats for %s/%s...\n", owner, repo)

	langs, err := gh.FetchLanguages(owner, repo)
	if err != nil {
		return err
	}

	bars := langs.ToSortedBars()
	gh.PrintStats(bars, owner+"/"+repo)
	return nil
}

