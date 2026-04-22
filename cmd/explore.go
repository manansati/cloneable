package cmd

import (
	"fmt"
	"time"

	gh "github.com/manansati/cloneable/internal/github"
	"github.com/manansati/cloneable/internal/ui"
	"github.com/spf13/cobra"
)

var exploreCmd = &cobra.Command{
	Use:   "explore",
	Short: "Explore trending repositories",
	RunE: func(cmd *cobra.Command, args []string) error {
		ui.PrintHeader(ui.HeaderInfo{
			OS:         sysInfo.DisplayName(),
			Distro:     string(sysInfo.Distro),
			PkgManager: pkgInfo.DisplayName(),
		})

		fmt.Printf("  Fetching trending repositories...\n\n")

		// Let's get repos from the last 30 days sorted by stars
		dateStr := time.Now().AddDate(0, -1, 0).Format("2006-01-02")
		query := "created:>" + dateStr

		results, total, err := gh.ExploreTrending(query, 1)
		if err != nil {
			return err
		}

		if len(results) == 0 {
			fmt.Printf("  No trending repositories found.\n\n")
			return nil
		}

		chosen, err := gh.RunSearchUI("Trending (Last 30 days)", results, total)
		if err != nil {
			return err
		}
		if chosen == nil {
			fmt.Println("\n  Cancelled.")
			return nil
		}

		fmt.Printf("\n  %s  Selected: %s\n\n", ui.Tick(), ui.SaffronBold(chosen.FullName))
		return runFullFlow(chosen.CloneURL)
	},
}
