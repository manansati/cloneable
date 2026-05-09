#!/usr/bin/env bash
# Cloneable installer for Linux and macOS
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/manansati/cloneable/main/scripts/install.sh | sudo sh

set -e

REPO="manansati/cloneable"
BINARY="cloneable"
INSTALL_DIR="/usr/local/bin"
BINARY_URL="https://raw.githubusercontent.com/${REPO}/main/binary/${BINARY}"

ORANGE="\033[38;2;255;140;0m"
GREEN="\033[38;2;0;230;118m"
RED="\033[38;2;255;82;82m"
GRAY="\033[38;2;136;136;136m"
BOLD="\033[1m"
RESET="\033[0m"

print_logo() {
  printf "${ORANGE}${BOLD}"
  printf ' ██████╗██╗      ██████╗ ███╗   ██╗███████╗ █████╗ ██████╗ ██╗     ███████╗\n'
  printf '██╔════╝██║     ██╔═══██╗████╗  ██║██╔════╝██╔══██╗██╔══██╗██║     ██╔════╝\n'
  printf '██║     ██║     ██║   ██║██╔██╗ ██║█████╗  ███████║██████╔╝██║     █████╗  \n'
  printf '██║     ██║     ██║   ██║██║╚██╗██║██╔══╝  ██╔══██║██╔══██╗██║     ██╔══╝  \n'
  printf '╚██████╗███████╗╚██████╔╝██║ ╚████║███████╗██║  ██║██████╔╝███████╗███████╗\n'
  printf ' ╚═════╝╚══════╝ ╚═════╝ ╚═╝  ╚═══╝╚══════╝╚═╝  ╚═╝╚═════╝ ╚══════╝╚══════╝   by Manan\n'
  printf "${RESET}\n"
}

info()    { printf "  ${ORANGE}→${RESET}  %s\n" "$1"; }
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

# ── Download and install ──────────────────────────────────────────────────────
print_logo
info "Downloading ${BINARY}..."

if command -v curl >/dev/null 2>&1; then
  curl -fsSL "$BINARY_URL" -o "${INSTALL_DIR}/${BINARY}" \
    || error "Download failed. Check your internet connection."
elif command -v wget >/dev/null 2>&1; then
  wget -qO "${INSTALL_DIR}/${BINARY}" "$BINARY_URL" \
    || error "Download failed. Check your internet connection."
else
  error "curl or wget is required but neither was found."
fi

chmod +x "${INSTALL_DIR}/${BINARY}"

printf "\n"
success "Cloneable installed to ${INSTALL_DIR}/${BINARY}"
printf "\n"
printf "  ${GRAY}Get started:${RESET}\n"
printf "    ${ORANGE}cloneable --help${RESET}\n\n"
