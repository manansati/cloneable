package phases

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/manansati/cloneable/internal/detection"
	"github.com/manansati/cloneable/internal/env"
	"github.com/manansati/cloneable/internal/pkgmanager"
)

// NOTE: Launching and Installing logic is split between two files:
// 1. internal/phases/install.go: Handles Phase II (Detection, Environment Setup, System/Language Dependencies)
// 2. internal/phases/launch.go: Handles Phase III (Building, Global Installation, and Launching/Running)

// LaunchContext holds everything Phase III needs.

// LaunchResult is what Phase III returns.

// RunLaunch executes Phase III: build → install globally (if chosen) → run.
// If a dependency error is encountered during run, it returns to Phase II and retries.

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

// isNativeCompiled returns true for languages that produce a standalone binary.
// For these, after `make install` / `zig build install`, the binary is in PATH
// and should be run directly — not with `zig build run` or `make run`.
func isNativeCompiled(tech detection.TechType) bool {
	switch tech {
	case detection.TechGo, detection.TechRust, detection.TechZig,
		detection.TechCpp, detection.TechC, detection.TechHaskell:
		return true
	}
	return false
}

// isInstallable returns true for repos that should offer a global install.
func isInstallable(tech detection.TechType, cat detection.RepoCategory) bool {
	if cat == detection.CategoryDotfiles || cat == detection.CategoryDocs || cat == detection.CategoryLibrary {
		return false
	}
	switch tech {
	case detection.TechDotfile, detection.TechDocs:
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
		// Pass nil cascade here — the full cascade is used when called via buildWithRetry
		return buildCpp(repoPath, log, extraEnv)

	case detection.TechZig:
		// Zig: just run `zig build` in the repo root
		return runIn(repoPath, log, extraEnv, "zig", "build")

	case detection.TechJava:
		for _, wrapper := range []string{"gradlew", "mvnw"} {
			wp := filepath.Join(repoPath, wrapper)
			if _, err := os.Stat(wp); err == nil {
				_ = os.Chmod(wp, 0755)
			}
		}
		return runIn(repoPath, log, extraEnv, profile.BuildCommands[0], profile.BuildCommands[1:]...)

	case detection.TechDotnet:
		// Find specific .sln or .csproj to avoid MSB1011 error
		project := findDotnetProjectInLaunch(repoPath)
		if project != "" {
			return runIn(repoPath, log, extraEnv, "dotnet", "build", project)
		}
		return runIn(repoPath, log, extraEnv, profile.BuildCommands[0], profile.BuildCommands[1:]...)

	default:
		return runIn(repoPath, log, extraEnv, profile.BuildCommands[0], profile.BuildCommands[1:]...)
	}
}

// findDotnetProjectInLaunch is the launch-phase version of findDotnetProject.
// It scans for .sln/.csproj files to specify to dotnet build.
func findDotnetProjectInLaunch(repoPath string) string {
	repoName := strings.ToLower(filepath.Base(repoPath))

	type extCandidate struct {
		ext string
	}
	for _, ec := range []extCandidate{{".sln"}, {".csproj"}, {".fsproj"}} {
		var matches []string
		entries, err := os.ReadDir(repoPath)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), ec.ext) {
				matches = append(matches, entry.Name())
			}
		}
		if len(matches) == 1 {
			return matches[0]
		}
		if len(matches) > 1 {
			// Pick the one matching repo name
			for _, f := range matches {
				base := strings.ToLower(strings.TrimSuffix(f, ec.ext))
				if base == repoName || strings.Contains(repoName, base) || strings.Contains(base, repoName) {
					return f
				}
			}
			return matches[0]
		}
	}
	return ""
}

// buildCpp handles the configure + build two-step for C/C++ projects.
// Configure runs here (NOT in setupCpp) because system deps must be
// installed before configure can succeed.
//
// cmake: cmake -B build -S . → cmake --build build  (with auto-retry for missing deps)
// meson: meson setup build  → meson compile -C build
// autotools: autoreconf → ./configure → make
// make: make (in repo root)
func buildCpp(repoPath string, log pkgmanager.LogWriter, extraEnv []string) error {
	return buildCppWithCascade(repoPath, log, extraEnv, nil, nil)
}

