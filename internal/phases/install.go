// Package phases contains the three main workflow phases of Cloneable.
// Phase II (this file): detect tech → setup env → install system deps → install language deps.
package phases

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/manansati/cloneable/internal/detection"
	"github.com/manansati/cloneable/internal/env"
	"github.com/manansati/cloneable/internal/logger"
	"github.com/manansati/cloneable/internal/pkgmanager"
	"github.com/manansati/cloneable/internal/ui"
)

// defaultCmdTimeout is the maximum time a single build/install command can run.
// Individual commands can override via runInWithTimeout.
const defaultCmdTimeout = 15 * time.Minute

// InstallContext holds everything Phase II needs to run.
type InstallContext struct {
	RepoPath string
	RepoName string
	OSInfo   *detection.OSInfo
	PkgInfo  *detection.PkgManagerInfo
}

// InstallResult is returned by RunInstall and passed to Phase III.
type InstallResult struct {
	Profile  *detection.TechProfile
	Env      *env.Environment
	Log      *logger.Logger

	// BinaryName is the primary binary installed (e.g. "fastfetch", "lazygit").
	// Empty for libraries, docs, and dotfiles.
	BinaryName string

	// InstalledGlobally is true if the binary was symlinked to ~/.local/bin.
	// False if the user declined or the project doesn't produce a binary.
	InstalledGlobally bool

	// GlobalInstallError holds any error that occurred during the symlinking phase.
	GlobalInstallError error

	// NeedsRestart is true if the user's terminal needs to be restarted
	// (because we had to add ~/.local/bin to their shell config).
	NeedsRestart bool
}

