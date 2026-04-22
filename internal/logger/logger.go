// Package logger handles all verbose output from Cloneable's install phases.
// Every noisy message (package manager output, compiler warnings, build steps)
// goes here — written to install.logs inside the repo. The UI never sees it.
package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const LogFileName = "install.logs"

// Logger writes structured log entries to install.logs inside the repo.
type Logger struct {
	file     *os.File
	repoPath string
	LogPath  string
}

// New creates a new Logger for the given repo path.
// Creates (or appends to) install.logs in the repo root.
func New(repoPath string) (*Logger, error) {
	logPath := filepath.Join(repoPath, LogFileName)

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("could not open log file %s: %w", logPath, err)
	}

	l := &Logger{
		file:     f,
		repoPath: repoPath,
		LogPath:  logPath,
	}

	// Write a session header
	l.writeRaw(fmt.Sprintf(
		"\n══════════════════════════════════════════════\n"+
			"  Cloneable install session — %s\n"+
			"══════════════════════════════════════════════\n",
		time.Now().Format("2006-01-02 15:04:05"),
	))

	return l, nil
}

// Write logs a single line. This is the LogWriter function signature
// used across pkgmanager and env packages.
func (l *Logger) Write(line string) {
	if l == nil || l.file == nil {
		return
	}
	l.writeRaw(fmt.Sprintf("[%s] %s\n", time.Now().Format("15:04:05"), line))
}

// Section writes a clearly visible section header to the log.
func (l *Logger) Section(name string) {
	l.writeRaw(fmt.Sprintf("\n── %s ──────────────────────────────\n", name))
}

// Error writes an error entry to the log.
func (l *Logger) Error(err error) {
	l.writeRaw(fmt.Sprintf("[ERROR] %s\n", err.Error()))
}

// Close flushes and closes the log file.
func (l *Logger) Close() {
	if l.file != nil {
		l.file.Close()
	}
}

// Writer returns the LogWriter function for use with pkgmanager and env packages.
func (l *Logger) Writer() func(string) {
	return l.Write
}

// writeRaw writes directly to the file with no formatting.
func (l *Logger) writeRaw(s string) {
	if l.file != nil {
		l.file.WriteString(s) //nolint:errcheck
	}
}

// ReadAll reads and returns the full contents of install.logs.
// Used by `cloneable --logs`.
func ReadAll(repoPath string) (string, error) {
	logPath := filepath.Join(repoPath, LogFileName)
	data, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no install.logs found in %s\n  Run cloneable first to generate logs", repoPath)
		}
		return "", err
	}
	return string(data), nil
}

// NewRaw creates a Logger writing to an arbitrary file path (not necessarily inside a repo).
// Used for pre-clone logging where the repo dir doesn't exist yet.
func NewRaw(path string) (*Logger, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	return &Logger{file: f, LogPath: path}, nil
}
