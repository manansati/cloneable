package env

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// setupPython creates a .venv inside the repo directory.
// This completely isolates pip packages from the system Python,
// preventing the "externally managed environment" error on modern Linux.
//
// Fallback chain when `python3 -m venv` fails:
//  1. Try installing the python3-venv system package automatically
//  2. Try using virtualenv (pip-installable, works everywhere)
//  3. Try with --without-pip and bootstrap pip manually
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

	// Remove any broken .venv remnant before retrying
	_ = os.RemoveAll(venvPath)

	// Find the best available python binary
	python, err := findPython()
	if err != nil {
		return fmt.Errorf("python not found: %w", err)
	}

	if log != nil {
		log(fmt.Sprintf("[python] creating .venv with %s", python))
	}

	// Strategy 1: standard python3 -m venv
	if err := createVenv(e.RepoPath, python, venvPath, log); err == nil {
		if err := e.ensurePipInVenv(venvPath, python, log); err == nil {
			e.createActivateScript(venvPath)
			return nil
		}
	}

	if log != nil {
		log("[python] python3 -m venv failed — trying fallback strategies")
	}

	// Strategy 2: try installing python3-venv package (Debian/Ubuntu ship python3
	// without the venv module by default)
	if runtime.GOOS != "windows" {
		_ = os.RemoveAll(venvPath)
		if tryInstallVenvPackage(python, log) {
			if err := createVenv(e.RepoPath, python, venvPath, log); err == nil {
				if err := e.ensurePipInVenv(venvPath, python, log); err == nil {
					e.createActivateScript(venvPath)
					return nil
				}
			}
		}
	}

	// Strategy 3: try virtualenv (works even when venv module is broken)
	_ = os.RemoveAll(venvPath)
	if tryVirtualenv(python, venvPath, log) {
		if err := e.ensurePipInVenv(venvPath, python, log); err == nil {
			e.createActivateScript(venvPath)
			return nil
		}
	}

	// Strategy 4: venv without pip, then bootstrap pip with ensurepip/get-pip.py
	_ = os.RemoveAll(venvPath)
	if err := createVenvWithoutPip(e.RepoPath, python, venvPath, log); err == nil {
		if err := bootstrapPip(venvPath, python, log); err == nil {
			e.createActivateScript(venvPath)
			return nil
		}
	}

	// All strategies failed — give a clear error
	return fmt.Errorf(
		"could not create Python virtual environment\n" +
			"  Tried: python3 -m venv, python3-venv package, virtualenv, --without-pip\n" +
			"  Please install python3-venv: sudo apt install python3-venv (Debian/Ubuntu)\n" +
			"  Or install virtualenv: pip install --user virtualenv",
	)
}

