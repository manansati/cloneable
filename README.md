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

No manual setup. No reading READMEs. Just run it.

---

## Install

**Linux / macOS**
```sh
curl -fsSL https://raw.githubusercontent.com/manansati/cloneable/main/scripts/install.sh | sh
```

**Windows (PowerShell)**
```powershell
irm https://raw.githubusercontent.com/manansati/cloneable/main/scripts/install.ps1 | iex
```

---

## Usage

| Command | Description |
|---|---|
| `cloneable <git-url>` | Clone, install dependencies, and launch |
| `cloneable` | Run inside an already-cloned repo |
| `cloneable clone <git-url>` | Clone only (no install, no launch) |
| `cloneable --run` | Launch the app in the current repo |
| `cloneable --fix` | Fix broken dependencies and reinstall |
| `cloneable --stats` | Show language breakdown of current repo |
| `cloneable stats <git-url>` | Language breakdown without cloning |
| `cloneable search <query>` | Search GitHub and launch interactively |
| `cloneable list` | List all Cloneable-managed installs |
| `cloneable remove <name>` | Remove an install cleanly |
| `cloneable --logs` | View installation logs |
| `cloneable update` | Update Cloneable itself |
| `cloneable --version` | Show version info |

---

## Examples

```sh
# Install and launch Neovim from source
cloneable https://github.com/neovim/neovim

# Try a terminal tool
cloneable https://github.com/Peltoche/lsd

# Search for repos
cloneable search "file manager"

# See what languages a repo uses (no clone needed)
cloneable stats https://github.com/microsoft/vscode

# Fix a broken install
cd ~/projects/ghostty
cloneable --fix
```

---

## Supported Technologies

| Language | Install Method |
|---|---|
| Go | `go install ./...` |
| Rust | `cargo install --path .` |
| Node.js | `npm` / `yarn` / `pnpm` |
| Python | `pip install` into `.venv` (isolated) |
| C / C++ | `cmake` / `meson` / `make` |
| Zig | `zig build install` |
| Flutter / Dart | `flutter pub get` |
| Java / Kotlin | `gradle` / `maven` |
| Ruby | `bundle install` |
| .NET | `dotnet restore` |
| Haskell | `stack` / `cabal` |
| Docker | `docker-compose up` |
| Dotfiles | `stow` / `chezmoi` / `install.sh` |

---

## Supported Package Managers

**Linux:** apt, dnf, pacman, zypper, apk, xbps, yay, paru, snap, flatpak  
**macOS:** Homebrew (auto-installed if missing)  
**Windows:** winget, Chocolatey, Scoop

---

## Environment Isolation

Cloneable never pollutes your system:

- **Python** → `.venv/` inside the repo, binary symlinked globally
- **Node.js** → local `node_modules/`, bin symlinked globally
- **Go** → `go install` (uses `~/go/bin`, already isolated)
- **Rust** → `cargo install --path .` (uses `~/.cargo/bin`)
- **C/C++** → installs to `~/.local/` prefix

Run `cloneable remove <name>` to cleanly undo everything.

---

## cloneable.yaml

Repo authors can ship a `cloneable.yaml` to give Cloneable exact instructions:

```yaml
name: myapp
type: go
depends_on: [ffmpeg, cmake]
build: make build
install: make install
run: ./myapp
global_binary: myapp
```

---

## For Developers

```sh
# Clone and build from source
git clone https://github.com/manansati/cloneable
cd cloneable
go mod tidy
go build -o cloneable .
./cloneable --version
```

---

## License

MIT © [manansati](https://github.com/manansati)
