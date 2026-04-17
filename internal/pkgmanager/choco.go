package pkgmanager

// Choco implements the Chocolatey package manager for Windows.
// Used as a fallback when winget doesn't have a package.
type Choco struct{}

func (c *Choco) Name() string      { return "choco" }
func (c *Choco) IsAvailable() bool { return commandExists("choco") }

func (c *Choco) IsInstalled(pkg string) bool {
	err := run(nil, "choco", "list", "--local-only", pkg)
	return err == nil
}

func (c *Choco) UpdateIndex(log LogWriter) error {
	// Chocolatey has no separate index refresh step
	return nil
}

func (c *Choco) Install(pkg string, log LogWriter) error {
	return run(log, "choco", "install", pkg, "-y")
}

// InstallSelf installs Chocolatey using the official PowerShell script.
func (c *Choco) InstallSelf(log LogWriter) error {
	if commandExists("choco") {
		return nil
	}
	return run(log,
		"powershell", "-NoProfile", "-ExecutionPolicy", "Bypass",
		"-Command",
		`Set-ExecutionPolicy Bypass -Scope Process -Force; `+
			`[System.Net.ServicePointManager]::SecurityProtocol = [System.Net.ServicePointManager]::SecurityProtocol -bor 3072; `+
			`iex ((New-Object System.Net.WebClient).DownloadString('https://community.chocolatey.org/install.ps1'))`,
	)
}
