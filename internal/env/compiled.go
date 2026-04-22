package env

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// ── Go ────────────────────────────────────────────────────────────────────────

// setupGo prepares the Go environment.
// Go manages its own global install via GOPATH/bin — no venv needed.
// We just verify GOPATH/bin is in PATH and set GOFLAGS for better defaults.
func (e *Environment) setupGo(log LogWriter) error {
	// Go uses ~/go/bin by default — already global
	// No isolated env directory needed
	e.EnvDir = ""

	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		home, _ := os.UserHomeDir()
		gopath = filepath.Join(home, "go")
	}

	// Ensure GOPATH/bin exists
	_ = os.MkdirAll(filepath.Join(gopath, "bin"), 0755)

	if log != nil {
		log(fmt.Sprintf("[go] GOPATH=%s", gopath))
	}
	return nil
}

// GoEnvVars returns environment variables for running go commands.
// CGO_ENABLED is left unset so Go auto-detects based on the project.
// Setting CGO_ENABLED=0 would break any project that uses cgo
// (SQLite bindings, GUI apps, crypto/x509 on some platforms, etc.).
func (e *Environment) GoEnvVars() []string {
	return []string{
		"GOFLAGS=-mod=mod",
	}
}

// GoBinDir returns the directory where `go install` places binaries.
func GoBinDir() string {
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		home, _ := os.UserHomeDir()
		gopath = filepath.Join(home, "go")
	}
	return filepath.Join(gopath, "bin")
}

// ── Rust ──────────────────────────────────────────────────────────────────────

// setupRust prepares the Rust/Cargo environment.
// Cargo manages its own global install via ~/.cargo/bin — no venv needed.
func (e *Environment) setupRust(log LogWriter) error {
	e.EnvDir = ""

	cargoHome := os.Getenv("CARGO_HOME")
	if cargoHome == "" {
		home, _ := os.UserHomeDir()
		cargoHome = filepath.Join(home, ".cargo")
	}

	// Set the build cache inside the repo to avoid polluting ~/.cargo/registry
	// with build artifacts from this specific project
	targetDir := filepath.Join(e.RepoPath, "target")
	_ = os.MkdirAll(targetDir, 0755)

	if log != nil {
		log(fmt.Sprintf("[rust] CARGO_HOME=%s", cargoHome))
		log(fmt.Sprintf("[rust] build target=%s", targetDir))
	}
	return nil
}

// RustEnvVars returns environment variables for cargo builds.
func (e *Environment) RustEnvVars() []string {
	return []string{
		"CARGO_TARGET_DIR=" + filepath.Join(e.RepoPath, "target"),
		"RUST_BACKTRACE=1",
	}
}

// RustBinDir returns the directory where `cargo install` places binaries.
func RustBinDir() string {
	cargoHome := os.Getenv("CARGO_HOME")
	if cargoHome == "" {
		home, _ := os.UserHomeDir()
		cargoHome = filepath.Join(home, ".cargo")
	}
	return filepath.Join(cargoHome, "bin")
}

// ── C / C++ ───────────────────────────────────────────────────────────────────

// setupCpp prepares a C/C++ build environment.
// IMPORTANT: This only creates the build directory. It does NOT run cmake/meson
// configure — that happens during the build phase in launch.go, AFTER system
// dependencies have been installed. Running configure before deps are present
// causes guaranteed failures.
func (e *Environment) setupCpp(log LogWriter) error {
	buildDir := filepath.Join(e.RepoPath, "build")
	e.EnvDir = buildDir

	if err := os.MkdirAll(buildDir, 0755); err != nil {
		return fmt.Errorf("could not create build directory: %w", err)
	}

	// Log which build system we detected — but do NOT configure yet.
	switch {
	case fileExistsInRepo(e.RepoPath, "CMakeLists.txt"):
		if log != nil {
			log("[c/c++] CMake detected — will configure during build phase")
		}
	case fileExistsInRepo(e.RepoPath, "meson.build"):
		if log != nil {
			log("[c/c++] Meson detected — will configure during build phase")
		}
	case fileExistsInRepo(e.RepoPath, "configure.ac") || fileExistsInRepo(e.RepoPath, "configure"):
		if log != nil {
			log("[c/c++] Autotools detected — will configure during build phase")
		}
	case fileExistsInRepo(e.RepoPath, "Makefile") || fileExistsInRepo(e.RepoPath, "GNUmakefile"):
		if log != nil {
			log("[c/c++] Makefile found — no separate configuration needed")
		}
	default:
		if log != nil {
			log("[c/c++] no recognised build system — will attempt make")
		}
	}

	return nil
}

// CppEnvVars returns environment variables for C/C++ builds.
func (e *Environment) CppEnvVars() []string {
	return []string{
		"CFLAGS=-O2",
		"CXXFLAGS=-O2",
	}
}

