package env

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/manansati/cloneable/internal/detection"
)

// setupNode prepares the Node.js environment.
// Node uses local node_modules by default — no extra setup needed.
// What we DO handle:
//   - Setting NODE_PATH so the modules are found
//   - Detecting the correct package manager (npm / yarn / pnpm)
//   - Symlinking the bin after install
func (e *Environment) setupNode(log LogWriter) error {
	e.EnvDir = filepath.Join(e.RepoPath, "node_modules")
	
	// On fresh Arch Linux installs, npm tries to install globals to /usr/lib/node_modules
	// which is owned by root. This causes permission errors (exit 243).
	// We force the prefix to ~/.local if we detect an unwritable default prefix.
	home, err := os.UserHomeDir()
	if err == nil {
		localPrefix := filepath.Join(home, ".local")
		if e.OSType == detection.OSLinux {
			if log != nil {
				log("[node] forcing npm prefix to " + localPrefix + " to avoid permission errors")
			}
			// Just set it unconditionally for this run using env vars later
			// For global installs, npm uses prefix
			_ = run(log, "npm", "config", "set", "prefix", localPrefix)
		}
	}

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
// Does NOT use --frozen-lockfile because fresh clones often have stale
// or missing lockfiles that cause install to fail outright.
func (e *Environment) NodeInstallCmd() []string {
	pm := e.NodePackageManager()
	switch pm {
	case "pnpm":
		return []string{"pnpm", "install", "--no-frozen-lockfile"}
	case "yarn":
		return []string{"yarn", "install"}
	default:
		return []string{"npm", "install"}
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

// PackageJSON represents the standard Node.js package.json structure.
type PackageJSON struct {
	Name    string            `json:"name"`
	Main    string            `json:"main"`
	Bin     interface{}       `json:"bin"`
	Scripts map[string]string `json:"scripts"`
}

func parsePackageJSON(path string) (*PackageJSON, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var pkg PackageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, err
	}
	return &pkg, nil
}

// NodeGlobalBinName reads package.json to find the "bin" field
// and returns the binary name(s) declared for global install.
// Returns empty slice if no bin field is found.
func (e *Environment) NodeGlobalBinName() []string {
	pkgJSONPath := filepath.Join(e.RepoPath, "package.json")
	pkg, err := parsePackageJSON(pkgJSONPath)
	if err != nil {
		return nil
	}

	var names []string
	switch v := pkg.Bin.(type) {
	case string:
		// If "bin" is a string, the binary name is the package name
		if pkg.Name != "" {
			names = append(names, pkg.Name)
		}
	case map[string]interface{}:
		for key := range v {
			names = append(names, key)
		}
	}
	return names
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
	pkgJSONPath := filepath.Join(e.RepoPath, "package.json")
	pkg, err := parsePackageJSON(pkgJSONPath)
	if err != nil || pkg.Scripts == nil {
		return []string{pm, "start"}
	}

	if _, ok := pkg.Scripts["start"]; ok {
		return []string{pm, "start"}
	}
	if _, ok := pkg.Scripts["dev"]; ok {
		return []string{pm, "run", "dev"}
	}
	// Fallback
	return []string{pm, "start"}
}

// NodeMainEntry returns the "main" field from package.json,
// used as a fallback run command: node <main>.
func (e *Environment) NodeMainEntry() string {
	pkgJSONPath := filepath.Join(e.RepoPath, "package.json")
	pkg, err := parsePackageJSON(pkgJSONPath)
	if err != nil {
		return ""
	}
	return pkg.Main
}


