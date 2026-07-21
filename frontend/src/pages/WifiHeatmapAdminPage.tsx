import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  AlertTriangle,
  CircleStop,
  Map as MapIcon,
  Play,
  RefreshCw,
  Save,
  Upload,
  WifiOff,
} from "lucide-react";
import {
  listWifiHeatmapSessions,
  queryWifiHeatmap,
  saveWifiHeatmapPoint,
  saveWifiSurveyRoutePoint,
  startWifiHeatmapSession,
  stopWifiHeatmapSession,
  type WifiHeatmapPointInput,
} from "../api/client";

type Connection = {
  plant: string;
  baseUrl: string;
  corePath: string;
  scenePath: string;
};
type Wifi = {
  plant: string;
  amr: string;
  rssi_dbm?: number;
  snr_db?: number;
  ap_name: string;
  band: string;
  channel: string;
  last_seen: string;
  source: string;
};
type Robot = {
  name: string;
  x: number;
  y: number;
  heading?: number;
  speed: number;
  moving: boolean;
  connected: boolean;
  mapVersion: string;
  timestamp: string;
  latency?: number;
};
type Scene = {
  md5: string;
  mapVersion?: string;
  bounds: { minX: number; minY: number; maxX: number; maxY: number };
  paths: { a: { x: number; y: number }; b: { x: number; y: number } }[];
  points: { name: string; x: number; y: number }[];
};
type SavedMap = {
  id: string;
  name: string;
  scene: Scene;
  savedAt: string;
  source: "RDS" | "Upload";
};
type Raw = {
  x: number;
  y: number;
  rssi_dbm: number;
  snr_db?: number;
  bssid: string;
  amr_id: string;
  timestamp: string;
  disconnect_event: boolean;
  roam_event: boolean;
};
type RoutePoint = {
  session_id: number;
  x: number;
  y: number;
  amr_id: string;
  timestamp: string;
  moving: boolean;
  connected: boolean;
  nearest_location: string;
};
type Cell = {
  x: number;
  y: number;
  measurement_count: number;
  average: number;
  minimum: number;
  maximum: number;
  worst: number;
  amr_count: number;
  most_common_bssid: string;
  first_timestamp: string;
  last_timestamp: string;
  confidence_level: string;
  average_snr?: number;
  disconnect_count: number;
  roam_count: number;
  contributing_amrs: string[];
};
type APPin = {
  bssid: string;
  name: string;
  x: number;
  y: number;
  source: "Manual" | "Estimated";
  confidence: "confirmed" | "low" | "medium" | "high";
  samples: number;
  updatedAt: string;
};
type SurveySession = {
  id: number;
  plant_id: string;
  map_id: string;
  amr_id: string;
  status: string;
  sample_count: number;
  route_count: number;
  started_at: string;
  stopped_at?: string;
};

const slug = (v: string) =>
  v
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "");
const proxy = (plant: string, kind: "core" | "scene") =>
  `/api/plants/${slug(plant)}/rds/${kind}?plant=${encodeURIComponent(plant)}`;
const color = (rssi: number, unknown = false) =>
  unknown
    ? "#64748b"
    : rssi >= -55
      ? "#16a34a"
      : rssi >= -65
        ? "#84cc16"
        : rssi >= -70
          ? "#facc15"
          : rssi >= -75
            ? "#f97316"
            : "#dc2626";
function parseScene(payload: any): Scene {
  const logical = payload?.data?.scene?.areas?.[0]?.logicalMap;
  if (!logical) throw new Error("RDS scene has no logical map");
  const points = (logical.advancedPoints || [])
    .map((p: any) => ({
      name: p.instanceName || "",
      x: Number(p.pos?.x),
      y: Number(p.pos?.y),
    }))
    .filter((p: any) => Number.isFinite(p.x) && Number.isFinite(p.y));
  const paths = (logical.advancedCurves || [])
    .map((p: any) => ({
      a: { x: Number(p.startPos?.pos?.x), y: Number(p.startPos?.pos?.y) },
      b: { x: Number(p.endPos?.pos?.x), y: Number(p.endPos?.pos?.y) },
    }))
    .filter((p: any) => [p.a.x, p.a.y, p.b.x, p.b.y].every(Number.isFinite));
  const all = [...points, ...paths.flatMap((p: any) => [p.a, p.b])];
  if (!all.length) throw new Error("RDS scene has no coordinates");
  return {
    md5: payload?.data?.md5 || payload?.data?.scene?.md5 || "unknown",
    bounds: {
      minX: Math.min(...all.map((p: any) => p.x)),
      minY: Math.min(...all.map((p: any) => p.y)),
      maxX: Math.max(...all.map((p: any) => p.x)),
      maxY: Math.max(...all.map((p: any) => p.y)),
    },
    paths,
    points,
  };
}
function parseRobots(payload: any): Robot[] {
  const data = payload?.data;
  return (data?.report || [])
    .map((item: any) => {
      const r = item.rbk_report || {};
      const speed = Math.hypot(Number(r.vx) || 0, Number(r.vy) || 0);
      return {
        name: item.uuid || item.vehicle_id || item.current_order?.vehicle || "",
        x: Number(r.x),
        y: Number(r.y),
        heading: Number.isFinite(Number(r.angle)) ? Number(r.angle) : undefined,
        speed,
        moving: speed > 0.01 || Math.abs(Number(r.w) || 0) > 0.01,
        connected: Number(item.connection_status) !== 0,
        mapVersion: data.scene_md5 || "unknown",
        timestamp: data.create_on || new Date().toISOString(),
        latency: Number.isFinite(Number(item.network_delay))
          ? Number(item.network_delay)
          : undefined,
      };
    })
    .filter(
      (r: Robot) => r.name && Number.isFinite(r.x) && Number.isFinite(r.y),
    );
}

