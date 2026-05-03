package detection

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	enry "github.com/go-enry/go-enry/v2"
	"gopkg.in/yaml.v3"
)

// TechProfile is the complete picture of a repository's technology stack.
// Every other part of Cloneable reads from this — Phase II and III depend on it.
type TechProfile struct {
	// Primary is the main technology of the project.
	// This determines the install and launch strategy.
	Primary TechType

	// Extra lists all additional technologies found in the repo.
	// e.g. a Rust app that uses Python build scripts: Primary=Rust, Extra=[Python]
	Extra []TechType

	// Category classifies the repo's purpose (CLI, App, Library, etc.).
	Category RepoCategory

	// SystemDeps are system packages that need to be installed before building.
	// e.g. ["cmake", "pkg-config", "libssl-dev", "ffmpeg"]
	SystemDeps []string

	// BuildCommands are the ordered commands to build the project.
	// nil means no build step needed (interpreted language).
	BuildCommands []string

	// RunCommands are the ordered commands to run/launch the project.
	RunCommands []string

	// InstallCommands are the commands to install the binary globally.
	InstallCommands []string

	// HasCloneableSpec is true if cloneable.yaml was found and used.
	// When true, the spec values override all detected values.
	HasCloneableSpec bool

	// CloneableSpec is the parsed cloneable.yaml, if present.
	CloneableSpec *CloneableSpec

	// Confidence is the overall detection confidence (0–100).
	// 100 = cloneable.yaml present. 90+ = primary manifest found at root.
	// Below 70 = Cloneable will warn the user.
	Confidence int

	// WorkingDir is the directory where the primary manifest was found.
	// Language-specific build/install commands should run here instead of RepoPath.
	WorkingDir string

	// Manifests are all manifest files that were found during scanning.
	Manifests []FoundManifest
}

// CloneableSpec is the parsed content of a cloneable.yaml file.
// Repo authors can ship this to give Cloneable exact instructions.
type CloneableSpec struct {
	Name        string            `yaml:"name"`
	Version     string            `yaml:"version"`
	Type        string            `yaml:"type"`
	DependsOn   []string          `yaml:"depends_on"`
	Build       string            `yaml:"build"`
	Install     string            `yaml:"install"`
	Run         string            `yaml:"run"`
	GlobalBin   string            `yaml:"global_binary"`
	Env         map[string]string `yaml:"env"`
	Args        []SpecArg         `yaml:"args"`
}

// SpecArg defines a CLI argument prompt shown in Phase III for tools that
// need arguments before launching.
type SpecArg struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Required    bool   `yaml:"required"`
}

