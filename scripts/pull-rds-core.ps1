param(
  [string]$Plant = "Shelbyville",
  [string]$BaseUrl = "",
  [string]$OutDir = "data/rds-snapshots",
  [switch]$IncludeScene,
  [string]$ConfigPath = "data/config/api-connections.json"
)

$ErrorActionPreference = "Stop"

function Normalize-PathValue([string]$Path, [string]$Fallback) {
  if ([string]::IsNullOrWhiteSpace($Path)) { $Path = $Fallback }
  if (-not $Path.StartsWith("/")) { $Path = "/$Path" }
  return $Path
}

if (-not $BaseUrl) {
  if (-not (Test-Path -LiteralPath $ConfigPath)) {
    throw "No BaseUrl was passed and local config was not found: $ConfigPath"
  }
  $connections = Get-Content -LiteralPath $ConfigPath -Raw | ConvertFrom-Json
  $connection = @($connections | Where-Object { $_.plant -eq $Plant })[0]
  if (-not $connection) { throw "No RDS connection configured for plant '$Plant' in $ConfigPath" }
  $BaseUrl = [string]$connection.baseUrl
  $corePath = Normalize-PathValue ([string]$connection.corePath) "/api/agv-report/core"
  $scenePath = Normalize-PathValue ([string]$connection.scenePath) "/api/display-scene"
} else {
  $corePath = "/api/agv-report/core"
  $scenePath = "/api/display-scene"
}

$BaseUrl = $BaseUrl.TrimEnd("/")
$timestamp = Get-Date -Format "yyyyMMdd-HHmmss"
$plantSlug = $Plant.ToLowerInvariant() -replace '[^a-z0-9]+','-'
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null

$corePathOut = Join-Path $OutDir "$plantSlug-core-$timestamp.json"
$coreUri = "$BaseUrl$corePath"
Invoke-WebRequest -Uri $coreUri -UseBasicParsing -TimeoutSec 20 -OutFile $corePathOut
Write-Output "Saved $Plant RDS core snapshot: $corePathOut"

if ($IncludeScene) {
  $scenePathOut = Join-Path $OutDir "$plantSlug-display-scene-$timestamp.json"
  $sceneUri = "$BaseUrl$scenePath"
  Invoke-WebRequest -Uri $sceneUri -UseBasicParsing -TimeoutSec 60 -OutFile $scenePathOut
  Write-Output "Saved $Plant RDS scene snapshot: $scenePathOut"
}

Write-Output "Import the core JSON in DRISHTI AMR Health > Admin > RDS Core Import, then select $Plant."