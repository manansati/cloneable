package pkgmanager

// Xbps implements the xbps package manager for Void Linux.
type Xbps struct{}

func (x *Xbps) Name() string      { return "xbps" }
func (x *Xbps) IsAvailable() bool { return commandExists("xbps-install") }

func (x *Xbps) IsInstalled(pkg string) bool {
	err := run(nil, "xbps-query", pkg)
	return err == nil
}

func (x *Xbps) UpdateIndex(log LogWriter) error {
	return run(log, "xbps-install", "-S")
}

func (x *Xbps) Install(pkg string, log LogWriter) error {
	return run(log, "xbps-install", "-Sy", pkg)
}

func (x *Xbps) InstallSelf(log LogWriter) error {
	return ErrCannotSelfInstall
}
