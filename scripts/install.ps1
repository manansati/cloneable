# Cloneable installer for Windows
#
# Usage (run in PowerShell):
#   irm https://raw.githubusercontent.com/manansati/cloneable/main/scripts/install.ps1 | iex

#Requires -Version 5.1
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$REPO        = "manansati/cloneable"
$BINARY      = "cloneable.exe"
$INSTALL_DIR = "$env:LOCALAPPDATA\Programs\cloneable"
$BINARY_URL  = "https://raw.githubusercontent.com/$REPO/main/binary/cloneable.exe"

# ── Enable ANSI colours on Windows 10+ ───────────────────────────────────────
try {
  [System.Console]::OutputEncoding = [System.Text.Encoding]::UTF8
  $kernel32 = Add-Type -MemberDefinition @'
    [DllImport("kernel32.dll")]
    public static extern bool SetConsoleMode(IntPtr hConsoleHandle, uint dwMode);
    [DllImport("kernel32.dll")]
    public static extern IntPtr GetStdHandle(int nStdHandle);
    [DllImport("kernel32.dll")]
    public static extern bool GetConsoleMode(IntPtr hConsoleHandle, out uint lpMode);
'@ -Name 'Kernel32' -Namespace 'Win32' -PassThru
  $handle = [Win32.Kernel32]::GetStdHandle(-11)
  $mode   = 0
  [Win32.Kernel32]::GetConsoleMode($handle, [ref]$mode) | Out-Null
  [Win32.Kernel32]::SetConsoleMode($handle, ($mode -bor 4)) | Out-Null
} catch {}

$ESC    = [char]27
$ORANGE = "${ESC}[38;2;255;140;0m"
$GREEN  = "${ESC}[38;2;0;230;118m"
$RED    = "${ESC}[38;2;255;82;82m"
$GRAY   = "${ESC}[38;2;136;136;136m"
$BOLD   = "${ESC}[1m"
$RESET  = "${ESC}[0m"

function Print-Logo {
  Write-Host ""
  Write-Host "${ORANGE}${BOLD} ██████╗██╗      ██████╗ ███╗   ██╗███████╗ █████╗ ██████╗ ██╗     ███████╗${RESET}"
  Write-Host "${ORANGE}${BOLD}██╔════╝██║     ██╔═══██╗████╗  ██║██╔════╝██╔══██╗██╔══██╗██║     ██╔════╝${RESET}"
  Write-Host "${ORANGE}${BOLD}██║     ██║     ██║   ██║██╔██╗ ██║█████╗  ███████║██████╔╝██║     █████╗  ${RESET}"
  Write-Host "${ORANGE}${BOLD}██║     ██║     ██║   ██║██║╚██╗██║██╔══╝  ██╔══██║██╔══██╗██║     ██╔══╝  ${RESET}"
  Write-Host "${ORANGE}${BOLD}╚██████╗███████╗╚██████╔╝██║ ╚████║███████╗██║  ██║██████╔╝███████╗███████╗${RESET}"
  Write-Host "${ORANGE}${BOLD} ╚═════╝╚══════╝ ╚═════╝ ╚═╝  ╚═══╝╚══════╝╚═╝  ╚═╝╚═════╝ ╚══════╝╚══════╝${RESET}"
  Write-Host ""
}

function Write-Info    ($msg) { Write-Host "  ${ORANGE}→${RESET}  $msg" }
function Write-Success ($msg) { Write-Host "  ${GREEN}${BOLD}✓${RESET}  $msg" }
function Write-Muted   ($msg) { Write-Host "  ${GRAY}$msg${RESET}" }
function Write-Err     ($msg) { Write-Host "`n  ${RED}✗${RESET}  $msg`n"; exit 1 }

# ── Download binary ───────────────────────────────────────────────────────────
Print-Logo
Write-Info "Downloading cloneable..."

New-Item -ItemType Directory -Path $INSTALL_DIR -Force | Out-Null

try {
  Invoke-WebRequest -Uri $BINARY_URL -OutFile "$INSTALL_DIR\$BINARY" -UseBasicParsing
} catch {
  Write-Err "Download failed. Check your internet connection."
}

# ── Add to user PATH if not already there ─────────────────────────────────────
$currentPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($currentPath -notlike "*$INSTALL_DIR*") {
  Write-Info "Adding to PATH..."
  [Environment]::SetEnvironmentVariable("Path", "$currentPath;$INSTALL_DIR", "User")
  $env:PATH = "$env:PATH;$INSTALL_DIR"
  Write-Muted "Restart your terminal for PATH to take effect."
}

Write-Host ""
Write-Success "Cloneable installed to $INSTALL_DIR\$BINARY"
Write-Host ""
Write-Host "  ${GRAY}Get started (open a new terminal, then):${RESET}"
Write-Host "    ${ORANGE}cloneable --help${RESET}"
Write-Host ""
