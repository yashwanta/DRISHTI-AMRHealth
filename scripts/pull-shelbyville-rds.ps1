param(
  [string]$BaseUrl = "http://10.205.22.12:8080",
  [string]$OutDir = "data/rds-snapshots",
  [switch]$IncludeScene
)

$ErrorActionPreference = "Stop"
$timestamp = Get-Date -Format "yyyyMMdd-HHmmss"
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null

$corePath = Join-Path $OutDir "shelbyville-core-$timestamp.json"
$coreUri = "$BaseUrl/api/agv-report/core"
Invoke-WebRequest -Uri $coreUri -UseBasicParsing -TimeoutSec 20 -OutFile $corePath
Write-Output "Saved RDS core snapshot: $corePath"

if ($IncludeScene) {
  $scenePath = Join-Path $OutDir "shelbyville-display-scene-$timestamp.json"
  $sceneUri = "$BaseUrl/api/display-scene"
  Invoke-WebRequest -Uri $sceneUri -UseBasicParsing -TimeoutSec 60 -OutFile $scenePath
  Write-Output "Saved RDS scene snapshot: $scenePath"
}

Write-Output "Import the core JSON in DRISHTI AMR Health > Admin > Shelbyville RDS Core Import."
