# Shelbyville RDS API Discovery

Plant: Shelbyville
RDS URL: http://10.205.22.12:8080
Observed UI version: RDS F-1.10.6 B-1.8.71
Discovery date: 2026-06-27

## How the API connection was found

1. Open the RDS URL and inspect the loaded HTML.
2. Read the public JavaScript bundle names from the HTML.
3. Download the bundles locally for text inspection.
4. Search the bundles for URL-like strings, `/api/...` paths, auth calls, robot calls,
   map calls, worksite calls, and monitoring calls.
5. Probe only known read-only/status endpoints first.

Do not probe destructive routes such as delete, update, lock, unlock, upload, terminate,
or controlled robot commands unless explicitly required and authorized.

## Auth Flow Found In Front-End Bundle

The login page calls:

- `GET /admin/encrypt`
- `POST /admin/login`

The login payload is inferred from the RDS bundle:

```json
{
  "username": "<username>",
  "password": "<md5(password)> when /admin/encrypt returns true, otherwise raw password",
  "sha2Password": "sha256(md5(password), 'Rds123!') as implemented by the RDS UI bundle"
}
```

On Shelbyville, `GET /admin/encrypt` returned:

```json
{"code":200,"msg":"Success","data":true}
```

## Read-Only Endpoints That Responded Without Login

These returned HTTP 200 during discovery:

- `GET /admin/encrypt`
- `GET /admin/oauth`
- `GET /api/display-scene`
- `GET /api/agv-report/core`

Useful observed behavior:

- `/api/display-scene` returns a scene/map object and an `md5` value.
- `/api/agv-report/core` returns live/core map and robot status data, including model/map metadata.

## High-Value Endpoints For DRISHTI - AMR Health

Map and scene:

- `GET /api/display-scene`
- `GET /api/download-scene`
- `POST /api/upload-scene` avoid unless intentionally uploading

Robot and AMR status:

- `GET /api/agv-report/core`
- `POST /api/stat/agvStatusCurrent`
- `POST /api/stat/agvStatus`
- `POST /api/stat/vehicleBatteryLevel`
- `POST /api/stat/vehicleBatteryLevelTrend`
- `POST /api/agv/getAllAttrAgv`
- `GET /api/agv/getDutyStatus`
- `GET /api/agv/getFireStatus`

WorkSite, stations, and plant points:

- `POST /api/work-sites/monitorSites`
- `POST /api/work-sites/siteList`
- `POST /api/work-sites/getAllSiteAreaAndGroup`
- `POST /api/work-sites/getAllExtFields`
- `POST /api/work-sites/findSiteLogByCondition`
- `GET /api/work-sites/export`

Task and evidence logs:

- `POST /api/queryTaskRecordById`
- `POST /api/queryBlocksByTaskRecordId`
- `POST /api/queryLogsByTaskRecordId`
- `POST /api/queryLogsByTaskRecordIdPageAble`
- `POST /api/findAll-taskdef/findTaskDefsByCondition`

Statistics and reports:

- `POST /api/stat/robotsStatusTime`
- `POST /api/stat/robotsStatusTimeTrend`
- `POST /api/stat/robotsAlarmsNum`
- `POST /api/stat/robotsAlarmsNumTrend`
- `POST /api/stat/robotsAlarmsTime`
- `POST /api/stat/robotsAlarmsTimeTrend`
- `POST /api/stat/agvCoreOrders`
- `POST /api/stat/agvCoreOrdersTrend`

## Browser Method For Future Discovery

1. Open RDS in Edge or Chrome.
2. Press `F12`.
3. Open the `Network` tab.
4. Filter by `Fetch/XHR`.
5. Log in.
6. Click `Monitor`, `Robot`, `WorkSite List`, `Task Records`, and `Statistics`.
7. Select each request and note:
   - Request URL
   - Method
   - Request payload
   - Response shape
   - Required cookies or headers
8. Use `Copy as cURL` for read-only requests only.

## Safe Next Step

Use a temporary read-only RDS account for live API discovery. Store credentials in a local ignored file or enter them interactively; never commit them.

Recommended local secret names:

```powershell
$env:RDS_BASE_URL = "http://10.205.22.12:8080"
$env:RDS_PLANT = "Shelbyville"
$env:RDS_USERNAME = "<temporary-readonly-user>"
$env:RDS_PASSWORD = "<password>"
```

Once credentials are available, the connector should:

