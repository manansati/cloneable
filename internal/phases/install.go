// Package phases contains the three main workflow phases of Cloneable.
// Phase II (this file): detect tech → setup env → install system deps → install language deps.
package phases

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/manansati/cloneable/internal/detection"
	"github.com/manansati/cloneable/internal/env"
	"github.com/manansati/cloneable/internal/logger"
	"github.com/manansati/cloneable/internal/pkgmanager"
	"github.com/manansati/cloneable/internal/ui"
)

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
				var msgs []string
				for pkg, err := range failures {
					msgs = append(msgs, fmt.Sprintf("%s: %v", pkg, err))
				}
				return fmt.Errorf("failed to install system packages: %s", strings.Join(msgs, "; "))
			}
			return nil
		})
		if sysErr != nil {
			if log != nil {
				log.Error(sysErr)
			}
			return result, sysErr
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

	// Strategy 1: pyproject.toml (modern, PEP 517)
	if fileExists(repoPath, "pyproject.toml") {
		return runIn(repoPath, log, environment.PythonEnvVars(), pip, "install", "-e", ".")
	}

	// Strategy 2: requirements.txt
	if fileExists(repoPath, "requirements.txt") {
		if err := runIn(repoPath, log, environment.PythonEnvVars(), pip, "install", "-r", "requirements.txt"); err != nil {
			return err
		}
	}

	// Also check for requirements-dev.txt / requirements_dev.txt
	for _, devReq := range []string{"requirements-dev.txt", "requirements_dev.txt", "requirements/dev.txt"} {
		if fileExists(repoPath, devReq) {
			_ = runIn(repoPath, log, environment.PythonEnvVars(), pip, "install", "-r", devReq)
		}
	}

	// Strategy 3: setup.py
	if fileExists(repoPath, "setup.py") {
		return runIn(repoPath, log, environment.PythonEnvVars(), pip, "install", "-e", ".")
	}

	return nil
}

func installNode(repoPath string, environment *env.Environment, log pkgmanager.LogWriter) error {
	// Ensure the right package manager is installed (pnpm/yarn if needed)
	if err := environment.EnsurePackageManager(log); err != nil {
		return err
	}

	cmd := environment.NodeInstallCmd()
	return runIn(repoPath, log, environment.NodeEnvVars(), cmd[0], cmd[1:]...)
}

func installGo(repoPath string, log pkgmanager.LogWriter) error {
	// go mod download fetches all dependencies declared in go.mod
	if err := runIn(repoPath, log, nil, "go", "mod", "download"); err != nil {
		return err
	}
	// go mod verify ensures integrity
	return runIn(repoPath, log, nil, "go", "mod", "verify")
}

func installRust(repoPath string, log pkgmanager.LogWriter) error {
	// cargo fetch downloads all crates declared in Cargo.toml
	return runIn(repoPath, log, nil, "cargo", "fetch")
}

func installJava(repoPath string, log pkgmanager.LogWriter) error {
	if fileExists(repoPath, "gradlew") {
		// Make gradlew executable (it often loses permissions on clone)
		_ = os.Chmod(filepath.Join(repoPath, "gradlew"), 0755)
		return runIn(repoPath, log, nil, "./gradlew", "dependencies")
	}
	if fileExists(repoPath, "mvnw") {
		_ = os.Chmod(filepath.Join(repoPath, "mvnw"), 0755)
		return runIn(repoPath, log, nil, "./mvnw", "dependency:resolve")
	}
	if fileExists(repoPath, "build.gradle") || fileExists(repoPath, "build.gradle.kts") {
		return runIn(repoPath, log, nil, "gradle", "dependencies")
	}
	return runIn(repoPath, log, nil, "mvn", "dependency:resolve")
}

func installZig(repoPath string, log pkgmanager.LogWriter) error {
	// zig build fetches dependencies declared in build.zig.zon
	return runIn(repoPath, log, nil, "zig", "build", "--fetch")
}

func installFlutter(repoPath string, log pkgmanager.LogWriter) error {
	if fileExists(repoPath, "pubspec.yaml") {
		return runIn(repoPath, log, nil, "flutter", "pub", "get")
	}
	// Pure Dart project
	return runIn(repoPath, log, nil, "dart", "pub", "get")
}

func installRuby(repoPath string, log pkgmanager.LogWriter) error {
	if fileExists(repoPath, "Gemfile") {
		return runIn(repoPath, log, nil, "bundle", "install")
	}
	return nil
}

func installDotnet(repoPath string, log pkgmanager.LogWriter) error {
	return runIn(repoPath, log, nil, "dotnet", "restore")
}

func installHaskell(repoPath string, log pkgmanager.LogWriter) error {
	if fileExists(repoPath, "stack.yaml") {
		return runIn(repoPath, log, nil, "stack", "build", "--only-dependencies")
	}
	return runIn(repoPath, log, nil, "cabal", "build", "--only-dependencies")
}

func installDocker(repoPath string, log pkgmanager.LogWriter) error {
	// Pull images declared in docker-compose
	if fileExists(repoPath, "docker-compose.yml") {
		return runIn(repoPath, log, nil, "docker-compose", "pull")
	}
	if fileExists(repoPath, "docker-compose.yaml") {
		return runIn(repoPath, log, nil, "docker-compose", "-f", "docker-compose.yaml", "pull")
	}
	return nil
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
		return os.RemoveAll(venv)

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
	}

	return nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// runIn runs a command in the given directory with optional extra env vars.
// Output is forwarded to the log writer.
func runIn(dir string, log pkgmanager.LogWriter, extraEnv []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir

	// Inherit current env and add extras
	cmd.Env = append(os.Environ(), extraEnv...)

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

// fileExists checks if a file exists relative to repoPath.
func fileExists(repoPath, rel string) bool {
	_, err := os.Stat(filepath.Join(repoPath, rel))
	return err == nil
}
