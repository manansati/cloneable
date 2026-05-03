package env

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/manansati/cloneable/internal/detection"
	"github.com/manansati/cloneable/internal/pkgmanager"
)

// ToolchainPaths maps each technology to the well-known directories where its
// binaries are typically installed. When a toolchain binary isn't in $PATH
// (e.g. the user installed Rust but hasn't restarted their terminal),
// we probe these locations and prepend the matching directory to $PATH.
//
// This solves the "cargo not found" class of errors — the binary exists on
// disk but the shell hasn't picked it up yet.
var ToolchainPaths = map[detection.TechType]func() []string{
	detection.TechRust: rustToolchainDirs,
	detection.TechGo:   goToolchainDirs,
	detection.TechNode: nodeToolchainDirs,
	detection.TechZig:  zigToolchainDirs,
	detection.TechPython: pythonToolchainDirs,
	detection.TechJava:   javaToolchainDirs,
	detection.TechFlutter: flutterToolchainDirs,
	detection.TechDart:    dartToolchainDirs,
	detection.TechHaskell: haskellToolchainDirs,
	detection.TechDotnet:  dotnetToolchainDirs,
	detection.TechRuby:    rubyToolchainDirs,
}

// toolchainBinaries maps each technology to the primary binary name(s) that
// must be available before language-level operations can proceed.
var toolchainBinaries = map[detection.TechType][]string{
	detection.TechRust:    {"cargo", "rustc"},
	detection.TechGo:      {"go"},
	detection.TechNode:    {"node", "npm"},
	detection.TechZig:     {"zig"},
	detection.TechPython:  {"python3"},
	detection.TechJava:    {"java", "javac"},
	detection.TechFlutter: {"flutter"},
	detection.TechDart:    {"dart"},
	detection.TechHaskell: {"ghc"},
	detection.TechDotnet:  {"dotnet"},
	detection.TechRuby:    {"ruby"},
	detection.TechCpp:     {"gcc", "cmake"},
	detection.TechC:       {"gcc", "cmake"},
}

// companionTool describes an additional binary that must be present for a technology.
type companionTool struct {
	Binary   string            // e.g. "bundle"
	Packages map[string]string // manager → package name
}

// companionTools maps each tech to additional binaries that must be present
// beyond the primary toolchain binaries.
var companionTools = map[detection.TechType][]companionTool{
	detection.TechRuby: {
		{Binary: "bundle", Packages: map[string]string{
			"pacman": "ruby-bundler", "apt": "ruby-bundler", "dnf": "rubygem-bundler",
			"zypper": "ruby-bundler", "apk": "ruby-bundler", "brew": "ruby",
		}},
		{Binary: "gem", Packages: map[string]string{
			"pacman": "rubygems", "apt": "rubygems", "dnf": "rubygems",
			"zypper": "ruby", "apk": "ruby", "brew": "ruby",
		}},
	},
	detection.TechCpp: {
		{Binary: "cmake", Packages: map[string]string{
			"pacman": "cmake", "apt": "cmake", "dnf": "cmake",
			"zypper": "cmake", "apk": "cmake", "brew": "cmake",
		}},
		{Binary: "make", Packages: map[string]string{
			"pacman": "base-devel", "apt": "build-essential", "dnf": "make",
			"zypper": "make", "apk": "make", "brew": "make",
		}},
		{Binary: "pkg-config", Packages: map[string]string{
			"pacman": "pkgconf", "apt": "pkg-config", "dnf": "pkgconf-pkg-config",
			"zypper": "pkg-config", "apk": "pkgconf", "brew": "pkg-config",
		}},
	},
	detection.TechC: {
		{Binary: "cmake", Packages: map[string]string{
			"pacman": "cmake", "apt": "cmake", "dnf": "cmake",
			"zypper": "cmake", "apk": "cmake", "brew": "cmake",
		}},
		{Binary: "make", Packages: map[string]string{
			"pacman": "base-devel", "apt": "build-essential", "dnf": "make",
			"zypper": "make", "apk": "make", "brew": "make",
		}},
	},
	detection.TechNode: {
		{Binary: "npx", Packages: map[string]string{
			"pacman": "npm", "apt": "npm", "dnf": "npm",
			"zypper": "npm", "apk": "npm", "brew": "node",
		}},
	},
	detection.TechPython: {
		{Binary: "pip3", Packages: map[string]string{
			"pacman": "python-pip", "apt": "python3-pip", "dnf": "python3-pip",
			"zypper": "python3-pip", "apk": "py3-pip", "brew": "python",
		}},
	},
	detection.TechHaskell: {
		{Binary: "cabal", Packages: map[string]string{
			"pacman": "cabal-install", "apt": "cabal-install", "dnf": "cabal-install",
			"zypper": "cabal-install", "apk": "cabal", "brew": "cabal-install",
		}},
		{Binary: "stack", Packages: map[string]string{
			"pacman": "stack", "apt": "haskell-stack", "dnf": "stack",
			"brew": "haskell-stack",
		}},
	},
}

