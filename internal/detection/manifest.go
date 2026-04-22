package detection

import (
	"os"
	"path/filepath"
	"strings"
)

// TechType represents a detected primary technology or language.
type TechType string

const (
	TechGo      TechType = "Go"
	TechRust    TechType = "Rust"
	TechNode    TechType = "Node.js"
	TechPython  TechType = "Python"
	TechJava    TechType = "Java"
	TechKotlin  TechType = "Kotlin"
	TechCpp     TechType = "C/C++"
	TechC       TechType = "C"
	TechZig     TechType = "Zig"
	TechFlutter TechType = "Flutter"
	TechDart    TechType = "Dart"
	TechRuby    TechType = "Ruby"
	TechDotnet  TechType = "dotnet"
	TechHaskell TechType = "Haskell"
	TechScripts TechType = "Scripts"
	TechDocs    TechType = "Documentation"
	TechDotfile TechType = "Dotfiles"
	TechDocker  TechType = "Docker"
	TechUnknown TechType = "Unknown"
)

// ManifestEntry maps a specific filename (relative to repo root) to a tech
// and a confidence score (0–100). Higher = more certain.
// This table is the single source of truth for detection — no guessing.
type ManifestEntry struct {
	File       string   // filename to look for (exact match in root or shallow search)
	Tech       TechType // technology this file indicates
	Confidence int      // 0–100
	IsPrimary  bool     // true = this file alone is enough to confirm the tech
}

// manifestTable is checked in order. First match with IsPrimary=true wins as
// the primary tech. All matches accumulate into the dependency list.
var manifestTable = []ManifestEntry{
	// ── Tier 1: Unambiguous primary manifests ─────────────────────────────────
	{File: "go.mod", Tech: TechGo, Confidence: 100, IsPrimary: true},
	{File: "Cargo.toml", Tech: TechRust, Confidence: 100, IsPrimary: true},
	{File: "pubspec.yaml", Tech: TechFlutter, Confidence: 100, IsPrimary: true},
	{File: "build.zig", Tech: TechZig, Confidence: 100, IsPrimary: true},
	{File: "build.zig.zon", Tech: TechZig, Confidence: 95, IsPrimary: false},
	{File: "pom.xml", Tech: TechJava, Confidence: 100, IsPrimary: true},
	{File: "build.gradle", Tech: TechJava, Confidence: 95, IsPrimary: true},
	{File: "build.gradle.kts", Tech: TechKotlin, Confidence: 100, IsPrimary: true},
	{File: "mix.exs", Tech: TechUnknown, Confidence: 100, IsPrimary: true}, // Elixir — unsupported but detected
	{File: "Package.swift", Tech: TechUnknown, Confidence: 100, IsPrimary: true}, // Swift

	// ── Tier 2: Strong indicators ─────────────────────────────────────────────
	{File: "package.json", Tech: TechNode, Confidence: 95, IsPrimary: true},
	{File: "pyproject.toml", Tech: TechPython, Confidence: 95, IsPrimary: true},
	{File: "setup.py", Tech: TechPython, Confidence: 90, IsPrimary: true},
	{File: "requirements.txt", Tech: TechPython, Confidence: 85, IsPrimary: false},
	{File: "setup.cfg", Tech: TechPython, Confidence: 80, IsPrimary: false},
	{File: "Pipfile", Tech: TechPython, Confidence: 88, IsPrimary: true},
	{File: "poetry.lock", Tech: TechPython, Confidence: 88, IsPrimary: false},
	{File: "CMakeLists.txt", Tech: TechCpp, Confidence: 90, IsPrimary: true},
	{File: "meson.build", Tech: TechCpp, Confidence: 88, IsPrimary: true},
	{File: "configure.ac", Tech: TechC, Confidence: 85, IsPrimary: true},
	{File: "Makefile", Tech: TechC, Confidence: 60, IsPrimary: false}, // too common to be primary alone
	{File: "GNUmakefile", Tech: TechC, Confidence: 60, IsPrimary: false},
	{File: "Gemfile", Tech: TechRuby, Confidence: 95, IsPrimary: true},
	{File: "*.gemspec", Tech: TechRuby, Confidence: 90, IsPrimary: true},
	{File: "*.csproj", Tech: TechDotnet, Confidence: 95, IsPrimary: true},
	{File: "*.sln", Tech: TechDotnet, Confidence: 90, IsPrimary: true},
	{File: "cabal.project", Tech: TechHaskell, Confidence: 95, IsPrimary: true},
	{File: "stack.yaml", Tech: TechHaskell, Confidence: 95, IsPrimary: true},

	// ── Tier 3: Secondary / additive signals ─────────────────────────────────
	{File: "docker-compose.yml", Tech: TechDocker, Confidence: 80, IsPrimary: false},
	{File: "docker-compose.yaml", Tech: TechDocker, Confidence: 80, IsPrimary: false},
	{File: "Dockerfile", Tech: TechDocker, Confidence: 70, IsPrimary: false},
	{File: "yarn.lock", Tech: TechNode, Confidence: 75, IsPrimary: false},
	{File: "pnpm-lock.yaml", Tech: TechNode, Confidence: 75, IsPrimary: false},
	{File: "package-lock.json", Tech: TechNode, Confidence: 75, IsPrimary: false},
	{File: ".stow-local-ignore", Tech: TechDotfile, Confidence: 90, IsPrimary: true},
	{File: ".chezmoi.yaml", Tech: TechDotfile, Confidence: 95, IsPrimary: true},
	{File: ".chezmoi.toml", Tech: TechDotfile, Confidence: 95, IsPrimary: true},
}

