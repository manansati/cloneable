package phases

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/manansati/cloneable/internal/detection"
	"github.com/manansati/cloneable/internal/env"
	"github.com/manansati/cloneable/internal/git"
	"github.com/manansati/cloneable/internal/logger"
	"github.com/manansati/cloneable/internal/pkgmanager"
	"github.com/manansati/cloneable/internal/ui"
)

// NOTE: Launching and Installing logic is split between two files:
// 1. internal/phases/install.go: Handles Phase II (Detection, Environment Setup, System/Language Dependencies)
// 2. internal/phases/launch.go: Handles Phase III (Building, Global Installation, and Launching/Running)

// LaunchContext holds everything Phase III needs.
type LaunchContext struct {
	InstallResult *InstallResult
	RepoPath      string
	RepoName      string
	OSInfo        *detection.OSInfo
	// GlobalInstall is true if the user confirmed global install.
	GlobalInstall bool
}

// LaunchResult is what Phase III returns.
type LaunchResult struct {
	InstalledGlobally bool
	BinaryName        string
	BinaryPath        string
}

// RunLaunch executes Phase III: build → install globally (if chosen) → run.
// If a dependency error is encountered during run, it returns to Phase II and retries.
func RunLaunch(ctx LaunchContext) (*LaunchResult, error) {
	profile := ctx.InstallResult.Profile
	environment := ctx.InstallResult.Env
	log := ctx.InstallResult.Log

	safeLog := func() pkgmanager.LogWriter {
		if log == nil {
			return func(string) {}
		}
		return log.Writer()
	}

	result := &LaunchResult{}

	// ── cloneable.yaml overrides ──────────────────────────────────────────────
	if profile.HasCloneableSpec && profile.CloneableSpec != nil {
		return runFromSpec(ctx, profile.CloneableSpec, log)
	}

	// IF it's a library, don't build or run it.
	if profile.Category == detection.CategoryLibrary {
		fmt.Printf("\n  %s  %s is a library and cannot be run directly.\n\n", ui.Tick(), ctx.RepoName)
		return result, nil
	}

	// ── Build step (compiled languages) ──────────────────────────────────────
	if needsBuild(profile.Primary) {
		// Error 7: On Windows, native compilation is not supported without MinGW/MSYS2.
		// Rather than fail cryptically, show a clear error message.
		if runtime.GOOS == "windows" && isNativeCompiled(profile.Primary) {
			// TODO: Add MinGW/MSYS2 detection and support in a future release.
			fmt.Printf("\n  %s  Compilation of %s projects is not yet supported on Windows.\n",
				ui.Warn("!"), profile.Primary)
			fmt.Printf("  %s  Native compilation requires toolchains (gcc, make, cmake) that\n", ui.Muted("→"))
			fmt.Printf("  %s  are not available by default on Windows. Install MinGW or WSL,\n", ui.Muted("→"))
			fmt.Printf("  %s  then try again. This will be fixed in a future release.\n\n", ui.Muted("→"))
			return result, nil
		}

		buildErr := ui.RunWithSpinner("Building", func() error {
			if log != nil {
				log.Section("Build")
			}
			// Bulletproof: ensure submodules are present before building
			_ = git.EnsureSubmodules(ctx.RepoPath, func(s string) {
				if log != nil {
					log.Write(s)
				}
			})
			return buildProject(profile.WorkingDir, profile, environment, safeLog())
		})
		if buildErr != nil {
			// Dependency error during build? Attempt Phase II retry once.
			if isDependencyError(buildErr) {
				if log != nil {
					log.Write("[launch] dependency error during build — retrying Phase II")
				}
				_, retryErr := RunInstall(InstallContext{
					RepoPath: ctx.RepoPath,
					RepoName: ctx.RepoName,
					OSInfo:   ctx.OSInfo,
				})
				if retryErr != nil {
					return nil, retryErr
				}
				// Retry build once
				if err := buildProject(profile.WorkingDir, profile, environment, safeLog()); err != nil {
					return nil, err
				}
			} else {
				if log != nil {
					log.Error(buildErr)
				}
				return nil, buildErr
			}
		}
	}

	// ── Global install — always, no confirmation needed ───────────────────────
	if isInstallable(profile.Primary, profile.Category) && len(profile.InstallCommands) > 0 {
		installErr := ui.RunWithSpinner("Installing globally", func() error {
			if log != nil {
				log.Section("Global Install")
			}
			extraEnv := envVarsForTech(profile.Primary, environment)

			if profile.Primary == detection.TechPython {
				// Python PEP 668 strategy — NO pipx (avoids nested venvs):
				// 1. Install into the .venv using the venv's pip (PEP 668 safe — targets .venv, not system)
				// 2. MakeGlobal() then symlinks .venv/bin/<name> → ~/.local/bin/<name>
				// 3. Last resort: pip install --user --break-system-packages

				err := runIn(profile.WorkingDir, safeLog(), extraEnv,
					environment.PipBin(), "install", "--no-build-isolation", ".")
				if err != nil {
					// Editable install might work where regular doesn't
					err = runIn(profile.WorkingDir, safeLog(), extraEnv,
						environment.PipBin(), "install", "--no-build-isolation", "-e", ".")
				}

				// Auto-fix legacy python projects missing setuptools/pkg_resources/distutils
				if err != nil && isLegacyPythonError(err) {
					if log != nil {
						log.Write("[install] caught legacy module error — injecting setuptools and retrying")
					}
					_ = runIn(profile.WorkingDir, safeLog(), extraEnv, environment.PipBin(), "install", "setuptools", "wheel", "packaging")
					err = runIn(profile.WorkingDir, safeLog(), extraEnv, environment.PipBin(), "install", "--no-build-isolation", ".")
				}
				if err != nil {
					// Last resort: pip --user --break-system-packages (escapes venv intentionally)
					if log != nil {
						log.Write("[install] venv pip install failed — trying --user --break-system-packages")
					}
					err = runIn(profile.WorkingDir, safeLog(), nil,
						"python3", "-m", "pip", "install", "--user", "--break-system-packages", ".")
				}
				return err
			}

			// Non-Python, non-Node: try without sudo first, then with sudo
			if profile.Primary == detection.TechNode {
				// Error 6: Validate package.json has a "name" field before npm install -g
				// npm crashes with "Cannot destructure property 'name'" on malformed packages
				if !hasValidPackageName(profile.WorkingDir) {
					if log != nil {
						log.Write("[install] package.json missing or has no 'name' field — skipping npm install -g")
					}
					fmt.Printf("\n  %s  %s is missing a 'name' field in package.json.\n", ui.Warn("!"), ctx.RepoName)
					fmt.Printf("  %s  Skipping global installation.\n\n", ui.Muted("→"))
					// Return special string so we can gracefully skip global install logic
					return fmt.Errorf("SKIP_GLOBAL")
				}
			}

			err := runIn(profile.WorkingDir, safeLog(), extraEnv,
				profile.InstallCommands[0], profile.InstallCommands[1:]...)
			if err != nil {
				if log != nil {
					log.Write("[install] non-root install failed — retrying with sudo")
				}
				err = runInSudo(profile.WorkingDir, safeLog(), extraEnv,
					profile.InstallCommands[0], profile.InstallCommands[1:]...)
			}
			return err
		})
		if installErr != nil {
			if installErr.Error() == "SKIP_GLOBAL" {
				return result, nil
			}
			if log != nil {
				log.Error(installErr)
			}
			// Non-fatal — tell user and skip launch (don't try to run what failed to install)
			fmt.Printf("\n  %s  Global install failed.\n", ui.Warn("!"))
			fmt.Printf("  %s  Check install.logs for details.\n\n", ui.Muted("→"))
			return result, nil
		}

		result.InstalledGlobally = true
		result.BinaryName = ctx.RepoName

		// Symlink the binary from its env location to ~/.local/bin/
		if profile.Primary == detection.TechPython {
			names := detection.GetPythonBinaryNames(profile.WorkingDir, ctx.RepoName)
			if len(names) > 0 {
				result.BinaryName = names[0] // Primary name
			}
			for _, name := range names {
				if err := environment.MakeGlobal(name, env.LogWriter(safeLog())); err != nil {
					if log != nil {
						log.Write(fmt.Sprintf("[launch] warning: MakeGlobal failed for %s: %s", name, err.Error()))
					}
				}
			}
		} else {
			if err := environment.MakeGlobal(ctx.RepoName, env.LogWriter(safeLog())); err != nil {
				if log != nil {
					log.Write("[launch] warning: MakeGlobal failed: " + err.Error())
				}
			}
		}
		environment.EnsureBinDirInPath()

		fmt.Printf("\n  %s  %s installed globally\n",
			ui.Tick(), ui.SaffronBold(ctx.RepoName))

		if profile.Primary == detection.TechPython {
			activateCmd := "source cloneable-activate.sh"
			if runtime.GOOS == "windows" {
				activateCmd = "cloneable-activate.bat"
			}
			fmt.Printf("  %s  To manually use the environment: %s\n",
				ui.Muted("→"), ui.Saffron(activateCmd))
		}

		// For compiled native apps (C, C++, Zig, Rust, Go, Haskell):
		// the binary is now in PATH — just tell the user how to run it.
		if isNativeCompiled(profile.Primary) {
			// Find the actual binary name (might be different from repo name, e.g. neovim -> nvim, redis -> redis-server)
			binPath, err := environment.FindBinary(ctx.RepoName)
			actualName := ctx.RepoName
			if err == nil {
				actualName = filepath.Base(binPath)
				result.BinaryName = actualName
			} else {
				// Error 8: Binary not found under repo name — try known name variants
				// Many projects install binaries with different names (redis → redis-server, etc.)
				variants := knownBinaryVariants(ctx.RepoName)
				for _, variant := range variants {
					if vPath, vErr := environment.FindBinary(variant); vErr == nil {
						binPath = vPath
						actualName = variant
						result.BinaryName = actualName
						err = nil
						break
					}
				}
			}

			fmt.Printf("  %s  Run it with: %s\n\n",
				ui.Muted("→"), ui.SaffronBold(actualName))

			// Ask if they want to open it right now
			// If it's a CLI tool, it's already in PATH — just tell them and exit
			if profile.Category == detection.CategoryCLI {
				return result, nil
			}

			shouldOpen, _ := ui.Confirm(fmt.Sprintf("Launch %s now?", actualName))
			if !shouldOpen {
				return result, nil
			}

			if err != nil {
				// Discovery failed — try exec.LookPath as last resort
				// The PATH was just updated by EnsureBinDirInPath, so try again
				for _, tryName := range append([]string{actualName}, knownBinaryVariants(ctx.RepoName)...) {
					if foundPath, pathErr := exec.LookPath(tryName); pathErr == nil {
						binPath = foundPath
						err = nil
						break
					}
				}
				if err != nil {
					return result, fmt.Errorf("could not find binary %q in search paths", actualName)
				}
			}

			fmt.Println()
			// Use absolute path to avoid PATH cache issues in current process
			launchErr := runInteractive(profile.WorkingDir, nil, binPath)
			// CLI tools commonly exit with code 1 or 2 when invoked without
			// arguments (they print usage and exit non-zero). This is expected
			// behavior — the tool IS installed and working. Don't report it
			// as an error.
			if launchErr != nil && isCLIUsageExit(launchErr) {
				return result, nil
			}
			return result, launchErr
		}

		// For interpreted apps (Python, Node, etc.) — ask to open
		if profile.Category == detection.CategoryCLI {
			fmt.Printf("  %s  Run it from anywhere with: %s\n\n", ui.Muted("→"), ui.SaffronBold(ctx.RepoName))
			return result, nil
		}

		shouldOpen, _ := ui.Confirm(fmt.Sprintf("Open %s now?", ctx.RepoName))
		if !shouldOpen {
			return result, nil
		}
		// Fall through to launch section
	}

	// ── Launch / Run ─────────────────────────────────────────────────────────
	// Only reached for interpreted languages or when no install command exists.
	if log != nil {
		log.Section("Launch")
	}
	runErr := launchProject(ctx, profile, environment, safeLog())
	if runErr != nil {
		// Dependency error? Retry Phase II once.
		if isDependencyError(runErr) {
			if log != nil {
				log.Write("[launch] dependency error at runtime — retrying Phase II")
			}
			_, retryErr := RunInstall(InstallContext{
				RepoPath: ctx.RepoPath,
				RepoName: ctx.RepoName,
				OSInfo:   ctx.OSInfo,
			})
			if retryErr == nil {
				runErr = launchProject(ctx, profile, environment, safeLog())
			}
		}
		if runErr != nil {
			if log != nil {
				log.Error(runErr)
			}
			return nil, runErr
		}
	}

	return result, nil
}