// buildCppWithCascade is the full version that accepts a package manager cascade
// for auto-installing missing CMake dependencies.
func buildCppWithCascade(repoPath string, log pkgmanager.LogWriter, extraEnv []string, cascade *pkgmanager.Cascade, osInfo *detection.OSInfo) error {
	buildDir := filepath.Join(repoPath, "build")
	home, _ := os.UserHomeDir()
	prefix := filepath.Join(home, ".local")

	// Ensure build directory exists
	_ = os.MkdirAll(buildDir, 0755)

	if fileExistsAbs(filepath.Join(repoPath, "CMakeLists.txt")) {
		return buildCMakeWithRetry(repoPath, buildDir, prefix, log, extraEnv, cascade, osInfo)
	}

	if fileExistsAbs(filepath.Join(repoPath, "meson.build")) {
		// Check if meson was already configured
		if !fileExistsAbs(filepath.Join(buildDir, "build.ninja")) {
			if err := runIn(repoPath, log, extraEnv,
				"meson", "setup", buildDir, repoPath,
				"--buildtype=release",
				fmt.Sprintf("--prefix=%s", prefix),
			); err != nil {
				return fmt.Errorf("meson setup failed: %w", err)
			}
		}
		return runIn(repoPath, log, extraEnv, "meson", "compile", "-C", buildDir)
	}

	if fileExistsAbs(filepath.Join(repoPath, "configure.ac")) && !fileExistsAbs(filepath.Join(repoPath, "configure")) {
		// Need to generate configure script first
		if err := runIn(repoPath, log, extraEnv, "autoreconf", "-fi"); err != nil {
			// Try autogen.sh as fallback
			if fileExistsAbs(filepath.Join(repoPath, "autogen.sh")) {
				_ = os.Chmod(filepath.Join(repoPath, "autogen.sh"), 0755)
				_ = runIn(repoPath, log, extraEnv, "./autogen.sh")
			}
		}
	}

	if fileExistsAbs(filepath.Join(repoPath, "configure")) {
		_ = os.Chmod(filepath.Join(repoPath, "configure"), 0755)
		if err := runIn(repoPath, log, extraEnv, "./configure", fmt.Sprintf("--prefix=%s", prefix)); err != nil {
			return err
		}
		return runIn(repoPath, log, extraEnv, "make", "-j4")
	}

	// Plain Makefile
	if fileExistsAbs(filepath.Join(repoPath, "Makefile")) || fileExistsAbs(filepath.Join(repoPath, "GNUmakefile")) {
		return runIn(repoPath, log, extraEnv, "make", "-j4")
	}

	return fmt.Errorf("no recognised C/C++ build system found (tried cmake, meson, configure, make)")
}