// EnsureToolchain verifies that the required toolchain binaries for the given
// tech are available. If a binary isn't in $PATH, it probes well-known install
// directories and prepends them to $PATH. If the toolchain is completely missing,
// it attempts to install it via the system package manager.
//
// This is called BEFORE any language-level dependency installation or build
// commands. It is the first line of defense against "command not found" errors.
func (e *Environment) EnsureToolchain(log LogWriter, cascade ...*pkgmanager.Cascade) error {
	var cas *pkgmanager.Cascade
	if len(cascade) > 0 {
		cas = cascade[0]
	}

	bins, ok := toolchainBinaries[e.Tech]
	if !ok {
		return nil // No specific toolchain needed
	}

	for _, bin := range bins {
		if binaryExists(bin) {
			continue // Already in PATH
		}

		if log != nil {
			log(fmt.Sprintf("[toolchain] %q not found in PATH — probing known locations", bin))
		}

		// Probe well-known directories
		found := false
		if dirFn, ok := ToolchainPaths[e.Tech]; ok {
			for _, dir := range dirFn() {
				candidate := filepath.Join(dir, bin)
				if runtime.GOOS == "windows" {
					candidate += ".exe"
				}
				if _, err := os.Stat(candidate); err == nil {
					// Found it! Prepend this directory to PATH
					prependToPath(dir)
					if log != nil {
						log(fmt.Sprintf("[toolchain] found %s at %s — added to PATH", bin, dir))
					}
					found = true
					break
				}
			}
		}

		if found {
			continue
		}

		// Binary not found anywhere — attempt auto-install
		if log != nil {
			log(fmt.Sprintf("[toolchain] %q not found anywhere — attempting auto-install", bin))
		}
		if err := autoInstallToolchain(e.Tech, bin, log, cas); err != nil {
			if log != nil {
				log(fmt.Sprintf("[toolchain] auto-install failed: %v", err))
			}
			// Don't return error — the build step will give a better error message
			continue
		}

		// Re-probe after install
		if dirFn, ok := ToolchainPaths[e.Tech]; ok {
			for _, dir := range dirFn() {
				candidate := filepath.Join(dir, bin)
				if runtime.GOOS == "windows" {
					candidate += ".exe"
				}
				if _, err := os.Stat(candidate); err == nil {
					prependToPath(dir)
					if log != nil {
						log(fmt.Sprintf("[toolchain] after install, found %s at %s", bin, dir))
					}
					break
				}
			}
		}

		// Also check if it's now directly in PATH (some package managers add to PATH)
		if !binaryExists(bin) {
			// Try common system paths directly
			for _, sysDir := range []string{"/usr/bin", "/usr/local/bin"} {
				candidate := filepath.Join(sysDir, bin)
				if _, err := os.Stat(candidate); err == nil {
					prependToPath(sysDir)
					if log != nil {
						log(fmt.Sprintf("[toolchain] found %s at %s after install", bin, sysDir))
					}
					break
				}
			}
		}
	}

	// Install companion tools (e.g., bundler for Ruby, cmake for C/C++)
	if companions, ok := companionTools[e.Tech]; ok {
		for _, comp := range companions {
			if binaryExists(comp.Binary) {
				continue
			}
			if log != nil {
				log(fmt.Sprintf("[toolchain] companion tool %q not found — installing", comp.Binary))
			}
			if cas != nil {
				managerName := cas.PrimaryName()
				if pkg, ok := comp.Packages[managerName]; ok && pkg != "" {
					var logFn pkgmanager.LogWriter
					if log != nil {
						logFn = pkgmanager.LogWriter(log)
					}
					_ = cas.Install(pkg, logFn)
				}
			} else {
				// Fallback: try gem install for bundler specifically
				if comp.Binary == "bundle" && binaryExists("gem") {
					_ = runCmd(log, "gem", "install", "bundler")
				}
			}
		}
	}

	return nil
}

