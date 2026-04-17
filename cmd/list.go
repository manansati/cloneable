package cmd

import (
	"fmt"
	"os"

	"github.com/manansati/cloneable/internal/receipt"
	"github.com/manansati/cloneable/internal/ui"
	"github.com/spf13/cobra"
)

// listCmd handles: cloneable list
// Shows all repos Cloneable has installed.
var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all repositories installed by Cloneable",
	Long: `Show all repositories that Cloneable has cloned and installed.

Displays: name, technology, install path, global binary (if any), install date.

Example:
  cloneable list
`,
	Args:          cobra.NoArgs,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := receipt.NewStore(sysInfo.ReceiptsDir)
		if err != nil {
			return err
		}

		receipts, err := store.All()
		if err != nil {
			return err
		}

		receipt.PrintList(receipts)
		return nil
	},
}

// removeCmd handles: cloneable remove <name>
// Removes a Cloneable-managed installation cleanly.
var removeCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a Cloneable-managed installation",
	Long: `Remove a repository that was installed by Cloneable.

Cloneable will:
  1. Show you what will be removed (symlinks, env dir, repo folder)
  2. Ask for confirmation
  3. Remove symlinks from your PATH
  4. Remove the isolated environment (e.g. .venv, node_modules)
  5. Optionally remove the cloned repo directory itself
  6. Delete the install receipt

Example:
  cloneable remove ghostty
  cloneable remove neovim
`,
	Args:          cobra.ExactArgs(1),
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		store, err := receipt.NewStore(sysInfo.ReceiptsDir)
		if err != nil {
			return err
		}

		// Load the receipt so we can show what will be removed
		r, err := store.Load(name)
		if err != nil {
			return err
		}
		if r == nil {
			return fmt.Errorf("%q is not managed by Cloneable\n\n  Run: cloneable list  to see installed repos", name)
		}

		// Show what will be removed
		receipt.PrintReceipt(r)

		// Ask if they want to remove the repo directory too
		removeRepo := false
		repoChoice, err := ui.RunSelector(
			"What would you like to remove?",
			[]ui.SelectorOption{
				{
					Label:       "Remove everything",
					Description: "Symlinks, env, and the cloned repo folder",
					Value:       "all",
				},
				{
					Label:       "Keep the repo folder",
					Description: "Remove symlinks and env only — keep the source code",
					Value:       "partial",
				},
				{
					Label:       "Cancel",
					Description: "Don't remove anything",
					Value:       "cancel",
				},
			},
		)
		if err != nil {
			return err
		}
		if repoChoice == nil || repoChoice.Value == "cancel" {
			fmt.Println("\n  Cancelled — nothing was removed.")
			return nil
		}

		removeRepo = repoChoice.Value == "all"

		// Confirm
		confirmed, err := ui.Confirm(fmt.Sprintf("Remove %s?", ui.SaffronBold(name)))
		if err != nil || !confirmed {
			fmt.Println("\n  Cancelled — nothing was removed.")
			return nil
		}

		// Perform removal
		if err := store.Remove(name, removeRepo); err != nil {
			return err
		}

		fmt.Printf("\n  %s  %s removed successfully\n\n", ui.Tick(), ui.SaffronBold(name))

		// Check if the binary is still in PATH from before
		if r.BinaryName != "" {
			_ = os.Remove(r.BinaryName) // best effort
		}

		return nil
	},
}
