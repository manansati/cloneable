package detection

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// GetPythonBinaryNames attempts to parse standard Python packaging files
// (pyproject.toml, setup.cfg, setup.py) to find the actual CLI binary names
// registered by the package. If none are found, it falls back to the repo name.
func GetPythonBinaryNames(repoPath, fallback string) []string {
	var names []string
	seen := make(map[string]bool)

	addName := func(n string) {
		n = strings.TrimSpace(n)
		if n != "" && !seen[n] {
			names = append(names, n)
			seen[n] = true
		}
	}

	// 1. pyproject.toml
	if content, err := os.ReadFile(filepath.Join(repoPath, "pyproject.toml")); err == nil {
		strContent := string(content)
		// Match [project.scripts] or [tool.poetry.scripts]
		// Then match key = "value" lines until the next section
		scriptsRegex := regexp.MustCompile(`(?m)^\[(?:project\.scripts|tool\.poetry\.scripts)\]\n(?:[^\[]*\n)*`)
		match := scriptsRegex.FindString(strContent)
		if match != "" {
			lines := strings.Split(match, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "[") || strings.HasPrefix(line, "#") {
					continue
				}
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 {
					addName(parts[0])
				}
			}
		}
	}

	// 2. setup.cfg
	if content, err := os.ReadFile(filepath.Join(repoPath, "setup.cfg")); err == nil {
		strContent := string(content)
		// Find [options.entry_points] then console_scripts =
		sectionRegex := regexp.MustCompile(`(?ms)^\[options\.entry_points\](.*?)(\n\[|$)`)
		match := sectionRegex.FindStringSubmatch(strContent)
		if len(match) > 1 {
			consoleScriptsRegex := regexp.MustCompile(`(?ms)^console_scripts\s*=\s*\n(.*?)(?:\n\S|$)`)
			csMatch := consoleScriptsRegex.FindStringSubmatch(match[1])
			if len(csMatch) > 1 {
				lines := strings.Split(csMatch[1], "\n")
				for _, line := range lines {
					line = strings.TrimSpace(line)
					parts := strings.SplitN(line, "=", 2)
					if len(parts) == 2 {
						addName(parts[0])
					}
				}
			}
		}
	}

	// 3. setup.py
	if content, err := os.ReadFile(filepath.Join(repoPath, "setup.py")); err == nil {
		strContent := string(content)
		// Look for 'console_scripts': [ 'name=...', ... ]
		// We use a simple regex to find things like "name=..." or 'name = ...' inside console_scripts
		csRegex := regexp.MustCompile(`(?ms)(?:'|")console_scripts(?:'|")\s*:\s*\[(.*?)\]`)
		match := csRegex.FindStringSubmatch(strContent)
		if len(match) > 1 {
			// Find individual entries
			entryRegex := regexp.MustCompile(`(?:'|")([^'"]+?)\s*=`)
			entries := entryRegex.FindAllStringSubmatch(match[1], -1)
			for _, e := range entries {
				addName(e[1])
			}
		}
	}

	if len(names) == 0 {
		return []string{fallback}
	}
	return names
}
