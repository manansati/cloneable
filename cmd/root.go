package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/manansati/cloneable/internal/detection"
	"github.com/manansati/cloneable/internal/env"
	"github.com/manansati/cloneable/internal/git"
	"github.com/manansati/cloneable/internal/phases"
	"github.com/manansati/cloneable/internal/receipt"
	"github.com/manansati/cloneable/internal/ui"
	"github.com/spf13/cobra"
)

// Version is set at build time via ldflags.
var Version = "0.1.0"

// BuildDate is injected at build time.
var BuildDate = "unknown"

// sysInfo holds detected OS information, populated once at startup.
var sysInfo *detection.OSInfo

// pkgInfo holds detected package managers, populated once at startup.
var pkgInfo *detection.PkgManagerInfo

var rootCmd = &cobra.Command{
	Use:           "cloneable [git-url]",
	Short:         "",
	Long:          "",
	Args:          cobra.MaximumNArgs(1),
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Name() == "help" {
			return nil
		}

		var err error
		sysInfo, err = detection.DetectOS()
		if err != nil {
			return fmt.Errorf("could not detect OS: %w", err)
		}

		if err := detection.EnsureDirectories(sysInfo); err != nil {
			return fmt.Errorf("could not create Cloneable directories: %w", err)
		}

		pkgInfo = detection.DetectPackageManagers(sysInfo)

		// Silent background update check
		go silentUpdateCheck()

		return nil
	},
	RunE: rootRunE,
}

