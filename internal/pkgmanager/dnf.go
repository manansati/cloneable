package pkgmanager

// Dnf implements the dnf package manager for Fedora, RHEL, CentOS Stream.
type Dnf struct{}

func (d *Dnf) Name() string      { return "dnf" }
func (d *Dnf) IsAvailable() bool { return commandExists("dnf") }

func (d *Dnf) IsInstalled(pkg string) bool {
	err := run(nil, "rpm", "-q", pkg)
	return err == nil
}

func (d *Dnf) UpdateIndex(log LogWriter) error {
	// dnf automatically refreshes metadata on install — explicit check is optional
	return run(log, "dnf", "check-update", "--assumeyes")
}

func (d *Dnf) Install(pkg string, log LogWriter) error {
	return run(log, "dnf", "install", "-y", pkg)
}

func (d *Dnf) InstallSelf(log LogWriter) error {
	return ErrCannotSelfInstall
}
