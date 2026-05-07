package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/manansati/cloneable/internal/ui"
	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall the Cloneable CLI from your system",
	Long: `Deletes the Cloneable binary from your PATH (e.g., /usr/local/bin/cloneable).
This requires sudo privileges on Linux/macOS.

Example:
  cloneable uninstall
`,
	Args:          cobra.NoArgs,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		confirmed, err := ui.Confirm("Are you sure you want to completely uninstall Cloneable?")
		if err != nil || !confirmed {
			fmt.Println("\n  Cancelled — nothing was removed.")
			return nil
		}

		fmt.Println() // Spacing

		// Run sudo rm /usr/local/bin/cloneable
		c := exec.Command("sudo", "rm", "-f", "/usr/local/bin/cloneable")
		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr

		if err := c.Run(); err != nil {
			return fmt.Errorf("failed to remove Cloneable: %w", err)
		}

		fmt.Printf("  %s  Cloneable was successfully uninstalled.\n\n", ui.Tick())
		return nil
	},
}
