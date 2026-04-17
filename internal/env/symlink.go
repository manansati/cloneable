package env

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// FindBinary searches for the compiled/installed binary in well-known locations.
func (e *Environment) FindBinary(name string) (string, error) {
	candidates := e.binarySearchPaths(name)

	for _, path := range candidates {
		if isExecutable(path) {
			return path, nil
		}
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

func (e *Environment) binarySearchPaths(name string) []string {
	var paths []string

	switch string(e.Tech) {
	case "Python":
		binDir := filepath.Join(e.RepoPath, ".venv", "bin")
		if runtime.GOOS == "windows" {
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
			filepath.Join(e.installPrefix(), "bin", name),
		)

	case "Java":
		paths = append(paths,
			filepath.Join(e.RepoPath, "build", "libs", name+".jar"),
		)
	}

	// Generic fallbacks for all techs
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

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode()&0111 != 0
}

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

// AddToPATHWindows adds a directory to the user's PATH permanently via registry.
func AddToPATHWindows(dir string, log LogWriter) error {
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

// AddToPATHUnix writes the PATH export to shell config files for ALL detected shells.
// It writes to every rc file that exists — not just the active shell.
func AddToPATHUnix(dir string, log LogWriter) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	type shellCfg struct {
		path    string
		line    string
		mkdirOf string // parent dir to create if needed
	}

	configs := []shellCfg{
		{
			path: filepath.Join(home, ".bashrc"),
			line: fmt.Sprintf("\n# Cloneable\nexport PATH=\"%s:$PATH\"\n", dir),
		},
		{
			path: filepath.Join(home, ".bash_profile"),
			line: fmt.Sprintf("\n# Cloneable\nexport PATH=\"%s:$PATH\"\n", dir),
		},
		{
			path: filepath.Join(home, ".zshrc"),
			line: fmt.Sprintf("\n# Cloneable\nexport PATH=\"%s:$PATH\"\n", dir),
		},
		{
			path:    filepath.Join(home, ".config", "fish", "config.fish"),
			line:    fmt.Sprintf("\n# Cloneable\nfish_add_path %s\n", dir),
			mkdirOf: filepath.Join(home, ".config", "fish"),
		},
		{
			path: filepath.Join(home, ".profile"),
			line: fmt.Sprintf("\n# Cloneable\nexport PATH=\"%s:$PATH\"\n", dir),
		},
	}

	written := 0
	for _, cfg := range configs {
		// Only write to files that already exist (don't create rc files for shells not installed)
		if _, err := os.Stat(cfg.path); os.IsNotExist(err) {
			continue
		}

		// Skip if already contains the path
		data, _ := os.ReadFile(cfg.path)
		if strings.Contains(string(data), dir) {
			continue
		}

		// Write
		if cfg.mkdirOf != "" {
			_ = os.MkdirAll(cfg.mkdirOf, 0755)
		}
		f, err := os.OpenFile(cfg.path, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			continue
		}
		_, _ = f.WriteString(cfg.line)
		f.Close()
		written++

		if log != nil {
			log(fmt.Sprintf("[env] updated %s", cfg.path))
		}
	}

	_ = written
	return nil
}

func isInPath(dir, pathEnv string) bool {
	sep := string(os.PathListSeparator)
	for _, p := range strings.Split(pathEnv, sep) {
		if filepath.Clean(p) == filepath.Clean(dir) {
			return true
		}
	}
	return false
}

func detectShell() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		return "bash"
	}
	return filepath.Base(shell)
}

func shellConfigFile(shell string) string {
	home, _ := os.UserHomeDir()
	switch shell {
	case "zsh":
		return filepath.Join(home, ".zshrc")
	case "fish":
		return filepath.Join(home, ".config", "fish", "config.fish")
	case "bash":
		bashrc := filepath.Join(home, ".bashrc")
		if _, err := os.Stat(bashrc); err == nil {
			return bashrc
		}
		return filepath.Join(home, ".bash_profile")
	default:
		return filepath.Join(home, ".profile")
	}
}
