package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/manansati/cloneable/internal/git"
	"github.com/manansati/cloneable/internal/logger"
	"github.com/manansati/cloneable/internal/ui"
	"github.com/spf13/cobra"
)

var cloneCmd = &cobra.Command{
	Use:          "clone <git-url>",
	Short:        "Clone a repository without installing or launching",
	Args:         cobra.ExactArgs(1),
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := runClone(args[0], git.DuplicateAsk)
		return err
	},
}

// runClone is the shared clone implementation.
// Does NOT use a bubbletea spinner — the git progress bar renders directly to stdout.
func runClone(rawURL string, onDuplicate git.DuplicateAction) (*git.CloneResult, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("could not determine current directory: %w", err)
	}

	repoName := git.ExtractRepoName(rawURL)

	ui.PrintHeader(ui.HeaderInfo{
		OS:         sysInfo.DisplayName(),
		Distro:     string(sysInfo.Distro),
		PkgManager: pkgInfo.DisplayName(),
		RepoName:   repoName,
	})

	if onDuplicate == git.DuplicateAsk {
		resolved, err := resolveDuplicate(cwd, repoName)
		if err != nil {
			return nil, err
		}
		onDuplicate = resolved
	}

	// Open a log writer for git output (goes to install.logs if repo exists,
	// otherwise discarded — we haven't cloned yet so there's no repo dir)
	var logFn func(string)
	tmpLogPath := filepath.Join(os.TempDir(), "cloneable-clone.log")
	if lf, err := logger.NewRaw(tmpLogPath); err == nil {
		logFn = lf.Write
		defer lf.Close()
	}

	// Print the label — progress bar will render on the same line block below
	fmt.Printf("\n  Cloning %s\n\n", ui.SaffronBold(repoName))

	result, err := git.Clone(git.CloneOptions{
		URL:         rawURL,
		DestDir:     cwd,
		OnDuplicate: onDuplicate,
		Auth:        gitAuthFromEnv(),
		ProgressFn:  logFn,
	})
	if err != nil {
		fmt.Println() // ensure we're on a new line after any partial progress
		return nil, err
	}

	if result.AlreadyExisted {
		fmt.Printf("\n  %s  Using existing directory: %s\n",
			ui.Tick(), ui.SaffronBold(result.ClonedPath))
	} else {
		fmt.Printf("\n  %s  %s cloned to %s\n",
			ui.Tick(),
			ui.SaffronBold(result.RepoName),
			ui.Muted(result.ClonedPath),
		)
	}

	return result, nil
}

func resolveDuplicate(destDir, repoName string) (git.DuplicateAction, error) {
	targetPath := filepath.Join(destDir, repoName)

	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		return git.DuplicateReplace, nil
	}

	opts := []ui.SelectorOption{
		{
			Label:       "Replace it",
			Description: "Delete the existing folder and clone fresh",
			Value:       "replace",
		},
		{
			Label:       "Use existing",
			Description: "Skip clone and use the folder as-is",
			Value:       "skip",
		},
	}

	fmt.Printf("\n  %s  Directory %s already exists.\n\n",
		ui.Warn("!"), ui.SaffronBold(repoName))

	result, err := ui.RunSelector("What would you like to do?", opts)
	if err != nil {
		return git.DuplicateSkip, err
	}
	if result == nil {
		return git.DuplicateSkip, fmt.Errorf("cancelled")
	}

	if result.Value == "replace" {
		return git.DuplicateReplace, nil
	}
	return git.DuplicateSkip, nil
}

func gitAuthFromEnv() *git.Auth {
	// Check env var first, then saved config
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		// Read from github config
		home, _ := os.UserHomeDir()
		if home != "" {
			// Simple read without importing github package (avoids import cycles)
			data, err := os.ReadFile(filepath.Join(home, ".cloneable", "config.json"))
			if err == nil {
				// Quick extract of token field
				s := string(data)
				if idx := indexStr(s, `"github_token"`); idx >= 0 {
					rest := s[idx+len(`"github_token"`):]
					if idx2 := indexStr(rest, `"`); idx2 >= 0 {
						rest = rest[idx2+1:]
						if idx3 := indexStr(rest, `"`); idx3 >= 0 {
							token = rest[:idx3]
						}
					}
				}
			}
		}
	}
	if token == "" {
		return nil
	}
	return &git.Auth{
		Username: "token",
		Token:    token,
	}
}

func indexStr(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
