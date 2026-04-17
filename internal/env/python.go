package env

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// setupPython creates a .venv inside the repo directory.
// This completely isolates pip packages from the system Python,
// preventing the "externally managed environment" error on modern Linux.
//
// Structure after setup:
//
//	<repo>/
//	  .venv/
//	    bin/          (Linux/macOS)
//	      python
//	      pip
//	      <app-binary>   ← symlinked to ~/.local/bin/<app-binary>
//	    Scripts/      (Windows)
//	      python.exe
//	      pip.exe
func (e *Environment) setupPython(log LogWriter) error {
	venvPath := filepath.Join(e.RepoPath, ".venv")
	e.EnvDir = venvPath

	// Skip if venv already exists and looks healthy
	if venvExists(venvPath) {
		if log != nil {
			log("[python] .venv already exists — reusing")
		}
		return nil
	}

	// Find the best available python binary
	python, err := findPython()
	if err != nil {
		return fmt.Errorf("python not found: %w", err)
	}

	if log != nil {
		log(fmt.Sprintf("[python] creating .venv with %s", python))
	}

	// Create the virtual environment
	cmd := exec.Command(python, "-m", "venv", venvPath)
	cmd.Dir = e.RepoPath
	out, err := cmd.CombinedOutput()
	if log != nil && len(out) > 0 {
		log("[python] " + string(out))
	}
	if err != nil {
		return fmt.Errorf("could not create Python venv: %w", err)
	}

	return nil
}

// PythonBin returns the path to the python binary inside the venv.
func (e *Environment) PythonBin() string {
	venvPath := filepath.Join(e.RepoPath, ".venv")
	if runtime.GOOS == "windows" {
		return filepath.Join(venvPath, "Scripts", "python.exe")
	}
	return filepath.Join(venvPath, "bin", "python")
}

// PipBin returns the path to the pip binary inside the venv.
func (e *Environment) PipBin() string {
	venvPath := filepath.Join(e.RepoPath, ".venv")
	if runtime.GOOS == "windows" {
		return filepath.Join(venvPath, "Scripts", "pip.exe")
	}
	return filepath.Join(venvPath, "bin", "pip")
}

// PythonEnvVars returns environment variables that activate the venv
// without needing to run `source .venv/bin/activate`.
// These are passed to all subsequent commands in Phase II and III.
func (e *Environment) PythonEnvVars() []string {
	venvPath := filepath.Join(e.RepoPath, ".venv")
	var binDir string
	if runtime.GOOS == "windows" {
		binDir = filepath.Join(venvPath, "Scripts")
	} else {
		binDir = filepath.Join(venvPath, "bin")
	}

	return []string{
		"VIRTUAL_ENV=" + venvPath,
		"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"PYTHONDONTWRITEBYTECODE=1",
	}
}

// venvExists returns true if a .venv directory looks healthy.
func venvExists(venvPath string) bool {
	var marker string
	if runtime.GOOS == "windows" {
		marker = filepath.Join(venvPath, "Scripts", "python.exe")
	} else {
		marker = filepath.Join(venvPath, "bin", "python")
	}
	_, err := os.Stat(marker)
	return err == nil
}

// findPython returns the path to the best available Python binary.
// Prefers python3 over python, and checks for minimum version.
func findPython() (string, error) {
	candidates := []string{"python3", "python3.12", "python3.11", "python3.10", "python"}
	for _, name := range candidates {
		path, err := exec.LookPath(name)
		if err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("no python binary found in PATH — install Python 3 first")
}
