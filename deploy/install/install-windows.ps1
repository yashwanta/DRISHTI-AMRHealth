<#
.SYNOPSIS
  Installs and runs DRISHTI - AMR Health on Windows 11 using Podman.

.DESCRIPTION
  - Installs Podman Desktop via winget when Podman is missing.
  - Starts/initializes the Podman machine when needed.
  - Creates local ignored config/data folders from sanitized examples when missing.
  - Builds the Go + React container image. Node.js and Go are downloaded inside the container build; they are not required on the host.
  - Runs the app at http://localhost:8088 by default.

.NOTES
  Run from the package/repository root in PowerShell.
#>
param(
  [int]$HostPort = 8088,
  [string]$ImageName = "drishti-amr-health",
  [string]$ContainerName = "AMR-Health",
  [string]$DatabaseContainerName = "AMR-Health-DB",
  [switch]$SkipPodmanInstall
)

$ErrorActionPreference = "Stop"
$root = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..\..\")).Path
Set-Location $root

function Test-Command($Name) {
  return [bool](Get-Command $Name -ErrorAction SilentlyContinue)
}

function Invoke-Step($Message, [scriptblock]$Action) {
  Write-Host "`n==> $Message" -ForegroundColor Cyan
  & $Action
}

function Test-IsAdministrator {
  $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
  $principal = [Security.Principal.WindowsPrincipal]::new($identity)
  return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Test-WSLReady {
  if (-not (Test-Command wsl.exe)) { return $false }
  & wsl.exe --status *> $null
  return $LASTEXITCODE -eq 0
}

function Register-InstallResume {
  $taskName = 'DRISHTI AMR Health - Complete Installation'
  $taskUser = [Security.Principal.WindowsIdentity]::GetCurrent().Name
  $argument = "-NoProfile -ExecutionPolicy Bypass -File `"$root\Install-DRISHTI-Windows.ps1`" -HostPort $HostPort"
  $action = New-ScheduledTaskAction -Execute (Join-Path $PSHOME 'powershell.exe') -Argument $argument
  $trigger = New-ScheduledTaskTrigger -AtLogOn -User $taskUser
  $principal = New-ScheduledTaskPrincipal -UserId $taskUser -LogonType Interactive -RunLevel Highest
  Register-ScheduledTask -TaskName $taskName -Action $action -Trigger $trigger -Principal $principal -Force | Out-Null
}

if (-not (Test-IsAdministrator)) {
  throw 'Run this installer from PowerShell as Administrator.'
}

Invoke-Step "Checking Windows virtualization support" {
  if (-not (Test-WSLReady)) {
    Write-Host 'WSL 2 is required by Podman. Windows will enable it now.' -ForegroundColor Yellow
    Register-InstallResume
    & wsl.exe --install --no-distribution
    if ($LASTEXITCODE -ne 0) { throw 'Windows could not enable WSL. Confirm hardware virtualization is enabled in BIOS/UEFI.' }
    Write-Host 'Restart Windows. DRISHTI installation will resume automatically after you sign in.' -ForegroundColor Yellow
    exit 3010
  }
}

Invoke-Step "Checking host dependencies" {
  Write-Host "Required host dependency: Podman."
  Write-Host "Container build dependencies are handled inside Containerfile: Node.js, npm, Go, Alpine, OpenSSH client."
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
    Write-Host "Podman Desktop install finished. Open a new PowerShell window if podman is not yet on PATH, then rerun this script."
    if (-not (Test-Command podman)) { exit 0 }
  }
  podman --version
}

Invoke-Step "Starting Podman machine" {
  $machineJson = podman machine list --format json 2>$null
  $machineList = if ($machineJson) { $machineJson | ConvertFrom-Json -ErrorAction SilentlyContinue } else { @() }
  if (-not $machineList -or $machineList.Count -eq 0) {
    podman machine init
    if ($LASTEXITCODE -ne 0) { throw 'Podman machine initialization failed. Run wsl --status and confirm WSL 2 is operational.' }
  }
  $machineJson = podman machine list --format json 2>$null
  $running = $machineJson | ConvertFrom-Json | Where-Object { $_.Running -eq $true }
  if (-not $running) {
    podman machine start
    if ($LASTEXITCODE -ne 0) { throw 'Podman machine failed to start.' }
  }
  podman info | Out-Null
  if ($LASTEXITCODE -ne 0) { throw 'Podman is installed but its Linux machine is not reachable.' }
}

Invoke-Step "Preparing local data config" {
  New-Item -ItemType Directory -Force -Path "data\config" | Out-Null
  New-Item -ItemType Directory -Force -Path "data\rds-snapshots" | Out-Null
  New-Item -ItemType Directory -Force -Path "data\keys" | Out-Null
  if (-not (Test-Path -LiteralPath "data\config\api-connections.json")) {
    Copy-Item -LiteralPath "data\config\api-connections.example.json" -Destination "data\config\api-connections.json"
    Write-Host "Created data\config\api-connections.json from example. Add real plant URLs from Admin > Setup."
  } else {
    Write-Host "Keeping existing local data\config\api-connections.json."
  }
}

Invoke-Step "Building container image" {
  podman build -t $ImageName .
  if ($LASTEXITCODE -ne 0) { throw "Container image build failed." }
}

function Test-PodmanResource([string[]]$Arguments) {
  $previousPreference = $ErrorActionPreference
  try {
    $ErrorActionPreference = "Continue"
    & podman @Arguments *> $null
    return $LASTEXITCODE -eq 0
  } finally {
    $ErrorActionPreference = $previousPreference
  }
}

function New-RandomSecret([int]$Bytes = 32) {
  $buffer = [byte[]]::new($Bytes)
  $generator = [Security.Cryptography.RandomNumberGenerator]::Create()
  try { $generator.GetBytes($buffer) }
  finally { $generator.Dispose() }
  return [Convert]::ToBase64String($buffer)
}

function ConvertFrom-ProtectedString([Security.SecureString]$Value) {
  $ptr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($Value)
  try { return [Runtime.InteropServices.Marshal]::PtrToStringBSTR($ptr) }
  finally { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($ptr) }
}

function Convert-ToPodmanPath([string]$Path) {
  $full = [IO.Path]::GetFullPath($Path)
  $drive = $full.Substring(0, 1).ToLowerInvariant()
  $rest = $full.Substring(2).Replace('\', '/')
  return "/mnt/$drive$rest"
}

$settingsPath = Join-Path $root "data\config\runtime-settings.clixml"
$networkName = "drishti-amr-health-source"
$volumeName = "drishti-amr-health-source-db"

Invoke-Step "Preparing runtime settings" {
  if (Test-Path -LiteralPath $settingsPath) {
    $script:settings = Import-Clixml -LiteralPath $settingsPath
    Write-Host "Keeping existing encrypted runtime settings."
  } else {
    $script:settings = [pscustomobject]@{
      EncryptionKey = New-RandomSecret 32
      SessionSecret = New-RandomSecret 48
      DatabasePassword = ConvertTo-SecureString (New-RandomSecret 32) -AsPlainText -Force
    }
    $script:settings | Export-Clixml -LiteralPath $settingsPath -Force
    Write-Host "Created encrypted runtime settings for the current Windows user."
  }
}

Invoke-Step "Preparing PostgreSQL" {
  $dbPassword = ConvertFrom-ProtectedString $settings.DatabasePassword
  if (-not (Test-PodmanResource @('network','inspect',$networkName))) {
    podman network create $networkName | Out-Null
  }
  if (-not (Test-PodmanResource @('volume','inspect',$volumeName))) {
    podman volume create $volumeName | Out-Null
  }

  if (Test-PodmanResource @('container','exists',$DatabaseContainerName)) {
    podman start $DatabaseContainerName *> $null
  } else {
    $dbArgs = @(
      'run','-d','--name',$DatabaseContainerName,
      '--network',$networkName,'--network-alias','database',
      '--restart','unless-stopped',
      '-e','POSTGRES_USER=amr','-e',("POSTGRES_PASSWORD={0}" -f $dbPassword),'-e','POSTGRES_DB=amrdashboard',
      '-v',("{0}:/var/lib/postgresql/data" -f $volumeName),
      'docker.io/library/postgres:16-alpine'
    )
    & podman @dbArgs | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "PostgreSQL container failed to start." }
  }
}

Invoke-Step "Replacing running application container" {
  $dbPassword = ConvertFrom-ProtectedString $settings.DatabasePassword
  $databaseURL = "postgres://amr:$([Uri]::EscapeDataString($dbPassword))@database:5432/amrdashboard?sslmode=disable"
  $dataPath = Convert-ToPodmanPath (Resolve-Path -LiteralPath "data").Path
  $runArgs = @(
    'run','-d','--replace','--name',$ContainerName,
    '--network',$networkName,'--restart','unless-stopped',
    '-p',("{0}:8090" -f $HostPort),
    '-v',("{0}:/app/data" -f $dataPath),
    '-e','PORT=8090','-e','DRISHTI_STATIC_DIR=/app/frontend/dist',
    '-e','DRISHTI_API_CONFIG=/app/data/config/api-connections.json',
    '-e',("DATABASE_URL={0}" -f $databaseURL),
    '-e',("ENCRYPTION_KEY={0}" -f $settings.EncryptionKey),
    '-e',("SESSION_SECRET={0}" -f $settings.SessionSecret),
    '-e',("ALLOWED_ORIGINS=http://localhost:{0}" -f $HostPort),
    $ImageName
  )
  & podman @runArgs | Out-Null
  if ($LASTEXITCODE -ne 0) {
    throw "AMR Health container failed to start. Check whether port $HostPort is already in use."
  }
}

Invoke-Step "Verifying app" {
  for ($attempt = 1; $attempt -le 30; $attempt++) {
    Start-Sleep -Seconds 2
    try {
      $health = Invoke-RestMethod -Uri "http://localhost:$HostPort/api/health" -TimeoutSec 5
      if ($health.ok) {
        Write-Host "DRISHTI - AMR Health is running: http://localhost:$HostPort" -ForegroundColor Green
        Unregister-ScheduledTask -TaskName 'DRISHTI AMR Health - Complete Installation' -Confirm:$false -ErrorAction SilentlyContinue
        Write-Host "Use Admin > Setup to add real plant RDS URLs."
        return
      }
    } catch { }

    $state = podman inspect $ContainerName --format '{{.State.Status}}' 2>$null
    if ($state -eq 'exited') { break }
  }

  Write-Host "`nAMR Health startup logs:" -ForegroundColor Yellow
  podman logs --tail 100 $ContainerName 2>&1 | Out-Host
  Write-Host "`nPostgreSQL startup logs:" -ForegroundColor Yellow
  podman logs --tail 50 $DatabaseContainerName 2>&1 | Out-Host
  throw "AMR Health did not become healthy at http://localhost:$HostPort. The startup logs above contain the underlying error."
}
