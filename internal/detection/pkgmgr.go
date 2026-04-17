package detection

import "os/exec"

// PkgManagerType identifies a specific package manager.
type PkgManagerType string

const (
	// Linux — official
	PkgApt    PkgManagerType = "apt"
	PkgDnf    PkgManagerType = "dnf"
	PkgPacman PkgManagerType = "pacman"
	PkgZypper PkgManagerType = "zypper"
	PkgApk    PkgManagerType = "apk"
	PkgXbps   PkgManagerType = "xbps"
	PkgPortage PkgManagerType = "portage"
	PkgNix    PkgManagerType = "nix"

	// Linux — community / universal
	PkgYay     PkgManagerType = "yay"     // AUR (Arch)
	PkgParu    PkgManagerType = "paru"    // AUR (Arch)
	PkgSnap    PkgManagerType = "snap"
	PkgFlatpak PkgManagerType = "flatpak"

	// macOS
	PkgBrew PkgManagerType = "brew"
	PkgPort PkgManagerType = "port" // MacPorts (rare but supported)

	// Windows
	PkgWinget PkgManagerType = "winget"
	PkgChoco  PkgManagerType = "choco"
	PkgScoop  PkgManagerType = "scoop"

	PkgUnknown PkgManagerType = "unknown"
)

// PkgManagerInfo holds the detected package managers for this system.
type PkgManagerInfo struct {
	// Primary is the main/official package manager for this OS/distro.
	// This is always tried first.
	Primary PkgManagerType

	// Community is the community package manager, if available.
	// e.g. yay or paru on Arch, snap/flatpak on others.
	Community []PkgManagerType

	// All is the full ordered list: Primary first, then Community.
	// This is the order Cloneable will attempt installs.
	All []PkgManagerType
}

// DetectPackageManagers detects all available package managers for the given system.
// It checks for actual binary presence — never assumes based on distro alone.
func DetectPackageManagers(info *OSInfo) *PkgManagerInfo {
	result := &PkgManagerInfo{}

	switch info.Type {
	case OSLinux:
		result.Primary = detectLinuxPrimary(info.Distro)
		result.Community = detectLinuxCommunity(info.Distro)
	case OSMacOS:
		result.Primary = detectMacPrimary()
		result.Community = []PkgManagerType{}
	case OSWindows:
		result.Primary = detectWindowsPrimary()
		result.Community = detectWindowsCommunity()
	}

	// Build the full ordered list
	result.All = []PkgManagerType{result.Primary}
	result.All = append(result.All, result.Community...)

	return result
}

// detectLinuxPrimary returns the official package manager for the given distro.
// It checks binary presence first — so even if the distro guess is wrong,
// we'll find the right manager.
func detectLinuxPrimary(distro LinuxDistro) PkgManagerType {
	// Check by distro family first (fast path)
	switch distro {
	case DistroArch, DistroManjaro, DistroEndeavor:
		if commandExists("pacman") {
			return PkgPacman
		}
	case DistroUbuntu, DistroDebian, DistroMint:
		if commandExists("apt") {
			return PkgApt
		}
	case DistroFedora, DistroRHEL, DistroCentOS:
		if commandExists("dnf") {
			return PkgDnf
		}
	case DistroOpenSUSE:
		if commandExists("zypper") {
			return PkgZypper
		}
	case DistroAlpine:
		if commandExists("apk") {
			return PkgApk
		}
	case DistroVoid:
		if commandExists("xbps-install") {
			return PkgXbps
		}
	case DistroGentoo:
		if commandExists("emerge") {
			return PkgPortage
		}
	case DistroNixOS:
		if commandExists("nix") {
			return PkgNix
		}
	}

	// Fallback: scan for any known package manager regardless of distro
	// (handles unknown distros or WSL environments)
	for _, mgr := range []struct {
		cmd string
		pkg PkgManagerType
	}{
		{"apt", PkgApt},
		{"dnf", PkgDnf},
		{"pacman", PkgPacman},
		{"zypper", PkgZypper},
		{"apk", PkgApk},
		{"xbps-install", PkgXbps},
		{"emerge", PkgPortage},
		{"nix", PkgNix},
	} {
		if commandExists(mgr.cmd) {
			return mgr.pkg
		}
	}

	return PkgUnknown
}

// detectLinuxCommunity returns community package managers available on the system.
// Order matters — yay/paru are preferred over snap/flatpak.
func detectLinuxCommunity(distro LinuxDistro) []PkgManagerType {
	var found []PkgManagerType

	// AUR helpers (Arch family only)
	if distro == DistroArch || distro == DistroManjaro || distro == DistroEndeavor {
		if commandExists("yay") {
			found = append(found, PkgYay)
		} else if commandExists("paru") {
			found = append(found, PkgParu)
		}
	}

	// Universal managers — available on any distro
	if commandExists("snap") {
		found = append(found, PkgSnap)
	}
	if commandExists("flatpak") {
		found = append(found, PkgFlatpak)
	}

	return found
}

// detectMacPrimary returns the primary package manager on macOS.
// Homebrew is overwhelmingly standard; MacPorts is a rare fallback.
func detectMacPrimary() PkgManagerType {
	if commandExists("brew") {
		return PkgBrew
	}
	if commandExists("port") {
		return PkgPort
	}
	// Neither installed — Cloneable will install Homebrew in Phase II
	return PkgUnknown
}

// detectWindowsPrimary returns the primary package manager on Windows.
// winget ships with modern Windows 10/11 by default.
func detectWindowsPrimary() PkgManagerType {
	if commandExists("winget") {
		return PkgWinget
	}
	if commandExists("choco") {
		return PkgChoco
	}
	if commandExists("scoop") {
		return PkgScoop
	}
	return PkgUnknown
}

// detectWindowsCommunity returns additional Windows package managers.
func detectWindowsCommunity() []PkgManagerType {
	var found []PkgManagerType
	// If winget is primary, also check choco and scoop as fallbacks
	if commandExists("choco") {
		found = append(found, PkgChoco)
	}
	if commandExists("scoop") {
		found = append(found, PkgScoop)
	}
	return found
}

// commandExists returns true if the given command is available in PATH.
// This is how we verify a package manager actually exists — no assumptions.
func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// DisplayName returns a clean human-readable name for the UI header bar.
// e.g. PkgPacman → "pacman", PkgYay → "pacman + yay"
func (p *PkgManagerInfo) DisplayName() string {
	if p.Primary == PkgUnknown {
		return "no package manager found"
	}
	name := string(p.Primary)
	// Show the first community manager alongside primary if present
	if len(p.Community) > 0 && p.Community[0] != PkgSnap && p.Community[0] != PkgFlatpak {
		name += " + " + string(p.Community[0])
	}
	return name
}