// prependToPath adds a directory to the front of $PATH for the current process.
func prependToPath(dir string) {
	current := os.Getenv("PATH")
	// Don't add if already present
	sep := string(os.PathListSeparator)
	for _, p := range strings.Split(current, sep) {
		if filepath.Clean(p) == filepath.Clean(dir) {
			return
		}
	}
	os.Setenv("PATH", dir+sep+current)
}

// autoInstallToolchain attempts to install a missing language toolchain
// using the system package manager or official installer scripts.
func autoInstallToolchain(tech detection.TechType, bin string, log LogWriter, cascade *pkgmanager.Cascade) error {
	// Map of tech → package names for each package manager
	toolchainPackages := map[detection.TechType]map[string]string{
		detection.TechGo: {
			"pacman": "go", "apt": "golang", "dnf": "golang",
			"zypper": "go", "apk": "go", "brew": "go",
		},
		detection.TechNode: {
			"pacman": "nodejs", "apt": "nodejs", "dnf": "nodejs",
			"zypper": "nodejs", "apk": "nodejs", "brew": "node",
		},
		detection.TechPython: {
			"pacman": "python", "apt": "python3", "dnf": "python3",
			"zypper": "python3", "apk": "python3", "brew": "python",
		},
		detection.TechRuby: {
			"pacman": "ruby", "apt": "ruby-full", "dnf": "ruby",
			"zypper": "ruby", "apk": "ruby", "brew": "ruby",
		},
		detection.TechJava: {
			"pacman": "jdk-openjdk", "apt": "default-jdk", "dnf": "java-17-openjdk-devel",
			"zypper": "java-17-openjdk-devel", "apk": "openjdk17", "brew": "openjdk",
		},
		detection.TechCpp: {
			"pacman": "base-devel", "apt": "build-essential", "dnf": "gcc-c++",
			"zypper": "gcc-c++", "apk": "build-base", "brew": "gcc",
		},
		detection.TechC: {
			"pacman": "base-devel", "apt": "build-essential", "dnf": "gcc",
			"zypper": "gcc", "apk": "build-base", "brew": "gcc",
		},
		detection.TechZig: {
			"pacman": "zig", "apt": "zig", "dnf": "zig",
			"brew": "zig",
		},
		detection.TechHaskell: {
			"pacman": "ghc", "apt": "ghc", "dnf": "ghc",
			"zypper": "ghc", "apk": "ghc", "brew": "ghc",
		},
		detection.TechDotnet: {
			"pacman": "dotnet-sdk", "apt": "dotnet-sdk-8.0", "dnf": "dotnet-sdk-8.0",
			"zypper": "dotnet-sdk-8.0", "brew": "dotnet",
		},
		detection.TechFlutter: {
			"pacman": "flutter", "brew": "flutter",
		},
		detection.TechDart: {
			"pacman": "dart", "apt": "dart", "brew": "dart",
		},
	}

	// Special case: Rust uses rustup, not the package manager
	if tech == detection.TechRust {
		return installRustToolchain(log)
	}

	// Special case: Node.js — also install npm separately on some distros
	if tech == detection.TechNode && bin == "npm" {
		if cascade != nil {
			var logFn pkgmanager.LogWriter
			if log != nil {
				logFn = pkgmanager.LogWriter(log)
			}
			managerName := cascade.PrimaryName()
			npmPkg := pkgmanager.ResolvePackageName("npm", managerName)
			_ = cascade.Install(npmPkg, logFn)
		}
		return nil
	}

	// Try the package manager cascade first
	if cascade != nil {
		pkgs, ok := toolchainPackages[tech]
		if ok {
			managerName := cascade.PrimaryName()
			var logFn pkgmanager.LogWriter
			if log != nil {
				logFn = pkgmanager.LogWriter(log)
			}

			if pkg, ok := pkgs[managerName]; ok && pkg != "" {
				if log != nil {
					log(fmt.Sprintf("[toolchain] installing %s via %s (%s)", tech, managerName, pkg))
				}
				err := cascade.Install(pkg, logFn)
				if err == nil {
					return nil
				}
				if log != nil {
					log(fmt.Sprintf("[toolchain] %s install via %s failed: %v", pkg, managerName, err))
				}
			}
		}
	}

	// Fallback: try installing via generic resolved name
	if cascade != nil {
		var logFn pkgmanager.LogWriter
		if log != nil {
			logFn = pkgmanager.LogWriter(log)
		}
		resolved := pkgmanager.ResolvePackageName(bin, cascade.PrimaryName())
		if err := cascade.Install(resolved, logFn); err == nil {
			return nil
		}
	}

	return fmt.Errorf("could not auto-install %s for %s — please install manually", bin, tech)
}

