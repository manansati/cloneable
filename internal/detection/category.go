package detection

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// RepoCategory describes what kind of repository this is.
type RepoCategory string

const (
	CategoryCLI      RepoCategory = "CLI Tool"      // Command-line tool (takes args, has --help)
	CategoryTUI      RepoCategory = "TUI Tool"       // Terminal UI (fullscreen terminal app)
	CategoryApp      RepoCategory = "Application"    // GUI application
	CategoryLibrary  RepoCategory = "Library"        // Library / framework (not directly runnable)
	CategoryDotfiles RepoCategory = "Dotfiles"       // Configuration files
	CategoryDocs     RepoCategory = "Documentation"  // Documentation / markdown only
	CategoryScripts  RepoCategory = "Scripts"        // Shell scripts / automation
	CategoryDocker   RepoCategory = "Docker"         // Docker-based project
	CategoryUnknown  RepoCategory = "Unknown"
)

// categorySignals maps filename patterns to repo categories.
// Checked after tech detection to refine what kind of project this is.
var categorySignals = []struct {
	file     string
	category RepoCategory
}{
	// Docker first — if docker-compose exists, treat as docker project
	{"docker-compose.yml", CategoryDocker},
	{"docker-compose.yaml", CategoryDocker},

	// Dotfile managers
	{".stow-local-ignore", CategoryDotfiles},
	{".chezmoi.yaml", CategoryDotfiles},
	{".chezmoi.toml", CategoryDotfiles},
	{"install.sh", CategoryDotfiles},   // common in dotfile repos — weighted with others
	{"setup.sh", CategoryDotfiles},
}

// DetermineCategory decides what category the repo falls into.
// It combines tech type, manifest signals, and directory structure.
func DetermineCategory(repoPath string, primary TechType, manifests []FoundManifest) RepoCategory {
	// Check for dotfile repo first (structure-based)
	if isDotfileRepo(repoPath) {
		return CategoryDotfiles
	}

	// Check for docs repo
	hasBuildable := primary != TechUnknown && primary != TechDocs
	if isDocsRepo(repoPath, hasBuildable) {
		return CategoryDocs
	}

	// Check for Docker — if docker-compose is present, that's the launch strategy
	for _, m := range manifests {
		if m.Entry.Tech == TechDocker && m.Entry.Confidence >= 80 {
			return CategoryDocker
		}
	}

	// Per-tech category heuristics
	switch primary {
	case TechDotfile:
		return CategoryDotfiles
	case TechDocs:
		return CategoryDocs
	case TechScripts:
		return CategoryScripts
	}

	// Look for CLI/TUI signals in package.json, Cargo.toml, go.mod, etc.
	if isCLIProject(repoPath, primary) {
		return CategoryCLI
	}
	if isLibraryProject(repoPath, primary) {
		return CategoryLibrary
	}

	// Default: treat as an app
	return CategoryApp
}

// isCLIProject returns true if the repo appears to be a CLI/TUI tool.
// Detection is per-tech and reads the actual manifest files.
func isCLIProject(repoPath string, tech TechType) bool {
	switch tech {
	case TechNode:
		return nodeIsCLI(repoPath)
	case TechRust:
		return rustIsCLI(repoPath)
	case TechGo:
		return goIsCLI(repoPath)
	case TechPython:
		return pythonIsCLI(repoPath)
	}
	return false
}

// isLibraryProject returns true if the repo is a library, not an app.
func isLibraryProject(repoPath string, tech TechType) bool {
	switch tech {
	case TechNode:
		return nodeIsLibrary(repoPath)
	case TechRust:
		return rustIsLibrary(repoPath)
	case TechGo:
		return goIsLibrary(repoPath)
	}
	return false
}

// ── Per-tech CLI/library detection ───────────────────────────────────────────

func nodeIsCLI(repoPath string) bool {
	data, err := os.ReadFile(filepath.Join(repoPath, "package.json"))
	if err != nil {
		return false
	}
	content := string(data)
	// CLI indicators in package.json
	return strings.Contains(content, `"bin"`) ||
		strings.Contains(content, `"cli"`) ||
		strings.Contains(content, `"commander"`) ||
		strings.Contains(content, `"yargs"`) ||
		strings.Contains(content, `"meow"`)
}

func nodeIsLibrary(repoPath string) bool {
	data, err := os.ReadFile(filepath.Join(repoPath, "package.json"))
	if err != nil {
		return false
	}
	content := string(data)
	// Library indicators
	return strings.Contains(content, `"main"`) &&
		!strings.Contains(content, `"start"`) &&
		!strings.Contains(content, `"bin"`)
}

