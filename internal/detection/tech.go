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

	if len(manifests) == 0 {
		// ── Step 3: Structural heuristics ─────────────────────────────────────
		if isDotfileRepo(repoPath) {
			profile.Primary = TechDotfile
			profile.Category = CategoryDotfiles
			profile.Confidence = 75
			return profile, nil
		}
		if isDocsRepo(repoPath) {
			profile.Primary = TechDocs
			profile.Category = CategoryDocs
			profile.Confidence = 70
			return profile, nil
		}
		// Scripts repo: check for shell scripts
		if hasShellScripts(repoPath) {
			profile.Primary = TechScripts
			profile.Category = CategoryScripts
			profile.Confidence = 65
			return profile, nil
		}
		// Truly unknown
		profile.Confidence = 0
		return profile, nil
	}

	// ── Step 4: Determine primary tech from manifests ─────────────────────────
	// Sort by confidence descending, then by depth ascending (root = preferred)
	sort.Slice(manifests, func(i, j int) bool {
		if manifests[i].Entry.Confidence != manifests[j].Entry.Confidence {
			return manifests[i].Entry.Confidence > manifests[j].Entry.Confidence
		}
		return manifests[i].Depth < manifests[j].Depth
	})

	// The first IsPrimary entry at highest confidence wins as Primary.
	for _, m := range manifests {
		if m.Entry.IsPrimary && profile.Primary == TechUnknown {
			profile.Primary = m.Entry.Tech
			profile.Confidence = m.Entry.Confidence
			// Penalise slightly if not at root
			if m.Depth > 0 {
				profile.Confidence = max(profile.Confidence-10, 50)
			}
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
		return []string{"git"} // go itself is installed separately
	case TechRust:
		// Most Rust projects need these for crates with C bindings
		deps := []string{"gcc", "pkg-config", "openssl"}
		// Check Cargo.toml for common system-dep signals
		data, err := os.ReadFile(filepath.Join(repoPath, "Cargo.toml"))
		if err == nil {
			content := string(data)
			if strings.Contains(content, "openssl") {
				deps = append(deps, "libssl-dev")
			}
			if strings.Contains(content, "sqlite") {
				deps = append(deps, "libsqlite3-dev")
			}
			if strings.Contains(content, "fontconfig") {
				deps = append(deps, "libfontconfig-dev")
			}
		}
		return deps
	case TechNode:
		return []string{"nodejs", "npm"}
	case TechPython:
		return []string{"python3", "python3-pip"}
	case TechJava:
		return []string{"java", "gradle"}
	case TechCpp, TechC:
		deps := []string{"gcc", "make", "pkg-config"}
		if fileExists(repoPath, "CMakeLists.txt") {
			deps = append(deps, "cmake")
		}
		if fileExists(repoPath, "meson.build") {
			deps = append(deps, "meson", "ninja-build")
		}
		return deps
	case TechZig:
		return []string{"zig"}
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

// ── Helpers ───────────────────────────────────────────────────────────────────

// hasShellScripts returns true if the repo root has .sh files.
func hasShellScripts(repoPath string) bool {
	entries, err := os.ReadDir(repoPath)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".sh") {
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
