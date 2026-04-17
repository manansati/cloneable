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

	// ── Build step (compiled languages) ──────────────────────────────────────
	if needsBuild(profile.Primary) {
		buildErr := ui.RunWithSpinner("Building", func() error {
			if log != nil {
				log.Section("Build")
			}
			return buildProject(ctx.RepoPath, profile, environment, safeLog())
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
				if err := buildProject(ctx.RepoPath, profile, environment, safeLog()); err != nil {
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

	// ── Global install confirmation ───────────────────────────────────────────
	if isInstallable(profile.Primary, profile.Category) && !ctx.GlobalInstall {
		opts := ui.GlobalInstallOptions(ctx.RepoName)
		choice, err := ui.RunSelector(
			fmt.Sprintf("How would you like to install %s?", ui.SaffronBold(ctx.RepoName)),
			opts,
		)
		if err != nil {
			return nil, err
		}
		if choice != nil && choice.Value == "global" {
			ctx.GlobalInstall = true
		}
	}

	// ── Global install ────────────────────────────────────────────────────────
	if ctx.GlobalInstall && len(profile.InstallCommands) > 0 {
		installErr := ui.RunWithSpinner("Installing globally", func() error {
			if log != nil {
				log.Section("Global Install")
			}
			return runIn(ctx.RepoPath, safeLog(), environment.GoEnvVars(),
				profile.InstallCommands[0], profile.InstallCommands[1:]...)
		})
		if installErr != nil {
			if log != nil {
				log.Error(installErr)
			}
			// Non-fatal — fall through to local run
			fmt.Printf("\n  %s  Global install failed — running locally instead\n", ui.Warn("!"))
		} else {
			result.InstalledGlobally = true
			result.BinaryName = ctx.RepoName

			// Make symlink if needed (Python, Node, C/C++)
			_ = environment.MakeGlobal(ctx.RepoName, safeLog())
			environment.EnsureBinDirInPath()

			fmt.Printf("\n  %s  %s installed globally\n",
				ui.Tick(), ui.SaffronBold(ctx.RepoName))
			return result, nil
		}
	}

	// ── Launch / Run ─────────────────────────────────────────────────────────
	runErr := ui.RunWithSpinner("Launching", func() error {
		if log != nil {
			log.Section("Launch")
		}
		return launchProject(ctx, profile, environment, safeLog())
	})
	if runErr != nil {
		// Dependency error during launch? Phase II retry.
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

// isInstallable returns true for repos that should offer a global install.
func isInstallable(tech detection.TechType, cat detection.RepoCategory) bool {
	if cat == detection.CategoryDotfiles || cat == detection.CategoryDocs {
		return false
	}
	switch tech {
	case detection.TechDotfile, detection.TechDocs, detection.TechScripts:
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
		// cmake --build or make — run in the build/ directory
		buildDir := filepath.Join(repoPath, "build")
		return runIn(buildDir, log, extraEnv, profile.BuildCommands[0], profile.BuildCommands[1:]...)

	case detection.TechJava:
		// Fix wrapper permissions first
		for _, wrapper := range []string{"gradlew", "mvnw"} {
			wp := filepath.Join(repoPath, wrapper)
			if _, err := os.Stat(wp); err == nil {
				_ = os.Chmod(wp, 0755)
			}
		}
		return runIn(repoPath, log, extraEnv, profile.BuildCommands[0], profile.BuildCommands[1:]...)

	default:
		return runIn(repoPath, log, extraEnv, profile.BuildCommands[0], profile.BuildCommands[1:]...)
	}
}

// ── Launch ────────────────────────────────────────────────────────────────────

// launchProject runs the project based on its tech and category.
func launchProject(ctx LaunchContext, profile *detection.TechProfile, environment *env.Environment, log pkgmanager.LogWriter) error {
	repoPath := ctx.RepoPath
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

	// For CLI tools that need arguments — show arg selector UI
	if profile.Category == detection.CategoryCLI {
		return launchCLI(ctx, profile, extraEnv, log)
	}

	// Standard run
	if len(profile.RunCommands) == 0 {
		return fmt.Errorf("no run command found for %s — try adding a cloneable.yaml", ctx.RepoName)
	}
	return runIn(repoPath, log, extraEnv, profile.RunCommands[0], profile.RunCommands[1:]...)
}

// launchCLI runs a CLI tool, showing an arg-selector UI first.
func launchCLI(ctx LaunchContext, profile *detection.TechProfile, extraEnv []string, log pkgmanager.LogWriter) error {
	if len(profile.RunCommands) == 0 {
		return fmt.Errorf("no run command detected for %s", ctx.RepoName)
	}

	// First run --help to get available options
	helpOutput := getHelpOutput(ctx.RepoPath, profile.RunCommands, extraEnv)

	// Parse --help output into selector options
	opts := parseHelpIntoOptions(helpOutput, ctx.RepoName)

	if len(opts) == 0 {
		// No parseable help — just run it directly
		return runIn(ctx.RepoPath, log, extraEnv, profile.RunCommands[0], profile.RunCommands[1:]...)
	}

	// Show arrow-key selector
	choice, err := ui.RunSelector(
		fmt.Sprintf("How do you want to run %s?", ui.SaffronBold(ctx.RepoName)),
		opts,
	)
	if err != nil || choice == nil {
		// Cancelled — run without args
		return runIn(ctx.RepoPath, log, extraEnv, profile.RunCommands[0], profile.RunCommands[1:]...)
	}

	// Custom argument
	if choice.Value == "__custom__" {
		// TODO: show a text input — for now use the run command directly
		return runIn(ctx.RepoPath, log, extraEnv, profile.RunCommands[0], profile.RunCommands[1:]...)
	}

	// Run with the selected argument
	cmdArgs := append(profile.RunCommands[1:], choice.Value)
	return runIn(ctx.RepoPath, log, extraEnv, profile.RunCommands[0], cmdArgs...)
}

// launchDotfiles applies dotfiles using stow, chezmoi, or install scripts.
func launchDotfiles(repoPath string, log pkgmanager.LogWriter) error {
	// Strategy 1: chezmoi
	if fileExists(repoPath, ".chezmoi.yaml") || fileExists(repoPath, ".chezmoi.toml") {
		return runIn(repoPath, log, nil, "chezmoi", "apply")
	}

	// Strategy 2: GNU stow
	if commandExists("stow") && (fileExists(repoPath, ".stow-local-ignore") || hasDotfileDirs(repoPath)) {
		return runIn(repoPath, log, nil, "stow", ".")
	}

	// Strategy 3: install.sh / setup.sh
	for _, script := range []string{"install.sh", "setup.sh", "bootstrap.sh", "install"} {
		scriptPath := filepath.Join(repoPath, script)
		if isExec(scriptPath) {
			_ = os.Chmod(scriptPath, 0755)
			return runIn(repoPath, log, nil, scriptPath)
		}
	}

	return fmt.Errorf("could not determine how to apply dotfiles — no stow, chezmoi, or install.sh found")
}

// launchDocs renders a markdown file or doc site in the terminal.
func launchDocs(repoPath string, log pkgmanager.LogWriter) error {
	// Try glow (Charm's markdown renderer)
	if commandExists("glow") {
		return runIn(repoPath, log, nil, "glow", ".")
	}
	// Try mdcat
	if commandExists("mdcat") {
		readmeFiles := []string{"README.md", "readme.md", "README", "docs/index.md"}
		for _, f := range readmeFiles {
			if fileExists(repoPath, f) {
				return runIn(repoPath, log, nil, "mdcat", filepath.Join(repoPath, f))
			}
		}
	}
	// Fallback: cat the README
	for _, f := range []string{"README.md", "readme.md", "README.txt", "README"} {
		if fileExists(repoPath, f) {
			content, err := os.ReadFile(filepath.Join(repoPath, f))
			if err == nil {
				fmt.Println(string(content))
				return nil
			}
		}
	}
	return fmt.Errorf("no documentation file found to display")
}

// launchDocker runs the project using docker-compose or docker run.
func launchDocker(repoPath string, log pkgmanager.LogWriter) error {
	if fileExists(repoPath, "docker-compose.yml") {
		return runIn(repoPath, log, nil, "docker-compose", "up")
	}
	if fileExists(repoPath, "docker-compose.yaml") {
		return runIn(repoPath, log, nil, "docker-compose", "-f", "docker-compose.yaml", "up")
	}
	if fileExists(repoPath, "Dockerfile") {
		repoName := filepath.Base(repoPath)
		if err := runIn(repoPath, log, nil, "docker", "build", "-t", repoName, "."); err != nil {
			return err
		}
		return runIn(repoPath, log, nil, "docker", "run", repoName)
	}
	return fmt.Errorf("no docker-compose.yml or Dockerfile found")
}

// launchScripts finds and runs the main shell script in the repo.
func launchScripts(repoPath string, log pkgmanager.LogWriter) error {
	// Look for common entry-point scripts
	for _, name := range []string{"main.sh", "run.sh", "start.sh", "install.sh"} {
		scriptPath := filepath.Join(repoPath, name)
		if isExec(scriptPath) || fileExists(repoPath, name) {
			_ = os.Chmod(scriptPath, 0755)
			return runIn(repoPath, log, nil, "bash", scriptPath)
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
			return runIn(repoPath, log, nil, "bash", scriptPath)
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
				return runIn(ctx.RepoPath, safeLog(), nil, parts[0], parts[1:]...)
			})
			if installErr == nil {
				return &LaunchResult{InstalledGlobally: true, BinaryName: spec.GlobalBin}, nil
			}
		}
	}

	// Run
	if spec.Run != "" {
		runErr := ui.RunWithSpinner("Launching", func() error {
			parts := strings.Fields(spec.Run)
			return runIn(ctx.RepoPath, safeLog(), nil, parts[0], parts[1:]...)
		})
		return &LaunchResult{}, runErr
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
func isDependencyError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	signals := []string{
		"no such file or directory",
		"command not found",
		"cannot find",
		"not found",
		"missing",
		"module not found",
		"cannot load",
		"importerror",
		"no module named",
		"cannot find module",
		"error while loading shared libraries",
		"library not found",
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
	cmd := exec.Command(runCmds[0], append(runCmds[1:], "--help")...)
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
		"i3": true, "sway": true, "rofi": true,
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