// RunInstall runs the full Phase II: detect → env setup → system deps → language deps.
// All verbose output goes to install.logs. The UI only sees the spinner.
func RunInstall(ctx InstallContext) (*InstallResult, error) {
	// ── Open install.logs ─────────────────────────────────────────────────────
	log, err := logger.New(ctx.RepoPath)
	if err != nil {
		// Non-fatal — continue without logging
		log = nil
	}

	result := &InstallResult{Log: log}

	// safeLog returns a LogWriter that is safe to call even when log is nil.
	safeLog := func() func(string) {
		if log == nil {
			return func(string) {}
		}
		return log.Writer()
	}

	// ── Tech detection ────────────────────────────────────────────────────────
	var profile *detection.TechProfile
	detectErr := ui.RunWithSpinner("Detecting technology", func() error {
		var err error
		profile, err = detection.DetectTech(ctx.RepoPath)
		if err != nil {
			return err
		}
		if profile.Confidence < 30 {
			// Error 9: Before giving up, check if repo name hints at dotfiles.
			// Repos like "hyprland-dots" may not have enough config dirs for
			// auto-detection, but the name itself is a strong signal.
			repoLower := strings.ToLower(ctx.RepoName)
			if strings.Contains(repoLower, "dots") ||
				strings.Contains(repoLower, "dotfile") ||
				strings.Contains(repoLower, "config") ||
				strings.Contains(repoLower, "rice") {
				profile.Primary = detection.TechDotfile
				profile.Category = detection.CategoryDotfiles
				profile.Confidence = 60
				return nil
			}

			return fmt.Errorf(
				"could not confidently detect the technology in this repository\n"+
					"  Detected: %s (confidence: %d%%)\n"+
					"  Try adding a cloneable.yaml to help Cloneable understand this repo",
				profile.Primary, profile.Confidence,
			)
		}
		return nil
	})
	if detectErr != nil {
		if log != nil {
			log.Error(detectErr)
		}
		// Error 9: Even when detection fails, offer to render README.md
		// so the user can follow manual installation instructions.
		readmePath := ""
		for _, candidate := range []string{"README.md", "readme.md", "Readme.md"} {
			if fileExists(ctx.RepoPath, candidate) {
				readmePath = filepath.Join(ctx.RepoPath, candidate)
				break
			}
		}
		if readmePath != "" {
			fmt.Printf("\n  %s  Could not detect the technology in this repository.\n", ui.Warn("!"))
			shouldRead, _ := ui.Confirm("Would you like to read the README.md for manual install instructions?")
			if shouldRead {
				fmt.Println()
				_ = ui.RenderMarkdown(readmePath)
			}
		}
		return nil, detectErr
	}

	result.Profile = profile

	// If it's a dotfile repo, skip everything else and just prompt for README
	if profile.Category == detection.CategoryDotfiles {
		readmePath := ""
		for _, candidate := range []string{"README.md", "readme.md", "Readme.md"} {
			if fileExists(ctx.RepoPath, candidate) {
				readmePath = filepath.Join(ctx.RepoPath, candidate)
				break
			}
		}
		if readmePath != "" {
			fmt.Printf("\n  %s  %s appears to be a dotfiles repository.\n", ui.Tick(), ctx.RepoName)
			shouldRead, _ := ui.Confirm("Would you like to read the README.md for manual install instructions?")
			if shouldRead {
				fmt.Println()
				_ = ui.RenderMarkdown(readmePath)
			}
		} else {
			fmt.Printf("\n  %s  %s appears to be a dotfiles repository.\n", ui.Tick(), ctx.RepoName)
		}
		// Return early - no dependencies/build needed for dotfiles
		return result, nil
	}

	if log != nil {
		log.Section("Tech Detection")
		log.Write(fmt.Sprintf("primary: %s", profile.Primary))
		log.Write(fmt.Sprintf("category: %s", profile.Category))
		log.Write(fmt.Sprintf("confidence: %d%%", profile.Confidence))
		if len(profile.Extra) > 0 {
			log.Write(fmt.Sprintf("extra techs: %v", profile.Extra))
		}
		log.Write(fmt.Sprintf("system deps: %v", profile.SystemDeps))
	}

	// ── Environment setup ─────────────────────────────────────────────────────
	environment := env.NewEnvironment(ctx.RepoPath, ctx.RepoName, profile.Primary, ctx.OSInfo)
	result.Env = environment

	// ── Toolchain verification ────────────────────────────────────────────────
	// Ensure the primary toolchain binary (cargo, go, zig, etc.) is available.
	// If installed but not in PATH, this finds it at well-known locations.
	// If missing entirely, this attempts to auto-install it via package manager.
	cascade := pkgmanager.NewCascade(ctx.OSInfo, ctx.PkgInfo)
	_ = ui.RunWithSpinner("Verifying toolchain", func() error {
		return environment.EnsureToolchain(safeLog(), cascade)
	})

	envErr := ui.RunWithSpinner("Preparing environment", func() error {
		return environment.Setup(safeLog())
	})
	if envErr != nil {
		if log != nil {
			log.Error(envErr)
		}
		return result, fmt.Errorf("environment setup failed: %w", envErr)
	}

	// ── System dependencies ───────────────────────────────────────────────────

	if len(profile.SystemDeps) > 0 {
		// Authenticate sudo upfront so the password prompt doesn't get swallowed by the spinner
		// or trigger multiple times during individual package installs.
		_ = pkgmanager.AuthenticateSudo()

		sysErr := ui.RunWithSpinner("Installing system dependencies", func() error {
			if log != nil {
				log.Section("System Dependencies")
			}

			// Resolve package names for the current manager
			resolvedDeps := make([]string, 0, len(profile.SystemDeps))
			for _, dep := range profile.SystemDeps {
				resolved := pkgmanager.ResolvePackageName(dep, cascade.PrimaryName())
				resolvedDeps = append(resolvedDeps, resolved)
				if log != nil {
					log.Write(fmt.Sprintf("resolving %s → %s", dep, resolved))
				}
			}

			failures := cascade.InstallMany(resolvedDeps, safeLog())
			if len(failures) > 0 {
				// Only fail if critical deps are missing. Non-critical deps
				// (like optional system libs) should not block the install.
				var criticalFails []string
				for pkg, err := range failures {
					criticalFails = append(criticalFails, fmt.Sprintf("%s: %v", pkg, err))
				}
				// Log all failures but only return error
				if log != nil {
					for _, msg := range criticalFails {
						log.Write(fmt.Sprintf("[sys-deps] FAILED: %s", msg))
					}
				}
				return fmt.Errorf("failed to install system packages: %s", strings.Join(criticalFails, "; "))
			}
			return nil
		})
		if sysErr != nil {
			if log != nil {
				log.Error(sysErr)
			}
			// System dep failures are non-fatal — the build might still succeed
			// if the deps were already installed by a previous run or are optional.
			if log != nil {
				log.Write("[install] system dep install had failures — continuing anyway")
			}
		}
	}

	// ── Language-level dependencies ───────────────────────────────────────────
	langErr := ui.RunWithSpinner("Installing dependencies", func() error {
		if log != nil {
			log.Section("Language Dependencies")
		}
		return installLanguageDeps(profile.WorkingDir, profile, environment, safeLog())
	})
	if langErr != nil {
		// Retry once: re-verify toolchain and try again
		if log != nil {
			log.Write("[install] language dep install failed — retrying after toolchain re-check")
		}
		_ = environment.EnsureToolchain(safeLog(), cascade)
		langErr = installLanguageDeps(profile.WorkingDir, profile, environment, safeLog())
	}
	if langErr != nil {
		if log != nil {
			log.Error(langErr)
		}
		return result, fmt.Errorf("dependency installation failed: %w", langErr)
	}

	// ── Build Project ─────────────────────────────────────────────────────────
	if len(profile.BuildCommands) > 0 {
		buildErr := ui.RunWithSpinner("Building project", func() error {
			if log != nil {
				log.Section("Build Phase")
			}
			return BuildProjectWithCascade(profile.WorkingDir, profile, environment, safeLog(), cascade, ctx.OSInfo)
		})
		if buildErr != nil {
			if log != nil {
				log.Error(buildErr)
			}
			return result, fmt.Errorf("build failed: %w", buildErr)
		}
	}

	// ── Ask user about global PATH install ────────────────────────────────────
	if !isLibraryOrDotfiles(profile.Category) {
		binName := determineBinaryName(profile)
		result.BinaryName = binName

		// Pre-flight check for Node.js: npm install -g requires a "name" field.
		if profile.Primary == detection.TechNode {
			data, err := os.ReadFile(filepath.Join(profile.WorkingDir, "package.json"))
			if err == nil {
				var pkg struct {
					Name string `json:"name"`
				}
				if json.Unmarshal(data, &pkg) == nil && pkg.Name == "" {
					if log != nil {
						log.Write("[install] package.json is missing a 'name' field — skipping global installation")
					}
					binName = "" // skip the prompt
				}
			}
		}

		if binName != "" {
			fmt.Println() // spacing
			shouldInstall, promptErr := ui.Confirm(fmt.Sprintf(
				"Install %s globally to your PATH?", ui.SaffronBold(binName)))

			if promptErr == nil && shouldInstall {
				// Execute the language-specific install commands
				if profile.Primary == detection.TechC || profile.Primary == detection.TechCpp || profile.Primary == detection.TechZig {
					_ = ui.RunWithSpinner("Installing binary", func() error {
						if log != nil {
							log.Section("Install Phase")
						}
						return installCompiledBinary(profile.WorkingDir, profile, environment, safeLog())
					})
				} else if len(profile.InstallCommands) > 0 {
					_ = ui.RunWithSpinner("Installing binary", func() error {
						if log != nil {
							log.Section("Install Phase")
						}
						extraEnv := envVarsForTech(profile.Primary, environment)
						_ = runIn(profile.WorkingDir, safeLog(), extraEnv, profile.InstallCommands[0], profile.InstallCommands[1:]...)
						return nil
					})
				}

				if err := environment.MakeGlobal(binName, safeLog()); err != nil {
					if safeLog() != nil {
						safeLog()(fmt.Sprintf("[install] MakeGlobal failed: %v", err))
					}

					// Python fallback: if MakeGlobal fails (no entry-point binary),
					// create a shell wrapper that activates the venv and runs the primary script.
					if profile.Primary == detection.TechPython {
						wrapErr := createPythonWrapper(profile.WorkingDir, binName, environment, safeLog())
						if wrapErr == nil {
							result.InstalledGlobally = true
						} else {
							result.GlobalInstallError = err
						}
					} else {
						// Pass the error up instead of printing directly to avoid duplicate output
						result.GlobalInstallError = err
					}
				} else {
					result.InstalledGlobally = true
				}
			}
			// If shouldInstall is false, InstalledGlobally remains false and root.go will handle the output.
		}
	}

	return result, nil
}

