// Package pkgmanager provides a unified abstraction over every system package
// manager Cloneable supports. All install logic in Phase II goes through here —
// no other package ever calls a package manager directly.
package pkgmanager

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Manager is the interface every package manager must implement.
// A single call to Install("cmake") will just work on any OS.
type Manager interface {
	// Name returns the human-readable name, e.g. "apt", "pacman", "winget".
	Name() string

	// IsAvailable returns true if this package manager is installed on the system.
	IsAvailable() bool

	// IsInstalled returns true if the given package is already installed.
	// This avoids unnecessary reinstalls.
	IsInstalled(pkg string) bool

	// Install installs a package. Output is written to logWriter.
	// Returns an error if the install fails.
	Install(pkg string, logWriter LogWriter) error

	// InstallSelf installs this package manager itself.
	// e.g. installs Homebrew if brew is not found on macOS.
	// Returns ErrCannotSelfInstall if self-installation is not supported.
	InstallSelf(logWriter LogWriter) error

	// UpdateIndex refreshes the package index (e.g. apt update).
	// Some package managers don't need this — they can return nil immediately.
	UpdateIndex(logWriter LogWriter) error
}

// LogWriter is a function that receives log lines from package manager output.
// All verbose output goes here (written to install.logs), never to the UI.
type LogWriter func(line string)

// ErrCannotSelfInstall is returned by InstallSelf for managers that
// cannot install themselves (e.g. apt, pacman — these are system-provided).
var ErrCannotSelfInstall = fmt.Errorf("this package manager cannot install itself")

// ErrPackageNotFound is returned when a package doesn't exist in any
// package manager's repository.
var ErrPackageNotFound = fmt.Errorf("package not found in any repository")

// ── shared helpers used by all implementations ────────────────────────────────

// run executes a command, streaming its combined output to logWriter.
// Returns an error if the command exits non-zero.
func run(logWriter LogWriter, name string, args ...string) error {
	cmd := exec.Command(name, args...)

	// Capture stdout + stderr together
	output, err := cmd.CombinedOutput()
	if logWriter != nil && len(output) > 0 {
		for _, line := range strings.Split(string(output), "\n") {
			if line != "" {
				logWriter("[" + name + "] " + line)
			}
		}
	}

	if err != nil {
		return fmt.Errorf("%s %s failed: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

// commandExists returns true if the binary is in PATH.
func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// packageInstalled checks if a package binary is available after install.
// This is a lightweight check — each manager overrides IsInstalled with
// a more accurate query where possible.
func packageInstalled(binary string) bool {
	return commandExists(binary)
}

// needsSudo returns true if the current process is not running as root.
// On Windows, sudo is not used — package managers handle elevation themselves.
func needsSudo() bool {
	if runtime.GOOS == "windows" {
		return false
	}
	return os.Getuid() != 0
}

// AuthenticateSudo runs 'sudo -v' to prompt the user for their password upfront.
// This prevents multiple sudo prompts or prompts hidden behind a spinner.
func AuthenticateSudo() error {
	if !needsSudo() {
		return nil
	}
	cmd := exec.Command("sudo", "-v")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// sudoRun executes a command with sudo prepended if not running as root.
// Used by system package managers that require root privileges (apt, pacman, etc.).
// sudo reads the password from /dev/tty, so it works even during spinner animations.
func sudoRun(logWriter LogWriter, name string, args ...string) error {
	if needsSudo() {
		args = append([]string{name}, args...)
		name = "sudo"
	}
	return run(logWriter, name, args...)
}
