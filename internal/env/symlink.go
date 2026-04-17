package env

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// findBinary searches for the compiled/installed binary in well-known locations.
// It checks in this order:
//  1. The isolated env bin dir (e.g. .venv/bin/, zig-out/bin/)
//  2. The repo root
//  3. Common build output dirs (build/, dist/, target/release/, bin/)
func (e *Environment) findBinary(name string) (string, error) {
	candidates := e.binarySearchPaths(name)

	for _, path := range candidates {
		if isExecutable(path) {
			return path, nil
		}
		// Windows: also try with .exe suffix
		if isExecutable(path + ".exe") {
			return path + ".exe", nil
		}
	}

	return "", fmt.Errorf(
		"could not find binary %q — looked in: %s",
		name,
		strings.Join(candidates, ", "),
	)
}

// binarySearchPaths returns all paths to check for the binary, in priority order.
func (e *Environment) binarySearchPaths(name string) []string {
	var paths []string

	// 1. Language-specific output directories
	switch e.Tech.String() {
	case "Python":
		binDir := filepath.Join(e.RepoPath, ".venv", "bin")
		if isWindows() {
			binDir = filepath.Join(e.RepoPath, ".venv", "Scripts")
		}
		paths = append(paths, filepath.Join(binDir, name))

	case "Node.js":
		paths = append(paths,
			filepath.Join(e.RepoPath, "node_modules", ".bin", name),
			filepath.Join(e.RepoPath, "bin", name),
		)

	case "Zig":
		paths = append(paths,
			filepath.Join(e.RepoPath, "zig-out", "bin", name),
		)

	case "Rust":
		paths = append(paths,
			filepath.Join(e.RepoPath, "target", "release", name),
			filepath.Join(e.RepoPath, "target", "debug", name),
		)

	case "Go":
		gopath := os.Getenv("GOPATH")
		if gopath == "" {
			home, _ := os.UserHomeDir()
			gopath = filepath.Join(home, "go")
		}
		paths = append(paths, filepath.Join(gopath, "bin", name))

	case "C/C++", "C":
		paths = append(paths,
			filepath.Join(e.RepoPath, "build", name),
			filepath.Join(e.RepoPath, "build", "bin", name),
			// cmake --install puts things in installPrefix/bin
			filepath.Join(e.installPrefix(), "bin", name),
		)

	case "Java":
		paths = append(paths,
			filepath.Join(e.RepoPath, "build", "libs", name+".jar"),
		)
	}

	// 2. Generic search paths — checked for all techs as fallback
	generic := []string{
		filepath.Join(e.RepoPath, name),
		filepath.Join(e.RepoPath, "bin", name),
		filepath.Join(e.RepoPath, "dist", name),
		filepath.Join(e.RepoPath, "dist", "bin", name),
		filepath.Join(e.RepoPath, "out", name),
		filepath.Join(e.RepoPath, "out", "bin", name),
		filepath.Join(e.RepoPath, "release", name),
		filepath.Join(e.EnvDir, name),
	}

	paths = append(paths, generic...)
	return paths
}

// isExecutable returns true if the path exists and is executable.
func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.IsDir() {
		return false
	}
	if isWindows() {
		// On Windows, any existing file with .exe/.cmd/.bat is executable
		return true
	}
	// Unix: check executable bit
	return info.Mode()&0111 != 0
}

// createWindowsWrapper creates a .cmd file that invokes the source binary.
// This gives Windows users a globally-accessible command without needing
// Unix symlinks (which require admin rights on older Windows versions).
func createWindowsWrapper(source, target string, log LogWriter) error {
	content := fmt.Sprintf("@echo off\n\"%s\" %%*\n", source)

	if err := os.WriteFile(target, []byte(content), 0755); err != nil {
		return fmt.Errorf("could not create Windows wrapper %s: %w", target, err)
	}

	if log != nil {
		log(fmt.Sprintf("[env] created Windows wrapper: %s → %s", source, target))
	}
	return nil
}

// AddToPATHWindows adds the given directory to the user's PATH on Windows
// via the registry. This persists after the terminal closes.
func AddToPATHWindows(dir string, log LogWriter) error {
	// Read current user PATH from registry
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		fmt.Sprintf(`[Environment]::SetEnvironmentVariable("PATH", $env:PATH + ";%s", "User")`, dir),
	)
	out, err := cmd.CombinedOutput()
	if log != nil && len(out) > 0 {
		log("[env] " + string(out))
	}
	if err != nil {
		return fmt.Errorf("could not update Windows PATH: %w", err)
	}
	return nil
}

// AddToPATHUnix appends a PATH export line to the user's shell config file.
// It detects the shell and writes to the right file.
// Only adds if not already present.
func AddToPATHUnix(dir string, log LogWriter) error {
	shell := detectShell()
	configFile := shellConfigFile(shell)

	if configFile == "" {
		return nil // Unknown shell — don't touch anything
	}

	// Read existing content
	existing, _ := os.ReadFile(configFile)
	exportLine := fmt.Sprintf(`export PATH="%s:$PATH"`, dir)

	// Already present?
	if strings.Contains(string(existing), dir) {
		return nil
	}

	// Append to config file
	f, err := os.OpenFile(configFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = fmt.Fprintf(f, "\n# Added by Cloneable\n%s\n", exportLine)
	if log != nil {
		log(fmt.Sprintf("[env] added %s to %s", dir, configFile))
	}
	return err
}

// isInPath returns true if the given directory is already in the PATH string.
func isInPath(dir, pathEnv string) bool {
	sep := string(os.PathListSeparator)
	for _, p := range strings.Split(pathEnv, sep) {
		if filepath.Clean(p) == filepath.Clean(dir) {
			return true
		}
	}
	return false
}

// detectShell returns the name of the current shell (bash, zsh, fish, etc.).
func detectShell() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		return "bash"
	}
	base := filepath.Base(shell)
	return base
}

// shellConfigFile returns the config file path for the given shell.
func shellConfigFile(shell string) string {
	home, _ := os.UserHomeDir()
	switch shell {
	case "zsh":
		return filepath.Join(home, ".zshrc")
	case "fish":
		return filepath.Join(home, ".config", "fish", "config.fish")
	case "bash":
		// Prefer .bashrc, but .bash_profile on macOS
		bashrc := filepath.Join(home, ".bashrc")
		if _, err := os.Stat(bashrc); err == nil {
			return bashrc
		}
		return filepath.Join(home, ".bash_profile")
	default:
		return filepath.Join(home, ".profile")
	}
}

func isWindows() bool {
	return os.Getenv("OS") == "Windows_NT"
}
