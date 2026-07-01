# DRISHTI - AMR Health Installation

This package installs the Go + React DRISHTI AMR Health app as a local Podman container.

## Host Dependencies

The installer handles the host dependency it needs:

- Windows 11: Podman Desktop, installed with `winget` when missing
- Linux: Podman plus common rootless runtime packages using `apt-get`, `dnf`, `yum`, `zypper`, or `apk`

Node.js, npm, Go, Alpine Linux, and the OpenSSH client are installed inside the container image during `podman build`. They do not need to be installed directly on the host.

## What Gets Installed

- A Podman image named `drishti-amr-health`
- A container named `AMR-Health`
- A local data folder mounted into the container at `/app/data`
- A sanitized local config file when missing: `data/config/api-connections.json`
- Local folders for snapshots and SSH key references: `data/rds-snapshots/`, `data/keys/`

## What Stays Local

These are ignored by Git and are not included in release packages:

- `data/config/api-connections.json`
- `data/rds-snapshots/`
- `data/keys/`
- imported browser dashboard data in localStorage

## Windows 11 Install

Open PowerShell in the extracted package folder:

```powershell
powershell -ExecutionPolicy Bypass -File .\Install-DRISHTI-Windows.ps1
```

Alternative direct script:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\install-windows.ps1
```

If Podman is installed for the first time and `podman` is not immediately available, open a new PowerShell window and rerun the command.

## Linux Install

From the extracted package folder:

```bash
chmod +x ./install-drishti-linux.sh ./scripts/install-linux.sh
./install-drishti-linux.sh
```

Alternative direct script:

```bash
chmod +x ./scripts/install-linux.sh
./scripts/install-linux.sh
```

## Open the App

```text
http://localhost:8088
```

## Custom Port

Windows:

```powershell
powershell -ExecutionPolicy Bypass -File .\Install-DRISHTI-Windows.ps1 -HostPort 8099
```

Linux:

```bash
HOST_PORT=8099 ./install-drishti-linux.sh
```

## Add Real Plant RDS URLs

After install, open the app and go to `Admin > RDS API Connections`.

Default RDS paths:

```text
/api/agv-report/core
/api/display-scene
```

## Build a Release Package

From the repository root on Windows:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\package-release.ps1 -Version 1.0.0
```

Output files are written to `dist/`. Release packages exclude real local API config, SSH keys, and RDS snapshots.