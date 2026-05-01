package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

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
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(removeCmd)

	rootCmd.Flags().BoolP("version", "v", false, "Print version information and exit")
	rootCmd.Flags().BoolP("fix", "f", false, "Fix broken dependencies and reinstall")
	rootCmd.Flags().BoolP("info", "i", false, "Show language/technology breakdown of current repo")
	rootCmd.Flags().BoolP("logs", "l", false, "View the installation logs for the current repo")

	rootCmd.CompletionOptions.DisableDefaultCmd = true

	rootCmd.SetUsageTemplate(`Usage:
  cloneable <git-url>    Clone and install dependencies for a repository
  cloneable              Explore trending repositories (or run inside cloned repo)

Commands:
  clone <url>    Clone only
  explore        Trending repositories
  search <query> Search GitHub interactively
  info [url]     Language breakdown
  list           List installed repos
  remove <name>  Remove an installation
  update         Update Cloneable

Flags:
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
		fmt.Printf("\n  %s  Logs: %s\n\n",
			ui.Muted("→"), ui.Muted(installResult.Log.LogPath))
	}

	offerPathIntegration(installResult)
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

	offerPathIntegration(installResult)
	return nil
}

func offerPathIntegration(installResult *phases.InstallResult) {
	if installResult == nil || installResult.Env == nil || installResult.Env.BinDir == "" {
		return
	}

	binDir := installResult.Env.BinDir
	pathEnv := os.Getenv("PATH")
	
	// Skip if already in PATH
	if env.IsInPath(binDir, pathEnv) {
		return
	}

	fmt.Printf("\n  %s  %s\n", ui.SaffronBold("Path Integration"), ui.Muted("The installed tools are located in "+binDir))
	shouldAdd, err := ui.Confirm(fmt.Sprintf("Would you like to add %s to your PATH?", binDir))
	if err == nil && shouldAdd {
		installResult.Env.EnsureBinDirInPath()
	} else {
		fmt.Printf("\n  %s  Done. You can run it manually from: %s\n\n", ui.Tick(), ui.SaffronBold(installResult.Env.RepoPath))
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

