<#
.SYNOPSIS
  Creates a clean install package for DRISHTI - AMR Health.

.DESCRIPTION
  Produces dist/DRISHTI-AMRHealth-<version>.zip and, when tar is available,
  dist/DRISHTI-AMRHealth-<version>.tar.gz.

  The package intentionally excludes:
  - real local API config: data/config/api-connections.json
  - raw RDS snapshots: data/rds-snapshots/
  - node_modules, frontend/dist, Go cache, Git metadata
#>
param(
  [string]$Version = (Get-Date -Format "yyyyMMdd-HHmmss"),
  [string]$OutputDir = "dist"
)

$ErrorActionPreference = "Stop"
$root = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..\")).Path
$packageName = "DRISHTI-AMRHealth-$Version"
$stageRoot = Join-Path $root ".package"
$stage = Join-Path $stageRoot $packageName
$out = Join-Path $root $OutputDir

function Copy-Path($RelativePath) {
  $source = Join-Path $root $RelativePath
  if (-not (Test-Path -LiteralPath $source)) { return }
  $destination = Join-Path $stage $RelativePath
  $parent = Split-Path -Parent $destination
  if ($parent) { New-Item -ItemType Directory -Force -Path $parent | Out-Null }
  Copy-Item -LiteralPath $source -Destination $destination -Recurse -Force
}

if (Test-Path -LiteralPath $stageRoot) { Remove-Item -LiteralPath $stageRoot -Recurse -Force }
New-Item -ItemType Directory -Force -Path $stage | Out-Null
New-Item -ItemType Directory -Force -Path $out | Out-Null

$paths = @(
  ".containerignore",
  ".dockerignore",
  ".gitignore",
  "Containerfile",
  "README.md",
  "INSTALL.md",
  "Install-DRISHTI-Windows.ps1",
  "install-drishti-linux.sh",
  "go.mod",
  "podman-compose.yml",
  "backend",
  "frontend\index.html",
  "frontend\package-lock.json",
  "frontend\package.json",
  "frontend\tsconfig.json",
  "frontend\vite.config.ts",
  "frontend\src",
  "data\config\api-connections.example.json",
  "docs",
  "scripts"
)

foreach ($path in $paths) { Copy-Path $path }

# Remove generated/runtime-only content if copied through broad directories.
$remove = @(
  "frontend\node_modules",
  "frontend\dist",
  "data\config\api-connections.json",
  "data\rds-snapshots",
  ".gocache",
  "rds-discovery"
)
foreach ($path in $remove) {
  $target = Join-Path $stage $path
  if (Test-Path -LiteralPath $target) { Remove-Item -LiteralPath $target -Recurse -Force }
}

$zipPath = Join-Path $out "$packageName.zip"
if (Test-Path -LiteralPath $zipPath) { Remove-Item -LiteralPath $zipPath -Force }
Compress-Archive -Path $stage -DestinationPath $zipPath -Force
Write-Output "Created $zipPath"

if (Get-Command tar -ErrorAction SilentlyContinue) {
  $tarPath = Join-Path $out "$packageName.tar.gz"
  if (Test-Path -LiteralPath $tarPath) { Remove-Item -LiteralPath $tarPath -Force }
  Push-Location $stageRoot
  try {
    tar -czf $tarPath $packageName
    Write-Output "Created $tarPath"
  } finally {
    Pop-Location
  }
}

Write-Output "Package does not include real local API config or RDS snapshots."