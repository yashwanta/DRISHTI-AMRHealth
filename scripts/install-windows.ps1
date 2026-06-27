<#
.SYNOPSIS
  Installs and runs DRISHTI - AMR Health on Windows 11 using Podman.

.DESCRIPTION
  - Installs Podman Desktop via winget when Podman is missing.
  - Starts/initializes the Podman machine when needed.
  - Creates local ignored config data/config/api-connections.json from the sanitized example if missing.
  - Builds the Go + React container image.
  - Runs the app at http://localhost:8088.

.NOTES
  Run from the package/repository root in PowerShell.
#>
param(
  [int]$HostPort = 8088,
  [string]$ImageName = "drishti-amr-health",
  [string]$ContainerName = "AMR-Health",
  [switch]$SkipPodmanInstall
)

$ErrorActionPreference = "Stop"
$root = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..\")).Path
Set-Location $root

function Test-Command($Name) {
  return [bool](Get-Command $Name -ErrorAction SilentlyContinue)
}

function Invoke-Step($Message, [scriptblock]$Action) {
  Write-Host "`n==> $Message" -ForegroundColor Cyan
  & $Action
}

Invoke-Step "Checking Podman" {
  if (-not (Test-Command podman)) {
    if ($SkipPodmanInstall) {
      throw "Podman is not installed. Install Podman Desktop, then rerun this script."
    }
    if (-not (Test-Command winget)) {
      throw "Podman is not installed and winget is unavailable. Install Podman Desktop manually, then rerun this script."
    }
    Write-Host "Installing Podman Desktop with winget..."
    winget install --id RedHat.Podman-Desktop -e --accept-package-agreements --accept-source-agreements
    Write-Host "Podman Desktop installed. Open a new PowerShell window if podman is not yet on PATH, then rerun this script."
    if (-not (Test-Command podman)) { exit 0 }
  }
  podman --version
}

Invoke-Step "Starting Podman machine" {
  $machineList = podman machine list --format json 2>$null | ConvertFrom-Json -ErrorAction SilentlyContinue
  if (-not $machineList -or $machineList.Count -eq 0) {
    podman machine init
  }
  $running = podman machine list --format json 2>$null | ConvertFrom-Json | Where-Object { $_.Running -eq $true }
  if (-not $running) {
    podman machine start
  }
}

Invoke-Step "Preparing local data config" {
  New-Item -ItemType Directory -Force -Path "data\config" | Out-Null
  New-Item -ItemType Directory -Force -Path "data\rds-snapshots" | Out-Null
  if (-not (Test-Path -LiteralPath "data\config\api-connections.json")) {
    Copy-Item -LiteralPath "data\config\api-connections.example.json" -Destination "data\config\api-connections.json"
    Write-Host "Created data\config\api-connections.json from example. Add real plant URLs in the app Admin page."
  } else {
    Write-Host "Keeping existing local data\config\api-connections.json."
  }
}

Invoke-Step "Building container image" {
  podman build -t $ImageName .
}

Invoke-Step "Replacing running container" {
  podman rm -f $ContainerName 2>$null | Out-Null
  $dataPath = (Resolve-Path -LiteralPath "data").Path
  podman run -d --name $ContainerName -p "${HostPort}:8090" -v "${dataPath}:/app/data" --restart unless-stopped $ImageName
}

Invoke-Step "Verifying app" {
  Start-Sleep -Seconds 3
  $health = Invoke-WebRequest -Uri "http://localhost:$HostPort/api/health" -UseBasicParsing -TimeoutSec 15
  if ($health.StatusCode -ne 200) { throw "Health check failed with status $($health.StatusCode)." }
  Write-Host "DRISHTI - AMR Health is running: http://localhost:$HostPort" -ForegroundColor Green
  Write-Host "Use Admin > RDS API Connections to add real plant RDS URLs."
}