func Execute() {
	// Error 5: On Windows, if the user double-clicked the exe (not running in a proper terminal),
	// re-launch ourselves in cmd.exe so the TUI works correctly.
	if runtime.GOOS == "windows" {
		ensureWindowsTerminal()
	}

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "\n  %s  %s\n\n", ui.Warn("error:"), err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(cloneCmd)
	rootCmd.AddCommand(fixCmd)
	rootCmd.AddCommand(infoCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(exploreCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(removeCmd)
	rootCmd.AddCommand(loginCmd)
	rootCmd.AddCommand(uninstallCmd)

	rootCmd.Flags().BoolP("run", "r", false, "Launch the Current Repository")
	rootCmd.Flags().BoolP("version", "v", false, "Print version information and exit")
	rootCmd.Flags().BoolP("fix", "f", false, "Fix broken dependencies and reinstall")
	rootCmd.Flags().BoolP("info", "i", false, "Show language/technology breakdown of current repo")
	rootCmd.Flags().BoolP("logs", "l", false, "View the installation logs for the current repo")

	rootCmd.CompletionOptions.DisableDefaultCmd = true

	rootCmd.SetUsageTemplate(`Usage:
  cloneable <git-url>    Clone and install dependencies for a repository
  cloneable              Explore trending repositories (or run inside cloned repo)

Commands:
  clone <url>    Clone and install dependencies
  explore        Explore trending repositories
  search <query> Search GitHub interactively
  info [url]     Show language breakdown
  list           List installed repositories
  remove <name>  Remove an installation
  update         Update Cloneable
  login <token>  Set GitHub API token
  uninstall      Uninstall Cloneable
  run            Launch the current repository
  fix            Fix broken dependencies
  logs           View install logs

Flags:
  -r, --run      Launch the current repository
  -f, --fix      Fix broken dependencies
  -i, --info     Language breakdown (current repo)
  -l, --logs     View install logs
  -v, --version  Print version
  -h, --help     Show this help
`)
}

func rootRunE(cmd *cobra.Command, args []string) error {
	versionFlag, _ := cmd.Flags().GetBool("version")
	if versionFlag {
		printVersion()
		return nil
	}

	runFlag, _ := cmd.Flags().GetBool("run")
	if runFlag {
		return runCmd.RunE(runCmd, []string{})
	}

	fixFlag, _ := cmd.Flags().GetBool("fix")
	if fixFlag {
		return fixCmd.RunE(fixCmd, args)
	}

	infoFlag, _ := cmd.Flags().GetBool("info")
	if infoFlag {
		return infoCmd.RunE(infoCmd, []string{})
	}

	logsFlag, _ := cmd.Flags().GetBool("logs")
	if logsFlag {
		return logsCmd.RunE(logsCmd, args)
	}

	if len(args) == 1 {
		return runFullFlow(args[0])
	}

	if isInsideGitRepo() {
		return runInsideRepo()
	}

	return exploreCmd.RunE(exploreCmd, args)
}

func runFullFlow(rawURL string) error {
	cloneResult, err := runClone(rawURL, git.DuplicateAsk)
	if err != nil {
		return err
	}

	installResult, err := phases.RunInstall(phases.InstallContext{
		RepoPath: cloneResult.ClonedPath,
		RepoName: cloneResult.RepoName,
		OSInfo:   sysInfo,
		PkgInfo:  pkgInfo,
	})
	if err != nil {
		if installResult != nil && installResult.Log != nil {
			return fmt.Errorf("%w\n\n  %s  See install.logs: %s", err, ui.Warn("!"), installResult.Log.LogPath)
		}
		return err
	}

	saveReceipt(cloneResult, installResult, rawURL)

	if installResult.Log != nil {
		fmt.Printf("\n  %s  Logs: %s\n",
			ui.Muted("→"), ui.Muted(installResult.Log.LogPath))
	}

	printPostInstallSummary(installResult)
	return nil
}

func runInsideRepo() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	repoName := git.CurrentRepoName(cwd)

	ui.PrintHeader(ui.HeaderInfo{
		OS:         sysInfo.DisplayName(),
		Distro:     string(sysInfo.Distro),
		PkgManager: pkgInfo.DisplayName(),
		RepoName:   repoName,
	})

	installResult, err := phases.RunInstall(phases.InstallContext{
		RepoPath: cwd,
		RepoName: repoName,
		OSInfo:   sysInfo,
		PkgInfo:  pkgInfo,
	})
	if err != nil {
		if installResult != nil && installResult.Log != nil {
			return fmt.Errorf("%w\n\n  %s  See install.logs: %s", err, ui.Warn("!"), installResult.Log.LogPath)
		}
		return err
	}

	printPostInstallSummary(installResult)
	return nil
}

// printPostInstallSummary prints the final usage summary after installation.
// Covers: success message, how to use the binary, PATH status, and restart needs.
func printPostInstallSummary(installResult *phases.InstallResult) {
	if installResult == nil {
		return
	}

	fmt.Println() // spacing

	// ── Determine what was installed ──────────────────────────────────────────
	binName := installResult.BinaryName
	isNonRunnable := installResult.Profile != nil &&
		(installResult.Profile.Category == detection.CategoryLibrary ||
			installResult.Profile.Category == detection.CategoryDocs ||
			installResult.Profile.Category == detection.CategoryDotfiles ||
			installResult.Profile.Category == detection.CategoryUnknown)

	// ── Libraries / docs / dotfiles / unknown — no binary to run ─────────────
	if isNonRunnable || binName == "" {
		tech := "Unknown"
		if installResult.Profile != nil {
			tech = string(installResult.Profile.Primary)
		}
		fmt.Printf("  %s  %s project installed successfully!\n", ui.Tick(), ui.SaffronBold(tech))

		// Offer to render README for repos without executables
		if installResult.Profile != nil {
			offerReadme(installResult.Profile.WorkingDir)
		}
		fmt.Println()
		return
	}

	// ── Binary was installed globally ────────────────────────────────────────
	if installResult.InstalledGlobally {
		fmt.Printf("  %s  Installed successfully!\n\n", ui.Tick())

		// Check if ~/.local/bin is in PATH
		if installResult.Env != nil {
			pathEnv := os.Getenv("PATH")
			binDir := installResult.Env.BinDir

			if env.IsInPath(binDir, pathEnv) {
				// Already in PATH — print usage and rehash hint
				fmt.Printf("  %s  Usage:\n", ui.Muted("→"))
				fmt.Printf("     %s\n\n", ui.SaffronBold(binName))

				shell := detectCurrentShell()
				switch shell {
				case "fish":
					// Fish doesn't cache — ready immediately
				case "zsh":
					fmt.Printf("  %s  Run %s or open a new terminal to use.\n\n",
						ui.Muted("→"), ui.SaffronBold("rehash"))
				default:
					fmt.Printf("  %s  Run %s or open a new terminal to use.\n\n",
						ui.Muted("→"), ui.SaffronBold("hash -r"))
				}
			} else {
				// Need to add to PATH
				fmt.Printf("  %s  %s is installed at %s\n",
					ui.Muted("→"), ui.SaffronBold(binName), ui.Muted(binDir))

				shouldAdd, err := ui.Confirm(fmt.Sprintf("Add %s to your PATH?", binDir))
				if err == nil && shouldAdd {
					installResult.Env.EnsureBinDirInPath()
					installResult.NeedsRestart = true

					fmt.Printf("  %s  Usage (after restarting terminal):\n", ui.Muted("→"))
					fmt.Printf("     %s\n\n", ui.SaffronBold(binName))

					shell := detectCurrentShell()
					switch shell {
					case "zsh":
						fmt.Printf("  %s  Restart your terminal, or run: %s\n\n",
							ui.Warn("!"), ui.SaffronBold("source ~/.zshrc"))
					case "fish":
						fmt.Printf("  %s  Restart your terminal, or run: %s\n\n",
							ui.Warn("!"), ui.SaffronBold("source ~/.config/fish/config.fish"))
					default:
						fmt.Printf("  %s  Restart your terminal, or run: %s\n\n",
							ui.Warn("!"), ui.SaffronBold("source ~/.bashrc"))
					}
				} else {
					fmt.Printf("\n  %s  You can run it directly:\n", ui.Muted("→"))
					fmt.Printf("     %q\n\n", filepath.Join(binDir, binName))
				}
			}
		}
		return
	}

	// ── User declined global install or install failed ───────────────────────
	if installResult.GlobalInstallError != nil {
		fmt.Printf("  %s  No distinct binary was found to install globally.\n\n", ui.Warn("!"))
	} else {
		fmt.Printf("  %s  Build completed successfully! Skipped global install.\n\n", ui.Tick())
	}

	if installResult.Profile != nil {
		runCmd := ""
		if len(installResult.Profile.RunCommands) > 0 {
			runCmd = strings.Join(installResult.Profile.RunCommands, " ")
		} else if binName != "" {
			// Validate that ./binName is actually an executable file, not a directory
			candidatePath := filepath.Join(installResult.Profile.WorkingDir, binName)
			info, err := os.Stat(candidatePath)
			if err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
				runCmd = fmt.Sprintf("%q", "./"+binName)
			} else {
				// ./binName is a directory or doesn't exist — use tech-specific fallback
				fallback := techRunFallback(installResult.Profile)
				if fallback != "" {
					runCmd = fallback
				}
			}
		}

		if runCmd != "" {
			fmt.Printf("  %s  To run the project locally:\n", ui.Muted("→"))
			fmt.Printf("     cd %s\n", ui.Muted(fmt.Sprintf("%q", installResult.Profile.WorkingDir)))
			fmt.Printf("     %s\n\n", ui.SaffronBold(runCmd))

			if installResult.Profile.Primary == detection.TechPython {
				fmt.Printf("  %s  Python virtual environment is ready at %s\n",
					ui.Muted("→"), ui.Muted(".venv"))
				fmt.Printf("     Activate it: %s\n\n", ui.SaffronBold("source cloneable-activate.sh"))
			}
		} else {
			// No run command could be determined — offer README
			offerReadme(installResult.Profile.WorkingDir)
			fmt.Println()
		}
	}
}