// DetectTech analyzes the repository at repoPath and returns a TechProfile.
//
// Detection order (strict, no guessing):
//  1. cloneable.yaml  → highest priority, repo author's intent
//  2. Root manifests  → go.mod, Cargo.toml, package.json, etc.
//  3. Shallow scan    → up to 2 levels deep for monorepos
//  4. Dotfile/doc heuristics → structure-based fallback
//  5. Unknown         → warn user, still attempt
func DetectTech(repoPath string) (*TechProfile, error) {
	profile := &TechProfile{
		Primary:    TechUnknown,
		Category:   CategoryUnknown,
		Confidence: 0,
		WorkingDir: repoPath,
	}

	// ── Step 1: Check for cloneable.yaml ─────────────────────────────────────
	spec, err := loadCloneableSpec(repoPath)
	if err == nil && spec != nil {
		profile.HasCloneableSpec = true
		profile.CloneableSpec = spec
		applySpec(profile, spec, repoPath)
		return profile, nil
	}

	// ── Step 2: Structural heuristic — dotfiles (BEFORE manifests) ────────────
	// A dotfile repo must never be hijacked by a nested package.json in .config/.
	if isDotfileRepo(repoPath) {
		profile.Primary = TechDotfile
		profile.Category = CategoryDotfiles
		profile.Confidence = 75
		return profile, nil
	}

	// ── Step 3: Scan manifest files ───────────────────────────────────────────
	manifests, err := ScanManifests(repoPath, 2)
	if err != nil {
		return profile, err
	}
	profile.Manifests = manifests

	// ── Step 4: Sort manifests — depth FIRST, then confidence ─────────────────
	// Root manifests (Depth 0) ALWAYS win over deeper manifests, regardless of
	// confidence. This prevents a nested hooks/package.json from hijacking a
	// root install.sh script repository.
	sort.Slice(manifests, func(i, j int) bool {
		if manifests[i].Depth != manifests[j].Depth {
			return manifests[i].Depth < manifests[j].Depth
		}
		return manifests[i].Entry.Confidence > manifests[j].Entry.Confidence
	})

	// ── Step 5: Pick primary from manifests ───────────────────────────────────
	// First pass: only consider root (Depth == 0) primaries.
	hasRootPrimary := false
	for _, m := range manifests {
		if m.Entry.IsPrimary && m.Depth == 0 {
			hasRootPrimary = true
			if profile.Primary == TechUnknown {
				profile.Primary = m.Entry.Tech
				profile.Confidence = m.Entry.Confidence
				profile.WorkingDir = filepath.Dir(m.FullPath)
			}
		}
	}

	// Second pass: if no root primary, fall back to deeper manifests.
	if !hasRootPrimary {
		for _, m := range manifests {
			if m.Entry.IsPrimary && profile.Primary == TechUnknown {
				profile.Primary = m.Entry.Tech
				profile.Confidence = max(m.Entry.Confidence-10, 50)
				profile.WorkingDir = filepath.Dir(m.FullPath)
				break
			}
		}
	}

	// ── Step 6: go-enry language detection fallback ───────────────────────────
	// If no manifest determined the primary, use go-enry to analyze source files.
	if profile.Primary == TechUnknown {
		enryTech := detectDominantLanguage(repoPath)
		if enryTech != TechUnknown {
			profile.Primary = enryTech
			profile.Confidence = 55
		}
	}

	// ── Step 6.5: Documentation and script fallbacks ──────────────────────────
	if profile.Primary == TechUnknown {
		if isDocsRepo(repoPath, false) {
			profile.Primary = TechDocs
			profile.Category = CategoryDocs
			profile.Confidence = 70
			return profile, nil
		}
		if hasShellScripts(repoPath) {
			profile.Primary = TechScripts
			profile.Category = CategoryScripts
			profile.Confidence = 65
		}
	}

	// Collect extra technologies (not the primary, not docker which is handled separately)
	seen := map[TechType]bool{profile.Primary: true}
	for _, m := range manifests {
		t := m.Entry.Tech
		if !seen[t] && t != TechUnknown {
			profile.Extra = append(profile.Extra, t)
			seen[t] = true
		}
	}

	// ── Step 7: Determine category ────────────────────────────────────────────
	profile.Category = DetermineCategory(profile.WorkingDir, profile.Primary, manifests)

	// ── Step 8: Determine system deps ─────────────────────────────────────────
	profile.SystemDeps = systemDepsFor(profile.WorkingDir, profile.Primary)

	// ── Step 9: Determine build / run / install commands ─────────────────────
	profile.BuildCommands = BuildCommand(profile.WorkingDir, profile.Primary)
	profile.RunCommands = RunCommand(profile.WorkingDir, profile.Primary, profile.Category)
	profile.InstallCommands = InstallGlobalCommand(profile.WorkingDir, profile.Primary)

	return profile, nil
}

// ── cloneable.yaml ────────────────────────────────────────────────────────────

// loadCloneableSpec reads and parses cloneable.yaml from the repo root.
// Returns (nil, nil) if the file doesn't exist — not an error.
func loadCloneableSpec(repoPath string) (*CloneableSpec, error) {
	path := filepath.Join(repoPath, "cloneable.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil // file doesn't exist — not an error
	}
	var spec CloneableSpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, err
	}
	return &spec, nil
}

