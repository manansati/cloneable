package pkgmanager

import (
	"fmt"
	"runtime"

	"github.com/manansati/cloneable/internal/detection"
)

// Cascade is the main entry point for all package installation in Cloneable.
// It holds an ordered list of managers and tries each one in turn.
//
// Install order:
//  1. Primary official manager (apt, pacman, dnf, brew, winget...)
//  2. Community manager if primary fails (yay/paru on Arch, choco/scoop on Windows)
//  3. Universal fallback (snap, then flatpak on Linux)
//  4. Error — package truly not available anywhere
type Cascade struct {
	managers []Manager
	indexUpdated bool // track if we've already refreshed the index this session
}

// NewCascade builds a Cascade for the current system using the detected
// OS and package manager info. Called once at startup.
func NewCascade(osInfo *detection.OSInfo, pkgInfo *detection.PkgManagerInfo) *Cascade {
	c := &Cascade{}

	switch osInfo.Type {
	case detection.OSLinux:
		c.managers = buildLinuxCascade(osInfo.Distro, pkgInfo)
	case detection.OSMacOS:
		c.managers = buildMacCascade()
	case detection.OSWindows:
		c.managers = buildWindowsCascade()
	default:
		// Unknown OS — try brew as a heuristic (works on Linux too)
		c.managers = []Manager{&Brew{}}
	}

	return c
}

// Install installs a system package using the cascade of available managers.
// It tries each manager in order, stopping at the first success.
// All output is forwarded to logWriter (install.logs).
func (c *Cascade) Install(pkg string, logWriter LogWriter) error {
	if len(c.managers) == 0 {
		return fmt.Errorf("no package manager available on this system")
	}

	// Refresh the index once per session before the first install
	if !c.indexUpdated {
		// Best-effort — don't fail if update fails
		_ = c.managers[0].UpdateIndex(logWriter)
		c.indexUpdated = true
	}

	var lastErr error
	for _, mgr := range c.managers {
		if !mgr.IsAvailable() {
			// Try to self-install this manager before skipping it
			if err := mgr.InstallSelf(logWriter); err != nil {
				continue // Can't install the manager — skip it
			}
			if !mgr.IsAvailable() {
				continue // Self-install failed silently
			}
		}

		// Skip if already installed
		if mgr.IsInstalled(pkg) {
			if logWriter != nil {
				logWriter(fmt.Sprintf("[%s] %s is already installed — skipping", mgr.Name(), pkg))
			}
			return nil
		}

		// Attempt install
		err := mgr.Install(pkg, logWriter)
		if err == nil {
			return nil // Success
		}

		// Log the failure and try the next manager
		if logWriter != nil {
			logWriter(fmt.Sprintf("[%s] failed to install %s: %v — trying next manager", mgr.Name(), pkg, err))
		}
		lastErr = err
	}

	// All managers failed
	return fmt.Errorf("could not install %s using any available package manager: %w", pkg, lastErr)
}

// IsInstalled returns true if the package is installed according to any manager.
func (c *Cascade) IsInstalled(pkg string) bool {
	for _, mgr := range c.managers {
		if mgr.IsAvailable() && mgr.IsInstalled(pkg) {
			return true
		}
	}
	return false
}

// PrimaryName returns the name of the first (primary) package manager.
func (c *Cascade) PrimaryName() string {
	if len(c.managers) == 0 {
		return "none"
	}
	return c.managers[0].Name()
}

// InstallMany installs a list of packages, stopping on first failure.
// Returns a map of package → error for any that failed.
func (c *Cascade) InstallMany(pkgs []string, logWriter LogWriter) map[string]error {
	failures := make(map[string]error)
	for _, pkg := range pkgs {
		if err := c.Install(pkg, logWriter); err != nil {
			failures[pkg] = err
		}
	}
	return failures
}

// ── Builder functions ─────────────────────────────────────────────────────────

// buildLinuxCascade returns the ordered manager list for the given Linux distro.
// Order: official → AUR/PPA/COPR → snap → flatpak
func buildLinuxCascade(distro detection.LinuxDistro, pkgInfo *detection.PkgManagerInfo) []Manager {
	var managers []Manager

	// Primary official manager
	switch distro {
	case detection.DistroArch, detection.DistroManjaro, detection.DistroEndeavor:
		managers = append(managers, &Pacman{})
		managers = append(managers, &AUR{})
	case detection.DistroUbuntu, detection.DistroDebian, detection.DistroMint:
		managers = append(managers, &Apt{})
	case detection.DistroFedora, detection.DistroRHEL, detection.DistroCentOS:
		managers = append(managers, &Dnf{})
	case detection.DistroOpenSUSE:
		managers = append(managers, &Zypper{})
	case detection.DistroAlpine:
		managers = append(managers, &Apk{})
	case detection.DistroVoid:
		managers = append(managers, &Xbps{})
	default:
		// Unknown distro — scan what's actually available
		if commandExists("apt-get") {
			managers = append(managers, &Apt{})
		} else if commandExists("dnf") {
			managers = append(managers, &Dnf{})
		} else if commandExists("pacman") {
			managers = append(managers, &Pacman{})
			managers = append(managers, &AUR{})
		} else if commandExists("zypper") {
			managers = append(managers, &Zypper{})
		} else if commandExists("apk") {
			managers = append(managers, &Apk{})
		}
	}

	// Universal fallbacks — always added at the end
	managers = append(managers, &Snap{})
	managers = append(managers, &Flatpak{})

	return managers
}

