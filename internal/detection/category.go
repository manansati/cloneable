package detection

import (
	"encoding/json"
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

	// Only promote to CategoryApp if there's real proof of an executable entry point.
	// Without this gate, repos like script collections or mixed-content projects
	// get treated as apps and receive bogus "./reponame" run commands.
	if hasAppEntryPoint(repoPath, primary) {
		return CategoryApp
	}

	// Default: unknown — the post-install summary will offer README rendering
	// instead of suggesting a broken run command.
	return CategoryUnknown
}

// hasAppEntryPoint returns true if the repo has a definitive executable entry point
// that proves it's an application (not just a collection of files).
func hasAppEntryPoint(repoPath string, tech TechType) bool {
	switch tech {
	case TechGo:
		// Go app: main.go or cmd/ directory
		_, errMain := os.Stat(filepath.Join(repoPath, "main.go"))
		_, errCmd := os.Stat(filepath.Join(repoPath, "cmd"))
		return errMain == nil || errCmd == nil
	case TechRust:
		// Rust app: src/main.rs or [[bin]] in Cargo.toml
		_, errMain := os.Stat(filepath.Join(repoPath, "src", "main.rs"))
		if errMain == nil {
			return true
		}
		data, err := os.ReadFile(filepath.Join(repoPath, "Cargo.toml"))
		if err == nil && strings.Contains(string(data), "[[bin]]") {
			return true
		}
		return false
	case TechPython:
		// Python app: main.py, app.py, __main__.py, or entry points in packaging
		for _, f := range []string{"main.py", "app.py", "__main__.py", "run.py", "cli.py"} {
			if fileExists(repoPath, f) {
				return true
			}
		}
		return pythonIsCLI(repoPath) // entry points in pyproject/setup.py count
	case TechNode:
		// Node app: has "start" or "dev" script, or "bin" field.
		// MUST have a "name" field to be considered a real application (needed for global install).
		data, err := os.ReadFile(filepath.Join(repoPath, "package.json"))
		if err == nil {
			var pkg struct {
				Name    string            `json:"name"`
				Scripts map[string]string `json:"scripts"`
				Bin     interface{}       `json:"bin"`
			}
			if json.Unmarshal(data, &pkg) == nil {
				if pkg.Name == "" {
					return false
				}
				if pkg.Bin != nil {
					return true
				}
				if pkg.Scripts != nil {
					if _, ok := pkg.Scripts["start"]; ok {
						return true
					}
					if _, ok := pkg.Scripts["dev"]; ok {
						return true
					}
				}
			}
		}
		return false
	case TechJava:
		// Java app: gradlew or pom.xml with mainClass
		if fileExists(repoPath, "gradlew") {
			return true
		}
		data, err := os.ReadFile(filepath.Join(repoPath, "pom.xml"))
		return err == nil && strings.Contains(string(data), "mainClass")
	case TechCpp, TechC:
		// C/C++ with a build system is considered an app
		return fileExists(repoPath, "CMakeLists.txt") ||
			fileExists(repoPath, "Makefile") ||
			fileExists(repoPath, "meson.build")
	case TechZig:
		return fileExists(repoPath, "build.zig")
	case TechDotnet:
		return true // .NET projects always have an entry point
	case TechHaskell:
		return true // Haskell projects always have an entry point
	case TechDocker:
		return true
	}
	return false
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
	cargoPath := filepath.Join(repoPath, "Cargo.toml")
	data, err := os.ReadFile(cargoPath)
	if err != nil {
		return false
	}
	content := string(data)

	// If it's a virtual manifest, check subdirectories (members)
	if strings.Contains(content, "[workspace]") && !strings.Contains(content, "[package]") {
		// Look for any subdirectory with a Cargo.toml that looks like a CLI
		entries, err := os.ReadDir(repoPath)
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					if rustIsCLI(filepath.Join(repoPath, entry.Name())) {
						return true
					}
				}
			}
		}
		return false
	}

	// Rust CLI: has [[bin]] section, or depends on clap/structopt
	return strings.Contains(content, "[[bin]]") ||
		strings.Contains(content, `"clap"`) ||
		strings.Contains(content, `"structopt"`) ||
		strings.Contains(content, `"argh"`)
}