// installPrefix returns the local install prefix for compiled apps.
// Always uses ~/.local on Unix so we never need root for install.
// Linux/macOS: ~/.local  Windows: %USERPROFILE%\.cloneable
func (e *Environment) installPrefix() string {
	if runtime.GOOS == "windows" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".cloneable")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local")
}

// ── Zig ───────────────────────────────────────────────────────────────────────

func (e *Environment) setupZig(log LogWriter) error {
	// Zig installs to zig-out/bin by default
	// We set the install prefix to ~/.local so it's global
	e.EnvDir = filepath.Join(e.RepoPath, "zig-out")
	if log != nil {
		log("[zig] build output will be in zig-out/")
	}
	return nil
}

// ZigEnvVars returns environment variables for zig builds.
func (e *Environment) ZigEnvVars() []string {
	return []string{
		"ZIG_GLOBAL_CACHE_DIR=" + filepath.Join(os.TempDir(), "zig-cache"),
	}
}

// ── Flutter / Dart ────────────────────────────────────────────────────────────

// setupFlutter prepares a Flutter/Dart environment.
// IMPORTANT: This only marks the env as ready. It does NOT run `flutter pub get` —
// that is a dependency install step and belongs in installFlutter(), which runs
// AFTER system deps are installed. Running pub get before Flutter SDK is installed
// causes guaranteed failures.
func (e *Environment) setupFlutter(log LogWriter) error {
	e.EnvDir = ""
	if log != nil {
		log("[flutter] environment ready — dependencies will be fetched during install phase")
	}
	return nil
}

// FlutterEnvVars returns environment variables for Flutter builds.
func (e *Environment) FlutterEnvVars() []string {
	return []string{
		"PUB_CACHE=" + filepath.Join(os.TempDir(), "flutter-pub-cache"),
	}
}

// ── Java ──────────────────────────────────────────────────────────────────────

func (e *Environment) setupJava(log LogWriter) error {
	// Create local gradle cache inside repo to avoid ~/.gradle pollution
	e.EnvDir = filepath.Join(e.RepoPath, ".gradle")
	if err := os.MkdirAll(e.EnvDir, 0755); err != nil {
		return err
	}
	if log != nil {
		log("[java] gradle cache will be stored in .gradle/")
	}
	return nil
}

// JavaEnvVars returns environment variables for Java/Gradle builds.
func (e *Environment) JavaEnvVars() []string {
	return []string{
		"GRADLE_USER_HOME=" + filepath.Join(e.RepoPath, ".gradle"),
	}
}

// ── Ruby ──────────────────────────────────────────────────────────────────────

func (e *Environment) setupRuby(log LogWriter) error {
	// Use bundler's local vendor/bundle for isolation
	vendorPath := filepath.Join(e.RepoPath, "vendor", "bundle")
	e.EnvDir = vendorPath

	if err := os.MkdirAll(vendorPath, 0755); err != nil {
		return err
	}

	// Configure bundler to use local vendor path — best-effort
	if binaryExists("bundle") {
		if err := runCmd(log, "bundle", "config", "set", "--local", "path", "vendor/bundle"); err != nil {
			if log != nil {
				log("[ruby] warning: could not configure bundler path — continuing anyway")
			}
		}
	}

	return nil
}

// RubyEnvVars returns environment variables for Ruby/Bundler.
func (e *Environment) RubyEnvVars() []string {
	return []string{
		"BUNDLE_PATH=" + filepath.Join(e.RepoPath, "vendor", "bundle"),
	}
}

// ── dotnet ────────────────────────────────────────────────────────────────────

func (e *Environment) setupDotnet(log LogWriter) error {
	// dotnet restore downloads packages to a local obj/ folder
	e.EnvDir = filepath.Join(e.RepoPath, "obj")
	if log != nil {
		log("[dotnet] packages will be restored to obj/")
	}
	return nil
}

// DotnetEnvVars returns environment variables for dotnet builds.
func (e *Environment) DotnetEnvVars() []string {
	return []string{
		"DOTNET_CLI_TELEMETRY_OPTOUT=1",
		"NUGET_PACKAGES=" + filepath.Join(e.RepoPath, "obj", "packages"),
	}
}

// ── Haskell ───────────────────────────────────────────────────────────────────

func (e *Environment) setupHaskell(log LogWriter) error {
	e.EnvDir = ""
	if fileExistsInRepo(e.RepoPath, "stack.yaml") {
		if log != nil {
			log("[haskell] stack project detected")
		}
	} else {
		if log != nil {
			log("[haskell] cabal project detected")
		}
	}
	return nil
}

// HaskellEnvVars returns environment variables for Haskell builds.
func (e *Environment) HaskellEnvVars() []string {
	return []string{
		"STACK_ROOT=" + filepath.Join(e.RepoPath, ".stack-work"),
	}
}