// buildCMakeWithRetry runs cmake configure+build with up to 3 retries.
// On each failure it parses the error for missing packages, installs them, and retries.
// Also passes -DCMAKE_POLICY_VERSION_MINIMUM=3.5 for legacy projects (fixes Redis).
func buildCMakeWithRetry(repoPath, buildDir, prefix string, log pkgmanager.LogWriter, extraEnv []string, cascade *pkgmanager.Cascade, osInfo *detection.OSInfo) error {
	const maxRetries = 3

	// Detect CMake version to decide whether to pass the policy flag
	policyFlag := detectCMakePolicyFlag()

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// Clean build dir on retry to avoid stale cache
			_ = os.RemoveAll(buildDir)
			_ = os.MkdirAll(buildDir, 0755)
			if log != nil {
				log(fmt.Sprintf("[cmake] retry %d/%d after installing missing dependencies", attempt, maxRetries))
			}
		}

		// Build cmake args
		cmakeArgs := []string{"-B", buildDir, "-S", repoPath,
			"-DCMAKE_BUILD_TYPE=Release",
			fmt.Sprintf("-DCMAKE_INSTALL_PREFIX=%s", prefix),
		}
		if policyFlag != "" {
			cmakeArgs = append(cmakeArgs, policyFlag)
		}

		// Step 1: configure
		err := runIn(repoPath, log, extraEnv, "cmake", cmakeArgs...)
		if err == nil {
			// Configure succeeded — now build
			return runIn(repoPath, log, extraEnv, "cmake", "--build", buildDir, "--parallel")
		}

		lastErr = err

		// Parse the error for missing packages
		missing := parseCMakeMissingPackages(err.Error())
		if len(missing) == 0 || cascade == nil {
			// Can't auto-fix — return the error
			if attempt == 0 && cascade == nil && len(missing) > 0 {
				// We detected missing packages but have no cascade — add hint
				return fmt.Errorf("cmake configure failed (missing: %s): %w",
					strings.Join(missing, ", "), err)
			}
			return fmt.Errorf("cmake configure failed: %w", err)
		}

		if log != nil {
			log(fmt.Sprintf("[cmake] configure failed — detected missing packages: %v", missing))
		}

		// Install missing packages
		managerName := cascade.PrimaryName()
		for _, pkg := range missing {
			sysPkgs := resolveCMakePackage(pkg, managerName)
			for _, sp := range sysPkgs {
				if log != nil {
					log(fmt.Sprintf("[cmake] installing missing dep: %s → %s", pkg, sp))
				}
				_ = cascade.Install(sp, log)
			}
		}
	}

	return fmt.Errorf("cmake configure failed after %d retries: %w", maxRetries, lastErr)
}

// detectCMakePolicyFlag checks the installed CMake version and returns the
// appropriate policy flag. CMake >= 3.27 supports CMAKE_POLICY_VERSION_MINIMUM
// which allows legacy projects (like Redis) to build on modern CMake.
func detectCMakePolicyFlag() string {
	out, err := exec.Command("cmake", "--version").CombinedOutput()
	if err != nil {
		return "" // cmake not found — will fail later with a better error
	}
	// Parse "cmake version 3.29.2"
	lines := strings.Split(string(out), "\n")
	if len(lines) == 0 {
		return ""
	}
	fields := strings.Fields(lines[0])
	if len(fields) < 3 {
		return ""
	}
	version := fields[2]
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return ""
	}
	major := 0
	minor := 0
	fmt.Sscanf(parts[0], "%d", &major)
	fmt.Sscanf(parts[1], "%d", &minor)

	// CMAKE_POLICY_VERSION_MINIMUM was added in CMake 3.27
	if major > 3 || (major == 3 && minor >= 27) {
		return "-DCMAKE_POLICY_VERSION_MINIMUM=3.5"
	}
	return ""
}

// parseCMakeMissingPackages extracts missing package names from CMake error output.
// Looks for patterns like:
//
//	"Could NOT find Luv"
//	"Could not find X (missing: X_LIBRARY)"
//	"CMake Error ... find_package(X)"
var cmakeFindRe = regexp.MustCompile(`(?i)Could NOT find (\w+)`)
var cmakeFindPkgRe = regexp.MustCompile(`(?i)find_package\((\w+)`)

func parseCMakeMissingPackages(output string) []string {
	seen := map[string]bool{}
	var result []string

	for _, match := range cmakeFindRe.FindAllStringSubmatch(output, -1) {
		pkg := match[1]
		if !seen[pkg] && pkg != "" {
			seen[pkg] = true
			result = append(result, pkg)
		}
	}

	// Also look for "No package 'X' found" (pkg-config errors)
	pkgConfigRe := regexp.MustCompile(`No package '([^']+)' found`)
	for _, match := range pkgConfigRe.FindAllStringSubmatch(output, -1) {
		pkg := match[1]
		if !seen[pkg] && pkg != "" {
			seen[pkg] = true
			result = append(result, pkg)
		}
	}

	return result
}

