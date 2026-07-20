param(
    [string]$Version = '0.6.0',
    [string]$OutputDir = 'dist',
    [string]$PodmanVersion = '5.8.3'
)

$ErrorActionPreference = 'Stop'
$root = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$stageRoot = Join-Path $root '.windows-exe-package'
$runtimeStage = Join-Path $root ".runtime-package\DRISHTI-AMRHealth-Runtime-Windows-$Version"
$output = Join-Path $root $OutputDir
$podmanInstaller = Join-Path $stageRoot 'podman-installer-windows-amd64.exe'
$expectedPodmanHash = 'b87fe41c112062b3f598574ed4aa3fb82aaa4bc150b29eb4915400e59b0b6b55'

if (Test-Path -LiteralPath $stageRoot) { Remove-Item -LiteralPath $stageRoot -Recurse -Force }
New-Item -ItemType Directory -Force -Path $stageRoot, $output | Out-Null

& (Join-Path $root 'scripts\package-runtime-windows.ps1') -Version $Version -OutputDir $OutputDir
if (-not (Test-Path -LiteralPath $runtimeStage)) { throw "Runtime staging folder not found: $runtimeStage" }

$podmanUrl = "https://github.com/podman-container-tools/podman/releases/download/v$PodmanVersion/podman-installer-windows-amd64.exe"
Write-Host "Downloading official Podman $PodmanVersion offline installer..." -ForegroundColor Cyan
Invoke-WebRequest -Uri $podmanUrl -OutFile $podmanInstaller -UseBasicParsing
$actualPodmanHash = (Get-FileHash -LiteralPath $podmanInstaller -Algorithm SHA256).Hash.ToLowerInvariant()
if ($actualPodmanHash -ne $expectedPodmanHash) {
    throw "Podman installer checksum mismatch. Expected $expectedPodmanHash, received $actualPodmanHash."
}

$iscc = Get-Command iscc.exe -ErrorAction SilentlyContinue
if (-not $iscc) {
    $candidates = @(
        (Join-Path $env:LOCALAPPDATA 'Programs\Inno Setup 6\ISCC.exe'),
        (Join-Path ${env:ProgramFiles(x86)} 'Inno Setup 6\ISCC.exe'),
        (Join-Path $env:ProgramFiles 'Inno Setup 6\ISCC.exe')
    )
    $candidate = $candidates | Where-Object { Test-Path -LiteralPath $_ } | Select-Object -First 1
    if ($candidate) { $iscc = Get-Item -LiteralPath $candidate }
}
if (-not $iscc) { throw 'Inno Setup 6 is required on the build computer (winget install JRSoftware.InnoSetup).' }

$env:DRISHTI_INSTALLER_VERSION = $Version
$env:DRISHTI_PAYLOAD_ROOT = $runtimeStage
$env:DRISHTI_PODMAN_INSTALLER = $podmanInstaller
$env:DRISHTI_OUTPUT_ROOT = $output
try {
    $isccPath = if ($iscc.Source) { $iscc.Source } else { $iscc.FullName }
    & $isccPath (Join-Path $root 'deploy\windows-exe\DRISHTI-AMRHealth.iss')
    if ($LASTEXITCODE -ne 0) { throw 'Inno Setup compilation failed.' }
} finally {
    Remove-Item Env:DRISHTI_INSTALLER_VERSION, Env:DRISHTI_PAYLOAD_ROOT, Env:DRISHTI_PODMAN_INSTALLER, Env:DRISHTI_OUTPUT_ROOT -ErrorAction SilentlyContinue
}

$setup = Join-Path $output "DRISHTI-AMRHealth-Setup-$Version-Windows-x64.exe"
$hash = Get-FileHash -LiteralPath $setup -Algorithm SHA256
Set-Content -LiteralPath "$setup.sha256" -Value "$($hash.Hash.ToLowerInvariant())  $([IO.Path]::GetFileName($setup))" -Encoding ASCII
Write-Host "Created offline Windows installer: $setup" -ForegroundColor Green
Write-Host "SHA256: $($hash.Hash)"
Write-Host 'The EXE includes Podman and source-free DRISHTI images. It does not contain an Agent API key.'
