// Package receipt manages Cloneable's local install database.
// Every successful install writes a JSON receipt to ~/.cloneable/receipts/<name>.json
// This powers: cloneable list, cloneable remove, and the self-update check.
package receipt

import "time"

// Receipt records everything about a single Cloneable-managed installation.
// One file per repo: ~/.cloneable/receipts/<repo-name>.json
type Receipt struct {
	// Identity
	Name      string `json:"name"`       // repo name, e.g. "ghostty"
	URL       string `json:"url"`        // original clone URL
	ClonePath string `json:"clone_path"` // absolute path to the cloned repo

	// Technology
	Tech     string `json:"tech"`     // primary tech, e.g. "Rust"
	Category string `json:"category"` // e.g. "CLI Tool"

	// Install info
	InstalledAt     time.Time `json:"installed_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	GloballyInstalled bool    `json:"globally_installed"`

	// Symlinks created by Cloneable — needed for clean removal
	Symlinks []SymlinkRecord `json:"symlinks,omitempty"`

	// Environment directory inside the repo (e.g. .venv, node_modules)
	EnvDir string `json:"env_dir,omitempty"`

	// Binary name registered globally (e.g. "ghostty")
	BinaryName string `json:"binary_name,omitempty"`

	// LogPath is the install.logs location for this repo
	LogPath string `json:"log_path,omitempty"`

	// Version is the git tag or commit hash at time of install
	Version string `json:"version,omitempty"`
}

// SymlinkRecord tracks a single symlink so it can be removed cleanly.
type SymlinkRecord struct {
	Source string `json:"source"` // binary inside env
	Target string `json:"target"` // global symlink in ~/.local/bin
}
