import React, { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import "./styles.css";

type View = "dashboard" | "logs" | "discovery" | "heatmap" | "reports" | "admin";
type Status = "Online" | "Offline" | "Disconnected" | "Unknown";
type Severity = "High" | "Medium" | "Low";
type APIConnection = { plant: string; baseUrl: string; corePath: string; scenePath: string };
type AMR = {
  id: string; name: string; plant: string; ip: string; status: Status; reconnects: number; disconnects: number; offline: string;
  worstDrop: string; rssi: number; ap: string; ssid: string; channel: string; band: string; imported?: boolean; source?: string;
  battery?: string; rdsX?: number | string; rdsY?: number | string; mapMd5?: string; modelMd5?: string; issue?: string;
  networkDelay?: number; connectionStatus?: number; connectivityReason?: string; currentStation?: string;
};
type WifiPoint = { plant: string; amr: string; x: number; y: number; rssi: number; quality: "Good" | "Weak" | "Poor" | "Critical"; ap: string; ssid: string; channel: string; band: string; reconnect: boolean; disconnect: boolean; offline: boolean; roaming: boolean; time: string; imported?: boolean; source?: string; rdsX?: number; rdsY?: number };
type LogEntry = { time: string; plant: string; amr: string; server: string; host: string; vm: string; source: string; category: string; severity: Severity; topic: string; message: string; imported?: boolean };
type BadZone = { plant: string; zone: string; disconnects: number; reconnects: number; offline: number; weak: number; roaming: number; score: number; robots?: string[]; reason?: string };
type WifiSource = { plant: string; name: string; method: "AMR SSH" | "Controller API" | "Manual Import"; host: string; username: string; secretRef: string; command: string; savedAt: string };
type WifiTestResult = { ok: boolean; method: string; host: string; message: string; output?: string; rssi?: number; ssid?: string; quality?: string };
type WifiDiscoverResult = { ok: boolean; plant: string; amr: string; host: string; command?: string; message: string; output?: string; rssi?: number; ssid?: string; quality?: WifiPoint["quality"] | "Unknown" };
type WifiDiscoverResponse = { ok: boolean; message: string; results?: WifiDiscoverResult[] | null };
type AppState = {
  amrs: AMR[]; wifiPoints: WifiPoint[]; logs: LogEntry[]; badZones: BadZone[]; sceneMaps: Record<string, SceneMap>;
  wifiSources: WifiSource[];
  discovery: { point: string; status: string; source: string; command: string; gap: string }[];
  rdsImportNote: string; uploadedMap: string;
};
type NormalizedRds = { amrs: AMR[]; points: WifiPoint[]; logs: LogEntry[]; summary: { plant: string; source: string; createdOn: string; modelMd5: string; sceneMd5: string; robots: number; disconnected: number } };
type MapPoint = { name: string; x: number; y: number; kind?: string };
type MapPath = { name: string; className: string; start: MapPoint; end: MapPoint; control1?: MapPoint; control2?: MapPoint };
type MapPolygon = { name: string; points: MapPoint[] };
type SceneMap = { plant: string; area: string; md5: string; bounds: { minX: number; minY: number; maxX: number; maxY: number }; paths: MapPath[]; points: MapPoint[]; bins: MapPolygon[] };

const STORAGE_KEY = "drishti-amr-health-react-v2";
const LEGACY_STORAGE_KEYS = ["drishti-amr-health-react-v1", "drishti-amr-health-state"];
const seed: AppState = {
  amrs: [],
  wifiPoints: [],
  logs: [],
  badZones: [],
  sceneMaps: {},
  wifiSources: [],
  discovery: [
    { point: "AMR live position", status: "Not Run", source: "Go RDS proxy", command: "GET /api/plants/{plant}/rds/core", gap: "Needs configured plant URL" },
    { point: "AMR map X/Y coordinates", status: "Not Run", source: "RDS core", command: "rbk_report.x / rbk_report.y", gap: "Scene geometry alignment still needed" },
    { point: "Wi-Fi RSSI", status: "Not Run", source: "AMR Linux Wi-Fi command", command: "iw dev wlan0 link", gap: "Requires AMR SSH or controller telemetry" },
    { point: "RDS map and model data", status: "Not Run", source: "RDS core", command: "model_md5, scene_md5", gap: "Available after import or live pull" }
  ],
  rdsImportNote: "No RDS core JSON imported yet. Use Pull Selected RDS to load live plant data.",
  uploadedMap: ""
};

function slug(value: string) { return value.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/(^-|-$)/g, ""); }
function unique(values: string[]) { return [...new Set(values.filter(Boolean))].sort(); }
function badge(value: string) { return <span className={`badge ${value.toLowerCase().replace(/\s+/g, "-")}`}>{value}</span>; }
function csvCell(value: unknown) { return `"${String(value ?? "").replace(/"/g, '""')}"`; }
function ssidLabel(value?: string | null) { const text = (value || "").trim(); return text && !/(not found|not reported|not captured|not connected|unknown|no such|command not found|error)/i.test(text) ? text : "not captured yet"; }
function isPlaceholderCredential(value: string) { return !value.trim() || /cyberark|ssh key reference|public key|ssh-rsa|ssh-ed25519|begin public key/i.test(value); }
function qualityFromConnection(disconnected: boolean, networkDelay?: number, _issue = false): WifiPoint["quality"] {
  if (disconnected) return "Critical";
  if (typeof networkDelay === "number" && Number.isFinite(networkDelay)) {
    if (networkDelay >= 150) return "Poor";
    if (networkDelay >= 80) return "Weak";
  }
  return "Good";
}
function rssiEstimate(quality: WifiPoint["quality"]) { return quality === "Good" ? -58 : quality === "Weak" ? -70 : quality === "Poor" ? -80 : -90; }
function connectivityQuality(amr: AMR): WifiPoint["quality"] {
  const disconnected = amr.status === "Disconnected" || amr.status === "Offline" || amr.connectionStatus === 0;
  return qualityFromConnection(disconnected, amr.networkDelay, amr.issue?.toLowerCase().includes("emergency") || amr.issue?.toLowerCase().includes("error"));
}
function connectivityReason(amr: AMR) {
  const quality = connectivityQuality(amr);
  if (quality === "Good") return "RDS reports active connection";
  if (amr.connectivityReason) return amr.connectivityReason;
  if (quality === "Critical") return amr.status === "Online" ? "RDS reports an error state" : "RDS reports robot disconnected or offline";
  if (quality === "Poor") return `High RDS network delay (${amr.networkDelay} ms)`;
  if (quality === "Weak") return `Elevated RDS network delay (${amr.networkDelay} ms)`;
  return "RDS reports active connection. RSSI source not connected yet.";
}
function deriveBadZones(amrs: AMR[], points: WifiPoint[] = []): BadZone[] {
  const buckets = new Map<string, BadZone>();
  const pointByAmr = new Map(points.map((point) => [point.amr, point]));
  amrs.forEach((amr) => {
    const point = pointByAmr.get(amr.name);
    const quality = point?.quality || connectivityQuality(amr);
    const isIssue = quality !== "Good" || amr.disconnects > 0 || amr.reconnects > 0 || amr.status !== "Online";
    if (!isIssue) return;
    const zoneName = amr.worstDrop || amr.currentStation || "Unknown location";
    const key = `${amr.plant}-${zoneName}`;
    const current = buckets.get(key) || { plant: amr.plant, zone: zoneName, disconnects: 0, reconnects: 0, offline: 0, weak: 0, roaming: 0, score: 0, robots: [], reason: "" };
    current.disconnects += Number(amr.disconnects || 0) + (quality === "Critical" ? 1 : 0);
    current.reconnects += Number(amr.reconnects || 0);
    current.offline += amr.status === "Offline" || amr.status === "Disconnected" ? 1 : 0;
    current.weak += quality === "Weak" || quality === "Poor" ? 1 : 0;
    current.roaming += point?.roaming ? 1 : 0;
    current.robots = unique([...(current.robots || []), amr.name]);
    current.reason = `${quality}: ${connectivityReason(amr)}`;
    current.score = Math.min(100, current.disconnects * 34 + current.offline * 24 + current.weak * 18 + current.reconnects * 8 + current.roaming * 6);
    buckets.set(key, current);
  });
  return [...buckets.values()].sort((a, b) => b.score - a.score || a.zone.localeCompare(b.zone));
}
function loadState(): AppState { try { return { ...seed, ...(JSON.parse(localStorage.getItem(STORAGE_KEY) || "null") || {}) }; } catch { return seed; } }
function normalizePath(path: string, fallback: string) { const value = (path || fallback).trim() || fallback; return value.startsWith("/") ? value : `/${value}`; }
function normalizeBaseUrl(url: string) { return (url || "").trim().replace(/\/+$/, ""); }
function apiUrl(connection: APIConnection, key: "corePath" | "scenePath") { return `${normalizeBaseUrl(connection.baseUrl)}${normalizePath(connection[key], key === "scenePath" ? "/api/display-scene" : "/api/agv-report/core")}`; }

