package phases

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/manansati/cloneable/internal/detection"
	"github.com/manansati/cloneable/internal/ui"
)

// LaunchContext holds everything Phase III needs.
type LaunchContext struct {
	InstallResult *InstallResult
	RepoPath      string
	RepoName      string
	OSInfo        *detection.OSInfo
	PkgInfo       *detection.PkgManagerInfo
}

// LaunchResult is what Phase III returns.
type LaunchResult struct {
	Success bool
}

// RunLaunch executes Phase III: build → install globally (if chosen) → run.
// If a dependency error is encountered during run, it returns to Phase II and retries.
func RunLaunch(ctx LaunchContext) (*LaunchResult, error) {
	profile := ctx.InstallResult.Profile
	environment := ctx.InstallResult.Env
	log := ctx.InstallResult.Log

	safeLog := func() func(string) {
		if log == nil {
			return func(string) {}
		}
		return log.Writer()
	}

	// ── Build (if necessary) ─────────────────────────────────────────────────
	if needsBuild(profile.Primary) && len(profile.BuildCommands) > 0 {
		// This should have been done in Phase II, but we re-check here
		// to ensure the project is ready for launch.
		err := ui.RunWithSpinner("Ensuring project is built", func() error {
			return BuildProjectWithCascade(profile.WorkingDir, profile, environment, safeLog(), nil, ctx.OSInfo)
		})
		if err != nil {
			return nil, fmt.Errorf("build failed: %w", err)
		}
	}

	// ── Launch ───────────────────────────────────────────────────────────────
	launchErr := launchProject(ctx)
	if launchErr != nil {
		return nil, launchErr
	}

	return &LaunchResult{Success: true}, nil
}

// ── Launch ────────────────────────────────────────────────────────────────────

// launchProject runs the project based on its tech and category.
func launchProject(ctx LaunchContext) error {
	profile := ctx.InstallResult.Profile

	switch profile.Category {
	case detection.CategoryCLI:
		return launchCLI(ctx)
	case detection.CategoryDotfiles:
		return launchDotfiles(ctx)
	case detection.CategoryDocs:
		return launchDocs(ctx)
	}

	// Fallback by tech
	switch profile.Primary {
	case detection.TechDocker:
		return launchDocker(ctx)
	case detection.TechScripts:
		return launchScripts(ctx)
	}

	// Last resort: if we have a binary name, try to run it
	if ctx.InstallResult.BinaryName != "" {
		return runInInteractive(ctx.RepoPath, nil, ctx.InstallResult.BinaryName)
	}

	return fmt.Errorf("don't know how to launch this project (tech: %s, category: %s)", profile.Primary, profile.Category)
}

// launchCLI runs a CLI tool, showing an arg-selector UI first.
func launchCLI(ctx LaunchContext) error {
	binName := ctx.InstallResult.BinaryName
	if binName == "" {
		binName = determineBinaryName(ctx.InstallResult.Profile)
	}

	// Try to find the binary
	binPath := binName
	if !filepath.IsAbs(binPath) {
		// Check .local/bin
		localBin := filepath.Join(ctx.InstallResult.Env.BinDir, binName)
		if fileExistsAbs(localBin) {
			binPath = localBin
		}
	}

	return runInInteractive(ctx.RepoPath, ctx.InstallResult.Env.GoEnvVars(), binPath)
}

// launchDotfiles applies dotfiles using stow, chezmoi, or install scripts.
func launchDotfiles(ctx LaunchContext) error {
	fmt.Printf("\n  %s  Dotfiles application is not yet fully automated.\n", ui.Tick())
	fmt.Println("  Please follow the manual instructions in the README.")
	return nil
}

// launchDocs renders a markdown file beautifully in the terminal.
func launchDocs(ctx LaunchContext) error {
	readme := findReadmeFile(ctx.RepoPath)
	if readme == "" {
		return fmt.Errorf("no README found in %s", ctx.RepoPath)
	}
	return ui.RenderMarkdown(filepath.Join(ctx.RepoPath, readme))
}

func findReadmeFile(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		name := strings.ToLower(e.Name())
		if name == "readme.md" || name == "readme" || name == "readme.txt" {
			return e.Name()
		}
	}
	return ""
}

// launchDocker runs the project using docker compose (v2 plugin) or docker-compose (standalone).
func launchDocker(ctx LaunchContext) error {
	composeCmd := resolveDockerCompose()
	return runInInteractive(ctx.RepoPath, nil, composeCmd[0], append(composeCmd[1:], "up")...)
}

// launchScripts finds and runs the main shell script in the repo.
func launchScripts(ctx LaunchContext) error {
	// Look for common entry points
	for _, s := range []string{"run.sh", "start.sh", "install.sh"} {
		if fileExistsAbs(filepath.Join(ctx.RepoPath, s)) {
			return runInInteractive(ctx.RepoPath, nil, "./"+s)
		}
	}
	return fmt.Errorf("no launch script found")
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func runInInteractive(dir string, extraEnv []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), extraEnv...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

