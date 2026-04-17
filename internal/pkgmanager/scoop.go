package pkgmanager

// Scoop implements the Scoop package manager for Windows.
// Scoop is particularly good for developer tools and CLI utilities.
// It installs to the user's home directory — no admin rights needed.
type Scoop struct{}

func (s *Scoop) Name() string      { return "scoop" }
func (s *Scoop) IsAvailable() bool { return commandExists("scoop") }

func (s *Scoop) IsInstalled(pkg string) bool {
	err := run(nil, "scoop", "info", pkg)
	return err == nil
}

func (s *Scoop) UpdateIndex(log LogWriter) error {
	return run(log, "scoop", "update")
}

func (s *Scoop) Install(pkg string, log LogWriter) error {
	return run(log, "scoop", "install", pkg)
}

// InstallSelf installs Scoop using the official PowerShell command.
// Scoop installs to ~/scoop by default — no admin rights required.
func (s *Scoop) InstallSelf(log LogWriter) error {
	if commandExists("scoop") {
		return nil
	}
	return run(log,
		"powershell", "-NoProfile", "-ExecutionPolicy", "RemoteSigned",
		"-Command",
		`irm get.scoop.sh | iex`,
	)
}
