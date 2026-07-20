# Wi-Fi Heatmap Implementation Plan

## Existing implementation digest

1. **AMR coordinates** — `frontend/src/AmrHealthApp.tsx` reads `data.report[].rbk_report.x/y` from the RDS Core proxy. The current map uses the raw RDS coordinate system and renders Y as `-y` in an SVG view box; paths, bins, robots, zoom, and pan share that transform.
2. **Wi-Fi telemetry** — `/api/discovery` supplies RSSI, SNR, AP/BSSID-like `ap_name`, channel, band, source, and `last_seen`. Values originate from RDS snapshots/fallback or live AMR SSH/TP-Link discovery in `backend/main.go`. The current discovery model does not expose noise, frequency, packet loss, or a distinct source ID.
3. **Identity matching** — the UI currently joins RDS and Wi-Fi values by AMR name within a selected plant. There is no persisted identity mapping table. The new collector therefore requires exact, case-insensitive plant and AMR agreement and refuses ambiguous/missing matches.
4. **Plant/map relationships** — API connections are keyed by plant. Scene maps are held in browser state by plant, with optional browser-only version history keyed by scene MD5. RDS robots report `current_map_md5`; the scene proxy reports scene/map MD5. There is no normalized database map table.
5. **Existing buttons** — Save Scan Point builds confidence samples for every visible AMR and stores them in browser `localStorage`. Start Scan Recording polls RDS every eight seconds and appends the same confidence samples. Neither action creates a synchronized server record or durable recording session.
6. **Existing persistence/APIs** — Wi-Fi discovery readings are a latest-value JSON cache. PostgreSQL has no scan-point or scan-session tables, and there are no scan/heatmap APIs.

## Safe implementation boundary

- Add an isolated `/admin/wifi-heatmap` page protected by the existing `heatmap` permission; leave the current AMR map and its buttons unchanged.
- Add idempotent PostgreSQL schema for durable scan points and recording sessions.
- Add authenticated, permission-scoped APIs for validation, single/batch collection, sessions, raw points, and grid aggregation.
- Reuse the RDS scene parser and exact `x/-y` SVG coordinate transform on the new page.
- Collect one synchronized record only when plant, AMR, map ID/version, source plant, and timestamp tolerance checks pass.
- Detect disconnects from connection state and roaming from BSSID changes; never synthesize measurements for empty cells.
- Record moving AMRs about every 2 seconds and stationary AMRs about every 10 seconds, configurable in the page. Deduplicate unchanged position/Wi-Fi tuples in the database.
- Render independent raw-point, grid/heatmap, disconnect, roaming, path, label, AP, and unknown-area layers. Reporting remains deliberately deferred.

## Verification

- Backend handler tests for validation, event detection, metric/aggregation parsing, and grid confidence.
- Frontend production type check/build and backend unit tests.
- Manual development verification using live configured RDS/discovery data; no production mock data is auto-started.

## Live-source information still required

- Canonical AMR identity if RDS UUID and Wi-Fi source name ever differ.
- Whether `ap_name` is always a BSSID, an AP label, or sometimes an SSID.
- Authoritative position timestamp field and clock synchronization guarantees.
- Frequency/noise/heading/latency/packet-loss fields or collection commands when available.
- Formal map ID versus scene MD5 semantics and plant-to-map ownership metadata.
