package cmd

import (
	"fmt"
	"os"

	"github.com/manansati/cloneable/internal/git"
	"github.com/manansati/cloneable/internal/phases"
	"github.com/manansati/cloneable/internal/ui"
	"github.com/spf13/cobra"
)

var fixCmd = &cobra.Command{
	Use:   "fix",
	Short: "Fix broken dependencies and reinstall cleanly",
	Long: `Diagnose and repair dependency problems in the current repository.

Cloneable will:
  1. Detect the technology in this repo
  2. Remove broken or corrupted packages and caches
  3. Reinstall everything from scratch
  4. Verify the environment is healthy

Must be run inside a cloned repository.

Example:
  cd ~/projects/neovim
  cloneable --fix
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

		result, err := phases.RunFix(phases.InstallContext{
			RepoPath: cwd,
			RepoName: repoName,
			OSInfo:   sysInfo,
			PkgInfo:  pkgInfo,
		})
		if err != nil {
			if result != nil && result.Log != nil {
				fmt.Printf("\n  %s  See install.logs: %s\n",
					ui.Warn("!"), result.Log.LogPath)
			}
			return err
		}

		fmt.Printf("\n  %s  All dependencies fixed successfully!\n\n", ui.Tick())
		if result.Log != nil {
			fmt.Printf("  %s  Logs: %s\n\n", ui.Muted("→"), ui.Muted(result.Log.LogPath))
		}
		return nil
	},
}
