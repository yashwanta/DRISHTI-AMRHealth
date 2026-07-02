import React, { useEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import "./styles.css";

type View = "dashboard" | "logs" | "discovery" | "heatmap" | "scans" | "reports" | "admin";
type Status = "Online" | "Offline" | "Disconnected" | "Unknown";
type Severity = "High" | "Medium" | "Low";
type ReportRange = "1h" | "6h" | "24h" | "custom";
type APIConnection = { plant: string; baseUrl: string; corePath: string; scenePath: string };
type AMR = {
  id: string; name: string; plant: string; ip: string; status: Status; reconnects: number; disconnects: number; offline: string;
  worstDrop: string; rssi: number; ap: string; ssid: string; channel: string; band: string; imported?: boolean; source?: string;
  battery?: string; rdsX?: number | string; rdsY?: number | string; mapMd5?: string; modelMd5?: string; issue?: string;
  networkDelay?: number; connectionStatus?: number; connectivityReason?: string; currentStation?: string;
  assignedTask?: string; safetyStatus?: string; moving?: boolean; commandApiStatus?: string; taskState?: string; rdsConfidence?: number;
};
type WifiPoint = { plant: string; amr: string; x: number; y: number; rssi: number; quality: "Good" | "Weak" | "Poor" | "Critical"; ap: string; ssid: string; channel: string; band: string; reconnect: boolean; disconnect: boolean; offline: boolean; roaming: boolean; time: string; imported?: boolean; source?: string; rdsX?: number; rdsY?: number; rdsConfidence?: number };
type ConfidenceSample = { id: string; plant: string; amr: string; time: string; x: number; y: number; rssi: number; quality: WifiPoint["quality"]; confidence: number; source: string; ssid: string; ip: string; status: Status; mapMd5?: string };
type LogEntry = { time: string; plant: string; amr: string; zone?: string; server: string; host: string; vm: string; source: string; category: string; severity: Severity; topic: string; message: string; imported?: boolean };
type BadZone = { plant: string; zone: string; disconnects: number; reconnects: number; offline: number; weak: number; roaming: number; score: number; robots?: string[]; reason?: string };
type ZoneEvent = { timestamp: string; amr: string; rds_delay_ms: number; duration_ms: number; reconnected_at: string };
type ZoneAcknowledgement = { id: number; zone_id: string; plant_id: string; ack_by: string; ack_at: string; notes: string };
type ZoneEventsResponse = { zone_id: string; plant_id: string; events: ZoneEvent[]; acknowledgement?: ZoneAcknowledgement };
type WifiSource = { plant: string; name: string; method: "AMR SSH" | "Controller API" | "Manual Import"; host: string; username: string; secretRef: string; command: string; savedAt: string };
type WifiTestResult = { ok: boolean; method: string; host: string; message: string; output?: string; rssi?: number; ssid?: string; quality?: string };
type WifiDiscoverResult = { ok: boolean; plant: string; amr: string; host: string; command?: string; message: string; output?: string; rssi?: number; ssid?: string; quality?: WifiPoint["quality"] | "Unknown" };
type WifiDiscoverResponse = { ok: boolean; message: string; results?: WifiDiscoverResult[] | null };
type DiscoveryAMR = { plant: string; amr: string; rssi_dbm?: number | null; snr_db?: number | null; ap_name?: string; band?: string; channel?: string; last_seen?: string; source?: string };
type DiscoverySortKey = "amr" | "plant" | "rssi_dbm" | "snr_db" | "ap_name" | "band" | "channel" | "last_seen" | "source";
type AppState = {
  amrs: AMR[]; wifiPoints: WifiPoint[]; logs: LogEntry[]; badZones: BadZone[]; sceneMaps: Record<string, SceneMap>;
  wifiSources: WifiSource[]; confidenceSamples: ConfidenceSample[];
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
  confidenceSamples: [],
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
function hasRealRssi(value?: number | null) { return typeof value === "number" && Number.isFinite(value) && value !== 0; }
function rssiSignalTier(value?: number | null) {
  if (!hasRealRssi(value)) return "none";
  if ((value as number) <= -80) return "poor";
  if ((value as number) <= -68) return "weak";
  return "good";
}
function snrSignalTier(value?: number | null) {
  if (typeof value !== "number" || !Number.isFinite(value)) return "none";
  if (value >= 25) return "good";
  if (value >= 15) return "weak";
  return "poor";
}
function signalBarsForRssi(value?: number | null) {
  const tier = rssiSignalTier(value);
  return tier === "good" ? 3 : tier === "weak" ? 2 : tier === "poor" ? 1 : 0;
}
function signalLabel(value?: number | null) {
  const tier = rssiSignalTier(value);
  return tier === "good" ? "Good" : tier === "weak" ? "Weak" : tier === "poor" ? "Poor" : "No Signal";
}
function SignalBarsIcon({ bars, tier }: { bars: number; tier: string }) {
  return <svg className={`signal-bars signal-${tier}`} viewBox="0 0 24 18" role="img" aria-label={`${bars} signal bars`}><rect x="3" y="11" width="4" height="5" rx="1" opacity={bars >= 1 ? 1 : 0.25} /><rect x="10" y="7" width="4" height="9" rx="1" opacity={bars >= 2 ? 1 : 0.25} /><rect x="17" y="3" width="4" height="13" rx="1" opacity={bars >= 3 ? 1 : 0.25} /></svg>;
}
function rssiDisplay(value?: number | null) { return hasRealRssi(value) ? `${value} dBm` : "No Signal"; }
function discoveryValue(row: DiscoveryAMR, key: DiscoverySortKey) {
  const value = row[key];
  if ((key === "rssi_dbm" || key === "snr_db") && typeof value !== "number") return null;
  return value ?? "";
}
function compareDiscoveryValues(a: DiscoveryAMR, b: DiscoveryAMR, key: DiscoverySortKey) {
  const av = discoveryValue(a, key);
  const bv = discoveryValue(b, key);
  if (typeof av === "number" || typeof bv === "number") {
    if (typeof av !== "number") return 1;
    if (typeof bv !== "number") return -1;
    return av - bv;
  }
  return String(av).localeCompare(String(bv), undefined, { numeric: true, sensitivity: "base" });
}
function csvCell(value: unknown) { return `"${String(value ?? "").replace(/"/g, '""')}"`; }
function ssidLabel(value?: string | null) { const text = (value || "").trim(); return text && !/(not found|not reported|not captured|not connected|unknown|no such|command not found|error)/i.test(text) ? text : "not captured yet"; }
function normalizeLocation(value?: string | null) { return (value || "").trim().toLowerCase().replace(/[^a-z0-9]+/g, ""); }
function locationMatches(current: string | undefined, home: string) {
  const currentKey = normalizeLocation(current);
  const homeKey = normalizeLocation(home);
  return Boolean(currentKey && homeKey && (currentKey === homeKey || currentKey.includes(homeKey) || homeKey.includes(currentKey)));
}
function isPlaceholderCredential(value: string) { return !value.trim() || /cyberark|ssh key reference|public key|ssh-rsa|ssh-ed25519|begin public key/i.test(value); }
function amrIsMoving(amr: AMR) {
  return amr.moving === true || ["RUNNING", "EXECUTING", "MOVING", "GOING"].includes((amr.taskState || "").toUpperCase());
}
function formatOrderTask(order: any) {
  if (!order || Object.keys(order).length === 0) return "No active task";
  const state = order.state || "unknown";
  const route = Array.isArray(order.keyRoute) && order.keyRoute.length ? order.keyRoute.join(" -> ") : order.blocks?.[0]?.location || "route not reported";
  return `${state}: ${route}`;
}
function safetyStatusFromRds(rbk: any, item: any, errors: any[]) {
  if (rbk.emergency === true || rbk.soft_emc === true) return "Safety stop active";
  if (rbk.brake === true) return "Brake active";
  if (rbk.blocked === true) return "Blocked";
  if (item.is_error === true || errors.length > 0) return "RDS error present";
  return "Clear";
}
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
function reportAmrStatus(amr: AMR) {
  if (amr.status === "Offline" || amr.status === "Disconnected" || amr.connectionStatus === 0) return "Offline";
  return connectivityQuality(amr) === "Good" ? "Good" : "At Risk";
}
function reportHealthTone(healthyCount: number) { return healthyCount >= 22 ? "good" : healthyCount >= 18 ? "warning" : "danger"; }
function zoneDomId(zone: BadZone) { return `report-zone-${slug(zone.plant)}-${slug(zone.zone)}`; }
function zoneApiId(zone: BadZone) { return `${zone.plant}|${zone.zone}`; }
function formatMaybeTime(value?: string) {
  if (!value) return "not reported";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}
function datetimeLocalValue(value: Date) {
  const offsetMs = value.getTimezoneOffset() * 60000;
  return new Date(value.getTime() - offsetMs).toISOString().slice(0, 16);
}
function addMinutes(value: string, minutes: number) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return datetimeLocalValue(new Date(date.getTime() + minutes * 60000));
}
function reportSearchMatches(values: unknown[], query: string) {
  const needle = query.trim().toLowerCase();
  if (!needle) return true;
  return values.some((value) => String(value ?? "").toLowerCase().includes(needle));
}
function highlightText(value: unknown, query: string) {
  const text = String(value ?? "");
  const needle = query.trim();
  if (!needle) return text;
  const lower = text.toLowerCase();
  const match = needle.toLowerCase();
  const parts: React.ReactNode[] = [];
  let cursor = 0;
  let index = lower.indexOf(match);
  while (index >= 0) {
    if (index > cursor) parts.push(text.slice(cursor, index));
    parts.push(<mark className="search-hit" key={`${text}-${index}`}>{text.slice(index, index + match.length)}</mark>);
    cursor = index + match.length;
    index = lower.indexOf(match, cursor);
  }
  if (cursor < text.length) parts.push(text.slice(cursor));
  return <>{parts}</>;
}
function normalizeRdsConfidence(value: unknown) {
  const raw = Number(value);
  if (!Number.isFinite(raw)) return undefined;
  const percent = raw <= 1 ? raw * 100 : raw;
  return Math.max(0, Math.min(100, Math.round(percent)));
}
function confidenceLabel(score: number) { return score >= 75 ? "High" : score <= 50 ? "Low" : "Medium"; }
function confidenceForReading(quality: WifiPoint["quality"], options: { hasLiveRssi: boolean; hasIp: boolean; hasPosition: boolean; hasSsid: boolean; status?: Status; rdsConfidence?: number }) {
  if (typeof options.rdsConfidence === "number" && Number.isFinite(options.rdsConfidence)) {
    const score = Math.max(0, Math.min(100, Math.round(options.rdsConfidence)));
    const label = confidenceLabel(score);
    return { score, label, basis: "RDS rbk_report.confidence" };
  }
  let score = 35;
  if (options.status === "Online") score += 25;
  else if (options.status === "Disconnected" || options.status === "Offline") score += 8;
  if (options.hasPosition) score += 15;
  if (options.hasIp) score += 10;
  if (options.hasLiveRssi) score += 30;
  else score -= 10;
  if (options.hasSsid) score += 10;
  if (quality === "Weak") score -= 4;
  if (quality === "Poor") score -= 8;
  if (quality === "Critical") score -= 12;
  const bounded = Math.max(5, Math.min(100, score));
  const label = confidenceLabel(bounded);
  const basis = options.hasLiveRssi ? "real AMR RSSI" : "RDS estimate";
  return { score: bounded, label, basis };
}
const CONFIDENCE_RETENTION_MS = 5 * 24 * 60 * 60 * 1000;
const MAX_CONFIDENCE_SAMPLES_PER_PLANT = 3000;
function confidenceBand(score: number) { return score >= 75 ? "high" : score <= 50 ? "low" : "medium"; }
function pathLength(path: MapPath) { return Math.hypot(path.end.x - path.start.x, path.end.y - path.start.y); }
function isCleanMapPath(path: MapPath, scene?: SceneMap) {
  const signature = `${path.name} ${path.className}`;
  const dx = Math.abs(path.end.x - path.start.x);
  const dy = Math.abs(path.end.y - path.start.y);
  const length = pathLength(path);
  const longDiagonalRoute = length > 8 && dx > 2.5 && dy > 1.2;
  const duplicateReturnRoute = path.name.includes("-") && path.start.name > path.end.name && length <= 12;
  const bothLandmarks = path.start.name.startsWith("LM") && path.end.name.startsWith("LM");
  const centerX = (path.start.x + path.end.x) / 2;
  const centerY = (path.start.y + path.end.y) / 2;
  const sceneWidth = scene ? Math.max(1, scene.bounds.maxX - scene.bounds.minX) : 1;
  const sceneHeight = scene ? Math.max(1, scene.bounds.maxY - scene.bounds.minY) : 1;
  const sceneCenterX = scene ? (scene.bounds.minX + scene.bounds.maxX) / 2 : 0;
  const insideVerticalBand = scene ? centerY > scene.bounds.minY + sceneHeight * 0.22 && centerY < scene.bounds.maxY - sceneHeight * 0.22 : false;
  const awayFromRightEdge = scene ? centerX < scene.bounds.maxX - sceneWidth * 0.06 : true;
  const centralConnectorRoute = bothLandmarks && /straightpath/i.test(path.className) && length >= 3 && centerX > sceneCenterX + 2 && insideVerticalBand && awayFromRightEdge;
  return !/(bi[- ]?direction|bidirection|two[- ]?way|degeneratebezier|bezierpath|bidirectional)/i.test(signature)
    && !longDiagonalRoute
    && !duplicateReturnRoute
    && !centralConnectorRoute;
}
function pruneConfidenceSamples(samples: ConfidenceSample[], now = new Date()) {
  const cutoff = now.getTime() - CONFIDENCE_RETENTION_MS;
  const byPlant = new Map<string, ConfidenceSample[]>();
  samples.filter((sample) => new Date(sample.time).getTime() >= cutoff).forEach((sample) => {
    byPlant.set(sample.plant, (byPlant.get(sample.plant) || []).concat(sample));
  });
  return [...byPlant.values()].flatMap((plantSamples) => plantSamples.sort((a, b) => a.time.localeCompare(b.time)).slice(-MAX_CONFIDENCE_SAMPLES_PER_PLANT));
}
function mergeConfidenceSamples(existing: ConfidenceSample[], next: ConfidenceSample[]) {
  const deduped = new Map<string, ConfidenceSample>();
  pruneConfidenceSamples((existing || []).concat(next)).forEach((sample) => deduped.set(sample.id, sample));
  return [...deduped.values()].sort((a, b) => a.time.localeCompare(b.time));
}
function buildConfidenceSamples(plant: string, amrs: AMR[], points: WifiPoint[], mapMd5?: string, capturedAt = new Date().toISOString()) {
  const pointByAmr = new Map(points.filter((point) => point.plant === plant).map((point) => [point.amr, point]));
  return amrs.filter((amr) => amr.plant === plant && Number.isFinite(Number(amr.rdsX)) && Number.isFinite(Number(amr.rdsY))).map((amr) => {
    const point = pointByAmr.get(amr.name);
    const quality = point?.quality || connectivityQuality(amr);
    const source = amr.source || point?.source || "RDS estimate";
    const hasLiveRssi = source === "AMR SSH Auto-Discovery";
    const ssid = ssidLabel(amr.ssid || point?.ssid);
    const confidence = confidenceForReading(quality, { hasLiveRssi, hasIp: amr.ip !== "unknown", hasPosition: true, hasSsid: ssid !== "not captured yet", status: amr.status, rdsConfidence: amr.rdsConfidence ?? point?.rdsConfidence }).score;
    const x = Number(amr.rdsX), y = Number(amr.rdsY);
    return { id: `${capturedAt}-${plant}-${amr.name}-${x.toFixed(2)}-${y.toFixed(2)}`, plant, amr: amr.name, time: capturedAt, x, y, rssi: amr.rssi ?? point?.rssi ?? rssiEstimate(quality), quality, confidence, source, ssid, ip: amr.ip, status: amr.status, mapMd5 } as ConfidenceSample;
  });
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
function loadState(): AppState { try { const loaded = { ...seed, ...(JSON.parse(localStorage.getItem(STORAGE_KEY) || "null") || {}) } as AppState; return { ...loaded, confidenceSamples: pruneConfidenceSamples(loaded.confidenceSamples || []) }; } catch { return seed; } }
function normalizePath(path: string, fallback: string) { const value = (path || fallback).trim() || fallback; return value.startsWith("/") ? value : `/${value}`; }
function normalizeBaseUrl(url: string) { return (url || "").trim().replace(/\/+$/, ""); }
function apiUrl(connection: APIConnection, key: "corePath" | "scenePath") { return `${normalizeBaseUrl(connection.baseUrl)}${normalizePath(connection[key], key === "scenePath" ? "/api/display-scene" : "/api/agv-report/core")}`; }
function rdsProxyUrl(plant: string, endpoint: "core" | "scene", save = false, plantFilter = "All") {
  const params = new URLSearchParams();
  if (save) params.set("save", "1");
  if (plantFilter !== "All") params.set("plant", plantFilter);
  const query = params.toString();
  return `/api/plants/${slug(plant)}/rds/${endpoint}${query ? `?${query}` : ""}`;
}

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
    const currentOrder = item.current_order || {};
    const currentStation = rbk.current_station || currentOrder.blocks?.[0]?.location || "No station reported";
    const taskState = currentOrder.state || "No task";
    const speed = Math.hypot(Number(rbk.vx) || 0, Number(rbk.vy) || 0);
    const moving = speed > 0.01 || Math.abs(Number(rbk.w) || 0) > 0.01 || ["RUNNING", "EXECUTING", "MOVING", "GOING"].includes(String(taskState).toUpperCase());
    const safetyStatus = safetyStatusFromRds(rbk, item, errors);
    const rdsConfidence = normalizeRdsConfidence(rbk.confidence);
    const quality = qualityFromConnection(disconnected, Number.isFinite(networkDelay) ? networkDelay : undefined, rbk.emergency === true || item.is_error === true || errors.length > 0);
    const reason = disconnected ? "RDS reports robot disconnected" : quality === "Poor" ? `High RDS network delay (${networkDelay} ms)` : quality === "Weak" ? `Elevated RDS network delay (${networkDelay} ms)` : safetyStatus !== "Clear" ? safetyStatus : warnings.length ? warnings[0].desc || warnings[0].describe || "RDS warning present" : "RDS reports active connection";
    return {
      id: `rds-${slug(plant)}-${slug(name)}`, name, plant, ip: basic.ip || "unknown", status: disconnected ? "Disconnected" : "Online",
      reconnects: 0, disconnects: disconnected ? 1 : 0, offline: disconnected ? "Disconnected now" : "0m", worstDrop: currentStation,
      rssi: rssiEstimate(quality), ap: "RDS Core connectivity", ssid: "RSSI source not connected", channel: "unknown", band: "unknown", imported: true, source,
      battery: Number.isFinite(Number(rbk.battery_level)) ? `${Math.round(Number(rbk.battery_level) * 100)}%` : "unknown",
      rdsX: rbk.x, rdsY: rbk.y, mapMd5: rbk.current_map_md5 || core.scene_md5 || "unknown", modelMd5: core.model_md5 || "unknown",
      networkDelay: Number.isFinite(networkDelay) ? networkDelay : undefined, connectionStatus: Number(item.connection_status), currentStation,
      assignedTask: formatOrderTask(currentOrder), safetyStatus, moving, taskState: String(taskState), rdsConfidence, commandApiStatus: connection ? "RDS read API available; command API not probed" : "Read-only: command API not configured",
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
      ap: "RDS Core connectivity", ssid: "RSSI source not connected", channel: "unknown", band: "unknown", reconnect: false, disconnect: disconnected, offline: disconnected, roaming: false, time: core.create_on || importedAt, imported: true, source, rdsX: Number(rbk.x), rdsY: Number(rbk.y), rdsConfidence: normalizeRdsConfidence(rbk.confidence)
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
function SceneMapView({ scene, points, amrs, confidenceSamples, confidenceMode, signalFilter, showMapLabels, showMapPaths, focusMode, zoomLevel, emptyMessage, onSelectAmr }: { scene?: SceneMap; points: WifiPoint[]; amrs: AMR[]; confidenceSamples: ConfidenceSample[]; confidenceMode: "Current" | "5 days" | "Changes"; signalFilter: string; showMapLabels: boolean; showMapPaths: boolean; focusMode: boolean; zoomLevel: number; emptyMessage: string; onSelectAmr?: (name: string) => void }) {
  const [hoveredMapPoint, setHoveredMapPoint] = useState<{ point: WifiPoint; left: number; top: number } | null>(null);
  const [panOffset, setPanOffset] = useState({ x: 0, y: 0 });
  const [dragStart, setDragStart] = useState<{ clientX: number; clientY: number; panX: number; panY: number } | null>(null);
  useEffect(() => { setPanOffset({ x: 0, y: 0 }); setDragStart(null); }, [scene?.md5, focusMode]);
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
    return { ...point, quality, rssi: rssiEstimate(quality), ssid: ssidLabel(point.ssid || amr.ssid), rdsConfidence: amr.rdsConfidence ?? point.rdsConfidence } as WifiPoint;
  });
  const pointNames = new Set(pointMarkers.map((point) => point.amr));
  const amrMarkers = amrs.filter((amr) => !pointNames.has(amr.name) && Number.isFinite(Number(amr.rdsX)) && Number.isFinite(Number(amr.rdsY))).map((amr) => ({ plant: amr.plant, amr: amr.name, quality: connectivityQuality(amr), rssi: rssiEstimate(connectivityQuality(amr)), rdsX: Number(amr.rdsX), rdsY: Number(amr.rdsY), rdsConfidence: amr.rdsConfidence } as WifiPoint));
  const robotPoints = pointMarkers.concat(amrMarkers);
  const robotXs = robotPoints.map((point) => Number(point.rdsX)).filter(Number.isFinite);
  const robotYs = robotPoints.map((point) => Number(point.rdsY)).filter(Number.isFinite);
  const focusBounds = (() => {
    if (!focusMode || !robotXs.length || !robotYs.length) return null;
    const robotMinX = Math.min(...robotXs), robotMaxX = Math.max(...robotXs), robotMinY = Math.min(...robotYs), robotMaxY = Math.max(...robotYs);
    const sceneWidth = Math.max(1, scene.bounds.maxX - scene.bounds.minX), sceneHeight = Math.max(1, scene.bounds.maxY - scene.bounds.minY);
    const minFocusSize = robotPoints.length > 5 ? 72 : 52;
    const focusWidth = Math.min(Math.max(robotMaxX - robotMinX + 36, minFocusSize), sceneWidth + 8);
    const focusHeight = Math.min(Math.max(robotMaxY - robotMinY + 36, minFocusSize), sceneHeight + 8);
    const centerX = (robotMinX + robotMaxX) / 2, centerY = (robotMinY + robotMaxY) / 2;
    return { minX: centerX - focusWidth / 2, maxX: centerX + focusWidth / 2, minY: centerY - focusHeight / 2, maxY: centerY + focusHeight / 2 };
  })();
  const pad = focusBounds ? 0 : 2;
  const minX = focusBounds ? focusBounds.minX : scene.bounds.minX - pad, maxX = focusBounds ? focusBounds.maxX : scene.bounds.maxX + pad, minY = focusBounds ? focusBounds.minY : scene.bounds.minY - pad, maxY = focusBounds ? focusBounds.maxY : scene.bounds.maxY + pad;
  const width = Math.max(1, maxX - minX), height = Math.max(1, maxY - minY);
  const zoom = Math.max(1, Math.min(6, zoomLevel || 1));
  const zoomWidth = width / zoom, zoomHeight = height / zoom;
  const centerX = (minX + maxX) / 2, centerY = (minY + maxY) / 2;
  const maxPanX = Math.max(0, (width - zoomWidth) / 2);
  const maxPanY = Math.max(0, (height - zoomHeight) / 2);
  const clampPan = (value: number, limit: number) => Math.max(-limit, Math.min(limit, value));
  const panX = clampPan(panOffset.x, maxPanX), panY = clampPan(panOffset.y, maxPanY);
  const viewMinX = centerX - zoomWidth / 2 + panX, viewMaxY = centerY + zoomHeight / 2 + panY;
  const labelPoints = showMapLabels ? scene.points : scene.points.filter((point) => point.name.startsWith("AP") || point.name.startsWith("PP"));
  const pointMeta = (point: WifiPoint) => {
    const amr = amrByName.get(point.amr);
    const delay = amr?.networkDelay !== undefined ? `${amr.networkDelay} ms` : "not reported";
    const ssid = ssidLabel(point.ssid || amr?.ssid);
    const ip = amr?.ip || "unknown";
    const hasLiveRssi = point.source === "AMR SSH Auto-Discovery" || amr?.source === "AMR SSH Auto-Discovery";
    const signal = hasLiveRssi ? `${point.rssi} dBm` : `${point.rssi} dBm estimated from RDS connection`;
    const rssiSource = hasLiveRssi ? "Connected from AMR SSH Auto-Discovery" : "Not connected - use Connect RSSI Source";
    const confidence = confidenceForReading(point.quality, { hasLiveRssi, hasIp: ip !== "unknown", hasPosition: Number.isFinite(Number(point.rdsX)) && Number.isFinite(Number(point.rdsY)), hasSsid: ssid !== "not captured yet", status: amr?.status, rdsConfidence: amr?.rdsConfidence ?? point.rdsConfidence });
    return { amr, delay, ssid, ip, signal, rssiSource, confidence, status: amr?.status || "unknown", battery: amr?.battery || "unknown", currentStation: amr?.currentStation || "unknown", assignedTask: amr?.assignedTask || "No active task", safetyStatus: amr?.safetyStatus || "unknown", commandApiStatus: amr?.commandApiStatus || "Read-only: not configured", moving: amr ? (amrIsMoving(amr) ? "Yes" : "No") : "unknown", location: amr?.worstDrop || "unknown", reason: amr ? connectivityReason(amr) : "RDS point marker" };
  };
  const showTooltip = (point: WifiPoint, event: React.MouseEvent<SVGGElement>) => {
    const box = event.currentTarget.closest(".map-shell")?.getBoundingClientRect();
    if (!box) return;
    setHoveredMapPoint({ point, left: Math.max(12, Math.min(event.clientX - box.left + 14, box.width - 292)), top: Math.max(12, Math.min(event.clientY - box.top + 14, box.height - 390)) });
  };
  const tooltipMeta = hoveredMapPoint ? pointMeta(hoveredMapPoint.point) : null;
  const robotRenderPoints = robotPoints.map((point, index) => {
    const markerX = Number(point.rdsX);
    const rawY = Number(point.rdsY) || 0;
    const markerY = y(rawY);
    const cluster = robotPoints.filter((other) => Math.abs(Number(other.rdsX) - markerX) <= 2.4 && Math.abs(Number(other.rdsY) - rawY) <= 2.4);
    const clusterIndex = robotPoints.slice(0, index).filter((other) => Math.abs(Number(other.rdsX) - markerX) <= 2.4 && Math.abs(Number(other.rdsY) - rawY) <= 2.4).length;
    const visibleClusterSize = Math.min(cluster.length, 6);
    const lane = cluster.length > 1 ? clusterIndex % 6 : 0;
    const row = cluster.length > 1 ? Math.floor(clusterIndex / 6) : 0;
    const staggerY = cluster.length > 1 ? (lane - (visibleClusterSize - 1) / 2) * (focusMode ? 1.15 : 0.78) : -(focusMode ? 0.48 : 0.38);
    const labelX = markerX + (focusMode ? 1.05 : 0.86) + row * (focusMode ? 3.6 : 2.4);
    return { ...point, markerX, markerY, labelX, labelY: markerY + staggerY - 0.2, confidenceY: markerY + staggerY + (focusMode ? 0.8 : 0.55) };
  });
  const sortedConfidenceSamples = confidenceSamples.filter((sample) => Number.isFinite(sample.x) && Number.isFinite(sample.y)).sort((a, b) => a.time.localeCompare(b.time));
  const recentCutoff = Date.now() - 12 * 60 * 60 * 1000;
  const cellKey = (sample: ConfidenceSample) => `${Math.round(sample.x / 3)}:${Math.round(sample.y / 3)}`;
  const latestByCell = new Map<string, ConfidenceSample>();
  const previousByCell = new Map<string, ConfidenceSample>();
  sortedConfidenceSamples.forEach((sample) => {
    const key = cellKey(sample);
    if (new Date(sample.time).getTime() >= recentCutoff) latestByCell.set(key, sample);
    else previousByCell.set(key, sample);
  });
  const changedKeys = new Set([...latestByCell.entries()].filter(([key, sample]) => {
    const previous = previousByCell.get(key);
    return !previous || Math.abs(sample.confidence - previous.confidence) >= 10;
  }).map(([key]) => key));
  const visibleConfidenceSamples = confidenceMode === "Current"
    ? sortedConfidenceSamples.filter((sample) => new Date(sample.time).getTime() >= recentCutoff)
    : confidenceMode === "Changes"
      ? [...latestByCell.entries()].filter(([key]) => changedKeys.has(key)).map(([, sample]) => sample)
      : sortedConfidenceSamples;
  const confidenceLines = confidenceMode === "Changes" ? [] : [...new Map(visibleConfidenceSamples.map((sample) => [sample.amr, visibleConfidenceSamples.filter((item) => item.amr === sample.amr).sort((a, b) => a.time.localeCompare(b.time))])).values()].filter((samples) => samples.length > 1);
  const confidenceSegments = confidenceLines.flatMap((samples) => samples.slice(1).map((sample, index) => {
    const previous = samples[index];
    const score = Math.min(previous.confidence, sample.confidence);
    return { key: `${sample.plant}-${sample.amr}-${previous.id}-${sample.id}`, points: `${previous.x},${y(previous.y)} ${sample.x},${y(sample.y)}`, className: `confidence-${confidenceBand(score)}`, title: `${sample.amr} ${score}% confidence path - ${new Date(previous.time).toLocaleString()} to ${new Date(sample.time).toLocaleString()}` };
  }));
  const confidenceClassForPoint = (point: (typeof robotRenderPoints)[number]) => `confidence-${confidenceBand(pointMeta(point).confidence.score)}`;
  const confidenceSampleMarkers = visibleConfidenceSamples.map((sample, index) => ({
    key: `${sample.id}-${index}`,
    x: sample.x,
    y: y(sample.y),
    className: `confidence-${confidenceBand(sample.confidence)}`,
    label: `${sample.amr} ${sample.confidence}%`,
    title: `${sample.amr} ${sample.confidence}% confidence - ${new Date(sample.time).toLocaleString()} - ${sample.source}`
  }));
  const visibleMapPaths = showMapPaths ? scene.paths.filter((path) => isCleanMapPath(path, scene)) : [];
  const startMapDrag = (event: React.PointerEvent<SVGSVGElement>) => {
    if (zoom <= 1 || (event.target as Element).closest(".map-robot")) return;
    event.currentTarget.setPointerCapture(event.pointerId);
    setDragStart({ clientX: event.clientX, clientY: event.clientY, panX, panY });
    setHoveredMapPoint(null);
  };
  const moveMapDrag = (event: React.PointerEvent<SVGSVGElement>) => {
    if (!dragStart) return;
    const dx = ((event.clientX - dragStart.clientX) / Math.max(1, event.currentTarget.clientWidth)) * zoomWidth;
    const dy = ((event.clientY - dragStart.clientY) / Math.max(1, event.currentTarget.clientHeight)) * zoomHeight;
    setPanOffset({ x: clampPan(dragStart.panX - dx, maxPanX), y: clampPan(dragStart.panY + dy, maxPanY) });
  };
  const endMapDrag = () => setDragStart(null);
  return <div className="map-shell scene-map"><svg className={`scene-map-svg ${zoom > 1 ? "draggable" : ""} ${dragStart ? "dragging" : ""}`} viewBox={`${viewMinX} ${-viewMaxY} ${zoomWidth} ${zoomHeight}`} role="img" aria-label={`${scene.plant} RDS map`} onPointerDown={startMapDrag} onPointerMove={moveMapDrag} onPointerUp={endMapDrag} onPointerCancel={endMapDrag} onPointerLeave={endMapDrag}>
    <rect x={viewMinX} y={-viewMaxY} width={zoomWidth} height={zoomHeight} className="map-bg" />
    <g>{scene.bins.map((bin) => <polygon key={bin.name} points={bin.points.map((point) => `${point.x},${y(point.y)}`).join(" ")} className="map-bin"><title>{bin.name}</title></polygon>)}</g>
    <g>{visibleMapPaths.map((path) => <path key={path.name} d={pathD(path)} className={`map-path ${path.className.toLowerCase()}`}><title>{path.name}</title></path>)}</g>
    <g>{labelPoints.map((point) => <g key={point.name} className={`map-node ${point.name.startsWith("PP") ? "pickup" : point.name.startsWith("AP") ? "action" : "landmark"}`}><circle cx={point.x} cy={y(point.y)} r="0.28" />{showMapLabels && <text x={point.x + 0.34} y={y(point.y) - 0.26}>{point.name}</text>}</g>)}</g>
    <g className="map-confidence-lines">{confidenceSegments.map((segment) => <polyline key={segment.key} points={segment.points} className={segment.className}><title>{segment.title}</title></polyline>)}</g>
    <g className="map-confidence-samples">{confidenceSampleMarkers.map((sample) => <g key={sample.key} className={sample.className}><circle cx={sample.x} cy={sample.y} r={focusMode ? "0.46" : "0.34"} /><text x={sample.x + 0.46} y={sample.y - 0.34}>{sample.label}</text><title>{sample.title}</title></g>)}</g>
    <g>{robotRenderPoints.map((point) => <g key={`${point.plant}-${point.amr}-zone`} className={`map-heat-zone ${confidenceClassForPoint(point)}`}><circle cx={point.markerX} cy={point.markerY} r={focusMode ? "3.6" : "3.2"} /><circle cx={point.markerX} cy={point.markerY} r={focusMode ? "1.8" : "1.6"} /></g>)}</g>
    <g>{robotRenderPoints.map((point) => <g key={`${point.plant}-${point.amr}`} className={`map-robot ${confidenceClassForPoint(point)}`} role="button" tabIndex={0} onMouseEnter={(event) => showTooltip(point, event)} onMouseMove={(event) => showTooltip(point, event)} onMouseLeave={() => setHoveredMapPoint(null)} onClick={() => onSelectAmr?.(point.amr)} onKeyDown={(event) => { if (event.key === "Enter" || event.key === " ") onSelectAmr?.(point.amr); }}> <circle cx={point.markerX} cy={point.markerY} r={focusMode ? "0.74" : "0.68"} /><text x={point.labelX} y={point.labelY}>{point.amr}</text><text className="confidence-label" x={point.labelX} y={point.confidenceY}>{pointMeta(point).confidence.score}%</text></g>)}</g>
  </svg>{hoveredMapPoint && tooltipMeta && <div className="map-hover-card" style={{ left: hoveredMapPoint.left, top: hoveredMapPoint.top }}><header><strong>{hoveredMapPoint.point.amr}</strong>{badge(tooltipMeta.confidence.label)}</header><div><span>AMR IP</span><strong>{tooltipMeta.ip}</strong></div><div><span>RSSI Source</span><strong>{tooltipMeta.rssiSource}</strong></div><div><span>Confidence</span><strong>{tooltipMeta.confidence.score}% {tooltipMeta.confidence.label} - {tooltipMeta.confidence.basis}</strong></div><div><span>Connected WiFi SSID</span><strong>{tooltipMeta.ssid}</strong></div><div><span>dBm strength</span><strong>{tooltipMeta.signal}</strong></div><div><span>Connection</span><strong>{tooltipMeta.status}</strong></div><div><span>Moving</span><strong>{tooltipMeta.moving}</strong></div><div><span>Battery</span><strong>{tooltipMeta.battery}</strong></div><div><span>Station</span><strong>{tooltipMeta.currentStation}</strong></div><div><span>Task</span><strong>{tooltipMeta.assignedTask}</strong></div><div><span>Safety</span><strong>{tooltipMeta.safetyStatus}</strong></div><div><span>Command API</span><strong>{tooltipMeta.commandApiStatus}</strong></div></div>}<div className="map-caption"><span>{scene.plant} - {scene.area} - {visibleMapPaths.length}/{scene.paths.length} path lines - {scene.points.length} points - {focusMode ? "AMR focus" : "full map"} - map MD5 {scene.md5}</span>{robotPoints.length === 0 && <strong className="map-caption-warning">{emptyMessage}</strong>}</div></div>;
}
function App() {
  const [state, setState] = useState<AppState>(loadState);
  const [connections, setConnections] = useState<APIConnection[]>([]);
  const [view, setView] = useState<View>("dashboard");
  const [plantFilter, setPlantFilter] = useState("All");
  const [search, setSearch] = useState("");
  const [debouncedSearch, setDebouncedSearch] = useState("");
  const [searchSuggestions, setSearchSuggestions] = useState<string[]>([]);
  const [showSearchSuggestions, setShowSearchSuggestions] = useState(false);
  const [selectedImportPlant, setSelectedImportPlant] = useState("Shelbyville");
  const [selectedAmr, setSelectedAmr] = useState<AMR | null>(null);
  const [signalFilter, setSignalFilter] = useState("All");
  const [focusMapOnAmrs, setFocusMapOnAmrs] = useState(true);
  const [showMapLabels, setShowMapLabels] = useState(false);
  const [showMapPaths, setShowMapPaths] = useState(true);
  const [showMovingOnly, setShowMovingOnly] = useState(false);
  const [mapZoom, setMapZoom] = useState(1);
  const [scanTargetAmr, setScanTargetAmr] = useState("");
  const [scanHomeLocation, setScanHomeLocation] = useState("");
  const [stopScanAtHome, setStopScanAtHome] = useState(true);
  const [dailyScanEnabled, setDailyScanEnabled] = useState(false);
  const [dailyScanTime, setDailyScanTime] = useState("06:00");
  const [lastDailyScanDate, setLastDailyScanDate] = useState("");
  const [confidenceHistoryMode, setConfidenceHistoryMode] = useState<"Current" | "5 days" | "Changes">("5 days");
  const [selectedScanTime, setSelectedScanTime] = useState("");
  const [logKeyword, setLogKeyword] = useState("");
  const [logAmrFilter, setLogAmrFilter] = useState("");
  const [logStart, setLogStart] = useState("");
  const [logEnd, setLogEnd] = useState("");
  const [timelineSeverities, setTimelineSeverities] = useState<Record<Severity, boolean>>({ High: true, Medium: true, Low: true });
  const [timelineRange, setTimelineRange] = useState<ReportRange>("24h");
  const [customRangeStart, setCustomRangeStart] = useState(datetimeLocalValue(new Date(Date.now() - 6 * 60 * 60 * 1000)));
  const [customRangeEnd, setCustomRangeEnd] = useState(datetimeLocalValue(new Date()));
  const [reportEvents, setReportEvents] = useState<LogEntry[]>([]);
  const [reportEventsUpdatedAt, setReportEventsUpdatedAt] = useState("");
  const [reportEventsStatus, setReportEventsStatus] = useState("Paused");
  const [eventsLive, setEventsLive] = useState(false);
  const [selectedTimelineEvent, setSelectedTimelineEvent] = useState<LogEntry | null>(null);
  const [apiForm, setApiForm] = useState<APIConnection>({ plant: "", baseUrl: "", corePath: "/api/agv-report/core", scenePath: "/api/display-scene" });
  const [wifiForm, setWifiForm] = useState<Omit<WifiSource, "savedAt">>({ plant: "Shelbyville", name: "AMR Wi-Fi RSSI", method: "AMR SSH", host: "", username: "", secretRef: "CyberArk or SSH key reference", command: "iw dev wlan0 link" });
  const [wifiTest, setWifiTest] = useState<WifiTestResult | null>(null);
  const [wifiDiscover, setWifiDiscover] = useState<WifiDiscoverResponse | null>(null);
  const [discoveryRows, setDiscoveryRows] = useState<DiscoveryAMR[]>([]);
  const [discoveryStatus, setDiscoveryStatus] = useState("Discovery signal table has not loaded yet.");
  const [discoverySort, setDiscoverySort] = useState<{ key: DiscoverySortKey; direction: "asc" | "desc" }>({ key: "rssi_dbm", direction: "asc" });
  const [selectedMapAmr, setSelectedMapAmr] = useState("");
  const [showHighEventsModal, setShowHighEventsModal] = useState(false);
  const [activeReportDrilldown, setActiveReportDrilldown] = useState<"health" | "risk" | null>(null);
  const [highlightedZoneId, setHighlightedZoneId] = useState("");
  const [expandedZoneId, setExpandedZoneId] = useState("");
  const [zoneEvents, setZoneEvents] = useState<Record<string, ZoneEvent[]>>({});
  const [zoneAcknowledgements, setZoneAcknowledgements] = useState<Record<string, ZoneAcknowledgement>>({});
  const [zoneEventStatus, setZoneEventStatus] = useState<Record<string, string>>({});
  const [zoneAckNotes, setZoneAckNotes] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState("");
  const [scanRecording, setScanRecording] = useState(false);
  const [scanStatus, setScanStatus] = useState("Scan recorder idle.");
  const searchInputRef = useRef<HTMLInputElement | null>(null);
  const scanTickRef = useRef(false);
  const scanSeenAwayRef = useRef(false);

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
  useEffect(() => { if (view === "discovery") void loadDiscoverySignals(); }, [view, plantFilter]);
  async function loadDiscoverySignals() {
    try {
      setDiscoveryStatus("Loading AMR Wi-Fi signal telemetry...");
      const params = plantFilter !== "All" ? `?plant=${encodeURIComponent(plantFilter)}` : "";
      const response = await fetch(`/api/discovery${params}`);
      const payload = await response.json() as { items?: DiscoveryAMR[]; message?: string; error?: string };
      if (!response.ok) throw new Error(payload.error || response.statusText);
      setDiscoveryRows(payload.items || []);
      setDiscoveryStatus(payload.message || `Loaded ${(payload.items || []).length} Discovery rows.`);
    } catch (error) {
      setDiscoveryRows([]);
      setDiscoveryStatus(`Discovery signal load failed: ${error instanceof Error ? error.message : String(error)}`);
    }
  }
  function toggleDiscoverySort(key: DiscoverySortKey) {
    setDiscoverySort((current) => current.key === key ? { key, direction: current.direction === "asc" ? "desc" : "asc" } : { key, direction: "asc" });
  }
  useEffect(() => {
    if (view !== "reports" || plantFilter === "All") return;
    const connection = connections.find((item) => item.plant === plantFilter);
    if (!connection) return;
    let cancelled = false;
    const refreshReports = async () => {
      try {
        const response = await fetch(rdsProxyUrl(plantFilter, "core", true, plantFilter));
        if (!response.ok) throw new Error((await response.json()).error || response.statusText);
        if (!cancelled) mergeImport(normalizeRdsCoreResponse(await response.json(), plantFilter, connection), "reports");
      } catch (error) {
        if (!cancelled) setState((current) => ({ ...current, rdsImportNote: `Reports refresh failed for ${plantFilter}: ${error instanceof Error ? error.message : String(error)}` }));
      }
    };
    void refreshReports();
    return () => { cancelled = true; };
  }, [view, plantFilter, connections, timelineRange, customRangeStart, customRangeEnd]);
  const plantOptions = useMemo(() => unique([...state.amrs.map((a) => a.plant), ...connections.map((c) => c.plant)]), [state.amrs, connections]);
  const sortedDiscoveryRows = useMemo(() => [...discoveryRows].sort((a, b) => {
    const deadA = hasRealRssi(a.rssi_dbm) && (a.rssi_dbm as number) <= -80;
    const deadB = hasRealRssi(b.rssi_dbm) && (b.rssi_dbm as number) <= -80;
    if (deadA !== deadB) return deadA ? -1 : 1;
    const direction = discoverySort.direction === "asc" ? 1 : -1;
    return compareDiscoveryValues(a, b, discoverySort.key) * direction;
  }), [discoveryRows, discoverySort]);
  const discoveryColumns: { key: DiscoverySortKey; label: string }[] = [
    { key: "amr", label: "AMR" },
    { key: "plant", label: "Plant" },
    { key: "rssi_dbm", label: "RSSI" },
    { key: "snr_db", label: "SNR" },
    { key: "ap_name", label: "AP" },
    { key: "band", label: "Band" },
    { key: "channel", label: "Channel" },
    { key: "last_seen", label: "Last Seen" },
    { key: "source", label: "Source" }
  ];
  const filteredAmrs = useMemo(() => state.amrs.filter((amr) => (plantFilter === "All" || amr.plant === plantFilter) && JSON.stringify(amr).toLowerCase().includes(search.toLowerCase())), [state.amrs, plantFilter, search]);
  const filteredLogs = useMemo(() => state.logs.filter((log) => {
    if (plantFilter !== "All" && log.plant !== plantFilter) return false;
    if (logAmrFilter && log.amr !== logAmrFilter) return false;
    const logTime = new Date(log.time).getTime();
    if (logStart && Number.isFinite(logTime) && logTime < new Date(logStart).getTime()) return false;
    if (logEnd && Number.isFinite(logTime) && logTime > new Date(logEnd).getTime()) return false;
    return !logKeyword || JSON.stringify(log).toLowerCase().includes(logKeyword.toLowerCase());
  }), [state.logs, plantFilter, logKeyword, logAmrFilter, logStart, logEnd]);
  const filteredPoints = useMemo(() => state.wifiPoints.filter((point) => (plantFilter === "All" || point.plant === plantFilter) && (signalFilter === "All" || point.quality === signalFilter)), [state.wifiPoints, plantFilter, signalFilter]);
  const heatmapPlant = selectedImportPlant;
  const heatmapSignalAmrs = useMemo(() => state.amrs.filter((amr) => amr.plant === heatmapPlant && (signalFilter === "All" || connectivityQuality(amr) === signalFilter)), [state.amrs, heatmapPlant, signalFilter]);
  const heatmapAmrs = useMemo(() => heatmapSignalAmrs.filter((amr) => !showMovingOnly || amrIsMoving(amr)), [heatmapSignalAmrs, showMovingOnly]);
  const heatmapEmptyMessage = showMovingOnly && heatmapSignalAmrs.length > 0 ? `No moving AMRs for ${heatmapPlant} right now. Turn off Moving only to view online idle AMRs from RDS.` : `No ${signalFilter === "All" ? "AMR" : signalFilter} markers for ${heatmapPlant}. Pull RDS data/map, or connect Wi-Fi RSSI for live signal markers.`;
  const visibleHeatmapAmrNames = useMemo(() => new Set(heatmapAmrs.map((amr) => amr.name)), [heatmapAmrs]);
  const heatmapPoints = useMemo(() => state.wifiPoints.filter((point) => point.plant === heatmapPlant && (signalFilter === "All" || point.quality === signalFilter) && (!showMovingOnly || visibleHeatmapAmrNames.has(point.amr))), [state.wifiPoints, signalFilter, heatmapPlant, showMovingOnly, visibleHeatmapAmrNames]);
  const heatmapReadinessAmrs = useMemo(() => state.amrs.filter((amr) => amr.plant === heatmapPlant), [state.amrs, heatmapPlant]);
  const heatmapScanLocations = useMemo(() => unique(heatmapReadinessAmrs.flatMap((amr) => [amr.currentStation || "", amr.worstDrop || ""]).filter(Boolean)), [heatmapReadinessAmrs]);
  const activeSceneMap = state.sceneMaps[heatmapPlant];
  useEffect(() => {
    const plantAmrs = state.amrs.filter((amr) => amr.plant === heatmapPlant);
    if (!plantAmrs.length) return;
    if (!plantAmrs.some((amr) => amr.name === scanTargetAmr)) setScanTargetAmr(plantAmrs[0].name);
  }, [state.amrs, heatmapPlant, scanTargetAmr]);
  useEffect(() => {
    const selected = state.amrs.find((amr) => amr.plant === heatmapPlant && amr.name === scanTargetAmr);
    if (!selected || scanHomeLocation.trim()) return;
    setScanHomeLocation(selected.currentStation || selected.worstDrop || "");
  }, [state.amrs, heatmapPlant, scanTargetAmr, scanHomeLocation]);
  const heatmapConfidenceSamples = useMemo(() => (state.confidenceSamples || []).filter((sample) => sample.plant === heatmapPlant), [state.confidenceSamples, heatmapPlant]);
  const displayedHeatmapConfidenceSamples = useMemo(() => selectedScanTime ? heatmapConfidenceSamples.filter((sample) => sample.time === selectedScanTime) : heatmapConfidenceSamples, [heatmapConfidenceSamples, selectedScanTime]);
  useEffect(() => {
    if (!scanRecording) return;
    let cancelled = false;
    const runTick = async () => {
      if (cancelled || scanTickRef.current) return;
      scanTickRef.current = true;
      try {
        await recordScanSample(heatmapPlant);
      } finally {
        scanTickRef.current = false;
      }
    };
    void runTick();
    const intervalId = window.setInterval(() => void runTick(), 8000);
    return () => {
      cancelled = true;
      window.clearInterval(intervalId);
    };
  }, [scanRecording, heatmapPlant, connections, scanTargetAmr, scanHomeLocation, stopScanAtHome]);
  useEffect(() => {
    if (!dailyScanEnabled || scanRecording) return;
    const intervalId = window.setInterval(() => {
      const now = new Date();
      const today = now.toISOString().slice(0, 10);
      const hhmm = now.toTimeString().slice(0, 5);
      if (today !== lastDailyScanDate && hhmm >= dailyScanTime && connections.some((item) => item.plant === heatmapPlant)) {
        scanSeenAwayRef.current = false;
        setLastDailyScanDate(today);
        setScanRecording(true);
        setScanStatus(`Daily recorder started for ${scanTargetAmr || heatmapPlant} at ${hhmm}.`);
      }
    }, 30000);
    return () => window.clearInterval(intervalId);
  }, [dailyScanEnabled, scanRecording, lastDailyScanDate, dailyScanTime, connections, heatmapPlant, scanTargetAmr]);
  const confidenceStats = useMemo(() => {
    const samples = displayedHeatmapConfidenceSamples;
    const low = samples.filter((sample) => sample.confidence < 65).length;
    const latest = samples[samples.length - 1];
    return { count: samples.length, low, latest: latest ? new Date(latest.time).toLocaleString() : "not captured yet" };
  }, [displayedHeatmapConfidenceSamples]);
  const mappedPlants = useMemo(() => plantOptions.filter((plant) => connections.some((connection) => connection.plant === plant)), [plantOptions, connections]);
  const scanRuns = useMemo(() => {
    const groups = new Map<string, ConfidenceSample[]>();
    (state.confidenceSamples || []).forEach((sample) => {
      const key = `${sample.plant}|${sample.time}`;
      groups.set(key, (groups.get(key) || []).concat(sample));
    });
    return [...groups.entries()].map(([key, samples]) => {
      const [plant, time] = key.split("|");
      const low = samples.filter((sample) => sample.confidence <= 50 || sample.quality === "Critical" || sample.quality === "Poor").length;
      const high = samples.filter((sample) => sample.confidence >= 75).length;
      return { key, plant, time, samples: samples.length, amrs: unique(samples.map((sample) => sample.amr)), low, high, mapMd5: samples[0]?.mapMd5 || "unknown" };
    }).sort((a, b) => b.time.localeCompare(a.time));
  }, [state.confidenceSamples]);
  const filteredScanRuns = useMemo(() => {
    const query = search.trim().toLowerCase();
    return scanRuns.filter((run) => {
      if (plantFilter !== "All" && run.plant !== plantFilter) return false;
      if (!query) return true;
      const haystack = [run.plant, run.time, new Date(run.time).toLocaleString(), run.mapMd5, ...run.amrs].join(" ").toLowerCase();
      return haystack.includes(query);
    });
  }, [scanRuns, plantFilter, search]);
  const reportAmrs = useMemo(() => state.amrs.filter((amr) => plantFilter === "All" || amr.plant === plantFilter), [state.amrs, plantFilter]);
  const reportPlantLogs = useMemo(() => state.logs.filter((log) => plantFilter === "All" || log.plant === plantFilter), [state.logs, plantFilter]);
  const lastPingTimeByAmr = useMemo(() => {
    const latest = new Map<string, string>();
    const setLatest = (amr: string, time?: string) => {
      if (!amr || !time || amr === "RDS Core") return;
      const current = latest.get(amr);
      if (!current || time.localeCompare(current) > 0) latest.set(amr, time);
    };
    reportPlantLogs.forEach((log) => setLatest(log.amr, log.time));
    state.wifiPoints.filter((point) => plantFilter === "All" || point.plant === plantFilter).forEach((point) => setLatest(point.amr, point.time));
    return latest;
  }, [reportPlantLogs, state.wifiPoints, plantFilter]);
  const healthDrilldownAmrs = useMemo(() => [...reportAmrs].sort((a, b) => {
    const rank = (amr: AMR) => reportAmrStatus(amr) === "Good" ? 1 : 0;
    return rank(a) - rank(b) || a.name.localeCompare(b.name);
  }), [reportAmrs]);
  const atRiskReportAmrs = useMemo(() => healthDrilldownAmrs.filter((amr) => reportAmrStatus(amr) !== "Good"), [healthDrilldownAmrs]);
  const visibleBadZones = useMemo(() => {
    const saved = state.badZones.filter((zone) => plantFilter === "All" || zone.plant === plantFilter);
    return saved.length ? saved : deriveBadZones(state.amrs.filter((amr) => plantFilter === "All" || amr.plant === plantFilter), state.wifiPoints.filter((point) => plantFilter === "All" || point.plant === plantFilter));
  }, [state.badZones, state.amrs, state.wifiPoints, plantFilter]);
  const reportZones = useMemo(() => [...visibleBadZones].sort((a, b) => {
    const ackA = Boolean(zoneAcknowledgements[zoneApiId(a)]);
    const ackB = Boolean(zoneAcknowledgements[zoneApiId(b)]);
    if (ackA !== ackB) return ackA ? 1 : -1;
    return b.score - a.score || a.zone.localeCompare(b.zone);
  }), [visibleBadZones, zoneAcknowledgements]);
  const selectedSeverityValues = useMemo(() => (Object.keys(timelineSeverities) as Severity[]).filter((severity) => timelineSeverities[severity]), [timelineSeverities]);
  function reportEventsQuery() {
    const params = new URLSearchParams();
    if (plantFilter !== "All") params.set("plant", plantFilter);
    params.set("range", timelineRange);
    params.set("severity", selectedSeverityValues.map((severity) => severity.toLowerCase()).join(","));
    if (timelineRange === "custom") {
      params.set("start", customRangeStart);
      params.set("end", customRangeEnd);
    }
    return params.toString();
  }
  function eventKey(event: LogEntry) { return [event.time, event.plant, event.amr, event.topic, event.message].join("|"); }
  async function fetchReportEvents(prepend = false) {
    if (!selectedSeverityValues.length) {
      setReportEvents([]);
      setReportEventsStatus("No severities selected");
      setReportEventsUpdatedAt(new Date().toISOString());
      return;
    }
    setReportEventsStatus(prepend ? "Live refresh..." : "Loading events...");
    try {
      const response = await fetch(`/api/reports/events?${reportEventsQuery()}`);
      if (!response.ok) throw new Error((await response.json()).error || response.statusText);
      const payload = await response.json() as { events: LogEntry[]; updated_at: string };
      setReportEvents((current) => {
        if (!prepend) return payload.events || [];
        const seen = new Set(current.map(eventKey));
        const fresh = (payload.events || []).filter((event) => !seen.has(eventKey(event)));
        return fresh.concat(current).sort((a, b) => b.time.localeCompare(a.time)).slice(0, 300);
      });
      setReportEventsUpdatedAt(payload.updated_at || new Date().toISOString());
      setReportEventsStatus(prepend ? "Live" : "Loaded");
    } catch (error) {
      setReportEventsStatus(`Events refresh failed: ${error instanceof Error ? error.message : String(error)}`);
    }
  }
  useEffect(() => {
    if (view !== "reports") return;
    void fetchReportEvents(false);
  }, [view, plantFilter, timelineRange, customRangeStart, customRangeEnd, timelineSeverities.High, timelineSeverities.Medium, timelineSeverities.Low]);
  useEffect(() => {
    if (view !== "reports" || !eventsLive) return;
    const intervalId = window.setInterval(() => void fetchReportEvents(true), 30000);
    return () => window.clearInterval(intervalId);
  }, [view, eventsLive, plantFilter, timelineRange, customRangeStart, customRangeEnd, timelineSeverities.High, timelineSeverities.Medium, timelineSeverities.Low]);
  const fallbackTimelineEvents = useMemo(() => reportPlantLogs.filter((log) => selectedSeverityValues.includes(log.severity)).sort((a, b) => b.time.localeCompare(a.time)), [reportPlantLogs, selectedSeverityValues]);
  const reportTimelineEvents = reportEventsUpdatedAt || reportEventsStatus.startsWith("Events refresh failed") || !fallbackTimelineEvents.length ? reportEvents : fallbackTimelineEvents;
  const selectedHeatmapAmr = useMemo(() => state.amrs.find((amr) => amr.plant === heatmapPlant && amr.name === selectedMapAmr) || null, [state.amrs, heatmapPlant, selectedMapAmr]);
  const reportSearchQuery = view === "reports" ? debouncedSearch.trim() : "";
  const filteredReportZones = useMemo(() => reportZones.filter((zone) => reportSearchMatches([zone.zone, zone.plant, ...(zone.robots || [])], reportSearchQuery)), [reportZones, reportSearchQuery]);
  const filteredReportTimelineEvents = useMemo(() => reportTimelineEvents.filter((log) => reportSearchMatches([log.amr, log.message, log.topic, log.plant, log.zone], reportSearchQuery)), [reportTimelineEvents, reportSearchQuery]);
  const highEventLogs = useMemo(() => filteredReportTimelineEvents.filter((log) => log.severity === "High"), [filteredReportTimelineEvents]);
  const reportCards = useMemo(() => {
    const badZones = visibleBadZones;
    const healthyAmrs = reportAmrs.filter((amr) => reportAmrStatus(amr) === "Good");
    const worstZone = badZones[0];
    return [
      { id: "health", label: "Plant Health", value: `${healthyAmrs.length}/${reportAmrs.length}`, help: "Open AMR health detail", tone: reportHealthTone(healthyAmrs.length) },
      { id: "risk", label: "Connectivity Risk", value: atRiskReportAmrs.length, help: atRiskReportAmrs.length ? `${atRiskReportAmrs.map((amr) => amr.name).slice(0, 3).join(", ")} need review` : "No at-risk AMRs in scope" },
      { id: "worst-area", label: "Worst Area", value: worstZone?.zone || "None", help: worstZone ? `${worstZone.plant} score ${worstZone.score}; ${(worstZone.robots || []).join(", ")}` : "No bad zones detected from current RDS sample" },
      { id: "events", label: "High Events", value: highEventLogs.length, help: highEventLogs.length ? "High severity events need review" : "No high severity RDS events in scope", action: "View All" }
    ];
  }, [visibleBadZones, reportAmrs, atRiskReportAmrs, highEventLogs]);
  const currentUser = "admin";
  async function toggleZone(zone: BadZone) {
    const zoneId = zoneApiId(zone);
    if (expandedZoneId === zoneId) {
      setExpandedZoneId("");
      return;
    }
    setExpandedZoneId(zoneId);
    if (zoneEvents[zoneId]) return;
    setZoneEventStatus((current) => ({ ...current, [zoneId]: "Loading events..." }));
    try {
      const response = await fetch(`/api/reports/bad-zones/${encodeURIComponent(zoneId)}/events`);
      if (!response.ok) throw new Error((await response.json()).error || response.statusText);
      const payload = await response.json() as ZoneEventsResponse;
      setZoneEvents((current) => ({ ...current, [zoneId]: payload.events || [] }));
      if (payload.acknowledgement) setZoneAcknowledgements((current) => ({ ...current, [zoneId]: payload.acknowledgement as ZoneAcknowledgement }));
      setZoneEventStatus((current) => ({ ...current, [zoneId]: "" }));
    } catch (error) {
      setZoneEventStatus((current) => ({ ...current, [zoneId]: `Event load failed: ${error instanceof Error ? error.message : String(error)}` }));
    }
  }
  async function acknowledgeZone(zone: BadZone, event: React.MouseEvent) {
    event.stopPropagation();
    const zoneId = zoneApiId(zone);
    setZoneEventStatus((current) => ({ ...current, [zoneId]: "Saving acknowledgement..." }));
    try {
      const response = await fetch(`/api/reports/bad-zones/${encodeURIComponent(zoneId)}/acknowledge`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ ack_by: currentUser, notes: zoneAckNotes[zoneId] || "", plant_id: zone.plant })
      });
      if (!response.ok) throw new Error((await response.json()).error || response.statusText);
      const ack = await response.json() as ZoneAcknowledgement;
      setZoneAcknowledgements((current) => ({ ...current, [zoneId]: ack }));
      setZoneAckNotes((current) => ({ ...current, [zoneId]: "" }));
      setZoneEventStatus((current) => ({ ...current, [zoneId]: "" }));
    } catch (error) {
      setZoneEventStatus((current) => ({ ...current, [zoneId]: `Acknowledge failed: ${error instanceof Error ? error.message : String(error)}` }));
    }
  }
  async function exportBadZonesCsv() {
    const params = new URLSearchParams({ format: "csv" });
    if (plantFilter !== "All") params.set("plant", plantFilter);
    setBusy("Exporting Bad Zones");
    try {
      const response = await fetch(`/api/reports/bad-zones/export?${params.toString()}`);
      if (!response.ok) throw new Error((await response.text()) || response.statusText);
      const blob = await response.blob();
      const downloadUrl = URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = downloadUrl;
      link.download = `DRISHTI_BadZones_${new Date().toISOString().slice(0, 10)}.csv`;
      document.body.appendChild(link);
      link.click();
      link.remove();
      URL.revokeObjectURL(downloadUrl);
    } catch (error) {
      window.alert(`Bad Zone CSV export failed: ${error instanceof Error ? error.message : String(error)}`);
    } finally {
      setBusy("");
    }
  }
  function scrollToWorstZone() {
    const zone = visibleBadZones[0];
    if (!zone) return;
    const targetId = zoneDomId(zone);
    setHighlightedZoneId(targetId);
    window.requestAnimationFrame(() => document.getElementById(targetId)?.scrollIntoView({ behavior: "smooth", block: "center" }));
    window.setTimeout(() => setHighlightedZoneId((current) => current === targetId ? "" : current), 1800);
  }
  function handleReportCardClick(id: string) {
    if (id === "health") setActiveReportDrilldown("health");
    if (id === "risk") setActiveReportDrilldown("risk");
    if (id === "worst-area") scrollToWorstZone();
  }
  function toggleTimelineSeverity(severity: Severity) {
    setTimelineSeverities((current) => ({ ...current, [severity]: !current[severity] }));
  }
  function openLogsForEvent(event: LogEntry) {
    const start = addMinutes(event.time, -5);
    const end = addMinutes(event.time, 5);
    setLogAmrFilter(event.amr || "");
    setLogStart(start);
    setLogEnd(end);
    setLogKeyword("");
    setSelectedTimelineEvent(null);
    setView("logs");
    const params = new URLSearchParams();
    params.set("view", "logs");
    if (event.amr) params.set("amr", event.amr);
    if (start) params.set("start", start);
    if (end) params.set("end", end);
    window.history.replaceState(null, "", `?${params.toString()}`);
  }
  function exportRssiCapture() {
    const capturedAt = new Date().toISOString();
    const pointByAmr = new Map(state.wifiPoints.filter((point) => point.plant === heatmapPlant).map((point) => [point.amr, point]));
    const rows = heatmapSignalAmrs.map((amr) => {
      const point = pointByAmr.get(amr.name);
      const source = amr.source || point?.source || "RDS estimate";
      const ssid = ssidLabel(amr.ssid || point?.ssid);
      const rssi = amr.rssi ?? point?.rssi ?? "";
      return [capturedAt, amr.plant, amr.name, amr.ip, ssid, rssi, amr.rdsConfidence ?? point?.rdsConfidence ?? "", connectivityQuality(amr), source, amr.networkDelay !== undefined ? `${amr.networkDelay} ms` : "", amr.rdsX ?? "", amr.rdsY ?? ""];
    });
    const csv = [["Captured At", "Plant", "AMR", "IP", "Connected WiFi SSID", "dBm Strength", "RDS Confidence", "Quality", "Source", "Network Delay", "RDS X", "RDS Y"], ...rows].map((row) => row.map(csvCell).join(",")).join("\n");
    const url = URL.createObjectURL(new Blob([csv], { type: "text/csv;charset=utf-8" }));
    const link = document.createElement("a");
    link.href = url;
    link.download = `drishti-${slug(heatmapPlant)}-wifi-rssi-${capturedAt.replace(/[:.]/g, "-")}.csv`;
    link.click();
    URL.revokeObjectURL(url);
  }
  async function recordScanSample(plant = heatmapPlant) {
    const connection = connections.find((item) => item.plant === plant);
    if (!connection) {
      setScanStatus(`No RDS connection saved for ${plant}.`);
      setScanRecording(false);
      return;
    }
    try {
      const response = await fetch(rdsProxyUrl(plant, "core", true, plant));
      if (!response.ok) throw new Error((await response.json()).error || response.statusText);
      const normalized = normalizeRdsCoreResponse(await response.json(), plant, connection);
      if (!state.sceneMaps[plant]) {
        await fetchSceneMap(plant).catch((error) => setScanStatus(`Recording ${plant}: confidence saved, but map pull failed: ${error instanceof Error ? error.message : String(error)}`));
      }
      mergeImport(normalized, "heatmap");
      const sampleCount = buildConfidenceSamples(normalized.summary.plant, normalized.amrs, normalized.points, state.sceneMaps[plant]?.md5 || normalized.summary.sceneMd5).length;
      const targetName = scanTargetAmr || normalized.amrs[0]?.name || "";
      const target = normalized.amrs.find((amr) => amr.name === targetName);
      const homeLocation = scanHomeLocation.trim();
      const currentLocations = [target?.currentStation, target?.worstDrop, target?.assignedTask].filter(Boolean) as string[];
      const currentLocation = currentLocations[0] || "unknown";
      const atHome = Boolean(target && homeLocation && currentLocations.some((location) => locationMatches(location, homeLocation)));
      const hadSeenAway = scanSeenAwayRef.current;
      if (target && homeLocation && !atHome) scanSeenAwayRef.current = true;
      if (stopScanAtHome && target && homeLocation && atHome && hadSeenAway) {
        scanSeenAwayRef.current = false;
        setScanRecording(false);
        setScanStatus(`Stopped recording: ${target.name} returned to ${homeLocation}. Saved ${sampleCount} points at ${new Date().toLocaleTimeString()}.`);
        return;
      }
      const homeStatus = homeLocation
        ? atHome && !hadSeenAway
          ? `; currently at home ${homeLocation}; recorder will stay on until ${targetName || "AMR"} leaves and returns`
          : `; current ${currentLocation}; waiting for return to home ${homeLocation}`
        : "";
      setScanStatus(`Recording ${plant}${targetName ? ` / ${targetName}` : ""}: saved ${sampleCount} confidence points at ${new Date().toLocaleTimeString()}${homeStatus}.`);
    } catch (error) {
      setScanStatus(`Scan recording failed: ${error instanceof Error ? error.message : String(error)}`);
    }
  }
  function saveConfidenceSnapshot(plant = heatmapPlant, capturedAt = new Date().toISOString()) {
    const amrs = state.amrs.filter((amr) => amr.plant === plant);
    const points = state.wifiPoints.filter((point) => point.plant === plant);
    return buildConfidenceSamples(plant, amrs, points, state.sceneMaps[plant]?.md5, capturedAt);
  }
  function saveAllConfidenceSnapshots() {
    const plants = unique(state.amrs.map((amr) => amr.plant));
    const capturedAt = new Date().toISOString();
    const samples = plants.flatMap((plant) => saveConfidenceSnapshot(plant, capturedAt));
    setState((current) => ({
      ...current,
      confidenceSamples: mergeConfidenceSamples(current.confidenceSamples || [], samples),
      rdsImportNote: samples.length ? `Saved ${samples.length} confidence samples across ${plants.length} plant maps. Keeping rolling 5-day history.` : "No mappable AMR confidence samples found. Pull RDS core/map first."
    }));
    if (samples.length) {
      setSelectedScanTime(capturedAt);
      setScanStatus(`Saved all map scan point: ${samples.length} samples across ${plants.length} plants at ${new Date(capturedAt).toLocaleString()}.`);
    }
  }
  function openScanRun(run: { plant: string; time: string }) {
    setSelectedImportPlant(run.plant);
    setSignalFilter("All");
    setShowMovingOnly(false);
    setSelectedMapAmr("");
    setSelectedScanTime(run.time);
    setConfidenceHistoryMode("5 days");
    setView("heatmap");
    if (!state.sceneMaps[run.plant] && connections.some((connection) => connection.plant === run.plant)) {
      void fetchSceneMap(run.plant).catch((error) => setScanStatus(`Opened saved scan, but map pull failed: ${error instanceof Error ? error.message : String(error)}`));
    }
    setScanStatus(`Opened saved scan for ${run.plant} from ${new Date(run.time).toLocaleString()}.`);
  }
  function deleteScanRun(run: { plant: string; time: string }) {
    const label = `${run.plant} ${new Date(run.time).toLocaleString()}`;
    if (!window.confirm(`Delete saved confidence scan ${label}?`)) return;
    setState((current) => ({
      ...current,
      confidenceSamples: (current.confidenceSamples || []).filter((sample) => !(sample.plant === run.plant && sample.time === run.time)),
      rdsImportNote: `Deleted saved confidence scan ${label}.`
    }));
    if (selectedScanTime === run.time && selectedImportPlant === run.plant) setSelectedScanTime("");
    setScanStatus(`Deleted saved confidence scan ${label}.`);
  }
  function deleteVisibleScanRuns() {
    if (!filteredScanRuns.length) return;
    if (!window.confirm(`Delete ${filteredScanRuns.length} visible saved confidence scans?`)) return;
    const keys = new Set(filteredScanRuns.map((run) => run.key));
    setState((current) => ({
      ...current,
      confidenceSamples: (current.confidenceSamples || []).filter((sample) => !keys.has(`${sample.plant}|${sample.time}`)),
      rdsImportNote: `Deleted ${keys.size} saved confidence scans from the current Scans filter.`
    }));
    if (selectedScanTime && keys.has(`${selectedImportPlant}|${selectedScanTime}`)) setSelectedScanTime("");
    setScanStatus(`Deleted ${keys.size} visible saved confidence scans.`);
  }
  function mergeImport(normalized: NormalizedRds, nextView: View = "dashboard") {
    setState((current) => ({
      ...current,
      amrs: current.amrs.filter((item) => item.plant !== normalized.summary.plant).concat(normalized.amrs),
      wifiPoints: current.wifiPoints.filter((item) => item.plant !== normalized.summary.plant).concat(normalized.points),
      logs: current.logs.filter((item) => item.plant !== normalized.summary.plant).concat(normalized.logs),
      badZones: current.badZones.filter((item) => item.plant !== normalized.summary.plant).concat(deriveBadZones(normalized.amrs, normalized.points)),
      confidenceSamples: mergeConfidenceSamples(current.confidenceSamples || [], buildConfidenceSamples(normalized.summary.plant, normalized.amrs, normalized.points, current.sceneMaps[normalized.summary.plant]?.md5 || normalized.summary.sceneMd5)),
      rdsImportNote: `Imported ${normalized.summary.robots} ${normalized.summary.plant} AMRs from RDS core (${normalized.summary.createdOn}). Disconnected: ${normalized.summary.disconnected}. Model MD5: ${normalized.summary.modelMd5}. Scene MD5: ${normalized.summary.sceneMd5}.`,
      discovery: current.discovery.map((item) => item.point.includes("AMR ") || item.point.includes("RDS ") ? { ...item, status: "Available", source: "Go RDS proxy", gap: `Updated from ${normalized.summary.plant} core feed` } : item)
    }));
    setView(nextView);
  }
  async function pullLiveCore() {
    const connection = connections.find((item) => item.plant === selectedImportPlant);
    if (!connection) return;
    setBusy(`Pulling ${selectedImportPlant}`);
    try {
      const response = await fetch(rdsProxyUrl(selectedImportPlant, "core", true, selectedImportPlant));
      if (!response.ok) throw new Error((await response.json()).error || response.statusText);
      const payload = await response.json();
      mergeImport(normalizeRdsCoreResponse(payload, selectedImportPlant, connection));
      void fetchSceneMap(selectedImportPlant).catch((error) => setState((current) => ({ ...current, rdsImportNote: `${current.rdsImportNote} Map pull failed: ${error instanceof Error ? error.message : String(error)}` })));
    } catch (error) {
      setState((current) => ({ ...current, rdsImportNote: `Live pull failed: ${error instanceof Error ? error.message : String(error)}` }));
    } finally { setBusy(""); }
  }
  async function fetchSceneMap(plant: string) {
    const response = await fetch(rdsProxyUrl(plant, "scene", true, plant));
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
  async function pullAllSceneMaps() {
    if (!mappedPlants.length) return;
    setBusy("Pulling all maps");
    const loaded: string[] = [];
    const failed: string[] = [];
    try {
      for (const plant of mappedPlants) {
        try {
          await fetchSceneMap(plant);
          loaded.push(plant);
        } catch (error) {
          failed.push(`${plant}: ${error instanceof Error ? error.message : String(error)}`);
        }
      }
      setState((current) => ({
        ...current,
        rdsImportNote: failed.length
          ? `Loaded maps for ${loaded.join(", ") || "none"}. Failed: ${failed.join("; ")}.`
          : `Loaded RDS maps for ${loaded.join(", ")}. Use Map Plant to switch between them.`
      }));
      setView("heatmap");
    } finally { setBusy(""); }
  }
  async function pullAllRdsData() {
    if (!mappedPlants.length) return;
    setBusy("Pulling all RDS");
    const loaded: string[] = [];
    const failed: string[] = [];
    try {
      for (const plant of mappedPlants) {
        const connection = connections.find((item) => item.plant === plant);
        if (!connection) continue;
        try {
          const response = await fetch(rdsProxyUrl(plant, "core", true, plant));
          if (!response.ok) throw new Error((await response.json()).error || response.statusText);
          mergeImport(normalizeRdsCoreResponse(await response.json(), plant, connection), "heatmap");
          await fetchSceneMap(plant);
          loaded.push(plant);
        } catch (error) {
          failed.push(`${plant}: ${error instanceof Error ? error.message : String(error)}`);
        }
      }
      setState((current) => ({
        ...current,
        rdsImportNote: failed.length
          ? `Loaded RDS core/maps for ${loaded.join(", ") || "none"}. Failed: ${failed.join("; ")}.`
          : `Loaded RDS core and maps for ${loaded.join(", ")}. Use Map Plant to switch between them.`
      }));
      setView("heatmap");
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
      setWifiDiscover({ ok: false, message: "Username is required for AMR RSSI auto-discovery. Enter the approved read-only AMR SSH username, save, then test again.", results: [] });
      return;
    }
    if (isPlaceholderCredential(source.secretRef)) {
      setWifiDiscover({ ok: false, message: "Credential Reference must be the private key path inside DRISHTI, for example /app/data/keys/<key_file>.", results: [] });
      return;
    }
    setBusy("Testing AMR RSSI");
    try {
      const response = await fetch("/api/wifi/discover", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ source, robots }) });
      const result = await response.json() as WifiDiscoverResponse;
      const results = result.results || [];
      setWifiDiscover({ ...result, results });
      const realResults = new Map(results.filter((item) => item.ok && item.rssi !== undefined && item.quality && item.quality !== "Unknown").map((item) => [item.amr, item]));
      setState((current) => {
        const nextWifiPoints = current.wifiPoints.map((point) => {
          const match = realResults.get(point.amr);
          return match && point.plant === match.plant ? { ...point, rssi: match.rssi!, quality: match.quality as WifiPoint["quality"], ap: `Robot ${match.host}`, ssid: ssidLabel(match.ssid), source: "AMR SSH Auto-Discovery", time: new Date().toISOString() } : point;
        });
        const nextAmrs = current.amrs.map((amr) => {
          const match = realResults.get(amr.name);
          return match && amr.plant === match.plant ? { ...amr, rssi: match.rssi!, ap: `Robot ${match.host}`, ssid: ssidLabel(match.ssid), source: "AMR SSH Auto-Discovery" } : amr;
        });
        return {
          ...current,
          wifiPoints: nextWifiPoints,
          amrs: nextAmrs,
          confidenceSamples: mergeConfidenceSamples(current.confidenceSamples || [], buildConfidenceSamples(plant, nextAmrs, nextWifiPoints, current.sceneMaps[plant]?.md5)),
          discovery: current.discovery.map((item) => item.point === "Wi-Fi RSSI" ? { ...item, status: result.ok ? "Available" : "Partial", source: "AMR SSH Auto-Discovery", command: "RDS basic_info.ip + SSH Wi-Fi command detection", gap: result.message } : item)
        };
      });
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
    <aside className="sidebar"><div className="brand-block"><div className="brand-mark">D</div><div><div className="brand-title">DRISHTI</div><div className="brand-subtitle">AMR Health</div></div></div><nav className="nav-list">{(["dashboard", "logs", "discovery", "heatmap", "scans", "reports", "admin"] as View[]).map((item) => <button key={item} className={`nav-item ${view === item ? "active" : ""}`} onClick={() => setView(item)}><span>{item[0].toUpperCase()}</span>{item[0].toUpperCase() + item.slice(1)}</button>)}</nav><div className="sidebar-status"><div className="status-dot"></div><div><strong>Go + React</strong><span>Local RDS proxy enabled</span></div></div></aside>
    <main className="main-content"><header className="topbar"><div><h1>{view === "dashboard" ? "AMR Health Dashboard" : view[0].toUpperCase() + view.slice(1)}</h1><p>Go backend, React UI, local config, and local-only RDS snapshots.</p></div><div className="topbar-controls"><label className="field compact"><span>Plant</span><select value={plantFilter} onChange={(e) => setPlantFilter(e.target.value)}><option>All</option>{plantOptions.map((plant) => <option key={plant}>{plant}</option>)}</select></label><div className="field search-field report-search-field"><span>Search</span><div className="search-box"><input ref={searchInputRef} value={search} onChange={(e) => { setSearch(e.target.value); if (view === "reports") setShowSearchSuggestions(true); }} onFocus={() => view === "reports" && setShowSearchSuggestions(true)} placeholder={view === "reports" ? "Search reports (Ctrl+K)" : view === "scans" ? "Saved scan, AMR, plant, map" : "AMR, IP, zone, AP, topic"} />{search && <button className="search-clear" type="button" onClick={() => { setSearch(""); setDebouncedSearch(""); setSearchSuggestions([]); setShowSearchSuggestions(false); searchInputRef.current?.focus(); }} aria-label="Clear search">X</button>}</div>{view === "reports" && showSearchSuggestions && searchSuggestions.length > 0 && <div className="suggestion-list">{searchSuggestions.map((suggestion) => <button key={suggestion} type="button" onMouseDown={(event) => event.preventDefault()} onClick={() => { setSearch(suggestion); setDebouncedSearch(suggestion); setSearchSuggestions([]); setShowSearchSuggestions(false); searchInputRef.current?.focus(); }}>{suggestion}</button>)}</div>}</div></div></header>
      {view === "dashboard" && <section className="view active-view"><div className="metric-grid">{metrics.map(([label, value, help]) => <article className="metric-card" key={label}><span className="metric-label">{label}</span><strong className="metric-value">{value}</strong><small className="metric-help">{help}</small></article>)}</div><div className="content-grid two-col"><section className="panel wide-panel"><div className="panel-header"><div><h2>AMR Fleet Health</h2><p>Investigate disconnects, offline time, and worst drop locations.</p></div><button className="primary-action" onClick={pullLiveCore} disabled={!connections.length || Boolean(busy)}>{busy || "Pull Selected RDS"}</button></div><div className="table-wrap"><table><thead><tr><th>AMR Name</th><th>Plant</th><th>IP</th><th>Status</th><th>Reconnect</th><th>Disconnect</th><th>Offline</th><th>Worst Drop</th><th>Investigate</th></tr></thead><tbody>{filteredAmrs.map((amr) => <tr key={amr.id}><td><strong>{amr.name}</strong></td><td>{amr.plant}</td><td>{amr.ip}</td><td>{badge(amr.status)}</td><td>{amr.reconnects}</td><td>{amr.disconnects}</td><td>{amr.offline}</td><td>{amr.worstDrop}</td><td><button className="row-action" onClick={() => setSelectedAmr(amr)}>Open</button></td></tr>)}</tbody></table></div></section><section className="panel"><div className="panel-header stacked"><h2>Bad Zone Areas</h2><p>Top repeated drop, reconnect, offline, and weak Wi-Fi areas.</p></div><div className="zone-list">{visibleBadZones.length ? visibleBadZones.map((zone) => <article className="zone-card" key={`${zone.plant}-${zone.zone}`}><header><strong>{zone.zone}</strong><span>{zone.score}</span></header><small>{zone.plant} - {zone.disconnects} disconnects, {zone.reconnects} reconnects, robots {(zone.robots || []).join(", ") || "none"}</small><small>{zone.reason || "Computed from current AMR connectivity"}</small><div className="score-bar"><span style={{ width: `${Math.min(zone.score, 100)}%` }}></span></div></article>) : <article className="zone-card"><strong>No Bad Zones</strong><small>Current RDS sample does not show disconnected, weak, or poor AMR connectivity in scope.</small></article>}</div></section></div>{selectedAmr && <section className="panel detail-panel"><div className="panel-header stacked"><h2>{selectedAmr.name} Detail</h2><p>{selectedAmr.plant} - {selectedAmr.ip} - worst drop: {selectedAmr.worstDrop}</p></div><div className="detail-grid">{[["Status", selectedAmr.status], ["Battery", selectedAmr.battery || "unknown"], ["RDS Position", selectedAmr.rdsX !== undefined ? `x ${selectedAmr.rdsX}, y ${selectedAmr.rdsY}` : "unknown"], ["Issue", selectedAmr.issue || "No issue"], ["Map / Model", `${selectedAmr.mapMd5 || "unknown"} / ${selectedAmr.modelMd5 || "unknown"}`]].map(([label, value]) => <article className="detail-card" key={label}><span>{label}</span><strong>{value}</strong></article>)}</div></section>}</section>}
      {view === "admin" && <section className="view active-view"><div className="admin-grid"><section className="panel"><div className="panel-header stacked"><h2>RDS Core Import</h2><p>Pull live RDS through the Go backend or import saved core JSON.</p></div><div className="import-actions"><label className="field compact"><span>Plant</span><select value={selectedImportPlant} onChange={(e) => setSelectedImportPlant(e.target.value)}>{plantOptions.map((plant) => <option key={plant}>{plant}</option>)}</select></label><button className="primary-action" onClick={pullLiveCore} disabled={Boolean(busy)}>{busy || "Pull Live Core"}</button><label className="file-action">Import Core JSON<input type="file" accept=".json,application/json" onChange={(e) => e.target.files?.[0] && void importFile(e.target.files[0])} /></label><button className="ghost-action" onClick={() => setState((current) => ({ ...current, amrs: current.amrs.filter((item) => !item.imported), wifiPoints: current.wifiPoints.filter((item) => !item.imported), logs: current.logs.filter((item) => !item.imported), rdsImportNote: "Imported RDS data cleared." }))}>Reset Imported Data</button></div><div className="threshold-note">{state.rdsImportNote}</div></section><section className="panel wide-panel"><div className="panel-header stacked"><h2>RDS API Connections</h2><p>Saved in local backend config, not committed to Git.</p></div><form className="form-grid" onSubmit={saveConnection}><label className="field"><span>Plant</span><input value={apiForm.plant} onChange={(e) => setApiForm({ ...apiForm, plant: e.target.value })} required placeholder="Shelbyville" /></label><label className="field"><span>Base URL</span><input value={apiForm.baseUrl} onChange={(e) => setApiForm({ ...apiForm, baseUrl: e.target.value })} required placeholder="http://rds-host:8080" /></label><label className="field"><span>Core Path</span><input value={apiForm.corePath} onChange={(e) => setApiForm({ ...apiForm, corePath: e.target.value })} required /></label><label className="field"><span>Scene Path</span><input value={apiForm.scenePath} onChange={(e) => setApiForm({ ...apiForm, scenePath: e.target.value })} required /></label><button className="primary-action" type="submit">Save Connection</button></form><div className="api-list">{connections.map((connection) => <article className="api-card" key={connection.plant}><header><strong>{connection.plant}</strong><button className="row-action" onClick={() => setApiForm(connection)}>Edit</button></header><div className="api-links"><span>Base <code>{connection.baseUrl}</code></span><span>Core <a href={apiUrl(connection, "corePath")} target="_blank">{apiUrl(connection, "corePath")}</a></span><span>Scene <a href={apiUrl(connection, "scenePath")} target="_blank">{apiUrl(connection, "scenePath")}</a></span><span>Local <code>/api/plants/{slug(connection.plant)}/rds/core</code></span></div></article>)}</div></section></div></section>}
      {view === "logs" && <section className="view active-view"><section className="panel filter-panel"><div className="panel-header"><div><h2>Log Investigation</h2><p>Filter AMR, RDS, Ubuntu, network, and VM evidence.</p></div><div className="log-filter-grid"><label className="field compact"><span>AMR</span><input value={logAmrFilter} onChange={(e) => setLogAmrFilter(e.target.value)} placeholder="AMR-01" /></label><label className="field compact"><span>From</span><input type="datetime-local" value={logStart} onChange={(e) => setLogStart(e.target.value)} /></label><label className="field compact"><span>To</span><input type="datetime-local" value={logEnd} onChange={(e) => setLogEnd(e.target.value)} /></label><label className="field compact"><span>Keyword</span><input value={logKeyword} onChange={(e) => setLogKeyword(e.target.value)} placeholder="disconnect, map, battery" /></label><button className="ghost-action" type="button" onClick={() => { setLogAmrFilter(""); setLogStart(""); setLogEnd(""); setLogKeyword(""); }}>Clear</button></div></div></section><section className="panel"><div className="table-wrap"><table><thead><tr><th>Time</th><th>Plant</th><th>AMR</th><th>Topic</th><th>Source</th><th>Severity</th><th>Message</th></tr></thead><tbody>{filteredLogs.map((log, index) => <tr key={index}><td>{new Date(log.time).toLocaleString()}</td><td>{log.plant}</td><td>{log.amr}</td><td>{log.topic}</td><td>{log.source}</td><td>{badge(log.severity)}</td><td>{log.message}</td></tr>)}</tbody></table></div></section></section>}
      {view === "discovery" && <section className="view active-view"><div className="admin-grid">
        <section className="panel">
          <div className="panel-header stacked"><h2>Wi-Fi RSSI Source</h2><p>Add the source used to collect live signal strength. Store a vault/key reference here, not a password.</p></div>
          <form className="form-grid" onSubmit={saveWifiSource}>
            <label className="field"><span>Plant</span><input value={wifiForm.plant} onChange={(e) => setWifiForm({ ...wifiForm, plant: e.target.value })} placeholder="Shelbyville" /></label>
            <label className="field"><span>Source Name</span><input value={wifiForm.name} onChange={(e) => setWifiForm({ ...wifiForm, name: e.target.value })} /></label>
            <label className="field"><span>Method</span><select value={wifiForm.method} onChange={(e) => setWifiForm({ ...wifiForm, method: e.target.value as WifiSource["method"] })}><option>AMR SSH</option><option>Controller API</option><option>Manual Import</option></select></label>
            <label className="field"><span>Host or API</span><input value={wifiForm.host} onChange={(e) => setWifiForm({ ...wifiForm, host: e.target.value })} placeholder="optional: one AMR IP for single-host test" /></label>
            <label className="field"><span>Username</span><input value={wifiForm.username} onChange={(e) => setWifiForm({ ...wifiForm, username: e.target.value })} placeholder="read-only user" /></label>
            <label className="field"><span>Credential Reference</span><input value={wifiForm.secretRef} onChange={(e) => setWifiForm({ ...wifiForm, secretRef: e.target.value })} placeholder="CyberArk account or SSH key path" /></label>
            <label className="field wide-field"><span>RSSI Command or Path</span><input value={wifiForm.command} onChange={(e) => setWifiForm({ ...wifiForm, command: e.target.value })} placeholder="iw dev wlan0 link" /></label>
            <button className="primary-action" type="submit">Save RSSI Source</button>
            <button className="ghost-action" type="button" onClick={() => void testWifiSource()} disabled={Boolean(busy)}>{busy === "Testing RSSI" ? busy : "Test One Host RSSI"}</button>
            <button className="ghost-action" type="button" onClick={() => void testAmrRssi()} disabled={Boolean(busy)}>{busy === "Testing AMR RSSI" ? busy : "Test RSSI on AMRs"}</button>
          </form>
          {wifiTest && <div className={`wifi-test-result ${wifiTest.ok ? "ok" : "error"}`}><header>{badge(wifiTest.ok ? "Available" : "Partial")}<strong>{wifiTest.message}</strong></header>{wifiTest.rssi !== undefined && <span>RSSI {wifiTest.rssi} dBm - {wifiTest.quality} - SSID {ssidLabel(wifiTest.ssid)}</span>}{wifiTest.output && <pre>{wifiTest.output}</pre>}</div>}
          {wifiDiscover && <div className={`wifi-test-result ${wifiDiscover.ok ? "ok" : "error"}`}><header>{badge(wifiDiscover.ok ? "Available" : "Partial")}<strong>{wifiDiscover.message}</strong></header><div className="wifi-result-list">{(wifiDiscover.results || []).map((item) => <article key={`${item.plant}-${item.amr}-${item.host}`} className={item.ok ? "ok" : "error"}><strong>{item.amr}</strong><span>{item.host} - {item.message}</span>{item.rssi !== undefined && <small>{item.rssi} dBm - {item.quality} - SSID {ssidLabel(item.ssid)} - {item.command}</small>}{item.output && <pre>{item.output}</pre>}</article>)}</div></div>}
          <div className="api-list source-list">{(state.wifiSources || []).map((source) => <article className="api-card" key={`${source.plant}-${source.name}`}><header><strong>{source.plant}</strong><span>{badge(source.method)}</span></header><div className="api-links"><span>{source.name}</span><span>Host <code>{source.host || "not set"}</code></span><span>User <code>{source.username || "not set"}</code></span><span>Credential <code>{source.secretRef || "not set"}</code></span><span>Command <code>{source.command}</code></span><button className="row-action" onClick={() => void testWifiSource(source)} disabled={Boolean(busy)}>Test One Host RSSI</button></div></article>)}</div>
        </section>
        <section className="panel">
          <div className="panel-header stacked"><h2>Data Discovery</h2><p>Source reliability and remaining telemetry gaps.</p></div>
          <div className="table-wrap"><table><thead><tr><th>Data Point</th><th>Status</th><th>Best Source</th><th>Command or Path</th><th>Gap</th></tr></thead><tbody>{state.discovery.map((item) => <tr key={item.point}><td><strong>{item.point}</strong></td><td>{badge(item.status)}</td><td>{item.source}</td><td><code>{item.command}</code></td><td>{item.gap}</td></tr>)}</tbody></table></div>
        </section>
        <section className="panel wide-panel discovery-signal-panel">
          <div className="panel-header"><div><h2>AMR Wi-Fi Signal</h2><p>Worst RSSI is sorted first. Dead Zone Risk rows stay pinned at the top.</p></div><button className="ghost-action" type="button" onClick={() => void loadDiscoverySignals()}>Refresh Signals</button></div>
          <div className="threshold-note">{discoveryStatus}</div>
          <div className="table-wrap"><table className="discovery-signal-table"><thead><tr>{discoveryColumns.map((column) => <th key={column.key}><button className="sortable-header" type="button" onClick={() => toggleDiscoverySort(column.key)}><span>{column.label}</span><small>{discoverySort.key === column.key ? discoverySort.direction.toUpperCase() : "SORT"}</small></button></th>)}</tr></thead><tbody>
            {sortedDiscoveryRows.length ? sortedDiscoveryRows.map((row) => {
              const rssiTier = rssiSignalTier(row.rssi_dbm);
              const snrTier = snrSignalTier(row.snr_db);
              const deadZone = hasRealRssi(row.rssi_dbm) && (row.rssi_dbm as number) <= -80;
              return <tr key={`${row.plant}-${row.amr}`} className={deadZone ? "dead-zone-row" : ""}>
                <td><strong>{row.amr}</strong>{deadZone && <span className="dead-zone-pill">Dead Zone Risk</span>}</td>
                <td>{row.plant}</td>
                <td><span className={`signal-cell signal-${rssiTier}`}><SignalBarsIcon bars={signalBarsForRssi(row.rssi_dbm)} tier={rssiTier} /><strong>{rssiDisplay(row.rssi_dbm)}</strong><small>{signalLabel(row.rssi_dbm)}</small></span></td>
                <td><span className={`snr-pill signal-${snrTier}`}>{typeof row.snr_db === "number" && Number.isFinite(row.snr_db) ? `${row.snr_db} dB` : "No SNR"}</span></td>
                <td>{row.ap_name || "not reported"}</td>
                <td>{row.band || "not reported"}</td>
                <td>{row.channel || "not reported"}</td>
                <td>{formatMaybeTime(row.last_seen)}</td>
                <td>{row.source || "RDS"}</td>
              </tr>;
            }) : <tr><td colSpan={discoveryColumns.length}>No Discovery rows yet. Pull RDS data or configure the Go RSSI fallback environment variables.</td></tr>}
          </tbody></table></div>
        </section>
      </div></section>}      {view === "heatmap" && <section className="view active-view"><section className="panel heatmap-panel"><div className="panel-header"><div><h2>AMR Plant Map</h2><p>RDS scene map with live AMR connectivity zones. Hover or click an AMR for connection detail; true RSSI appears after the Discovery source is connected.</p></div><div className="heatmap-actions"><label className="field compact"><span>Map Plant</span><select value={heatmapPlant} onChange={(e) => { setSelectedImportPlant(e.target.value); setSelectedMapAmr(""); setSelectedScanTime(""); }}>{plantOptions.filter((plant) => plant !== "All").map((plant) => <option key={plant}>{plant}</option>)}</select></label><label className="field compact"><span>Signal</span><select value={signalFilter} onChange={(e) => setSignalFilter(e.target.value)}><option>All</option><option>Good</option><option>Weak</option><option>Poor</option><option>Critical</option></select></label><button className="primary-action" onClick={() => void pullSceneMap(heatmapPlant)} disabled={Boolean(busy)}>{busy || "Pull RDS Map"}</button><button className="ghost-action" onClick={() => void pullAllRdsData()} disabled={Boolean(busy) || !mappedPlants.length}>{busy === "Pulling all RDS" ? busy : "Pull All RDS"}</button><button className="ghost-action" onClick={() => void pullAllSceneMaps()} disabled={Boolean(busy) || !mappedPlants.length}>{busy === "Pulling all maps" ? busy : "Pull All Maps"}</button><button className="ghost-action" onClick={saveAllConfidenceSnapshots} disabled={!state.amrs.length}>Save Scan Point</button><button className={scanRecording ? "danger-action" : "ghost-action"} onClick={() => { const next = !scanRecording; scanSeenAwayRef.current = false; setScanRecording(next); setScanStatus(next ? `Recording ${heatmapPlant}${scanTargetAmr ? ` / ${scanTargetAmr}` : ""} every 8 seconds...` : "Scan recorder stopped."); }} disabled={!connections.some((item) => item.plant === heatmapPlant)}>{scanRecording ? "Stop Scan Recording" : "Start Scan Recording"}</button><label className="field compact"><span>Scan AMR</span><select value={scanTargetAmr} onChange={(e) => { setScanTargetAmr(e.target.value); scanSeenAwayRef.current = false; }}><option value="">Auto select</option>{heatmapReadinessAmrs.map((amr) => <option key={`${amr.id}-scan`} value={amr.name}>{amr.name}</option>)}</select></label><label className="field compact"><span>Home Location</span><input list="scan-home-options" value={scanHomeLocation} onChange={(e) => { setScanHomeLocation(e.target.value); scanSeenAwayRef.current = false; }} placeholder="LM/PP/home station" /></label><datalist id="scan-home-options">{heatmapScanLocations.map((location) => <option key={location} value={location} />)}</datalist><label className="check-field"><input type="checkbox" checked={stopScanAtHome} onChange={(e) => setStopScanAtHome(e.target.checked)} /><span>Stop at home</span></label><label className="check-field"><input type="checkbox" checked={dailyScanEnabled} onChange={(e) => setDailyScanEnabled(e.target.checked)} /><span>Daily recorder</span></label><label className="field compact"><span>Daily Time</span><input type="time" value={dailyScanTime} onChange={(e) => setDailyScanTime(e.target.value)} /></label><label className="field compact"><span>Confidence Map</span><select value={confidenceHistoryMode} onChange={(e) => setConfidenceHistoryMode(e.target.value as "Current" | "5 days" | "Changes")}><option>Current</option><option>5 days</option><option>Changes</option></select></label>{selectedScanTime && <button className="ghost-action" type="button" onClick={() => setSelectedScanTime("")}>Viewing {new Date(selectedScanTime).toLocaleString()} - Clear</button>}<button className="ghost-action" onClick={exportRssiCapture} disabled={!heatmapSignalAmrs.length}>Export RSSI CSV</button><button className="ghost-action" onClick={() => setView("discovery")}>Connect RSSI Source</button><label className="check-field"><input type="checkbox" checked={focusMapOnAmrs} onChange={(e) => setFocusMapOnAmrs(e.target.checked)} /><span>Focus AMRs</span></label><label className="check-field"><input type="checkbox" checked={showMovingOnly} onChange={(e) => setShowMovingOnly(e.target.checked)} /><span>Moving only</span></label><label className="check-field"><input type="checkbox" checked={showMapPaths} onChange={(e) => setShowMapPaths(e.target.checked)} /><span>Path lines</span></label><label className="check-field"><input type="checkbox" checked={showMapLabels} onChange={(e) => setShowMapLabels(e.target.checked)} /><span>Map labels</span></label><div className="zoom-controls" aria-label="Map zoom"><button type="button" onClick={() => setMapZoom((value) => Math.max(1, Number((value - 0.5).toFixed(1))))}>-</button><strong>{mapZoom.toFixed(1)}x</strong><button type="button" onClick={() => setMapZoom((value) => Math.min(6, Number((value + 0.5).toFixed(1))))}>+</button><button type="button" onClick={() => setMapZoom(1)}>Reset</button></div></div></div><SceneMapView scene={activeSceneMap} points={heatmapPoints} amrs={heatmapAmrs} confidenceSamples={displayedHeatmapConfidenceSamples} confidenceMode={confidenceHistoryMode} signalFilter={signalFilter} showMapLabels={showMapLabels} showMapPaths={showMapPaths} focusMode={focusMapOnAmrs} zoomLevel={mapZoom} emptyMessage={heatmapEmptyMessage} onSelectAmr={(name) => setSelectedMapAmr(name)} /><div className="legend-row"><span><i className="legend confidence-high"></i>75%+ confidence</span><span><i className="legend confidence-medium"></i>51-74% confidence</span><span><i className="legend confidence-low"></i>50% and below confidence</span><span>{confidenceStats.count} saved samples / {confidenceStats.low} low / latest {confidenceStats.latest}</span><span>{scanStatus}</span></div><div className="readiness-panel"><header><strong>Moving AMRs / Command Readiness</strong><span>{showMovingOnly ? "Map filter: moving only" : "Map filter: all AMRs"}</span></header><div className="readiness-grid">{heatmapReadinessAmrs.map((amr) => <article className={`readiness-card ${amrIsMoving(amr) ? "moving" : "idle"}`} key={`${amr.id}-readiness`}><header><strong>{amr.name}</strong>{badge(amrIsMoving(amr) ? "Moving" : amr.status)}</header><div><span>Online</span><strong>{amr.status}</strong></div><div><span>Battery</span><strong>{amr.battery || "unknown"}</strong></div><div><span>Current Station</span><strong>{amr.currentStation || "unknown"}</strong></div><div><span>Assigned Task</span><strong>{amr.assignedTask || "No active task"}</strong></div><div><span>Safety Status</span><strong>{amr.safetyStatus || "unknown"}</strong></div><div><span>Command API</span><strong>{amr.commandApiStatus || "Read-only: not configured"}</strong></div></article>)}</div></div>{selectedHeatmapAmr && <div className="map-detail-card"><header><strong>{selectedHeatmapAmr.name}</strong>{badge(connectivityQuality(selectedHeatmapAmr))}</header><div className="detail-grid">{[["Status", selectedHeatmapAmr.status], ["IP", selectedHeatmapAmr.ip], ["Connection", connectivityReason(selectedHeatmapAmr)], ["Network Delay", selectedHeatmapAmr.networkDelay !== undefined ? `${selectedHeatmapAmr.networkDelay} ms` : "not reported"], ["Location", selectedHeatmapAmr.worstDrop || "unknown"], ["Battery", selectedHeatmapAmr.battery || "unknown"], ["Current Station", selectedHeatmapAmr.currentStation || "unknown"], ["Assigned Task", selectedHeatmapAmr.assignedTask || "No active task"], ["Safety Status", selectedHeatmapAmr.safetyStatus || "unknown"], ["Command API", selectedHeatmapAmr.commandApiStatus || "Read-only: not configured"], ["Connected WiFi SSID", ssidLabel(selectedHeatmapAmr.ssid)], ["RSSI Source", selectedHeatmapAmr.source === "AMR SSH Auto-Discovery" ? "Connected from AMR SSH Auto-Discovery" : "Not connected - use Connect RSSI Source"], ["Confidence", (() => { const confidence = confidenceForReading(connectivityQuality(selectedHeatmapAmr), { hasLiveRssi: selectedHeatmapAmr.source === "AMR SSH Auto-Discovery", hasIp: selectedHeatmapAmr.ip !== "unknown", hasPosition: selectedHeatmapAmr.rdsX !== undefined && selectedHeatmapAmr.rdsY !== undefined, hasSsid: ssidLabel(selectedHeatmapAmr.ssid) !== "not captured yet", status: selectedHeatmapAmr.status, rdsConfidence: selectedHeatmapAmr.rdsConfidence }); return `${confidence.score}% ${confidence.label} - ${confidence.basis}`; })()], ["RDS Confidence", selectedHeatmapAmr.rdsConfidence !== undefined ? `${selectedHeatmapAmr.rdsConfidence}%` : "not reported"], ["dBm Strength", selectedHeatmapAmr.source === "AMR SSH Auto-Discovery" ? `${selectedHeatmapAmr.rssi} dBm` : `${selectedHeatmapAmr.rssi} dBm estimated`], ["RDS Position", selectedHeatmapAmr.rdsX !== undefined ? `x ${selectedHeatmapAmr.rdsX}, y ${selectedHeatmapAmr.rdsY}` : "unknown"]].map(([label, value]) => <article className="detail-card" key={label}><span>{label}</span><strong>{value}</strong></article>)}</div></div>}</section></section>}      {view === "scans" && <section className="view active-view"><section className="panel"><div className="panel-header"><div><h2>Scan History</h2><p>Stored scan results from Save Scan Point and Scan Recording. Results stay local in this browser for 5 days; use Plant and Search to filter by AMR, plant, timestamp, or map ID.</p></div><div className="scan-actions"><button className="primary-action" onClick={saveAllConfidenceSnapshots} disabled={!state.amrs.length}>Save Current Scan Point</button><button className="delete-action" onClick={deleteVisibleScanRuns} disabled={!filteredScanRuns.length}>Delete Visible Scans</button></div></div><div className="scan-summary-grid"><article><span>Stored Scan Results</span><strong>{scanRuns.length}</strong></article><article><span>Visible After Filter</span><strong>{filteredScanRuns.length}</strong></article><article><span>Matching AMRs</span><strong>{unique(filteredScanRuns.flatMap((run) => run.amrs)).length}</strong></article><article><span>Storage</span><strong>Local / 5 days</strong></article></div><div className="threshold-note">Search checks saved plant names, AMR names, date/time, and map MD5. It does not start a new scan.</div><div className="scan-list">{filteredScanRuns.length ? filteredScanRuns.map((run) => <article className="scan-card" key={run.key}><header><div><strong>{run.plant}</strong><span>{new Date(run.time).toLocaleString()}</span></div>{badge(run.low ? "Weak" : "Good")}</header><div className="scan-meta"><span>{run.samples} samples</span><span>{run.amrs.length} AMRs</span><span>{run.high} high confidence</span><span>{run.low} low/poor</span><span>Map {run.mapMd5}</span></div><div className="scan-amrs">{run.amrs.slice(0, 12).join(", ")}{run.amrs.length > 12 ? ` +${run.amrs.length - 12} more` : ""}</div><div className="scan-actions"><button className="row-action" onClick={() => openScanRun(run)}>Open Saved Map</button><button className="delete-action" onClick={() => deleteScanRun(run)}>Delete Scan Map</button></div></article>) : <article className="scan-card empty"><strong>{scanRuns.length ? "No scan points match this filter" : "No saved scan points yet"}</strong><span>{scanRuns.length ? "Change Plant or Search to see other saved scan points." : "Use Save Current Scan Point here, or Start Scan Recording on the Heatmap page."}</span></article>}</div></section></section>}            {view === "reports" && <section className="view active-view"><div className="report-grid">{reportCards.map((card) => { const clickable = card.id !== "events"; return <article className={`report-card ${card.tone ? `tone-${card.tone}` : ""} ${clickable ? "clickable" : ""}`} key={card.label} role={clickable ? "button" : undefined} tabIndex={clickable ? 0 : undefined} onClick={() => clickable && handleReportCardClick(card.id)} onKeyDown={(event) => { if (clickable && (event.key === "Enter" || event.key === " ")) { event.preventDefault(); handleReportCardClick(card.id); } }}><span>{card.label}</span><strong>{card.value}</strong><small>{card.help}</small>{card.action && <button className="report-card-action" type="button" onClick={(event) => { event.stopPropagation(); setShowHighEventsModal(true); }} disabled={!highEventLogs.length}>View All</button>}</article>; })}</div><div className="content-grid two-col"><section className="panel"><div className="panel-header"><div><h2>Bad Zone Summary</h2><p>Areas computed from repeated disconnects, offline AMRs, weak/poor connectivity, and reconnect evidence.</p></div><button className="ghost-action" type="button" onClick={() => void exportBadZonesCsv()} disabled={busy === "Exporting Bad Zones"}>{busy === "Exporting Bad Zones" ? "Exporting..." : "Export CSV"}</button></div><div className="zone-list">{filteredReportZones.length ? filteredReportZones.map((zone) => { const zoneId = zoneApiId(zone); const ack = zoneAcknowledgements[zoneId]; const expanded = expandedZoneId === zoneId; const events = zoneEvents[zoneId] || []; return <article id={zoneDomId(zone)} className={`zone-card ${highlightedZoneId === zoneDomId(zone) ? "zone-flash" : ""} ${ack ? "acknowledged" : ""}`} key={`${zone.plant}-${zone.zone}-report`} role="button" tabIndex={0} onClick={() => void toggleZone(zone)} onKeyDown={(event) => { if (event.key === "Enter" || event.key === " ") { event.preventDefault(); void toggleZone(zone); } }}><header><strong>{highlightText(zone.zone, reportSearchQuery)}</strong><span className="zone-score"><span>{zone.score}</span><button className="score-help" type="button" title="Severity score 0-20, computed from disconnect frequency, RDS delay, and offline duration. Higher = worse." onClick={(event) => event.stopPropagation()}>?</button></span></header>{ack && <span className="ack-badge">Reviewed by {ack.ack_by} at {formatMaybeTime(ack.ack_at)}</span>}<small>{highlightText(zone.plant, reportSearchQuery)} - robots {highlightText((zone.robots || []).join(", ") || "none", reportSearchQuery)}</small><small>{zone.reason || "Computed from current AMR connectivity"}</small><div className="score-bar"><span style={{ width: `${Math.min(zone.score, 100)}%` }}></span></div>{expanded && <div className="zone-expanded" onClick={(event) => event.stopPropagation()}><div className="table-wrap compact-table"><table><thead><tr><th>Timestamp</th><th>RDS Delay (ms)</th><th>Duration (ms)</th><th>Reconnected At</th></tr></thead><tbody>{events.length ? events.map((item, index) => <tr key={`${zoneId}-${item.timestamp}-${index}`}><td>{formatMaybeTime(item.timestamp)}</td><td>{item.rds_delay_ms}</td><td>{item.duration_ms}</td><td>{formatMaybeTime(item.reconnected_at)}</td></tr>) : <tr><td colSpan={4}>No recent disconnect events found for this zone.</td></tr>}</tbody></table></div><div className="ack-controls"><input value={zoneAckNotes[zoneId] || ""} onChange={(event) => setZoneAckNotes((current) => ({ ...current, [zoneId]: event.target.value }))} placeholder="Optional acknowledgement notes" /><button className="row-action" type="button" onClick={(event) => void acknowledgeZone(zone, event)}>Acknowledge</button></div>{zoneEventStatus[zoneId] && <small className="zone-status">{zoneEventStatus[zoneId]}</small>}</div>}</article>; }) : <article className="zone-card"><strong>{reportSearchQuery ? "No Matching Bad Zones" : "No Bad Zones"}</strong><small>{reportSearchQuery ? "No bad zones match the current report search." : "Current RDS sample does not show disconnected, weak, or poor AMR connectivity in scope."}</small></article>}</div></section><section className="panel timeline-panel"><div className="panel-header"><div><h2>Correlation Timeline</h2><p>Imported RDS and infrastructure evidence.</p></div><button className={`live-toggle ${eventsLive ? "active" : ""}`} type="button" onClick={() => setEventsLive((current) => !current)}>{eventsLive && <span className="live-dot"></span>}{eventsLive ? "Live" : "Live Off"}</button></div><div className="timeline-controls"><div className="chip-row">{(["High", "Medium", "Low"] as Severity[]).map((severity) => <button key={severity} className={`filter-chip ${timelineSeverities[severity] ? "selected" : ""}`} type="button" onClick={() => toggleTimelineSeverity(severity)}>{severity}</button>)}</div><label className="field compact"><span>Range</span><select value={timelineRange} onChange={(e) => setTimelineRange(e.target.value as ReportRange)}><option value="1h">Last 1hr</option><option value="6h">Last 6hr</option><option value="24h">Last 24hr</option><option value="custom">Custom</option></select></label>{timelineRange === "custom" && <><label className="field compact"><span>Start</span><input type="datetime-local" value={customRangeStart} onChange={(e) => setCustomRangeStart(e.target.value)} /></label><label className="field compact"><span>End</span><input type="datetime-local" value={customRangeEnd} onChange={(e) => setCustomRangeEnd(e.target.value)} /></label></>}<button className="ghost-action" type="button" onClick={() => void fetchReportEvents(false)}>Refresh</button></div><div className="timeline-status">{eventsLive ? <span><span className="live-dot"></span>Live</span> : <span>Paused - last updated at {reportEventsUpdatedAt ? new Date(reportEventsUpdatedAt).toLocaleTimeString() : "not yet"}</span>}<span>{reportEventsStatus}</span></div><div className="timeline">{filteredReportTimelineEvents.length ? filteredReportTimelineEvents.map((log, index) => <article className="timeline-item clickable" key={`${log.time}-${log.amr}-${log.topic}-${index}`} role="button" tabIndex={0} onClick={() => setSelectedTimelineEvent(log)} onKeyDown={(event) => { if (event.key === "Enter" || event.key === " ") { event.preventDefault(); setSelectedTimelineEvent(log); } }}><time>{new Date(log.time).toLocaleString()}</time><div><strong>{highlightText(log.topic, reportSearchQuery)}</strong><small>{highlightText(log.plant, reportSearchQuery)} - {highlightText(log.amr || "Unknown AMR", reportSearchQuery)} - {highlightText(log.zone || "No zone", reportSearchQuery)} - {log.source} - {highlightText(log.message, reportSearchQuery)}</small></div>{badge(log.severity)}</article>) : <article className="timeline-item empty"><div><strong>{reportSearchQuery ? "No Matching Events" : "No events in this range"}</strong><small>{reportSearchQuery ? "No correlation events match the current report search." : "Adjust severity chips, plant, or time range."}</small></div></article>}</div></section></div></section>}{showHighEventsModal && <div className="modal-backdrop" role="dialog" aria-modal="true" aria-label="High Events"><section className="event-modal"><header><div><h2>High Events</h2><p>High severity RDS events in the current report filter.</p></div><button className="modal-close" type="button" onClick={() => setShowHighEventsModal(false)} aria-label="Close high events">Close</button></header><div className="event-list">{highEventLogs.length ? highEventLogs.map((log, index) => <article className="event-row" key={`${log.time}-${log.amr}-${log.topic}-${index}`}><strong>{log.amr || "Unknown AMR"}</strong><span>{log.topic || log.message || "High severity event"}</span><time>{log.time ? new Date(log.time).toLocaleString() : "Timestamp not available"}</time></article>) : <article className="event-row empty"><strong>No High Events</strong><span>No high severity RDS events in the current filter.</span><time>-</time></article>}</div></section></div>}{selectedTimelineEvent && <aside className="event-drawer" role="dialog" aria-modal="true" aria-label="Event Detail"><header><div><h2>{selectedTimelineEvent.topic}</h2><p>{selectedTimelineEvent.plant} - {selectedTimelineEvent.amr || "Unknown AMR"}</p></div><button className="drawer-close" type="button" onClick={() => setSelectedTimelineEvent(null)} aria-label="Close event detail">X</button></header><div className="drawer-body"><article><span>Full Event Text</span><strong>{selectedTimelineEvent.message}</strong></article><article><span>AMR</span><strong>{selectedTimelineEvent.amr || "Unknown AMR"}</strong></article><article><span>Plant</span><strong>{selectedTimelineEvent.plant}</strong></article><article><span>Zone</span><strong>{selectedTimelineEvent.zone || "not reported"}</strong></article><article><span>Timestamp</span><strong>{formatMaybeTime(selectedTimelineEvent.time)}</strong></article><button className="row-action" type="button" onClick={() => openLogsForEvent(selectedTimelineEvent)}>View logs for this AMR +/-5 min</button></div></aside>}{activeReportDrilldown && <div className="modal-backdrop" role="dialog" aria-modal="true" aria-label={activeReportDrilldown === "health" ? "Plant Health Detail" : "Connectivity Risk Detail"}><section className="event-modal report-drilldown-modal"><header><div><h2>{activeReportDrilldown === "health" ? "Plant Health" : "Connectivity Risk"}</h2><p>{activeReportDrilldown === "health" ? "AMRs sorted with unhealthy units first." : "At-risk AMRs with last seen time and RDS network delay."}</p></div><button className="modal-close" type="button" onClick={() => setActiveReportDrilldown(null)} aria-label="Close report detail">Close</button></header><div className="report-drilldown-list">{activeReportDrilldown === "health" ? (healthDrilldownAmrs.length ? healthDrilldownAmrs.map((amr) => <article className="report-drilldown-row health-row" key={`${amr.plant}-${amr.name}-health`}><strong>{amr.name}</strong>{badge(reportAmrStatus(amr))}<span>{amr.plant}</span><span>{amr.currentStation || amr.worstDrop || "No station reported"}</span></article>) : <article className="report-drilldown-row empty"><strong>No AMRs</strong><span>No AMRs match the current plant filter.</span></article>) : (atRiskReportAmrs.length ? atRiskReportAmrs.map((amr) => <article className="report-drilldown-row risk-row" key={`${amr.plant}-${amr.name}-risk`}><strong>{amr.name}</strong><span>{formatMaybeTime(lastPingTimeByAmr.get(amr.name))}</span><span>{Number.isFinite(Number(amr.networkDelay)) ? `${amr.networkDelay} ms` : "not reported"}</span>{badge(reportAmrStatus(amr))}</article>) : <article className="report-drilldown-row empty"><strong>No At-Risk AMRs</strong><span>Current plant filter has no connectivity risk.</span></article>)}</div></section></div>}    </main>
  </div>;
}

createRoot(document.getElementById("root")!).render(<App />);

