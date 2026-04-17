package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/manansati/cloneable/internal/ui"
	"github.com/spf13/cobra"
)

// updateCmd handles: cloneable update
var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update Cloneable to the latest version",
	Long: `Check for a newer version of Cloneable and update if available.

Cloneable also performs a silent background check on every run
and notifies you if an update is available.

Example:
  cloneable update
`,
	Args:          cobra.NoArgs,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("\n  Current version: %s\n", ui.SaffronBold("v"+Version))
		fmt.Printf("  Checking for updates...\n\n")

		latest, err := fetchLatestVersion()
		if err != nil {
			return fmt.Errorf("could not check for updates: %w", err)
		}

		if latest == "" || latest == Version {
			fmt.Printf("  %s  Already on the latest version!\n\n", ui.Tick())
			return nil
		}

		fmt.Printf("  %s  New version available: %s\n\n",
			ui.Saffron("↑"), ui.SaffronBold("v"+latest))

		fmt.Printf("  To update, run:\n\n")
		if runtime.GOOS == "windows" {
			fmt.Printf("    irm https://raw.githubusercontent.com/manansati/cloneable/main/scripts/install.ps1 | iex\n\n")
		} else {
			fmt.Printf("    curl -fsSL https://raw.githubusercontent.com/manansati/cloneable/main/scripts/install.sh | sh\n\n")
		}
		return nil
	},
}

// silentUpdateCheck runs in a goroutine on every startup.
// If a new version is found, it prints a one-line notice after the main
// command finishes. Uses a short timeout so it never blocks.
func silentUpdateCheck() {
	latest, err := fetchLatestVersion()
	if err != nil || latest == "" || latest == Version {
		return
	}

	// Only notify if it's actually newer
	if isNewerVersion(latest, Version) {
		fmt.Printf("\n  %s  Cloneable %s is available  (you have %s)\n"+
			"      Run: %s\n",
			ui.Saffron("↑"),
			ui.SaffronBold("v"+latest),
			ui.Muted("v"+Version),
			ui.Muted("cloneable update"),
		)
	}
}

// githubRelease is the minimal shape of the GitHub releases API response.
type githubRelease struct {
	TagName string `json:"tag_name"`
}

// fetchLatestVersion calls the GitHub releases API and returns the latest
// version string (without the "v" prefix).
func fetchLatestVersion() (string, error) {
	client := &http.Client{Timeout: 4 * time.Second}

	resp, err := client.Get("https://api.github.com/repos/manansati/cloneable/releases/latest")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var release githubRelease
	if err := json.Unmarshal(body, &release); err != nil {
		return "", err
	}

	// Strip leading "v" so we can compare to Version which has no "v"
	version := strings.TrimPrefix(release.TagName, "v")
	return version, nil
}

// isNewerVersion returns true if candidate is strictly newer than current.
// Simple semver comparison: splits on "." and compares numerically left to right.
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
