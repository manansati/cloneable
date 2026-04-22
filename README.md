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
  search <query> Search GitHub interactively
  info [url]     Language breakdown
  list           List installed repos
  remove <n>     Remove an installation
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

## Supported Technologies

Go · Rust · Node.js · Python · C/C++ · Zig · Flutter/Dart · Java/Kotlin · Ruby · .NET · Haskell · Docker · Dotfiles

## Supported Package Managers

**Linux:** apt · dnf · pacman · zypper · apk · xbps · yay · paru · snap · flatpak  
**macOS:** Homebrew (auto-installed if missing)  
**Windows:** winget · Chocolatey · Scoop

---

MIT © [manansati](https://github.com/manansati)