// cmakePkgMap maps CMake find_package names to system package names per manager.
var cmakePkgMap = map[string]map[string]string{
	"Luv":        {"apt": "libluv1-dev", "pacman": "luv", "dnf": "lua-luv-devel", "apk": "luv-dev"},
	"LibUV":      {"apt": "libuv1-dev", "pacman": "libuv", "dnf": "libuv-devel", "apk": "libuv-dev"},
	"Unibilium":  {"apt": "libunibilium-dev", "pacman": "unibilium", "dnf": "unibilium-devel"},
	"LibVterm":   {"apt": "libvterm-dev", "pacman": "libvterm", "dnf": "libvterm-devel"},
	"MsgPack":    {"apt": "libmsgpack-dev", "pacman": "msgpack-c", "dnf": "msgpack-devel"},
	"TreeSitter": {"apt": "libtree-sitter-dev", "pacman": "tree-sitter", "dnf": "tree-sitter-devel"},
	"CURL":       {"apt": "libcurl4-openssl-dev", "pacman": "curl", "dnf": "libcurl-devel"},
	"Iconv":      {"apt": "libc6-dev", "pacman": "glibc", "dnf": "glibc-devel"},
	"OpenSSL":    {"apt": "libssl-dev", "pacman": "openssl", "dnf": "openssl-devel"},
	"ZLIB":       {"apt": "zlib1g-dev", "pacman": "zlib", "dnf": "zlib-devel"},
	"BZip2":      {"apt": "libbz2-dev", "pacman": "bzip2", "dnf": "bzip2-devel"},
	"LibLZMA":    {"apt": "liblzma-dev", "pacman": "xz", "dnf": "xz-devel"},
	"Readline":   {"apt": "libreadline-dev", "pacman": "readline", "dnf": "readline-devel"},
	"PCRE2":      {"apt": "libpcre2-dev", "pacman": "pcre2", "dnf": "pcre2-devel"},
	"PCRE":       {"apt": "libpcre3-dev", "pacman": "pcre", "dnf": "pcre-devel"},
	"Libssh2":    {"apt": "libssh2-1-dev", "pacman": "libssh2", "dnf": "libssh2-devel"},
	"Lua":        {"apt": "liblua5.4-dev", "pacman": "lua", "dnf": "lua-devel"},
	"GLib":       {"apt": "libglib2.0-dev", "pacman": "glib2", "dnf": "glib2-devel"},
	"GTK":        {"apt": "libgtk-3-dev", "pacman": "gtk3", "dnf": "gtk3-devel"},
	"GTK4":       {"apt": "libgtk-4-dev", "pacman": "gtk4", "dnf": "gtk4-devel"},
	"Boost":      {"apt": "libboost-all-dev", "pacman": "boost", "dnf": "boost-devel"},
	"Protobuf":   {"apt": "libprotobuf-dev", "pacman": "protobuf", "dnf": "protobuf-devel"},
	"EXPAT":      {"apt": "libexpat1-dev", "pacman": "expat", "dnf": "expat-devel"},
	"Freetype":   {"apt": "libfreetype6-dev", "pacman": "freetype2", "dnf": "freetype-devel"},
	"Fontconfig": {"apt": "libfontconfig-dev", "pacman": "fontconfig", "dnf": "fontconfig-devel"},
	"HarfBuzz":   {"apt": "libharfbuzz-dev", "pacman": "harfbuzz", "dnf": "harfbuzz-devel"},
	"PNG":        {"apt": "libpng-dev", "pacman": "libpng", "dnf": "libpng-devel"},
	"JPEG":       {"apt": "libjpeg-dev", "pacman": "libjpeg-turbo", "dnf": "libjpeg-turbo-devel"},
	"TIFF":       {"apt": "libtiff-dev", "pacman": "libtiff", "dnf": "libtiff-devel"},
	"X11":        {"apt": "libx11-dev", "pacman": "libx11", "dnf": "libX11-devel"},
	"Wayland":    {"apt": "libwayland-dev", "pacman": "wayland", "dnf": "wayland-devel"},
	"PkgConfig":  {"apt": "pkg-config", "pacman": "pkgconf", "dnf": "pkgconf-pkg-config"},
	"Threads":    {}, // Always available, skip
}

