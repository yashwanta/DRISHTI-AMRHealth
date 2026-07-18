param(
    [string]$Version = (Get-Date -Format 'yyyyMMdd-HHmmss'),
    [string]$OutputDir = 'dist'
)

$ErrorActionPreference = 'Stop'
$root = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$stageRoot = Join-Path $root '.runtime-package-linux'
$packageName = "DRISHTI-AMRHealth-Runtime-Linux-$Version"
$stage = Join-Path $stageRoot $packageName
$payload = Join-Path $stage 'payload'
$output = Join-Path $root $OutputDir
$imageArchive = Join-Path $payload 'drishti-runtime-images.tar'

if (-not (Get-Command podman -ErrorAction SilentlyContinue)) { throw 'Podman is required on the build computer.' }
if (-not (Get-Command tar -ErrorAction SilentlyContinue)) { throw 'tar is required to create the Linux runtime bundle.' }
if (Test-Path -LiteralPath $stageRoot) { Remove-Item -LiteralPath $stageRoot -Recurse -Force }
New-Item -ItemType Directory -Force -Path $payload | Out-Null
New-Item -ItemType Directory -Force -Path $output | Out-Null

podman build -t localhost/drishti-amr-health:runtime -f (Join-Path $root 'Containerfile') $root
if ($LASTEXITCODE -ne 0) { throw 'AMR Health image build failed.' }
podman pull docker.io/library/postgres:16-alpine | Out-Host
if ($LASTEXITCODE -ne 0) { throw 'PostgreSQL image pull failed.' }
podman save -o $imageArchive localhost/drishti-amr-health:runtime docker.io/library/postgres:16-alpine
if ($LASTEXITCODE -ne 0 -or -not (Test-Path -LiteralPath $imageArchive)) { throw 'Image export failed.' }

Copy-Item -LiteralPath (Join-Path $root 'deploy\runtime-linux\install-drishti-amr-health.sh') -Destination $stage
Copy-Item -LiteralPath (Join-Path $root 'deploy\runtime-linux\start-drishti-amr-health.sh') -Destination $stage
Copy-Item -LiteralPath (Join-Path $root 'deploy\runtime-linux\README.txt') -Destination $stage

$tarPath = Join-Path $output "$packageName.tar.gz"
if (Test-Path -LiteralPath $tarPath) { Remove-Item -LiteralPath $tarPath -Force }
Push-Location $stageRoot
try { tar -czf $tarPath $packageName } finally { Pop-Location }
if ($LASTEXITCODE -ne 0) { throw 'Linux runtime archive creation failed.' }

$hash = Get-FileHash -LiteralPath $tarPath -Algorithm SHA256
Set-Content -LiteralPath "$tarPath.sha256" -Value "$($hash.Hash.ToLowerInvariant())  $([IO.Path]::GetFileName($tarPath))" -Encoding ASCII
Write-Host "Created source-free Linux runtime installer: $tarPath" -ForegroundColor Green
Write-Host "SHA256: $($hash.Hash)"
