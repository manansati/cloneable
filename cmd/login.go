package cmd

import (
	"fmt"

	"github.com/manansati/cloneable/internal/github"
	"github.com/manansati/cloneable/internal/ui"
	"github.com/spf13/cobra"
)

var loginCmd = &cobra.Command{
	Use:   "login <github-api-key>",
	Short: "Log in with a GitHub API token to increase search limits",
	Long: `Save a GitHub API token to increase the rate limit for the search command.
Without a token, GitHub limits you to 60 requests per hour. With a token, you get 5,000.

Example:
  cloneable login ghp_abc123...
`,
	Args:          cobra.ExactArgs(1),
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		token := args[0]
		
		fmt.Printf("\n  Validating token...\n")
		if err := github.ValidateToken(token); err != nil {
			return fmt.Errorf("invalid token: %w", err)
		}
		
		cfg := github.LoadConfig()
		cfg.GitHubToken = token
		
		if err := github.SaveConfig(cfg); err != nil {
			return fmt.Errorf("failed to save token: %w", err)
		}
		
		fmt.Printf("  %s  Successfully logged in! Token saved to ~/.cloneable/config.json\n\n", ui.Tick())
		return nil
	},
}