1. Authenticate with `/admin/login`.
2. Save only session cookies/tokens in memory during discovery.
3. Pull `/api/display-scene` and `/api/agv-report/core` first.
4. Pull worksite and robot inventory endpoints.
5. Save sanitized snapshots into DRISHTI AMR Health for map, AMR, and bad-zone correlation.

## Core Feed Field Mapping Confirmed From Sample

The `/api/agv-report/core` response can directly populate DRISHTI AMR Health:

- AMR name: `report[].uuid` or `report[].vehicle_id`
- IP address: `report[].basic_info.ip`
- Online/disconnected: `report[].connection_status` plus `report[].undispatchable_reason.disconnect`
- Current station: `report[].rbk_report.current_station` or current order block location
- Current area: `report[].basic_info.current_area[]`
- RDS X/Y position: `report[].rbk_report.x`, `report[].rbk_report.y`
- Heading: `report[].rbk_report.angle`
- Battery: `report[].rbk_report.battery_level`
- Confidence: `report[].rbk_report.confidence`
- Network delay: `report[].network_delay`
- Emergency stop: `report[].rbk_report.emergency`
- Robot warnings/errors: `report[].rbk_report.alarms`, `report[].rbk_report.warnings`
- Core warnings: `data.alarms.warnings` and `data.warnings`
- Map/model metadata: `data.model_md5`, `data.scene_md5`, `report[].rbk_report.current_map_md5`

From the first Shelbyville sample, DRISHTI can identify:

- `AMR-02` as disconnected, battery 36%, station `PP10`, IP `10.215.48.167`
- `AMR-01` as online, battery 80%, station `PP91`, area `Area-01`, IP `10.215.48.171`

Wi-Fi RSSI/AP/SSID/channel/band are still missing from this feed. RDS provides reliable robot position and status, but Wi-Fi correlation still needs AMR Linux Wi-Fi telemetry, controller data, or logs.

## Local Import Flow

DRISHTI AMR Health now supports importing this JSON directly:

1. Save a `/api/agv-report/core` response as `.json`.
2. Open DRISHTI AMR Health.
3. Go to `Admin`.
4. Use `RDS Core Import`.
5. Select the matching plant, then import the JSON file.

The app normalizes AMR rows, RDS position points, evidence logs, and discovery status. Raw plant snapshots should stay local and should not be committed.

Helper scripts are available:

```powershell
.\scripts\pull-rds-core.ps1 -Plant Shelbyville
.\scripts\pull-rds-core.ps1 -Plant Springfield
.\scripts\pull-rds-core.ps1 -Plant Hopkinsville
.\scripts\pull-shelbyville-rds.ps1
.\scripts\pull-shelbyville-rds.ps1 -IncludeScene
```

Snapshots are written under `data/rds-snapshots/`, which is ignored by Git.
## RDS API Links

| Plant | Base URL | Core feed | Scene endpoint |
| --- | --- | --- | --- |
| Shelbyville | `http://10.205.22.12:8080` | `http://10.205.22.12:8080/api/agv-report/core` | `http://10.205.22.12:8080/api/display-scene` |
| Springfield | `http://10.222.10.76:8080` | `http://10.222.10.76:8080/api/agv-report/core` | `http://10.222.10.76:8080/api/display-scene` |
| Hopkinsville | `http://10.216.4.59:8080` | `http://10.216.4.59:8080/api/agv-report/core` | `http://10.216.4.59:8080/api/display-scene` |

These links are also visible in DRISHTI AMR Health under `Admin` > `RDS API Connections`. Changes made there are saved to browser localStorage on the local machine.

## Springfield RDS Core Endpoint

Springfield uses the same RDS core contract:

- Base URL: `http://10.222.10.76:8080`
- Core feed: `GET /api/agv-report/core`
- Response: JSON with `data.report[]`, `data.model_md5`, `data.scene_md5`, and alarm arrays.

The Springfield import path uses the same parser as Shelbyville. Select `Springfield` in Admin before importing a Springfield core JSON file so AMRs, positions, and logs are labeled correctly.

## Hopkinsville RDS Core Endpoint

Hopkinsville uses the same RDS core contract:

- Base URL: `http://10.216.4.59:8080`
- Core feed: `GET /api/agv-report/core`
- Response: JSON with `data.report[]`, `data.model_md5`, `data.scene_md5`, and alarm arrays.

The Hopkinsville import path uses the same parser as Shelbyville and Springfield. Select `Hopkinsville` in Admin before importing a Hopkinsville core JSON file so AMRs, positions, and logs are labeled correctly.