// applySpec populates the TechProfile from a CloneableSpec.
// The spec is authoritative — it overrides all heuristic detection.
func applySpec(profile *TechProfile, spec *CloneableSpec, repoPath string) {
	profile.Confidence = 100

	// Map the spec's type string to a TechType
	profile.Primary = specTypeToTech(spec.Type)

	// System deps
	profile.SystemDeps = spec.DependsOn

	// Commands — split shell string into args
	if spec.Build != "" {
		profile.BuildCommands = strings.Fields(spec.Build)
	} else {
		profile.BuildCommands = BuildCommand(repoPath, profile.Primary)
	}

	if spec.Run != "" {
		profile.RunCommands = strings.Fields(spec.Run)
	} else {
		profile.RunCommands = RunCommand(repoPath, profile.Primary, CategoryUnknown)
	}

	if spec.Install != "" {
		profile.InstallCommands = strings.Fields(spec.Install)
	} else {
		profile.InstallCommands = InstallGlobalCommand(repoPath, profile.Primary)
	}

	profile.Category = DetermineCategory(repoPath, profile.Primary, nil)
}

// specTypeToTech maps the string in cloneable.yaml's "type" field to TechType.
func specTypeToTech(t string) TechType {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "go", "golang":
		return TechGo
	case "rust":
		return TechRust
	case "node", "nodejs", "node.js":
		return TechNode
	case "python":
		return TechPython
	case "java":
		return TechJava
	case "kotlin":
		return TechKotlin
	case "c++", "cpp":
		return TechCpp
	case "c":
		return TechC
	case "zig":
		return TechZig
	case "flutter":
		return TechFlutter
	case "dart":
		return TechDart
	case "ruby":
		return TechRuby
	case "dotnet", ".net":
		return TechDotnet
	case "haskell":
		return TechHaskell
	case "docker":
		return TechDocker
	case "dotfiles":
		return TechDotfile
	case "docs", "documentation":
		return TechDocs
	case "scripts":
		return TechScripts
	default:
		return TechUnknown
	}
}

// ── System dependency tables ──────────────────────────────────────────────────