func buildMacCascade() []Manager {
	return []Manager{
		&Brew{}, // Homebrew is the universal macOS manager
		         // InstallSelf() will install it if missing
	}
}

func buildWindowsCascade() []Manager {
	return []Manager{
		&Winget{}, // Ships with modern Windows — try first
		&Choco{},  // Chocolatey — largest Windows package repo
		&Scoop{},  // Scoop — great for developer tools
	}
}

// ── Package name mapping ──────────────────────────────────────────────────────

// PackageNames maps a logical package name to the correct name on each OS.
// The same tool often has different package names across distros.
// e.g. "python3" on Ubuntu is "python" on some others.
//
// Key: logical name (what Cloneable asks for internally)
// Value: map of OS/manager → actual package name
var PackageNames = map[string]map[string]string{
	"python3": {
		"apt":    "python3",
		"dnf":    "python3",
		"pacman": "python",
		"brew":   "python",
		"winget": "Python.Python.3",
		"choco":  "python3",
		"scoop":  "python",
	},
	"nodejs": {
		"apt":    "nodejs",
		"dnf":    "nodejs",
		"pacman": "nodejs",
		"brew":   "node",
		"winget": "OpenJS.NodeJS",
		"choco":  "nodejs",
		"scoop":  "nodejs",
	},
	"npm": {
		"apt":    "npm",
		"dnf":    "npm",
		"pacman": "npm",
		"brew":   "node", // npm ships with node on brew
		"winget": "OpenJS.NodeJS", // npm ships with node on winget
		"choco":  "nodejs",
		"scoop":  "nodejs",
	},
	"java": {
		"apt":    "default-jdk",
		"dnf":    "java-17-openjdk",
		"pacman": "jdk-openjdk",
		"brew":   "openjdk",
		"winget": "Microsoft.OpenJDK.17",
		"choco":  "openjdk",
		"scoop":  "openjdk17",
	},
	"cmake": {
		"apt":    "cmake",
		"dnf":    "cmake",
		"pacman": "cmake",
		"brew":   "cmake",
		"winget": "Kitware.CMake",
		"choco":  "cmake",
		"scoop":  "cmake",
	},
	"pkg-config": {
		"apt":    "pkg-config",
		"dnf":    "pkgconf-pkg-config",
		"pacman": "pkgconf",
		"brew":   "pkg-config",
		"winget": "", // not directly available
		"choco":  "pkgconfiglite",
		"scoop":  "pkg-config",
	},
	"gcc": {
		"apt":    "gcc",
		"dnf":    "gcc",
		"pacman": "gcc",
		"brew":   "gcc",
		"winget": "GnuWin32.Make", // closest on Windows
		"choco":  "mingw",
		"scoop":  "gcc",
	},
	"make": {
		"apt":    "build-essential",
		"dnf":    "make",
		"pacman": "base-devel",
		"brew":   "make",
		"winget": "GnuWin32.Make",
		"choco":  "make",
		"scoop":  "make",
	},
	"git": {
		"apt":    "git",
		"dnf":    "git",
		"pacman": "git",
		"brew":   "git",
		"winget": "Git.Git",
		"choco":  "git",
		"scoop":  "git",
	},
	"curl": {
		"apt":    "curl",
		"dnf":    "curl",
		"pacman": "curl",
		"brew":   "curl",
		"winget": "cURL.cURL",
		"choco":  "curl",
		"scoop":  "curl",
	},
	"flutter": {
		"apt":    "flutter",
		"dnf":    "flutter",
		"pacman": "flutter",
		"brew":   "flutter",
		"winget": "Google.FlutterSDK",
		"choco":  "flutter",
		"scoop":  "flutter",
	},
	"dart": {
		"apt":    "dart",
		"dnf":    "dart",
		"pacman": "dart",
		"brew":   "dart",
		"winget": "Dart.Dart",
		"choco":  "dart-sdk",
		"scoop":  "dart-sdk",
	},
	"dotnet-sdk": {
		"apt":    "dotnet-sdk-8.0",
		"dnf":    "dotnet-sdk-8.0",
		"pacman": "dotnet-sdk",
		"brew":   "dotnet",
		"winget": "Microsoft.DotNet.SDK.8",
		"choco":  "dotnet-sdk",
		"scoop":  "dotnet-sdk",
	},
	"meson": {
		"apt":    "meson",
		"dnf":    "meson",
		"pacman": "meson",
		"brew":   "meson",
		"winget": "",
		"choco":  "meson",
		"scoop":  "meson",
	},
	"ninja-build": {
		"apt":    "ninja-build",
		"dnf":    "ninja-build",
		"pacman": "ninja",
		"brew":   "ninja",
		"winget": "Ninja-build.Ninja",
		"choco":  "ninja",
		"scoop":  "ninja",
	},
}

// ResolvePackageName returns the correct package name for the current
// package manager. Falls back to the logical name if no mapping exists.
func ResolvePackageName(logicalName string, managerName string) string {
	names, ok := PackageNames[logicalName]
	if !ok {
		return logicalName // No mapping — use as-is
	}
	resolved, ok := names[managerName]
	if !ok || resolved == "" {
		return logicalName // No mapping for this manager
	}
	return resolved
}

// CurrentOS returns the OS string for package name resolution.
func CurrentOS() string {
	return runtime.GOOS
}
