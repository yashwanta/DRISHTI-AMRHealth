# DRISHTI - AMR Health

Local dashboard for AMR health, log investigation, discovery, Wi-Fi heat maps,
and plant configuration.

## Run with Podman Desktop

```powershell
podman build -t drishti-amr-health .
podman rm -f AMR-Health
podman run -d --name AMR-Health -p 8088:80 drishti-amr-health
```

Open:

```text
http://localhost:8088
```

## What is included

- AMR Health dashboard with plant filters, bad-zone ranking, and AMR detail view
- Log investigation page with topic, severity, source, server, VM, AMR, and date filters
- Discovery page for AMR position, RSSI, AP/BSSID, SSID, channel, band, reconnect,
  offline, Roboshop/RDS, Fleet Manager, Ubuntu, Proxmox, and VM data
- AMR Wi-Fi heat map with signal, disconnect, reconnect, offline, and roaming overlays
- Admin configuration for plants, AMRs, Fleet Manager servers, log sources,
  custom troubleshooting commands, and RSSI thresholds
- Historical report views for worst AMRs, bad zones, AP/roaming issues, and
  correlated infrastructure events

The first version uses browser local storage and seeded sample data so the app is
usable immediately. Discovery is intentionally explicit about which data points
are available, partial, or missing before the heat map is treated as reliable.