// systemDepsFor returns the system-level packages typically needed to build
// a project of the given tech type. These are installed via the package manager
// in Phase II before the language-level dependencies are installed.
func systemDepsFor(repoPath string, tech TechType) []string {
	// Base tools needed by almost every build system or installer script
	baseDeps := []string{"curl", "wget", "unzip", "tar", "git"}

	var deps []string
	switch tech {
	case TechGo:
		deps = append(baseDeps, "make")

	case TechRust:
		deps = append(baseDeps, "gcc", "pkg-config", "make")
		// Deep scan Cargo.toml + Cargo.lock for system lib signals
		for _, file := range []string{"Cargo.toml", "Cargo.lock"} {
			data, err := os.ReadFile(filepath.Join(repoPath, file))
			if err != nil {
				continue
			}
			content := strings.ToLower(string(data))
			if strings.Contains(content, "openssl") {
				deps = appendUniq(deps, "openssl", "libssl-dev")
			}
			if strings.Contains(content, "sqlite") {
				deps = appendUniq(deps, "libsqlite3-dev", "sqlite3")
			}
			if strings.Contains(content, "fontconfig") {
				deps = appendUniq(deps, "libfontconfig-dev", "fontconfig")
			}
			if strings.Contains(content, "gtk") {
				deps = appendUniq(deps, "libgtk-3-dev", "gtk3")
			}
			if strings.Contains(content, "glib") {
				deps = appendUniq(deps, "libglib2.0-dev", "glib2")
			}
			if strings.Contains(content, "alsa") {
				deps = appendUniq(deps, "libasound2-dev", "alsa-lib")
			}
			if strings.Contains(content, "dbus") {
				deps = appendUniq(deps, "libdbus-1-dev", "dbus")
			}
			if strings.Contains(content, "zlib") {
				deps = appendUniq(deps, "zlib1g-dev", "zlib")
			}
			if strings.Contains(content, "freetype") {
				deps = appendUniq(deps, "libfreetype6-dev", "freetype2")
			}
		}
		return deps

	case TechZig:
		// Zig projects scan build.zig for system library usage
		deps = append(baseDeps, "zig", "make", "pkg-config")
		data, err := os.ReadFile(filepath.Join(repoPath, "build.zig"))
		if err == nil {
			content := strings.ToLower(string(data))
			// GTK / glib (Ghostty uses this)
			if strings.Contains(content, "glib") || strings.Contains(content, "gresource") {
				deps = appendUniq(deps, "libglib2.0-dev", "glib2", "glib-compile-resources")
			}
			if strings.Contains(content, "gtk") {
				deps = appendUniq(deps, "libgtk-4-dev", "libgtk-3-dev", "gtk4", "gtk3")
			}
			// Ghostty-specific dependencies
			if strings.Contains(content, "ghostty") || strings.Contains(content, "apprt") {
				deps = appendUniq(deps,
					"libglib2.0-dev", "libgtk-4-dev", "libadwaita-1-dev",
					"glib2", "gtk4", "libadwaita",
					"blueprint-compiler", "glib-compile-resources",
					"pandoc", "wayland-protocols", "libxkbcommon-dev", "libxkbcommon",
					"libgtk4-layer-shell-dev", "gtk4-layer-shell",
				)
			}
			if strings.Contains(content, "freetype") {
				deps = appendUniq(deps, "libfreetype6-dev", "freetype2")
			}
			if strings.Contains(content, "fontconfig") {
				deps = appendUniq(deps, "libfontconfig-dev", "fontconfig")
			}
			if strings.Contains(content, "harfbuzz") {
				deps = appendUniq(deps, "libharfbuzz-dev", "harfbuzz")
			}
			if strings.Contains(content, "libpng") || strings.Contains(content, "png") {
				deps = appendUniq(deps, "libpng-dev", "libpng")
			}
			if strings.Contains(content, "opengl") || strings.Contains(content, "gl.h") {
				deps = appendUniq(deps, "libgl1-mesa-dev", "mesa")
			}
			if strings.Contains(content, "wayland") {
				deps = appendUniq(deps, "libwayland-dev", "wayland", "wayland-protocols")
			}
			if strings.Contains(content, "x11") || strings.Contains(content, "xlib") {
				deps = appendUniq(deps, "libx11-dev", "libxorg-dev")
			}
		}
		return deps

	case TechNode:
		deps = append(baseDeps, "nodejs", "npm", "build-essential")
		return deps

	case TechPython:
		deps = append(baseDeps, "python3", "python3-venv", "build-essential")
		return deps

	case TechJava:
		if fileExists(repoPath, "gradlew") || fileExists(repoPath, "build.gradle") || fileExists(repoPath, "build.gradle.kts") {
			deps = append(baseDeps, "java", "gradle")
		} else {
			deps = append(baseDeps, "java", "maven")
		}
		return deps

	case TechCpp, TechC:
		deps = append(baseDeps, "gcc", "g++", "make", "pkg-config")
		// Scan CMakeLists.txt for find_package calls
		data, err := os.ReadFile(filepath.Join(repoPath, "CMakeLists.txt"))
		if err == nil {
			content := strings.ToLower(string(data))
			if strings.Contains(content, "cmake") {
				deps = appendUniq(deps, "cmake")
			}
			if strings.Contains(content, "openssl") {
				deps = appendUniq(deps, "libssl-dev", "openssl")
			}
			if strings.Contains(content, "gtk") {
				deps = appendUniq(deps, "libgtk-3-dev", "gtk3-devel")
			}
			if strings.Contains(content, "boost") {
				deps = appendUniq(deps, "libboost-all-dev", "boost-devel")
			}
			if strings.Contains(content, "qt") {
				deps = appendUniq(deps, "qt5-default", "qt5-qtbase-devel")
			}
			if strings.Contains(content, "lua") {
				deps = appendUniq(deps, "liblua5.3-dev", "lua-devel")
			}
			if strings.Contains(content, "python") {
				deps = appendUniq(deps, "python3-dev", "python3-devel")
			}
		}
		if fileExists(repoPath, "meson.build") {
			deps = appendUniq(deps, "meson", "ninja-build")
		}
		return deps

	case TechFlutter:
		return []string{"flutter"}
	case TechDart:
		return []string{"dart"}
	case TechRuby:
		deps = append(baseDeps, "ruby", "build-essential")
		return deps

	case TechDotnet:
		deps = append(baseDeps, "dotnet-sdk")
		return deps

	case TechHaskell:
		deps = append(baseDeps, "ghc", "cabal-install")
		return deps

	case TechDocker, TechDotfile, TechDocs, TechScripts, TechUnknown:
		return baseDeps
	}

	return baseDeps
}

