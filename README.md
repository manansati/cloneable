# Cloneable

> Clone any GitHub repository, install all its dependencies, and launch it — automatically.

```
 ██████╗██╗      ██████╗ ███╗   ██╗███████╗ █████╗ ██████╗ ██╗     ███████╗
██╔════╝██║     ██╔═══██╗████╗  ██║██╔════╝██╔══██╗██╔══██╗██║     ██╔════╝
██║     ██║     ██║   ██║██╔██╗ ██║█████╗  ███████║██████╔╝██║     █████╗
██║     ██║     ██║   ██║██║╚██╗██║██╔══╝  ██╔══██║██╔══██╗██║     ██╔══╝
╚██████╗███████╗╚██████╔╝██║ ╚████║███████╗██║  ██║██████╔╝███████╗███████╗
 ╚═════╝╚══════╝ ╚═════╝ ╚═╝  ╚═══╝╚══════╝╚═╝  ╚═╝╚═════╝ ╚══════╝╚══════╝
```

---

## Install

**Linux / macOS** — run in terminal:
```sh
curl -fsSL https://raw.githubusercontent.com/manansati/cloneable/main/scripts/install.sh | sudo sh
```

**Windows** — run in PowerShell as Administrator:
```powershell
irm https://raw.githubusercontent.com/manansati/cloneable/main/scripts/install.ps1 | iex
```

That's it. `cloneable` will be available everywhere immediately after install.

> **Already have the source code?** Just run `sudo sh scripts/install.sh` from the repo folder — same result.

---

## Usage

```
cloneable <git-url>    Clone, install, and launch a repository
cloneable              Run inside an already-cloned repository

Commands:
  clone <url>    Clone only
  explore        Trending repositories
  search <query> Search GitHub interactively
  info [url]     Language breakdown
  list           List installed repos
  remove <name>  Remove an installation
  update         Update Cloneable

Flags:
  -r, --run      Launch the current repo
  -f, --fix      Fix broken dependencies
  -i, --info     Language breakdown (current repo)
  -l, --logs     View install logs
  -v, --version  Print version
  -h, --help     Show help
```

---

## How It Works

Cloneable follows a three-phase pipeline for every repository:

### Phase I — Clone
- Clones the repository with full progress tracking
- Handles duplicate directories (replace / reuse)
- Supports GitHub authentication via `GITHUB_TOKEN`

### Phase II — Detect & Install
1. **Tech Detection**: Scans manifest files (`go.mod`, `Cargo.toml`, `package.json`, `pyproject.toml`, etc.) to determine the primary language
2. **Environment Setup**: Creates an isolated environment (`.venv` for Python, `node_modules` for Node.js, etc.)
3. **System Dependencies**: Installs system packages via your OS package manager (`apt`, `pacman`, `dnf`, `brew`, `winget`, etc.)
4. **Language Dependencies**: Installs language-level packages (`pip install`, `npm install`, `cargo fetch`, `go mod download`, etc.)

### Phase III — Build & Launch
1. **Build**: Compiles the project (for compiled languages like Go, Rust, C/C++, Zig)
2. **Global Install**: Installs the binary to `~/.local/bin` (for Python, symlinks directly from the `.venv` to avoid nested environments)
3. **Launch**: Runs the application interactively

---

## Python & PEP 668 Compliance

