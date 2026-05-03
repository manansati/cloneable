package cmd

import (
	"fmt"
	"strings"

	gh "github.com/manansati/cloneable/internal/github"
	"github.com/manansati/cloneable/internal/ui"
	"github.com/spf13/cobra"
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search GitHub repositories and launch one interactively",
	Long: `Search GitHub for repositories matching your query.

Results are shown in a scrollable list. Use arrow keys to navigate,
press Enter to clone + install + launch the selected repo.

Set GITHUB_TOKEN for higher rate limits (5,000 req/hr vs 60/hr).

Examples:
  cloneable search ghostty
  cloneable search neovim
  cloneable search "terminal file manager"
`,
	Args:          cobra.MinimumNArgs(1),
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		query := strings.Join(args, " ")

		// Print header with search context
		ui.PrintHeader(ui.HeaderInfo{
			OS:         sysInfo.DisplayName(),
			Distro:     string(sysInfo.Distro),
			PkgManager: pkgInfo.DisplayName(),
		})

		fmt.Printf("  Searching GitHub for %q...\n\n",
			ui.SaffronBold(query))

		// Fetch results
		results, total, err := gh.SearchRepos(query, 1)
		if err != nil {
			return err
		}

		if len(results) == 0 {
			fmt.Printf("  No results found for %q\n\n", query)
			return nil
		}

		// Show arrow-key UI
		chosen, err := gh.RunSearchUI(query, results, total)
		if err != nil {
			return err
		}
		if chosen == nil {
			fmt.Println("\n  Cancelled.")
			return nil
		}

		// User picked a repo — show details and start flow
		fmt.Printf("\n  %s  Selected: %s\n",
			ui.Tick(), ui.SaffronBold(chosen.FullName))
		if chosen.Description != "" {
			fmt.Printf("  %s  %s\n", ui.Muted("→"), chosen.Description)
		}
		fmt.Printf("  %s  ⭐ %s  •  %s\n\n",
			ui.Muted("→"), gh.FormatStars(chosen.Stars), chosen.Language)

		return runFullFlow(chosen.CloneURL)
	},
}

