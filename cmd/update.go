package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	gh "github.com/manansati/cloneable/internal/github"
	"github.com/manansati/cloneable/internal/ui"
	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update Cloneable to the latest version",
	SilenceUsage:  true,
	SilenceErrors: true,
	Args:          cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("\n  Current version: %s\n", ui.SaffronBold("v"+Version))
		fmt.Printf("  Checking for updates...\n\n")

		latest, err := fetchLatestVersion()
		if err != nil {
			// Non-fatal — just say can't check
			fmt.Printf("  %s  Could not check for updates: %s\n\n", ui.Warn("!"), err)
			return nil
		}

		if latest == "" {
			fmt.Printf("  %s  No releases published yet — you have the latest source build.\n\n", ui.Tick())
			return nil
		}

		if !isNewerVersion(latest, Version) {
			fmt.Printf("  %s  Already on the latest version!\n\n", ui.Tick())
			return nil
		}

		fmt.Printf("  %s  New version available: %s\n\n",
			ui.Saffron("↑"), ui.SaffronBold("v"+latest))
		fmt.Printf("  Updating Cloneable...\n")

		c := exec.Command("sh", "-c", "curl -fsSL https://raw.githubusercontent.com/manansati/cloneable/main/scripts/install.sh | sudo sh")
		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr

		if err := c.Run(); err != nil {
			return fmt.Errorf("failed to update: %w", err)
		}
		
		return nil
	},
}

// silentUpdateCheck runs as a goroutine on every startup.
// If a new version is found, prints a one-line notice.
func silentUpdateCheck() {
	latest, err := fetchLatestVersion()
	if err != nil || latest == "" {
		return
	}
	if isNewerVersion(latest, Version) {
		fmt.Printf("\n  %s  Cloneable %s is available  (you have %s)  run: %s\n",
			ui.Saffron("↑"),
			ui.SaffronBold("v"+latest),
			ui.Muted("v"+Version),
			ui.Muted("cloneable update"),
		)
	}
}

// fetchLatestVersion returns the latest release tag, or "" if none exist.
func fetchLatestVersion() (string, error) {
	return gh.FetchLatestVersion("manansati/cloneable")
}

// isNewerVersion returns true if candidate is strictly newer than current.
func isNewerVersion(candidate, current string) bool {
	cp := splitVersion(candidate)
	cur := splitVersion(current)
	for i := 0; i < len(cp) && i < len(cur); i++ {
		if cp[i] > cur[i] {
			return true
		}
		if cp[i] < cur[i] {
			return false
		}
	}
	return len(cp) > len(cur)
}

func splitVersion(v string) []int {
	parts := strings.Split(v, ".")
	nums := make([]int, 0, len(parts))
	for _, p := range parts {
		n := 0
		for _, c := range p {
			if c >= '0' && c <= '9' {
				n = n*10 + int(c-'0')
			}
		}
		nums = append(nums, n)
	}
	return nums
}
