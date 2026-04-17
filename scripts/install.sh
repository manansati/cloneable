#!/usr/bin/env bash
# Cloneable installer for Linux and macOS
# Usage: curl -fsSL https://raw.githubusercontent.com/manansati/cloneable/main/scripts/install.sh | sh
set -e

REPO="manansati/cloneable"
BINARY="cloneable"
INSTALL_DIR="$HOME/.local/bin"

SAFFRON="\033[38;2;255;140;0m"
GREEN="\033[38;2;0;230;118m"
RED="\033[38;2;255;82;82m"
GRAY="\033[38;2;136;136;136m"
RESET="\033[0m"

info()    { printf "  ${SAFFRON}→${RESET}  %s\n" "$1"; }
success() { printf "  ${GREEN}✓${RESET}  %s\n" "$1"; }
error()   { printf "  ${RED}✗${RESET}  %s\n" "$1" >&2; exit 1; }
muted()   { printf "  ${GRAY}%s${RESET}\n" "$1"; }

detect_os() {
  case "$(uname -s)" in
    Linux*)  echo "linux" ;;
    Darwin*) echo "darwin" ;;
    *)       error "Unsupported OS: $(uname -s)" ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64)  echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *)             error "Unsupported architecture: $(uname -m)" ;;
  esac
}

fetch_latest_version() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
      | grep '"tag_name"' \
      | sed 's/.*"tag_name": *"v\([^"]*\)".*/\1/'
  elif command -v wget >/dev/null 2>&1; then
    wget -qO- "https://api.github.com/repos/${REPO}/releases/latest" \
      | grep '"tag_name"' \
      | sed 's/.*"tag_name": *"v\([^"]*\)".*/\1/'
  else
    error "curl or wget is required"
  fi
}

download_binary() {
  local version="$1" os="$2" arch="$3"
  local filename="${BINARY}_${version}_${os}_${arch}.tar.gz"
  local url="https://github.com/${REPO}/releases/download/v${version}/${filename}"
  local tmp_dir
  tmp_dir="$(mktemp -d)"

  info "Downloading Cloneable v${version} for ${os}/${arch}..."

  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "${tmp_dir}/${filename}" || error "Download failed"
  else
    wget -qO "${tmp_dir}/${filename}" "$url" || error "Download failed"
  fi

  tar -xzf "${tmp_dir}/${filename}" -C "${tmp_dir}"
  mkdir -p "$INSTALL_DIR"
  mv "${tmp_dir}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
  chmod +x "${INSTALL_DIR}/${BINARY}"
  rm -rf "$tmp_dir"
}

# Write PATH export to every shell config that exists on this system.
# This covers users who switch shells or have multiple installed.
ensure_in_path() {
  local export_line="export PATH=\"${INSTALL_DIR}:\$PATH\""
  local fish_line="fish_add_path ${INSTALL_DIR}"
  local added=0

  # bash
  for f in "$HOME/.bashrc" "$HOME/.bash_profile"; do
    if [ -f "$f" ] && ! grep -q "$INSTALL_DIR" "$f" 2>/dev/null; then
      printf "\n# Added by Cloneable\n%s\n" "$export_line" >> "$f"
      muted "Updated $f"
      added=1
    fi
  done

  # zsh
  if [ -f "$HOME/.zshrc" ] && ! grep -q "$INSTALL_DIR" "$HOME/.zshrc" 2>/dev/null; then
    printf "\n# Added by Cloneable\n%s\n" "$export_line" >> "$HOME/.zshrc"
    muted "Updated ~/.zshrc"
    added=1
  fi

  # fish
  local fish_cfg="$HOME/.config/fish/config.fish"
  if [ -f "$fish_cfg" ] && ! grep -q "$INSTALL_DIR" "$fish_cfg" 2>/dev/null; then
    printf "\n# Added by Cloneable\n%s\n" "$fish_line" >> "$fish_cfg"
    muted "Updated $fish_cfg"
    added=1
  fi

  # .profile (POSIX fallback, covers dash, sh, etc.)
  if [ -f "$HOME/.profile" ] && ! grep -q "$INSTALL_DIR" "$HOME/.profile" 2>/dev/null; then
    printf "\n# Added by Cloneable\n%s\n" "$export_line" >> "$HOME/.profile"
    muted "Updated ~/.profile"
    added=1
  fi

  # If nothing was found at all, print a manual instruction
  if [ "$added" -eq 0 ] && ! echo "$PATH" | grep -q "$INSTALL_DIR"; then
    muted "Add this to your shell config manually:"
    muted "  $export_line"
  fi

  if [ "$added" -gt 0 ]; then
    muted ""
    muted "Restart your terminal (or run: source ~/.bashrc) to use cloneable."
  fi
}

main() {
  printf "\n"
  printf "  ${SAFFRON}Installing Cloneable${RESET}\n"
  printf "\n"

  local os arch version
  os="$(detect_os)"
  arch="$(detect_arch)"
  info "Detected: ${os}/${arch}"

  version="$(fetch_latest_version)"
  [ -z "$version" ] && error "Could not fetch latest version"

  download_binary "$version" "$os" "$arch"
  ensure_in_path

  printf "\n"
  success "Cloneable v${version} installed"
  muted "Binary: ${INSTALL_DIR}/${BINARY}"
  printf "\n"
}

main "$@"
