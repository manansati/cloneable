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

**Windows** — Under Development (use WSL/minGW)
```sh
curl -fsSL https://raw.githubusercontent.com/manansati/cloneable/main/scripts/install.sh | sudo sh
```
---

## Usage

```
Usage:
  cloneable <git-url>    Clone and install dependencies for a repository
  cloneable              Explore trending repositories (or run inside cloned repo)

Commands:
  clone <url>    Clone and install dependencies
  explore        Explore trending repositories
  search <query> Search GitHub interactively
  info [url]     Show language breakdown
  list           List installed repositories
  remove <name>  Remove an installation
  update         Update Cloneable
  login <token>  Set GitHub API token
  uninstall      Uninstall Cloneable
  run            Launch the current repository
  fix            Fix broken dependencies
  logs           View install logs

Flags:
  -r, --run      Launch the current repository
  -f, --fix      Fix broken dependencies
  -i, --info     Language breakdown (current repo)
  -l, --logs     View install logs
  -v, --version  Print version
  -h, --help     Show this help
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

### If facing any issue, please let us know through issue tab, it;s under development 

---

## For Nerds — Build from Source

```sh
git clone https://github.com/manansati/cloneable
cd cloneable
go mod tidy
go build -o cloneable .
sudo mv cloneable /usr/local/bin/cloneable

# Verify
cloneable --version
```
