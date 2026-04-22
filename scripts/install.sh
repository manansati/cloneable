#!/usr/bin/env bash
# Cloneable installer for Linux and macOS
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/manansati/cloneable/main/scripts/install.sh | sudo sh
#
# Installs to /usr/local/bin — already in PATH on every system.
# No PATH editing. No terminal restart. Works immediately.

set -e

REPO="manansati/cloneable"
BINARY="cloneable"
INSTALL_DIR="/usr/local/bin"

SAFFRON="\033[38;2;255;140;0m"
GREEN="\033[38;2;0;230;118m"
RED="\033[38;2;255;82;82m"
GRAY="\033[38;2;136;136;136m"
BOLD="\033[1m"
RESET="\033[0m"

info()    { printf "  ${SAFFRON}→${RESET}  %s\n" "$1"; }
success() { printf "  ${GREEN}${BOLD}✓${RESET}  %s\n" "$1"; }
error()   { printf "\n  ${RED}✗${RESET}  %s\n\n" "$1" >&2; exit 1; }
muted()   { printf "  ${GRAY}%s${RESET}\n" "$1"; }

# ── Root check ────────────────────────────────────────────────────────────────
if [ "$(id -u)" -ne 0 ]; then
  printf "\n  ${RED}✗${RESET}  This installer needs root to write to ${INSTALL_DIR}\n\n"
  printf "  Run with sudo:\n"
  printf "    ${GRAY}curl -fsSL https://raw.githubusercontent.com/${REPO}/main/scripts/install.sh | sudo sh${RESET}\n\n"
  exit 1
fi

# ── Detect platform ───────────────────────────────────────────────────────────
detect_os() {
  case "$(uname -s)" in
    Linux*)  echo "linux"  ;;
    Darwin*) echo "darwin" ;;
    *)       error "Unsupported OS: $(uname -s)" ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64)  echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *)             error "Unsupported CPU: $(uname -m)" ;;
  esac
}

# ── HTTP helpers ──────────────────────────────────────────────────────────────
http_get() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$1" -o "$2" 2>/dev/null; return $?
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$2" "$1" 2>/dev/null; return $?
  else
    error "curl or wget required"
  fi
}

http_stdout() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$1" 2>/dev/null
  elif command -v wget >/dev/null 2>&1; then
    wget -qO- "$1" 2>/dev/null
  fi
}

# ── Fetch latest release version ──────────────────────────────────────────────
# Returns empty string if repo has no releases yet
fetch_version() {
  local api="https://api.github.com/repos/${REPO}/releases/latest"
  local body

  if [ -n "$GITHUB_TOKEN" ]; then
    body=$(curl -fsSL -H "Authorization: Bearer $GITHUB_TOKEN" "$api" 2>/dev/null || true)
  else
    body=$(http_stdout "$api" || true)
  fi

  # No releases yet
  echo "$body" | grep -q '"message"' && echo "" && return

  echo "$body" | grep '"tag_name"' \
    | sed 's/.*"tag_name": *"v\([^"]*\)".*/\1/' \
    | head -1
}

# ── Install pre-built binary ──────────────────────────────────────────────────
install_binary() {
  local version="$1" os="$2" arch="$3"
  local file="${BINARY}_${version}_${os}_${arch}.tar.gz"
  local url="https://github.com/${REPO}/releases/download/v${version}/${file}"
  local tmp; tmp="$(mktemp -d)"

  info "Downloading v${version} for ${os}/${arch}..."

  http_get "$url" "${tmp}/${file}" || { rm -rf "$tmp"; return 1; }

  tar -xzf "${tmp}/${file}" -C "$tmp" 2>/dev/null || { rm -rf "$tmp"; return 1; }

  local bin; bin=$(find "$tmp" -name "$BINARY" -type f | head -1)
  [ -z "$bin" ] && { rm -rf "$tmp"; return 1; }

  install -m 755 "$bin" "${INSTALL_DIR}/${BINARY}"
  rm -rf "$tmp"
  return 0
}

# ── Build from source (fallback when no release exists) ───────────────────────
build_from_source() {
  printf "\n  ${BOLD}Building from source...${RESET}\n"
  muted "(This happens when there are no pre-built releases yet)"
  printf "\n"

  command -v go >/dev/null 2>&1 || \
    error "Go not found. Install from https://go.dev/dl then run this script again."

  command -v git >/dev/null 2>&1 || \
    error "git not found. Install git then run this script again."

  info "Go $(go version | awk '{print $3}') found"

  local tmp; tmp="$(mktemp -d)"

  info "Cloning repository..."
  git clone --depth=1 "https://github.com/${REPO}.git" "${tmp}/src" \
    > /dev/null 2>&1 \
    || error "Could not clone — check your internet connection."

  info "Building binary..."
  cd "${tmp}/src"
  go mod tidy > /dev/null 2>&1
  go build \
    -ldflags "-s -w -X github.com/manansati/cloneable/cmd.Version=dev" \
    -o "${tmp}/${BINARY}" . \
    || error "Build failed."

  install -m 755 "${tmp}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
  cd /
  rm -rf "$tmp"
}

# ── Main ──────────────────────────────────────────────────────────────────────
main() {
  printf "\n"
  printf "  ${SAFFRON}${BOLD}Installing Cloneable${RESET}\n"
  printf "\n"

  local os arch
  os="$(detect_os)"
  arch="$(detect_arch)"
  info "Platform: ${os}/${arch}"

  local version
  version="$(fetch_version)"

  if [ -n "$version" ]; then
    if install_binary "$version" "$os" "$arch"; then
      printf "\n"
      success "Cloneable v${version} installed"
      muted "Location: ${INSTALL_DIR}/${BINARY}"
      printf "\n  Try it:\n"
      printf "    cloneable --help\n\n"
      exit 0
    fi
    muted "Binary download failed — falling back to source build..."
  else
    muted "No releases found — building from source..."
  fi

  build_from_source

  printf "\n"
  success "Cloneable installed"
  muted "Location: ${INSTALL_DIR}/${BINARY}"
  printf "\n  Try it:\n"
  printf "    cloneable --help\n\n"
}

main "$@"
