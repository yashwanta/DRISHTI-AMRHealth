# Standalone AMR Health

This deployment runs AMR Health with its own PostgreSQL database, Podman
network, persistent volume, runtime data, and SSH trusted-host file. It does
not connect to or modify the SiteOps containers.

## First-time setup

1. Copy `.env.example` to `.env` and replace every `CHANGE_ME` value. In
   `DATABASE_URL`, URL-encode special characters in the database password.
2. Create `data/ssh/known_hosts`. Add only fingerprints that have been
   independently verified with the plant owner.
3. On this Windows host, run:

   `powershell -ExecutionPolicy Bypass -File deploy/standalone/start.ps1 -Build`

   On a host with a Compose provider installed, you can instead run:

   `podman compose --env-file deploy/standalone/.env -f deploy/standalone/compose.yml up -d --build`

AMR Health is then available at `http://localhost:8099` unless the port was
changed in `.env`.

## Operations

- Start on Windows: `powershell -ExecutionPolicy Bypass -File deploy/standalone/start.ps1`
- Start with Compose: `podman compose --env-file deploy/standalone/.env -f deploy/standalone/compose.yml up -d`
- Stop: `podman compose --env-file deploy/standalone/.env -f deploy/standalone/compose.yml down`
- Logs: `podman logs -f AMR-Health`
- Database logs: `podman logs -f AMR-Health-DB`

Do not add `--volumes` to the stop command unless the standalone AMR Health
database is intentionally being deleted.

The initial production migration may copy the current SiteOps database once.
After that point the two databases are independent; future AMR Health syncs
are executed by the AMR Health service itself.