function normalizeRdsCoreResponse(payload: any, plant: string, connection?: APIConnection): NormalizedRds {
  const core = payload?.data;
  const reports = Array.isArray(core?.report) ? core.report : [];
  if (!core || reports.length === 0) throw new Error("No data.report array found in RDS core JSON.");
  const positions = reports.map((item: any) => item.rbk_report).filter(Boolean).filter((rbk: any) => Number.isFinite(Number(rbk.x)) && Number.isFinite(Number(rbk.y)));
  const xs = positions.map((rbk: any) => Number(rbk.x));
  const ys = positions.map((rbk: any) => Number(rbk.y));
  const minX = Math.min(...xs), maxX = Math.max(...xs), minY = Math.min(...ys), maxY = Math.max(...ys);
  const scale = (value: unknown, min: number, max: number) => max === min ? 50 : 10 + ((Number(value) - min) / (max - min)) * 80;
  const importedAt = new Date().toISOString();
  const source = `${plant} RDS Core`;
  const rdsHost = connection?.baseUrl ? new URL(connection.baseUrl).hostname : "Local Go RDS proxy";
  const amrs: AMR[] = reports.map((item: any) => {
    const rbk = item.rbk_report || {};
    const basic = item.basic_info || {};
    const name = item.uuid || item.vehicle_id || item.current_order?.vehicle || "Unknown AMR";
    const warnings = [...(item.warnings || []), ...(rbk.warnings || []), ...(rbk.alarms?.warnings || [])];
    const errors = [...(item.errors || []), ...(rbk.errors || []), ...(rbk.alarms?.errors || [])];
    const networkDelay = Number(item.network_delay);
    const disconnected = Number(item.connection_status) === 0 || item.undispatchable_reason?.disconnect === true;
    const currentStation = rbk.current_station || item.current_order?.blocks?.[0]?.location || "No station reported";
    const quality = qualityFromConnection(disconnected, Number.isFinite(networkDelay) ? networkDelay : undefined, rbk.emergency === true || item.is_error === true || errors.length > 0);
    const reason = disconnected ? "RDS reports robot disconnected" : quality === "Poor" ? `High RDS network delay (${networkDelay} ms)` : quality === "Weak" ? `Elevated RDS network delay (${networkDelay} ms)` : rbk.emergency ? "Emergency stop active" : errors.length ? "RDS error present" : warnings.length ? warnings[0].desc || warnings[0].describe || "RDS warning present" : "RDS reports active connection";
    return {
      id: `rds-${slug(plant)}-${slug(name)}`, name, plant, ip: basic.ip || "unknown", status: disconnected ? "Disconnected" : "Online",
      reconnects: 0, disconnects: disconnected ? 1 : 0, offline: disconnected ? "Disconnected now" : "0m", worstDrop: currentStation,
      rssi: rssiEstimate(quality), ap: "RDS Core connectivity", ssid: "RSSI source not connected", channel: "unknown", band: "unknown", imported: true, source,
      battery: Number.isFinite(Number(rbk.battery_level)) ? `${Math.round(Number(rbk.battery_level) * 100)}%` : "unknown",
      rdsX: rbk.x, rdsY: rbk.y, mapMd5: rbk.current_map_md5 || core.scene_md5 || "unknown", modelMd5: core.model_md5 || "unknown",
      networkDelay: Number.isFinite(networkDelay) ? networkDelay : undefined, connectionStatus: Number(item.connection_status), currentStation,
      issue: reason, connectivityReason: reason
    };
  });
  const points: WifiPoint[] = reports.map((item: any) => {
    const rbk = item.rbk_report || {};
    const name = item.uuid || item.vehicle_id || item.current_order?.vehicle || "Unknown AMR";
    const networkDelay = Number(item.network_delay);
    const disconnected = Number(item.connection_status) === 0 || item.undispatchable_reason?.disconnect === true;
    const quality = qualityFromConnection(disconnected, Number.isFinite(networkDelay) ? networkDelay : undefined, rbk.emergency === true || item.is_error === true);
    return {
      plant, amr: name, x: Math.max(5, Math.min(95, Number.isFinite(Number(rbk.x)) ? scale(rbk.x, minX, maxX) : 50)), y: Math.max(5, Math.min(95, Number.isFinite(Number(rbk.y)) ? scale(rbk.y, minY, maxY) : 50)),
      rssi: rssiEstimate(quality), quality,
      ap: "RDS Core connectivity", ssid: "RSSI source not connected", channel: "unknown", band: "unknown", reconnect: false, disconnect: disconnected, offline: disconnected, roaming: false, time: core.create_on || importedAt, imported: true, source, rdsX: Number(rbk.x), rdsY: Number(rbk.y)
    };
  });
  const logs: LogEntry[] = [];
  const pushLog = (entry: any, severity: Severity, topic: string, fallback: string) => logs.push({ time: core.create_on || importedAt, plant, amr: entry.desc?.match(/\[(.*?)\]/)?.[1] || entry.desc?.match(/(AMR-[0-9]+)/)?.[1] || "RDS Core", server: rdsHost, host: `${plant} RDS`, vm: "", source: "RDS Core", category: "RDS", severity, topic, message: entry.desc || entry.describe || fallback, imported: true });
  [...(core.warnings || []), ...(core.alarms?.warnings || [])].forEach((warning: any, index: number) => pushLog(warning, "High", "RDS Core issue", `RDS warning ${warning.code || index}`));
  [...(core.errors || []), ...(core.alarms?.errors || [])].forEach((error: any, index: number) => pushLog(error, "High", "RDS Core issue", `RDS error ${error.code || index}`));
  reports.forEach((item: any) => {
    const rbk = item.rbk_report || {};
    const name = item.uuid || item.vehicle_id || item.current_order?.vehicle || "Unknown AMR";
    const disconnected = Number(item.connection_status) === 0 || item.undispatchable_reason?.disconnect === true;
    if (disconnected) logs.push({ time: core.create_on || importedAt, plant, amr: name, server: rdsHost, host: `${plant} RDS`, vm: "", source: "RDS Core", category: "AMR", severity: "High", topic: "Robot offline / disconnect", message: `${name} is disconnected in RDS core feed. IP ${item.basic_info?.ip || "unknown"}.`, imported: true });
    [...(item.warnings || []), ...(rbk.warnings || []), ...(rbk.alarms?.warnings || [])].forEach((warning: any) => logs.push({ time: core.create_on || importedAt, plant, amr: name, server: rdsHost, host: `${plant} RDS`, vm: "", source: "AMR Robot", category: "AMR", severity: item.is_error ? "High" : "Medium", topic: "RDS Core issue", message: warning.desc || warning.describe || `RDS warning ${warning.code || "unknown"}`, imported: true }));
  });
  return { amrs, points, logs, summary: { plant, source, createdOn: core.create_on || "unknown", modelMd5: core.model_md5 || "unknown", sceneMd5: core.scene_md5 || "unknown", robots: amrs.length, disconnected: amrs.filter((amr) => amr.status === "Disconnected").length } };
}

