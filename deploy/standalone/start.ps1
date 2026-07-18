param(
    [switch]$Build
)

$ErrorActionPreference = 'Stop'
$deployDir = $PSScriptRoot
$repoRoot = (Resolve-Path (Join-Path $deployDir '..\..')).Path
$envFile = Join-Path $deployDir '.env'

if (-not (Test-Path -LiteralPath $envFile)) {
    throw "Missing $envFile. Copy .env.example to .env and set its secrets first."
}

$settings = @{}
foreach ($line in Get-Content -LiteralPath $envFile) {
    $trimmed = $line.Trim()
    if (-not $trimmed -or $trimmed.StartsWith('#')) { continue }
    $parts = $trimmed -split '=', 2
    if ($parts.Count -eq 2) { $settings[$parts[0].Trim()] = $parts[1] }
}

$required = @(
    'POSTGRES_USER', 'POSTGRES_PASSWORD', 'POSTGRES_DB', 'DATABASE_URL',
    'ENCRYPTION_KEY', 'SESSION_SECRET'
)
foreach ($name in $required) {
    if (-not $settings.ContainsKey($name) -or -not $settings[$name]) {
        throw "Missing required setting $name in $envFile"
    }
    Set-Item -Path "Env:$name" -Value $settings[$name]
}

foreach ($name in @(
    'ALLOWED_ORIGINS', 'AMR_HEALTH_PORT',
    'DRISHTI_DISCOVERY_RDS_BASE_URL', 'DRISHTI_DISCOVERY_RDS_SESSION_TOKEN',
    'OLLAMA_URL', 'OLLAMA_MODEL', 'LLM_API_KEY'
)) {
    if ($settings.ContainsKey($name)) { Set-Item -Path "Env:$name" -Value $settings[$name] }
}

$port = if ($settings['AMR_HEALTH_PORT']) { $settings['AMR_HEALTH_PORT'] } else { '8099' }
$knownHosts = Join-Path $repoRoot 'data\ssh\known_hosts'
if (-not (Test-Path -LiteralPath $knownHosts)) {
    throw "Missing $knownHosts. Add independently verified SSH host fingerprints first."
}

function Convert-ToPodmanPath([string]$path) {
    $full = [System.IO.Path]::GetFullPath($path)
    $drive = $full.Substring(0, 1).ToLowerInvariant()
    $rest = $full.Substring(2).Replace('\', '/')
    return "/mnt/$drive$rest"
}

podman network inspect drishti-amr-health_default *> $null
if ($LASTEXITCODE -ne 0) { podman network create drishti-amr-health_default | Out-Null }

podman volume inspect drishti-amr-health_pgdata *> $null
if ($LASTEXITCODE -ne 0) { podman volume create drishti-amr-health_pgdata | Out-Null }

$dbExists = podman container exists AMR-Health-DB
if ($LASTEXITCODE -eq 0) {
    # Preserve an existing installation's database identity. The local .env may
    # have been created later from the example and must not silently replace or
    # reset credentials for an existing PostgreSQL volume.
    $dbInspect = (podman inspect AMR-Health-DB | ConvertFrom-Json)[0]
    $dbSettings = @{}
    foreach ($entry in $dbInspect.Config.Env) {
        $parts = $entry -split '=', 2
        if ($parts.Count -eq 2) { $dbSettings[$parts[0]] = $parts[1] }
    }
    if ($dbSettings['POSTGRES_USER'] -and $dbSettings['POSTGRES_PASSWORD'] -and $dbSettings['POSTGRES_DB']) {
        $dbUser = [Uri]::EscapeDataString($dbSettings['POSTGRES_USER'])
        $dbPassword = [Uri]::EscapeDataString($dbSettings['POSTGRES_PASSWORD'])
        $dbName = [Uri]::EscapeDataString($dbSettings['POSTGRES_DB'])
        $env:DATABASE_URL = "postgres://${dbUser}:${dbPassword}@database:5432/${dbName}?sslmode=disable"
    }
    podman start AMR-Health-DB *> $null
} else {
    podman run -d --name AMR-Health-DB `
        --network drishti-amr-health_default `
        --network-alias database `
        --restart unless-stopped `
        --env POSTGRES_USER --env POSTGRES_PASSWORD --env POSTGRES_DB `
        -v drishti-amr-health_pgdata:/var/lib/postgresql/data `
        docker.io/library/postgres:16-alpine | Out-Null
}

if ($Build) {
    podman build -t localhost/drishti-amr-health:latest -f (Join-Path $repoRoot 'Containerfile') $repoRoot
    if ($LASTEXITCODE -ne 0) { throw 'AMR Health image build failed.' }
}

$env:ALLOWED_ORIGINS = if ($settings['ALLOWED_ORIGINS']) { $settings['ALLOWED_ORIGINS'] } else { "http://localhost:$port" }
$env:PORT = '8090'
$env:DRISHTI_STATIC_DIR = '/app/frontend/dist'
$env:DRISHTI_API_CONFIG = '/app/data/config/api-connections.json'
$dataPath = Convert-ToPodmanPath (Join-Path $repoRoot 'data')
$knownHostsPath = Convert-ToPodmanPath $knownHosts

podman run -d --replace --name AMR-Health `
    --network drishti-amr-health_default `
    --restart unless-stopped `
    -p "${port}:8090" `
    -v "${dataPath}:/app/data" `
    -v "${knownHostsPath}:/app/known_hosts:ro" `
    --env ALLOWED_ORIGINS --env DATABASE_URL --env ENCRYPTION_KEY `
    --env SESSION_SECRET --env PORT --env DRISHTI_STATIC_DIR `
    --env DRISHTI_API_CONFIG `
    --env DRISHTI_DISCOVERY_RDS_BASE_URL `
    --env DRISHTI_DISCOVERY_RDS_SESSION_TOKEN `
    --env OLLAMA_URL --env OLLAMA_MODEL --env LLM_API_KEY `
    localhost/drishti-amr-health:latest | Out-Null

if ($LASTEXITCODE -ne 0) { throw 'AMR Health container start failed.' }
Write-Output "Standalone AMR Health started at http://localhost:$port"