Modern Linux distributions enforce [PEP 668](https://peps.python.org/pep-0668/), which prevents `pip install` from modifying the system Python environment. Cloneable handles this transparently:

1. **Virtual Environment**: Every Python project gets its own `.venv` inside the repo directory — system Python is never touched
2. **Global Symlinking**: When installing a Python CLI tool globally, Cloneable installs it via the `pip` inside the isolated `.venv`, and then symlinks the executable to `~/.local/bin` (avoids nested venvs that `pipx` would create)
3. **Activation Scripts**: After setup, activation scripts are generated:
   - `cloneable-activate.sh` — for Bash/Zsh (source it to activate the venv)
   - `cloneable-activate.fish` — for Fish shell
   - `cloneable-activate.bat` — for Windows CMD

**Fallback chain** for venv creation:
1. `python3 -m venv` (standard)
2. Install `python3-venv` system package and retry
3. `virtualenv` (pip-installable, works everywhere)
4. `python3 -m venv --without-pip` + bootstrap pip manually

---

## Dotfiles & Configuration Repos

Cloneable auto-detects dotfile repositories by looking for known config directories (`nvim`, `zsh`, `tmux`, `hypr`, `kitty`, `alacritty`, `i3`, `sway`, etc.) and dotfile manager config files.

**Supported dotfile managers:**
- **chezmoi** — `.chezmoi.yaml` / `.chezmoi.toml` / `.chezmoiroot`
- **yadm** — `.yadm` / `.config/yadm`
- **GNU stow** — `.stow-local-ignore` or detected config directories
- **Makefile** — `make install` for Makefile-based dotfile repos
- **Install scripts** — `install.sh`, `setup.sh`, `bootstrap.sh` (plus `.ps1` / `.bat` on Windows)

---

## Documentation Repos

Repos that are primarily markdown (like awesome lists, tutorials, or spec documents) are detected automatically. Cloneable will:

1. Find the best markdown file to display (`README.md`, `docs/index.md`, etc.)
2. Render it beautifully in the terminal using:
   - **glow** (if installed — best quality)
   - **mdcat** (if installed)
   - **Built-in renderer** (always available, no external tools needed) — with syntax highlighting, styled headers, code blocks, tables, links, and proper ANSI colors

---

## Supported Technologies

| Technology | Detection | Build | Global Install |
|---|---|---|---|
| **Go** | `go.mod` | `go build` | `go install ./...` |
| **Rust** | `Cargo.toml` | `cargo build --release` | `cargo install --path .` |
| **Node.js** | `package.json` | `npm/yarn/pnpm build` | `npm install -g .` |
| **Python** | `pyproject.toml`, `setup.py`, `requirements.txt` | venv + pip install | `.venv` to `~/.local/bin` symlink |
| **C/C++** | `CMakeLists.txt`, `meson.build`, `Makefile` | cmake/meson/make | `cmake --install` / `make install` |
| **Zig** | `build.zig` | `zig build` | `zig build install -p ~/.local` |
| **Java/Kotlin** | `pom.xml`, `build.gradle` | `gradle build` / `mvn package` | — |
| **Flutter/Dart** | `pubspec.yaml` | `flutter build` | — |
| **Ruby** | `Gemfile` | `bundle install` | — |
| **.NET** | `*.csproj`, `*.sln` | `dotnet build` | `dotnet tool install --global` |
| **Haskell** | `stack.yaml`, `cabal.project` | `stack build` / `cabal build` | `stack install` / `cabal install` |
| **Docker** | `docker-compose.yml`, `Dockerfile` | `docker compose pull` | `docker compose up` |
| **Dotfiles** | Config directories | — | stow / chezmoi / install scripts |
| **Documentation** | Markdown files | — | Built-in terminal renderer |
| **Shell Scripts** | `.sh` files | — | `bash <script>` |

## Supported Package Managers

**Linux:** apt · dnf · pacman · zypper · apk · xbps · yay · paru · snap · flatpak
**macOS:** Homebrew (auto-installed if missing)
**Windows:** winget · Chocolatey · Scoop

Package names are automatically mapped across managers (e.g. `python3` on apt → `python` on pacman → `Python.Python.3` on winget).

---

## Troubleshooting

### Python install fails with "externally-managed-environment"
This is PEP 668. Cloneable handles it automatically by using virtual environments and symlinks. If you still see this error:
```sh
cloneable --fix   # inside the repo directory
```

### Build fails with missing system libraries
Cloneable scans manifest files for dependency hints, but some projects need unlisted system packages. Install them manually and re-run:
```sh
sudo apt install <missing-package>  # or your distro's equivalent
cloneable --fix
```

### `cloneable --fix` — nuclear option
Removes all cached build state and reinstalls from scratch:
```sh
cd ~/projects/some-repo
cloneable --fix
```

---

## For developers — build from source

```sh
git clone https://github.com/manansati/cloneable
cd cloneable
go mod tidy
go build -o cloneable .
sudo mv cloneable /usr/local/bin/cloneable

# Verify
cloneable --version
```

---

## Publishing a release (for maintainers)

Install goreleaser once:
```sh
go install github.com/goreleaser/goreleaser/v2@latest
```

Tag and publish:
```sh
git tag v0.1.0
git push origin v0.1.0
GITHUB_TOKEN=your_token goreleaser release --clean
```

After this, the install script will download pre-built binaries automatically — no Go required for users.

---

MIT © [manansati](https://github.com/manansati)
