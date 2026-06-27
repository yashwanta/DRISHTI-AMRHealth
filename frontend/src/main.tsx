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
};
type WifiPoint = { plant: string; amr: string; x: number; y: number; rssi: number; quality: "Good" | "Weak" | "Poor" | "Critical"; ap: string; ssid: string; channel: string; band: string; reconnect: boolean; disconnect: boolean; offline: boolean; roaming: boolean; time: string; imported?: boolean; source?: string; rdsX?: number; rdsY?: number };
type LogEntry = { time: string; plant: string; amr: string; server: string; host: string; vm: string; source: string; category: string; severity: Severity; topic: string; message: string; imported?: boolean };
type BadZone = { plant: string; zone: string; disconnects: number; reconnects: number; offline: number; weak: number; roaming: number; score: number };
type AppState = {
  amrs: AMR[]; wifiPoints: WifiPoint[]; logs: LogEntry[]; badZones: BadZone[]; sceneMaps: Record<string, SceneMap>;
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
    const disconnected = Number(item.connection_status) === 0 || item.undispatchable_reason?.disconnect === true;
    const currentStation = rbk.current_station || item.current_order?.blocks?.[0]?.location || "No station reported";
    return {
      id: `rds-${slug(plant)}-${slug(name)}`, name, plant, ip: basic.ip || "unknown", status: disconnected ? "Disconnected" : "Online",
      reconnects: 0, disconnects: disconnected ? 1 : 0, offline: disconnected ? "Disconnected now" : "0m", worstDrop: currentStation,
      rssi: -60, ap: "RDS Core position only", ssid: "unknown", channel: "unknown", band: "unknown", imported: true, source,
      battery: Number.isFinite(Number(rbk.battery_level)) ? `${Math.round(Number(rbk.battery_level) * 100)}%` : "unknown",
      rdsX: rbk.x, rdsY: rbk.y, mapMd5: rbk.current_map_md5 || core.scene_md5 || "unknown", modelMd5: core.model_md5 || "unknown",
      issue: disconnected ? "RDS reports robot disconnected" : rbk.emergency ? "Emergency stop active" : errors.length ? "RDS error present" : warnings.length ? warnings[0].desc || warnings[0].describe || "RDS warning present" : "No active RDS issue"
    };
  });
  const points: WifiPoint[] = reports.map((item: any) => {
    const rbk = item.rbk_report || {};
    const name = item.uuid || item.vehicle_id || item.current_order?.vehicle || "Unknown AMR";
    const disconnected = Number(item.connection_status) === 0 || item.undispatchable_reason?.disconnect === true;
    return {
      plant, amr: name, x: Math.max(5, Math.min(95, Number.isFinite(Number(rbk.x)) ? scale(rbk.x, minX, maxX) : 50)), y: Math.max(5, Math.min(95, Number.isFinite(Number(rbk.y)) ? scale(rbk.y, minY, maxY) : 50)),
      rssi: -60, quality: disconnected || rbk.emergency === true || item.is_error === true ? "Critical" : "Good",
      ap: "RDS Core position only", ssid: "unknown", channel: "unknown", band: "unknown", reconnect: false, disconnect: disconnected, offline: disconnected, roaming: false, time: core.create_on || importedAt, imported: true, source, rdsX: Number(rbk.x), rdsY: Number(rbk.y)
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
function SceneMapView({ scene, points, amrs }: { scene?: SceneMap; points: WifiPoint[]; amrs: AMR[] }) {
  if (!scene) return <div className="map-shell map-empty"><strong>No RDS map loaded</strong><span>Pull a plant map to show the real RDS layout here.</span></div>;
  const pad = 2;
  const minX = scene.bounds.minX - pad, maxX = scene.bounds.maxX + pad, minY = scene.bounds.minY - pad, maxY = scene.bounds.maxY + pad;
  const width = Math.max(1, maxX - minX), height = Math.max(1, maxY - minY);
  const y = (value: number) => -value;
  const pathD = (path: MapPath) => path.control1 && path.control2
    ? `M ${path.start.x} ${y(path.start.y)} C ${path.control1.x} ${y(path.control1.y)} ${path.control2.x} ${y(path.control2.y)} ${path.end.x} ${y(path.end.y)}`
    : `M ${path.start.x} ${y(path.start.y)} L ${path.end.x} ${y(path.end.y)}`;
  const pointMarkers = points.filter((point) => Number.isFinite(point.rdsX) && Number.isFinite(point.rdsY));
  const pointNames = new Set(pointMarkers.map((point) => point.amr));
  const amrMarkers = amrs.filter((amr) => !pointNames.has(amr.name) && Number.isFinite(Number(amr.rdsX)) && Number.isFinite(Number(amr.rdsY))).map((amr) => ({ plant: amr.plant, amr: amr.name, quality: amr.status === "Online" ? "Good" : "Critical", rssi: amr.rssi, rdsX: Number(amr.rdsX), rdsY: Number(amr.rdsY) } as WifiPoint));
  const robotPoints = pointMarkers.concat(amrMarkers);
  return <div className="map-shell scene-map"><svg className="scene-map-svg" viewBox={`${minX} ${-maxY} ${width} ${height}`} role="img" aria-label={`${scene.plant} RDS map`}>
    <rect x={minX} y={-maxY} width={width} height={height} className="map-bg" />
    <g>{scene.bins.map((bin) => <polygon key={bin.name} points={bin.points.map((point) => `${point.x},${y(point.y)}`).join(" ")} className="map-bin"><title>{bin.name}</title></polygon>)}</g>
    <g>{scene.paths.map((path) => <path key={path.name} d={pathD(path)} className={`map-path ${path.className.toLowerCase()}`}><title>{path.name}</title></path>)}</g>
    <g>{scene.points.map((point) => <g key={point.name} className={`map-node ${point.name.startsWith("PP") ? "pickup" : point.name.startsWith("AP") ? "action" : "landmark"}`}><circle cx={point.x} cy={y(point.y)} r="0.22" /><text x={point.x + 0.28} y={y(point.y) - 0.22}>{point.name}</text></g>)}</g>
    <g>{robotPoints.map((point) => <g key={`${point.plant}-${point.amr}`} className={`map-robot ${point.quality.toLowerCase()}`}><circle cx={point.rdsX} cy={y(point.rdsY || 0)} r="0.62" /><text x={(point.rdsX || 0) + 0.72} y={y(point.rdsY || 0) - 0.45}>{point.amr}</text><title>{`${point.amr} ${point.quality} ${point.rssi} dBm`}</title></g>)}</g>
  </svg><div className="map-caption">{scene.plant} - {scene.area} - {scene.paths.length} paths - {scene.points.length} points - map MD5 {scene.md5}</div></div>;
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
  const [logKeyword, setLogKeyword] = useState("");
  const [apiForm, setApiForm] = useState<APIConnection>({ plant: "", baseUrl: "", corePath: "/api/agv-report/core", scenePath: "/api/display-scene" });
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
  const activeSceneMap = state.sceneMaps[heatmapPlant];
  function mergeImport(normalized: NormalizedRds) {
    setState((current) => ({
      ...current,
      amrs: current.amrs.filter((item) => item.plant !== normalized.summary.plant).concat(normalized.amrs),
      wifiPoints: current.wifiPoints.filter((item) => item.plant !== normalized.summary.plant).concat(normalized.points),
      logs: current.logs.filter((item) => item.plant !== normalized.summary.plant).concat(normalized.logs),
      badZones: current.badZones.filter((item) => item.plant !== normalized.summary.plant),
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
  const metrics = [
    ["AMRs", filteredAmrs.length, "Filtered inventory"],
    ["Online", filteredAmrs.filter((amr) => amr.status === "Online").length, "Healthy now"],
    ["Disconnected / Offline", filteredAmrs.filter((amr) => amr.status === "Disconnected" || amr.status === "Offline").length, "Needs investigation"],
    ["TCP Reconnects", filteredAmrs.reduce((sum, amr) => sum + Number(amr.reconnects || 0), 0), "Current sample window"]
  ];
  return <div className="app-shell">
    <aside className="sidebar"><div className="brand-block"><div className="brand-mark">D</div><div><div className="brand-title">DRISHTI</div><div className="brand-subtitle">AMR Health</div></div></div><nav className="nav-list">{(["dashboard", "logs", "discovery", "heatmap", "reports", "admin"] as View[]).map((item) => <button key={item} className={`nav-item ${view === item ? "active" : ""}`} onClick={() => setView(item)}><span>{item[0].toUpperCase()}</span>{item[0].toUpperCase() + item.slice(1)}</button>)}</nav><div className="sidebar-status"><div className="status-dot"></div><div><strong>Go + React</strong><span>Local RDS proxy enabled</span></div></div></aside>
    <main className="main-content"><header className="topbar"><div><h1>{view === "dashboard" ? "AMR Health Dashboard" : view[0].toUpperCase() + view.slice(1)}</h1><p>Go backend, React UI, local config, and local-only RDS snapshots.</p></div><div className="topbar-controls"><label className="field compact"><span>Plant</span><select value={plantFilter} onChange={(e) => setPlantFilter(e.target.value)}><option>All</option>{plantOptions.map((plant) => <option key={plant}>{plant}</option>)}</select></label><label className="field search-field"><span>Search</span><input value={search} onChange={(e) => setSearch(e.target.value)} placeholder="AMR, IP, zone, AP, topic" /></label></div></header>
      {view === "dashboard" && <section className="view active-view"><div className="metric-grid">{metrics.map(([label, value, help]) => <article className="metric-card" key={label}><span className="metric-label">{label}</span><strong className="metric-value">{value}</strong><small className="metric-help">{help}</small></article>)}</div><div className="content-grid two-col"><section className="panel wide-panel"><div className="panel-header"><div><h2>AMR Fleet Health</h2><p>Investigate disconnects, offline time, and worst drop locations.</p></div><button className="primary-action" onClick={pullLiveCore} disabled={!connections.length || Boolean(busy)}>{busy || "Pull Selected RDS"}</button></div><div className="table-wrap"><table><thead><tr><th>AMR Name</th><th>Plant</th><th>IP</th><th>Status</th><th>Reconnect</th><th>Disconnect</th><th>Offline</th><th>Worst Drop</th><th>Investigate</th></tr></thead><tbody>{filteredAmrs.map((amr) => <tr key={amr.id}><td><strong>{amr.name}</strong></td><td>{amr.plant}</td><td>{amr.ip}</td><td>{badge(amr.status)}</td><td>{amr.reconnects}</td><td>{amr.disconnects}</td><td>{amr.offline}</td><td>{amr.worstDrop}</td><td><button className="row-action" onClick={() => setSelectedAmr(amr)}>Open</button></td></tr>)}</tbody></table></div></section><section className="panel"><div className="panel-header stacked"><h2>Bad Zone Areas</h2><p>Top repeated drop, reconnect, offline, and weak Wi-Fi areas.</p></div><div className="zone-list">{state.badZones.filter((zone) => plantFilter === "All" || zone.plant === plantFilter).map((zone) => <article className="zone-card" key={`${zone.plant}-${zone.zone}`}><header><strong>{zone.zone}</strong><span>{zone.score}</span></header><small>{zone.plant} - {zone.disconnects} disconnects, {zone.reconnects} reconnects</small><div className="score-bar"><span style={{ width: `${Math.min(zone.score, 100)}%` }}></span></div></article>)}</div></section></div>{selectedAmr && <section className="panel detail-panel"><div className="panel-header stacked"><h2>{selectedAmr.name} Detail</h2><p>{selectedAmr.plant} - {selectedAmr.ip} - worst drop: {selectedAmr.worstDrop}</p></div><div className="detail-grid">{[["Status", selectedAmr.status], ["Battery", selectedAmr.battery || "unknown"], ["RDS Position", selectedAmr.rdsX !== undefined ? `x ${selectedAmr.rdsX}, y ${selectedAmr.rdsY}` : "unknown"], ["Issue", selectedAmr.issue || "No issue"], ["Map / Model", `${selectedAmr.mapMd5 || "unknown"} / ${selectedAmr.modelMd5 || "unknown"}`]].map(([label, value]) => <article className="detail-card" key={label}><span>{label}</span><strong>{value}</strong></article>)}</div></section>}</section>}
      {view === "admin" && <section className="view active-view"><div className="admin-grid"><section className="panel"><div className="panel-header stacked"><h2>RDS Core Import</h2><p>Pull live RDS through the Go backend or import saved core JSON.</p></div><div className="import-actions"><label className="field compact"><span>Plant</span><select value={selectedImportPlant} onChange={(e) => setSelectedImportPlant(e.target.value)}>{plantOptions.map((plant) => <option key={plant}>{plant}</option>)}</select></label><button className="primary-action" onClick={pullLiveCore} disabled={Boolean(busy)}>{busy || "Pull Live Core"}</button><label className="file-action">Import Core JSON<input type="file" accept=".json,application/json" onChange={(e) => e.target.files?.[0] && void importFile(e.target.files[0])} /></label><button className="ghost-action" onClick={() => setState((current) => ({ ...current, amrs: current.amrs.filter((item) => !item.imported), wifiPoints: current.wifiPoints.filter((item) => !item.imported), logs: current.logs.filter((item) => !item.imported), rdsImportNote: "Imported RDS data cleared." }))}>Reset Imported Data</button></div><div className="threshold-note">{state.rdsImportNote}</div></section><section className="panel wide-panel"><div className="panel-header stacked"><h2>RDS API Connections</h2><p>Saved in local backend config, not committed to Git.</p></div><form className="form-grid" onSubmit={saveConnection}><label className="field"><span>Plant</span><input value={apiForm.plant} onChange={(e) => setApiForm({ ...apiForm, plant: e.target.value })} required placeholder="Shelbyville" /></label><label className="field"><span>Base URL</span><input value={apiForm.baseUrl} onChange={(e) => setApiForm({ ...apiForm, baseUrl: e.target.value })} required placeholder="http://rds-host:8080" /></label><label className="field"><span>Core Path</span><input value={apiForm.corePath} onChange={(e) => setApiForm({ ...apiForm, corePath: e.target.value })} required /></label><label className="field"><span>Scene Path</span><input value={apiForm.scenePath} onChange={(e) => setApiForm({ ...apiForm, scenePath: e.target.value })} required /></label><button className="primary-action" type="submit">Save Connection</button></form><div className="api-list">{connections.map((connection) => <article className="api-card" key={connection.plant}><header><strong>{connection.plant}</strong><button className="row-action" onClick={() => setApiForm(connection)}>Edit</button></header><div className="api-links"><span>Base <code>{connection.baseUrl}</code></span><span>Core <a href={apiUrl(connection, "corePath")} target="_blank">{apiUrl(connection, "corePath")}</a></span><span>Scene <a href={apiUrl(connection, "scenePath")} target="_blank">{apiUrl(connection, "scenePath")}</a></span><span>Local <code>/api/plants/{slug(connection.plant)}/rds/core</code></span></div></article>)}</div></section></div></section>}
      {view === "logs" && <section className="view active-view"><section className="panel filter-panel"><div className="panel-header"><div><h2>Log Investigation</h2><p>Filter AMR, RDS, Ubuntu, network, and VM evidence.</p></div><label className="field compact"><span>Keyword</span><input value={logKeyword} onChange={(e) => setLogKeyword(e.target.value)} placeholder="disconnect, map, battery" /></label></div></section><section className="panel"><div className="table-wrap"><table><thead><tr><th>Time</th><th>Plant</th><th>AMR</th><th>Topic</th><th>Source</th><th>Severity</th><th>Message</th></tr></thead><tbody>{filteredLogs.map((log, index) => <tr key={index}><td>{new Date(log.time).toLocaleString()}</td><td>{log.plant}</td><td>{log.amr}</td><td>{log.topic}</td><td>{log.source}</td><td>{badge(log.severity)}</td><td>{log.message}</td></tr>)}</tbody></table></div></section></section>}
      {view === "discovery" && <section className="view active-view"><section className="panel"><div className="panel-header stacked"><h2>Data Discovery</h2><p>Source reliability and remaining telemetry gaps.</p></div><div className="table-wrap"><table><thead><tr><th>Data Point</th><th>Status</th><th>Best Source</th><th>Command or Path</th><th>Gap</th></tr></thead><tbody>{state.discovery.map((item) => <tr key={item.point}><td><strong>{item.point}</strong></td><td>{badge(item.status)}</td><td>{item.source}</td><td><code>{item.command}</code></td><td>{item.gap}</td></tr>)}</tbody></table></div></section></section>}
      {view === "heatmap" && <section className="view active-view"><section className="panel heatmap-panel"><div className="panel-header"><div><h2>AMR Plant Map</h2><p>RDS scene map with AMR positions overlaid.</p></div><div className="heatmap-actions"><label className="field compact"><span>Map Plant</span><select value={heatmapPlant} onChange={(e) => setSelectedImportPlant(e.target.value)}>{plantOptions.filter((plant) => plant !== "All").map((plant) => <option key={plant}>{plant}</option>)}</select></label><label className="field compact"><span>Signal</span><select value={signalFilter} onChange={(e) => setSignalFilter(e.target.value)}><option>All</option><option>Good</option><option>Weak</option><option>Poor</option><option>Critical</option></select></label><button className="primary-action" onClick={() => void pullSceneMap(heatmapPlant)} disabled={Boolean(busy)}>{busy || "Pull RDS Map"}</button></div></div><SceneMapView scene={activeSceneMap} points={heatmapPoints} amrs={state.amrs.filter((amr) => amr.plant === heatmapPlant)} /><div className="legend-row"><span><i className="legend good"></i>Good</span><span><i className="legend weak"></i>Weak</span><span><i className="legend poor"></i>Poor</span><span><i className="legend critical"></i>Critical / offline</span></div></section></section>}
      {view === "reports" && <section className="view active-view"><div className="metric-grid">{[["Bad Zones", state.badZones.length, "Current plant scope"], ["Poor/Critical Points", state.wifiPoints.filter((p) => ["Poor", "Critical"].includes(p.quality)).length, "Wi-Fi evidence points"], ["High Severity Logs", state.logs.filter((log) => log.severity === "High").length, "Correlated events"]].map(([label, value, help]) => <article className="metric-card" key={label}><span className="metric-label">{label}</span><strong className="metric-value">{value}</strong><small className="metric-help">{help}</small></article>)}</div><section className="panel"><div className="panel-header stacked"><h2>Correlation Timeline</h2><p>Imported RDS and infrastructure evidence.</p></div><div className="timeline">{state.logs.slice().sort((a, b) => b.time.localeCompare(a.time)).map((log, index) => <article className="timeline-item" key={index}><time>{new Date(log.time).toLocaleString()}</time><div><strong>{log.topic}</strong><small>{log.plant} - {log.source} - {log.message}</small></div>{badge(log.severity)}</article>)}</div></section></section>}
    </main>
  </div>;
}

createRoot(document.getElementById("root")!).render(<App />);