func isLibraryOrDotfiles(cat detection.RepoCategory) bool {
	return cat == detection.CategoryLibrary || cat == detection.CategoryDotfiles ||
		cat == detection.CategoryDocs || cat == detection.CategoryUnknown
}

func determineBinaryName(profile *detection.TechProfile) string {
	if profile.HasCloneableSpec && profile.CloneableSpec.GlobalBin != "" {
		return profile.CloneableSpec.GlobalBin
	}
	
	// Fast fallback based on the repo name
	// This relies on the environment finding the binary during MakeGlobal
	name := filepath.Base(profile.WorkingDir)
	return name
}

// installCompiledBinary attempts to install a compiled binary using multiple
// fallback strategies. This fixes the fastfetch-style failure where cmake builds
// succeed but the binary doesn't end up in PATH.
//
// Strategy order:
//  1. cmake --install (with local prefix)
//  2. sudo cmake --install (system prefix)
//  3. meson install (for meson projects)
//  4. make install PREFIX=~/.local
//  5. sudo make install
//  6. Manual symlink from build output directory
func installCompiledBinary(workingDir string, profile *detection.TechProfile, environment *env.Environment, log pkgmanager.LogWriter) error {
	home, _ := os.UserHomeDir()
	prefix := filepath.Join(home, ".local")
	buildDir := filepath.Join(workingDir, "build")

	// Strategy 1: cmake --install with local prefix
	if fileExists(workingDir, "CMakeLists.txt") && dirExists(buildDir) {
		if log != nil {
			log("[install] trying cmake --install with prefix " + prefix)
		}
		err := runIn(workingDir, log, nil, "cmake", "--install", buildDir, "--prefix", prefix)
		if err == nil {
			if log != nil {
				log("[install] cmake --install succeeded")
			}
			return nil
		}
		if log != nil {
			log(fmt.Sprintf("[install] cmake --install with local prefix failed: %v", err))
		}

		// Strategy 2: sudo cmake --install (system default prefix /usr/local)
		if log != nil {
			log("[install] trying sudo cmake --install (system prefix)")
		}
		err = runInSudo(workingDir, log, nil, "cmake", "--install", buildDir)
		if err == nil {
			return nil
		}
		if log != nil {
			log(fmt.Sprintf("[install] sudo cmake --install failed: %v", err))
		}
	}

	// Strategy 3: meson install
	if fileExists(workingDir, "meson.build") && dirExists(buildDir) {
		if log != nil {
			log("[install] trying meson install")
		}
		err := runIn(workingDir, log, nil, "meson", "install", "-C", buildDir)
		if err == nil {
			return nil
		}
		// Try with sudo
		err = runInSudo(workingDir, log, nil, "meson", "install", "-C", buildDir)
		if err == nil {
			return nil
		}
		if log != nil {
			log(fmt.Sprintf("[install] meson install failed: %v", err))
		}
	}

	// Strategy 4: make install PREFIX=~/.local
	makefileDirs := []string{workingDir, buildDir}
	for _, dir := range makefileDirs {
		makefile := filepath.Join(dir, "Makefile")
		if _, err := os.Stat(makefile); err != nil {
			continue
		}
		if log != nil {
			log("[install] trying make install PREFIX=" + prefix + " in " + dir)
		}
		err := runIn(dir, log, nil, "make", "install", "PREFIX="+prefix, "DESTDIR=")
		if err == nil {
			return nil
		}

		// Strategy 5: sudo make install
		if log != nil {
			log("[install] trying sudo make install")
		}
		err = runInSudo(dir, log, nil, "make", "install")
		if err == nil {
			return nil
		}
		if log != nil {
			log(fmt.Sprintf("[install] make install failed: %v", err))
		}
	}

	// Strategy 6: Zig build install
	if profile.Primary == detection.TechZig {
		if log != nil {
			log("[install] trying zig build install -p " + prefix)
		}
		err := runIn(workingDir, log, nil, "zig", "build", "install", "-p", prefix)
		if err == nil {
			return nil
		}
	}

	// Strategy 7: Manual symlink from build output
	if log != nil {
		log("[install] all install methods failed — attempting manual symlink from build output")
	}
	return symlinkBuildOutput(workingDir, profile, environment, log)
}

// symlinkBuildOutput scans the build directory for executables and symlinks
// the most likely candidate to ~/.local/bin.
func symlinkBuildOutput(workingDir string, profile *detection.TechProfile, environment *env.Environment, log pkgmanager.LogWriter) error {
	binName := determineBinaryName(profile)
	
	// Directories to scan for the binary
	searchDirs := []string{
		filepath.Join(workingDir, "build"),
		filepath.Join(workingDir, "build", "bin"),
		filepath.Join(workingDir, "build", "src"),
		filepath.Join(workingDir, "build", "Release"),
		filepath.Join(workingDir, "zig-out", "bin"),
	}

	// First pass: look for exact name match
	for _, dir := range searchDirs {
		candidate := filepath.Join(dir, binName)
		if isExecutableFile(candidate) {
			return symlinkToLocalBin(candidate, binName, environment, log)
		}
	}

	// Second pass: recursive scan for any executable matching the repo name
	for _, dir := range searchDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			if !isExecutableFile(path) {
				continue
			}
			name := strings.ToLower(entry.Name())
			repoName := strings.ToLower(binName)
			// Skip shared libraries and test binaries
			if strings.HasSuffix(name, ".so") || strings.HasSuffix(name, ".dll") ||
				strings.HasSuffix(name, ".dylib") || strings.Contains(name, "test") {
				continue
			}
			if strings.Contains(name, repoName) || strings.Contains(repoName, name) {
				return symlinkToLocalBin(path, entry.Name(), environment, log)
			}
		}
	}

	// Third pass: find ANY single executable in the build output
	for _, dir := range searchDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		var executables []string
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			name := strings.ToLower(entry.Name())
			if isExecutableFile(path) && !strings.HasSuffix(name, ".so") && !strings.HasSuffix(name, ".dll") {
				executables = append(executables, path)
			}
		}
		if len(executables) == 1 {
			base := filepath.Base(executables[0])
			return symlinkToLocalBin(executables[0], base, environment, log)
		}
	}

	return fmt.Errorf("could not find binary to install — check build output in %s/build/", workingDir)
}