function propertyValue(properties: any[] | undefined, key: string) {
  return (properties || []).find((item) => item?.key === key)?.stringValue || "";
}
function mapPointFrom(pos: any, name = ""): MapPoint | null {
  const x = Number(pos?.x), y = Number(pos?.y);
  return Number.isFinite(x) && Number.isFinite(y) ? { name, x, y } : null;
}
function normalizeSceneResponse(payload: any, plant: string): SceneMap {
  const scene = payload?.data?.scene;
  const area = scene?.areas?.[0];
  const logical = area?.logicalMap;
  if (!logical) throw new Error("No RDS scene logical map found.");
  const points: MapPoint[] = (logical.advancedPoints || []).map((point: any) => {
    const mapped = mapPointFrom(point.pos, point.instanceName);
    return mapped ? { ...mapped, kind: point.className || "Point" } : null;
  }).filter(Boolean);
  const paths: MapPath[] = (logical.advancedCurves || []).map((curve: any) => {
    const start = mapPointFrom(curve.startPos?.pos, curve.startPos?.instanceName);
    const end = mapPointFrom(curve.endPos?.pos, curve.endPos?.instanceName);
    if (!start || !end) return null;
    const control1 = mapPointFrom(curve.controlPos1, "c1") || undefined;
    const control2 = mapPointFrom(curve.controlPos2, "c2") || undefined;
    return { name: curve.instanceName || `${start.name}-${end.name}`, className: curve.className || "Path", start, end, control1, control2 };
  }).filter(Boolean);
  const bins: MapPolygon[] = (logical.binLocationsList || []).flatMap((group: any) => (group.binLocationList || []).map((bin: any) => {
    try {
      const raw = propertyValue(bin.property, "points");
      const parsed = raw ? JSON.parse(raw) : [];
      const binPoints = parsed.map((item: any, index: number) => mapPointFrom(item, `${bin.pointName || bin.instanceName}-${index}`)).filter(Boolean);
      return binPoints.length ? { name: bin.pointName || bin.instanceName || "Bin", points: binPoints } : null;
    } catch { return null; }
  })).filter(Boolean);
  const all = [...points, ...paths.flatMap((path) => [path.start, path.end, path.control1, path.control2].filter(Boolean) as MapPoint[]), ...bins.flatMap((bin) => bin.points)];
  if (!all.length) throw new Error("RDS scene did not contain mappable points.");
  const xs = all.map((point) => point.x), ys = all.map((point) => point.y);
  return { plant, area: area?.name || "RDS Area", md5: payload?.data?.md5 || scene?.md5 || "unknown", bounds: { minX: Math.min(...xs), minY: Math.min(...ys), maxX: Math.max(...xs), maxY: Math.max(...ys) }, paths, points, bins };
}
function SceneMapView({ scene, points, amrs, signalFilter, showMapLabels, focusMode, onSelectAmr }: { scene?: SceneMap; points: WifiPoint[]; amrs: AMR[]; signalFilter: string; showMapLabels: boolean; focusMode: boolean; onSelectAmr?: (name: string) => void }) {
  const [hoveredMapPoint, setHoveredMapPoint] = useState<{ point: WifiPoint; left: number; top: number } | null>(null);
  if (!scene) return <div className="map-shell map-empty"><strong>No RDS map loaded</strong><span>Pull a plant map to show the real RDS layout here.</span></div>;
  const y = (value: number) => -value;
  const pathD = (path: MapPath) => path.control1 && path.control2
    ? `M ${path.start.x} ${y(path.start.y)} C ${path.control1.x} ${y(path.control1.y)} ${path.control2.x} ${y(path.control2.y)} ${path.end.x} ${y(path.end.y)}`
    : `M ${path.start.x} ${y(path.start.y)} L ${path.end.x} ${y(path.end.y)}`;
  const amrByName = new Map(amrs.map((amr) => [amr.name, amr]));
  const pointMarkers = points.filter((point) => Number.isFinite(point.rdsX) && Number.isFinite(point.rdsY)).map((point) => {
    const amr = amrByName.get(point.amr);
    if (!amr || point.source === "AMR SSH Auto-Discovery") return point;
    const quality = connectivityQuality(amr);
    return { ...point, quality, rssi: rssiEstimate(quality), ssid: ssidLabel(point.ssid || amr.ssid) } as WifiPoint;
  });
  const pointNames = new Set(pointMarkers.map((point) => point.amr));
  const amrMarkers = amrs.filter((amr) => !pointNames.has(amr.name) && Number.isFinite(Number(amr.rdsX)) && Number.isFinite(Number(amr.rdsY))).map((amr) => ({ plant: amr.plant, amr: amr.name, quality: connectivityQuality(amr), rssi: rssiEstimate(connectivityQuality(amr)), rdsX: Number(amr.rdsX), rdsY: Number(amr.rdsY) } as WifiPoint));
  const robotPoints = pointMarkers.concat(amrMarkers);
  const robotXs = robotPoints.map((point) => Number(point.rdsX)).filter(Number.isFinite);
  const robotYs = robotPoints.map((point) => Number(point.rdsY)).filter(Number.isFinite);
  const focusBounds = focusMode && robotXs.length && robotYs.length ? { minX: Math.min(...robotXs) - 10, maxX: Math.max(...robotXs) + 10, minY: Math.min(...robotYs) - 10, maxY: Math.max(...robotYs) + 10 } : null;
  const pad = focusBounds ? 0 : 2;
  const minX = focusBounds ? focusBounds.minX : scene.bounds.minX - pad, maxX = focusBounds ? focusBounds.maxX : scene.bounds.maxX + pad, minY = focusBounds ? focusBounds.minY : scene.bounds.minY - pad, maxY = focusBounds ? focusBounds.maxY : scene.bounds.maxY + pad;
  const width = Math.max(1, maxX - minX), height = Math.max(1, maxY - minY);
  const labelPoints = showMapLabels ? scene.points : scene.points.filter((point) => point.name.startsWith("AP") || point.name.startsWith("PP"));
  const pointMeta = (point: WifiPoint) => {
    const amr = amrByName.get(point.amr);
    const delay = amr?.networkDelay !== undefined ? `${amr.networkDelay} ms` : "not reported";
    const ssid = ssidLabel(point.ssid || amr?.ssid);
    const ip = amr?.ip || "unknown";
    const hasLiveRssi = point.source === "AMR SSH Auto-Discovery" || amr?.source === "AMR SSH Auto-Discovery";
    const signal = hasLiveRssi ? `${point.rssi} dBm` : `${point.rssi} dBm estimated from RDS connection`;
    const rssiSource = hasLiveRssi ? "Connected from AMR SSH Auto-Discovery" : "Not connected - use Connect RSSI Source";
    return { amr, delay, ssid, ip, signal, rssiSource, status: amr?.status || "unknown", location: amr?.worstDrop || "unknown", reason: amr ? connectivityReason(amr) : "RDS point marker" };
  };
  const showTooltip = (point: WifiPoint, event: React.MouseEvent<SVGGElement>) => {
    const box = event.currentTarget.closest(".map-shell")?.getBoundingClientRect();
    if (!box) return;
    setHoveredMapPoint({ point, left: Math.max(12, Math.min(event.clientX - box.left + 14, box.width - 292)), top: Math.max(12, Math.min(event.clientY - box.top + 14, box.height - 224)) });
  };
  const tooltipMeta = hoveredMapPoint ? pointMeta(hoveredMapPoint.point) : null;
  return <div className="map-shell scene-map"><svg className="scene-map-svg" viewBox={`${minX} ${-maxY} ${width} ${height}`} role="img" aria-label={`${scene.plant} RDS map`}>
    <rect x={minX} y={-maxY} width={width} height={height} className="map-bg" />
    <g>{scene.bins.map((bin) => <polygon key={bin.name} points={bin.points.map((point) => `${point.x},${y(point.y)}`).join(" ")} className="map-bin"><title>{bin.name}</title></polygon>)}</g>
    <g>{scene.paths.map((path) => <path key={path.name} d={pathD(path)} className={`map-path ${path.className.toLowerCase()}`}><title>{path.name}</title></path>)}</g>
    <g>{labelPoints.map((point) => <g key={point.name} className={`map-node ${point.name.startsWith("PP") ? "pickup" : point.name.startsWith("AP") ? "action" : "landmark"}`}><circle cx={point.x} cy={y(point.y)} r="0.28" />{showMapLabels && <text x={point.x + 0.34} y={y(point.y) - 0.26}>{point.name}</text>}</g>)}</g>
    <g>{robotPoints.map((point) => <g key={`${point.plant}-${point.amr}-zone`} className={`map-heat-zone ${point.quality.toLowerCase()}`}><circle cx={point.rdsX} cy={y(point.rdsY || 0)} r={focusMode ? "5.4" : "3.8"} /><circle cx={point.rdsX} cy={y(point.rdsY || 0)} r={focusMode ? "2.7" : "1.9"} /></g>)}</g>
    <g>{robotPoints.map((point) => <g key={`${point.plant}-${point.amr}`} className={`map-robot ${point.quality.toLowerCase()}`} role="button" tabIndex={0} onMouseEnter={(event) => showTooltip(point, event)} onMouseMove={(event) => showTooltip(point, event)} onMouseLeave={() => setHoveredMapPoint(null)} onClick={() => onSelectAmr?.(point.amr)} onKeyDown={(event) => { if (event.key === "Enter" || event.key === " ") onSelectAmr?.(point.amr); }}> <circle cx={point.rdsX} cy={y(point.rdsY || 0)} r={focusMode ? "1.25" : "0.86"} /><text x={(point.rdsX || 0) + (focusMode ? 1.45 : 0.98)} y={y(point.rdsY || 0) - (focusMode ? 0.9 : 0.6)}>{point.amr}</text></g>)}</g>
  </svg>{hoveredMapPoint && tooltipMeta && <div className="map-hover-card" style={{ left: hoveredMapPoint.left, top: hoveredMapPoint.top }}><header><strong>{hoveredMapPoint.point.amr}</strong>{badge(hoveredMapPoint.point.quality)}</header><div><span>AMR IP</span><strong>{tooltipMeta.ip}</strong></div><div><span>RSSI Source</span><strong>{tooltipMeta.rssiSource}</strong></div><div><span>Connected WiFi SSID</span><strong>{tooltipMeta.ssid}</strong></div><div><span>dBm strength</span><strong>{tooltipMeta.signal}</strong></div><div><span>Connection</span><strong>{tooltipMeta.status}</strong></div></div>}{robotPoints.length === 0 && <div className="map-overlay-note">No {signalFilter} markers for {scene.plant}. RDS currently provides AMR position/status; true Wi-Fi RSSI overlay needs Wi-Fi telemetry.</div>}<div className="map-caption">{scene.plant} - {scene.area} - {scene.paths.length} paths - {scene.points.length} points - {focusMode ? "AMR focus" : "full map"} - map MD5 {scene.md5}</div></div>;
}
function App() {
  const [state, setState] = useState<AppState>(loadState);
  const [connections, setConnections] = useState<APIConnection[]>([]);
  const [view, setView] = useState<View>("dashboard");
  const [plantFilter, setPlantFilter] = useState("All");
  const [search, setSearch] = useState("");
  const [selectedImportPlant, setSelectedImportPlant] = useState("Shelbyville");
  const [selectedAmr, setSelectedAmr] = useState<AMR | null>(null);
  const [signalFilter, setSignalFilter] = useState("All");
  const [focusMapOnAmrs, setFocusMapOnAmrs] = useState(true);
  const [showMapLabels, setShowMapLabels] = useState(false);
  const [logKeyword, setLogKeyword] = useState("");
  const [apiForm, setApiForm] = useState<APIConnection>({ plant: "", baseUrl: "", corePath: "/api/agv-report/core", scenePath: "/api/display-scene" });
  const [wifiForm, setWifiForm] = useState<Omit<WifiSource, "savedAt">>({ plant: "Shelbyville", name: "AMR Wi-Fi RSSI", method: "AMR SSH", host: "", username: "", secretRef: "CyberArk or SSH key reference", command: "iw dev wlan0 link" });
  const [wifiTest, setWifiTest] = useState<WifiTestResult | null>(null);
  const [wifiDiscover, setWifiDiscover] = useState<WifiDiscoverResponse | null>(null);
  const [selectedMapAmr, setSelectedMapAmr] = useState("");
  const [busy, setBusy] = useState("");

  useEffect(() => { LEGACY_STORAGE_KEYS.forEach((key) => localStorage.removeItem(key)); localStorage.setItem(STORAGE_KEY, JSON.stringify(state)); }, [state]);
  useEffect(() => { void loadConnections(); }, []);
  async function loadConnections() {
    const response = await fetch("/api/connections");
    if (response.ok) {
      const next = await response.json() as APIConnection[];
      setConnections(next);
      if (next[0] && !next.some((item) => item.plant === selectedImportPlant)) setSelectedImportPlant(next[0].plant);
    }
  }
  const plantOptions = useMemo(() => unique([...state.amrs.map((a) => a.plant), ...connections.map((c) => c.plant)]), [state.amrs, connections]);
  const filteredAmrs = useMemo(() => state.amrs.filter((amr) => (plantFilter === "All" || amr.plant === plantFilter) && JSON.stringify(amr).toLowerCase().includes(search.toLowerCase())), [state.amrs, plantFilter, search]);
  const filteredLogs = useMemo(() => state.logs.filter((log) => (plantFilter === "All" || log.plant === plantFilter) && (!logKeyword || JSON.stringify(log).toLowerCase().includes(logKeyword.toLowerCase()))), [state.logs, plantFilter, logKeyword]);
  const filteredPoints = useMemo(() => state.wifiPoints.filter((point) => (plantFilter === "All" || point.plant === plantFilter) && (signalFilter === "All" || point.quality === signalFilter)), [state.wifiPoints, plantFilter, signalFilter]);
  const heatmapPlant = selectedImportPlant;
  const heatmapPoints = useMemo(() => state.wifiPoints.filter((point) => point.plant === heatmapPlant && (signalFilter === "All" || point.quality === signalFilter)), [state.wifiPoints, signalFilter, heatmapPlant]);
  const heatmapAmrs = useMemo(() => state.amrs.filter((amr) => amr.plant === heatmapPlant && (signalFilter === "All" || connectivityQuality(amr) === signalFilter)), [state.amrs, heatmapPlant, signalFilter]);
  const activeSceneMap = state.sceneMaps[heatmapPlant];
  const visibleBadZones = useMemo(() => {
    const saved = state.badZones.filter((zone) => plantFilter === "All" || zone.plant === plantFilter);
    return saved.length ? saved : deriveBadZones(state.amrs.filter((amr) => plantFilter === "All" || amr.plant === plantFilter), state.wifiPoints);
  }, [state.badZones, state.amrs, state.wifiPoints, plantFilter]);
  const selectedHeatmapAmr = useMemo(() => state.amrs.find((amr) => amr.plant === heatmapPlant && amr.name === selectedMapAmr) || null, [state.amrs, heatmapPlant, selectedMapAmr]);
  const reportCards = useMemo(() => {
    const badZones = visibleBadZones;
    const issueAmrs = filteredAmrs.filter((amr) => connectivityQuality(amr) !== "Good");
    const highLogs = filteredLogs.filter((log) => log.severity === "High");
    const worstZone = badZones[0];
    return [
      { label: "Plant Health", value: `${filteredAmrs.filter((amr) => connectivityQuality(amr) === "Good").length}/${filteredAmrs.length}`, help: "AMRs with good current RDS connectivity" },
      { label: "Connectivity Risk", value: issueAmrs.length, help: issueAmrs.length ? `${issueAmrs.map((amr) => amr.name).slice(0, 3).join(", ")} need review` : "No weak, poor, or critical AMRs in scope" },
      { label: "Worst Area", value: worstZone?.zone || "None", help: worstZone ? `${worstZone.plant} score ${worstZone.score}; ${(worstZone.robots || []).join(", ")}` : "No bad zones detected from current RDS sample" },
      { label: "High Events", value: highLogs.length, help: highLogs[0] ? highLogs[0].message : "No high severity RDS events in scope" }
    ];
  }, [visibleBadZones, filteredAmrs, filteredLogs]);
  function exportRssiCapture() {
    const capturedAt = new Date().toISOString();
    const pointByAmr = new Map(state.wifiPoints.filter((point) => point.plant === heatmapPlant).map((point) => [point.amr, point]));
    const rows = heatmapAmrs.map((amr) => {
      const point = pointByAmr.get(amr.name);
      const source = amr.source || point?.source || "RDS estimate";
      const ssid = ssidLabel(amr.ssid || point?.ssid);
      const rssi = amr.rssi ?? point?.rssi ?? "";
      return [capturedAt, amr.plant, amr.name, amr.ip, ssid, rssi, connectivityQuality(amr), source, amr.networkDelay !== undefined ? `${amr.networkDelay} ms` : "", amr.rdsX ?? "", amr.rdsY ?? ""];
    });
    const csv = [["Captured At", "Plant", "AMR", "IP", "Connected WiFi SSID", "dBm Strength", "Quality", "Source", "Network Delay", "RDS X", "RDS Y"], ...rows].map((row) => row.map(csvCell).join(",")).join("\n");
    const url = URL.createObjectURL(new Blob([csv], { type: "text/csv;charset=utf-8" }));
    const link = document.createElement("a");
    link.href = url;
    link.download = `drishti-${slug(heatmapPlant)}-wifi-rssi-${capturedAt.replace(/[:.]/g, "-")}.csv`;
    link.click();
    URL.revokeObjectURL(url);
  }
  function mergeImport(normalized: NormalizedRds) {
    setState((current) => ({
      ...current,
      amrs: current.amrs.filter((item) => item.plant !== normalized.summary.plant).concat(normalized.amrs),
      wifiPoints: current.wifiPoints.filter((item) => item.plant !== normalized.summary.plant).concat(normalized.points),
      logs: current.logs.filter((item) => item.plant !== normalized.summary.plant).concat(normalized.logs),
      badZones: current.badZones.filter((item) => item.plant !== normalized.summary.plant).concat(deriveBadZones(normalized.amrs, normalized.points)),
      rdsImportNote: `Imported ${normalized.summary.robots} ${normalized.summary.plant} AMRs from RDS core (${normalized.summary.createdOn}). Disconnected: ${normalized.summary.disconnected}. Model MD5: ${normalized.summary.modelMd5}. Scene MD5: ${normalized.summary.sceneMd5}.`,
      discovery: current.discovery.map((item) => item.point.includes("AMR ") || item.point.includes("RDS ") ? { ...item, status: "Available", source: "Go RDS proxy", gap: `Updated from ${normalized.summary.plant} core feed` } : item)
    }));
    setView("dashboard");
  }
  async function pullLiveCore() {
    const connection = connections.find((item) => item.plant === selectedImportPlant);
    if (!connection) return;
    setBusy(`Pulling ${selectedImportPlant}`);
    try {
      const response = await fetch(`/api/plants/${slug(selectedImportPlant)}/rds/core?save=1`);
      if (!response.ok) throw new Error((await response.json()).error || response.statusText);
      const payload = await response.json();
      mergeImport(normalizeRdsCoreResponse(payload, selectedImportPlant, connection));
      void fetchSceneMap(selectedImportPlant).catch((error) => setState((current) => ({ ...current, rdsImportNote: `${current.rdsImportNote} Map pull failed: ${error instanceof Error ? error.message : String(error)}` })));
    } catch (error) {
      setState((current) => ({ ...current, rdsImportNote: `Live pull failed: ${error instanceof Error ? error.message : String(error)}` }));
    } finally { setBusy(""); }
  }
  async function fetchSceneMap(plant: string) {
    const response = await fetch(`/api/plants/${slug(plant)}/rds/scene?save=1`);
    if (!response.ok) throw new Error((await response.json()).error || response.statusText);
    const sceneMap = normalizeSceneResponse(await response.json(), plant);
    setState((current) => ({ ...current, sceneMaps: { ...current.sceneMaps, [plant]: sceneMap } }));
    return sceneMap;
  }
  async function pullSceneMap(plant = heatmapPlant) {
    if (!plant) return;
    setBusy(`Pulling ${plant} map`);
    try {
      await fetchSceneMap(plant);
      setView("heatmap");
    } catch (error) {
      setState((current) => ({ ...current, rdsImportNote: `Map pull failed: ${error instanceof Error ? error.message : String(error)}` }));
    } finally { setBusy(""); }
  }
  async function importFile(file: File) {
    const connection = connections.find((item) => item.plant === selectedImportPlant);
    try { mergeImport(normalizeRdsCoreResponse(JSON.parse(await file.text()), selectedImportPlant, connection)); }
    catch (error) { setState((current) => ({ ...current, rdsImportNote: `Import failed: ${error instanceof Error ? error.message : String(error)}` })); }
  }
  async function saveConnection(event: React.FormEvent) {
    event.preventDefault();
    const payload = { ...apiForm, baseUrl: normalizeBaseUrl(apiForm.baseUrl), corePath: normalizePath(apiForm.corePath, "/api/agv-report/core"), scenePath: normalizePath(apiForm.scenePath, "/api/display-scene") };
    const response = await fetch("/api/connections", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(payload) });
    if (response.ok) setConnections(await response.json() as APIConnection[]);
    setApiForm({ plant: "", baseUrl: "", corePath: "/api/agv-report/core", scenePath: "/api/display-scene" });
  }
  async function testWifiSource(source?: WifiSource) {
    const payload: WifiSource = source || { ...wifiForm, plant: wifiForm.plant.trim() || selectedImportPlant, name: wifiForm.name.trim() || `${wifiForm.plant || selectedImportPlant} RSSI source`, host: wifiForm.host.trim(), username: wifiForm.username.trim(), secretRef: wifiForm.secretRef.trim(), command: wifiForm.command.trim() || "iw dev wlan0 link", savedAt: new Date().toISOString() };
    setBusy("Testing RSSI");
    setWifiTest(null);
    setWifiDiscover(null);
    try {
      const response = await fetch("/api/wifi/test", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(payload) });
      const result = await response.json() as WifiTestResult;
      setWifiTest({ ...result, ok: response.ok && result.ok });
      setState((current) => ({ ...current, discovery: current.discovery.map((item) => item.point === "Wi-Fi RSSI" ? { ...item, status: response.ok && result.ok ? "Available" : "Partial", source: payload.method, command: payload.command, gap: result.message } : item) }));
    } catch (error) {
      setWifiTest({ ok: false, method: payload.method, host: payload.host, message: `RSSI test failed: ${error instanceof Error ? error.message : String(error)}` });
    } finally { setBusy(""); }
  }
  async function testAmrRssi() {
    const plant = wifiForm.plant.trim() || selectedImportPlant;
    const robots = state.amrs.filter((amr) => amr.plant === plant && amr.ip && amr.ip !== "unknown").map((amr) => ({ plant: amr.plant, name: amr.name, ip: amr.ip }));
    const source: WifiSource = { ...wifiForm, plant, host: "", username: wifiForm.username.trim(), secretRef: wifiForm.secretRef.trim(), command: wifiForm.command.trim() || "iw dev wlan0 link", name: wifiForm.name.trim() || `${plant} AMR RSSI`, savedAt: new Date().toISOString() };
    setWifiTest(null);
    setWifiDiscover(null);
    if (!source.username) {
      setWifiDiscover({ ok: false, message: "Username is required for AMR RSSI auto-discovery. Enter robowatch, save, then test again.", results: [] });
      return;
    }
    if (isPlaceholderCredential(source.secretRef)) {
      setWifiDiscover({ ok: false, message: "Credential Reference must be the private key path inside DRISHTI, for example /app/data/keys/robowatch_id.", results: [] });
      return;
    }
    setBusy("Testing AMR RSSI");
    try {
      const response = await fetch("/api/wifi/discover", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ source, robots }) });
      const result = await response.json() as WifiDiscoverResponse;
      const results = result.results || [];
      setWifiDiscover({ ...result, results });
      const realResults = new Map(results.filter((item) => item.ok && item.rssi !== undefined && item.quality && item.quality !== "Unknown").map((item) => [item.amr, item]));
      setState((current) => ({
        ...current,
        wifiPoints: current.wifiPoints.map((point) => {
          const match = realResults.get(point.amr);
          return match && point.plant === match.plant ? { ...point, rssi: match.rssi!, quality: match.quality as WifiPoint["quality"], ap: `Robot ${match.host}`, ssid: ssidLabel(match.ssid), source: "AMR SSH Auto-Discovery", time: new Date().toISOString() } : point;
        }),
        amrs: current.amrs.map((amr) => {
          const match = realResults.get(amr.name);
          return match && amr.plant === match.plant ? { ...amr, rssi: match.rssi!, ap: `Robot ${match.host}`, ssid: ssidLabel(match.ssid), source: "AMR SSH Auto-Discovery" } : amr;
        }),
        discovery: current.discovery.map((item) => item.point === "Wi-Fi RSSI" ? { ...item, status: result.ok ? "Available" : "Partial", source: "AMR SSH Auto-Discovery", command: "RDS basic_info.ip + SSH Wi-Fi command detection", gap: result.message } : item)
      }));
    } catch (error) {
      setWifiDiscover({ ok: false, message: `AMR RSSI auto-discovery failed: ${error instanceof Error ? error.message : String(error)}`, results: [] });
    } finally { setBusy(""); }
  }
  function saveWifiSource(event: React.FormEvent) {
    event.preventDefault();
    const plant = wifiForm.plant.trim() || selectedImportPlant;
    const payload: WifiSource = { ...wifiForm, plant, name: wifiForm.name.trim() || `${plant} RSSI source`, host: wifiForm.host.trim(), username: wifiForm.username.trim(), secretRef: wifiForm.secretRef.trim(), command: wifiForm.command.trim() || "iw dev wlan0 link", savedAt: new Date().toISOString() };
    setState((current) => ({
      ...current,
      wifiSources: (current.wifiSources || []).filter((item) => !(item.plant === payload.plant && item.name === payload.name)).concat(payload),
      discovery: current.discovery.map((item) => item.point === "Wi-Fi RSSI" ? { ...item, status: "Partial", source: payload.method, command: payload.command, gap: "Source saved locally; parser/collector still needs to be wired to collect live RSSI." } : item)
    }));
  }
  const metrics = [
    ["AMRs", filteredAmrs.length, "Filtered inventory"],
    ["Online", filteredAmrs.filter((amr) => amr.status === "Online").length, "Healthy now"],
    ["Disconnected / Offline", filteredAmrs.filter((amr) => amr.status === "Disconnected" || amr.status === "Offline").length, "Needs investigation"],
    ["TCP Reconnects", filteredAmrs.reduce((sum, amr) => sum + Number(amr.reconnects || 0), 0), "Current sample window"]
  ];
  return <div className="app-shell">
    <aside className="sidebar"><div className="brand-block"><div className="brand-mark">D</div><div><div className="brand-title">DRISHTI</div><div className="brand-subtitle">AMR Health</div></div></div><nav className="nav-list">{(["dashboard", "logs", "discovery", "heatmap", "reports", "admin"] as View[]).map((item) => <button key={item} className={`nav-item ${view === item ? "active" : ""}`} onClick={() => setView(item)}><span>{item[0].toUpperCase()}</span>{item[0].toUpperCase() + item.slice(1)}</button>)}</nav><div className="sidebar-status"><div className="status-dot"></div><div><strong>Go + React</strong><span>Local RDS proxy enabled</span></div></div></aside>
    <main className="main-content"><header className="topbar"><div><h1>{view === "dashboard" ? "AMR Health Dashboard" : view[0].toUpperCase() + view.slice(1)}</h1><p>Go backend, React UI, local config, and local-only RDS snapshots.</p></div><div className="topbar-controls"><label className="field compact"><span>Plant</span><select value={plantFilter} onChange={(e) => setPlantFilter(e.target.value)}><option>All</option>{plantOptions.map((plant) => <option key={plant}>{plant}</option>)}</select></label><label className="field search-field"><span>Search</span><input value={search} onChange={(e) => setSearch(e.target.value)} placeholder="AMR, IP, zone, AP, topic" /></label></div></header>
      {view === "dashboard" && <section className="view active-view"><div className="metric-grid">{metrics.map(([label, value, help]) => <article className="metric-card" key={label}><span className="metric-label">{label}</span><strong className="metric-value">{value}</strong><small className="metric-help">{help}</small></article>)}</div><div className="content-grid two-col"><section className="panel wide-panel"><div className="panel-header"><div><h2>AMR Fleet Health</h2><p>Investigate disconnects, offline time, and worst drop locations.</p></div><button className="primary-action" onClick={pullLiveCore} disabled={!connections.length || Boolean(busy)}>{busy || "Pull Selected RDS"}</button></div><div className="table-wrap"><table><thead><tr><th>AMR Name</th><th>Plant</th><th>IP</th><th>Status</th><th>Reconnect</th><th>Disconnect</th><th>Offline</th><th>Worst Drop</th><th>Investigate</th></tr></thead><tbody>{filteredAmrs.map((amr) => <tr key={amr.id}><td><strong>{amr.name}</strong></td><td>{amr.plant}</td><td>{amr.ip}</td><td>{badge(amr.status)}</td><td>{amr.reconnects}</td><td>{amr.disconnects}</td><td>{amr.offline}</td><td>{amr.worstDrop}</td><td><button className="row-action" onClick={() => setSelectedAmr(amr)}>Open</button></td></tr>)}</tbody></table></div></section><section className="panel"><div className="panel-header stacked"><h2>Bad Zone Areas</h2><p>Top repeated drop, reconnect, offline, and weak Wi-Fi areas.</p></div><div className="zone-list">{visibleBadZones.length ? visibleBadZones.map((zone) => <article className="zone-card" key={`${zone.plant}-${zone.zone}`}><header><strong>{zone.zone}</strong><span>{zone.score}</span></header><small>{zone.plant} - {zone.disconnects} disconnects, {zone.reconnects} reconnects, robots {(zone.robots || []).join(", ") || "none"}</small><small>{zone.reason || "Computed from current AMR connectivity"}</small><div className="score-bar"><span style={{ width: `${Math.min(zone.score, 100)}%` }}></span></div></article>) : <article className="zone-card"><strong>No Bad Zones</strong><small>Current RDS sample does not show disconnected, weak, or poor AMR connectivity in scope.</small></article>}</div></section></div>{selectedAmr && <section className="panel detail-panel"><div className="panel-header stacked"><h2>{selectedAmr.name} Detail</h2><p>{selectedAmr.plant} - {selectedAmr.ip} - worst drop: {selectedAmr.worstDrop}</p></div><div className="detail-grid">{[["Status", selectedAmr.status], ["Battery", selectedAmr.battery || "unknown"], ["RDS Position", selectedAmr.rdsX !== undefined ? `x ${selectedAmr.rdsX}, y ${selectedAmr.rdsY}` : "unknown"], ["Issue", selectedAmr.issue || "No issue"], ["Map / Model", `${selectedAmr.mapMd5 || "unknown"} / ${selectedAmr.modelMd5 || "unknown"}`]].map(([label, value]) => <article className="detail-card" key={label}><span>{label}</span><strong>{value}</strong></article>)}</div></section>}</section>}
      {view === "admin" && <section className="view active-view"><div className="admin-grid"><section className="panel"><div className="panel-header stacked"><h2>RDS Core Import</h2><p>Pull live RDS through the Go backend or import saved core JSON.</p></div><div className="import-actions"><label className="field compact"><span>Plant</span><select value={selectedImportPlant} onChange={(e) => setSelectedImportPlant(e.target.value)}>{plantOptions.map((plant) => <option key={plant}>{plant}</option>)}</select></label><button className="primary-action" onClick={pullLiveCore} disabled={Boolean(busy)}>{busy || "Pull Live Core"}</button><label className="file-action">Import Core JSON<input type="file" accept=".json,application/json" onChange={(e) => e.target.files?.[0] && void importFile(e.target.files[0])} /></label><button className="ghost-action" onClick={() => setState((current) => ({ ...current, amrs: current.amrs.filter((item) => !item.imported), wifiPoints: current.wifiPoints.filter((item) => !item.imported), logs: current.logs.filter((item) => !item.imported), rdsImportNote: "Imported RDS data cleared." }))}>Reset Imported Data</button></div><div className="threshold-note">{state.rdsImportNote}</div></section><section className="panel wide-panel"><div className="panel-header stacked"><h2>RDS API Connections</h2><p>Saved in local backend config, not committed to Git.</p></div><form className="form-grid" onSubmit={saveConnection}><label className="field"><span>Plant</span><input value={apiForm.plant} onChange={(e) => setApiForm({ ...apiForm, plant: e.target.value })} required placeholder="Shelbyville" /></label><label className="field"><span>Base URL</span><input value={apiForm.baseUrl} onChange={(e) => setApiForm({ ...apiForm, baseUrl: e.target.value })} required placeholder="http://rds-host:8080" /></label><label className="field"><span>Core Path</span><input value={apiForm.corePath} onChange={(e) => setApiForm({ ...apiForm, corePath: e.target.value })} required /></label><label className="field"><span>Scene Path</span><input value={apiForm.scenePath} onChange={(e) => setApiForm({ ...apiForm, scenePath: e.target.value })} required /></label><button className="primary-action" type="submit">Save Connection</button></form><div className="api-list">{connections.map((connection) => <article className="api-card" key={connection.plant}><header><strong>{connection.plant}</strong><button className="row-action" onClick={() => setApiForm(connection)}>Edit</button></header><div className="api-links"><span>Base <code>{connection.baseUrl}</code></span><span>Core <a href={apiUrl(connection, "corePath")} target="_blank">{apiUrl(connection, "corePath")}</a></span><span>Scene <a href={apiUrl(connection, "scenePath")} target="_blank">{apiUrl(connection, "scenePath")}</a></span><span>Local <code>/api/plants/{slug(connection.plant)}/rds/core</code></span></div></article>)}</div></section></div></section>}
      {view === "logs" && <section className="view active-view"><section className="panel filter-panel"><div className="panel-header"><div><h2>Log Investigation</h2><p>Filter AMR, RDS, Ubuntu, network, and VM evidence.</p></div><label className="field compact"><span>Keyword</span><input value={logKeyword} onChange={(e) => setLogKeyword(e.target.value)} placeholder="disconnect, map, battery" /></label></div></section><section className="panel"><div className="table-wrap"><table><thead><tr><th>Time</th><th>Plant</th><th>AMR</th><th>Topic</th><th>Source</th><th>Severity</th><th>Message</th></tr></thead><tbody>{filteredLogs.map((log, index) => <tr key={index}><td>{new Date(log.time).toLocaleString()}</td><td>{log.plant}</td><td>{log.amr}</td><td>{log.topic}</td><td>{log.source}</td><td>{badge(log.severity)}</td><td>{log.message}</td></tr>)}</tbody></table></div></section></section>}
      {view === "discovery" && <section className="view active-view"><div className="admin-grid"><section className="panel"><div className="panel-header stacked"><h2>Wi-Fi RSSI Source</h2><p>Add the source used to collect live signal strength. Store a vault/key reference here, not a password.</p></div><form className="form-grid" onSubmit={saveWifiSource}><label className="field"><span>Plant</span><input value={wifiForm.plant} onChange={(e) => setWifiForm({ ...wifiForm, plant: e.target.value })} placeholder="Shelbyville" /></label><label className="field"><span>Source Name</span><input value={wifiForm.name} onChange={(e) => setWifiForm({ ...wifiForm, name: e.target.value })} /></label><label className="field"><span>Method</span><select value={wifiForm.method} onChange={(e) => setWifiForm({ ...wifiForm, method: e.target.value as WifiSource["method"] })}><option>AMR SSH</option><option>Controller API</option><option>Manual Import</option></select></label><label className="field"><span>Host or API</span><input value={wifiForm.host} onChange={(e) => setWifiForm({ ...wifiForm, host: e.target.value })} placeholder="optional: one AMR IP for single-host test" /></label><label className="field"><span>Username</span><input value={wifiForm.username} onChange={(e) => setWifiForm({ ...wifiForm, username: e.target.value })} placeholder="read-only user" /></label><label className="field"><span>Credential Reference</span><input value={wifiForm.secretRef} onChange={(e) => setWifiForm({ ...wifiForm, secretRef: e.target.value })} placeholder="CyberArk account or SSH key path" /></label><label className="field wide-field"><span>RSSI Command or Path</span><input value={wifiForm.command} onChange={(e) => setWifiForm({ ...wifiForm, command: e.target.value })} placeholder="iw dev wlan0 link" /></label><button className="primary-action" type="submit">Save RSSI Source</button><button className="ghost-action" type="button" onClick={() => void testWifiSource()} disabled={Boolean(busy)}>{busy === "Testing RSSI" ? busy : "Test One Host RSSI"}</button><button className="ghost-action" type="button" onClick={() => void testAmrRssi()} disabled={Boolean(busy)}>{busy === "Testing AMR RSSI" ? busy : "Test RSSI on AMRs"}</button></form>{wifiTest && <div className={`wifi-test-result ${wifiTest.ok ? "ok" : "error"}`}><header>{badge(wifiTest.ok ? "Available" : "Partial")}<strong>{wifiTest.message}</strong></header>{wifiTest.rssi !== undefined && <span>RSSI {wifiTest.rssi} dBm - {wifiTest.quality} - SSID {ssidLabel(wifiTest.ssid)}</span>}{wifiTest.output && <pre>{wifiTest.output}</pre>}</div>}{wifiDiscover && <div className={`wifi-test-result ${wifiDiscover.ok ? "ok" : "error"}`}><header>{badge(wifiDiscover.ok ? "Available" : "Partial")}<strong>{wifiDiscover.message}</strong></header><div className="wifi-result-list">{(wifiDiscover.results || []).map((item) => <article key={`${item.plant}-${item.amr}-${item.host}`} className={item.ok ? "ok" : "error"}><strong>{item.amr}</strong><span>{item.host} - {item.message}</span>{item.rssi !== undefined && <small>{item.rssi} dBm - {item.quality} - SSID {ssidLabel(item.ssid)} - {item.command}</small>}{item.output && <pre>{item.output}</pre>}</article>)}</div></div>}<div className="api-list source-list">{(state.wifiSources || []).map((source) => <article className="api-card" key={`${source.plant}-${source.name}`}><header><strong>{source.plant}</strong><span>{badge(source.method)}</span></header><div className="api-links"><span>{source.name}</span><span>Host <code>{source.host || "not set"}</code></span><span>User <code>{source.username || "not set"}</code></span><span>Credential <code>{source.secretRef || "not set"}</code></span><span>Command <code>{source.command}</code></span><button className="row-action" onClick={() => void testWifiSource(source)} disabled={Boolean(busy)}>Test One Host RSSI</button></div></article>)}</div></section><section className="panel"><div className="panel-header stacked"><h2>Data Discovery</h2><p>Source reliability and remaining telemetry gaps.</p></div><div className="table-wrap"><table><thead><tr><th>Data Point</th><th>Status</th><th>Best Source</th><th>Command or Path</th><th>Gap</th></tr></thead><tbody>{state.discovery.map((item) => <tr key={item.point}><td><strong>{item.point}</strong></td><td>{badge(item.status)}</td><td>{item.source}</td><td><code>{item.command}</code></td><td>{item.gap}</td></tr>)}</tbody></table></div></section></div></section>}      {view === "heatmap" && <section className="view active-view"><section className="panel heatmap-panel"><div className="panel-header"><div><h2>AMR Plant Map</h2><p>RDS scene map with live AMR connectivity zones. Hover or click an AMR for connection detail; true RSSI appears after the Discovery source is connected.</p></div><div className="heatmap-actions"><label className="field compact"><span>Map Plant</span><select value={heatmapPlant} onChange={(e) => { setSelectedImportPlant(e.target.value); setSelectedMapAmr(""); }}>{plantOptions.filter((plant) => plant !== "All").map((plant) => <option key={plant}>{plant}</option>)}</select></label><label className="field compact"><span>Signal</span><select value={signalFilter} onChange={(e) => setSignalFilter(e.target.value)}><option>All</option><option>Good</option><option>Weak</option><option>Poor</option><option>Critical</option></select></label><button className="primary-action" onClick={() => void pullSceneMap(heatmapPlant)} disabled={Boolean(busy)}>{busy || "Pull RDS Map"}</button><button className="ghost-action" onClick={exportRssiCapture} disabled={!heatmapAmrs.length}>Export RSSI CSV</button><button className="ghost-action" onClick={() => setView("discovery")}>Connect RSSI Source</button><label className="check-field"><input type="checkbox" checked={focusMapOnAmrs} onChange={(e) => setFocusMapOnAmrs(e.target.checked)} /><span>Focus AMRs</span></label><label className="check-field"><input type="checkbox" checked={showMapLabels} onChange={(e) => setShowMapLabels(e.target.checked)} /><span>Map labels</span></label></div></div><SceneMapView scene={activeSceneMap} points={heatmapPoints} amrs={heatmapAmrs} signalFilter={signalFilter} showMapLabels={showMapLabels} focusMode={focusMapOnAmrs} onSelectAmr={(name) => setSelectedMapAmr(name)} /><div className="legend-row"><span><i className="legend good"></i>Good / connected</span><span><i className="legend weak"></i>Weak / delay 80+ ms</span><span><i className="legend poor"></i>Poor / delay 150+ ms</span><span><i className="legend critical"></i>Critical / disconnected</span></div>{selectedHeatmapAmr && <div className="map-detail-card"><header><strong>{selectedHeatmapAmr.name}</strong>{badge(connectivityQuality(selectedHeatmapAmr))}</header><div className="detail-grid">{[["Status", selectedHeatmapAmr.status], ["IP", selectedHeatmapAmr.ip], ["Connection", connectivityReason(selectedHeatmapAmr)], ["Network Delay", selectedHeatmapAmr.networkDelay !== undefined ? `${selectedHeatmapAmr.networkDelay} ms` : "not reported"], ["Location", selectedHeatmapAmr.worstDrop || "unknown"], ["Battery", selectedHeatmapAmr.battery || "unknown"], ["Connected WiFi SSID", ssidLabel(selectedHeatmapAmr.ssid)], ["RSSI Source", selectedHeatmapAmr.source === "AMR SSH Auto-Discovery" ? "Connected from AMR SSH Auto-Discovery" : "Not connected - use Connect RSSI Source"], ["dBm Strength", selectedHeatmapAmr.source === "AMR SSH Auto-Discovery" ? `${selectedHeatmapAmr.rssi} dBm` : `${selectedHeatmapAmr.rssi} dBm estimated`], ["RDS Position", selectedHeatmapAmr.rdsX !== undefined ? `x ${selectedHeatmapAmr.rdsX}, y ${selectedHeatmapAmr.rdsY}` : "unknown"]].map(([label, value]) => <article className="detail-card" key={label}><span>{label}</span><strong>{value}</strong></article>)}</div></div>}</section></section>}      {view === "reports" && <section className="view active-view"><div className="report-grid">{reportCards.map((card) => <article className="report-card" key={card.label}><span>{card.label}</span><strong>{card.value}</strong><small>{card.help}</small></article>)}</div><div className="content-grid two-col"><section className="panel"><div className="panel-header stacked"><h2>Bad Zone Summary</h2><p>Areas computed from repeated disconnects, offline AMRs, weak/poor connectivity, and reconnect evidence.</p></div><div className="zone-list">{visibleBadZones.length ? visibleBadZones.map((zone) => <article className="zone-card" key={`${zone.plant}-${zone.zone}-report`}><header><strong>{zone.zone}</strong><span>{zone.score}</span></header><small>{zone.plant} - robots {(zone.robots || []).join(", ") || "none"}</small><small>{zone.reason || "Computed from current AMR connectivity"}</small><div className="score-bar"><span style={{ width: `${Math.min(zone.score, 100)}%` }}></span></div></article>) : <article className="zone-card"><strong>No Bad Zones</strong><small>Current RDS sample does not show disconnected, weak, or poor AMR connectivity in scope.</small></article>}</div></section><section className="panel"><div className="panel-header stacked"><h2>Correlation Timeline</h2><p>Imported RDS and infrastructure evidence.</p></div><div className="timeline">{filteredLogs.slice().sort((a, b) => b.time.localeCompare(a.time)).map((log, index) => <article className="timeline-item" key={index}><time>{new Date(log.time).toLocaleString()}</time><div><strong>{log.topic}</strong><small>{log.plant} - {log.source} - {log.message}</small></div>{badge(log.severity)}</article>)}</div></section></div></section>}    </main>
  </div>;
}

createRoot(document.getElementById("root")!).render(<App />);