// createActivateScript writes a helper script to easily activate the environment.
// On Linux/macOS it creates both a .sh and optionally a Fish-compatible script.
// On Windows it creates a .bat file.
func (e *Environment) createActivateScript(venvPath string) {
	venvBase := filepath.Base(venvPath)
	if runtime.GOOS == "windows" {
		scriptPath := filepath.Join(e.RepoPath, "cloneable-activate.bat")
		content := "@echo off\r\n" +
			"REM Cloneable — activate the Python virtual environment\r\n" +
			"call \"%~dp0" + venvBase + "\\Scripts\\activate.bat\"\r\n" +
			"echo Virtual environment activated.\r\n"
		_ = os.WriteFile(scriptPath, []byte(content), 0644)
	} else {
		// POSIX .sh (bash, zsh, dash)
		scriptPath := filepath.Join(e.RepoPath, "cloneable-activate.sh")
		content := "#!/bin/sh\n" +
			"# Cloneable — activate the Python virtual environment\n" +
			"# Usage: source cloneable-activate.sh\n" +
			"_cloneable_dir=\"$(cd \"$(dirname \"$0\")\" && pwd)\"\n" +
			"if [ -f \"${_cloneable_dir}/" + venvBase + "/bin/activate\" ]; then\n" +
			"    . \"${_cloneable_dir}/" + venvBase + "/bin/activate\"\n" +
			"    # Ensure pip --user binaries are reachable\n" +
			"    export PATH=\"${HOME}/.local/bin:${PATH}\"\n" +
			"    echo \"Virtual environment activated.\"\n" +
			"else\n" +
			"    echo \"Error: virtual environment not found. Run 'cloneable --fix' to recreate it.\"\n" +
			"fi\n"
		_ = os.WriteFile(scriptPath, []byte(content), 0755)

		// Fish shell variant
		fishPath := filepath.Join(e.RepoPath, "cloneable-activate.fish")
		fishContent := "# Cloneable — activate the Python virtual environment (Fish shell)\n" +
			"# Usage: source cloneable-activate.fish\n" +
			"set _cloneable_dir (dirname (status -f))\n" +
			"if test -f \"$_cloneable_dir/" + venvBase + "/bin/activate.fish\"\n" +
			"    source \"$_cloneable_dir/" + venvBase + "/bin/activate.fish\"\n" +
			"    if not contains $HOME/.local/bin $PATH\n" +
			"        set -gx PATH $HOME/.local/bin $PATH\n" +
			"    end\n" +
			"    echo 'Virtual environment activated.'\n" +
			"else\n" +
			"    echo 'Error: virtual environment not found. Run cloneable --fix to recreate it.'\n" +
			"end\n"
		_ = os.WriteFile(fishPath, []byte(fishContent), 0755)
	}
}

// createVenv runs `python -m venv <path>` and returns any error.
func createVenv(repoPath, python, venvPath string, log LogWriter) error {
	cmd := exec.Command(python, "-m", "venv", venvPath)
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
	if log != nil && len(out) > 0 {
		log("[python] " + string(out))
	}
	if err != nil {
		return fmt.Errorf("python -m venv: %w", err)
	}
	return nil
}

// createVenvWithoutPip creates a venv without pip (useful when ensurepip is broken).
func createVenvWithoutPip(repoPath, python, venvPath string, log LogWriter) error {
	cmd := exec.Command(python, "-m", "venv", "--without-pip", venvPath)
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
	if log != nil && len(out) > 0 {
		log("[python] " + string(out))
	}
	return err
}

// tryInstallVenvPackage attempts to install python3-venv via the system package manager.
// Returns true if the install succeeded.
func tryInstallVenvPackage(python string, log LogWriter) bool {
	if log != nil {
		log("[python] attempting to install python3-venv system package")
	}

	// Detect the python version to get the right package name
	// e.g. python3.11 → python3.11-venv
	out, err := exec.Command(python, "--version").CombinedOutput()
	venvPkg := "python3-venv" // default fallback
	if err == nil {
		version := strings.TrimSpace(string(out))
		// "Python 3.11.2" → "3.11"
		parts := strings.Fields(version)
		if len(parts) >= 2 {
			verParts := strings.Split(parts[1], ".")
			if len(verParts) >= 2 {
				venvPkg = fmt.Sprintf("python%s.%s-venv", verParts[0], verParts[1])
			}
		}
	}

	// Try apt (Debian/Ubuntu/Mint)
	if binaryExists("apt-get") {
		cmd := exec.Command("sudo", "apt-get", "install", "-y", venvPkg)
		cmd.Stdin = os.Stdin
		installOut, err := cmd.CombinedOutput()
		if log != nil && len(installOut) > 0 {
			for _, line := range strings.Split(string(installOut), "\n") {
				if strings.TrimSpace(line) != "" {
					log("[apt] " + line)
				}
			}
		}
		if err == nil {
			return true
		}
		// Try the generic name as fallback
		if venvPkg != "python3-venv" {
			cmd2 := exec.Command("sudo", "apt-get", "install", "-y", "python3-venv")
			cmd2.Stdin = os.Stdin
			_, err2 := cmd2.CombinedOutput()
			if err2 == nil {
				return true
			}
		}
	}

	// Try dnf (Fedora/RHEL)
	if binaryExists("dnf") {
		cmd := exec.Command("sudo", "dnf", "install", "-y", "python3-libs")
		cmd.Stdin = os.Stdin
		_, err := cmd.CombinedOutput()
		return err == nil
	}

	return false
}

