package cmd

import (
	"fmt"
	"os"

	"github.com/manansati/cloneable/internal/logger"
	"github.com/manansati/cloneable/internal/ui"
	"github.com/spf13/cobra"
)

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "View the installation logs for the current repository",
	Long: `Display the installation log for the current repository.

Cloneable stores all verbose output (package installs, build output,
compiler messages) in install.logs in the repo root.

Example:
  cd ~/projects/hyprland
  cloneable --logs
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

		content, err := logger.ReadAll(cwd)
		if err != nil {
			return err
		}

		fmt.Printf("\n  %s\n\n", ui.SaffronBold("install.logs"))
		fmt.Println(ui.Muted(content))
		return nil
	},
}