// symlinkToLocalBin creates a symlink from source to ~/.local/bin/name
func symlinkToLocalBin(source, name string, environment *env.Environment, log pkgmanager.LogWriter) error {
	target := filepath.Join(environment.BinDir, name)
	if err := os.MkdirAll(environment.BinDir, 0755); err != nil {
		return err
	}
	// Remove existing symlink/file
	_ = os.Remove(target)

	if err := os.Symlink(source, target); err != nil {
		// If symlink fails (e.g. cross-device), try copying
		if log != nil {
			log(fmt.Sprintf("[install] symlink failed, trying copy: %v", err))
		}
		data, readErr := os.ReadFile(source)
		if readErr != nil {
			return fmt.Errorf("could not read binary %s: %w", source, readErr)
		}
		if writeErr := os.WriteFile(target, data, 0755); writeErr != nil {
			return fmt.Errorf("could not copy binary to %s: %w", target, writeErr)
		}
	}

	environment.Symlinks = append(environment.Symlinks, env.Symlink{Source: source, Target: target})
	if log != nil {
		log(fmt.Sprintf("[install] installed %s → %s", source, target))
	}
	return nil
}

// isExecutableFile returns true if the path exists, is a file, and has execute permission.
func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0111 != 0
}

// dirExists returns true if the path exists and is a directory.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// installLanguageDeps installs the language-level dependencies for the repo.
// Each tech has its own install command — this function dispatches to the right one.
func installLanguageDeps(repoPath string, profile *detection.TechProfile, environment *env.Environment, log pkgmanager.LogWriter) error {

	switch profile.Primary {
	case detection.TechPython:
		return installPython(repoPath, environment, log)

	case detection.TechNode:
		return installNode(repoPath, environment, log)

	case detection.TechGo:
		return installGo(repoPath, log)

	case detection.TechRust:
		return installRust(repoPath, log)

	case detection.TechJava:
		return installJava(repoPath, log)

	case detection.TechCpp, detection.TechC:
		// C/C++ system deps already installed above — nothing more here
		return nil

	case detection.TechZig:
		return installZig(repoPath, log)

	case detection.TechFlutter, detection.TechDart:
		return installFlutter(repoPath, log)

	case detection.TechRuby:
		return installRuby(repoPath, log)

	case detection.TechDotnet:
		return installDotnet(repoPath, log)

	case detection.TechHaskell:
		return installHaskell(repoPath, log)

	case detection.TechDocker:
		// Docker: just pull images — no local deps
		return installDocker(repoPath, log)

	case detection.TechDotfile, detection.TechDocs, detection.TechScripts:
		// No language deps for these types
		return nil

	default:
		if log != nil {
			log(fmt.Sprintf("unknown tech %s — skipping language dep install", profile.Primary))
		}
		return nil
	}
}

// ── Per-tech installers ───────────────────────────────────────────────────────

func installPython(repoPath string, environment *env.Environment, log pkgmanager.LogWriter) error {
	pip := environment.PipBin()
	envVars := environment.PythonEnvVars()

	// Upgrade pip and install wheel + setuptools first to prevent legacy failures.
	// This is critical for old repos that use pkg_resources, distutils, etc.
	// Best-effort — don't fail if these don't work.
	_ = runIn(repoPath, log, envVars, pip, "install", "--upgrade", "pip")
	_ = runIn(repoPath, log, envVars, pip, "install", "wheel", "setuptools", "packaging")

	// ── Pre-install heavyweight build dependencies ────────────────────────────
	// Some projects (like vLLM) require packages like torch to be present
	// during the metadata generation phase of pip install. Without this,
	// pip fails with "ModuleNotFoundError: No module named 'torch'".
	prereqs := scanPythonBuildPrereqs(repoPath)
	if len(prereqs) > 0 {
		if log != nil {
			log(fmt.Sprintf("[python] pre-installing build prerequisites: %s", strings.Join(prereqs, ", ")))
		}
		prereqArgs := append([]string{"install"}, prereqs...)
		_ = runIn(repoPath, log, envVars, pip, prereqArgs...)
	}

	reqInstalled := false

	// Strategy 1: pyproject.toml (modern, PEP 517)
	if fileExists(repoPath, "pyproject.toml") {
		// Try editable install first (better for development)
		err := runIn(repoPath, log, envVars, pip, "install", "--no-build-isolation", "-e", ".")
		if err != nil {
			// Some projects don't support editable installs — try regular
			err = runIn(repoPath, log, envVars, pip, "install", "--no-build-isolation", ".")
		}
		// Auto-fix legacy modules missing in isolated build environments
		if err != nil && isLegacyPythonError(err) {
			if log != nil {
				log("[python] caught legacy module error — injecting setuptools/pkg_resources and retrying")
			}
			_ = runIn(repoPath, log, envVars, pip, "install", "setuptools", "wheel", "packaging")
			err = runIn(repoPath, log, envVars, pip, "install", "--no-build-isolation", ".")
		}
		if err == nil {
			reqInstalled = true
		} else if log != nil {
			log(fmt.Sprintf("[python] pyproject.toml install failed: %v — trying other strategies", err))
		}
	}

	// Strategy 2: requirements.txt
	if fileExists(repoPath, "requirements.txt") {
		err := runIn(repoPath, log, envVars, pip, "install", "-r", "requirements.txt")
		// Auto-fix legacy module errors in requirements too
		if err != nil && isLegacyPythonError(err) {
			if log != nil {
				log("[python] caught legacy module error in requirements.txt — injecting setuptools and retrying")
			}
			_ = runIn(repoPath, log, envVars, pip, "install", "setuptools", "wheel", "packaging")
			err = runIn(repoPath, log, envVars, pip, "install", "-r", "requirements.txt")
		}
		if err == nil {
			reqInstalled = true
		} else if !reqInstalled {
			// If nothing else worked either, this is the error to return
			if log != nil {
				log(fmt.Sprintf("[python] requirements.txt install failed: %v", err))
			}
			return err
		}
	}

	// Also check for dev requirements (best-effort, never fail)
	for _, devReq := range []string{
		"requirements-dev.txt", "requirements_dev.txt",
		"requirements/dev.txt", "dev-requirements.txt",
		"test-requirements.txt", "requirements/test.txt",
	} {
		if fileExists(repoPath, devReq) {
			_ = runIn(repoPath, log, envVars, pip, "install", "-r", devReq)
		}
	}

	// Strategy 3: setup.py (legacy)
	if fileExists(repoPath, "setup.py") && !reqInstalled {
		err := runIn(repoPath, log, envVars, pip, "install", "--no-build-isolation", "-e", ".")
		if err != nil {
			err = runIn(repoPath, log, envVars, pip, "install", "--no-build-isolation", ".")
		}
		// Auto-fix legacy python projects missing setuptools/distutils/pkg_resources
		if err != nil && isLegacyPythonError(err) {
			if log != nil {
				log("[python] caught legacy module error — injecting setuptools and retrying")
			}
			_ = runIn(repoPath, log, envVars, pip, "install", "setuptools", "wheel")
			err = runIn(repoPath, log, envVars, pip, "install", "--no-build-isolation", ".")
		}
		if err != nil {
			return err
		}
		reqInstalled = true
	}

	// Strategy 4: Pipfile (Pipenv)
	if fileExists(repoPath, "Pipfile") && !reqInstalled {
		if commandExistsInPath("pipenv") {
			return runIn(repoPath, log, envVars, "pipenv", "install")
		}
		// Install pipenv into venv, then use it
		_ = runIn(repoPath, log, envVars, pip, "install", "pipenv")
		return runIn(repoPath, log, envVars, "pipenv", "install")
	}

	// Strategy 5: Poetry
	if fileExists(repoPath, "poetry.lock") && !reqInstalled {
		if commandExistsInPath("poetry") {
			return runIn(repoPath, log, envVars, "poetry", "install", "--no-interaction")
		}
		// Install poetry into venv and use it
		_ = runIn(repoPath, log, envVars, pip, "install", "poetry")
		return runIn(repoPath, log, envVars, "poetry", "install", "--no-interaction")
	}

	// Strategy 6: conda environment.yml (best-effort, never fail)
	if fileExists(repoPath, "environment.yml") && !reqInstalled {
		if commandExistsInPath("conda") {
			_ = runIn(repoPath, log, nil, "conda", "env", "create", "-f", "environment.yml", "--prefix", filepath.Join(repoPath, ".conda-env"), "--force")
		} else if log != nil {
			log("[python] environment.yml found but conda is not installed — skipping")
		}
	}

	return nil
}

