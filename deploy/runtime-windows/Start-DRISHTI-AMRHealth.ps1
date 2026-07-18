param(
    [string]$InstallRoot = (Join-Path $env:ProgramData 'DRISHTI-AMRHealth')
)

$ErrorActionPreference = 'Stop'
$settingsPath = Join-Path $InstallRoot 'runtime-settings.clixml'
if (-not (Test-Path -LiteralPath $settingsPath)) {
    throw "Runtime settings are missing: $settingsPath"
}

function ConvertFrom-ProtectedString([System.Security.SecureString]$value) {
    $ptr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($value)
    try { return [Runtime.InteropServices.Marshal]::PtrToStringBSTR($ptr) }
    finally { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($ptr) }
}

function Convert-ToPodmanPath([string]$path) {
    $full = [IO.Path]::GetFullPath($path)
    $drive = $full.Substring(0, 1).ToLowerInvariant()
    $rest = $full.Substring(2).Replace('\', '/')
    return "/mnt/$drive$rest"
}

$settings = Import-Clixml -LiteralPath $settingsPath
$dbPassword = ConvertFrom-ProtectedString $settings.DatabasePassword
$databaseURL = "postgres://amr:$([Uri]::EscapeDataString($dbPassword))@database:5432/amrdashboard?sslmode=disable"
$dataPath = Convert-ToPodmanPath (Join-Path $InstallRoot 'data')
$knownHosts = Convert-ToPodmanPath (Join-Path $InstallRoot 'data\ssh\known_hosts')

podman machine start *> $null
podman network inspect drishti-amr-health-runtime *> $null
if ($LASTEXITCODE -ne 0) { podman network create drishti-amr-health-runtime | Out-Null }
podman volume inspect drishti-amr-health-runtime-db *> $null
if ($LASTEXITCODE -ne 0) { podman volume create drishti-amr-health-runtime-db | Out-Null }

podman container exists AMR-Health-DB
if ($LASTEXITCODE -ne 0) {
    podman run -d --name AMR-Health-DB `
        --network drishti-amr-health-runtime --network-alias database `
        --restart unless-stopped `
        -e POSTGRES_USER=amr -e POSTGRES_PASSWORD=$dbPassword -e POSTGRES_DB=amrdashboard `
        -v drishti-amr-health-runtime-db:/var/lib/postgresql/data `
        docker.io/library/postgres:16-alpine | Out-Null
} else {
    podman start AMR-Health-DB *> $null
}

$secretArgs = @()
podman secret inspect drishti_llm_api_key *> $null
if ($LASTEXITCODE -eq 0) {
    $secretArgs = @('--secret', 'drishti_llm_api_key,target=llm_api_key')
}

$runArgs = @(
    'run','-d','--replace','--name','AMR-Health',
    '--network','drishti-amr-health-runtime','--restart','unless-stopped',
    '-p',("{0}:8090" -f $settings.HostPort),
    '-v',("{0}:/app/data" -f $dataPath),
    '-v',("{0}:/app/known_hosts:ro" -f $knownHosts),
    '-e','PORT=8090','-e','DRISHTI_STATIC_DIR=/app/frontend/dist',
    '-e','DRISHTI_API_CONFIG=/app/data/config/api-connections.json',
    '-e',("DATABASE_URL={0}" -f $databaseURL),
    '-e',("ENCRYPTION_KEY={0}" -f $settings.EncryptionKey),
    '-e',("SESSION_SECRET={0}" -f $settings.SessionSecret),
    '-e',("ALLOWED_ORIGINS=http://localhost:{0}" -f $settings.HostPort),
    '-e',("OLLAMA_URL={0}" -f $settings.LLMURL),
    '-e',("OLLAMA_MODEL={0}" -f $settings.LLMModel),
    '-e','LLM_API_KEY_FILE=/run/secrets/llm_api_key'
) + $secretArgs + @('localhost/drishti-amr-health:runtime')

& podman @runArgs | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'AMR Health container failed to start.' }

for ($attempt = 1; $attempt -le 20; $attempt++) {
    Start-Sleep -Seconds 2
    try {
        $health = Invoke-RestMethod -Uri ("http://localhost:{0}/api/health" -f $settings.HostPort) -TimeoutSec 5
        if ($health.ok) {
            Write-Host ("DRISHTI AMR Health is ready: http://localhost:{0}" -f $settings.HostPort) -ForegroundColor Green
            exit 0
        }
    } catch { }
}
throw 'AMR Health did not become healthy. Run: podman logs AMR-Health'
