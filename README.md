```
 ██████╗██╗      ██████╗ ███╗   ██╗███████╗ █████╗ ██████╗ ██╗     ███████╗
██╔════╝██║     ██╔═══██╗████╗  ██║██╔════╝██╔══██╗██╔══██╗██║     ██╔════╝
██║     ██║     ██║   ██║██╔██╗ ██║█████╗  ███████║██████╔╝██║     █████╗
██║     ██║     ██║   ██║██║╚██╗██║██╔══╝  ██╔══██║██╔══██╗██║     ██╔══╝
╚██████╗███████╗╚██████╔╝██║ ╚████║███████╗██║  ██║██████╔╝███████╗███████╗
 ╚═════╝╚══════╝ ╚═════╝ ╚═╝  ╚═══╝╚══════╝╚═╝  ╚═╝╚═════╝ ╚══════╝╚══════╝
```
> Git Repository Master

---

## Install

**Linux / macOS (beta)** — run in terminal:
```sh
curl -fsSL https://raw.githubusercontent.com/manansati/cloneable/main/scripts/install.sh | sudo sh
```

**Windows** — Under Development

---

## Usage

```
cloneable <git-url>    Clone, install Dependency, and Install Globally
cloneable              Browse Trending Repositries

Commands:
  clone <url>    Clone only
  search <query> Search GitHub Interactively
  info <url>     Technology Breakdown
  list           List Installed Repository
  remove <name>  Remove an Installation
  update         Update Cloneable

Flags:
  -r, --run      Launch the Current Repository
  -f, --fix      Fix broken Dependencies
  -i, --info     Technology Breakdown (Current Folder)
  -l, --logs     View Install Logs
  -v, --version  Print Version
  -h, --help     Show this Help
```

---

## Supported Technologies

| Technology | Detection |
|---|---|
| **Go** | `go.mod` |
| **Rust** | `Cargo.toml` |
| **Node.js** | `package.json` |
| **Python** | `pyproject.toml`, `setup.py`, `requirements.txt` |
| **C/C++** | `CMakeLists.txt`, `meson.build`, `Makefile` |
| **Zig** | `build.zig` |
| **Java/Kotlin** | `pom.xml`, `build.gradle` | 
| **Flutter/Dart** | `pubspec.yaml` |
| **Ruby** | `Gemfile` |
| **.NET** | `*.csproj`, `*.sln` |
| **Haskell** | `stack.yaml`, `cabal.project` | 
| **Docker** | `docker-compose.yml`, `Dockerfile` |
| **Documentation** | Markdown files |
| **Dotfiles** | Config Directories |
| **Shell Scripts** | `.sh` files |

## Package Managers Used

**Linux:** apt · dnf · pacman · zypper · apk · xbps · yay · paru · snap · flatpak <br>
**macOS:** Homebrew <br>
**Windows:** winget · Chocolatey · Scoop

---

## Troubleshooting

### Under Development

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
