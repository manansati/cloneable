package pkgmanager

// Zypper implements the zypper package manager for openSUSE Leap and Tumbleweed.
type Zypper struct{}

func (z *Zypper) Name() string      { return "zypper" }
func (z *Zypper) IsAvailable() bool { return commandExists("zypper") }

func (z *Zypper) IsInstalled(pkg string) bool {
	err := run(nil, "rpm", "-q", pkg)
	return err == nil
}

func (z *Zypper) UpdateIndex(log LogWriter) error {
	return sudoRun(log, "zypper", "--non-interactive", "refresh")
}

func (z *Zypper) Install(pkg string, log LogWriter) error {
	return sudoRun(log, "zypper", "--non-interactive", "install", pkg)
}

func (z *Zypper) InstallSelf(log LogWriter) error {
	return ErrCannotSelfInstall
}