export default function WifiHeatmapAdminPage() {
  const [connections, setConnections] = useState<Connection[]>([]),
    [plant, setPlant] = useState(""),
    [scene, setScene] = useState<Scene>(),
    [savedMaps, setSavedMaps] = useState<SavedMap[]>([]),
    [robots, setRobots] = useState<Robot[]>([]),
    [fleetNames, setFleetNames] = useState<string[]>([]),
    [wifi, setWifi] = useState<Wifi[]>([]),
    [selectedAmrs, setSelectedAmrs] = useState<string[]>([]),
    [status, setStatus] = useState("Load a configured plant to begin."),
    [recording, setRecording] = useState<{
      id: number;
      started: number;
      count: number;
      wifiCount: number;
    }>(),
    [metric, setMetric] = useState("rssi"),
    [aggregation, setAggregation] = useState("average"),
    [grid, setGrid] = useState(3),
    [minimum, setMinimum] = useState(3),
    [opacity, setOpacity] = useState(0.6),
    [layers, setLayers] = useState({
      heat: true,
      raw: true,
      route: true,
      path: true,
      events: true,
      labels: true,
      aps: true,
      unknown: true,
    }),
    [cells, setCells] = useState<Cell[]>([]),
    [raw, setRaw] = useState<Raw[]>([]),
    [route, setRoute] = useState<RoutePoint[]>([]),
    [apPins, setAPPins] = useState<APPin[]>([]),
    [placingAP, setPlacingAP] = useState<{ bssid: string; name: string }>(),
    [selected, setSelected] = useState<Cell>(),
    [sessions, setSessions] = useState<SurveySession[]>([]),
    [elapsed, setElapsed] = useState(0);
  const lastRef = useRef(new Map<string, number>()),
    recordingRef = useRef(recording),
    recordingSceneRef = useRef<Scene>(),
    mapFileRef = useRef<HTMLInputElement | null>(null),
    amrPickerRef = useRef<HTMLDetailsElement | null>(null),
    mapSvgRef = useRef<SVGSVGElement | null>(null);
  recordingRef.current = recording;
  useEffect(() => {
    fetch("/api/connections")
      .then((r) => r.json())
      .then((v: Connection[]) => {
        setConnections(v);
        if (v[0]) setPlant(v[0].plant);
      })
      .catch((e) => setStatus(String(e)));
  }, []);
  const saveMapChoice = useCallback(
    (nextScene: Scene, name: string, source: SavedMap["source"]) => {
      const key = `drishti-wifi-survey-maps:${plant}`;
      let current: SavedMap[] = [];
      try {
        current = JSON.parse(localStorage.getItem(key) || "[]");
      } catch {
        /* replace invalid cache */
      }
      const entry: SavedMap = {
        id: nextScene.md5,
        name,
        scene: nextScene,
        savedAt: new Date().toISOString(),
        source,
      };
      const next = [
        entry,
        ...current.filter((item) => item.id !== entry.id),
      ].slice(0, 20);
      localStorage.setItem(key, JSON.stringify(next));
      setSavedMaps(next);
      setScene(nextScene);
    },
    [plant],
  );
  const refreshLive = useCallback(async () => {
    if (!plant) throw new Error("Select a plant first.");
    setStatus(`Pulling ${plant} RDS core, scene, and Wi-Fi data...`);
    const [coreR, sceneR, wifiR] = await Promise.all([
      fetch(proxy(plant, "core")),
      fetch(proxy(plant, "scene")),
      fetch(`/api/discovery?plant=${encodeURIComponent(plant)}`),
    ]);
    if (!coreR.ok || !sceneR.ok || !wifiR.ok)
      throw new Error(
        `RDS pull failed (core ${coreR.status}, scene ${sceneR.status}, Wi-Fi ${wifiR.status})`,
      );
    const [core, sceneData, wifiData] = await Promise.all([
      coreR.json(),
      sceneR.json(),
      wifiR.json(),
    ]);
    const nextRobots = parseRobots(core);
    const nextScene = {
      ...parseScene(sceneData),
      mapVersion: String(core?.data?.scene_md5 || "unknown"),
    };
    const nextWifi: Wifi[] = wifiData.items || [];
    setRobots(nextRobots);
    saveMapChoice(
      nextScene,
      `${plant} RDS ${nextScene.md5.slice(0, 10)}`,
      "RDS",
    );
    setWifi(nextWifi);
    setStatus(
      `Pulled ${plant} RDS map ${nextScene.md5}; Core version ${nextScene.mapVersion}; ${nextRobots.length} positioned AMRs.`,
    );
    return { robots: nextRobots, scene: nextScene, wifi: nextWifi };
  }, [plant, saveMapChoice]);
  const refreshTelemetry = useCallback(async () => {
    const [coreR, wifiR] = await Promise.all([
      fetch(proxy(plant, "core")),
      fetch(`/api/discovery?plant=${encodeURIComponent(plant)}`),
    ]);
    if (!coreR.ok || !wifiR.ok)
      throw new Error(
        `Live telemetry failed (core ${coreR.status}, Wi-Fi ${wifiR.status})`,
      );
    const [core, wifiData] = await Promise.all([coreR.json(), wifiR.json()]);
    const nextRobots = parseRobots(core),
      nextWifi: Wifi[] = wifiData.items || [];
    setRobots(nextRobots);
    setWifi(nextWifi);
    return { robots: nextRobots, wifi: nextWifi };
  }, [plant]);
  useEffect(() => {
    if (!plant) return;
    let maps: SavedMap[] = [];
    try {
      maps = JSON.parse(
        localStorage.getItem(`drishti-wifi-survey-maps:${plant}`) || "[]",
      );
    } catch {
      /* ignore invalid cache */
    }
    setSavedMaps(maps);
    setScene(maps[0]?.scene);
    setRobots([]);
    setWifi([]);
    setStatus(
      maps.length
        ? `Loaded saved ${plant} map. Use Pull RDS Map for current positions.`
        : `No saved map for ${plant}. Pull RDS Map or upload an RDS scene JSON.`,
    );
  }, [plant]);
  useEffect(() => {
    if (!plant) return;
    setSelectedAmrs([]);
    fetch(`/api/amr/fleet?plant=${encodeURIComponent(plant)}`)
      .then((r) =>
        r.ok ? r.json() : Promise.reject(new Error("Fleet unavailable")),
      )
      .then((items: { name: string }[]) =>
        setFleetNames(
          [...new Set(items.map((item) => item.name).filter(Boolean))].sort(),
        ),
      )
      .catch(() => setFleetNames([]));
  }, [plant]);
  const availableAmrs = useMemo(
    () =>
      [
        ...new Set([
          ...fleetNames,
          ...robots.map((r) => r.name),
          ...wifi.map((w) => w.amr),
        ]),
      ].sort(),
    [fleetNames, robots, wifi],
  );
  const uploadMap = async (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) return;
    try {
      const payload = JSON.parse(await file.text());
      const nextScene = parseScene(payload);
      saveMapChoice(nextScene, file.name, "Upload");
      setStatus(
        `Uploaded ${file.name} for ${plant}; map ${nextScene.md5}. Pull RDS Map to load current AMR positions.`,
      );
    } catch (error) {
      setStatus(
        `Map upload failed: ${error instanceof Error ? error.message : String(error)}. Upload an exported RDS scene JSON file.`,
      );
    }
  };
  const apStorageKey = scene
    ? `drishti-wifi-survey-aps:${plant}:${scene.md5}`
    : "";
  useEffect(() => {
    if (!apStorageKey) {
      setAPPins([]);
      return;
    }
    try {
      setAPPins(JSON.parse(localStorage.getItem(apStorageKey) || "[]"));
    } catch {
      setAPPins([]);
    }
  }, [apStorageKey]);
  const storeAPPins = (pins: APPin[]) => {
    setAPPins(pins);
    if (apStorageKey) localStorage.setItem(apStorageKey, JSON.stringify(pins));
  };
  const estimateAPs = () => {
    const groups = new Map<string, Raw[]>();
    raw
      .filter((point) => point.bssid && point.bssid !== "unknown-ap")
      .forEach((point) =>
        groups.set(point.bssid, (groups.get(point.bssid) || []).concat(point)),
      );
    const estimates: APPin[] = [];
    groups.forEach((samples, bssid) => {
      if (samples.length < 3) return;
      const strongest = [...samples]
        .sort((a, b) => b.rssi_dbm - a.rssi_dbm)
        .slice(0, 30);
      let weight = 0,
        x = 0,
        y = 0;
      strongest.forEach((point) => {
        const w = Math.max(1, point.rssi_dbm + 100) ** 2;
        weight += w;
        x += point.x * w;
        y += point.y * w;
      });
      const count = samples.length;
      estimates.push({
        bssid,
        name: bssid,
        x: x / weight,
        y: y / weight,
        source: "Estimated",
        confidence: count >= 30 ? "high" : count >= 10 ? "medium" : "low",
        samples: count,
        updatedAt: new Date().toISOString(),
      });
    });
    const manual = apPins.filter((pin) => pin.source === "Manual");
    storeAPPins([
      ...manual,
      ...estimates.filter(
        (pin) =>
          !manual.some(
            (saved) => saved.bssid.toLowerCase() === pin.bssid.toLowerCase(),
          ),
      ),
    ]);
    setStatus(
      estimates.length
        ? `Estimated ${estimates.length} AP location${estimates.length === 1 ? "" : "s"} from recorded BSSID/RSSI samples. Review before treating them as physical locations.`
        : "At least 3 recorded samples with a real BSSID are required per AP.",
    );
  };
  const beginPlaceAP = () => {
    if (!scene) {
      setStatus("Select a map before placing an AP.");
      return;
    }
    const bssid = window
      .prompt("Enter the AP BSSID (preferred) or unique AP identifier:")
      ?.trim();
    if (!bssid) return;
    const name =
      window.prompt("Enter an AP display name:", bssid)?.trim() || bssid;
    setPlacingAP({ bssid, name });
    setStatus(
      `Click the map where ${name} is physically installed. Press Escape or click Place AP again to cancel.`,
    );
  };
  const placeAPOnMap = (event: React.MouseEvent<SVGSVGElement>) => {
    if (!placingAP || !mapSvgRef.current) return;
    const point = mapSvgRef.current.createSVGPoint();
    point.x = event.clientX;
    point.y = event.clientY;
    const matrix = mapSvgRef.current.getScreenCTM();
    if (!matrix) return;
    const mapped = point.matrixTransform(matrix.inverse());
    const pin: APPin = {
      ...placingAP,
      x: mapped.x,
      y: -mapped.y,
      source: "Manual",
      confidence: "confirmed",
      samples: 0,
      updatedAt: new Date().toISOString(),
    };
    storeAPPins([
      pin,
      ...apPins.filter(
        (item) => item.bssid.toLowerCase() !== pin.bssid.toLowerCase(),
      ),
    ]);
    setPlacingAP(undefined);
    setStatus(
      `Placed ${pin.name} at RDS coordinates ${pin.x.toFixed(2)}, ${pin.y.toFixed(2)}.`,
    );
  };
  const effectiveMapVersion =
    scene?.mapVersion ||
    robots.find((r) => r.mapVersion && r.mapVersion !== "unknown")
      ?.mapVersion ||
    scene?.md5 ||
    "";
  const mismatch = useMemo(() => {
    if (!scene) return "No active RDS map";
    const bad =
      scene.mapVersion && scene.mapVersion !== "unknown"
        ? robots.find(
            (r) =>
              r.mapVersion !== "unknown" && r.mapVersion !== scene.mapVersion,
          )
        : undefined;
    if (bad)
      return `Map mismatch: ${bad.name} reports Core version ${bad.mapVersion}, selected map expects ${scene.mapVersion}`;
    const wrong = wifi.find(
      (w) => w.plant.toLowerCase() !== plant.toLowerCase(),
    );
    return wrong ? `Plant mismatch: Wi-Fi source reports ${wrong.plant}` : "";
  }, [scene, robots, wifi, plant]);
  const buildPoint = (
    name: string,
    session?: number,
    sourceRobots = robots,
    sourceWifi = wifi,
    sourceScene = scene,
  ): WifiHeatmapPointInput => {
    if (!sourceScene) throw new Error("No active map");
    const r = sourceRobots.find(
      (v) => v.name.toLowerCase() === name.toLowerCase(),
    );
    const w = sourceWifi.find(
      (v) => v.amr.toLowerCase() === name.toLowerCase(),
    );
    if (!r) throw new Error(`Missing position for ${name}`);
    if (!w || typeof w.rssi_dbm !== "number")
      throw new Error(`Missing Wi-Fi measurement for ${name}`);
    if (
      sourceScene.mapVersion &&
      sourceScene.mapVersion !== "unknown" &&
      r.mapVersion !== "unknown" &&
      r.mapVersion !== sourceScene.mapVersion
    )
      throw new Error(`Map mismatch for ${name}`);
    const mapVersion =
      sourceScene.mapVersion || r.mapVersion || sourceScene.md5;
    return {
      session_id: session,
      plant_id: plant,
      source_plant: w.plant,
      map_id: sourceScene.md5,
      map_version: mapVersion,
      amr_id: r.name,
      wifi_amr_id: w.amr,
      timestamp: new Date().toISOString(),
      x: r.x,
      y: r.y,
      heading: r.heading,
      moving: r.moving,
      speed: r.speed,
      rssi_dbm: w.rssi_dbm,
      snr_db: w.snr_db,
      bssid: w.ap_name || "unknown-ap",
      channel: parseInt(w.channel) || 0,
      band: w.band || "unknown",
      connected: r.connected,
      latency_ms: r.latency,
      source_id: w.source || "discovery",
      position_timestamp: r.timestamp,
      wifi_timestamp: w.last_seen || new Date().toISOString(),
    };
  };
  const reloadHeat = useCallback(async () => {
    if (!scene || !plant || !effectiveMapVersion) return;
    const data = await queryWifiHeatmap({
      plant,
      map: scene.md5,
      map_version: effectiveMapVersion,
      metric,
      aggregation_type: aggregation,
      grid_size: grid,
      ...(selectedAmrs.length ? { amr: selectedAmrs.join(",") } : {}),
    });
    setCells(data.cells || []);
    setRaw(data.points || []);
    setRoute(data.route_points || []);
  }, [
    scene,
    plant,
    effectiveMapVersion,
    metric,
    aggregation,
    grid,
    selectedAmrs,
  ]);
  const reloadSessions = useCallback(async () => {
    const data = await listWifiHeatmapSessions();
    setSessions(Array.isArray(data) ? data : []);
  }, []);
  useEffect(() => {
    void reloadHeat().catch(() => undefined);
  }, [reloadHeat]);
  useEffect(() => {
    void reloadSessions().catch(() => undefined);
  }, [reloadSessions]);
  const saveOne = async () => {
    try {
      if (mismatch) throw new Error(mismatch);
      if (!selectedAmrs.length) throw new Error("Pick one or more AMRs first.");
      let saved = 0,
        duplicates = 0;
      for (const target of selectedAmrs) {
        const result = await saveWifiHeatmapPoint(buildPoint(target));
        if (result.saved) saved++;
        else if (result.duplicate) duplicates++;
      }
      setStatus(
        `Saved ${saved} synchronized point${saved === 1 ? "" : "s"} for ${selectedAmrs.length} selected AMR${selectedAmrs.length === 1 ? "" : "s"}${duplicates ? `; ${duplicates} unchanged skipped` : ""}.`,
      );
      await reloadHeat();
    } catch (e: any) {
      setStatus(e.response?.data?.error || e.message);
    }
  };
  const collect = useCallback(async () => {
    const active = recordingRef.current,
      activeScene = recordingSceneRef.current;
    if (!active || !activeScene) return;
    try {
      const live = await refreshTelemetry();
      // The RDS controller and Wi-Fi source can use different clocks. Since
      // these responses are fetched together, use their shared collection time.
      const capturedAt = new Date().toISOString();
      let routeAdded = 0,
        wifiAdded = 0;
      const notices: string[] = [];
      for (const name of selectedAmrs) {
        const r = live.robots.find((v) => v.name === name);
        if (!r) {
          notices.push(`Missing RDS position for ${name}`);
          continue;
        }
        const interval = (r.moving ? 2 : 10) * 1000;
        if (Date.now() - (lastRef.current.get(name) || 0) < interval) continue;
        const nearest = [...activeScene.points].sort(
          (a, b) =>
            Math.hypot(a.x - r.x, a.y - r.y) - Math.hypot(b.x - r.x, b.y - r.y),
        )[0];
        try {
          const result = await saveWifiSurveyRoutePoint({
            session_id: active.id,
            plant_id: plant,
            map_id: activeScene.md5,
            map_version:
              activeScene.mapVersion || r.mapVersion || activeScene.md5,
            amr_id: r.name,
            timestamp: r.timestamp,
            x: r.x,
            y: r.y,
            heading: r.heading,
            moving: r.moving,
            speed: r.speed,
            connected: r.connected,
            nearest_location: nearest?.name || "",
          });
          lastRef.current.set(name, Date.now());
          if (result.saved) routeAdded++;
        } catch (error: any) {
          notices.push(
            error.response?.data?.error || error.message || String(error),
          );
          continue;
        }
        try {
          const result = await saveWifiHeatmapPoint(
            {
              ...buildPoint(name, active.id, live.robots, live.wifi, activeScene),
              timestamp: capturedAt,
              position_timestamp: capturedAt,
              wifi_timestamp: capturedAt,
            },
          );
          if (result.saved) wifiAdded++;
        } catch (error: any) {
          notices.push(
            `Route saved; Wi-Fi skipped for ${name}: ${error.response?.data?.error || error.message || String(error)}`,
          );
        }
      }
      if (routeAdded || wifiAdded)
        setRecording((v) =>
          v
            ? {
                ...v,
                count: v.count + routeAdded,
                wifiCount: v.wifiCount + wifiAdded,
              }
            : v,
        );
      setStatus(
        notices.length
          ? `Recording session ${active.id}: ${notices[0]}`
          : `Recording session ${active.id}: ${routeAdded} route / ${wifiAdded} Wi-Fi point${wifiAdded === 1 ? "" : "s"} added.`,
      );
      await reloadHeat();
    } catch (error: any) {
      setStatus(
        `Recording session ${active.id}: ${error.response?.data?.error || error.message || String(error)}`,
      );
    }
  }, [selectedAmrs, refreshTelemetry, reloadHeat, plant]);
  useEffect(() => {
    if (!recording) return;
    const id = window.setInterval(() => void collect(), 1000);
    void collect();
    return () => clearInterval(id);
  }, [recording?.id, collect]);
  useEffect(() => {
    if (!recording) return;
    const id = window.setInterval(
      () => setElapsed(Math.floor((Date.now() - recording.started) / 1000)),
      1000,
    );
    return () => clearInterval(id);
  }, [recording]);
  const start = async () => {
    try {
      if (!selectedAmrs.length) throw new Error("Pick one or more AMRs first.");
      const live = await refreshLive();
      const mapVersion = live.scene.mapVersion || live.scene.md5;
      const s = await startWifiHeatmapSession({
        plant_id: plant,
        map_id: live.scene.md5,
        map_version: mapVersion,
        amr_id: selectedAmrs.join(","),
        moving_interval_seconds: 2,
        stationary_interval_seconds: 10,
        timestamp_tolerance_seconds: 15,
      });
      recordingSceneRef.current = live.scene;
      lastRef.current.clear();
      setRecording({ id: s.id, started: Date.now(), count: 0, wifiCount: 0 });
      setStatus(
        `Recording session ${s.id} started for ${selectedAmrs.length} AMR${selectedAmrs.length === 1 ? "" : "s"}. Route positions record even when Wi-Fi is unavailable.`,
      );
    } catch (e: any) {
      setStatus(e.response?.data?.error || e.message);
    }
  };
  const stop = async () => {
    if (!recording) return;
    await stopWifiHeatmapSession(recording.id);
    setStatus(
      `Session ${recording.id} stopped with ${recording.count} route and ${recording.wifiCount} Wi-Fi points.`,
    );
    setRecording(undefined);
    recordingSceneRef.current = undefined;
    await Promise.all([reloadHeat(), reloadSessions()]);
  };
  const b = scene?.bounds,
    pad = 2,
    w = b ? Math.max(1, b.maxX - b.minX) : 1,
    h = b ? Math.max(1, b.maxY - b.minY) : 1;
  const eligible = cells.filter((c) => c.measurement_count >= minimum);
  const displayValue = (c: Cell) =>
    aggregation === "minimum"
      ? c.minimum
      : aggregation === "maximum"
        ? c.maximum
        : aggregation === "worst"
          ? c.worst
          : c.average;
  const routeGroups = [
    ...route
      .reduce((groups, point) => {
        const key = `${point.session_id}:${point.amr_id}`;
        groups.set(key, [...(groups.get(key) || []), point]);
        return groups;
      }, new Map<string, RoutePoint[]>())
      .entries(),
  ];
  const liveRobotStatus = (robot: Robot) => {
    if (!robot.connected) return { label: "Disconnected", fill: "#dc2626" };
    const reading = wifi.find(
      (item) => item.amr.toLowerCase() === robot.name.toLowerCase(),
    );
    if (typeof reading?.rssi_dbm !== "number")
      return { label: "Wi-Fi unknown", fill: "#64748b" };
    return reading.rssi_dbm < -70
      ? { label: `Weak ${reading.rssi_dbm} dBm`, fill: "#f97316" }
      : { label: `Good ${reading.rssi_dbm} dBm`, fill: "#16a34a" };
  };
  return (
    <div className="flex-1 overflow-auto bg-gray-950 text-gray-100 p-5 space-y-4">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-bold flex items-center gap-2">
            <MapIcon />
            Wi-Fi Survey Heatmap
          </h1>
          <p className="text-sm text-gray-400">
            Admin-only synchronized RDS position and Wi-Fi history
          </p>
        </div>
        <div className="flex gap-2">
          <button
            className="px-3 py-2 rounded bg-blue-600 flex gap-2"
            onClick={saveOne}
            disabled={!!recording}
          >
            <Save size={16} />
            Save Scan Point
          </button>
          {recording ? (
            <button
              className="px-3 py-2 rounded bg-red-700 flex gap-2"
              onClick={stop}
            >
              <CircleStop size={16} />
              Stop ({elapsed}s / {recording.count} route / {recording.wifiCount}{" "}
              Wi-Fi)
            </button>
          ) : (
            <button
              className="px-3 py-2 rounded bg-emerald-700 flex gap-2"
              onClick={start}
            >
              <Play size={16} />
              Start Scan Recording
            </button>
          )}
        </div>
      </header>
      {(mismatch || status) && (
        <div
          className={`rounded border p-3 text-sm flex gap-2 ${mismatch ? "border-amber-600 bg-amber-950/40 text-amber-200" : "border-gray-700 bg-gray-900 text-gray-300"}`}
        >
          {mismatch && <AlertTriangle size={18} />}
          <span>{mismatch || status}</span>
        </div>
      )}
      <section className="bg-gray-900 border border-gray-800 rounded p-4 space-y-3">
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
          <label className="text-xs text-gray-400">
            Plant
            <select
              className="block w-full mt-1 bg-gray-950 p-2 rounded"
              value={plant}
              onChange={(e) => setPlant(e.target.value)}
            >
              {connections.map((c) => (
                <option key={c.plant}>{c.plant}</option>
              ))}
            </select>
          </label>
          <label className="text-xs text-gray-400">
            Pick Map
            <select
              className="block w-full mt-1 bg-gray-950 p-2 rounded"
              value={scene?.md5 || ""}
              onChange={(e) => {
                const choice = savedMaps.find(
                  (item) => item.id === e.target.value,
                );
                if (choice) {
                  setScene(choice.scene);
                  setStatus(`Selected ${choice.name}.`);
                }
              }}
            >
              <option value="">No map selected</option>
              {savedMaps.map((item) => (
                <option key={item.id} value={item.id}>
                  {item.name} ({item.source})
                </option>
              ))}
            </select>
          </label>
          <button
            type="button"
            className="mt-5 p-2 rounded bg-cyan-700 flex items-center justify-center gap-2 text-sm"
            onClick={() =>
              void refreshLive().catch((e) => setStatus(e.message))
            }
          >
            <RefreshCw size={16} />
            Pull RDS Map
          </button>
          <button
            type="button"
            className="mt-5 p-2 rounded bg-slate-700 flex items-center justify-center gap-2 text-sm"
            onClick={() => mapFileRef.current?.click()}
          >
            <Upload size={16} />
            Upload Plant Map
          </button>
          <input
            ref={mapFileRef}
            type="file"
            accept=".json,application/json"
            className="hidden"
            onChange={uploadMap}
          />
        </div>
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
          <details
            ref={amrPickerRef}
            className="relative text-xs text-gray-400"
          >
            <summary className="mt-5 bg-gray-950 p-2 rounded cursor-pointer list-none">
              {selectedAmrs.length
                ? `${selectedAmrs.length} AMR${selectedAmrs.length === 1 ? "" : "s"} selected`
                : "Pick AMRs..."}
            </summary>
            <div className="absolute z-20 mt-1 w-full min-w-64 max-h-72 overflow-auto rounded border border-gray-700 bg-gray-950 p-3 shadow-xl">
              <div className="flex gap-2 pb-2 border-b border-gray-800">
                <button
                  type="button"
                  className="text-blue-400"
                  onClick={() => {
                    setSelectedAmrs(availableAmrs);
                    if (amrPickerRef.current) amrPickerRef.current.open = false;
                  }}
                >
                  Select all
                </button>
                <button
                  type="button"
                  className="text-gray-400"
                  onClick={() => {
                    setSelectedAmrs([]);
                    if (amrPickerRef.current) amrPickerRef.current.open = false;
                  }}
                >
                  Clear
                </button>
              </div>
              {availableAmrs.length ? (
                availableAmrs.map((name) => (
                  <label
                    key={name}
                    className="flex items-center gap-2 py-1.5 text-gray-200"
                  >
                    <input
                      type="checkbox"
                      checked={selectedAmrs.includes(name)}
                      onChange={() => {
                        setSelectedAmrs((current) =>
                          current.includes(name)
                            ? current.filter((v) => v !== name)
                            : [...current, name],
                        );
                        window.setTimeout(() => {
                          if (amrPickerRef.current)
                            amrPickerRef.current.open = false;
                        }, 0);
                      }}
                    />
                    <span>{name}</span>
                  </label>
                ))
              ) : (
                <p className="py-3 text-amber-300">
                  No AMRs returned for this plant.
                </p>
              )}
            </div>
          </details>
          <label className="text-xs text-gray-400">
            Metric
            <select
              className="block w-full mt-1 bg-gray-950 p-2 rounded"
              value={metric}
              onChange={(e) => setMetric(e.target.value)}
            >
              <option value="rssi">RSSI</option>
              <option value="snr">SNR</option>
              <option value="disconnect">Disconnect count</option>
              <option value="roaming">Roaming count</option>
            </select>
          </label>
          <label className="text-xs text-gray-400">
            Aggregation
            <select
              className="block w-full mt-1 bg-gray-950 p-2 rounded"
              value={aggregation}
              onChange={(e) => setAggregation(e.target.value)}
            >
              <option>average</option>
              <option>worst</option>
              <option>minimum</option>
              <option>maximum</option>
            </select>
          </label>
          <label className="text-xs text-gray-400">
            Grid / min samples
            <input
              className="block w-full mt-1 bg-gray-950 p-2 rounded"
              value={`${grid},${minimum}`}
              onChange={(e) => {
                const [a, b] = e.target.value.split(",").map(Number);
                if (a > 0) setGrid(a);
                if (b > 0) setMinimum(b);
              }}
            />
          </label>
        </div>
      </section>
      <div className="flex flex-wrap items-center gap-3 text-xs">
        {Object.entries(layers).map(([k, v]) => (
          <label key={k} className="flex gap-1">
            <input
              type="checkbox"
              checked={v}
              onChange={() => setLayers((x) => ({ ...x, [k]: !v }))}
            />
            {k}
          </label>
        ))}
        <label>
          opacity{" "}
          <input
            type="range"
            min=".1"
            max="1"
            step=".1"
            value={opacity}
            onChange={(e) => setOpacity(Number(e.target.value))}
          />
        </label>
        <button
          type="button"
          className="rounded bg-violet-700 px-3 py-1.5"
          onClick={estimateAPs}
        >
          Estimate AP Locations
        </button>
        <button
          type="button"
          className={`rounded px-3 py-1.5 ${placingAP ? "bg-amber-600" : "bg-sky-700"}`}
          onClick={() => (placingAP ? setPlacingAP(undefined) : beginPlaceAP())}
        >
          {placingAP ? "Cancel AP Placement" : "Place AP Manually"}
        </button>
        {apPins.length > 0 && (
          <span className="text-cyan-300">
            {apPins.length} AP pin{apPins.length === 1 ? "" : "s"}
          </span>
        )}
      </div>
      <section
        className={`relative bg-slate-900 border rounded min-h-[560px] overflow-hidden ${placingAP ? "border-amber-500 cursor-crosshair" : "border-slate-700"}`}
      >
        {scene ? (
          <svg
            ref={mapSvgRef}
            className="w-full h-[650px]"
            viewBox={`${b!.minX - pad} ${-b!.maxY - pad} ${w + pad * 2} ${h + pad * 2}`}
            onClick={placeAPOnMap}
          >
            <defs>
              <filter id="smooth">
                <feGaussianBlur stdDeviation={grid * 0.7} />
              </filter>
            </defs>
            {layers.path && (
              <g stroke="#334155" strokeWidth=".22">
                {scene.paths.map((p, i) => (
                  <line key={i} x1={p.a.x} y1={-p.a.y} x2={p.b.x} y2={-p.b.y} />
                ))}
              </g>
            )}
            {layers.route && (
              <g>
                {routeGroups.map(([key, points]) => {
                  const latest = points[points.length - 1];
                  return (
                    <g key={key}>
                      <polyline
                        points={points
                          .map((point) => `${point.x},${-point.y}`)
                          .join(" ")}
                        fill="none"
                        stroke="#22d3ee"
                        strokeWidth=".38"
                        strokeLinejoin="round"
                        strokeLinecap="round"
                      />
                      {latest && (
                        <circle
                          cx={latest.x}
                          cy={-latest.y}
                          r=".55"
                          fill="#22d3ee"
                        >
                          <title>
                            {latest.amr_id} route · {points.length} points ·
                            nearest {latest.nearest_location || "unknown"}
                          </title>
                        </circle>
                      )}
                    </g>
                  );
                })}
              </g>
            )}
            {layers.unknown &&
              cells
                .filter((c) => c.measurement_count < minimum)
                .map((c, i) => (
                  <rect
                    key={`u${i}`}
                    x={c.x}
                    y={-c.y - grid}
                    width={grid}
                    height={grid}
                    fill="#64748b"
                    opacity=".32"
                  />
                ))}
            {layers.heat && (
              <g filter="url(#smooth)" opacity={opacity}>
                {eligible.map((c, i) => (
                  <rect
                    key={i}
                    x={c.x}
                    y={-c.y - grid}
                    width={grid}
                    height={grid}
                    rx={grid / 2}
                    fill={
                      metric === "rssi"
                        ? color(displayValue(c))
                        : metric === "snr"
                          ? color(-90 + displayValue(c))
                          : displayValue(c) > 0
                            ? "#dc2626"
                            : "#16a34a"
                    }
                    onClick={() => setSelected(c)}
                  />
                ))}
              </g>
            )}
            {layers.raw &&
              raw.map((p, i) => (
                <circle
                  key={i}
                  cx={p.x}
                  cy={-p.y}
                  r=".45"
                  fill={color(p.rssi_dbm)}
                  stroke="#020617"
                  strokeWidth=".12"
                >
                  <title>
                    {p.amr_id} {p.rssi_dbm} dBm
                  </title>
                </circle>
              ))}
            {layers.events &&
              raw
                .filter((p) => p.disconnect_event || p.roam_event)
                .map((p, i) =>
                  p.disconnect_event ? (
                    <g key={`e${i}`} transform={`translate(${p.x} ${-p.y})`}>
                      <line
                        x1="-.7"
                        y1="-.7"
                        x2=".7"
                        y2=".7"
                        stroke="#ef4444"
                        strokeWidth=".3"
                      />
                      <line
                        x1="-.7"
                        y1=".7"
                        x2=".7"
                        y2="-.7"
                        stroke="#ef4444"
                        strokeWidth=".3"
                      />
                    </g>
                  ) : (
                    <circle
                      key={`e${i}`}
                      cx={p.x}
                      cy={-p.y}
                      r=".9"
                      fill="none"
                      stroke="#a855f7"
                      strokeWidth=".3"
                    />
                  ),
                )}
            {layers.aps &&
              scene.points
                .filter((p) => /^AP/i.test(p.name))
                .map((p) => (
                  <circle
                    key={p.name}
                    cx={p.x}
                    cy={-p.y}
                    r=".65"
                    fill="#38bdf8"
                  >
                    <title>{p.name} (RDS map point)</title>
                  </circle>
                ))}
            {layers.aps &&
              apPins.map((pin) => (
                <g key={pin.bssid} transform={`translate(${pin.x} ${-pin.y})`}>
                  <circle
                    r="1.05"
                    fill={pin.source === "Manual" ? "#22d3ee" : "#a855f7"}
                    stroke="#f8fafc"
                    strokeWidth=".18"
                  />
                  <path
                    d="M -.38 -.18 L 0 -.55 L .38 -.18 M -.55 .08 Q 0 -.45 .55 .08 M 0 -.55 L 0 .55"
                    fill="none"
                    stroke="#020617"
                    strokeWidth=".13"
                  />
                  <text x="1.25" y=".35" fontSize="1.45" fill="#e2e8f0">
                    {pin.name}
                  </text>
                  <title>
                    {pin.name} · {pin.bssid} · {pin.source} · {pin.confidence}{" "}
                    confidence · {pin.samples} samples
                  </title>
                </g>
              ))}
            {layers.labels &&
              scene.points
                .filter((p) => /^AP|^PP/i.test(p.name))
                .map((p) => (
                  <text
                    key={p.name}
                    x={p.x + 0.5}
                    y={-p.y - 0.5}
                    fontSize="1.5"
                    fill="#94a3b8"
                  >
                    {p.name}
                  </text>
                ))}
            {robots.map((robot) => {
              const signal = liveRobotStatus(robot);
              return (
                <g key={`robot-${robot.name}`} transform={`translate(${robot.x} ${-robot.y})`}>
                  <circle r=".82" fill={signal.fill} stroke="#f8fafc" strokeWidth=".2" />
                  <text x="1.05" y=".4" fontSize="1.35" fill="#f8fafc">{robot.name}</text>
                  <title>{robot.name} · {signal.label}</title>
                </g>
              );
            })}
          </svg>
        ) : (
          <div className="p-10 text-gray-500">No RDS scene loaded.</div>
        )}
        {placingAP && (
          <div className="absolute left-3 top-3 rounded bg-amber-950/95 border border-amber-500 px-3 py-2 text-sm text-amber-100">
            Click the physical location of {placingAP.name}
          </div>
        )}
        {selected && (
          <aside className="absolute right-3 top-3 w-72 rounded bg-gray-950/95 border border-gray-700 p-3 text-xs space-y-1">
            <strong>
              Grid cell ({selected.x.toFixed(2)}, {selected.y.toFixed(2)})
            </strong>
            <div>
              Average {selected.average.toFixed(1)} / worst{" "}
              {selected.worst.toFixed(1)}
            </div>
            <div>
              Average SNR {selected.average_snr?.toFixed(1) ?? "unknown"} dB
            </div>
            <div>
              {selected.measurement_count} samples · {selected.confidence_level}{" "}
              confidence
            </div>
            <div>AMRs {selected.contributing_amrs.join(", ") || "none"}</div>
            <div>AP {selected.most_common_bssid || "unknown"}</div>
            <div>
              Disconnects {selected.disconnect_count} · roams{" "}
              {selected.roam_count}
            </div>
            <div>
              {new Date(selected.first_timestamp).toLocaleString()} –{" "}
              {new Date(selected.last_timestamp).toLocaleString()}
            </div>
          </aside>
        )}
      </section>
      <section className="bg-gray-900 border border-gray-800 rounded p-4 space-y-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div><h2 className="font-semibold">Survey Reports</h2><p className="text-xs text-gray-400">Stopped scans remain here; their saved points stay visible on the map above.</p></div>
          <button type="button" className="rounded bg-slate-700 px-3 py-1.5 text-xs" onClick={() => void reloadSessions()}>Refresh reports</button>
        </div>
        <div className="overflow-auto">
          <table className="w-full text-xs text-left">
            <thead className="text-gray-400"><tr><th className="p-2">Session</th><th className="p-2">AMRs</th><th className="p-2">Started</th><th className="p-2">Stopped</th><th className="p-2">Route points</th><th className="p-2">Wi-Fi samples</th><th className="p-2">Status</th></tr></thead>
            <tbody>
              {sessions.filter((item) => item.plant_id === plant).map((item) => (
                <tr key={item.id} className="border-t border-gray-800"><td className="p-2">#{item.id}</td><td className="p-2">{item.amr_id}</td><td className="p-2">{new Date(item.started_at).toLocaleString()}</td><td className="p-2">{item.stopped_at ? new Date(item.stopped_at).toLocaleString() : "—"}</td><td className="p-2">{item.route_count}</td><td className="p-2">{item.sample_count}</td><td className="p-2 capitalize">{item.status}</td></tr>
              ))}
              {!sessions.some((item) => item.plant_id === plant) && <tr><td className="p-3 text-gray-400" colSpan={7}>No survey reports for this plant yet.</td></tr>}
            </tbody>
          </table>
        </div>
      </section>
      <footer className="text-xs text-gray-500 flex flex-wrap gap-4">
        <span className="text-green-500">● Good connection</span>
        <span className="text-orange-500">● Weak (&lt; -70 dBm)</span>
        <span className="text-red-500">● Disconnected</span>
        <span>Green ≥ -55</span>
        <span>Lime -56…-65</span>
        <span>Yellow -66…-70</span>
        <span>Orange -71…-75</span>
        <span>Red &lt; -75</span>
        <span className="flex gap-1">
          <WifiOff size={13} />
          Gray = unknown/low samples
        </span>
      </footer>
    </div>
  );
}
