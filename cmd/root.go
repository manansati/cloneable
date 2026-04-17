package cmd

import (
	"fmt"
	"os"
	"runtime"

	"github.com/manansati/cloneable/internal/detection"
	"github.com/manansati/cloneable/internal/git"
	"github.com/manansati/cloneable/internal/phases"
	"github.com/manansati/cloneable/internal/receipt"
	"github.com/manansati/cloneable/internal/ui"
	"github.com/spf13/cobra"
)

// Version is the current version of Cloneable.
// This is set at build time via ldflags:
//
//	go build -ldflags "-X github.com/manansati/cloneable/cmd.Version=1.0.0"
var Version = "0.1.0"

// BuildDate is injected at build time.
var BuildDate = "unknown"

// sysInfo holds the detected OS information, populated once at startup.
var sysInfo *detection.OSInfo

// pkgInfo holds the detected package managers, populated once at startup.
var pkgInfo *detection.PkgManagerInfo

// rootCmd is the base command. When called with no subcommand:
//   - With a URL argument  → full clone + install + launch flow
//   - Inside a git repo    → install + launch flow
//   - Otherwise            → print help
var rootCmd = &cobra.Command{
	Use:   "cloneable [git-url]",
	Short: "Clone, install, and launch any GitHub repo — automatically",
	Long: `
  Cloneable detects your OS, clones a GitHub repository,
  installs every dependency it needs, and launches the app.
  No manual setup. No guessing. Just works.

  Usage examples:
    cloneable https://github.com/user/repo    Clone + install + launch
    cloneable                                 Run app in current repo
    cloneable clone https://github.com/...   Clone only
    cloneable search ghostty                 Search GitHub repos
    cloneable --stats                        Language breakdown
    cloneable --fix                          Fix broken dependencies
    cloneable --run                          Launch already-cloned repo
    cloneable --logs                         View installation logs
    cloneable update                         Update Cloneable itself
`,
	Args:          cobra.MaximumNArgs(1),
	SilenceUsage:  true,
	SilenceErrors: true,
	// PersistentPreRunE runs before every command, including subcommands.
	// This is where we detect the OS and package managers once at startup.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Skip heavy detection for lightweight commands
		name := cmd.Name()
		if name == "help" {
			return nil
		}

		var err error
		sysInfo, err = detection.DetectOS()
		if err != nil {
			return fmt.Errorf("could not detect OS: %w", err)
		}

		// Ensure ~/.cloneable/, ~/.cloneable/receipts/, and bin dir exist
		if err := detection.EnsureDirectories(sysInfo); err != nil {
			return fmt.Errorf("could not create Cloneable directories: %w", err)
		}

		pkgInfo = detection.DetectPackageManagers(sysInfo)

		// Check elevation for commands that need it
		if detection.RequiresElevation(name) && !sysInfo.IsElevated {
			fmt.Print(detection.ElevationMessage(sysInfo.Type))
			os.Exit(1)
		}

		// Silent background update check — runs async, never blocks startup
		go silentUpdateCheck()

		return nil
	},
	RunE: rootRunE,
}

// Execute is the entry point called by main.go.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "\n  error: %s\n\n", err)
		os.Exit(1)
	}
}

