# DRISHTI - AMR Health

Local Go + React dashboard for AMR health, RDS core imports, API connection management, log investigation, discovery, Wi-Fi heat maps, and plant configuration.

## One-Command Install

Windows 11:

```powershell
powershell -ExecutionPolicy Bypass -File .\Install-DRISHTI-Windows.ps1
```

Linux:

```bash
chmod +x ./install-drishti-linux.sh ./deploy/install/install-linux.sh
./install-drishti-linux.sh
```

The installer verifies/installs Podman, builds the container image, creates local ignored data folders, starts `AMR-Health`, and verifies `http://localhost:8088/api/health`.

## Project Layout

- `.github/workflows/` - GitHub Actions validation workflow
- `backend/` - Go backend and local RDS proxy
- `frontend/` - React + TypeScript UI
- `deploy/install/` - Windows and Linux installer implementations
- `deploy/compose/` - optional Podman Compose file
- `tools/rds/` - local RDS snapshot helper scripts
- `scripts/` - release/package automation

See `docs/project-structure.md` for the full layout. See `docs/operational-security-memory.md` for standing Fleet Manager, RDS, AMR, and DRISHTI security guidance.

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


## Confidence Level Scans

Use the Heatmap and Scans pages to record AMR confidence along the plant map without sending motion commands.

- Heatmap markers and saved scan overlays use the same confidence thresholds: `75%+` green, `51-74%` yellow, and `50% and below` red.
- `Start Scan Recording` pulls RDS core on a timer, saves AMR confidence samples locally, and pulls the plant scene map if it is missing.
- With `Stop at home` enabled, the recorder stays active until the selected AMR leaves its home location and then returns.
- `Save Scan Point` stores the current plant confidence snapshot manually.
- The `Scans` page lists saved scan maps by plant and time, lets you open a previous scan on the Heatmap page, and lets you delete one scan map or all scans visible in the current filter.
- Saved scan history is stored in browser `localStorage` on localhost and is pruned to a rolling 5-day window.
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