package env

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ResolveExecutable finds the absolute path of an executable to avoid PATH cache issues.
func ResolveExecutable(name string) string {
	if filepath.IsAbs(name) {
		return name
	}
	if absPath, err := exec.LookPath(name); err == nil {
		return absPath
	}
	
	home, _ := os.UserHomeDir()
	commonDirs := []string{
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, "go", "bin"),
		filepath.Join(home, ".cargo", "bin"),
		"/opt/flutter/bin",
		"/usr/local/bin",
		"/usr/bin",
		"/bin",
	}
	
	for _, dir := range commonDirs {
		candidate := filepath.Join(dir, name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return name
}

// runCmd executes a command, streaming combined output to logWriter.
// This is the package-level run helper used by all env files.
func runCmd(log LogWriter, name string, args ...string) error {
	name = ResolveExecutable(name)
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()

	if log != nil && len(out) > 0 {
		for _, line := range strings.Split(string(out), "\n") {
			if strings.TrimSpace(line) != "" {
				log(fmt.Sprintf("[%s] %s", name, line))
			}
		}
	}

	if err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

// binaryExists returns true if the named binary is in PATH.
func binaryExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