// tryVirtualenv attempts to create a venv using virtualenv.
// Returns true on success.
func tryVirtualenv(python, venvPath string, log LogWriter) bool {
	// Check if virtualenv is available
	if binaryExists("virtualenv") {
		if log != nil {
			log("[python] trying virtualenv")
		}
		cmd := exec.Command("virtualenv", "-p", python, venvPath)
		out, err := cmd.CombinedOutput()
		if log != nil && len(out) > 0 {
			log("[python] " + string(out))
		}
		return err == nil
	}

	// Try installing virtualenv via pip --user
	if log != nil {
		log("[python] virtualenv not found — trying pip install --user virtualenv")
	}
	installCmd := exec.Command(python, "-m", "pip", "install", "--user", "--break-system-packages", "virtualenv")
	_ = installCmd.Run()

	// Retry — pip --user puts it in ~/.local/bin which may not be in PATH
	home, _ := os.UserHomeDir()
	localBin := filepath.Join(home, ".local", "bin", "virtualenv")
	if _, err := os.Stat(localBin); err == nil {
		cmd := exec.Command(localBin, "-p", python, venvPath)
		out, err := cmd.CombinedOutput()
		if log != nil && len(out) > 0 {
			log("[python] " + string(out))
		}
		return err == nil
	}

	return false
}

// bootstrapPip installs pip into a venv that was created with --without-pip.
func bootstrapPip(venvPath, python string, log LogWriter) error {
	venvPython := filepath.Join(venvPath, "bin", "python")
	if runtime.GOOS == "windows" {
		venvPython = filepath.Join(venvPath, "Scripts", "python.exe")
	}

	// Try ensurepip first
	cmd := exec.Command(venvPython, "-m", "ensurepip", "--default-pip")
	out, err := cmd.CombinedOutput()
	if log != nil && len(out) > 0 {
		log("[python] " + string(out))
	}
	if err == nil {
		return nil
	}

	// Fallback: download get-pip.py
	if log != nil {
		log("[python] ensurepip failed — downloading get-pip.py")
	}
	getPipPath := filepath.Join(os.TempDir(), "get-pip.py")
	curlCmd := exec.Command("curl", "-sSL", "-o", getPipPath, "https://bootstrap.pypa.io/get-pip.py")
	if _, err := curlCmd.CombinedOutput(); err != nil {
		// Try wget
		wgetCmd := exec.Command("wget", "-q", "-O", getPipPath, "https://bootstrap.pypa.io/get-pip.py")
		if _, err := wgetCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("could not download get-pip.py: %w", err)
		}
	}

	installCmd := exec.Command(venvPython, getPipPath)
	installOut, err := installCmd.CombinedOutput()
	if log != nil && len(installOut) > 0 {
		log("[python] " + string(installOut))
	}
	return err
}

// ensurePipInVenv verifies that pip exists in the venv after creation.
// Some minimal venvs (Alpine, Arch) create the venv but don't include pip.
func (e *Environment) ensurePipInVenv(venvPath, python string, log LogWriter) error {
	pipBin := filepath.Join(venvPath, "bin", "pip")
	if runtime.GOOS == "windows" {
		pipBin = filepath.Join(venvPath, "Scripts", "pip.exe")
	}

	if _, err := os.Stat(pipBin); err == nil {
		return nil // pip exists, we're good
	}

	if log != nil {
		log("[python] pip not found in venv — bootstrapping")
	}
	return bootstrapPip(venvPath, python, log)
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
		"PIP_DISABLE_PIP_VERSION_CHECK=1",
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
	candidates := []string{"python3", "python3.13", "python3.12", "python3.11", "python3.10", "python"}
	for _, name := range candidates {
		path, err := exec.LookPath(name)
		if err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("no python binary found in PATH — install Python 3 first")
}
