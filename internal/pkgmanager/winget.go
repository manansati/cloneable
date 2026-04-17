package pkgmanager

// Winget implements the winget package manager for Windows 10/11.
// winget ships by default on Windows 10 1709+ and all Windows 11 versions.
type Winget struct{}

func (w *Winget) Name() string      { return "winget" }
func (w *Winget) IsAvailable() bool { return commandExists("winget") }

func (w *Winget) IsInstalled(pkg string) bool {
	err := run(nil, "winget", "list", "--id", pkg, "--exact")
	return err == nil
}

func (w *Winget) UpdateIndex(log LogWriter) error {
	// winget source update refreshes package sources
	return run(log, "winget", "source", "update")
}

func (w *Winget) Install(pkg string, log LogWriter) error {
	return run(log,
		"winget", "install",
		"--id", pkg,
		"--exact",
		"--silent",
		"--accept-package-agreements",
		"--accept-source-agreements",
	)
}

func (w *Winget) InstallSelf(log LogWriter) error {
	// winget is part of Windows — cannot be installed via a CLI command
	return ErrCannotSelfInstall
}
