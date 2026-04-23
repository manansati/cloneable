// Package phases contains the three main workflow phases of Cloneable.
// Phase II (this file): detect tech → setup env → install system deps → install language deps.
package phases

import (
	"context"
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
	Profile *detection.TechProfile
	Env     *env.Environment
	Log     *logger.Logger
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
		return nil, detectErr
	}

	result.Profile = profile

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
	cascade := pkgmanager.NewCascade(ctx.OSInfo, ctx.PkgInfo)

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
		return installLanguageDeps(ctx.RepoPath, profile, environment, safeLog())
	})
	if langErr != nil {
		if log != nil {
			log.Error(langErr)
		}
		return result, fmt.Errorf("dependency installation failed: %w", langErr)
	}

	return result, nil
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

	// Upgrade pip and install wheel first to prevent legacy setup.py failures.
	// Best-effort — don't fail if these don't work.
	_ = runIn(repoPath, log, envVars, pip, "install", "--upgrade", "pip")
	_ = runIn(repoPath, log, envVars, pip, "install", "wheel", "setuptools")

	reqInstalled := false

	// Strategy 1: pyproject.toml (modern, PEP 517)
	if fileExists(repoPath, "pyproject.toml") {
		// Try editable install first (better for development)
		err := runIn(repoPath, log, envVars, pip, "install", "-e", ".")
		if err != nil {
			// Some projects don't support editable installs — try regular
			err = runIn(repoPath, log, envVars, pip, "install", ".")
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
		err := runIn(repoPath, log, envVars, pip, "install", "-e", ".")
		if err != nil {
			err = runIn(repoPath, log, envVars, pip, "install", ".")
		}
		// Auto-fix legacy python projects missing setuptools
		if err != nil && strings.Contains(err.Error(), "pkg_resources") {
			if log != nil {
				log("[python] caught 'pkg_resources' error — injecting setuptools and retrying")
			}
			_ = runIn(repoPath, log, envVars, pip, "install", "setuptools")
			err = runIn(repoPath, log, envVars, pip, "install", ".")
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
	_ = runIn(repoPath, log, nil, "dotnet", "restore")
	return nil
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
		return cleanBrokenState(ctx.RepoPath, profile.Primary, log)
	})
	if cleanErr != nil && log != nil {
		log.Error(cleanErr)
	}

	// Re-run full install
	return RunInstall(ctx)
}

// cleanBrokenState removes known broken state for the given tech.
func cleanBrokenState(repoPath string, tech detection.TechType, log *logger.Logger) error {
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
			filepath.Join(repoPath, "node_modules"),
			filepath.Join(repoPath, ".npm"),
		}
		for _, d := range dirs {
			if logFn != nil {
				logFn(fmt.Sprintf("removing %s", d))
			}
			os.RemoveAll(d) //nolint:errcheck
		}

	case detection.TechRust:
		return runIn(repoPath, logFn, nil, "cargo", "clean")

	case detection.TechGo:
		return runIn(repoPath, logFn, nil, "go", "clean", "-modcache")

	case detection.TechJava:
		if fileExists(repoPath, "gradlew") {
			return runIn(repoPath, logFn, nil, "./gradlew", "clean")
		}
		return runIn(repoPath, logFn, nil, "mvn", "clean")

	case detection.TechCpp, detection.TechC:
		buildDir := filepath.Join(repoPath, "build")
		if logFn != nil {
			logFn(fmt.Sprintf("removing %s", buildDir))
		}
		return os.RemoveAll(buildDir)

	case detection.TechZig:
		for _, dir := range []string{"zig-out", "zig-cache", ".zig-cache"} {
			d := filepath.Join(repoPath, dir)
			if logFn != nil {
				logFn(fmt.Sprintf("removing %s", d))
			}
			os.RemoveAll(d) //nolint:errcheck
		}

	case detection.TechFlutter, detection.TechDart:
		// Remove .dart_tool and pubspec.lock for clean rebuild
		for _, d := range []string{".dart_tool", ".flutter-plugins", ".flutter-plugins-dependencies"} {
			os.RemoveAll(filepath.Join(repoPath, d)) //nolint:errcheck
		}

	case detection.TechDotnet:
		for _, d := range []string{"bin", "obj"} {
			os.RemoveAll(filepath.Join(repoPath, d)) //nolint:errcheck
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
	if useSudo && pkgmanager.NeedsSudo() {
		args = append([]string{name}, args...)
		name = "sudo"
	}

	name = env.ResolveExecutable(name)

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
