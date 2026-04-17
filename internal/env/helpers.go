package env

import (
	"fmt"
	"os/exec"
	"strings"
)

// runCmd executes a command, streaming combined output to logWriter.
// This is the package-level run helper used by all env files.
func runCmd(log LogWriter, name string, args ...string) error {
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
