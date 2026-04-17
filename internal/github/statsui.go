package github

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ── Colour palette for languages ──────────────────────────────────────────────
// These match GitHub's language colours where possible.

var langColors = map[string]lipgloss.Color{
	"Go":         lipgloss.Color("#00ADD8"),
	"Rust":       lipgloss.Color("#DEA584"),
	"Python":     lipgloss.Color("#3572A5"),
	"JavaScript": lipgloss.Color("#F1E05A"),
	"TypeScript": lipgloss.Color("#2B7489"),
	"C":          lipgloss.Color("#555555"),
	"C++":        lipgloss.Color("#F34B7D"),
	"Java":       lipgloss.Color("#B07219"),
	"Kotlin":     lipgloss.Color("#A97BFF"),
	"Swift":      lipgloss.Color("#F05138"),
	"Ruby":       lipgloss.Color("#701516"),
	"PHP":        lipgloss.Color("#4F5D95"),
	"CSS":        lipgloss.Color("#563D7C"),
	"HTML":       lipgloss.Color("#E34C26"),
	"Shell":      lipgloss.Color("#89E051"),
	"Zig":        lipgloss.Color("#EC915C"),
	"Dart":       lipgloss.Color("#00B4AB"),
	"Lua":        lipgloss.Color("#000080"),
	"Haskell":    lipgloss.Color("#5E5086"),
	"Scala":      lipgloss.Color("#DC322F"),
	"Elixir":     lipgloss.Color("#6E4A7E"),
	"Nix":        lipgloss.Color("#7E7EFF"),
	"Makefile":   lipgloss.Color("#427819"),
	"CMake":      lipgloss.Color("#DA3434"),
	"Markdown":   lipgloss.Color("#888888"),
}

// defaultColor is used for languages not in the map.
var defaultColor = lipgloss.Color("#FF8C00") // saffron

// langColor returns the display color for a language.
func langColor(name string) lipgloss.Color {
	if c, ok := langColors[name]; ok {
		return c
	}
	return defaultColor
}

// PrintStats renders a language breakdown in the terminal.
// barWidth is the total width of the progress bar in characters.
func PrintStats(bars []LangBar, repoName string) {
	if len(bars) == 0 {
		fmt.Printf("\n  No language data found for %s\n\n", repoName)
		return
	}

	const barWidth = 30

	// Find the longest language name for alignment
	maxNameLen := 0
	for _, b := range bars {
		if len(b.Name) > maxNameLen {
			maxNameLen = len(b.Name)
		}
	}
	if maxNameLen < 10 {
		maxNameLen = 10
	}

	fmt.Printf("\n  %s\n\n",
		lipgloss.NewStyle().Foreground(lipgloss.Color("#FF8C00")).Bold(true).Render(repoName+" — language breakdown"),
	)

	for _, b := range bars {
		color := langColor(b.Name)
		dot := lipgloss.NewStyle().Foreground(color).Render("●")

		// Pad name
		name := b.Name
		padding := strings.Repeat(" ", maxNameLen-len(name)+1)

		// Percentage string, right-aligned to 5 chars: " 4.2%"
		pct := fmt.Sprintf("%4.1f%%", b.Percent)

		// Progress bar: filled + empty
		filled := int(b.Percent / 100 * float64(barWidth))
		if filled > barWidth {
			filled = barWidth
		}
		empty := barWidth - filled

		filledBar := lipgloss.NewStyle().Foreground(color).Render(strings.Repeat("█", filled))
		emptyBar := lipgloss.NewStyle().Foreground(lipgloss.Color("#3A3A3A")).Render(strings.Repeat("░", empty))

		fmt.Printf("  %s  %s%s%s  %s%s\n",
			dot,
			lipgloss.NewStyle().Foreground(lipgloss.Color("#F2F2F2")).Render(name),
			padding,
			lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Render(pct),
			filledBar,
			emptyBar,
		)
	}

	fmt.Println()
}

// ── Local stats (scan the current repo) ──────────────────────────────────────

// ScanLocalStats walks the repo at repoPath and counts bytes per language
// by file extension. Used when the user runs `cloneable --stats` inside a repo
// without providing a URL.
func ScanLocalStats(repoPath string) (LangStats, error) {
	stats := make(LangStats)

	err := filepath.Walk(repoPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip unreadable files
		}

		// Skip hidden dirs and known non-source dirs
		if info.IsDir() {
			name := info.Name()
			skip := map[string]bool{
				".git": true, "node_modules": true, "vendor": true,
				"target": true, "build": true, "dist": true,
				".venv": true, "venv": true, "__pycache__": true,
				"zig-out": true, "zig-cache": true, ".gradle": true,
				".cloneable": true, ".stack-work": true,
			}
			if skip[name] {
				return filepath.SkipDir
			}
			return nil
		}

		lang := extToLanguage(filepath.Ext(path))
		if lang == "" {
			return nil
		}

		stats[lang] += int(info.Size())
		return nil
	})

	if err != nil {
		return nil, err
	}

	return stats, nil
}

// extToLanguage maps a file extension to a language name.
// Returns "" for unknown/binary extensions.
var extToLanguage = func(ext string) string {
	ext = strings.ToLower(ext)
	m := map[string]string{
		".go":    "Go",
		".rs":    "Rust",
		".py":    "Python",
		".js":    "JavaScript",
		".mjs":   "JavaScript",
		".cjs":   "JavaScript",
		".ts":    "TypeScript",
		".tsx":   "TypeScript",
		".jsx":   "JavaScript",
		".c":     "C",
		".h":     "C",
		".cpp":   "C++",
		".cc":    "C++",
		".cxx":   "C++",
		".hpp":   "C++",
		".java":  "Java",
		".kt":    "Kotlin",
		".kts":   "Kotlin",
		".swift": "Swift",
		".rb":    "Ruby",
		".php":   "PHP",
		".css":   "CSS",
		".scss":  "CSS",
		".sass":  "CSS",
		".html":  "HTML",
		".htm":   "HTML",
		".sh":    "Shell",
		".bash":  "Shell",
		".zsh":   "Shell",
		".fish":  "Shell",
		".zig":   "Zig",
		".dart":  "Dart",
		".lua":   "Lua",
		".hs":    "Haskell",
		".lhs":   "Haskell",
		".scala": "Scala",
		".ex":    "Elixir",
		".exs":   "Elixir",
		".nix":   "Nix",
		".md":    "Markdown",
		".yaml":  "YAML",
		".yml":   "YAML",
		".toml":  "TOML",
		".json":  "JSON",
		".xml":   "XML",
		".sql":   "SQL",
	}
	return m[ext]
}
