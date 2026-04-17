package pkgmanager

// Pacman implements the pacman package manager for Arch Linux and derivatives.
type Pacman struct{}

func (p *Pacman) Name() string      { return "pacman" }
func (p *Pacman) IsAvailable() bool { return commandExists("pacman") }

func (p *Pacman) IsInstalled(pkg string) bool {
	// pacman -Q queries the local package database — fast and accurate
	err := run(nil, "pacman", "-Q", pkg)
	return err == nil
}

func (p *Pacman) UpdateIndex(log LogWriter) error {
	return sudoRun(log, "pacman", "-Sy", "--noconfirm")
}

func (p *Pacman) Install(pkg string, log LogWriter) error {
	return sudoRun(log, "pacman", "-S", "--noconfirm", "--needed", pkg)
}

func (p *Pacman) InstallSelf(log LogWriter) error {
	return ErrCannotSelfInstall
}
