# DRISHTI - AMR Health Installation

This package installs the Go + React DRISHTI AMR Health app as a local Podman container.

## What Gets Installed

- A Go backend serving local API routes and the React app
- A React/TypeScript dashboard UI
- A Podman image named `drishti-amr-health`
- A container named `AMR-Health`
- A local data folder mounted into the container

## What Stays Local

These are not included in the release package and are ignored by Git:

- `data/config/api-connections.json`
- `data/rds-snapshots/`
- imported browser dashboard data

The installer creates `data/config/api-connections.json` from the sanitized example if it does not already exist.

## Windows 11 Install

Open PowerShell in the extracted package folder:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\install-windows.ps1
```

The script checks for Podman. If Podman is missing and `winget` is available, it attempts to install Podman Desktop.

Open the app:

```text
http://localhost:8088
```

## Linux Install

From the extracted package folder:

```bash
chmod +x ./scripts/install-linux.sh
./scripts/install-linux.sh
```

The script installs Podman when possible using `apt-get`, `dnf`, `yum`, `zypper`, or `apk`.

Open the app:

```text
http://localhost:8088
```

## Custom Port

Windows:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\install-windows.ps1 -HostPort 8099
```

Linux:

```bash
HOST_PORT=8099 ./scripts/install-linux.sh
```

## Add Real Plant RDS URLs

After install, open:

```text
http://localhost:8088
```

Go to `Admin > RDS API Connections` and add your real plant RDS base URLs and paths.

The default paths are:

```text
/api/agv-report/core
/api/display-scene
```

## Build a Release Package

From the repository root on Windows:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\package-release.ps1 -Version 1.0.0
```

Output files are written to `dist/`.