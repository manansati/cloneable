package pkgmanager

// Flatpak implements the Flatpak universal package manager for Linux.
// Used as a last-resort fallback — Flatpak packages are sandboxed and
// may not be suitable for CLI tools, but it's better than failing.
type Flatpak struct{}

func (f *Flatpak) Name() string      { return "flatpak" }
func (f *Flatpak) IsAvailable() bool { return commandExists("flatpak") }

func (f *Flatpak) IsInstalled(pkg string) bool {
	err := run(nil, "flatpak", "info", pkg)
	return err == nil
}

func (f *Flatpak) UpdateIndex(log LogWriter) error {
	return run(log, "flatpak", "update", "--noninteractive")
}

func (f *Flatpak) Install(pkg string, log LogWriter) error {
	// Add Flathub if not already added — it's the main Flatpak repository
	_ = run(log, "flatpak", "remote-add", "--if-not-exists", "flathub",
		"https://dl.flathub.org/repo/flathub.flatpakrepo")

	return run(log, "flatpak", "install", "--noninteractive", "flathub", pkg)
}

func (f *Flatpak) InstallSelf(log LogWriter) error {
	if commandExists("apt-get") {
		return run(log, "apt-get", "install", "-y", "flatpak")
	}
	if commandExists("dnf") {
		return run(log, "dnf", "install", "-y", "flatpak")
	}
	if commandExists("pacman") {
		return run(log, "pacman", "-S", "--noconfirm", "flatpak")
	}
	return ErrCannotSelfInstall
}