// ── Build ─────────────────────────────────────────────────────────────────────

// needsBuild returns true for languages that require a compile step before running.
func needsBuild(tech detection.TechType) bool {
	switch tech {
	case detection.TechGo, detection.TechRust, detection.TechZig,
		detection.TechCpp, detection.TechC, detection.TechJava,
		detection.TechDotnet, detection.TechHaskell:
		return true
	}
	return false
}

// isNativeCompiled returns true for languages that produce a standalone binary.
// For these, after `make install` / `zig build install`, the binary is in PATH
// and should be run directly — not with `zig build run` or `make run`.
func isNativeCompiled(tech detection.TechType) bool {
	switch tech {
	case detection.TechGo, detection.TechRust, detection.TechZig,
		detection.TechCpp, detection.TechC, detection.TechHaskell:
		return true
	}
	return false
}

// isInstallable returns true for repos that should offer a global install.
func isInstallable(tech detection.TechType, cat detection.RepoCategory) bool {
	if cat == detection.CategoryDotfiles || cat == detection.CategoryDocs || cat == detection.CategoryLibrary {
		return false
	}
	switch tech {
	case detection.TechDotfile, detection.TechDocs:
		return false
	}
	return true
}

// buildProject runs the build commands for the given tech.
func buildProject(repoPath string, profile *detection.TechProfile, environment *env.Environment, log pkgmanager.LogWriter) error {
	if len(profile.BuildCommands) == 0 {
		return nil
	}

	extraEnv := envVarsForTech(profile.Primary, environment)

	switch profile.Primary {
	case detection.TechCpp, detection.TechC:
		return buildCpp(repoPath, log, extraEnv)

	case detection.TechZig:
		// Zig: just run `zig build` in the repo root
		return runIn(repoPath, log, extraEnv, "zig", "build")

	case detection.TechJava:
		for _, wrapper := range []string{"gradlew", "mvnw"} {
			wp := filepath.Join(repoPath, wrapper)
			if _, err := os.Stat(wp); err == nil {
				_ = os.Chmod(wp, 0755)
			}
		}
		return runIn(repoPath, log, extraEnv, profile.BuildCommands[0], profile.BuildCommands[1:]...)

	case detection.TechDotnet:
		// Find specific .sln or .csproj to avoid MSB1011 error
		project := findDotnetProjectInLaunch(repoPath)
		if project != "" {
			return runIn(repoPath, log, extraEnv, "dotnet", "build", project)
		}
		return runIn(repoPath, log, extraEnv, profile.BuildCommands[0], profile.BuildCommands[1:]...)

	default:
		return runIn(repoPath, log, extraEnv, profile.BuildCommands[0], profile.BuildCommands[1:]...)
	}
}

