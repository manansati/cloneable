package detection

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// checkElevation returns true if the current process has admin/root privileges.
// This is called once during DetectOS() and stored in OSInfo.IsElevated.
func checkElevation() bool {
	switch runtime.GOOS {
	case "windows":
		return checkWindowsAdmin()
	default:
		// Linux and macOS: UID 0 = root
		return os.Getuid() == 0
	}
}

// checkWindowsAdmin checks for Administrator rights on Windows by attempting
// to open a privileged registry key. This is the most reliable method
// without importing external packages.
func checkWindowsAdmin() bool {
	// Try to open a key that requires admin rights.
	// If it succeeds, we have admin. If it fails with access denied, we don't.
	cmd := exec.Command("net", "session")
	err := cmd.Run()
	return err == nil
}

// ElevationMessage returns the appropriate message to show the user
// when Cloneable is not running with sufficient privileges.
func ElevationMessage(osType OSType) string {
	switch osType {
	case OSWindows:
		return windowsElevationMessage()
	case OSMacOS:
		return unixElevationMessage("macOS")
	default:
		return unixElevationMessage("Linux")
	}
}

func windowsElevationMessage() string {
	lines := []string{
		"",
		"  Cloneable needs Administrator rights to install dependencies.",
		"",
		"  How to fix:",
		"    1. Close this window",
		"    2. Search for your terminal (cmd / PowerShell / Windows Terminal)",
		"    3. Right-click → \"Run as Administrator\"",
		"    4. Run cloneable again",
		"",
	}
	return strings.Join(lines, "\n")
}

func unixElevationMessage(os string) string {
	lines := []string{
		"",
		"  Cloneable needs sudo to install dependencies.",
		"",
		"  How to fix:",
		"    Run:  sudo cloneable <your command>",
		"",
	}
	_ = os // reserved for future OS-specific messaging
	return strings.Join(lines, "\n")
}

// RequiresElevation returns true for commands that need admin/root rights.
// Some commands (--version, --help, --stats, search) never need elevation.
func RequiresElevation(command string) bool {
	safe := map[string]bool{
		"version": true,
		"help":    true,
		"stats":   true,
		"search":  true,
		"logs":    true,
		"update":  true,
	}
	return !safe[command]
}