// techRunFallback returns a tech-specific run command string when ./binName
// is not a valid executable (e.g. it's a directory with the same name as the repo).
func techRunFallback(profile *detection.TechProfile) string {
	if profile == nil {
		return ""
	}
	switch profile.Primary {
	case detection.TechGo:
		return "go run ."
	case detection.TechRust:
		return "cargo run --release"
	case detection.TechPython:
		for _, entry := range []string{"main.py", "app.py", "run.py", "cli.py"} {
			candidate := filepath.Join(profile.WorkingDir, entry)
			if _, err := os.Stat(candidate); err == nil {
				return "python " + entry
			}
		}
		repoName := filepath.Base(profile.WorkingDir)
		return "python -m " + repoName
	case detection.TechNode:
		return "npm start"
	case detection.TechJava:
		if _, err := os.Stat(filepath.Join(profile.WorkingDir, "gradlew")); err == nil {
			return "./gradlew run"
		}
		return "mvn exec:java"
	case detection.TechZig:
		return "zig build run"
	case detection.TechFlutter:
		return "flutter run"
	case detection.TechDart:
		return "dart run"
	case detection.TechRuby:
		return "bundle exec ruby main.rb"
	case detection.TechDotnet:
		return "dotnet run"
	case detection.TechHaskell:
		if _, err := os.Stat(filepath.Join(profile.WorkingDir, "stack.yaml")); err == nil {
			return "stack run"
		}
		return "cabal run"
	}
	return ""
}