// findDotnetProjectInLaunch is the launch-phase version of findDotnetProject.
// It scans for .sln/.csproj files to specify to dotnet build.
func findDotnetProjectInLaunch(repoPath string) string {
	repoName := strings.ToLower(filepath.Base(repoPath))

	type extCandidate struct {
		ext string
	}
	for _, ec := range []extCandidate{{".sln"}, {".csproj"}, {".fsproj"}} {
		var matches []string
		entries, err := os.ReadDir(repoPath)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), ec.ext) {
				matches = append(matches, entry.Name())
			}
		}
		if len(matches) == 1 {
			return matches[0]
		}
		if len(matches) > 1 {
			// Pick the one matching repo name
			for _, f := range matches {
				base := strings.ToLower(strings.TrimSuffix(f, ec.ext))
				if base == repoName || strings.Contains(repoName, base) || strings.Contains(base, repoName) {
					return f
				}
			}
			return matches[0]
		}
	}
	return ""
}

// buildCpp handles the configure + build two-step for C/C++ projects.
// Configure runs here (NOT in setupCpp) because system deps must be
// installed before configure can succeed.
//
// cmake: cmake -B build -S . → cmake --build build
// meson: meson setup build  → meson compile -C build
// autotools: autoreconf → ./configure → make
// make: make (in repo root)
func buildCpp(repoPath string, log pkgmanager.LogWriter, extraEnv []string) error {
	buildDir := filepath.Join(repoPath, "build")
	home, _ := os.UserHomeDir()
	prefix := filepath.Join(home, ".local")

	// Ensure build directory exists
	_ = os.MkdirAll(buildDir, 0755)

	if fileExistsAbs(filepath.Join(repoPath, "CMakeLists.txt")) {
		// Step 1: configure — always re-run to pick up newly installed deps
		if err := runIn(repoPath, log, extraEnv,
			"cmake", "-B", buildDir, "-S", repoPath,
			"-DCMAKE_BUILD_TYPE=Release",
			fmt.Sprintf("-DCMAKE_INSTALL_PREFIX=%s", prefix),
		); err != nil {
			return fmt.Errorf("cmake configure failed: %w", err)
		}
		// Step 2: build
		return runIn(repoPath, log, extraEnv, "cmake", "--build", buildDir, "--parallel")
	}

	if fileExistsAbs(filepath.Join(repoPath, "meson.build")) {
		// Check if meson was already configured
		if !fileExistsAbs(filepath.Join(buildDir, "build.ninja")) {
			if err := runIn(repoPath, log, extraEnv,
				"meson", "setup", buildDir, repoPath,
				"--buildtype=release",
				fmt.Sprintf("--prefix=%s", prefix),
			); err != nil {
				return fmt.Errorf("meson setup failed: %w", err)
			}
		}
		return runIn(repoPath, log, extraEnv, "meson", "compile", "-C", buildDir)
	}

	if fileExistsAbs(filepath.Join(repoPath, "configure.ac")) && !fileExistsAbs(filepath.Join(repoPath, "configure")) {
		// Need to generate configure script first
		if err := runIn(repoPath, log, extraEnv, "autoreconf", "-fi"); err != nil {
			// Try autogen.sh as fallback
			if fileExistsAbs(filepath.Join(repoPath, "autogen.sh")) {
				_ = os.Chmod(filepath.Join(repoPath, "autogen.sh"), 0755)
				_ = runIn(repoPath, log, extraEnv, "./autogen.sh")
			}
		}
	}

	if fileExistsAbs(filepath.Join(repoPath, "configure")) {
		_ = os.Chmod(filepath.Join(repoPath, "configure"), 0755)
		if err := runIn(repoPath, log, extraEnv, "./configure", fmt.Sprintf("--prefix=%s", prefix)); err != nil {
			return err
		}
		return runIn(repoPath, log, extraEnv, "make", "-j4")
	}

	// Plain Makefile
	if fileExistsAbs(filepath.Join(repoPath, "Makefile")) || fileExistsAbs(filepath.Join(repoPath, "GNUmakefile")) {
		return runIn(repoPath, log, extraEnv, "make", "-j4")
	}

	return fmt.Errorf("no recognised C/C++ build system found (tried cmake, meson, configure, make)")
}

