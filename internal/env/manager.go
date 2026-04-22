// Package env handles isolated environment creation for each technology.
// The core philosophy:
//   - Never pollute the system Python/Node/etc with project dependencies
//   - Everything installs into the repo's own environment folder
//   - A single symlink in ~/.local/bin makes it globally accessible forever
//   - The receipt system tracks every symlink so `cloneable remove` cleans up
package env

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/manansati/cloneable/internal/detection"
	"github.com/manansati/cloneable/internal/ui"
)

// Environment represents an isolated environment for a single repository.
// It knows where the repo lives, what tech it uses, and where to put symlinks.
type Environment struct {
	// RepoPath is the absolute path to the cloned repository.
	RepoPath string

	// RepoName is the short name of the repo, e.g. "ghostty".
	RepoName string

	// Tech is the primary technology detected in the repo.
	Tech detection.TechType

	// BinDir is where global symlinks are placed, e.g. ~/.local/bin
	BinDir string

	// EnvDir is the isolated environment folder inside the repo.
	// e.g. RepoPath/.venv for Python, RepoPath/node_modules for Node.
	// Empty for languages that manage their own global install (Go, Rust).
	EnvDir string

	// OSType is the current operating system.
	OSType detection.OSType

	// Symlinks tracks every symlink created for this environment.
	// Stored in the receipt so they can be removed later.
	Symlinks []Symlink
}

// Symlink records a single symlink created by Cloneable.
type Symlink struct {
	Source string // The binary inside the env (e.g. .venv/bin/myapp)
	Target string // The global symlink (e.g. ~/.local/bin/myapp)
}

// LogWriter is a function that receives log lines.
// All verbose output goes here (written to install.logs), never to the UI.
type LogWriter func(line string)

// NewEnvironment creates an Environment for the given repo and tech.
func NewEnvironment(repoPath, repoName string, tech detection.TechType, osInfo *detection.OSInfo) *Environment {
	return &Environment{
		RepoPath: repoPath,
		RepoName: repoName,
		Tech:     tech,
		BinDir:   osInfo.BinDir,
		OSType:   osInfo.Type,
	}
}

// Setup creates the isolated environment for this repo's technology.
// This is called in Phase II before dependencies are installed.
// It does not install any packages — it just prepares the environment.
func (e *Environment) Setup(log LogWriter) error {
	switch e.Tech {
	case detection.TechPython:
		return e.setupPython(log)
	case detection.TechNode:
		return e.setupNode(log)
	case detection.TechGo:
		return e.setupGo(log)
	case detection.TechRust:
		return e.setupRust(log)
	case detection.TechJava:
		return e.setupJava(log)
	case detection.TechCpp, detection.TechC:
		return e.setupCpp(log)
	case detection.TechZig:
		return e.setupZig(log)
	case detection.TechFlutter, detection.TechDart:
		return e.setupFlutter(log)
	case detection.TechRuby:
		return e.setupRuby(log)
	case detection.TechDotnet:
		return e.setupDotnet(log)
	case detection.TechHaskell:
		return e.setupHaskell(log)
	default:
		// Unknown tech — no special environment needed
		return nil
	}
}

// MakeGlobal creates symlinks so the installed binary is accessible globally.
// Called after Phase III (launch/install) completes successfully.
// globalBinaryName is the name of the binary to symlink (e.g. "ghostty").
func (e *Environment) MakeGlobal(globalBinaryName string, log LogWriter) error {
	// Ensure the bin directory exists
	if err := os.MkdirAll(e.BinDir, 0755); err != nil {
		return fmt.Errorf("could not create bin directory %s: %w", e.BinDir, err)
	}

	// Find the binary source path
	source, err := e.FindBinary(globalBinaryName)
	if err != nil {
		return err
	}

	target := filepath.Join(e.BinDir, globalBinaryName)
	if runtime.GOOS == "windows" {
		target += ".cmd" // Windows needs a .cmd wrapper
	}

	// Remove existing symlink/file at target if present
	_ = os.Remove(target)

	if runtime.GOOS == "windows" {
		// Windows: create a .cmd batch file wrapper instead of a symlink
		return createWindowsWrapper(source, target, log)
	}

	// Unix: create a proper symlink
	if err := os.Symlink(source, target); err != nil {
		return fmt.Errorf("could not create symlink %s → %s: %w", source, target, err)
	}

	e.Symlinks = append(e.Symlinks, Symlink{Source: source, Target: target})

	if log != nil {
		log(fmt.Sprintf("[env] symlinked %s → %s", source, target))
	}

	return nil
}

// EnsureBinDirInPath writes PATH exports to every shell config found on this system.
// It covers bash, zsh, fish, and POSIX .profile — all at once.
func (e *Environment) EnsureBinDirInPath() {
	pathEnv := os.Getenv("PATH")
	if isInPath(e.BinDir, pathEnv) {
		return
	}

	if runtime.GOOS == "windows" {
		_ = AddToPATHWindows(e.BinDir, nil)
	} else {
		_ = AddToPATHUnix(e.BinDir, nil)
	}

	// Update current process PATH so we can launch it right away
	newPath := fmt.Sprintf("%s%c%s", e.BinDir, os.PathListSeparator, pathEnv)
	os.Setenv("PATH", newPath)

	fmt.Printf("\n  %s  Added %s to your PATH.\n", ui.Tick(), ui.Muted(e.BinDir))
	if runtime.GOOS != "windows" {
		fmt.Printf("  %s  %s\n\n", ui.Warn("!"), ui.SaffronBold("Restart your terminal to apply to future sessions."))
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func fileContains(path, str string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), str)
}

func appendToFile(path, content string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(content)
	return err
}

// RemoveAll removes the isolated environment and all symlinks.
// Called by `cloneable remove`.
func (e *Environment) RemoveAll(log LogWriter) error {
	// Remove symlinks first
	for _, sym := range e.Symlinks {
		if err := os.Remove(sym.Target); err != nil && !os.IsNotExist(err) {
			if log != nil {
				log(fmt.Sprintf("[env] warning: could not remove symlink %s: %v", sym.Target, err))
			}
		}
	}

	// Remove the environment directory if it exists
	if e.EnvDir != "" {
		if err := os.RemoveAll(e.EnvDir); err != nil {
			return fmt.Errorf("could not remove environment directory %s: %w", e.EnvDir, err)
		}
	}

	return nil
}