func rustIsCLI(repoPath string) bool {
	data, err := os.ReadFile(filepath.Join(repoPath, "Cargo.toml"))
	if err != nil {
		return false
	}
	content := string(data)
	// Rust CLI: has [[bin]] section, or depends on clap/structopt
	return strings.Contains(content, "[[bin]]") ||
		strings.Contains(content, `"clap"`) ||
		strings.Contains(content, `"structopt"`) ||
		strings.Contains(content, `"argh"`)
}

func rustIsLibrary(repoPath string) bool {
	// If src/lib.rs exists and no src/main.rs, it's a library
	_, hasLib := os.Stat(filepath.Join(repoPath, "src", "lib.rs"))
	_, hasMain := os.Stat(filepath.Join(repoPath, "src", "main.rs"))
	return hasLib == nil && os.IsNotExist(hasMain)
}

func goIsCLI(repoPath string) bool {
	// Go CLI tools have main.go in root or cmd/ directory
	_, hasMain := os.Stat(filepath.Join(repoPath, "main.go"))
	_, hasCmd := os.Stat(filepath.Join(repoPath, "cmd"))
	return hasMain == nil || hasCmd == nil
}

func goIsLibrary(repoPath string) bool {
	// Pure library: no main.go, no cmd/ directory
	_, hasMain := os.Stat(filepath.Join(repoPath, "main.go"))
	_, hasCmd := os.Stat(filepath.Join(repoPath, "cmd"))
	return os.IsNotExist(hasMain) && os.IsNotExist(hasCmd)
}

func pythonIsCLI(repoPath string) bool {
	// Check for __main__.py, setup.py entry_points, or pyproject scripts
	_, hasMain := os.Stat(filepath.Join(repoPath, "__main__.py"))
	if hasMain == nil {
		return true
	}
	data, err := os.ReadFile(filepath.Join(repoPath, "pyproject.toml"))
	if err == nil && (strings.Contains(string(data), "[tool.poetry.scripts]") ||
		strings.Contains(string(data), "[project.scripts]")) {
		return true
	}
	data, err = os.ReadFile(filepath.Join(repoPath, "setup.py"))
	if err == nil && strings.Contains(string(data), "entry_points") {
		return true
	}
	return false
}

// ── Build & Run command determination ────────────────────────────────────────

// localPrefix returns ~/.local for the install prefix on Unix,
// or %USERPROFILE%\.cloneable on Windows. Used to avoid needing sudo.
func localPrefix() string {
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		return filepath.Join(home, ".cloneable")
	}
	return filepath.Join(home, ".local")
}

// BuildCommand returns the best build command for the given tech in the repo.
func BuildCommand(repoPath string, tech TechType) []string {
	switch tech {
	case TechGo:
		// Build all packages; `go install ./...` will place binaries in GOPATH/bin
		return []string{"go", "build", "-v", "./..."}
	case TechRust:
		return []string{"cargo", "build", "--release"}
	case TechNode:
		if fileExists(repoPath, "yarn.lock") {
			return []string{"yarn", "build"}
		}
		if fileExists(repoPath, "pnpm-lock.yaml") {
			return []string{"pnpm", "build"}
		}
		return []string{"npm", "run", "build"}
	case TechPython:
		return []string{"pip", "install", "-e", "."}
	case TechJava:
		if fileExists(repoPath, "gradlew") {
			return []string{"./gradlew", "build"}
		}
		if fileExists(repoPath, "mvnw") {
			return []string{"./mvnw", "package"}
		}
		if fileExists(repoPath, "build.gradle") || fileExists(repoPath, "build.gradle.kts") {
			return []string{"gradle", "build"}
		}
		return []string{"mvn", "package"}
	case TechCpp:
		// The actual build logic is in launch.go's buildCpp which handles the
		// full configure+build two-step. This just provides the "build" command
		// for the profile. buildCpp() ignores this and does its own thing.
		if fileExists(repoPath, "CMakeLists.txt") {
			return []string{"cmake", "--build", "build", "--parallel"}
		}
		if fileExists(repoPath, "meson.build") {
			return []string{"meson", "compile", "-C", "build"}
		}
		return []string{"make", "-j4"}
	case TechC:
		if fileExists(repoPath, "CMakeLists.txt") {
			return []string{"cmake", "--build", "build", "--parallel"}
		}
		return []string{"make", "-j4"}
	case TechZig:
		return []string{"zig", "build"}
	case TechFlutter:
		return []string{"flutter", "build"}
	case TechRuby:
		return []string{"bundle", "exec", "rake", "build"}
	case TechDotnet:
		return []string{"dotnet", "build"}
	case TechHaskell:
		if fileExists(repoPath, "stack.yaml") {
			return []string{"stack", "build"}
		}
		return []string{"cabal", "build"}
	}
	return nil
}

