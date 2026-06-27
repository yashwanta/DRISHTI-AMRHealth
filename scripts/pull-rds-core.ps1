param(
  [ValidateSet("Shelbyville", "Springfield")]
  [string]$Plant = "Shelbyville",
  [string]$BaseUrl = "",
  [string]$OutDir = "data/rds-snapshots",
  [switch]$IncludeScene
)

$ErrorActionPreference = "Stop"

$knownPlants = @{
  Shelbyville = "http://10.205.22.12:8080"
  Springfield = "http://10.222.10.76:8080"
}

if (-not $BaseUrl) {
  $BaseUrl = $knownPlants[$Plant]
}

if (-not $BaseUrl) {
  throw "No RDS base URL configured for plant '$Plant'. Pass -BaseUrl explicitly."
}

$timestamp = Get-Date -Format "yyyyMMdd-HHmmss"
$plantSlug = $Plant.ToLowerInvariant()
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null

$corePath = Join-Path $OutDir "$plantSlug-core-$timestamp.json"
$coreUri = "$BaseUrl/api/agv-report/core"
Invoke-WebRequest -Uri $coreUri -UseBasicParsing -TimeoutSec 20 -OutFile $corePath
Write-Output "Saved $Plant RDS core snapshot: $corePath"

if ($IncludeScene) {
  $scenePath = Join-Path $OutDir "$plantSlug-display-scene-$timestamp.json"
  $sceneUri = "$BaseUrl/api/display-scene"
  Invoke-WebRequest -Uri $sceneUri -UseBasicParsing -TimeoutSec 60 -OutFile $scenePath
  Write-Output "Saved $Plant RDS scene snapshot: $scenePath"
}

Write-Output "Import the core JSON in DRISHTI AMR Health > Admin > RDS Core Import, then select $Plant."
