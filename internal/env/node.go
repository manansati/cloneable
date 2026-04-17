package env

import (
	"os"
	"path/filepath"
)

// setupNode prepares the Node.js environment.
// Node uses local node_modules by default — no extra setup needed.
// What we DO handle:
//   - Setting NODE_PATH so the modules are found
//   - Detecting the correct package manager (npm / yarn / pnpm)
//   - Symlinking the bin after install
func (e *Environment) setupNode(log LogWriter) error {
	e.EnvDir = filepath.Join(e.RepoPath, "node_modules")
	// node_modules is created by npm/yarn/pnpm install — nothing to do here
	if log != nil {
		log("[node] environment ready — node_modules will be created during dependency install")
	}
	return nil
}

// NodePackageManager returns the package manager to use for this Node project.
// Detection is lockfile-first — never guesses.
func (e *Environment) NodePackageManager() string {
	switch {
	case fileExistsInRepo(e.RepoPath, "pnpm-lock.yaml"):
		return "pnpm"
	case fileExistsInRepo(e.RepoPath, "yarn.lock"):
		return "yarn"
	default:
		return "npm"
	}
}

// NodeInstallCmd returns the correct install command for this project.
func (e *Environment) NodeInstallCmd() []string {
	pm := e.NodePackageManager()
	switch pm {
	case "pnpm":
		return []string{"pnpm", "install", "--frozen-lockfile"}
	case "yarn":
		return []string{"yarn", "install", "--frozen-lockfile"}
	default:
		return []string{"npm", "install", "--prefer-offline"}
	}
}

// NodeBinDir returns the path to node_modules/.bin where CLIs live.
func (e *Environment) NodeBinDir() string {
	return filepath.Join(e.RepoPath, "node_modules", ".bin")
}

// NodeEnvVars returns environment variables for running Node commands
// with the local node_modules in scope.
func (e *Environment) NodeEnvVars() []string {
	return []string{
		"NODE_PATH=" + filepath.Join(e.RepoPath, "node_modules"),
		"PATH=" + e.NodeBinDir() + string(os.PathListSeparator) + os.Getenv("PATH"),
	}
}

// NodeGlobalBinName reads package.json to find the "bin" field
// and returns the binary name(s) declared for global install.
// Returns empty slice if no bin field is found.
func (e *Environment) NodeGlobalBinName() []string {
	pkgJSON := filepath.Join(e.RepoPath, "package.json")
	data, err := os.ReadFile(pkgJSON)
	if err != nil {
		return nil
	}

	// Simple extraction — full JSON parsing happens in Phase III
	// This is just for the env layer to know what binary to symlink.
	names := extractBinNames(string(data))
	return names
}

// extractBinNames extracts binary names from a package.json "bin" field.
// Handles both string and object forms:
//
//	"bin": "myapp"
//	"bin": { "myapp": "dist/cli.js", "myapp2": "dist/cli2.js" }
func extractBinNames(content string) []string {
	// Find "bin" key
	binIdx := indexOf(content, `"bin"`)
	if binIdx < 0 {
		return nil
	}

	// Find the value after "bin":
	colonIdx := indexOf(content[binIdx:], `:`)
	if colonIdx < 0 {
		return nil
	}
	rest := trimSpace(content[binIdx+colonIdx+1:])

	var names []string

	if len(rest) > 0 && rest[0] == '"' {
		// String form: "bin": "myapp" — use the package name
		nameIdx := indexOf(content, `"name"`)
		if nameIdx >= 0 {
			nameColon := indexOf(content[nameIdx:], `:`)
			if nameColon >= 0 {
				nameRest := trimSpace(content[nameIdx+nameColon+1:])
				if len(nameRest) > 0 && nameRest[0] == '"' {
					end := indexOf(nameRest[1:], `"`)
					if end >= 0 {
						names = append(names, nameRest[1:end+1])
					}
				}
			}
		}
	} else if len(rest) > 0 && rest[0] == '{' {
		// Object form: extract all keys
		depth := 0
		inStr := false
		i := 0
		for i < len(rest) {
			ch := rest[i]
			if ch == '"' && !inStr {
				inStr = true
				i++
				end := indexOf(rest[i:], `"`)
				if end >= 0 {
					key := rest[i : i+end]
					// Only add top-level keys (depth == 1 = inside the bin object)
					if depth == 1 {
						names = append(names, key)
					}
					i += end + 1
				}
				inStr = false
				continue
			}
			if ch == '{' {
				depth++
			} else if ch == '}' {
				depth--
				if depth == 0 {
					break
				}
			}
			i++
		}
	}

	return names
}

// ── small string helpers (no regexp to keep binary size small) ────────────────

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func trimSpace(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	return s[i:]
}

func fileExistsInRepo(repoPath, rel string) bool {
	_, err := os.Stat(filepath.Join(repoPath, rel))
	return err == nil
}

// EnsurePackageManager checks that the detected package manager is installed.
// If pnpm or yarn is required but missing, installs it via npm.
func (e *Environment) EnsurePackageManager(log LogWriter) error {
	pm := e.NodePackageManager()
	switch pm {
	case "pnpm":
		if !commandExists("pnpm") {
			if log != nil {
				log("[node] pnpm not found — installing via npm")
			}
			return run(log, "npm", "install", "-g", "pnpm")
		}
	case "yarn":
		if !commandExists("yarn") {
			if log != nil {
				log("[node] yarn not found — installing via npm")
			}
			return run(log, "npm", "install", "-g", "yarn")
		}
	}
	return nil
}

// run executes a command and logs output. Reuses the interface.go helper
// via the package-level function defined in helpers.go.
func run(log LogWriter, name string, args ...string) error {
	return runCmd(log, name, args...)
}

// commandExists checks if a binary is in PATH.
func commandExists(name string) bool {
	return binaryExists(name)
}

// NodeStartScript returns the npm/yarn/pnpm start script for this project,
// or nil if no start script is found in package.json.
func (e *Environment) NodeStartScript() []string {
	pm := e.NodePackageManager()
	pkgJSON := filepath.Join(e.RepoPath, "package.json")
	data, err := os.ReadFile(pkgJSON)
	if err != nil {
		return []string{pm, "start"}
	}

	content := string(data)
	// Check if "start" script is defined
	if indexOf(content, `"start"`) >= 0 {
		return []string{pm, "start"}
	}
	// Check for "dev" script (common in Vite/Next.js projects)
	if indexOf(content, `"dev"`) >= 0 {
		return []string{pm, "run", "dev"}
	}
	// Fallback
	return []string{pm, "start"}
}

// NodeMainEntry returns the "main" field from package.json,
// used as a fallback run command: node <main>.
func (e *Environment) NodeMainEntry() string {
	pkgJSON := filepath.Join(e.RepoPath, "package.json")
	data, err := os.ReadFile(pkgJSON)
	if err != nil {
		return ""
	}

	content := string(data)
	mainIdx := indexOf(content, `"main"`)
	if mainIdx < 0 {
		return ""
	}
	colonIdx := indexOf(content[mainIdx:], `:`)
	if colonIdx < 0 {
		return ""
	}
	rest := trimSpace(content[mainIdx+colonIdx+1:])
	if len(rest) == 0 || rest[0] != '"' {
		return ""
	}
	end := indexOf(rest[1:], `"`)
	if end < 0 {
		return ""
	}
	return rest[1 : end+1]
}