func init() {
	// Register all subcommands
	rootCmd.AddCommand(cloneCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(fixCmd)
	rootCmd.AddCommand(statsCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(removeCmd)

	// Version flag: -v / --version
	rootCmd.Flags().BoolP("version", "v", false, "Print version information and exit")

	// Run flag: -r / --run
	rootCmd.Flags().BoolP("run", "r", false, "Launch the app in the current repository")

	// Fix flag: -f / --fix
	rootCmd.Flags().BoolP("fix", "f", false, "Fix broken dependencies and reinstall")

	// Stats flag: -s / --stats
	rootCmd.Flags().BoolP("stats", "s", false, "Show language/technology breakdown of current repo")

	// Logs flag: -l / --logs
	rootCmd.Flags().BoolP("logs", "l", false, "View the installation logs for the current repo")

	// Disable the default completion command
	rootCmd.CompletionOptions.DisableDefaultCmd = true
}

// rootRunE handles the root command logic with no subcommand.
func rootRunE(cmd *cobra.Command, args []string) error {
	// -- Version flag --
	versionFlag, _ := cmd.Flags().GetBool("version")
	if versionFlag {
		printVersion()
		return nil
	}

	// -- Run flag --
	runFlag, _ := cmd.Flags().GetBool("run")
	if runFlag {
		return runCmd.RunE(runCmd, args)
	}

	// -- Fix flag --
	fixFlag, _ := cmd.Flags().GetBool("fix")
	if fixFlag {
		return fixCmd.RunE(fixCmd, args)
	}

	// -- Stats flag (no URL = current repo) --
	statsFlag, _ := cmd.Flags().GetBool("stats")
	if statsFlag {
		return statsCmd.RunE(statsCmd, []string{})
	}

	// -- Logs flag --
	logsFlag, _ := cmd.Flags().GetBool("logs")
	if logsFlag {
		return logsCmd.RunE(logsCmd, args)
	}

	// -- URL provided: full clone + install + launch --
	if len(args) == 1 {
		return runFullFlow(args[0])
	}

	// -- No args, no flags: check if inside a git repo --
	if isInsideGitRepo() {
		return runInsideRepo()
	}

	// -- Fallback: show help --
	return cmd.Help()
}

// runFullFlow is the main Cloneable flow: clone → install → launch.
func runFullFlow(rawURL string) error {
	// ── Phase I: Clone ────────────────────────────────────────────────────────
	cloneResult, err := runClone(rawURL, git.DuplicateAsk)
	if err != nil {
		return err
	}

	// ── Phase II: Install dependencies ────────────────────────────────────────
	installResult, err := phases.RunInstall(phases.InstallContext{
		RepoPath: cloneResult.ClonedPath,
		RepoName: cloneResult.RepoName,
		OSInfo:   sysInfo,
		PkgInfo:  pkgInfo,
	})
	if err != nil {
		if installResult != nil && installResult.Log != nil {
			fmt.Printf("\n  %s  See install.logs: %s\n",
				ui.Warn("!"), installResult.Log.LogPath)
		}
		return err
	}

	// ── Phase III: Launch ─────────────────────────────────────────────────────
	launchResult, launchErr := phases.RunLaunch(phases.LaunchContext{
		InstallResult: installResult,
		RepoPath:      cloneResult.ClonedPath,
		RepoName:      cloneResult.RepoName,
		OSInfo:        sysInfo,
	})
	if launchErr != nil {
		if installResult.Log != nil {
			fmt.Printf("\n  %s  See install.logs: %s\n",
				ui.Warn("!"), installResult.Log.LogPath)
		}
		return launchErr
	}

	// ── Save receipt ──────────────────────────────────────────────────────────
	saveReceipt(cloneResult, installResult, launchResult, rawURL)

	if installResult.Log != nil {
		fmt.Printf("\n  %s  Logs: %s\n\n",
			ui.Muted("→"), ui.Muted(installResult.Log.LogPath))
	}
	return nil
}

// runInsideRepo runs install + launch on an already-cloned repo in the cwd.
func runInsideRepo() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	repoName := git.CurrentRepoName(cwd)

	// Print header
	ui.PrintHeader(ui.HeaderInfo{
		OS:         sysInfo.DisplayName(),
		Distro:     string(sysInfo.Distro),
		PkgManager: pkgInfo.DisplayName(),
		RepoName:   repoName,
	})

	// ── Phase II: Install dependencies ────────────────────────────────────────
	installResult, err := phases.RunInstall(phases.InstallContext{
		RepoPath: cwd,
		RepoName: repoName,
		OSInfo:   sysInfo,
		PkgInfo:  pkgInfo,
	})
	if err != nil {
		if installResult != nil && installResult.Log != nil {
			fmt.Printf("\n  %s  See install.logs: %s\n",
				ui.Warn("!"), installResult.Log.LogPath)
		}
		return err
	}

	// ── Phase III: Launch ─────────────────────────────────────────────────────
	_, launchErr := phases.RunLaunch(phases.LaunchContext{
		InstallResult: installResult,
		RepoPath:      cwd,
		RepoName:      repoName,
		OSInfo:        sysInfo,
	})
	if launchErr != nil {
		if installResult.Log != nil {
			fmt.Printf("\n  %s  See install.logs: %s\n",
				ui.Warn("!"), installResult.Log.LogPath)
		}
		return launchErr
	}

	return nil
}

// isInsideGitRepo checks if the current working directory is inside a git repo.
// Full implementation lives in internal/git — this is a lightweight pre-check.
func isInsideGitRepo() bool {
	_, err := os.Stat(".git")
	return !os.IsNotExist(err)
}

// printVersion prints formatted version information.
func printVersion() {
	fmt.Printf("\n")
	fmt.Printf("  Cloneable  v%s\n", Version)
	fmt.Printf("  Platform   %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("  Go         %s\n", runtime.Version())
	if BuildDate != "unknown" {
		fmt.Printf("  Built      %s\n", BuildDate)
	}
	if sysInfo != nil {
		fmt.Printf("  OS         %s\n", sysInfo.DisplayName())
	}
	if pkgInfo != nil {
		fmt.Printf("  Packages   %s\n", pkgInfo.DisplayName())
	}
	if sysInfo != nil {
		fmt.Printf("  Bin dir    %s\n", sysInfo.BinDir)
	}
	fmt.Printf("  Repo       https://github.com/manansati/cloneable\n")
	fmt.Printf("\n")
}

// saveReceipt writes an install receipt after a successful full-flow run.
// Non-fatal — a failed receipt write never stops the user from using their app.
func saveReceipt(
	cloneResult *git.CloneResult,
	installResult *phases.InstallResult,
	launchResult *phases.LaunchResult,
	rawURL string,
) {
	store, err := receipt.NewStore(sysInfo.ReceiptsDir)
	if err != nil {
		return // Non-fatal
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
		// Convert env symlinks to receipt symlinks
		for _, sym := range installResult.Env.Symlinks {
			r.Symlinks = append(r.Symlinks, receipt.SymlinkRecord{
				Source: sym.Source,
				Target: sym.Target,
			})
		}
	}

	if launchResult != nil {
		r.GloballyInstalled = launchResult.InstalledGlobally
		r.BinaryName = launchResult.BinaryName
	}

	r.Version = git.DefaultBranch(cloneResult.ClonedPath)

	_ = store.Save(r) // Non-fatal
}