// offerReadme searches for a README file and offers to render it.
// Used for repos where no executable was found (libraries, docs, unknown).
func offerReadme(repoPath string) {
	readmePath := ""
	for _, candidate := range []string{"README.md", "readme.md", "Readme.md", "README.rst", "README.txt", "README"} {
		full := filepath.Join(repoPath, candidate)
		if _, err := os.Stat(full); err == nil {
			readmePath = full
			break
		}
	}
	if readmePath == "" {
		return
	}

	shouldRead, err := ui.Confirm("Would you like to read the README?")
	if err != nil || !shouldRead {
		return
	}
	fmt.Println()
	_ = ui.RenderMarkdown(readmePath)
}

// detectCurrentShell returns the current shell name (bash, zsh, fish).
func detectCurrentShell() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		return "bash"
	}
	base := filepath.Base(shell)
	switch base {
	case "zsh":
		return "zsh"
	case "fish":
		return "fish"
	default:
		return "bash"
	}
}

func isInsideGitRepo() bool {
	_, err := os.Stat(".git")
	return !os.IsNotExist(err)
}

func printVersion() {
	fmt.Printf("cloneable %s\n", Version)
}

func saveReceipt(
	cloneResult *git.CloneResult,
	installResult *phases.InstallResult,
	rawURL string,
) {
	store, err := receipt.NewStore(sysInfo.ReceiptsDir)
	if err != nil {
		return
	}

	r := &receipt.Receipt{
		Name:      cloneResult.RepoName,
		URL:       rawURL,
		ClonePath: cloneResult.ClonedPath,
	}

	if installResult != nil && installResult.Profile != nil {
		r.Tech = string(installResult.Profile.Primary)
		r.Category = string(installResult.Profile.Category)
	}

	if installResult != nil && installResult.Log != nil {
		r.LogPath = installResult.Log.LogPath
	}

	if installResult != nil && installResult.Env != nil {
		r.EnvDir = installResult.Env.EnvDir
		for _, sym := range installResult.Env.Symlinks {
			r.Symlinks = append(r.Symlinks, receipt.SymlinkRecord{
				Source: sym.Source,
				Target: sym.Target,
			})
		}
	}

	r.Version = git.DefaultBranch(cloneResult.ClonedPath)

	_ = store.Save(r)
}

// ensureWindowsTerminal checks if we're running in a proper terminal on Windows.
// When the user double-clicks cloneable.exe, Windows launches it without a console.
// This function detects that situation and re-launches in cmd.exe.
func ensureWindowsTerminal() {
	if runtime.GOOS != "windows" {
		return
	}

	// Check if we have a proper console window by checking if stdin is a terminal.
	// If not (e.g. double-clicked from Explorer), re-launch in cmd.exe.
	if !isTerminal() {
		// Re-launch ourselves in cmd.exe with all original arguments
		exePath, err := os.Executable()
		if err != nil {
			return
		}

		args := []string{"/C", exePath}
		args = append(args, os.Args[1:]...)
		args = append(args, "& pause")

		cmd := exec.Command("cmd.exe", args...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		_ = cmd.Run()
		os.Exit(0)
	}
}

// isTerminal returns true if stdout appears to be a terminal.
func isTerminal() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

