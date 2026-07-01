<#
.SYNOPSIS
  One-command installer for DRISHTI - AMR Health on Windows 11.

.DESCRIPTION
  Run this from the extracted package or repository root. It installs/verifies Podman,
  prepares local data folders, builds the DRISHTI container image, starts the app,
  and verifies http://localhost:8088/api/health.
#>
param(
  [int]$HostPort = 8088,
  [string]$ImageName = "drishti-amr-health",
  [string]$ContainerName = "AMR-Health",
  [switch]$SkipPodmanInstall
)

$ErrorActionPreference = "Stop"
$installer = Join-Path $PSScriptRoot "scripts\install-windows.ps1"
if (-not (Test-Path -LiteralPath $installer)) {
  throw "Missing installer script: $installer"
}
& $installer -HostPort $HostPort -ImageName $ImageName -ContainerName $ContainerName -SkipPodmanInstall:$SkipPodmanInstall