param(
    [int]$HostPort = 8099,
    [string]$InstallRoot = (Join-Path $env:ProgramData 'DRISHTI-AMRHealth'),
    [string]$LLMURL = 'https://llm.eidonix.com',
    [string]$LLMModel = 'deepseek-v4-pro',
    [switch]$SkipPodmanInstall,
    [switch]$SkipLLMKey
)

$ErrorActionPreference = 'Stop'
$bundleRoot = $PSScriptRoot
$imageArchive = Join-Path $bundleRoot 'payload\drishti-runtime-images.tar'
if (-not (Test-Path -LiteralPath $imageArchive)) { throw "Missing runtime image archive: $imageArchive" }

$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$principal = [Security.Principal.WindowsPrincipal]::new($identity)
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw 'Run this installer from PowerShell as Administrator.'
}

function Update-ProcessPath {
    $machinePath = [Environment]::GetEnvironmentVariable('Path', 'Machine')
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $env:Path = @($machinePath, $userPath) -join ';'
}

Update-ProcessPath
if (-not (Get-Command podman -ErrorAction SilentlyContinue)) {
    if ($SkipPodmanInstall) { throw 'Podman is required but is not installed.' }
    if (-not (Get-Command winget -ErrorAction SilentlyContinue)) { throw 'Podman and winget are unavailable.' }
    winget install --id RedHat.Podman -e --accept-package-agreements --accept-source-agreements
    Update-ProcessPath
    if (-not (Get-Command podman -ErrorAction SilentlyContinue)) {
        throw 'Podman was installed. Open a new Administrator PowerShell window and run the installer again.'
    }
}

$machines = podman machine list --format json 2>$null | ConvertFrom-Json
if (-not $machines) { podman machine init }
podman machine start *> $null
podman info | Out-Null

New-Item -ItemType Directory -Force -Path $InstallRoot | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $InstallRoot 'data\config') | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $InstallRoot 'data\ssh') | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $InstallRoot 'data\rds-snapshots') | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $InstallRoot 'data\keys') | Out-Null
$knownHosts = Join-Path $InstallRoot 'data\ssh\known_hosts'
if (-not (Test-Path -LiteralPath $knownHosts)) { New-Item -ItemType File -Path $knownHosts | Out-Null }

Copy-Item -LiteralPath (Join-Path $bundleRoot 'Start-DRISHTI-AMRHealth.ps1') -Destination $InstallRoot -Force
podman load -i $imageArchive | Out-Host
if ($LASTEXITCODE -ne 0) { throw 'Could not load the bundled runtime images.' }

function New-RandomSecret([int]$bytes = 32) {
    $buffer = [byte[]]::new($bytes)
    [Security.Cryptography.RandomNumberGenerator]::Fill($buffer)
    return [Convert]::ToBase64String($buffer)
}

$settings = [pscustomobject]@{
    HostPort = $HostPort
    LLMURL = $LLMURL
    LLMModel = $LLMModel
    EncryptionKey = New-RandomSecret 32
    SessionSecret = New-RandomSecret 48
    DatabasePassword = ConvertTo-SecureString (New-RandomSecret 32) -AsPlainText -Force
}
$settings | Export-Clixml -LiteralPath (Join-Path $InstallRoot 'runtime-settings.clixml') -Force

if (-not $SkipLLMKey) {
    $secureKey = Read-Host 'Enter the Agent API key (not stored in the installer)' -AsSecureString
    $ptr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secureKey)
    try {
        $plainKey = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($ptr)
        if ($plainKey) {
            podman secret inspect drishti_llm_api_key *> $null
            if ($LASTEXITCODE -eq 0) { podman secret rm drishti_llm_api_key | Out-Null }
            $plainKey | podman secret create drishti_llm_api_key - | Out-Null
        }
    } finally {
        [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($ptr)
        $plainKey = $null
    }
}

& (Join-Path $InstallRoot 'Start-DRISHTI-AMRHealth.ps1') -InstallRoot $InstallRoot

$desktop = [Environment]::GetFolderPath('CommonDesktopDirectory')
$shortcutPath = Join-Path $desktop 'DRISHTI AMR Health.url'
Set-Content -LiteralPath $shortcutPath -Value "[InternetShortcut]`r`nURL=http://localhost:$HostPort`r`n" -Encoding ASCII
Write-Host "Installation complete. Desktop shortcut created." -ForegroundColor Green
