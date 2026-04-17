package pkgmanager

// Snap implements the Snap universal package manager for Linux.
// Used as a fallback when the distro's official manager doesn't have a package.
type Snap struct{}

func (s *Snap) Name() string      { return "snap" }
func (s *Snap) IsAvailable() bool { return commandExists("snap") }

func (s *Snap) IsInstalled(pkg string) bool {
	err := run(nil, "snap", "list", pkg)
	return err == nil
}

func (s *Snap) UpdateIndex(log LogWriter) error {
	// Snap auto-refreshes — no explicit update needed
	return nil
}

func (s *Snap) Install(pkg string, log LogWriter) error {
	return run(log, "snap", "install", pkg)
}

// InstallSelf installs snapd via apt/dnf if it's not already present.
// snapd is the daemon that manages snap packages.
func (s *Snap) InstallSelf(log LogWriter) error {
	if commandExists("apt-get") {
		return run(log, "apt-get", "install", "-y", "snapd")
	}
	if commandExists("dnf") {
		return run(log, "dnf", "install", "-y", "snapd")
	}
	return ErrCannotSelfInstall
}