// RunCommand returns the best run command for the given tech in the repo.
func RunCommand(repoPath string, tech TechType, category RepoCategory) []string {
	switch tech {
	case TechGo:
		return []string{"go", "run", "."}
	case TechRust:
		return []string{"cargo", "run", "--release"}
	case TechNode:
		if fileExists(repoPath, "yarn.lock") {
			return []string{"yarn", "start"}
		}
		if fileExists(repoPath, "pnpm-lock.yaml") {
			return []string{"pnpm", "start"}
		}
		return []string{"npm", "start"}
	case TechPython:
		// Try common entry points in order
		for _, entry := range []string{"main.py", "app.py", "run.py", "cli.py", "__main__.py"} {
			if fileExists(repoPath, entry) {
				return []string{"python", entry}
			}
		}
		return []string{"python", "-m", "."}
	case TechJava:
		if fileExists(repoPath, "gradlew") {
			return []string{"./gradlew", "run"}
		}
		return []string{"mvn", "exec:java"}
	case TechCpp, TechC:
		// After build, the binary name matches the repo name.
		// launch.go's FindBinary handles locating it — RunCommand is unused for native compiled.
		return nil
	case TechZig:
		// Same — binary is found by name after install. No "zig build run" ever.
		return nil
	case TechFlutter:
		return []string{"flutter", "run"}
	case TechDart:
		return []string{"dart", "run"}
	case TechRuby:
		if fileExists(repoPath, "bin/rails") {
			return []string{"bin/rails", "server"}
		}
		return []string{"bundle", "exec", "ruby", "main.rb"}
	case TechDotnet:
		return []string{"dotnet", "run"}
	case TechHaskell:
		if fileExists(repoPath, "stack.yaml") {
			return []string{"stack", "run"}
		}
		return []string{"cabal", "run"}
	case TechDocker:
		// Modern Docker uses `docker compose` (plugin), fallback to `docker-compose` (standalone)
		if fileExists(repoPath, "docker-compose.yml") || fileExists(repoPath, "docker-compose.yaml") {
			return []string{"docker", "compose", "up"}
		}
		return []string{"docker", "build", "-t", "app", ".", "&&", "docker", "run", "app"}
	}
	return nil
}

// InstallGlobalCommand returns the command that installs the binary globally.
// Where possible, installs to ~/.local (no root needed) instead of /usr/local.
func InstallGlobalCommand(repoPath string, tech TechType) []string {
	prefix := localPrefix()

	switch tech {
	case TechGo:
		return []string{"go", "install", "./..."}
	case TechRust:
		return []string{"cargo", "install", "--path", "."}
	case TechNode:
		return []string{"npm", "install", "-g", "."}
	case TechPython:
		// Install from the venv's pip (the venv env vars will be set by the caller).
		return []string{"pip", "install", "."}
	case TechCpp, TechC:
		if fileExists(repoPath, "CMakeLists.txt") {
			return []string{"cmake", "--install", "build", "--prefix", prefix}
		}
		if fileExists(repoPath, "meson.build") {
			return []string{"meson", "install", "-C", "build"}
		}
		// Plain makefile — PREFIX tells make where to install
		return []string{"make", "install", "PREFIX=" + prefix}
	case TechZig:
		// zig build install -p ~/.local → binary lands in ~/.local/bin
		return []string{"zig", "build", "install", "-p", prefix}
	case TechHaskell:
		if fileExists(repoPath, "stack.yaml") {
			return []string{"stack", "install"}
		}
		return []string{"cabal", "install", "--overwrite-policy=always"}
	case TechDotnet:
		return []string{"dotnet", "tool", "install", "--global", "."}
	case TechScripts:
		if fileExists(repoPath, "Makefile") || fileExists(repoPath, "GNUmakefile") {
			return []string{"make", "install", "PREFIX=" + prefix}
		}
	}
	return nil
}

// fileExists is a small helper to check if a file exists relative to repoPath.
func fileExists(repoPath, rel string) bool {
	_, err := os.Stat(filepath.Join(repoPath, rel))
	return err == nil
}
