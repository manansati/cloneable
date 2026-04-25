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

	// Fallback: If we couldn't find it by exact name, scan the primary output
	// directory for this tech to see if there's ANY executable we can use.
	fallbackPath := e.findAnyExecutableInOutputDir()
	if fallbackPath != "" {
		return fallbackPath, nil
	}

	return "", fmt.Errorf(
		"could not find binary %q — looked in: %s",
		name,
		strings.Join(candidates, ", "),
	)
}

func (e *Environment) binarySearchPaths(name string) []string {
	var paths []string

	// Name mapping for known projects where repo name != binary name
	names := []string{name}
	if strings.Contains(name, "-") {
		names = append(names, strings.ReplaceAll(name, "-", "_"))
	}
	if name == "neovim" {
		names = append(names, "nvim")
	}

	for _, n := range names {
		switch string(e.Tech) {
		case "Python":
			binDir := filepath.Join(e.RepoPath, ".venv", "bin")
			if runtime.GOOS == "windows" {
				binDir = filepath.Join(e.RepoPath, ".venv", "Scripts")
			}
			paths = append(paths, filepath.Join(binDir, n))

		case "Node.js":
			paths = append(paths,
				filepath.Join(e.RepoPath, "node_modules", ".bin", n),
				filepath.Join(e.RepoPath, "bin", n),
			)

		case "Zig":
			paths = append(paths,
				filepath.Join(e.RepoPath, "zig-out", "bin", n),
				filepath.Join(e.RepoPath, "zig-out", n), // some zig versions put it in root
			)

		case "Rust":
			paths = append(paths,
				filepath.Join(e.RepoPath, "target", "release", n),
				filepath.Join(e.RepoPath, "target", "debug", n),
			)

		case "Go":
			gopath := os.Getenv("GOPATH")
			if gopath == "" {
				home, _ := os.UserHomeDir()
				gopath = filepath.Join(home, "go")
			}
			paths = append(paths, filepath.Join(gopath, "bin", n))

		case "C/C++", "C":
			paths = append(paths,
				filepath.Join(e.RepoPath, "build", n),
				filepath.Join(e.RepoPath, "build", "bin", n),
				filepath.Join(e.installPrefix(), "bin", n),
			)

		case "Java":
			paths = append(paths,
				filepath.Join(e.RepoPath, "build", "libs", n+".jar"),
			)
		}

		// Generic fallbacks for all techs
		generic := []string{
			filepath.Join(e.RepoPath, n),
			filepath.Join(e.RepoPath, "bin", n),
			filepath.Join(e.RepoPath, "dist", n),
			filepath.Join(e.RepoPath, "dist", "bin", n),
			filepath.Join(e.RepoPath, "out", n),
			filepath.Join(e.RepoPath, "out", "bin", n),
			filepath.Join(e.RepoPath, "release", n),
			filepath.Join(e.EnvDir, n),
		}
		paths = append(paths, generic...)

		// System paths (for global installs)
		if runtime.GOOS != "windows" {
			paths = append(paths,
				filepath.Join("/usr/local/bin", n),
				filepath.Join("/usr/bin", n),
				filepath.Join("/bin", n),
				filepath.Join("/opt/bin", n),
			)
		}
	}

	return paths
}

// findAnyExecutableInOutputDir checks the primary compilation output directory
// for the current technology. If it finds exactly one executable file, it returns it.
// If it finds multiple, it tries to pick the one that looks most like a primary binary
// (e.g. ignoring .so, .dll, or files with 'test' in the name).
func (e *Environment) findAnyExecutableInOutputDir() string {
	var outDir string
	switch string(e.Tech) {
	case "Rust":
		outDir = filepath.Join(e.RepoPath, "target", "release")
	case "Zig":
		outDir = filepath.Join(e.RepoPath, "zig-out", "bin")
		if !fileExists(outDir) {
			outDir = filepath.Join(e.RepoPath, "zig-out")
		}
	case "C/C++", "C":
		outDir = filepath.Join(e.RepoPath, "build")
	case "Go":
		// Go puts it in GOPATH/bin, which we already thoroughly check by name.
		// If it's not there, it's not anywhere.
		return ""
	default:
		return ""
	}

	entries, err := os.ReadDir(outDir)
	if err != nil {
		return ""
	}

	var executables []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(outDir, entry.Name())
		if isExecutable(path) {
			// Ignore common non-primary executables
			name := strings.ToLower(entry.Name())
			if strings.HasSuffix(name, ".so") || strings.HasSuffix(name, ".dll") || strings.HasSuffix(name, ".dylib") || strings.Contains(name, "test") {
				continue
			}
			executables = append(executables, path)
		}
	}

	// If we found exactly one viable executable, return it!
	if len(executables) == 1 {
		return executables[0]
	}

	// If we found multiple, try to find one that shares a prefix with the repo name
	repoNameLower := strings.ToLower(e.RepoName)
	if len(executables) > 1 {
		for _, exe := range executables {
			base := strings.ToLower(filepath.Base(exe))
			base = strings.TrimSuffix(base, ".exe")
			if strings.HasPrefix(repoNameLower, base) || strings.HasPrefix(base, repoNameLower) {
				return exe
			}
		}
	}

	return ""
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
			line:    fmt.Sprintf("\n# Cloneable\nif not contains %s $PATH\n    set -gx PATH %s $PATH\nend\n", dir, dir),
			mkdirOf: filepath.Join(home, ".config", "fish"),
		},
		{
			path: filepath.Join(home, ".profile"),
			line: fmt.Sprintf("\n# Cloneable\nexport PATH=\"%s:$PATH\"\n", dir),
		},
		{
			path: filepath.Join(home, ".kshrc"),
			line: fmt.Sprintf("\n# Cloneable\nexport PATH=\"%s:$PATH\"\n", dir),
		},
	}

	for _, cfg := range configs {
		// Only write to files that already exist or for which we have a reason to believe the shell is present
		if _, err := os.Stat(cfg.path); os.IsNotExist(err) && cfg.mkdirOf == "" {
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
		
		f, err := os.OpenFile(cfg.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			continue
		}
		_, _ = f.WriteString(cfg.line)
		f.Close()

		if log != nil {
			log(fmt.Sprintf("[env] updated %s", cfg.path))
		}
	}

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