// installRustToolchain installs Rust via rustup (the official installer).
func installRustToolchain(log LogWriter) error {
	if runtime.GOOS == "windows" {
		// On Windows, rustup-init.exe is the way
		return fmt.Errorf("please install Rust from https://rustup.rs")
	}

	if log != nil {
		log("[toolchain] installing Rust via rustup...")
	}

	// curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
	cmd := exec.Command("sh", "-c",
		"curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain stable")
	out, err := cmd.CombinedOutput()
	if log != nil && len(out) > 0 {
		for _, line := range strings.Split(string(out), "\n") {
			if strings.TrimSpace(line) != "" {
				log("[rustup] " + line)
			}
		}
	}
	if err != nil {
		return fmt.Errorf("rustup install failed: %w", err)
	}

	// Add cargo bin to PATH immediately
	home, _ := os.UserHomeDir()
	cargoBin := filepath.Join(home, ".cargo", "bin")
	prependToPath(cargoBin)

	return nil
}

// ── Well-known directory providers ────────────────────────────────────────────

func rustToolchainDirs() []string {
	home, _ := os.UserHomeDir()
	dirs := []string{
		filepath.Join(home, ".cargo", "bin"),
	}
	// Also scan rustup toolchain directories
	rustupDir := filepath.Join(home, ".rustup", "toolchains")
	entries, err := os.ReadDir(rustupDir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() && strings.HasPrefix(e.Name(), "stable") {
				dirs = append(dirs, filepath.Join(rustupDir, e.Name(), "bin"))
			}
		}
	}
	// System-wide fallbacks
	dirs = append(dirs,
		"/usr/local/bin",
		"/usr/bin",
	)
	return dirs
}

func goToolchainDirs() []string {
	home, _ := os.UserHomeDir()
	dirs := []string{
		filepath.Join(home, "go", "bin"),
		"/usr/local/go/bin",
		"/snap/go/current/bin",
	}
	if goroot := os.Getenv("GOROOT"); goroot != "" {
		dirs = append([]string{filepath.Join(goroot, "bin")}, dirs...)
	}
	dirs = append(dirs, "/usr/local/bin", "/usr/bin")
	return dirs
}

func nodeToolchainDirs() []string {
	home, _ := os.UserHomeDir()
	dirs := []string{}

	// nvm — scan for installed versions (newest first)
	nvmDir := filepath.Join(home, ".nvm", "versions", "node")
	entries, err := os.ReadDir(nvmDir)
	if err == nil {
		// Iterate in reverse to prefer newer versions
		for i := len(entries) - 1; i >= 0; i-- {
			if entries[i].IsDir() {
				dirs = append(dirs, filepath.Join(nvmDir, entries[i].Name(), "bin"))
			}
		}
	}

	// fnm
	fnmDir := filepath.Join(home, ".fnm", "node-versions")
	fnmEntries, err := os.ReadDir(fnmDir)
	if err == nil {
		for i := len(fnmEntries) - 1; i >= 0; i-- {
			if fnmEntries[i].IsDir() {
				dirs = append(dirs, filepath.Join(fnmDir, fnmEntries[i].Name(), "installation", "bin"))
			}
		}
	}

	dirs = append(dirs,
		"/usr/local/bin",
		"/usr/bin",
		filepath.Join(home, ".local", "bin"),
	)
	return dirs
}

