package cmd

import (
	"fmt"
	"os"

	"github.com/manansati/cloneable/internal/git"
	"github.com/manansati/cloneable/internal/phases"
	"github.com/manansati/cloneable/internal/ui"
	"github.com/spf13/cobra"
)

// runCmd handles: cloneable --run / cloneable -r
// Runs install + launch on the current repo — skips clone.
var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Launch the app in the current repository (skips clone & install)",
	Long: `Launch the application in the current directory.
You must already be inside a cloned repository.
Cloneable will detect the repo type and launch it appropriately.

Example:
  cd ~/projects/ghostty
  cloneable --run
`,
	Args:          cobra.NoArgs,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !isInsideGitRepo() {
			return fmt.Errorf("not inside a git repository — please cd into a cloned repo first")
		}

		cwd, err := os.Getwd()
		if err != nil {
			return err
		}

		repoName := git.CurrentRepoName(cwd)

		ui.PrintHeader(ui.HeaderInfo{
			OS:         sysInfo.DisplayName(),
			Distro:     string(sysInfo.Distro),
			PkgManager: pkgInfo.DisplayName(),
			RepoName:   repoName,
		})

		// Run install first (detects tech, sets up env)
		installResult, err := phases.RunInstall(phases.InstallContext{
			RepoPath: cwd,
			RepoName: repoName,
			OSInfo:   sysInfo,
			PkgInfo:  pkgInfo,
		})
		if err != nil {
			return err
		}

		// Then launch
		_, launchErr := phases.RunLaunch(phases.LaunchContext{
			InstallResult: installResult,
			RepoPath:      cwd,
			RepoName:      repoName,
			OSInfo:        sysInfo,
			PkgInfo:       pkgInfo,
		})
		if launchErr != nil {
			return launchErr
		}

		return nil
	},
}
