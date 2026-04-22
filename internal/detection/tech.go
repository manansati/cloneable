package detection

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

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
	}

	// ── Step 1: Check for cloneable.yaml ─────────────────────────────────────
	spec, err := loadCloneableSpec(repoPath)
	if err == nil && spec != nil {
		profile.HasCloneableSpec = true
		profile.CloneableSpec = spec
		applySpec(profile, spec, repoPath)
		return profile, nil
	}

	// ── Step 2: Scan manifest files ───────────────────────────────────────────
	manifests, err := ScanManifests(repoPath, 2)
	if err != nil {
		return profile, err
	}
	profile.Manifests = manifests

	// Wait, we don't return early. We just proceed.
	// ── Step 4: Determine primary tech from manifests ─────────────────────────
	// Sort by confidence descending, then by depth ascending (root = preferred)
	sort.Slice(manifests, func(i, j int) bool {
		if manifests[i].Entry.Confidence != manifests[j].Entry.Confidence {
			return manifests[i].Entry.Confidence > manifests[j].Entry.Confidence
		}
		return manifests[i].Depth < manifests[j].Depth
	})

	// The first IsPrimary entry at highest confidence wins as Primary.
	// BUT: if two primaries have the same confidence at the same depth,
	// count source files to break the tie. This handles repos like Neovim
	// which have both build.zig (100) and CMakeLists.txt (90) — but are
	// fundamentally C projects by source file count.
	for _, m := range manifests {
		if m.Entry.IsPrimary && profile.Primary == TechUnknown {
			profile.Primary = m.Entry.Tech
			profile.Confidence = m.Entry.Confidence
			if m.Depth > 0 {
				profile.Confidence = max(profile.Confidence-10, 50)
			}
		}
	}

	// Source-file tie-breaker: if the winner has confidence <= 100 and there
	// is a competing primary at root with confidence >= 85, count actual source
	// files to see which language dominates.
	profile.Primary = refineBySourceFiles(repoPath, manifests, profile.Primary)

	// ── Step 4.5: Structural heuristics fallback ──────────────────────────────
	// If no primary manifest was found (or if we only found non-primary manifests like Makefile)
	if profile.Primary == TechUnknown {
		if isDotfileRepo(repoPath) {
			profile.Primary = TechDotfile
			profile.Category = CategoryDotfiles
			profile.Confidence = 75
			return profile, nil
		}
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
			// We DO NOT return early here because a repo with a Makefile and shell scripts
			// might need `make install` from the Install commands logic later.
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

	// ── Step 5: Determine category ────────────────────────────────────────────
	profile.Category = DetermineCategory(repoPath, profile.Primary, manifests)

	// ── Step 6: Determine system deps ─────────────────────────────────────────
	profile.SystemDeps = systemDepsFor(repoPath, profile.Primary)

	// ── Step 7: Determine build / run / install commands ─────────────────────
	profile.BuildCommands = BuildCommand(repoPath, profile.Primary)
	profile.RunCommands = RunCommand(repoPath, profile.Primary, profile.Category)
	profile.InstallCommands = InstallGlobalCommand(repoPath, profile.Primary)

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
	switch tech {
	case TechGo:
		return []string{"git", "make"}

	case TechRust:
		deps := []string{"gcc", "pkg-config", "make"}
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
		deps := []string{"zig", "make", "pkg-config"}
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
		return []string{"nodejs", "npm"}

	case TechPython:
		return []string{"python3", "python3-pip", "pipx"}

	case TechJava:
		if fileExists(repoPath, "gradlew") || fileExists(repoPath, "build.gradle") || fileExists(repoPath, "build.gradle.kts") {
			return []string{"java", "gradle"}
		}
		return []string{"java", "maven"}

	case TechCpp, TechC:
		deps := []string{"gcc", "g++", "make", "pkg-config"}
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
		return []string{"ruby", "bundler"}
	case TechDotnet:
		return []string{"dotnet-sdk"}
	case TechHaskell:
		return []string{"ghc", "cabal-install"}
	case TechDocker:
		return []string{"docker"}
	}
	return nil
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

// max returns the larger of two ints.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// refineBySourceFiles is called when manifest-based detection might have picked
// the wrong primary. It counts actual source files by extension to validate or
// correct the detected tech. This handles repos like Neovim that have a build.zig
// (added later for optional Zig builds) but are fundamentally C/C++ projects.
func refineBySourceFiles(repoPath string, manifests []FoundManifest, detected TechType) TechType {
	// Only apply correction when there are competing primaries at root
	rootPrimaries := make(map[TechType]int) // tech -> manifest confidence
	for _, m := range manifests {
		if m.Entry.IsPrimary && m.Depth == 0 {
			if existing, ok := rootPrimaries[m.Entry.Tech]; !ok || m.Entry.Confidence > existing {
				rootPrimaries[m.Entry.Tech] = m.Entry.Confidence
			}
		}
	}

	// If only one primary at root, trust it — no correction needed
	if len(rootPrimaries) <= 1 {
		return detected
	}

	// Count source files by language
	counts := countSourceFiles(repoPath)
	if len(counts) == 0 {
		return detected
	}

	// Find the dominant language
	dominant := detected
	maxCount := 0
	for lang, count := range counts {
		if count > maxCount {
			maxCount = count
			dominant = lang
		}
	}

	// Only override if the dominant language is very clearly different
	// (at least 3x more files than the detected tech's file count)
	detectedCount := counts[detected]
	if detectedCount == 0 || maxCount >= detectedCount*3 {
		// Make sure the dominant tech actually has a root manifest
		if _, ok := rootPrimaries[dominant]; ok {
			return dominant
		}
	}

	return detected
}

// countSourceFiles walks the repo root (one level deep) and counts
// source files per technology type by extension.
func countSourceFiles(repoPath string) map[TechType]int {
	counts := make(map[TechType]int)

	// Extension → tech type mapping for source file counting
	extToTech := map[string]TechType{
		".c":    TechC,
		".h":    TechC,
		".cpp":  TechCpp,
		".cc":   TechCpp,
		".cxx":  TechCpp,
		".hpp":  TechCpp,
		".zig":  TechZig,
		".rs":   TechRust,
		".go":   TechGo,
		".py":   TechPython,
		".js":   TechNode,
		".ts":   TechNode,
		".java": TechJava,
		".kt":   TechKotlin,
		".rb":   TechRuby,
		".lua":  TechUnknown, // Lua — not a primary tech, but helps with neovim detection
	}

	// Walk src/, lib/, include/ and root — these are the most telling
	searchDirs := []string{repoPath, "src", "lib", "include", "source"}

	for _, dir := range searchDirs {
		fullDir := dir
		if dir != repoPath {
			fullDir = filepath.Join(repoPath, dir)
		}
		entries, err := os.ReadDir(fullDir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(entry.Name()))
			if tech, ok := extToTech[ext]; ok && tech != TechUnknown {
				counts[tech]++
			}
		}
	}

	// C and C++ are the same project type — merge them
	if cCount, ok := counts[TechC]; ok {
		counts[TechCpp] += cCount
		delete(counts, TechC)
	}

	return counts
}
