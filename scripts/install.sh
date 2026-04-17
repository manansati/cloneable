#!/usr/bin/env bash
# Cloneable installer for Linux and macOS
# Usage: curl -fsSL https://raw.githubusercontent.com/manansati/cloneable/main/scripts/install.sh | sh
set -e

REPO="manansati/cloneable"
BINARY="cloneable"
INSTALL_DIR="$HOME/.local/bin"

# ── Colours ───────────────────────────────────────────────────────────────────
SAFFRON="\033[38;2;255;140;0m"
GREEN="\033[38;2;0;230;118m"
RED="\033[38;2;255;82;82m"
GRAY="\033[38;2;136;136;136m"
RESET="\033[0m"

info()    { printf "  ${SAFFRON}→${RESET}  %s\n" "$1"; }
success() { printf "  ${GREEN}✓${RESET}  %s\n" "$1"; }
error()   { printf "  ${RED}✗${RESET}  %s\n" "$1" >&2; exit 1; }
muted()   { printf "  ${GRAY}%s${RESET}\n" "$1"; }

# ── Detect OS and architecture ────────────────────────────────────────────────
detect_os() {
  case "$(uname -s)" in
    Linux*)  echo "linux" ;;
    Darwin*) echo "darwin" ;;
    *)       error "Unsupported OS: $(uname -s)" ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *) error "Unsupported architecture: $(uname -m)" ;;
  esac
}

# ── Fetch latest version from GitHub ─────────────────────────────────────────
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
    error "curl or wget is required to download Cloneable"
  fi
}

# ── Download binary ───────────────────────────────────────────────────────────
download_binary() {
  local version="$1"
  local os="$2"
  local arch="$3"

  local filename="${BINARY}_${version}_${os}_${arch}.tar.gz"
  local url="https://github.com/${REPO}/releases/download/v${version}/${filename}"
  local tmp_dir
  tmp_dir="$(mktemp -d)"

  info "Downloading Cloneable v${version} for ${os}/${arch}..."

  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "${tmp_dir}/${filename}" || \
      error "Download failed. Check your internet connection."
  else
    wget -qO "${tmp_dir}/${filename}" "$url" || \
      error "Download failed. Check your internet connection."
  fi

  # Extract
  tar -xzf "${tmp_dir}/${filename}" -C "${tmp_dir}"

  # Move binary to install dir
  mkdir -p "$INSTALL_DIR"
  mv "${tmp_dir}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
  chmod +x "${INSTALL_DIR}/${BINARY}"

  # Cleanup
  rm -rf "$tmp_dir"
}

# ── Ensure install dir is in PATH ─────────────────────────────────────────────
ensure_in_path() {
  if echo "$PATH" | grep -q "$INSTALL_DIR"; then
    return
  fi

  local shell_config=""
  case "$SHELL" in
    */zsh)  shell_config="$HOME/.zshrc" ;;
    */fish) shell_config="$HOME/.config/fish/config.fish" ;;
    *)      shell_config="$HOME/.bashrc" ;;
  esac

  if [ -n "$shell_config" ] && ! grep -q "$INSTALL_DIR" "$shell_config" 2>/dev/null; then
    echo "" >> "$shell_config"
    echo "# Added by Cloneable installer" >> "$shell_config"
    echo "export PATH=\"$INSTALL_DIR:\$PATH\"" >> "$shell_config"
    muted "Added $INSTALL_DIR to PATH in $shell_config"
    muted "Restart your terminal or run: source $shell_config"
  fi
}

# ── Main ──────────────────────────────────────────────────────────────────────
main() {
  printf "\n"
  printf "  ${SAFFRON}Installing Cloneable${RESET}\n"
  printf "\n"

  local os arch version
  os="$(detect_os)"
  arch="$(detect_arch)"

  info "Detected: ${os}/${arch}"

  version="$(fetch_latest_version)"
  if [ -z "$version" ]; then
    error "Could not fetch latest version from GitHub"
  fi

  download_binary "$version" "$os" "$arch"
  ensure_in_path

  printf "\n"
  success "Cloneable v${version} installed to ${INSTALL_DIR}/${BINARY}"
  printf "\n"
  muted "Usage:"
  muted "  cloneable https://github.com/user/repo"
  muted "  cloneable search ghostty"
  muted "  cloneable --help"
  printf "\n"
}

main "$@"