// resolveCMakePackage maps a CMake find_package name to system package names.
func resolveCMakePackage(cmakeName string, managerName string) []string {
	// Check the exact name first
	if pkgs, ok := cmakePkgMap[cmakeName]; ok {
		if resolved, ok := pkgs[managerName]; ok && resolved != "" {
			return []string{resolved}
		}
	}
	// Try case-insensitive match
	for key, pkgs := range cmakePkgMap {
		if strings.EqualFold(key, cmakeName) {
			if resolved, ok := pkgs[managerName]; ok && resolved != "" {
				return []string{resolved}
			}
		}
	}
	// Also try pkg-config style names (e.g. "luv" → "libluv1-dev" on apt)
	resolved := pkgmanager.ResolvePackageName(strings.ToLower(cmakeName), managerName)
	if resolved != strings.ToLower(cmakeName) {
		return []string{resolved}
	}
	// Last resort: guess the -dev package name pattern
	lower := strings.ToLower(cmakeName)
	switch managerName {
	case "apt":
		return []string{"lib" + lower + "-dev"}
	case "dnf":
		return []string{lower + "-devel"}
	case "pacman":
		return []string{lower}
	default:
		return []string{lower}
	}
}

func fileExistsAbs(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ── Launch ────────────────────────────────────────────────────────────────────

// launchProject runs the project based on its tech and category.

// launchCLI runs a CLI tool, showing an arg-selector UI first.

// launchDotfiles applies dotfiles using stow, chezmoi, or install scripts.
// Never prints README — only applies configs.

// findReadmeFile searches for a README file in the given directory (case-insensitive).

// launchDocs renders a markdown file beautifully in the terminal.
// Strategy: try glow (best) → mdcat → built-in renderer (always available).

// launchDocker runs the project using docker compose (v2 plugin) or docker-compose (standalone).

// launchScripts finds and runs the main shell script in the repo.

// ── cloneable.yaml spec launch ────────────────────────────────────────────────

// runFromSpec launches a project using the exact commands in cloneable.yaml.

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
// This is intentionally strict — only clear signals of missing packages/modules
// trigger a Phase II retry. Generic strings like "not found" or "missing" are
// excluded because they match too many legitimate build errors.

// getHelpOutput runs the tool with --help and returns its output.
// Used to populate the CLI arg selector.

// parseHelpIntoOptions extracts CLI flags from --help output into selector options.

// hasDotfileDirs returns true if the repo has typical dotfile directories.

// commandExists checks if a binary is in PATH.

// isExec returns true if the file exists and has execute permission.

// runInteractive runs a command with stdin/stdout/stderr connected to the host terminal.
// This is required for interactive CLI tools (like btop, vim, etc.).

// isCLIUsageExit returns true if the error is an ExitError with code 1 or 2.
// CLI tools commonly exit with these codes when run without arguments —
// they print their usage/help and exit non-zero. This is expected behaviour
// for utilities like zoxide, ripgrep, fd, etc. after a successful install.

// hasValidPackageName checks if package.json exists and contains a valid "name" field.
// npm install -g crashes with "Cannot destructure property 'name'" when the name
// is missing or malformed. This prevents that cryptic error.

// knownBinaryVariants returns alternative binary names for well-known projects
// where the binary name differs from the repository name.
// This solves Error 8 (redis installs redis-server/redis-cli, not "redis").

// symlinkNodeBins finds executables in node_modules/.bin and symlinks them
// to ~/.local/bin. This is the last-resort fallback when npm install -g
// and npm link both fail (e.g. missing name field in package.json).

// BuildProjectWithCascade is like buildProject but passes a package manager cascade
// to C/C++ builds for on-the-fly dependency resolution (fixes Neovim-style failures).
func BuildProjectWithCascade(repoPath string, profile *detection.TechProfile, environment *env.Environment,
	log pkgmanager.LogWriter, cascade *pkgmanager.Cascade, osInfo *detection.OSInfo) error {

	if len(profile.BuildCommands) == 0 {
		return nil
	}

	extraEnv := envVarsForTech(profile.Primary, environment)

	switch profile.Primary {
	case detection.TechCpp, detection.TechC:
		return buildCppWithCascade(repoPath, log, extraEnv, cascade, osInfo)
	default:
		return buildProject(repoPath, profile, environment, log)
	}
}
