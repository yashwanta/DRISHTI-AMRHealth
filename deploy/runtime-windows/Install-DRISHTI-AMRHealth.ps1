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

function Test-WSLReady {
    if (-not (Get-Command wsl.exe -ErrorAction SilentlyContinue)) { return $false }
    & wsl.exe --status *> $null
    return $LASTEXITCODE -eq 0
}

function Register-InstallResume {
    $taskName = 'DRISHTI AMR Health - Complete Installation'
    $taskUser = [Security.Principal.WindowsIdentity]::GetCurrent().Name
    $argument = "-NoProfile -ExecutionPolicy Bypass -File `"$bundleRoot\Install-DRISHTI-AMRHealth.ps1`" -HostPort $HostPort -InstallRoot `"$InstallRoot`" -LLMURL `"$LLMURL`" -LLMModel `"$LLMModel`" -SkipPodmanInstall"
    if ($SkipLLMKey) { $argument += ' -SkipLLMKey' }
    $action = New-ScheduledTaskAction -Execute (Join-Path $PSHOME 'powershell.exe') -Argument $argument
    $trigger = New-ScheduledTaskTrigger -AtLogOn -User $taskUser
    $taskPrincipal = New-ScheduledTaskPrincipal -UserId $taskUser -LogonType Interactive -RunLevel Highest
    Register-ScheduledTask -TaskName $taskName -Action $action -Trigger $trigger -Principal $taskPrincipal -Force | Out-Null
}

if (-not (Test-WSLReady)) {
    Write-Host 'WSL 2 is required by Podman. Windows will enable it now.' -ForegroundColor Yellow
    Register-InstallResume
    & wsl.exe --install --no-distribution
    if ($LASTEXITCODE -ne 0) { throw 'Windows could not enable WSL. Confirm hardware virtualization is enabled in BIOS/UEFI.' }
    Write-Host 'Restart Windows. DRISHTI installation will resume automatically after you sign in.' -ForegroundColor Yellow
    exit 3010
}

function Update-ProcessPath {
    $machinePath = [Environment]::GetEnvironmentVariable('Path', 'Machine')
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $env:Path = @($machinePath, $userPath) -join ';'
}

function Test-PodmanResource([string[]]$Arguments) {
    $previousPreference = $ErrorActionPreference
    try {
        $ErrorActionPreference = 'Continue'
        & podman @Arguments *> $null
        return $LASTEXITCODE -eq 0
    } finally {
        $ErrorActionPreference = $previousPreference
    }
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
if (-not $machines) {
    podman machine init
    if ($LASTEXITCODE -ne 0) { throw 'Podman machine initialization failed.' }
}
$machines = podman machine list --format json 2>$null | ConvertFrom-Json
if (-not ($machines | Where-Object { $_.Running -eq $true })) {
    podman machine start | Out-Host
    if ($LASTEXITCODE -ne 0) { throw 'Podman machine failed to start.' }
}
podman info | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'Podman is installed but its Linux machine is not reachable.' }

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
    $generator = [Security.Cryptography.RandomNumberGenerator]::Create()
    try { $generator.GetBytes($buffer) }
    finally { $generator.Dispose() }
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
            if (Test-PodmanResource @('secret','inspect','drishti_llm_api_key')) {
                podman secret rm drishti_llm_api_key | Out-Null
            }
            $plainKey | podman secret create drishti_llm_api_key - | Out-Null
        }
    } finally {
        [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($ptr)
        $plainKey = $null
    }
}

& (Join-Path $InstallRoot 'Start-DRISHTI-AMRHealth.ps1') -InstallRoot $InstallRoot

$taskName = 'DRISHTI AMR Health - Automatic Startup'
$startScript = Join-Path $InstallRoot 'Start-DRISHTI-AMRHealth.ps1'
$taskUser = [Security.Principal.WindowsIdentity]::GetCurrent().Name
$taskAction = New-ScheduledTaskAction `
    -Execute (Join-Path $PSHOME 'powershell.exe') `
    -Argument ("-NoProfile -NonInteractive -ExecutionPolicy Bypass -WindowStyle Hidden -File `"{0}`" -InstallRoot `"{1}`"" -f $startScript, $InstallRoot)
$taskTrigger = New-ScheduledTaskTrigger -AtLogOn -User $taskUser
$taskSettings = New-ScheduledTaskSettingsSet -StartWhenAvailable -ExecutionTimeLimit (New-TimeSpan -Minutes 10) -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1)
$taskPrincipal = New-ScheduledTaskPrincipal -UserId $taskUser -LogonType Interactive -RunLevel Highest
Register-ScheduledTask -TaskName $taskName -Action $taskAction -Trigger $taskTrigger -Settings $taskSettings -Principal $taskPrincipal -Force | Out-Null
Unregister-ScheduledTask -TaskName 'DRISHTI AMR Health - Complete Installation' -Confirm:$false -ErrorAction SilentlyContinue

$desktop = [Environment]::GetFolderPath('CommonDesktopDirectory')
$shortcutPath = Join-Path $desktop 'DRISHTI AMR Health.url'
Set-Content -LiteralPath $shortcutPath -Value "[InternetShortcut]`r`nURL=http://localhost:$HostPort`r`n" -Encoding ASCII
Write-Host "Installation complete. Desktop shortcut and automatic startup task created." -ForegroundColor Green
