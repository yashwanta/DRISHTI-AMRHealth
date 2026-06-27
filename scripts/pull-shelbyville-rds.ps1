param(
  [string]$BaseUrl = "",
  [string]$OutDir = "data/rds-snapshots",
  [switch]$IncludeScene
)

$scriptPath = Join-Path $PSScriptRoot "pull-rds-core.ps1"
& $scriptPath -Plant Shelbyville -BaseUrl $BaseUrl -OutDir $OutDir -IncludeScene:$IncludeScene
