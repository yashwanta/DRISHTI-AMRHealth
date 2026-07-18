param(
    [string]$Version = (Get-Date -Format 'yyyyMMdd-HHmmss'),
    [string]$OutputDir = 'dist'
)

$ErrorActionPreference = 'Stop'
$root = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$stageRoot = Join-Path $root '.runtime-package'
$packageName = "DRISHTI-AMRHealth-Runtime-Windows-$Version"
$stage = Join-Path $stageRoot $packageName
$payload = Join-Path $stage 'payload'
$output = Join-Path $root $OutputDir
$archive = Join-Path $payload 'drishti-runtime-images.tar'

if (-not (Get-Command podman -ErrorAction SilentlyContinue)) {
    throw 'Podman is required on the build computer to create the runtime image bundle.'
}

if (Test-Path -LiteralPath $stageRoot) { Remove-Item -LiteralPath $stageRoot -Recurse -Force }
New-Item -ItemType Directory -Force -Path $payload | Out-Null
New-Item -ItemType Directory -Force -Path $output | Out-Null

Write-Host 'Building production AMR Health runtime image...' -ForegroundColor Cyan
podman build -t localhost/drishti-amr-health:runtime -f (Join-Path $root 'Containerfile') $root
if ($LASTEXITCODE -ne 0) { throw 'AMR Health runtime image build failed.' }

Write-Host 'Ensuring PostgreSQL runtime image is available...' -ForegroundColor Cyan
podman pull docker.io/library/postgres:16-alpine | Out-Host
if ($LASTEXITCODE -ne 0) { throw 'PostgreSQL runtime image pull failed.' }

Write-Host 'Exporting source-free container images...' -ForegroundColor Cyan
podman save -o $archive localhost/drishti-amr-health:runtime docker.io/library/postgres:16-alpine
if ($LASTEXITCODE -ne 0 -or -not (Test-Path -LiteralPath $archive)) { throw 'Runtime image export failed.' }

Copy-Item -LiteralPath (Join-Path $root 'deploy\runtime-windows\Install-DRISHTI-AMRHealth.ps1') -Destination $stage
Copy-Item -LiteralPath (Join-Path $root 'deploy\runtime-windows\Start-DRISHTI-AMRHealth.ps1') -Destination $stage
Copy-Item -LiteralPath (Join-Path $root 'deploy\runtime-windows\README.txt') -Destination $stage

$zip = Join-Path $output "$packageName.zip"
if (Test-Path -LiteralPath $zip) { Remove-Item -LiteralPath $zip -Force }
Compress-Archive -LiteralPath $stage -DestinationPath $zip -CompressionLevel Optimal

$hash = Get-FileHash -LiteralPath $zip -Algorithm SHA256
Set-Content -LiteralPath "$zip.sha256" -Value "$($hash.Hash.ToLowerInvariant())  $([IO.Path]::GetFileName($zip))" -Encoding ASCII

Write-Host "Created source-free runtime installer: $zip" -ForegroundColor Green
Write-Host "SHA256: $($hash.Hash)"
Write-Host 'The bundle contains no Git metadata, source tree, .env file, or Agent API key.'