// appendUniq appends items to the slice only if they are not already present.
func appendUniq(slice []string, items ...string) []string {
	existing := make(map[string]bool, len(slice))
	for _, s := range slice {
		existing[s] = true
	}
	for _, item := range items {
		if !existing[item] {
			slice = append(slice, item)
			existing[item] = true
		}
	}
	return slice
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// hasShellScripts returns true if the repo root has .sh files or bash shebangs.
func hasShellScripts(repoPath string) bool {
	entries, err := os.ReadDir(repoPath)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), ".sh") {
			return true
		}
		// Check for shebang
		file, err := os.Open(filepath.Join(repoPath, entry.Name()))
		if err != nil {
			continue
		}
		buf := make([]byte, 128)
		n, _ := file.Read(buf)
		file.Close()
		content := string(buf[:n])
		if strings.HasPrefix(content, "#!") && (strings.Contains(content, "bash") || strings.Contains(content, "sh")) {
			return true
		}
	}
	return false
}

// TechDisplayName returns a human-readable display string for the UI header.
// e.g. TechNode → "Node.js", TechCpp → "C/C++"
func TechDisplayName(tech TechType) string {
	return string(tech)
}

// detectDominantLanguage uses go-enry to analyze the repository's source files
// and returns the dominant programming language as a TechType.
// This replaces the old countSourceFiles/refineBySourceFiles approach with
// a comprehensive, content-aware language detection engine.
func detectDominantLanguage(repoPath string) TechType {
	// langBytes tracks total bytes per enry language name.
	langBytes := make(map[string]int64)

	filepath.WalkDir(repoPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if shouldSkipDir(name) || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}

		// Prevent blocking on named pipes (FIFOs) or other non-regular files
		if !d.Type().IsRegular() {
			return nil
		}

		// Get path relative to repo root for enry analysis
		relPath, err := filepath.Rel(repoPath, path)
		if err != nil {
			return nil
		}

		// Skip vendor, generated, and dotfiles
		if enry.IsVendor(relPath) || enry.IsDotFile(relPath) {
			return nil
		}

		// Read up to 16KB for language detection (enry doesn't need the whole file)
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()

		content, err := io.ReadAll(io.LimitReader(f, 16*1024))
		if err != nil || len(content) == 0 {
			return nil
		}

		// Skip generated files
		if enry.IsGenerated(relPath, content) {
			return nil
		}

		lang := enry.GetLanguage(filepath.Base(path), content)
		if lang == "" || enry.GetLanguageType(lang) != enry.Programming {
			return nil
		}

		// Use actual file size for accurate byte weighting
		info, err := d.Info()
		if err != nil {
			return nil
		}
		langBytes[lang] += info.Size()

		return nil
	})

	if len(langBytes) == 0 {
		return TechUnknown
	}

	// Find the dominant language by byte count
	var dominant string
	var maxBytes int64
	for lang, bytes := range langBytes {
		if bytes > maxBytes {
			maxBytes = bytes
			dominant = lang
		}
	}

	return enryLangToTech(dominant)
}

// enryLangToTech maps a go-enry language name to our internal TechType.
func enryLangToTech(lang string) TechType {
	switch lang {
	case "Go":
		return TechGo
	case "Rust":
		return TechRust
	case "JavaScript", "TypeScript", "TSX", "JSX":
		return TechNode
	case "Python":
		return TechPython
	case "Java":
		return TechJava
	case "Kotlin":
		return TechKotlin
	case "C":
		return TechC
	case "C++":
		return TechCpp
	case "Zig":
		return TechZig
	case "Dart":
		return TechDart
	case "Ruby":
		return TechRuby
	case "C#", "F#":
		return TechDotnet
	case "Haskell":
		return TechHaskell
	case "Shell", "Bash", "Zsh", "Fish":
		return TechScripts
	default:
		return TechUnknown
	}
}
