# DRISHTI Operational Security Memory

Use this note as standing guidance when adding Fleet Manager, RDS, AMR, and DRISHTI features.

## Core Rule

DRISHTI should be a read-only observability and health tool by default. Any feature that can move, start, stop, assign, or recover an AMR must be treated as a separate high-risk control feature with explicit approval, role checks, audit logging, and plant safety review.

## Fleet Manager

- Prefer read-only Fleet Manager integration first: robot status, assigned task, current station, battery, alarms, connection state, and last update time.
- Do not send motion, task-start, task-cancel, or route commands from DRISHTI unless the feature is explicitly approved and protected.
- If command APIs are discovered, show only whether they appear available. Do not expose command buttons by default.
- Store Fleet Manager base URLs in Go/backend configuration or environment variables, not React source.
- Keep any session token, cookie, API key, or credential in backend-only storage.
- Add audit records before enabling any future write/action endpoint.

## RDS

- Treat RDS as the source of map, AMR position, robot health, and report data.
- RDS response snapshots should stay local and ignored by Git.
- Plant RDS API URLs may be configuration, but tokens and session cookies must never be committed or bundled into React.
- If RDS data is shown in the UI, label whether it is live, cached, or imported.
- Avoid hardcoding plant IPs in frontend files. Put plant endpoints in Go config or environment variables.
- If a new RDS endpoint is added, document whether it is read-only or capable of control.

## AMR SSH and Wi-Fi RSSI

- Use read-only AMR SSH accounts for RSSI collection.
- Store private key paths only, never private key contents.
- Treat credential references as write-only from the UI. Do not display the saved key path back to users.
- The Go backend should read SSH keys at runtime from a mounted path such as `/app/data/keys/...`.
- Never log key paths, key contents, tokens, or full SSH command lines containing credential paths.
- Auto-detect Wi-Fi interfaces instead of assuming `wlan0`.
- RSSI collection should time out quickly and return clear status: available, partial, failed, or timeout.
- If an AMR has no Wi-Fi interface visible, report that clearly instead of showing only an exit code.

## DRISHTI Application

- Keep DRISHTI local-first for sensitive plant data.
- Commit app code, docs, install scripts, and examples only.
- Do not commit local RDS snapshots, imported telemetry, keys, tokens, cookies, or production config.
- Frontend React code should not contain plant secrets, session tokens, SSH usernames tied to credentials, or private IPs that should remain operationally sensitive.
- Backend endpoints should validate inputs, use short timeouts, sanitize command output, and avoid returning secrets.
- Every feature that imports or scans live plant data should state where the data is stored.
- Every new integration should include a quick source scan before commit, for example:

```powershell
rg -n "KNOWN_SSH_USER|KNOWN_PRIVATE_IP|session[_-]?token|Bearer|BEGIN (RSA |OPENSSH |)PRIVATE KEY" frontend/src
```

## Recommended Build Path

1. Keep DRISHTI read-only for Fleet Manager, RDS, and AMR telemetry.
2. Improve confidence maps with live RSSI and historical comparisons.
3. Add clear labels for live versus cached data.
4. Add role-based access before any operational controls.
5. Add audit logs before any command-capable endpoint.
6. Review plant safety impact before enabling command features.

## Never Do

- Never commit credentials or live RDS response dumps.
- Never put session tokens or private key paths in React bundle output.
- Never run AMR motion commands from DRISHTI without a reviewed control design.
- Never hide whether data is estimated, stale, cached, or live.
- Never let SSH/RDS requests hang indefinitely.