// isLegacyPythonError returns true if the error looks like a missing legacy Python module.
// This covers pkg_resources, distutils, setuptools, and other common legacy errors
// that old Python projects hit when building in modern isolated environments.
func isLegacyPythonError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	signals := []string{
		"pkg_resources",
		"no module named 'setuptools'",
		"no module named 'distutils'",
		"modulenotfounderror: no module named",
		"no module named 'wheel'",
		"no module named 'packaging'",
		"error in setup command",
		"failed to build",
		"legacy-install-failure",
		"subprocess-exited-with-error",
	}
	for _, s := range signals {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// createPythonWrapper generates a shell wrapper script in ~/.local/bin/<name>
// that activates the repo's .venv and runs the primary Python script.
// This is the fallback when MakeGlobal fails because the project has no
// entry-point binary (no console_scripts in pyproject/setup.py).
func createPythonWrapper(repoPath, binName string, environment *env.Environment, log pkgmanager.LogWriter) error {
	// Find the primary Python script to wrap
	entryScript := ""
	for _, candidate := range []string{
		"main.py", "app.py", "cli.py", "run.py", "__main__.py",
		filepath.Base(repoPath) + ".py",
	} {
		if fileExists(repoPath, candidate) {
			entryScript = candidate
			break
		}
	}

	if entryScript == "" {
		// Check if the repo can be run as a module (has __init__.py in a subpackage)
		repoName := filepath.Base(repoPath)
		pkgDir := filepath.Join(repoPath, repoName)
		if fileExists(pkgDir, "__init__.py") || fileExists(pkgDir, "__main__.py") {
			// Can be run as `python -m <reponame>`
			entryScript = ""
		} else {
			// No script found — look for any .py file in root
			entries, err := os.ReadDir(repoPath)
			if err == nil {
				for _, e := range entries {
					if !e.IsDir() && strings.HasSuffix(e.Name(), ".py") &&
						e.Name() != "setup.py" && e.Name() != "conftest.py" {
						entryScript = e.Name()
						break
					}
				}
			}
		}
	}

	// Build the wrapper script content
	venvBin := filepath.Join(repoPath, ".venv", "bin")
	absRepoPath := repoPath

	var scriptContent string
	if entryScript != "" {
		absScript := filepath.Join(absRepoPath, entryScript)
		scriptContent = fmt.Sprintf(`#!/usr/bin/env bash
# Auto-generated by Cloneable — activates venv and runs %s
export VIRTUAL_ENV="%s/.venv"
export PATH="%s:$PATH"
unset PYTHONHOME
cd "%s"
exec python3 "%s" "$@"
`, entryScript, absRepoPath, venvBin, absRepoPath, absScript)
	} else {
		// Module mode: python -m <reponame>
		moduleName := strings.ReplaceAll(filepath.Base(repoPath), "-", "_")
		scriptContent = fmt.Sprintf(`#!/usr/bin/env bash
# Auto-generated by Cloneable — activates venv and runs module %s
export VIRTUAL_ENV="%s/.venv"
export PATH="%s:$PATH"
unset PYTHONHOME
cd "%s"
exec python3 -m %s "$@"
`, moduleName, absRepoPath, venvBin, absRepoPath, moduleName)
	}

	// Write the wrapper to ~/.local/bin/<binName>
	if err := os.MkdirAll(environment.BinDir, 0755); err != nil {
		return fmt.Errorf("could not create bin directory: %w", err)
	}

	wrapperPath := filepath.Join(environment.BinDir, binName)
	_ = os.Remove(wrapperPath) // Remove existing

	if err := os.WriteFile(wrapperPath, []byte(scriptContent), 0755); err != nil {
		return fmt.Errorf("could not write Python wrapper: %w", err)
	}

	environment.Symlinks = append(environment.Symlinks, env.Symlink{
		Source: repoPath,
		Target: wrapperPath,
	})

	if log != nil {
		if entryScript != "" {
			log(fmt.Sprintf("[python] created wrapper %s → %s/%s", wrapperPath, repoPath, entryScript))
		} else {
			log(fmt.Sprintf("[python] created wrapper %s → python -m %s", wrapperPath, filepath.Base(repoPath)))
		}
	}

	return nil
}

func installNode(repoPath string, environment *env.Environment, log pkgmanager.LogWriter) error {
	if err := environment.EnsurePackageManager(env.LogWriter(log)); err != nil {
		return err
	}

	if !fileExists(repoPath, "package.json") {
		return nil
	}

	cmd := environment.NodeInstallCmd()
	err := runIn(repoPath, log, environment.NodeEnvVars(), cmd[0], cmd[1:]...)
	if err != nil {
		// Fallback: if the preferred install failed, try plain npm install
		// This handles cases where pnpm/yarn lockfiles are incompatible
		if cmd[0] != "npm" {
			if log != nil {
				log(fmt.Sprintf("[node] %s install failed — falling back to npm install", cmd[0]))
			}
			err = runIn(repoPath, log, environment.NodeEnvVars(), "npm", "install")
		}
	}
	return err
}

func installGo(repoPath string, log pkgmanager.LogWriter) error {
	if !fileExists(repoPath, "go.mod") {
		return nil
	}
	if err := runIn(repoPath, log, nil, "go", "mod", "download"); err != nil {
		return err
	}
	_ = runIn(repoPath, log, nil, "go", "mod", "verify")
	return nil
}

func installRust(repoPath string, log pkgmanager.LogWriter) error {
	if !fileExists(repoPath, "Cargo.toml") {
		return nil
	}
	return runIn(repoPath, log, nil, "cargo", "fetch")
}

func installJava(repoPath string, log pkgmanager.LogWriter) error {
	if fileExists(repoPath, "gradlew") {
		_ = os.Chmod(filepath.Join(repoPath, "gradlew"), 0755)
		err := runIn(repoPath, log, nil, "./gradlew", "dependencies")
		if err != nil {
			// Some projects use assemble instead
			_ = runIn(repoPath, log, nil, "./gradlew", "assemble")
		}
		return nil
	}
	if fileExists(repoPath, "mvnw") {
		_ = os.Chmod(filepath.Join(repoPath, "mvnw"), 0755)
		_ = runIn(repoPath, log, nil, "./mvnw", "dependency:resolve")
		return nil
	}
	if fileExists(repoPath, "build.gradle") || fileExists(repoPath, "build.gradle.kts") {
		_ = runIn(repoPath, log, nil, "gradle", "dependencies")
		return nil
	}
	if fileExists(repoPath, "pom.xml") {
		_ = runIn(repoPath, log, nil, "mvn", "dependency:resolve")
	}
	return nil
}

func installZig(repoPath string, log pkgmanager.LogWriter) error {
	if !fileExists(repoPath, "build.zig.zon") {
		return nil
	}
	_ = runIn(repoPath, log, nil, "zig", "build", "--fetch")
	return nil
}

func installFlutter(repoPath string, log pkgmanager.LogWriter) error {
	if fileExists(repoPath, "pubspec.yaml") {
		err := runIn(repoPath, log, nil, "flutter", "pub", "get")
		if err != nil {
			// Fallback: try dart pub get (for pure Dart projects)
			return runIn(repoPath, log, nil, "dart", "pub", "get")
		}
		return nil
	}
	return runIn(repoPath, log, nil, "dart", "pub", "get")
}

func installRuby(repoPath string, log pkgmanager.LogWriter) error {
	if fileExists(repoPath, "Gemfile") {
		if !commandExistsInPath("bundle") && !commandExistsInPath("bundler") {
			if log != nil {
				log("[ruby] bundle not found in PATH — attempting 'gem install bundler'")
			}
			_ = runIn(repoPath, log, nil, "gem", "install", "bundler")
		}

		err := runIn(repoPath, log, nil, "bundle", "install")
		if err != nil {
			// Some systems need --path vendor/bundle
			return runIn(repoPath, log, nil, "bundle", "install", "--path", "vendor/bundle")
		}
		return nil
	}
	return nil
}

func installDotnet(repoPath string, log pkgmanager.LogWriter) error {
	// Find specific .sln or .csproj to avoid MSB1011 error
	// ("more than one project or solution file")
	project := findDotnetProject(repoPath)
	if project != "" {
		_ = runIn(repoPath, log, nil, "dotnet", "restore", project)
	} else {
		_ = runIn(repoPath, log, nil, "dotnet", "restore")
	}
	return nil
}

// findDotnetProject returns the best .sln or .csproj file to use.
// Prefers .sln over .csproj. If multiple exist, picks the one matching
// the repo name or the first alphabetically.
func findDotnetProject(repoPath string) string {
	repoName := strings.ToLower(filepath.Base(repoPath))

	// First look for .sln files (solution files are preferred)
	slnFiles := findFilesWithExt(repoPath, ".sln")
	if len(slnFiles) == 1 {
		return slnFiles[0]
	}
	if len(slnFiles) > 1 {
		// Pick the one matching the repo name if possible
		for _, f := range slnFiles {
			base := strings.ToLower(strings.TrimSuffix(filepath.Base(f), ".sln"))
			if base == repoName || strings.Contains(repoName, base) || strings.Contains(base, repoName) {
				return f
			}
		}
		return slnFiles[0] // Fall back to first
	}

	// Then look for .csproj files
	csprojFiles := findFilesWithExt(repoPath, ".csproj")
	if len(csprojFiles) == 1 {
		return csprojFiles[0]
	}
	if len(csprojFiles) > 1 {
		for _, f := range csprojFiles {
			base := strings.ToLower(strings.TrimSuffix(filepath.Base(f), ".csproj"))
			if base == repoName || strings.Contains(repoName, base) || strings.Contains(base, repoName) {
				return f
			}
		}
		return csprojFiles[0]
	}

	// Then look for .fsproj files
	fsprojFiles := findFilesWithExt(repoPath, ".fsproj")
	if len(fsprojFiles) >= 1 {
		return fsprojFiles[0]
	}

	return "" // Let dotnet figure it out
}

// findFilesWithExt scans the repo root (non-recursive) for files with the given extension.
func findFilesWithExt(repoPath, ext string) []string {
	var results []string
	entries, err := os.ReadDir(repoPath)
	if err != nil {
		return results
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(strings.ToLower(entry.Name()), ext) {
			results = append(results, entry.Name())
		}
	}
	return results
}

func installHaskell(repoPath string, log pkgmanager.LogWriter) error {
	if fileExists(repoPath, "stack.yaml") {
		_ = runIn(repoPath, log, nil, "stack", "build", "--only-dependencies")
		return nil
	}
	_ = runIn(repoPath, log, nil, "cabal", "build", "--only-dependencies")
	return nil
}

func installDocker(repoPath string, log pkgmanager.LogWriter) error {
	// Modern Docker uses `docker compose` (plugin), fallback to `docker-compose` (standalone)
	composeCmd := resolveDockerCompose()

	if fileExists(repoPath, "docker-compose.yml") {
		return runIn(repoPath, log, nil, composeCmd[0], append(composeCmd[1:], "pull")...)
	}
	if fileExists(repoPath, "docker-compose.yaml") {
		return runIn(repoPath, log, nil, composeCmd[0], append(composeCmd[1:], "-f", "docker-compose.yaml", "pull")...)
	}
	return nil
}

// resolveDockerCompose returns the correct docker compose command.
// Modern Docker Engine v2+ uses `docker compose` (plugin), while older
// installations use the standalone `docker-compose` binary.
func resolveDockerCompose() []string {
	// Try the modern plugin syntax first
	cmd := exec.Command("docker", "compose", "version")
	if err := cmd.Run(); err == nil {
		return []string{"docker", "compose"}
	}
	// Fallback to standalone binary
	return []string{"docker-compose"}
}

// ── Fix (re-install) logic ────────────────────────────────────────────────────

// RunFix re-runs Phase II from scratch, cleaning broken state first.
// Called by `cloneable --fix`.
func RunFix(ctx InstallContext) (*InstallResult, error) {
	log, _ := logger.New(ctx.RepoPath)

	// Detect tech first so we know what to clean
	profile, err := detection.DetectTech(ctx.RepoPath)
	if err != nil {
		return nil, err
	}

	// Clean broken state for this tech
	cleanErr := ui.RunWithSpinner("Cleaning broken state", func() error {
		return cleanBrokenState(ctx.RepoPath, profile.WorkingDir, profile.Primary, log)
	})
	if cleanErr != nil && log != nil {
		log.Error(cleanErr)
	}

	// Re-run full install
	return RunInstall(ctx)
}

// cleanBrokenState removes known broken state for the given tech.
func cleanBrokenState(repoPath string, workingDir string, tech detection.TechType, log *logger.Logger) error {
	var logFn pkgmanager.LogWriter
	if log != nil {
		logFn = log.Writer()
	}

	switch tech {
	case detection.TechPython:
		venv := filepath.Join(repoPath, ".venv")
		if logFn != nil {
			logFn(fmt.Sprintf("removing %s", venv))
		}
		_ = os.RemoveAll(venv)
		// Also remove activate scripts and __pycache__
		for _, f := range []string{"cloneable-activate.sh", "cloneable-activate.bat", "cloneable-activate.fish"} {
			_ = os.Remove(filepath.Join(repoPath, f))
		}
		_ = os.RemoveAll(filepath.Join(repoPath, "__pycache__"))
		_ = os.RemoveAll(filepath.Join(repoPath, ".eggs"))
		_ = os.RemoveAll(filepath.Join(repoPath, "*.egg-info"))
		return nil

	case detection.TechNode:
		dirs := []string{
			filepath.Join(workingDir, "node_modules"),
			filepath.Join(workingDir, ".npm"),
		}
		for _, d := range dirs {
			if logFn != nil {
				logFn(fmt.Sprintf("removing %s", d))
			}
			os.RemoveAll(d) //nolint:errcheck
		}

	case detection.TechRust:
		return runIn(workingDir, logFn, nil, "cargo", "clean")

	case detection.TechGo:
		return runIn(workingDir, logFn, nil, "go", "clean", "-modcache")

	case detection.TechJava:
		if fileExists(workingDir, "gradlew") {
			return runIn(workingDir, logFn, nil, "./gradlew", "clean")
		}
		return runIn(workingDir, logFn, nil, "mvn", "clean")

	case detection.TechCpp, detection.TechC:
		buildDir := filepath.Join(workingDir, "build")
		if logFn != nil {
			logFn(fmt.Sprintf("removing %s", buildDir))
		}
		return os.RemoveAll(buildDir)

	case detection.TechZig:
		for _, dir := range []string{"zig-out", "zig-cache", ".zig-cache"} {
			d := filepath.Join(workingDir, dir)
			if logFn != nil {
				logFn(fmt.Sprintf("removing %s", d))
			}
			os.RemoveAll(d) //nolint:errcheck
		}

	case detection.TechFlutter, detection.TechDart:
		// Remove .dart_tool and pubspec.lock for clean rebuild
		for _, d := range []string{".dart_tool", ".flutter-plugins", ".flutter-plugins-dependencies"} {
			os.RemoveAll(filepath.Join(workingDir, d)) //nolint:errcheck
		}

	case detection.TechDotnet:
		for _, d := range []string{"bin", "obj"} {
			os.RemoveAll(filepath.Join(workingDir, d)) //nolint:errcheck
		}
	}

	return nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// runIn runs a command in the given directory with optional extra env vars.
// Output is forwarded to the log writer.
// Commands are given a timeout to prevent hanging forever.
func runIn(dir string, log pkgmanager.LogWriter, extraEnv []string, name string, args ...string) error {
	return runInWithTimeout(dir, log, extraEnv, false, defaultCmdTimeout, name, args...)
}

// runInSudo runs a command with sudo in the given directory.
func runInSudo(dir string, log pkgmanager.LogWriter, extraEnv []string, name string, args ...string) error {
	return runInWithTimeout(dir, log, extraEnv, true, defaultCmdTimeout, name, args...)
}

func runInWithTimeout(dir string, log pkgmanager.LogWriter, extraEnv []string, useSudo bool, timeout time.Duration, name string, args ...string) error {
	name = env.ResolveExecutable(name)

	if useSudo && pkgmanager.NeedsSudo() {
		// Pass essential environment variables to sudo so tools like rustup/cargo
		// can find their configurations and don't fail when running as root.
		envArgs := []string{
			"HOME=" + os.Getenv("HOME"),
			"PATH=" + os.Getenv("PATH"),
		}
		if cargoHome := os.Getenv("CARGO_HOME"); cargoHome != "" {
			envArgs = append(envArgs, "CARGO_HOME="+cargoHome)
		}
		if rustupHome := os.Getenv("RUSTUP_HOME"); rustupHome != "" {
			envArgs = append(envArgs, "RUSTUP_HOME="+rustupHome)
		}
		
		// Build `sudo env HOME=... PATH=... cmd args...`
		sudoArgs := append([]string{"env"}, envArgs...)
		sudoArgs = append(sudoArgs, name)
		sudoArgs = append(sudoArgs, args...)
		
		args = sudoArgs
		name = "sudo"
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir

	// Inherit current env and add extras
	cmd.Env = append(os.Environ(), extraEnv...)

	out, err := cmd.CombinedOutput()
	if log != nil && len(out) > 0 {
		for _, line := range strings.Split(string(out), "\n") {
			if strings.TrimSpace(line) != "" {
				log(fmt.Sprintf("[%s] %s", filepath.Base(name), line))
			}
		}
	}

	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("%s %s: timed out after %v", name, strings.Join(args, " "), timeout)
	}

	if err != nil {
		return fmt.Errorf("%s %s: %w\nOutput: %s", name, strings.Join(args, " "), err, string(out))
	}
	return nil
}

// commandExistsInPath checks if a binary is in PATH.
func commandExistsInPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// fileExists checks if a file exists relative to repoPath.
func fileExists(repoPath, rel string) bool {
	_, err := os.Stat(filepath.Join(repoPath, rel))
	return err == nil
}

// ── Python build prerequisites ───────────────────────────────────────────────

// heavyweightPythonPackages maps package name prefixes to pip install names.
// These are packages that must be pre-installed before pip can even generate
// metadata for projects that depend on them (like vLLM needing torch).
var heavyweightPythonPackages = map[string]string{
	"torch":         "torch",
	"torchvision":   "torchvision",
	"torchaudio":    "torchaudio",
	"tensorflow":    "tensorflow",
	"jax":           "jax",
	"jaxlib":        "jaxlib",
	"numpy":         "numpy",
	"scipy":         "scipy",
	"cython":        "cython",
	"meson-python":  "meson-python",
	"meson":         "meson",
	"scikit-build":  "scikit-build",
	"scikit-build-core": "scikit-build-core",
	"cmake":         "cmake",
	"ninja":         "ninja",
	"pybind11":      "pybind11",
	"nanobind":      "nanobind",
	"cffi":          "cffi",
	"swig":          "swig",
}

// scanPythonBuildPrereqs scans pyproject.toml and setup.py for heavyweight
// build dependencies that must be pre-installed before pip install can work.
func scanPythonBuildPrereqs(repoPath string) []string {
	seen := map[string]bool{}
	var prereqs []string

	addIfHeavy := func(dep string) {
		// Normalize: strip version specifiers (e.g. "torch>=2.0" → "torch")
		dep = strings.TrimSpace(dep)
		name := dep
		for _, sep := range []string{">=", "<=", "==", "!=", ">", "<", "~=", "[", ";"} {
			if idx := strings.Index(name, sep); idx > 0 {
				name = name[:idx]
			}
		}
		name = strings.TrimSpace(strings.ToLower(name))
		if pipName, ok := heavyweightPythonPackages[name]; ok && !seen[name] {
			seen[name] = true
			prereqs = append(prereqs, pipName)
		}
	}

	// 1. pyproject.toml — [build-system].requires
	if data, err := os.ReadFile(filepath.Join(repoPath, "pyproject.toml")); err == nil {
		content := string(data)
		// Find [build-system] section and extract requires = [...]
		if idx := strings.Index(content, "[build-system]"); idx >= 0 {
			section := content[idx:]
			if reqIdx := strings.Index(section, "requires"); reqIdx >= 0 {
				rest := section[reqIdx:]
				if bracketStart := strings.Index(rest, "["); bracketStart >= 0 {
					if bracketEnd := strings.Index(rest[bracketStart:], "]"); bracketEnd >= 0 {
						reqList := rest[bracketStart+1 : bracketStart+bracketEnd]
						// Parse comma-separated quoted strings
						for _, item := range strings.Split(reqList, ",") {
							item = strings.Trim(strings.TrimSpace(item), "\"'")
							addIfHeavy(item)
						}
					}
				}
			}
		}

		// Also check [project].dependencies and [project.optional-dependencies]
		if idx := strings.Index(content, "[project]"); idx >= 0 {
			section := content[idx:]
			// Find next section start or end
			nextSection := strings.Index(section[1:], "\n[")
			if nextSection < 0 {
				nextSection = len(section)
			} else {
				nextSection++ // offset for the skipped first char
			}
			projSection := section[:nextSection]
			if depIdx := strings.Index(projSection, "dependencies"); depIdx >= 0 {
				rest := projSection[depIdx:]
				if bracketStart := strings.Index(rest, "["); bracketStart >= 0 {
					if bracketEnd := strings.Index(rest[bracketStart:], "]"); bracketEnd >= 0 {
						depList := rest[bracketStart+1 : bracketStart+bracketEnd]
						for _, item := range strings.Split(depList, ",") {
							item = strings.Trim(strings.TrimSpace(item), "\"'")
							addIfHeavy(item)
						}
					}
				}
			}
		}
	}

	// 2. setup.py — setup_requires and install_requires
	if data, err := os.ReadFile(filepath.Join(repoPath, "setup.py")); err == nil {
		content := string(data)
		for _, key := range []string{"setup_requires", "install_requires"} {
			if idx := strings.Index(content, key); idx >= 0 {
				rest := content[idx:]
				if bracketStart := strings.Index(rest, "["); bracketStart >= 0 {
					if bracketEnd := strings.Index(rest[bracketStart:], "]"); bracketEnd >= 0 {
						depList := rest[bracketStart+1 : bracketStart+bracketEnd]
						for _, item := range strings.Split(depList, ",") {
							item = strings.Trim(strings.TrimSpace(item), "\"'")
							addIfHeavy(item)
						}
					}
				}
			}
		}
	}

	return prereqs
}