func fileExistsAbs(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}


// ── Launch ────────────────────────────────────────────────────────────────────

// launchProject runs the project based on its tech and category.
func launchProject(ctx LaunchContext, profile *detection.TechProfile, environment *env.Environment, log pkgmanager.LogWriter) error {
	repoPath := profile.WorkingDir
	extraEnv := envVarsForTech(profile.Primary, environment)

	switch profile.Category {
	case detection.CategoryDotfiles:
		return launchDotfiles(repoPath, log)

	case detection.CategoryDocs:
		return launchDocs(repoPath, log)

	case detection.CategoryDocker:
		return launchDocker(repoPath, log)

	case detection.CategoryScripts:
		return launchScripts(repoPath, log)
	}

	// For native compiled languages — find the binary by name instead of using RunCommands
	if isNativeCompiled(profile.Primary) {
		binPath, err := environment.FindBinary(ctx.RepoName)
		if err == nil {
			// Run it interactively
			return runInteractive(repoPath, extraEnv, binPath)
		}
		// Binary not found in expected locations — tell the user
		fmt.Printf("\n  %s  %s is installed — run it from your terminal: %s\n\n",
			ui.Tick(), ctx.RepoName, ui.SaffronBold(ctx.RepoName))
		return nil
	}

	// For CLI tools that need arguments — show arg selector UI
	if profile.Category == detection.CategoryCLI {
		return launchCLI(ctx, profile, extraEnv, log)
	}

	// Standard run
	if len(profile.RunCommands) == 0 {
		return fmt.Errorf("no run command found for %s — try adding a cloneable.yaml", ctx.RepoName)
	}
	return runInteractive(repoPath, extraEnv, profile.RunCommands[0], profile.RunCommands[1:]...)
}

