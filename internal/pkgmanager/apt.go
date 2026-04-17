package pkgmanager

// Apt implements the apt package manager for Debian/Ubuntu/Mint/Pop!_OS.
type Apt struct{}

func (a *Apt) Name() string      { return "apt" }
func (a *Apt) IsAvailable() bool { return commandExists("apt-get") }

func (a *Apt) IsInstalled(pkg string) bool {
	// dpkg-query is the accurate way to check apt package status
	err := run(nil, "dpkg-query", "-W", "-f=${Status}", pkg)
	return err == nil
}

func (a *Apt) UpdateIndex(log LogWriter) error {
	return run(log, "apt-get", "update", "-y")
}

func (a *Apt) Install(pkg string, log LogWriter) error {
	return run(log,
		"apt-get", "install", "-y",
		"--no-install-recommends",
		pkg,
	)
}

func (a *Apt) InstallSelf(log LogWriter) error {
	// apt is provided by the OS — cannot self-install
	return ErrCannotSelfInstall
}
