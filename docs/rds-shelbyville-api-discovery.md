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