// launchCLI runs a CLI tool, showing an arg-selector UI first.
func launchCLI(ctx LaunchContext, profile *detection.TechProfile, extraEnv []string, log pkgmanager.LogWriter) error {
	if len(profile.RunCommands) == 0 {
		return fmt.Errorf("no run command detected for %s", ctx.RepoName)
	}

	// First run --help to get available options
	helpOutput := getHelpOutput(profile.WorkingDir, profile.RunCommands, extraEnv)

	// Parse --help output into selector options
	opts := parseHelpIntoOptions(helpOutput, ctx.RepoName)

	if len(opts) == 0 {
		// No parseable help — just run it directly
		return runInteractive(profile.WorkingDir, extraEnv, profile.RunCommands[0], profile.RunCommands[1:]...)
	}

	// Show arrow-key selector
	choice, err := ui.RunSelector(
		fmt.Sprintf("How do you want to run %s?", ui.SaffronBold(ctx.RepoName)),
		opts,
	)
	if err != nil || choice == nil {
		// Cancelled — run without args
		return runInteractive(profile.WorkingDir, extraEnv, profile.RunCommands[0], profile.RunCommands[1:]...)
	}

	// Custom argument
	if choice.Value == "__custom__" {
		// TODO: show a text input — for now use the run command directly
		return runInteractive(profile.WorkingDir, extraEnv, profile.RunCommands[0], profile.RunCommands[1:]...)
	}

	// No arguments
	if choice.Value == "__noargs__" {
		return runInteractive(profile.WorkingDir, extraEnv, profile.RunCommands[0], profile.RunCommands[1:]...)
	}

	// Run with the selected argument
	cmdArgs := append(profile.RunCommands[1:], choice.Value)
	return runInteractive(profile.WorkingDir, extraEnv, profile.RunCommands[0], cmdArgs...)
}

