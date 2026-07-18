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
powershell -ExecutionPolicy Bypass -File .\deploy\install\install-windows.ps1
```

If Podman is installed for the first time and `podman` is not immediately available, open a new PowerShell window and rerun the command.

## Linux Install

From the extracted package folder:

```bash
chmod +x ./install-drishti-linux.sh ./deploy/install/install-linux.sh
./install-drishti-linux.sh
```

Alternative direct script:

```bash
chmod +x ./deploy/install/install-linux.sh
./deploy/install/install-linux.sh
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

## Build a Source-Free Windows Runtime Installer

The runtime installer contains prebuilt AMR Health and PostgreSQL container
images. Target users do not need Git, Go, Node.js, npm, or the source tree.

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\package-runtime-windows.ps1 -Version 1.0.0
```

The generated ZIP and SHA-256 checksum are written to `dist/`. The installer
prompts for the Agent API key on the target computer and creates it as a Podman
secret; the key is not embedded in the ZIP or application image.

## Build the Offline Windows EXE

The offline EXE embeds the official Podman for Windows installer plus the
source-free DRISHTI and PostgreSQL images. A target user does not need Git,
Go, Node.js, npm, or a separate Podman download.

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\package-windows-exe.ps1 -Version 0.5.0
```

Run the resulting `DRISHTI-AMRHealth-Setup-0.5.0-Windows-x64.exe` as an
administrator. Windows virtualization and WSL 2 support must be available;
Podman may request a restart when Windows first enables those platform
features. The Agent API key is requested during runtime initialization and is
never built into the EXE.

## Build a Source-Free Linux Plant-Server Installer

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\package-runtime-linux.ps1 -Version 1.0.0
```

The generated `.tar.gz` contains the prebuilt AMR Health and PostgreSQL images,
installs a persistent systemd service, and can expose the application on the
plant LAN. Use `--public-url http://SERVER-IP:8099 --open-firewall` during
installation so browsers elsewhere in the plant use the correct origin.
