# DRISHTI - AMR Health

Local Go + React dashboard for AMR health, RDS core imports, API connection management, log investigation, discovery, Wi-Fi heat maps, and plant configuration.

## Architecture

```text
React + TypeScript UI
        |
        v
Go backend on localhost
        |
        +-- Local API connection config: data/config/api-connections.json
        +-- Local RDS snapshots: data/rds-snapshots/
        +-- Shelbyville / Springfield / Hopkinsville RDS core proxy
```

Raw RDS pulls and local API connection config are ignored by Git. The committed file `data/config/api-connections.example.json` is only a sanitized template.

## Run With Podman

```powershell
podman build -t drishti-amr-health .
podman rm -f AMR-Health
podman run -d --name AMR-Health -p 8088:8090 -v ${PWD}\data:/app/data drishti-amr-health
```

Open:

```text
http://localhost:8088
```

## Development

Backend:

```powershell
$env:GOCACHE = "$PWD\.gocache"
go run ./backend
```

Frontend:

```powershell
cd frontend
npm.cmd install
npm.cmd run dev
```

## Local API Routes

```text
GET  /api/health
GET  /api/connections
POST /api/connections
PUT  /api/connections
GET  /api/plants/{plant}/rds/core?save=1
GET  /api/plants/{plant}/rds/scene?save=1
```

Use `Admin > RDS API Connections` to manage plant URLs locally. Use `Admin > RDS Core Import` to pull live RDS core data through Go or import a saved JSON response.