// launchDotfiles applies dotfiles using stow, chezmoi, or install scripts.
// Never prints README — only applies configs.
func launchDotfiles(repoPath string, log pkgmanager.LogWriter) error {
	// Strategy 1: chezmoi
	if fileExists(repoPath, ".chezmoi.yaml") || fileExists(repoPath, ".chezmoi.toml") || fileExists(repoPath, ".chezmoiroot") {
		fmt.Printf("\n  %s  Applying dotfiles with chezmoi...\n\n", ui.Saffron("→"))
		return runInteractive(repoPath, nil, "chezmoi", "apply")
	}

	// Strategy 2: yadm (yet another dotfiles manager)
	if fileExists(repoPath, ".yadm") || fileExists(repoPath, ".config/yadm") {
		fmt.Printf("\n  %s  Applying dotfiles with yadm...\n\n", ui.Saffron("→"))
		return runInteractive(repoPath, nil, "yadm", "bootstrap")
	}

	// Strategy 3: GNU stow
	if commandExists("stow") && (fileExists(repoPath, ".stow-local-ignore") || hasDotfileDirs(repoPath)) {
		fmt.Printf("\n  %s  Applying dotfiles with stow...\n\n", ui.Saffron("→"))
		return runInteractive(repoPath, nil, "stow", "--target", os.Getenv("HOME"), ".")
	}

	// Strategy 4: Makefile install
	if fileExists(repoPath, "Makefile") || fileExists(repoPath, "GNUmakefile") {
		fmt.Printf("\n  %s  Running 'make install'...\n\n", ui.Saffron("→"))
		return runInteractive(repoPath, nil, "make", "install")
	}

	// Strategy 5: install/setup/bootstrap scripts (OS-appropriate)
	var scripts []string
	if runtime.GOOS == "windows" {
		scripts = []string{
			"install.ps1", "setup.ps1", "bootstrap.ps1",
			"install.bat", "setup.bat", "bootstrap.bat",
			"install.sh", "setup.sh", "bootstrap.sh",
		}
	} else {
		scripts = []string{
			"install.sh", "setup.sh", "bootstrap.sh",
			"install", "setup", "bootstrap",
		}
	}
	for _, script := range scripts {
		scriptPath := filepath.Join(repoPath, script)
		if _, err := os.Stat(scriptPath); err == nil {
			_ = os.Chmod(scriptPath, 0755)
			fmt.Printf("\n  %s  Running %s...\n\n", ui.Saffron("→"), script)
			if runtime.GOOS == "windows" && strings.HasSuffix(script, ".ps1") {
				return runInteractive(repoPath, nil, "powershell", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
			}
			return runInteractive(repoPath, nil, "bash", scriptPath)
		}
	}

	// Strategy 6: show what's in the repo — don't print files, just tell user
	fmt.Printf("\n  %s  Dotfiles cloned to: %s\n", ui.Tick(), ui.SaffronBold(repoPath))
	fmt.Printf("  %s  No automatic installer found (no stow, chezmoi, yadm, or install script).\n", ui.Muted("→"))
	fmt.Printf("  %s  Copy the config files you need manually.\n\n", ui.Muted("→"))
	return nil
}

// launchDocs renders a markdown file beautifully in the terminal.
// Strategy: try glow (best) → mdcat → built-in renderer (always available).
func launchDocs(repoPath string, log pkgmanager.LogWriter) error {
	// Find the best markdown file to display — case-insensitive check
	mdFile := ""
	for _, candidate := range []string{
		"README.md", "readme.md", "Readme.md", "README.markdown",
		"docs/README.md", "docs/index.md", "docs/readme.md",
		"DOCS.md", "docs.md",
		"GUIDE.md", "guide.md",
		"CONTRIBUTING.md", "CHANGELOG.md",
	} {
		if fileExists(repoPath, candidate) {
			mdFile = filepath.Join(repoPath, candidate)
			break
		}
	}

	// If no candidate matched, scan root for any .md file
	if mdFile == "" {
		entries, err := os.ReadDir(repoPath)
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				name := strings.ToLower(entry.Name())
				if strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".markdown") {
					mdFile = filepath.Join(repoPath, entry.Name())
					break
				}
			}
		}
	}

	if mdFile == "" {
		fmt.Printf("\n  %s  Documentation cloned to: %s\n", ui.Tick(), ui.SaffronBold(repoPath))
		fmt.Printf("  %s  No markdown files found — browse the files manually.\n\n", ui.Muted("→"))
		return nil
	}

	shouldRender, _ := ui.Confirm(fmt.Sprintf("No runnable application detected. Read %s?", filepath.Base(mdFile)))
	if !shouldRender {
		fmt.Printf("\n  %s  Documentation cloned to: %s\n\n", ui.Tick(), ui.SaffronBold(repoPath))
		return nil
	}

	fmt.Printf("\n  %s  Rendering %s\n\n", ui.Tick(), ui.SaffronBold(filepath.Base(mdFile)))

	// Strategy 1: glow (Charm's markdown renderer) — best output
	if commandExists("glow") {
		return runInteractive(repoPath, nil, "glow", mdFile)
	}

	// Strategy 2: mdcat
	if commandExists("mdcat") {
		return runInteractive(repoPath, nil, "mdcat", mdFile)
	}

	// Strategy 3: built-in renderer (always works, no external tools needed)
	return ui.RenderMarkdown(mdFile)
}

// launchDocker runs the project using docker compose (v2 plugin) or docker-compose (standalone).
func launchDocker(repoPath string, log pkgmanager.LogWriter) error {
	compose := resolveDockerCompose()
	if fileExists(repoPath, "docker-compose.yml") {
		return runInteractive(repoPath, nil, compose[0], append(compose[1:], "up")...)
	}
	if fileExists(repoPath, "docker-compose.yaml") {
		return runInteractive(repoPath, nil, compose[0], append(compose[1:], "-f", "docker-compose.yaml", "up")...)
	}
	if fileExists(repoPath, "Dockerfile") {
		repoName := filepath.Base(repoPath)
		if err := runIn(repoPath, log, nil, "docker", "build", "-t", repoName, "."); err != nil {
			return err
		}
		return runInteractive(repoPath, nil, "docker", "run", repoName)
	}
	return fmt.Errorf("no docker-compose.yml or Dockerfile found")
}

// launchScripts finds and runs the main shell script in the repo.
func launchScripts(repoPath string, log pkgmanager.LogWriter) error {
	repoName := filepath.Base(repoPath)

	// First check if there's a file matching the repo name (e.g. neofetch)
	if fileExists(repoPath, repoName) {
		scriptPath := filepath.Join(repoPath, repoName)
		_ = os.Chmod(scriptPath, 0755)
		return runInteractive(repoPath, nil, "bash", scriptPath)
	}

	// Look for common entry-point scripts
	for _, name := range []string{"main.sh", "run.sh", "start.sh", "install.sh"} {
		scriptPath := filepath.Join(repoPath, name)
		if isExec(scriptPath) || fileExists(repoPath, name) {
			_ = os.Chmod(scriptPath, 0755)
			return runInteractive(repoPath, nil, "bash", scriptPath)
		}
	}
	// Fall back to the first .sh file found
	entries, err := os.ReadDir(repoPath)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".sh") {
			scriptPath := filepath.Join(repoPath, entry.Name())
			_ = os.Chmod(scriptPath, 0755)
			return runInteractive(repoPath, nil, "bash", scriptPath)
		}
	}
	return fmt.Errorf("no shell script found to run")
}

