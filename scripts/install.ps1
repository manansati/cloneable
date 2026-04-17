# Cloneable installer for Windows
# Usage: irm https://raw.githubusercontent.com/manansati/cloneable/main/scripts/install.ps1 | iex

$ErrorActionPreference = "Stop"

$Repo    = "manansati/cloneable"
$Binary  = "cloneable.exe"
$InstDir = "$env:USERPROFILE\.cloneable\bin"

function Write-Info    ($msg) { Write-Host "  -> $msg" -ForegroundColor DarkYellow }
function Write-Success ($msg) { Write-Host "  + $msg"  -ForegroundColor Green }
function Write-Muted   ($msg) { Write-Host "  $msg"    -ForegroundColor DarkGray }
function Write-Fail    ($msg) { Write-Host "  x $msg"  -ForegroundColor Red; exit 1 }

# ── Detect architecture ───────────────────────────────────────────────────────
function Get-Arch {
  $arch = (Get-WmiObject Win32_Processor).AddressWidth
  if ($arch -eq 64) { return "amd64" }
  Write-Fail "Unsupported architecture"
}

# ── Fetch latest version from GitHub ─────────────────────────────────────────
function Get-LatestVersion {
  $url     = "https://api.github.com/repos/$Repo/releases/latest"
  $headers = @{ "User-Agent" = "cloneable-installer" }
  try {
    $response = Invoke-RestMethod -Uri $url -Headers $headers
    return $response.tag_name.TrimStart("v")
  } catch {
    Write-Fail "Could not fetch latest version: $_"
  }
}

# ── Download and install ──────────────────────────────────────────────────────
function Install-Cloneable ($version, $arch) {
  $filename = "cloneable_${version}_windows_${arch}.zip"
  $url      = "https://github.com/$Repo/releases/download/v$version/$filename"
  $tmp      = [System.IO.Path]::GetTempPath() + [System.Guid]::NewGuid().ToString()

  New-Item -ItemType Directory -Path $tmp -Force | Out-Null
  $zipPath = "$tmp\$filename"

  Write-Info "Downloading Cloneable v$version for windows/$arch..."

  try {
    Invoke-WebRequest -Uri $url -OutFile $zipPath -UseBasicParsing
  } catch {
    Write-Fail "Download failed: $_"
  }

  # Extract
  Expand-Archive -Path $zipPath -DestinationPath $tmp -Force

  # Create install dir and move binary
  New-Item -ItemType Directory -Path $InstDir -Force | Out-Null
  $exePath = "$tmp\cloneable.exe"
  if (-Not (Test-Path $exePath)) {
    Write-Fail "Binary not found in archive"
  }
  Copy-Item $exePath -Destination "$InstDir\$Binary" -Force

  # Cleanup
  Remove-Item $tmp -Recurse -Force
}

# ── Add install dir to user PATH ──────────────────────────────────────────────
function Add-ToPath {
  $current = [Environment]::GetEnvironmentVariable("PATH", "User")
  if ($current -like "*$InstDir*") { return }

  $new = "$InstDir;$current"
  [Environment]::SetEnvironmentVariable("PATH", $new, "User")
  Write-Muted "Added $InstDir to user PATH"
  Write-Muted "Restart your terminal for PATH changes to take effect"
}

# ── Main ──────────────────────────────────────────────────────────────────────
Write-Host ""
Write-Host "  Installing Cloneable" -ForegroundColor DarkYellow
Write-Host ""

$arch    = Get-Arch
$version = Get-LatestVersion

Write-Info "Detected: windows/$arch"

Install-Cloneable $version $arch
Add-ToPath

Write-Host ""
Write-Success "Cloneable v$version installed to $InstDir\$Binary"
Write-Host ""
Write-Muted "Usage:"
Write-Muted "  cloneable https://github.com/user/repo"
Write-Muted "  cloneable search ghostty"
Write-Muted "  cloneable --help"
Write-Host ""
