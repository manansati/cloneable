# Cloneable installer for Windows
#
# Usage (PowerShell as Administrator):
#   irm https://raw.githubusercontent.com/manansati/cloneable/main/scripts/install.ps1 | iex
#
# Installs to C:\Program Files\cloneable and adds to system PATH.
# Run PowerShell as Administrator for this to work.

$ErrorActionPreference = "Stop"

$Repo      = "manansati/cloneable"
$Binary    = "cloneable.exe"
$InstDir   = "C:\Program Files\cloneable"

function Write-Info    ($m) { Write-Host "  -> $m" -ForegroundColor DarkYellow }
function Write-Success ($m) { Write-Host "  + $m"  -ForegroundColor Green }
function Write-Muted   ($m) { Write-Host "  $m"    -ForegroundColor DarkGray }
function Write-Fail    ($m) { Write-Host "`n  x $m`n" -ForegroundColor Red; exit 1 }

# ── Admin check ───────────────────────────────────────────────────────────────
$isAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole(
  [Security.Principal.WindowsBuiltInRole]::Administrator
)

if (-not $isAdmin) {
  Write-Host "`n  x This installer needs Administrator rights." -ForegroundColor Red
  Write-Host ""
  Write-Host "  How to fix:" -ForegroundColor DarkGray
  Write-Host "    1. Search for PowerShell in Start Menu" -ForegroundColor DarkGray
  Write-Host "    2. Right-click -> Run as Administrator" -ForegroundColor DarkGray
  Write-Host "    3. Paste and run the install command again" -ForegroundColor DarkGray
  Write-Host ""
  exit 1
}

# ── Detect architecture ───────────────────────────────────────────────────────
function Get-Arch {
  $p = (Get-WmiObject Win32_Processor).Architecture
  if ($p -eq 9) { return "amd64" }
  if ($p -eq 12) { return "arm64" }
  Write-Fail "Unsupported CPU architecture"
}

# ── Fetch latest release ──────────────────────────────────────────────────────
function Get-LatestVersion {
  $url = "https://api.github.com/repos/$Repo/releases/latest"
  $headers = @{ "User-Agent" = "cloneable-installer" }
  try {
    $r = Invoke-RestMethod -Uri $url -Headers $headers -ErrorAction Stop
    if ($r.message) { return "" }  # "Not Found" etc
    return $r.tag_name.TrimStart("v")
  } catch {
    return ""
  }
}

# ── Install pre-built binary ──────────────────────────────────────────────────
function Install-Binary($version, $arch) {
  $file = "cloneable_${version}_windows_${arch}.zip"
  $url  = "https://github.com/$Repo/releases/download/v$version/$file"
  $tmp  = [System.IO.Path]::Combine([System.IO.Path]::GetTempPath(), [System.Guid]::NewGuid())
  New-Item -ItemType Directory -Path $tmp -Force | Out-Null

  Write-Info "Downloading v$version (windows/$arch)..."

  try {
    Invoke-WebRequest -Uri $url -OutFile "$tmp\$file" -UseBasicParsing -ErrorAction Stop
  } catch {
    Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
    return $false
  }

  try {
    Expand-Archive -Path "$tmp\$file" -DestinationPath $tmp -Force
  } catch {
    Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
    return $false
  }

  $exe = Get-ChildItem -Path $tmp -Filter "cloneable.exe" -Recurse | Select-Object -First 1
  if (-not $exe) {
    Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
    return $false
  }

  New-Item -ItemType Directory -Path $InstDir -Force | Out-Null
  Copy-Item $exe.FullName -Destination "$InstDir\$Binary" -Force
  Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
  return $true
}

# ── Build from source (fallback) ──────────────────────────────────────────────
function Build-FromSource {
  Write-Host "`n  Building from source..." -ForegroundColor White
  Write-Muted "(No pre-built release found)"
  Write-Host ""

  if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Fail "Go not found. Install from https://go.dev/dl then run this script again."
  }
  if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
    Write-Fail "git not found. Install from https://git-scm.com then run this script again."
  }

  Write-Info "Go $(go version) found"

  $tmp = [System.IO.Path]::Combine([System.IO.Path]::GetTempPath(), [System.Guid]::NewGuid())
  New-Item -ItemType Directory -Path $tmp -Force | Out-Null

  Write-Info "Cloning repository..."
  git clone --depth=1 "https://github.com/$Repo.git" "$tmp\src" 2>&1 | Out-Null
  if ($LASTEXITCODE -ne 0) { Write-Fail "Could not clone repository." }

  Write-Info "Building binary..."
  Set-Location "$tmp\src"
  go mod tidy 2>&1 | Out-Null
  go build -ldflags "-s -w -X github.com/manansati/cloneable/cmd.Version=dev" -o "$tmp\cloneable.exe" .
  if ($LASTEXITCODE -ne 0) { Write-Fail "Build failed." }

  New-Item -ItemType Directory -Path $InstDir -Force | Out-Null
  Copy-Item "$tmp\cloneable.exe" -Destination "$InstDir\$Binary" -Force
  Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}

# ── Add to system PATH ────────────────────────────────────────────────────────
function Add-ToPath {
  $machinePath = [Environment]::GetEnvironmentVariable("PATH", "Machine")
  if ($machinePath -like "*$InstDir*") {
    return  # Already there
  }
  [Environment]::SetEnvironmentVariable("PATH", "$machinePath;$InstDir", "Machine")
  Write-Muted "Added $InstDir to system PATH"
  Write-Muted "Restart your terminal to use cloneable from anywhere"
}

# ── Main ──────────────────────────────────────────────────────────────────────
Write-Host ""
Write-Host "  Installing Cloneable" -ForegroundColor DarkYellow
Write-Host ""

$arch    = Get-Arch
$version = Get-LatestVersion

if ($version) {
  $ok = Install-Binary $version $arch
  if ($ok) {
    Add-ToPath
    Write-Host ""
    Write-Success "Cloneable v$version installed"
    Write-Muted "Location: $InstDir\$Binary"
    Write-Host ""
    Write-Host "  Try it (open a new terminal):"
    Write-Host "    cloneable --help"
    Write-Host ""
    exit 0
  }
  Write-Muted "Download failed — building from source..."
} else {
  Write-Muted "No releases found — building from source..."
}

Build-FromSource
Add-ToPath

Write-Host ""
Write-Success "Cloneable installed"
Write-Muted "Location: $InstDir\$Binary"
Write-Host ""
Write-Host "  Open a new terminal and try:"
Write-Host "    cloneable --help"
Write-Host ""