// dotfileIndicators are directory names commonly found in dotfile repos.
// If a repo has no primary manifest AND contains these, it's a dotfile repo.
var dotfileIndicators = []string{
	".config", "nvim", "zsh", "bash", "fish", "tmux",
	"hypr", "waybar", "kitty", "alacritty", "i3", "sway",
	"rofi", "dunst", "polybar", "eww",
}

// docfileIndicators are filenames that suggest a documentation-only repo.
var docfileIndicators = []string{
	"README.md", "readme.md", "DOCS.md", "docs.md",
	"index.md", "book.toml", // mdBook
	"mkdocs.yml",            // MkDocs
	"_config.yml",           // Jekyll
}

// FoundManifest is a manifest file that was actually found in the repository.
type FoundManifest struct {
	Entry    ManifestEntry
	FullPath string // absolute path to the file
	Depth    int    // 0 = root, 1 = one level deep, etc.
}

// ScanManifests walks the repo (up to maxDepth levels deep) and returns
// every manifest file found, ordered by confidence descending.
// maxDepth=2 covers root + one subdir — enough for monorepos without being slow.
func ScanManifests(repoPath string, maxDepth int) ([]FoundManifest, error) {
	var found []FoundManifest

	err := walkDepth(repoPath, 0, maxDepth, func(path string, depth int) {
		for _, entry := range manifestTable {
			filename := filepath.Base(path)

			// Handle glob patterns (e.g. "*.gemspec", "*.csproj")
			if strings.HasPrefix(entry.File, "*") {
				ext := strings.TrimPrefix(entry.File, "*")
				if strings.HasSuffix(filename, ext) {
					found = append(found, FoundManifest{
						Entry:    entry,
						FullPath: path,
						Depth:    depth,
					})
				}
				continue
			}

			// Exact filename match
			if filename == entry.File {
				found = append(found, FoundManifest{
					Entry:    entry,
					FullPath: path,
					Depth:    depth,
				})
			}
		}
	})

	return found, err
}

// walkDepth is a depth-limited directory walker.
// It calls fn for every regular file found within maxDepth levels.
func walkDepth(root string, currentDepth, maxDepth int, fn func(path string, depth int)) error {
	if currentDepth > maxDepth {
		return nil
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		fullPath := filepath.Join(root, entry.Name())

		// Skip hidden dirs and common non-source dirs
		if entry.IsDir() {
			if shouldSkipDir(entry.Name()) {
				continue
			}
			walkDepth(fullPath, currentDepth+1, maxDepth, fn) //nolint:errcheck
			continue
		}

		fn(fullPath, currentDepth)
	}

	return nil
}

// shouldSkipDir returns true for directories that should never be scanned.
func shouldSkipDir(name string) bool {
	skip := map[string]bool{
		".git":         true,
		"node_modules": true,
		"vendor":       true,
		"target":       true, // Rust build output
		"build":        true,
		"dist":         true,
		".gradle":      true,
		"__pycache__":  true,
		".venv":        true,
		"venv":         true,
		".tox":         true,
		"zig-out":      true,
		"zig-cache":    true,
		".cloneable":   true,
	}
	return skip[name]
}

// isDotfileRepo checks whether a repo looks like a dotfile collection
// by looking for known config directory names.
func isDotfileRepo(repoPath string) bool {
	entries, err := os.ReadDir(repoPath)
	if err != nil {
		return false
	}

	hits := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		for _, indicator := range dotfileIndicators {
			if strings.EqualFold(entry.Name(), indicator) {
				hits++
			}
		}
	}
	// 2+ known config dirs = very likely a dotfile repo
	return hits >= 2
}

// isDocsRepo checks whether a repo is documentation-only.
// This is intentionally strict — we only trigger on explicit doc site
// manifests (mkdocs.yml, book.toml, _config.yml) OR on repos that have
// no code manifests and are primarily markdown files. The latter catches
// repos like build-your-own-x that are pure README collections.
func isDocsRepo(repoPath string, hasBuildable bool) bool {
	entries, err := os.ReadDir(repoPath)
	if err != nil {
		return false
	}

	// Only explicit doc site manifests trigger docs mode
	explicitDocManifests := map[string]bool{
		"book.toml":   true, // mdBook
		"mkdocs.yml":  true, // MkDocs
		"mkdocs.yaml": true,
		"_config.yml": true, // Jekyll (only if no other manifests)
		"docusaurus.config.js": true,
		"docusaurus.config.ts": true,
	}

	for _, entry := range entries {
		if explicitDocManifests[entry.Name()] {
			return true
		}
	}

	// If the repo has a primary buildable technology, do not fall back to heuristics.
	if hasBuildable {
		return false
	}

	// Fallback: if the repo has a README.md and the majority of files are
	// markdown, treat it as a documentation repo. This catches repos like
	// build-your-own-x, awesome-* lists, etc.
	hasReadme := false
	mdCount := 0
	totalFiles := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(entry.Name())
		// Ignore cloneable's own log file so it doesn't break the heuristic
		if name == "install.logs" {
			continue
		}
		totalFiles++
		if name == "readme.md" || name == "readme.markdown" {
			hasReadme = true
		}
		if strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".markdown") ||
			strings.HasSuffix(name, ".rst") || strings.HasSuffix(name, ".txt") {
			mdCount++
		}
	}

	// A repo with a README and where >=50% of root files are docs
	if hasReadme && totalFiles > 0 && mdCount*2 >= totalFiles {
		return true
	}

	// A repo with just a README and maybe a LICENSE — still docs
	if hasReadme && totalFiles <= 3 {
		return true
	}

	return false
}
