package pkgmanager

// Brew implements the Homebrew package manager for macOS (and Linux).
// If Homebrew is not installed, InstallSelf() will install it automatically
// using the official install script.
type Brew struct{}

func (b *Brew) Name() string      { return "brew" }
func (b *Brew) IsAvailable() bool { return commandExists("brew") }

func (b *Brew) IsInstalled(pkg string) bool {
	err := run(nil, "brew", "list", "--formula", pkg)
	if err == nil {
		return true
	}
	// Also check casks (GUI apps installed via brew)
	err = run(nil, "brew", "list", "--cask", pkg)
	return err == nil
}

func (b *Brew) UpdateIndex(log LogWriter) error {
	return run(log, "brew", "update")
}

func (b *Brew) Install(pkg string, log LogWriter) error {
	// Try formula first, fall back to cask for GUI apps
	err := run(log, "brew", "install", pkg)
	if err != nil {
		// Some packages are casks (e.g. "flutter" is a cask)
		return run(log, "brew", "install", "--cask", pkg)
	}
	return nil
}

// InstallSelf installs Homebrew using the official curl installer.
// This is the standard, widely-used method documented at brew.sh.
// Requires: curl, bash (both present on all macOS versions).
func (b *Brew) InstallSelf(log LogWriter) error {
	if commandExists("brew") {
		return nil
	}

	// Official Homebrew install script
	return run(log,
		"/bin/bash", "-c",
		`curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh | bash`,
	)
}
