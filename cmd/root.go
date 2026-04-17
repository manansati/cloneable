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
	Short:         "Clone, install, and launch any GitHub repo — automatically",
	Long:          `Clone any GitHub repository, install its dependencies, and launch it.`,
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
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "\n  error: %s\n\n", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(cloneCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(fixCmd)
	rootCmd.AddCommand(infoCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(removeCmd)

	rootCmd.Flags().BoolP("version", "v", false, "Print version information and exit")
	rootCmd.Flags().BoolP("run", "r", false, "Launch the app in the current repository")
	rootCmd.Flags().BoolP("fix", "f", false, "Fix broken dependencies and reinstall")
	rootCmd.Flags().BoolP("info", "i", false, "Show language/technology breakdown of current repo")
	rootCmd.Flags().BoolP("logs", "l", false, "View the installation logs for the current repo")

	rootCmd.CompletionOptions.DisableDefaultCmd = true

	rootCmd.SetUsageTemplate(`Usage:
  cloneable <git-url>    Clone, install, and launch a repository
  cloneable              Run inside an already-cloned repository

Commands:
  clone <url>    Clone only
  search <query> Search GitHub interactively
  info [url]     Language breakdown
  list           List installed repos
  remove <name>  Remove an installation
  update         Update Cloneable

Flags:
  -r, --run      Launch the current repo
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
		return runCmd.RunE(runCmd, args)
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

	return cmd.Help()
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
			fmt.Printf("\n  %s  See install.logs: %s\n",
				ui.Warn("!"), installResult.Log.LogPath)
		}
		return err
	}

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

	saveReceipt(cloneResult, installResult, launchResult, rawURL)

	if installResult.Log != nil {
		fmt.Printf("\n  %s  Logs: %s\n\n",
			ui.Muted("→"), ui.Muted(installResult.Log.LogPath))
	}
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
			fmt.Printf("\n  %s  See install.logs: %s\n",
				ui.Warn("!"), installResult.Log.LogPath)
		}
		return err
	}

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

func isInsideGitRepo() bool {
	_, err := os.Stat(".git")
	return !os.IsNotExist(err)
}

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

func saveReceipt(
	cloneResult *git.CloneResult,
	installResult *phases.InstallResult,
	launchResult *phases.LaunchResult,
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

	if launchResult != nil {
		r.GloballyInstalled = launchResult.InstalledGlobally
		r.BinaryName = launchResult.BinaryName
	}

	r.Version = git.DefaultBranch(cloneResult.ClonedPath)

	_ = store.Save(r)
}
