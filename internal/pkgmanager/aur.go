package pkgmanager

// AUR implements AUR helper support for Arch Linux.
// It tries yay first, then paru. If neither is installed, it can
// install yay from source using git + makepkg.
type AUR struct {
	helper string // "yay" or "paru" — detected at runtime
}

func (a *AUR) Name() string { return "aur (" + a.resolveHelper() + ")" }

func (a *AUR) IsAvailable() bool {
	return commandExists("yay") || commandExists("paru")
}

func (a *AUR) IsInstalled(pkg string) bool {
	helper := a.resolveHelper()
	if helper == "" {
		return false
	}
	err := run(nil, helper, "-Q", pkg)
	return err == nil
}

func (a *AUR) UpdateIndex(log LogWriter) error {
	helper := a.resolveHelper()
	if helper == "" {
		return nil
	}
	return run(log, helper, "-Sy", "--noconfirm")
}

func (a *AUR) Install(pkg string, log LogWriter) error {
	helper := a.resolveHelper()
	if helper == "" {
		// Try to install yay first
		if err := a.InstallSelf(log); err != nil {
			return err
		}
		helper = "yay"
	}
	return run(log, helper, "-S", "--noconfirm", "--needed", pkg)
}

// InstallSelf installs yay from the AUR using git + makepkg.
// This is the standard bootstrap method for yay on a fresh Arch install.
// Requires: git, base-devel (should already be present on Arch).
func (a *AUR) InstallSelf(log LogWriter) error {
	if commandExists("yay") || commandExists("paru") {
		return nil
	}

	// Clone yay from AUR and build it
	steps := []struct {
		name string
		args []string
	}{
		{"git", []string{"git", "clone", "https://aur.archlinux.org/yay.git", "/tmp/yay-install"}},
		{"makepkg", []string{"makepkg", "-si", "--noconfirm", "-C", "/tmp/yay-install"}},
	}

	for _, step := range steps {
		if err := run(log, step.args[0], step.args[1:]...); err != nil {
			return err
		}
	}
	return nil
}

// resolveHelper returns the available AUR helper binary name.
func (a *AUR) resolveHelper() string {
	if a.helper != "" {
		return a.helper
	}
	if commandExists("yay") {
		a.helper = "yay"
	} else if commandExists("paru") {
		a.helper = "paru"
	}
	return a.helper
}