func isRustVirtualManifest(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	content := string(data)
	return strings.Contains(content, "[workspace]") && !strings.Contains(content, "[package]")
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
		// Only return a build command if package.json actually has a "build" script.
		// Projects like fkill-cli don't have one and would fail with "missing script".
		if !nodeHasBuildScript(repoPath) {
			return nil
		}
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
		// Only build if there's a Rakefile with a build task
		if fileExists(repoPath, "Rakefile") {
			return []string{"bundle", "exec", "rake", "build"}
		}
		return nil
	case TechDotnet:
		// Find the specific project file to avoid MSB1011 multi-project error
		project := findDotnetProjectFile(repoPath)
		if project != "" {
			return []string{"dotnet", "build", project}
		}
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
		pm := "npm"
		if fileExists(repoPath, "yarn.lock") {
			pm = "yarn"
		} else if fileExists(repoPath, "pnpm-lock.yaml") {
			pm = "pnpm"
		}

		data, err := os.ReadFile(filepath.Join(repoPath, "package.json"))
		if err == nil {
			var pkg struct {
				Scripts map[string]string `json:"scripts"`
				Main    string            `json:"main"`
				Bin     interface{}       `json:"bin"`
			}
			if err := json.Unmarshal(data, &pkg); err == nil {
				if pkg.Scripts != nil {
					if _, ok := pkg.Scripts["start"]; ok {
						return []string{pm, "start"}
					}
					if _, ok := pkg.Scripts["dev"]; ok {
						return []string{pm, "run", "dev"}
					}
				}
				if pkg.Main != "" {
					return []string{"node", pkg.Main}
				}
				if pkg.Bin != nil {
					switch v := pkg.Bin.(type) {
					case string:
						return []string{"node", v}
					case map[string]interface{}:
						for _, script := range v {
							if str, ok := script.(string); ok {
								return []string{"node", str}
							}
						}
					}
				}
			}
		}

		if fileExists(repoPath, "index.js") {
			return []string{"node", "index.js"}
		}
		
		// If absolutely nothing is found, return empty so we don't suggest a broken command
		return nil
	case TechPython:
		// Try common entry points in order
		for _, entry := range []string{"main.py", "app.py", "run.py", "cli.py", "__main__.py"} {
			if fileExists(repoPath, entry) {
				return []string{"python", entry}
			}
		}
		repoName := filepath.Base(repoPath)
		if category == CategoryCLI {
			// For CLI tools installed via pip, the binary might be different from the repo name.
			names := GetPythonBinaryNames(repoPath, repoName)
			return []string{names[0]}
		}
		
		names := GetPythonBinaryNames(repoPath, repoName)
		// Fallback to running it as a module
		return []string{"python", "-m", names[0]}
	case TechJava:
		if fileExists(repoPath, "gradlew") {
			return []string{"./gradlew", "run"}
		}
		if fileExists(repoPath, "mvnw") {
			return []string{"./mvnw", "exec:java"}
		}
		// Error 3: Only use exec:java if pom.xml has mainClass configured
		// Archetype/parent-only repos don't have a main class and will fail.
		if fileExists(repoPath, "pom.xml") {
			data, _ := os.ReadFile(filepath.Join(repoPath, "pom.xml"))
			if data != nil && (strings.Contains(string(data), "mainClass") || strings.Contains(string(data), "exec-maven-plugin")) {
				return []string{"mvn", "exec:java"}
			}
			// No main class — just package it
			return []string{"mvn", "package"}
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
		// Find the specific project file to avoid MSB1011 multi-project error
		project := findDotnetProjectFile(repoPath)
		if project != "" {
			return []string{"dotnet", "run", "--project", project}
		}
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
		if isRustVirtualManifest(filepath.Join(repoPath, "Cargo.toml")) {
			// In a workspace, try to find a member that looks like a CLI
			entries, err := os.ReadDir(repoPath)
			if err == nil {
				for _, entry := range entries {
					if entry.IsDir() && rustIsCLI(filepath.Join(repoPath, entry.Name())) {
						return []string{"cargo", "install", "--path", entry.Name()}
					}
				}
			}
			// Fallback for complex workspaces where we can't find the CLI member:
			// Run cargo build --release instead of cargo install.
			// It will be a fast no-op (since Phase III already built it), but it ensures
			// this install step succeeds so Cloneable proceeds to symlink the binary
			// from target/release to ~/.local/bin.
			return []string{"cargo", "build", "--release"}
		}
		return []string{"cargo", "install", "--path", "."}
	case TechNode:
		return []string{"npm", "install", "-g", "."}
	case TechPython:
		// Install from the venv's pip (the venv env vars will be set by the caller).
		// This is naturally PEP 668 compliant because it targets our isolated .venv
		// and MakeGlobal() will handle the symlinking.
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
		if fileExists(repoPath, "install.sh") {
			return []string{"./install.sh"}
		}
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

// nodeHasBuildScript returns true if package.json contains a "build" script.
// This prevents running `npm run build` on projects that don't have one
// (like fkill-cli), which would fail with "missing script: build".
func nodeHasBuildScript(repoPath string) bool {
	data, err := os.ReadFile(filepath.Join(repoPath, "package.json"))
	if err != nil {
		return false
	}

	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(data, &pkg); err == nil && pkg.Scripts != nil {
		_, ok := pkg.Scripts["build"]
		return ok
	}
	return false
}

// findDotnetProjectFile scans the repo root for .sln or .csproj files.
// If multiple exist, picks the one matching the repo name or the first found.
// Returns empty string if only one exists (dotnet handles it fine by itself).
func findDotnetProjectFile(repoPath string) string {
	repoName := strings.ToLower(filepath.Base(repoPath))

	for _, ext := range []string{".sln", ".csproj", ".fsproj"} {
		var matches []string
		entries, err := os.ReadDir(repoPath)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), ext) {
				matches = append(matches, entry.Name())
			}
		}
		// Only specify the file if there are multiple (single file works fine without specifying)
		if len(matches) > 1 {
			// Pick the one matching repo name
			for _, f := range matches {
				base := strings.ToLower(strings.TrimSuffix(f, ext))
				if base == repoName || strings.Contains(repoName, base) || strings.Contains(base, repoName) {
					return f
				}
			}
			return matches[0]
		}
	}
	return ""
}
