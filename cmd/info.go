package cmd

import (
	"fmt"
	"os"

	"github.com/manansati/cloneable/internal/git"
	gh "github.com/manansati/cloneable/internal/github"
	"github.com/spf13/cobra"
)

// infoCmd handles:
//
//	cloneable --info           → breakdown of current repo
//	cloneable info <git-url>  → breakdown of remote repo (no clone needed)
var infoCmd = &cobra.Command{
	Use:           "info [git-url]",
	Short:         "Show language and technology breakdown of a repository",
	SilenceUsage:  true,
	SilenceErrors: true,
	Args:          cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 {
			return remoteInfo(args[0])
		}

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

func remoteInfo(rawURL string) error {
	owner, repo, err := git.ExtractOwnerRepo(rawURL)
	if err != nil {
		return fmt.Errorf("invalid GitHub URL: %w", err)
	}

	fmt.Printf("\n  Fetching info for %s/%s...\n", owner, repo)

	langs, err := gh.FetchLanguages(owner, repo)
	if err != nil {
		return err
	}

	bars := langs.ToSortedBars()
	gh.PrintStats(bars, owner+"/"+repo)
	return nil
}