func zigToolchainDirs() []string {
	home, _ := os.UserHomeDir()
	return []string{
		filepath.Join(home, "zig"),
		filepath.Join(home, ".local", "bin"),
		"/usr/local/bin",
		"/usr/bin",
		"/snap/zig/current",
	}
}

func pythonToolchainDirs() []string {
	home, _ := os.UserHomeDir()
	dirs := []string{
		filepath.Join(home, ".pyenv", "shims"),
		filepath.Join(home, ".local", "bin"),
	}
	// conda
	condaDir := filepath.Join(home, "miniconda3", "bin")
	if _, err := os.Stat(condaDir); err == nil {
		dirs = append(dirs, condaDir)
	}
	condaDir2 := filepath.Join(home, "anaconda3", "bin")
	if _, err := os.Stat(condaDir2); err == nil {
		dirs = append(dirs, condaDir2)
	}
	dirs = append(dirs, "/usr/local/bin", "/usr/bin")
	return dirs
}

func javaToolchainDirs() []string {
	dirs := []string{}
	if javaHome := os.Getenv("JAVA_HOME"); javaHome != "" {
		dirs = append(dirs, filepath.Join(javaHome, "bin"))
	}
	// Scan /usr/lib/jvm for installed JDKs
	jvmDir := "/usr/lib/jvm"
	entries, err := os.ReadDir(jvmDir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() {
				dirs = append(dirs, filepath.Join(jvmDir, e.Name(), "bin"))
			}
		}
	}
	dirs = append(dirs, "/usr/local/bin", "/usr/bin")
	return dirs
}

func flutterToolchainDirs() []string {
	home, _ := os.UserHomeDir()
	return []string{
		filepath.Join(home, "flutter", "bin"),
		filepath.Join(home, ".flutter", "bin"),
		"/opt/flutter/bin",
		"/usr/local/flutter/bin",
		filepath.Join(home, "snap", "flutter", "common", "flutter", "bin"),
		"/snap/flutter/current/bin",
		"/usr/local/bin",
		"/usr/bin",
	}
}

func dartToolchainDirs() []string {
	home, _ := os.UserHomeDir()
	return []string{
		filepath.Join(home, ".pub-cache", "bin"),
		"/usr/lib/dart/bin",
		"/usr/local/bin",
		"/usr/bin",
	}
}

func haskellToolchainDirs() []string {
	home, _ := os.UserHomeDir()
	return []string{
		filepath.Join(home, ".ghcup", "bin"),
		filepath.Join(home, ".cabal", "bin"),
		filepath.Join(home, ".local", "bin"),
		"/usr/local/bin",
		"/usr/bin",
	}
}

func dotnetToolchainDirs() []string {
	home, _ := os.UserHomeDir()
	return []string{
		filepath.Join(home, ".dotnet"),
		filepath.Join(home, ".dotnet", "tools"),
		"/usr/local/bin",
		"/usr/bin",
		"/usr/share/dotnet",
	}
}

func rubyToolchainDirs() []string {
	home, _ := os.UserHomeDir()
	dirs := []string{
		filepath.Join(home, ".rbenv", "shims"),
		filepath.Join(home, ".rvm", "rubies", "default", "bin"),
	}
	// Scan .rbenv/versions
	rbenvDir := filepath.Join(home, ".rbenv", "versions")
	entries, err := os.ReadDir(rbenvDir)
	if err == nil {
		for i := len(entries) - 1; i >= 0; i-- {
			if entries[i].IsDir() {
				dirs = append(dirs, filepath.Join(rbenvDir, entries[i].Name(), "bin"))
			}
		}
	}
	dirs = append(dirs, "/usr/local/bin", "/usr/bin")
	return dirs
}
