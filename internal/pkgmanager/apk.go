package pkgmanager

// Apk implements the apk package manager for Alpine Linux.
type Apk struct{}

func (a *Apk) Name() string      { return "apk" }
func (a *Apk) IsAvailable() bool { return commandExists("apk") }

func (a *Apk) IsInstalled(pkg string) bool {
	err := run(nil, "apk", "info", "-e", pkg)
	return err == nil
}

func (a *Apk) UpdateIndex(log LogWriter) error {
	return sudoRun(log, "apk", "update")
}

func (a *Apk) Install(pkg string, log LogWriter) error {
	return sudoRun(log, "apk", "add", "--no-cache", pkg)
}

func (a *Apk) InstallSelf(log LogWriter) error {
	return ErrCannotSelfInstall
}
