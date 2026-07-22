# DRISHTI AMR Health

DRISHTI AMR Health is a standalone Go and React application for monitoring AMR
fleet health, reviewing RDS and system logs, investigating incidents, collecting
Wi-Fi RSSI through AMR-to-TP-Link connections, and maintaining plant maps and
scan history.

AMR Health runs independently from DRISHTI SiteOps. Its application container,
PostgreSQL database, Podman network, persistent data, and credentials are kept
separate from SiteOps.

## Download for Windows

Download the current offline Windows installer from the official release page:

**[Download DRISHTI AMR Health v0.6.2](https://github.com/yashwanta/DRISHTI-AMRHealth/releases/tag/v0.6.2)**

The Windows x64 EXE contains:

- the compiled DRISHTI AMR Health application (no source tree);
- PostgreSQL 16;
- the official Podman for Windows 5.8.3 installer; and
- scripts that initialize and start the local runtime.

### Windows installation

1. Download `DRISHTI-AMRHealth-Setup-0.6.2-Windows-x64.exe` and its `.sha256`
   file from the release page.
2. Verify the EXE checksum if required by your organization.
3. Run the EXE as Administrator.
4. Enter the Agent API key when prompted. The key is not embedded in the EXE.
5. Allow a Windows restart if Podman needs to enable WSL 2 or virtualization
   features.
6. Open **[http://localhost:8099](http://localhost:8099)**.

The installer starts Podman, PostgreSQL, and DRISHTI immediately, verifies the
application health endpoint, and registers an automatic startup task for the
installing Windows account so the complete runtime returns after later logons.

The current installer is not Authenticode-signed, so Windows SmartScreen may
display `Unknown publisher`. Verify that the file came from the release page and
that its SHA-256 checksum matches the attached checksum file.

## Linux plant-server installation

The source-free Linux package is intended for a server that users can reach on
the plant network. Build the package from a trusted release workstation:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\package-runtime-linux.ps1 -Version 0.6.2
```

Copy the resulting `.tar.gz` to the plant server, extract it, and run:

```bash
sudo bash ./install-drishti-amr-health.sh \
  --public-url http://SERVER-IP:8099 \
  --open-firewall
```

Replace `SERVER-IP` with the server's real plant-network IP. Users then open
`http://SERVER-IP:8099`; `localhost` only works from the server itself.

## Application areas

- **Dashboard** — fleet and incident overview.
- **Logs** — server, VM, application, network, and AMR/RDS evidence.
- **RDS Logs** — RoboWatch/FleetManager connectivity and log collection.
- **Agent** — rule and LLM-assisted incident explanation and remediation.
- **AMR Fleet** — RDS-derived AMR state and connection health.
- **AMR Logs** — robot-specific log evidence.
- **WiFi Overview** — current Wi-Fi connection overview.
- **WiFi Signal Strength** — live TP-Link RSSI, SNR, channel, and band data.
- **Scans** — saved map snapshots and scan history.
- **Reports** — operational reports.

Administrative permissions can control access to User Management, Discovery,
Heat Map, Servers, Sync Jobs, and Change Password.

## Runtime data and secrets

The Windows runtime stores persistent files under:

```text
C:\ProgramData\DRISHTI-AMRHealth
```

The Agent API key is requested during installation and created as a Podman
secret. It is not included in the Git repository, application image, release
EXE, or `.env` file. A local computer administrator can still inspect local
runtime resources; use a hosted LLM proxy when complete key isolation is
required.

Plant API configuration, SSH known hosts, private keys, RDS snapshots, and
PostgreSQL data are also local runtime data and must not be committed to Git.

## Wi-Fi RSSI connection model

For TP-Link RSSI, DRISHTI connects to an AMR's plant-network address over SSH
(commonly port `8022`). From that AMR connection it reaches the TP-Link at its
fixed robot-local address:

```text
http://192.168.1.254
```

Do not replace this value with the AMR plant-network IP. Each AMR provides the
route to its own TP-Link device. TP-Link sessions are cached per AMR, and lockout
responses are backed off before authentication is attempted again.

## Developer setup

End users do not need Git, Go, Node.js, or npm. These instructions are only for
developers working from the repository.

Build and start the standalone Podman deployment on port `8099`:

```powershell
powershell -ExecutionPolicy Bypass -File .\deploy\standalone\start.ps1 -Build
```

Open **[http://localhost:8099](http://localhost:8099)**.

Run backend tests:

```powershell
cd backend
$env:GOCACHE = "$PWD\.gocache"
go test ./...
```

Run the frontend development server:

```powershell
cd frontend
npm.cmd install
npm.cmd run dev
```

## Project layout

- `backend/` — Go API, authentication, RDS proxy, SSH, Wi-Fi, and Agent logic.
- `frontend/` — React and TypeScript application.
- `deploy/standalone/` — independent AMR Health development deployment.
- `deploy/runtime-windows/` — source-free Windows runtime scripts.
- `deploy/runtime-linux/` — source-free Linux plant-server runtime scripts.
- `deploy/windows-exe/` — offline Windows EXE definition.
- `scripts/` — release and installer build automation.
- `data/config/api-connections.example.json` — sanitized API configuration
  example; never place real passwords in this file.

See [INSTALL.md](INSTALL.md) for packaging details and advanced installation
options.

## Repository

Source and releases: **[yashwanta/DRISHTI-AMRHealth](https://github.com/yashwanta/DRISHTI-AMRHealth)**

Contributors are listed in [CONTRIBUTORS.md](CONTRIBUTORS.md).
