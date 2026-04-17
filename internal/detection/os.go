// Package detection handles all system and repository detection logic.
// It is the foundation every other package depends on — no guessing,
// everything is read directly from the system.
package detection

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// OSType represents the detected operating system.
type OSType string

const (
	OSLinux   OSType = "Linux"
	OSMacOS   OSType = "macOS"
	OSWindows OSType = "Windows"
	OSUnknown OSType = "Unknown"
)

// LinuxDistro represents a detected Linux distribution.
type LinuxDistro string

const (
	DistroArch     LinuxDistro = "Arch"
	DistroManjaro  LinuxDistro = "Manjaro"
	DistroEndeavor LinuxDistro = "EndeavourOS"
	DistroUbuntu   LinuxDistro = "Ubuntu"
	DistroDebian   LinuxDistro = "Debian"
	DistroMint     LinuxDistro = "Mint"
	DistroFedora   LinuxDistro = "Fedora"
	DistroRHEL     LinuxDistro = "RHEL"
	DistroCentOS   LinuxDistro = "CentOS"
	DistroOpenSUSE LinuxDistro = "openSUSE"
	DistroAlpine   LinuxDistro = "Alpine"
	DistroVoid     LinuxDistro = "Void"
	DistroGentoo   LinuxDistro = "Gentoo"
	DistroNixOS    LinuxDistro = "NixOS"
	DistroUnknown  LinuxDistro = "Unknown"
)

// OSInfo holds all detected information about the host system.
// This struct is passed around to every part of Cloneable that
// needs to know about the environment it's running in.
type OSInfo struct {
	// Type is the broad OS family.
	Type OSType

	// Distro is the specific Linux distribution (empty on macOS/Windows).
	Distro LinuxDistro

	// DistroVersion is the version string, e.g. "22.04", "39", "rolling".
	DistroVersion string

	// Arch is the CPU architecture, e.g. "amd64", "arm64".
	Arch string

	// IsElevated is true if Cloneable is running as root/Administrator.
	IsElevated bool

	// HomeDir is the current user's home directory.
	HomeDir string

	// CloneableDir is ~/.cloneable/ — Cloneable's data directory.
	CloneableDir string

	// BinDir is where Cloneable places global binaries.
	// Linux/macOS: ~/.local/bin
	// Windows:     %USERPROFILE%\.cloneable\bin
	BinDir string

	// ReceiptsDir is where install receipts are stored.
	// Always: ~/.cloneable/receipts/
	ReceiptsDir string
}

// DetectOS returns full information about the host operating system.
// This function is safe to call multiple times — it reads the system
// fresh each time and never caches.
func DetectOS() (*OSInfo, error) {
	info := &OSInfo{
		Arch: runtime.GOARCH,
	}

	// -- Detect OS type --
	switch runtime.GOOS {
	case "linux":
		info.Type = OSLinux
		info.Distro, info.DistroVersion = detectLinuxDistro()
	case "darwin":
		info.Type = OSMacOS
	case "windows":
		info.Type = OSWindows
	default:
		info.Type = OSUnknown
	}

	// -- Home directory --
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	info.HomeDir = home

	// -- Cloneable directories (platform-aware) --
	info.CloneableDir = filepath.Join(home, ".cloneable")
	info.ReceiptsDir = filepath.Join(home, ".cloneable", "receipts")

	if info.Type == OSWindows {
		// Windows: use our own bin dir since ~/.local/bin doesn't exist
		info.BinDir = filepath.Join(home, ".cloneable", "bin")
	} else {
		// Linux / macOS: XDG standard location
		info.BinDir = filepath.Join(home, ".local", "bin")
	}

	// -- Elevation check --
	info.IsElevated = checkElevation()

	return info, nil
}

// detectLinuxDistro reads /etc/os-release to identify the Linux distribution
// and its version. Returns (DistroUnknown, "") if the file cannot be read.
//
// /etc/os-release is the modern standard across all major distros.
// We also fall back to /etc/issue for very old systems.
func detectLinuxDistro() (LinuxDistro, string) {
	// Primary: /etc/os-release (systemd standard, supported by all modern distros)
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		// Fallback: /etc/issue (older systems)
		return detectFromIssue()
	}

	fields := parseOSRelease(string(data))

	name := strings.ToLower(fields["ID"])
	version := fields["VERSION_ID"]
	if version == "" {
		version = fields["BUILD_ID"] // Arch uses BUILD_ID
	}

	switch {
	case name == "arch":
		return DistroArch, version
	case name == "manjaro":
		return DistroManjaro, version
	case name == "endeavouros":
		return DistroEndeavor, version
	case name == "ubuntu":
		return DistroUbuntu, version
	case name == "debian":
		return DistroDebian, version
	case strings.Contains(name, "linuxmint") || name == "mint":
		return DistroMint, version
	case name == "fedora":
		return DistroFedora, version
	case name == "rhel":
		return DistroRHEL, version
	case name == "centos":
		return DistroCentOS, version
	case strings.Contains(name, "opensuse"):
		return DistroOpenSUSE, version
	case name == "alpine":
		return DistroAlpine, version
	case name == "void":
		return DistroVoid, version
	case name == "gentoo":
		return DistroGentoo, version
	case name == "nixos":
		return DistroNixOS, version
	default:
		// Unknown distro — use the PRETTY_NAME so the UI can still show something
		pretty := fields["PRETTY_NAME"]
		if pretty != "" {
			return LinuxDistro(pretty), version
		}
		return DistroUnknown, ""
	}
}

// parseOSRelease parses the key=value pairs in /etc/os-release into a map.
// Handles both quoted and unquoted values.
func parseOSRelease(content string) map[string]string {
	fields := make(map[string]string)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		// Strip surrounding quotes
		val = strings.Trim(val, `"'`)
		fields[key] = val
	}
	return fields
}

// detectFromIssue is a fallback for very old Linux systems without /etc/os-release.
func detectFromIssue() (LinuxDistro, string) {
	data, err := os.ReadFile("/etc/issue")
	if err != nil {
		return DistroUnknown, ""
	}
	line := strings.ToLower(strings.Split(string(data), "\n")[0])
	switch {
	case strings.Contains(line, "ubuntu"):
		return DistroUbuntu, ""
	case strings.Contains(line, "debian"):
		return DistroDebian, ""
	case strings.Contains(line, "fedora"):
		return DistroFedora, ""
	default:
		return DistroUnknown, ""
	}
}

// EnsureDirectories creates all Cloneable directories if they don't exist.
// Safe to call on every startup — does nothing if they already exist.
func EnsureDirectories(info *OSInfo) error {
	dirs := []string{
		info.CloneableDir,
		info.ReceiptsDir,
		info.BinDir,
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
}

// DisplayName returns a clean human-readable name for the OS,
// used in the UI header bar.
func (info *OSInfo) DisplayName() string {
	switch info.Type {
	case OSLinux:
		if info.Distro != DistroUnknown && info.Distro != "" {
			return string(info.Distro)
		}
		return "Linux"
	case OSMacOS:
		return "macOS"
	case OSWindows:
		return "Windows"
	default:
		return "Unknown"
	}
}