// ── cloneable.yaml spec launch ────────────────────────────────────────────────

// runFromSpec launches a project using the exact commands in cloneable.yaml.
func runFromSpec(ctx LaunchContext, spec *detection.CloneableSpec, log *logger.Logger) (*LaunchResult, error) {
	safeLog := func() pkgmanager.LogWriter {
		if log == nil {
			return func(string) {}
		}
		return log.Writer()
	}

	// Build
	if spec.Build != "" {
		buildErr := ui.RunWithSpinner("Building", func() error {
			parts := strings.Fields(spec.Build)
			return runIn(ctx.RepoPath, safeLog(), nil, parts[0], parts[1:]...)
		})
		if buildErr != nil {
			return nil, buildErr
		}
	}

	// Global install confirmation
	if spec.Install != "" {
		opts := ui.GlobalInstallOptions(ctx.RepoName)
		choice, _ := ui.RunSelector(
			fmt.Sprintf("How would you like to install %s?", ui.SaffronBold(ctx.RepoName)),
			opts,
		)
		if choice != nil && choice.Value == "global" {
			installErr := ui.RunWithSpinner("Installing globally", func() error {
				parts := strings.Fields(spec.Install)
				return runInSudo(ctx.RepoPath, safeLog(), nil, parts[0], parts[1:]...)
			})
			if installErr == nil {
				return &LaunchResult{InstalledGlobally: true, BinaryName: spec.GlobalBin}, nil
			}
		}
	}

	// Run
	if spec.Run != "" {
		parts := strings.Fields(spec.Run)
		fmt.Printf("\n  %s  Launching\n\n", ui.Tick())
		return &LaunchResult{}, runInteractive(ctx.RepoPath, nil, parts[0], parts[1:]...)
	}

	return &LaunchResult{}, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// envVarsForTech returns the right environment variables for running a tech's commands.
func envVarsForTech(tech detection.TechType, environment *env.Environment) []string {
	switch tech {
	case detection.TechPython:
		return environment.PythonEnvVars()
	case detection.TechNode:
		return environment.NodeEnvVars()
	case detection.TechGo:
		return environment.GoEnvVars()
	case detection.TechRust:
		return environment.RustEnvVars()
	case detection.TechJava:
		return environment.JavaEnvVars()
	case detection.TechCpp, detection.TechC:
		return environment.CppEnvVars()
	case detection.TechZig:
		return environment.ZigEnvVars()
	case detection.TechFlutter, detection.TechDart:
		return environment.FlutterEnvVars()
	case detection.TechRuby:
		return environment.RubyEnvVars()
	case detection.TechDotnet:
		return environment.DotnetEnvVars()
	case detection.TechHaskell:
		return environment.HaskellEnvVars()
	}
	return nil
}

// isDependencyError returns true if the error looks like a missing dependency.
// This is intentionally strict — only clear signals of missing packages/modules
// trigger a Phase II retry. Generic strings like "not found" or "missing" are
// excluded because they match too many legitimate build errors.
func isDependencyError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	signals := []string{
		"command not found",
		"module not found",
		"cannot load",
		"importerror",
		"no module named",
		"cannot find module",
		"error while loading shared libraries",
		"library not found",
		"package not found",
		"could not resolve dependencies",
		"unresolved import",
	}
	for _, s := range signals {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// getHelpOutput runs the tool with --help and returns its output.
// Used to populate the CLI arg selector.
func getHelpOutput(repoPath string, runCmds []string, extraEnv []string) string {
	if len(runCmds) == 0 {
		return ""
	}
	name := env.ResolveExecutable(runCmds[0])
	cmd := exec.Command(name, append(runCmds[1:], "--help")...)
	cmd.Dir = repoPath
	cmd.Env = append(os.Environ(), extraEnv...)
	out, _ := cmd.CombinedOutput()
	return string(out)
}

// parseHelpIntoOptions extracts CLI flags from --help output into selector options.
func parseHelpIntoOptions(helpText, repoName string) []ui.SelectorOption {
	var opts []ui.SelectorOption
	lines := strings.Split(helpText, "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Look for lines starting with -- (flags) or - (short flags)
		if !strings.HasPrefix(trimmed, "--") && !strings.HasPrefix(trimmed, "-") {
			continue
		}
		// Skip --help and --version lines
		lower := strings.ToLower(trimmed)
		if strings.Contains(lower, "--help") || strings.Contains(lower, "--version") {
			continue
		}

		// Extract the flag and description
		parts := strings.Fields(trimmed)
		if len(parts) == 0 {
			continue
		}
		flag := parts[0]
		// Strip trailing comma from short/long flag combos (-f, --flag)
		flag = strings.TrimSuffix(flag, ",")

		desc := ""
		if len(parts) > 1 {
			desc = strings.Join(parts[1:], " ")
		}

		opts = append(opts, ui.SelectorOption{
			Label:       flag,
			Description: desc,
			Value:       flag,
		})

		// Cap at 12 options to keep the UI clean
		if len(opts) >= 12 {
			break
		}
	}

	// Always add a "Run without arguments" and "Custom argument" option
	opts = append([]ui.SelectorOption{
		{
			Label:       fmt.Sprintf("Run %s (no arguments)", repoName),
			Description: "Launch with default settings",
			Value:       "__noargs__",
		},
	}, opts...)

	opts = append(opts, ui.SelectorOption{
		Label:       "Custom argument",
		Description: "Type your own argument",
		Value:       "__custom__",
	})

	return opts
}

// hasDotfileDirs returns true if the repo has typical dotfile directories.
func hasDotfileDirs(repoPath string) bool {
	entries, err := os.ReadDir(repoPath)
	if err != nil {
		return false
	}
	dotDirs := map[string]bool{
		"nvim": true, "zsh": true, "bash": true, "fish": true,
		"tmux": true, "hypr": true, "kitty": true, "alacritty": true,
		"i3": true, "sway": true, "rofi": true, ".config": true,
		"wezterm": true, "foot": true, "starship": true, "picom": true,
		"bspwm": true, "awesome": true, "vim": true, ".vim": true,
		"emacs": true, ".emacs.d": true, "dunst": true, "polybar": true,
		"eww": true, "waybar": true,
	}
	for _, e := range entries {
		if e.IsDir() && dotDirs[e.Name()] {
			return true
		}
	}
	return false
}

// commandExists checks if a binary is in PATH.
func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// isExec returns true if the file exists and has execute permission.
func isExec(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode()&0111 != 0
}

// runInteractive runs a command with stdin/stdout/stderr connected to the host terminal.
// This is required for interactive CLI tools (like btop, vim, etc.).
func runInteractive(dir string, extraEnv []string, name string, args ...string) error {
	name = env.ResolveExecutable(name)
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), extraEnv...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func showTail(path string, lines int) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	allLines := strings.Split(string(data), "\n")
	start := len(allLines) - lines
	if start < 0 {
		start = 0
	}
	for i := start; i < len(allLines); i++ {
		line := allLines[i]
		if strings.TrimSpace(line) == "" {
			continue
		}
		fmt.Printf("    %s\n", ui.Muted(line))
	}
}

// isCLIUsageExit returns true if the error is an ExitError with code 1 or 2.
// CLI tools commonly exit with these codes when run without arguments —
// they print their usage/help and exit non-zero. This is expected behaviour
// for utilities like zoxide, ripgrep, fd, etc. after a successful install.
func isCLIUsageExit(err error) bool {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code := exitErr.ExitCode()
		return code == 1 || code == 2
	}
	return false
}

