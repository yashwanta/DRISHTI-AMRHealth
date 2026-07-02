# RDS API Discovery

DRISHTI AMR Health now runs as a local Go + React app. The Go backend reads plant RDS API connections from a local ignored file:

```text
data/config/api-connections.json
```

A sanitized template is committed at:

```text
data/config/api-connections.example.json
```

Raw RDS responses and snapshots stay local under:

```text
data/rds-snapshots/
```

Both the real API config and snapshots are ignored by Git.

## Local API Connections

Use `Admin > RDS API Connections` in the app to add or update plant RDS base URLs and paths. The standard RDS paths are:

```text
/api/agv-report/core
/api/display-scene
```

The frontend calls the local Go backend instead of directly calling plant RDS systems:

```text
GET /api/plants/{plant}/rds/core?save=1
GET /api/plants/{plant}/rds/scene?save=1
```

## Helper Script

The pull script reads the same local ignored config file:

```powershell
.\tools\rds\pull-rds-core.ps1 -Plant Shelbyville
.\tools\rds\pull-rds-core.ps1 -Plant Springfield
.\tools\rds\pull-rds-core.ps1 -Plant Hopkinsville
.\tools\rds\pull-rds-core.ps1 -Plant Shelbyville -IncludeScene
```

You can also pass a one-off base URL without saving it:

```powershell
.\tools\rds\pull-rds-core.ps1 -Plant TestPlant -BaseUrl "http://rds-host:8080"
```

## Core Feed Mapping

The `/api/agv-report/core` response can populate DRISHTI AMR Health:

- AMR name: `report[].uuid` or `report[].vehicle_id`
- IP address: `report[].basic_info.ip`
- Online/disconnected: `report[].connection_status` plus `report[].undispatchable_reason.disconnect`
- Current station: `report[].rbk_report.current_station` or current order block location
- RDS X/Y position: `report[].rbk_report.x`, `report[].rbk_report.y`
- Heading: `report[].rbk_report.angle`
- Battery: `report[].rbk_report.battery_level`
- Confidence: `report[].rbk_report.confidence`
- Network delay: `report[].network_delay`
- Emergency stop: `report[].rbk_report.emergency`
- Core warnings/errors: `data.alarms`, `data.warnings`, `data.errors`
- Map/model metadata: `data.model_md5`, `data.scene_md5`, `report[].rbk_report.current_map_md5`

Wi-Fi RSSI/AP/SSID/channel/band are still not supplied by this RDS feed. That still requires AMR Linux Wi-Fi telemetry, a wireless controller source, or logs.