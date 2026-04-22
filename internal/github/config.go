package github

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config holds Cloneable's GitHub settings, stored at ~/.cloneable/config.json
type Config struct {
	GitHubToken string `json:"github_token,omitempty"`
}

var configPath string

func init() {
	home, err := os.UserHomeDir()
	if err == nil {
		configPath = filepath.Join(home, ".cloneable", "config.json")
	}
}

// LoadConfig reads ~/.cloneable/config.json. Returns empty config if file doesn't exist.
func LoadConfig() Config {
	if configPath == "" {
		return Config{}
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return Config{}
	}
	var cfg Config
	_ = json.Unmarshal(data, &cfg)
	return cfg
}

// SaveConfig writes the config to ~/.cloneable/config.json.
func SaveConfig(cfg Config) error {
	if configPath == "" {
		return nil
	}
	_ = os.MkdirAll(filepath.Dir(configPath), 0755)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0600) // 0600 = owner read/write only (token is sensitive)
}

// GetToken returns the active GitHub token.
// Priority: env var GITHUB_TOKEN > saved config token.
func GetToken() string {
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		return t
	}
	return LoadConfig().GitHubToken
}

// PromptForToken shows an interactive prompt asking the user for a GitHub token.
// Saves it to config if provided. Returns the token (empty if user skipped).
func PromptForToken(reason string) string {
	fmt.Printf("\n  %s\n\n", reason)
	fmt.Printf("  You can create a free token at:\n")
	fmt.Printf("  https://github.com/settings/tokens/new\n")
	fmt.Printf("  (No special scopes needed for public repos)\n\n")
	fmt.Printf("  Enter your GitHub token (or press Enter to skip): ")

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return ""
	}

	token := strings.TrimSpace(input)
	if token == "" {
		fmt.Printf("\n  Skipped. You can set it later with:\n")
		fmt.Printf("  export GITHUB_TOKEN=your_token\n\n")
		return ""
	}

	// Save it
	cfg := LoadConfig()
	cfg.GitHubToken = token
	if err := SaveConfig(cfg); err == nil {
		fmt.Printf("\n  ✓  Token saved to ~/.cloneable/config.json\n\n")
	}

	return token
}