// hasValidPackageName checks if package.json exists and contains a valid "name" field.
// npm install -g crashes with "Cannot destructure property 'name'" when the name
// is missing or malformed. This prevents that cryptic error.
func hasValidPackageName(repoPath string) bool {
	data, err := os.ReadFile(filepath.Join(repoPath, "package.json"))
	if err != nil {
		return false
	}
	content := string(data)
	// Check for "name" field with a non-empty string value
	nameIdx := strings.Index(content, `"name"`)
	if nameIdx < 0 {
		return false
	}
	// Find the colon after "name"
	rest := content[nameIdx+len(`"name"`):]
	colonIdx := strings.Index(rest, ":")
	if colonIdx < 0 {
		return false
	}
	// Find the value - should be a non-empty quoted string
	valStr := strings.TrimSpace(rest[colonIdx+1:])
	if len(valStr) < 2 || valStr[0] != '"' {
		return false
	}
	// Find closing quote
	endQuote := strings.Index(valStr[1:], `"`)
	if endQuote <= 0 {
		return false // empty name ""
	}
	return true
}

// knownBinaryVariants returns alternative binary names for well-known projects
// where the binary name differs from the repository name.
// This solves Error 8 (redis installs redis-server/redis-cli, not "redis").
func knownBinaryVariants(repoName string) []string {
	variants := map[string][]string{
		"redis":    {"redis-server", "redis-cli", "redis-sentinel", "redis-benchmark"},
		"neovim":   {"nvim"},
		"neofetch": {"neofetch"},
		"nginx":    {"nginx"},
		"httpd":    {"httpd", "apache2"},
		"postgres": {"postgres", "pg_ctl", "psql"},
		"postgresql": {"postgres", "pg_ctl", "psql"},
		"sqlite":   {"sqlite3"},
		"tmux":     {"tmux"},
		"vim":      {"vim"},
		"emacs":    {"emacs"},
		"gcc":      {"gcc", "g++"},
		"llvm":     {"llc", "clang"},
		"cmake":    {"cmake"},
		"curl":     {"curl"},
		"wget":     {"wget"},
		"htop":     {"htop"},
		"btop":     {"btop"},
		"zsh":      {"zsh"},
		"fish":     {"fish"},
	}

	if v, ok := variants[strings.ToLower(repoName)]; ok {
		return v
	}

	// Also try with common prefixes/suffixes stripped
	lower := strings.ToLower(repoName)
	var guesses []string
	// Try repo-name-d (daemon pattern: redis → redisd, but also redis-server)
	guesses = append(guesses, lower+"-server", lower+"d", lower+"-cli")
	return guesses
}
