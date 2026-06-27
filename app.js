const STORAGE_KEY = "drishti-amr-health-v1";
const RDS_PLANT_HOSTS = { Shelbyville: "10.205.22.12", Springfield: "10.222.10.76", Hopkinsville: "10.216.4.59" };
const topics = [
  "Robot offline / disconnect", "Application crash", "Ubuntu server reboot", "Ubuntu server shutdown", "Ubuntu log gap",
  "VM stopped", "VM started", "VM reboot", "VM killed by OOM", "Host memory exhaustion", "Swap full",
  "Proxmox host reboot", "Proxmox host shutdown", "Backup job", "Backup found VM stopped", "HA action",
  "Disk / SMART issue", "Disk error", "Network / DHCP failure", "RDS Core issue", "RDS map update",
  "RDS model / MD5", "Charge command", "Battery error", "Battery status", "Dock command", "GoTarget station",
  "Settings reset", "Settings defaulted", "RDS upgrade", "RDS Core activation", "RDS scene error",
  "Admin evidence search", "Template / code reference"
];

const seed = {
  thresholds: { good: -60, weak: -70, poor: -80, interval: 60 },
  plants: [
    { id: "shelbyville", name: "Shelbyville", fleetManager: "shv-fleet-ubuntu01", roboshop: "shv-roboshop-core:9443", logPath: "/var/log/roboshop" },
    { id: "springfield", name: "Springfield", fleetManager: "spf-fleet-ubuntu01", roboshop: "spf-roboshop-core:9443", logPath: "/data/rds/logs" },
    { id: "hopkinsville", name: "Hopkinsville", fleetManager: "hkv-fleet-ubuntu01", roboshop: "hkv-roboshop-core:9443", logPath: "/var/log/fleet-manager" }
  ],
  amrs: [
    { id: "shv-amr-01", name: "SHV-AMR-01", plant: "Shelbyville", ip: "10.24.12.41", status: "Online", reconnects: 4, disconnects: 1, offline: "0m", worstDrop: "Assembly north aisle", rssi: -58, ap: "AP-SHV-A12", ssid: "AMR-Fleet", channel: "44", band: "5 GHz" },
    { id: "shv-amr-02", name: "SHV-AMR-02", plant: "Shelbyville", ip: "10.24.12.42", status: "Disconnected", reconnects: 18, disconnects: 7, offline: "14m", worstDrop: "Dock door 3", rssi: -76, ap: "AP-SHV-D03", ssid: "AMR-Fleet", channel: "149", band: "5 GHz" },
    { id: "spf-amr-07", name: "SPF-AMR-07", plant: "Springfield", ip: "10.30.8.77", status: "Online", reconnects: 8, disconnects: 2, offline: "0m", worstDrop: "Outbound lane", rssi: -64, ap: "AP-SPF-O09", ssid: "AMR-Fleet", channel: "11", band: "2.4 GHz" },
    { id: "hkv-amr-03", name: "HKV-AMR-03", plant: "Hopkinsville", ip: "10.40.9.23", status: "Offline", reconnects: 22, disconnects: 11, offline: "42m", worstDrop: "Charge station west", rssi: -83, ap: "AP-HKV-C02", ssid: "AMR-Fleet", channel: "157", band: "5 GHz" },
    { id: "hkv-amr-04", name: "HKV-AMR-04", plant: "Hopkinsville", ip: "10.40.9.24", status: "Unknown", reconnects: 1, disconnects: 0, offline: "unknown", worstDrop: "No recent drop", rssi: -59, ap: "AP-HKV-F01", ssid: "AMR-Fleet", channel: "36", band: "5 GHz" }
  ],
  badZones: [
    { plant: "Shelbyville", zone: "Dock door 3", disconnects: 14, reconnects: 36, offline: 6, weak: 29, roaming: 18, score: 95 },
    { plant: "Hopkinsville", zone: "Charge station west", disconnects: 20, reconnects: 44, offline: 10, weak: 34, roaming: 12, score: 92 },
    { plant: "Springfield", zone: "Outbound lane", disconnects: 8, reconnects: 16, offline: 2, weak: 20, roaming: 9, score: 64 },
    { plant: "Shelbyville", zone: "Assembly north aisle", disconnects: 5, reconnects: 12, offline: 1, weak: 11, roaming: 8, score: 48 }
  ],
  logs: [
    { time: "2026-06-26T08:12", plant: "Shelbyville", amr: "SHV-AMR-02", server: "shv-fleet-ubuntu01", host: "pve-shv-01", vm: "214", source: "Roboshop Core", category: "AMR", severity: "High", topic: "Robot offline / disconnect", message: "TCP reconnect storm followed by robot disconnected at Dock door 3." },
    { time: "2026-06-26T08:14", plant: "Shelbyville", amr: "SHV-AMR-02", server: "shv-fleet-ubuntu01", host: "pve-shv-01", vm: "214", source: "Network / DHCP", category: "Network", severity: "Medium", topic: "Network / DHCP failure", message: "DHCP renewal delay observed on AMR VLAN near AP-SHV-D03." },
    { time: "2026-06-26T09:33", plant: "Hopkinsville", amr: "HKV-AMR-03", server: "hkv-fleet-ubuntu01", host: "pve-hkv-02", vm: "311", source: "Ubuntu", category: "Server", severity: "High", topic: "Swap full", message: "Swap usage reached 96 percent before Fleet Manager timeout warnings." },
    { time: "2026-06-26T09:40", plant: "Hopkinsville", amr: "HKV-AMR-03", server: "hkv-fleet-ubuntu01", host: "pve-hkv-02", vm: "311", source: "Proxmox", category: "VM", severity: "High", topic: "VM killed by OOM", message: "Kernel OOM report references Fleet Manager VM memory pressure." },
    { time: "2026-06-26T10:02", plant: "Springfield", amr: "SPF-AMR-07", server: "spf-fleet-ubuntu01", host: "pve-spf-01", vm: "120", source: "RDS", category: "RDS", severity: "Low", topic: "RDS map update", message: "Map package applied; no MD5 mismatch found." },
    { time: "2026-06-26T10:17", plant: "Springfield", amr: "SPF-AMR-07", server: "spf-fleet-ubuntu01", host: "pve-spf-01", vm: "120", source: "AMR Robot", category: "Battery", severity: "Medium", topic: "Battery status", message: "Battery sag warning during GoTarget station command." }
  ],
  commands: [
    { id: "cmd-reconnect", name: "Find TCP reconnects", topic: "Robot offline / disconnect", category: "AMR", description: "Search Roboshop Core logs for reconnect and disconnect evidence.", serverType: "Fleet Manager Ubuntu", text: "grep -R -i 'tcp reconnect|disconnect' /var/log/roboshop", expected: "Timestamp, AMR, reconnect count", notes: "Read-only", active: true },
    { id: "cmd-oom", name: "Check VM OOM", topic: "VM killed by OOM", category: "Proxmox", description: "Find VM OOM and host memory exhaustion events.", serverType: "Proxmox host", text: "journalctl -k --since '2 hours ago' | grep -i 'oom|killed process'", expected: "Kernel OOM lines with VM process", notes: "Run on host", active: true },
    { id: "cmd-wifi", name: "Read Wi-Fi link", topic: "Wi-Fi RSSI", category: "Network", description: "Collect RSSI, SSID, BSSID, channel, and band from Linux wireless tools.", serverType: "AMR Linux shell", text: "iw dev wlan0 link && iw dev wlan0 station dump", expected: "RSSI, BSSID, tx/rx bitrate", notes: "Availability depends on AMR access", active: true }
  ],
  discovery: [
    { point: "AMR live position", status: "Not Run", source: "Roboshop Core API or RDS map data", command: "GET /api/robots or RDS position topic", gap: "Endpoint and auth not configured" },
    { point: "AMR map X/Y coordinates", status: "Not Run", source: "Roboshop Core logs or database", command: "position payload parser", gap: "Need sample logs or DB schema" },
    { point: "Wi-Fi RSSI", status: "Not Run", source: "AMR Linux Wi-Fi command", command: "iw dev wlan0 link", gap: "Requires AMR SSH or telemetry export" },
    { point: "Connected AP/BSSID", status: "Not Run", source: "AMR Linux Wi-Fi command or controller", command: "iw dev wlan0 link | grep Connected", gap: "Controller integration not configured" },
    { point: "SSID, channel, and band", status: "Not Run", source: "AMR command or Aruba/Cisco controller", command: "iw dev wlan0 info; iw dev wlan0 link", gap: "Channel to band mapping needs source" },
    { point: "Disconnect and reconnect events", status: "Not Run", source: "Roboshop Core logs", command: "grep -i 'disconnect|reconnect'", gap: "Need plant log path validation" },
    { point: "Match Wi-Fi data with AMR location", status: "Not Run", source: "Timestamp correlation", command: "join by AMR name and timestamp window", gap: "Needs synchronized clocks and shared AMR IDs" },
    { point: "RDS map and model data", status: "Not Run", source: "RDS logs", command: "grep -i 'map|model|md5'", gap: "RDS log path not verified" },
    { point: "Ubuntu server reboot and shutdown", status: "Not Run", source: "systemd journal", command: "journalctl --list-boots; last -x reboot shutdown", gap: "SSH settings pending" },
    { point: "Proxmox host and VM events", status: "Not Run", source: "Proxmox journal/API", command: "journalctl -u pvedaemon -u pveproxy", gap: "Host credentials pending" }
  ],
  wifiPoints: [
    { plant: "Shelbyville", amr: "SHV-AMR-01", x: 18, y: 38, rssi: -58, quality: "Good", ap: "AP-SHV-A12", ssid: "AMR-Fleet", channel: "44", band: "5 GHz", reconnect: false, disconnect: false, offline: false, roaming: false, time: "2026-06-26T08:02" },
    { plant: "Shelbyville", amr: "SHV-AMR-02", x: 78, y: 38, rssi: -76, quality: "Poor", ap: "AP-SHV-D03", ssid: "AMR-Fleet", channel: "149", band: "5 GHz", reconnect: true, disconnect: true, offline: false, roaming: true, time: "2026-06-26T08:12" },
    { plant: "Shelbyville", amr: "SHV-AMR-02", x: 86, y: 58, rssi: -84, quality: "Critical", ap: "AP-SHV-D03", ssid: "AMR-Fleet", channel: "149", band: "5 GHz", reconnect: true, disconnect: true, offline: true, roaming: true, time: "2026-06-26T08:18" },
    { plant: "Springfield", amr: "SPF-AMR-07", x: 71, y: 56, rssi: -64, quality: "Weak", ap: "AP-SPF-O09", ssid: "AMR-Fleet", channel: "11", band: "2.4 GHz", reconnect: true, disconnect: false, offline: false, roaming: true, time: "2026-06-26T10:17" },
    { plant: "Hopkinsville", amr: "HKV-AMR-03", x: 23, y: 78, rssi: -83, quality: "Critical", ap: "AP-HKV-C02", ssid: "AMR-Fleet", channel: "157", band: "5 GHz", reconnect: true, disconnect: true, offline: true, roaming: false, time: "2026-06-26T09:33" },
    { plant: "Hopkinsville", amr: "HKV-AMR-04", x: 48, y: 78, rssi: -59, quality: "Good", ap: "AP-HKV-F01", ssid: "AMR-Fleet", channel: "36", band: "5 GHz", reconnect: false, disconnect: false, offline: false, roaming: false, time: "2026-06-26T09:50" }
  ],
  uploadedMap: ""
};

let state = loadState();
let selectedAmrId = null;

const $ = (selector, root = document) => root.querySelector(selector);
const $$ = (selector, root = document) => Array.from(root.querySelectorAll(selector));
const slug = (value) => String(value).toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/(^-|-$)/g, "");
const selectedPlant = () => $("#globalPlantFilter").value;
const searchText = () => $("#globalSearch").value.trim().toLowerCase();
const plantMatches = (plant) => selectedPlant() === "All" || plant === selectedPlant();
const textMatches = (item) => !searchText() || JSON.stringify(item).toLowerCase().includes(searchText());

function loadState() {
  try {
    const saved = JSON.parse(localStorage.getItem(STORAGE_KEY));
    return saved ? { ...structuredClone(seed), ...saved } : structuredClone(seed);
  } catch (_) {
    return structuredClone(seed);
  }
}
function saveState() { localStorage.setItem(STORAGE_KEY, JSON.stringify(state)); }
function unique(values) { return [...new Set(values.filter(Boolean))].sort(); }
function knownPlantNames() { return unique([...state.plants.map((plant) => plant.name), ...state.amrs.map((amr) => amr.plant), ...state.logs.map((log) => log.plant), ...state.wifiPoints.map((point) => point.plant), ...Object.keys(RDS_PLANT_HOSTS)]); }
function matchFilter(filter, value) { return filter === "All" || filter === value; }
function countBy(items, key) { return items.reduce((acc, item) => { acc[item[key]] = (acc[item[key]] || 0) + 1; return acc; }, {}); }
function escapeHtml(value) { return String(value).replace(/[&<>"]/g, (char) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[char])); }
function escapeAttr(value) { return escapeHtml(value).replace(/'/g, "&#39;"); }
function badge(value) { const klass = String(value).toLowerCase().replace(/\s+/g, "-"); return `<span class="badge ${klass}">${escapeHtml(value)}</span>`; }
function formatTime(value) { return new Date(value).toLocaleString([], { month: "short", day: "2-digit", hour: "2-digit", minute: "2-digit" }); }
function rdsHostForPlant(plant) { return RDS_PLANT_HOSTS[plant] || "RDS host not configured"; }

function init() {
  bindNavigation();
  bindForms();
  bindFilters();
  refreshAll();
}
function refreshAll() {
  populateGlobalFilters();
  renderDashboard();
  renderLogs();
  renderCommands();
  renderDiscovery();
  renderHeatMap();
  renderReports();
  renderAdmin();
}
function bindNavigation() {
  $$(".nav-item").forEach((button) => button.addEventListener("click", () => showView(button.dataset.view)));
  $$('[data-nav-target]').forEach((button) => button.addEventListener("click", () => showView(button.dataset.navTarget)));
  $("#openHeatMapFromDetail").addEventListener("click", () => showView("heatmap"));
}
function showView(view) {
  $$(".nav-item").forEach((button) => button.classList.toggle("active", button.dataset.view === view));
  $$(".view").forEach((section) => section.classList.toggle("active-view", section.id === view));
  const copy = {
    dashboard: ["AMR Health Dashboard", "Current AMR status, reconnects, disconnects, and bad-zone signals."],
    logs: ["Log Investigation", "Query by troubleshooting topic instead of manually opening every system."],
    discovery: ["Data Discovery", "Confirm live position, Wi-Fi, disconnect, RDS, Ubuntu, Proxmox, and VM data sources."],
    heatmap: ["AMR Wi-Fi Heat Map", "Overlay AMR position, RSSI, AP, roaming, reconnect, disconnect, and offline points."],
    reports: ["Correlation Reports", "Rank worst AMRs, zones, APs, and infrastructure events over time."],
    admin: ["Admin Configuration", "Manage plants, AMRs, Fleet Manager, Roboshop, logs, commands, and thresholds."]
  }[view];
  $("#viewTitle").textContent = copy[0];
  $("#viewSubtitle").textContent = copy[1];
}
function setOptions(select, options, current = "All") {
  select.innerHTML = options.map((option) => `<option ${option === current ? "selected" : ""}>${escapeHtml(option)}</option>`).join("");
}
function populateGlobalFilters() {
  const current = $("#globalPlantFilter").value || "All";
  const plantOptions = knownPlantNames();
  setOptions($("#globalPlantFilter"), ["All", ...plantOptions], current);
  const amrSelect = $('#amrForm select[name="plant"]');
  setOptions(amrSelect, state.plants.map((plant) => plant.name), amrSelect.value || state.plants[0]?.name);
  const rdsImportPlant = $("#rdsImportPlant");
  if (rdsImportPlant) setOptions(rdsImportPlant, plantOptions, rdsImportPlant.value || "Shelbyville");
}
function filteredAmrs() { return state.amrs.filter((amr) => plantMatches(amr.plant) && textMatches(amr)); }
function renderMetrics(target, rows) {
  $(target).innerHTML = rows.map(([label, value, help]) => `<article class="metric-card"><span class="metric-label">${escapeHtml(label)}</span><strong class="metric-value">${escapeHtml(value)}</strong><small class="metric-help">${escapeHtml(help)}</small></article>`).join("");
}
function renderDashboard() {
  const amrs = filteredAmrs();
  const online = amrs.filter((amr) => amr.status === "Online").length;
  const disconnected = amrs.filter((amr) => amr.status === "Disconnected").length;
  const offline = amrs.filter((amr) => amr.status === "Offline").length;
  const reconnects = amrs.reduce((sum, amr) => sum + Number(amr.reconnects || 0), 0);
  renderMetrics("#dashboardMetrics", [
    ["AMRs", amrs.length, "Filtered inventory"],
    ["Online", online, "Healthy now"],
    ["Disconnected / Offline", disconnected + offline, "Needs investigation"],
    ["TCP Reconnects", reconnects, "Current sample window"]
  ]);
  $("#amrTableBody").innerHTML = amrs.map((amr) => `
    <tr><td><strong>${escapeHtml(amr.name)}</strong></td><td>${escapeHtml(amr.plant)}</td><td>${escapeHtml(amr.ip)}</td><td>${badge(amr.status)}</td><td>${amr.reconnects}</td><td>${amr.disconnects}</td><td>${escapeHtml(amr.offline)}</td><td>${escapeHtml(amr.worstDrop)}</td><td><button class="row-action" data-investigate="${escapeAttr(amr.id)}">Open</button></td></tr>
  `).join("") || `<tr><td colspan="9">No AMRs match the current filters.</td></tr>`;
  $$('[data-investigate]').forEach((button) => button.addEventListener("click", () => renderAmrDetail(button.dataset.investigate)));
  const zones = state.badZones.filter((zone) => plantMatches(zone.plant) && textMatches(zone)).sort((a, b) => b.score - a.score);
  $("#badZoneList").innerHTML = zones.map((zone) => `
    <article class="zone-card"><header><strong>${escapeHtml(zone.zone)}</strong><span>${zone.score}</span></header><small>${escapeHtml(zone.plant)} - ${zone.disconnects} disconnects, ${zone.reconnects} reconnects, ${zone.weak} weak signal reads, ${zone.roaming} roaming events</small><div class="score-bar"><span style="width:${Math.min(zone.score, 100)}%"></span></div></article>
  `).join("") || `<div class="empty-state">No bad zones match the current filters.</div>`;
}

function renderAmrDetail(id) {
  selectedAmrId = id;
  const amr = state.amrs.find((item) => item.id === id);
  if (!amr) return;
  $("#detailTitle").textContent = `${amr.name} Detail`;
  $("#detailSubtitle").textContent = `${amr.plant} - ${amr.ip} - worst drop: ${amr.worstDrop}`;
  const recentLogs = state.logs.filter((log) => log.amr === amr.name).slice(0, 3).map((log) => `${log.topic}: ${log.message}`).join(" | ") || "No logs found";
  const points = state.wifiPoints.filter((point) => point.amr === amr.name);
  $("#amrDetailContent").classList.remove("empty-state");
  const detailRows = [
    ["Status", badge(amr.status)], ["Wi-Fi", `${amr.rssi} dBm via ${escapeHtml(amr.ap)}`],
    ["Disconnect History", `${amr.disconnects} disconnects, ${amr.reconnects} reconnects`], ["Offline History", escapeHtml(amr.offline)],
    ["Heat Map Points", `${points.length} correlated positions`], ["SSID / Channel", `${escapeHtml(amr.ssid)} ch ${escapeHtml(amr.channel)} ${escapeHtml(amr.band)}`],
    ["Worst Drop", escapeHtml(amr.worstDrop)], ["Recent Logs", escapeHtml(recentLogs)]
  ];
  if (amr.source) detailRows.push(["Source", escapeHtml(amr.source)]);
  if (amr.battery) detailRows.push(["Battery", escapeHtml(amr.battery)]);
  if (amr.rdsX !== undefined && amr.rdsY !== undefined) detailRows.push(["RDS Position", `x ${escapeHtml(amr.rdsX)}, y ${escapeHtml(amr.rdsY)}, angle ${escapeHtml(amr.rdsAngle ?? "unknown")}`]);
  if (amr.currentStation) detailRows.push(["Current Station", escapeHtml(amr.currentStation)]);
  if (amr.currentArea) detailRows.push(["Current Area", escapeHtml(amr.currentArea || "none")]);
  if (amr.mapMd5 || amr.modelMd5) detailRows.push(["RDS Map / Model", `${escapeHtml(amr.currentMap || "unknown")} / ${escapeHtml(amr.modelMd5 || "unknown")}`]);
  if (amr.confidence !== undefined || amr.networkDelay !== undefined) detailRows.push(["Confidence / Delay", `${escapeHtml(amr.confidence ?? "unknown")} / ${escapeHtml(amr.networkDelay ?? "unknown")} ms`]);
  if (amr.issue) detailRows.push(["RDS Issue", escapeHtml(amr.issue)]);
  $("#amrDetailContent").innerHTML = detailRows.map(([label, value]) => `<article class="detail-card"><span>${label}</span><strong>${value}</strong></article>`).join("");
  $("#amrDetailPanel").scrollIntoView({ behavior: "smooth", block: "start" });
}

function populateLogFilters() {
  setOptions($("#logTopicFilter"), ["All", ...topics], $("#logTopicFilter").value || "All");
  setOptions($("#logAmrFilter"), ["All", ...state.amrs.map((amr) => amr.name)], $("#logAmrFilter").value || "All");
  setOptions($("#logServerFilter"), ["All", ...unique(state.logs.map((log) => log.server))], $("#logServerFilter").value || "All");
  setOptions($("#logHostFilter"), ["All", ...unique(state.logs.map((log) => log.host))], $("#logHostFilter").value || "All");
  setOptions($("#logSourceFilter"), ["All", ...unique(state.logs.map((log) => log.source))], $("#logSourceFilter").value || "All");
  setOptions($("#logCategoryFilter"), ["All", ...unique(state.logs.map((log) => log.category))], $("#logCategoryFilter").value || "All");
  setOptions($("#logSeverityFilter"), ["All", "High", "Medium", "Low"], $("#logSeverityFilter").value || "All");
}
function getLogFilters() {
  return {
    topic: $("#logTopicFilter").value, amr: $("#logAmrFilter").value, server: $("#logServerFilter").value,
    host: $("#logHostFilter").value, vm: $("#logVmFilter").value.trim(), source: $("#logSourceFilter").value,
    category: $("#logCategoryFilter").value, severity: $("#logSeverityFilter").value, from: $("#logFromFilter").value,
    to: $("#logToFilter").value, keyword: $("#logKeywordFilter").value.trim()
  };
}
function renderLogs() {
  populateLogFilters();
  const filters = getLogFilters();
  const logs = state.logs.filter((log) => {
    const fromOk = !filters.from || log.time >= filters.from;
    const toOk = !filters.to || log.time <= filters.to;
    return plantMatches(log.plant) && textMatches(log) && matchFilter(filters.topic, log.topic) && matchFilter(filters.amr, log.amr)
      && matchFilter(filters.server, log.server) && matchFilter(filters.host, log.host) && matchFilter(filters.source, log.source)
      && matchFilter(filters.category, log.category) && matchFilter(filters.severity, log.severity) && (!filters.vm || log.vm.includes(filters.vm))
      && (!filters.keyword || JSON.stringify(log).toLowerCase().includes(filters.keyword.toLowerCase())) && fromOk && toOk;
  });
  $("#logResultCount").textContent = `${logs.length} result${logs.length === 1 ? "" : "s"}`;
  $("#logTableBody").innerHTML = logs.map((log) => `<tr><td>${formatTime(log.time)}</td><td>${escapeHtml(log.plant)}</td><td>${escapeHtml(log.amr)}</td><td>${escapeHtml(log.topic)}</td><td>${escapeHtml(log.source)}</td><td>${badge(log.severity)}</td><td>${escapeHtml(log.message)}</td></tr>`).join("") || `<tr><td colspan="7">No log evidence matches the current filters.</td></tr>`;
}
function renderCommands() {
  const commands = state.commands.filter((command) => command.active !== false && textMatches(command));
  $("#commandLibrary").innerHTML = commands.map((command) => `<article class="command-card"><header><strong>${escapeHtml(command.name)}</strong>${badge(command.category)}</header><small>${escapeHtml(command.topic)} - ${escapeHtml(command.serverType)}</small><p>${escapeHtml(command.description || command.notes || "Saved troubleshooting command")}</p><code>${escapeHtml(command.text)}</code></article>`).join("");
}
function renderDiscovery() {
  const counts = countBy(state.discovery, "status");
  $("#discoverySummary").innerHTML = ["Available", "Partial", "Missing", "Not Run"].map((status) => `<article class="discovery-card"><strong>${counts[status] || 0}</strong><span>${status}</span></article>`).join("");
  $("#discoveryTableBody").innerHTML = state.discovery.map((item) => `<tr><td><strong>${escapeHtml(item.point)}</strong></td><td>${badge(item.status)}</td><td>${escapeHtml(item.source)}</td><td><code>${escapeHtml(item.command)}</code></td><td>${escapeHtml(item.gap)}</td></tr>`).join("");
}
function runDiscovery() {
  const resultMap = {
    "Disconnect and reconnect events": ["Available", "Roboshop Core logs", "grep -R -i 'disconnect|reconnect' configured log paths", "Ready after path validation"],
    "Ubuntu server reboot and shutdown": ["Available", "systemd journal", "journalctl --list-boots; last -x reboot shutdown", "Needs SSH credentials for live run"],
    "Proxmox host and VM events": ["Partial", "Proxmox journal/API", "journalctl -u pvedaemon -u pveproxy", "API token and host list required"],
    "AMR live position": ["Partial", "Roboshop Core API", "GET /api/robots", "Need authenticated endpoint test"],
    "AMR map X/Y coordinates": ["Partial", "Roboshop/RDS position payload", "position payload parser", "Need sample payloads"],
    "Wi-Fi RSSI": ["Missing", "AMR shell or controller", "iw dev wlan0 link", "No AMR SSH/controller source configured"],
    "Connected AP/BSSID": ["Missing", "AMR shell or wireless controller", "iw dev wlan0 link", "No controller integration configured"],
    "SSID, channel, and band": ["Missing", "AMR shell or controller", "iw dev wlan0 info", "Need Wi-Fi source"],
    "Match Wi-Fi data with AMR location": ["Partial", "Timestamp correlation", "join by AMR name and timestamp window", "Needs synchronized source data"],
    "RDS map and model data": ["Partial", "RDS logs", "grep -i 'map|model|md5'", "Need configured RDS path per plant"]
  };
  state.discovery = state.discovery.map((item) => {
    const update = resultMap[item.point];
    return update ? { ...item, status: update[0], source: update[1], command: update[2], gap: update[3] } : item;
  });
  saveState();
  renderDiscovery();
}
function renderHeatMap() {
  const image = $("#plantMapImage");
  image.src = state.uploadedMap || "assets/plant-map.svg";
  const signal = $("#signalFilter").value;
  const points = state.wifiPoints.filter((point) => {
    if (!plantMatches(point.plant) || !textMatches(point)) return false;
    if (signal !== "All" && point.quality !== signal) return false;
    if (point.disconnect && !$("#showDisconnects").checked) return false;
    if (point.reconnect && !$("#showReconnects").checked) return false;
    if (point.offline && !$("#showOffline").checked) return false;
    if (point.roaming && !$("#showRoaming").checked) return false;
    return true;
  });
  $("#heatMapOverlay").innerHTML = points.map((point) => `<button class="map-point ${point.quality.toLowerCase()}" style="left:${point.x}%;top:${point.y}%" data-label="${escapeAttr(point.amr)} ${point.rssi} dBm ${escapeAttr(point.ap)}" title="${escapeAttr(`${point.amr} ${point.quality} ${point.rssi} dBm via ${point.ap}`)}"></button>`).join("");
}
function renderReports() {
  const amrs = filteredAmrs();
  const zones = state.badZones.filter((zone) => plantMatches(zone.plant));
  const critical = state.wifiPoints.filter((point) => plantMatches(point.plant) && ["Poor", "Critical"].includes(point.quality));
  renderMetrics("#reportMetrics", [
    ["Bad Zones", zones.length, "Current plant scope"],
    ["Poor/Critical Points", critical.length, "Wi-Fi evidence points"],
    ["High Severity Logs", state.logs.filter((log) => plantMatches(log.plant) && log.severity === "High").length, "Correlated events"],
    ["Commands", state.commands.length, "Saved playbook items"]
  ]);
  $("#worstAmrReport").innerHTML = amrs.map((amr) => ({ ...amr, score: amr.disconnects * 4 + amr.reconnects + Math.max(0, Math.abs(amr.rssi) - 55) })).sort((a, b) => b.score - a.score).map((amr) => `<article class="rank-card"><header><strong>${escapeHtml(amr.name)}</strong><span>${amr.score}</span></header><small>${escapeHtml(amr.plant)} - ${amr.disconnects} disconnects, ${amr.reconnects} reconnects, ${amr.rssi} dBm</small><div class="score-bar"><span style="width:${Math.min(amr.score, 100)}%"></span></div></article>`).join("");
  const apScores = Object.values(state.wifiPoints.filter((point) => plantMatches(point.plant)).reduce((acc, point) => {
    acc[point.ap] ||= { ap: point.ap, weak: 0, events: 0, score: 0 };
    acc[point.ap].weak += ["Weak", "Poor", "Critical"].includes(point.quality) ? 1 : 0;
    acc[point.ap].events += Number(point.disconnect) + Number(point.reconnect) + Number(point.offline) + Number(point.roaming);
    acc[point.ap].score += Math.max(0, Math.abs(point.rssi) - 50) + acc[point.ap].events;
    return acc;
  }, {})).sort((a, b) => b.score - a.score);
  $("#worstApReport").innerHTML = apScores.map((ap) => `<article class="rank-card"><header><strong>${escapeHtml(ap.ap)}</strong><span>${Math.round(ap.score)}</span></header><small>${ap.weak} weak/poor points, ${ap.events} disconnect/reconnect/offline/roaming flags</small><div class="score-bar"><span style="width:${Math.min(ap.score, 100)}%"></span></div></article>`).join("");
  $("#correlationTimeline").innerHTML = state.logs.filter((log) => plantMatches(log.plant)).sort((a, b) => b.time.localeCompare(a.time)).map((log) => `<article class="timeline-item"><time>${formatTime(log.time)}</time><div><strong>${escapeHtml(log.topic)}</strong><small>${escapeHtml(log.plant)} - ${escapeHtml(log.source)} - ${escapeHtml(log.message)}</small></div>${badge(log.severity)}</article>`).join("");
}
function renderAdmin() {
  $("#plantAdminList").innerHTML = state.plants.map((plant) => `<article class="admin-card"><div><strong>${escapeHtml(plant.name)}</strong><small>${escapeHtml(plant.fleetManager)} - ${escapeHtml(plant.roboshop)} - ${escapeHtml(plant.logPath)}</small></div><button class="delete-action" data-delete-plant="${escapeAttr(plant.name)}">Remove</button></article>`).join("");
  $("#amrAdminList").innerHTML = state.amrs.map((amr) => `<article class="admin-card"><div><strong>${escapeHtml(amr.name)}</strong><small>${escapeHtml(amr.plant)} - ${escapeHtml(amr.ip)} - ${escapeHtml(amr.status)}</small></div><button class="delete-action" data-delete-amr="${escapeAttr(amr.id)}">Remove</button></article>`).join("");
  $('#thresholdForm input[name="good"]').value = state.thresholds.good;
  $('#thresholdForm input[name="weak"]').value = state.thresholds.weak;
  $('#thresholdForm input[name="poor"]').value = state.thresholds.poor;
  $('#thresholdForm input[name="interval"]').value = state.thresholds.interval;
  $("#thresholdNote").textContent = `Good: ${state.thresholds.good} dBm and stronger. Weak: ${state.thresholds.weak} to ${state.thresholds.good - 1}. Poor: ${state.thresholds.poor} to ${state.thresholds.weak - 1}. Critical: disconnect/offline or below ${state.thresholds.poor}.`;
  $("#rdsImportNote").textContent = state.rdsImportNote || "No RDS core JSON imported yet.";
  $$('[data-delete-plant]').forEach((button) => button.addEventListener("click", () => deletePlant(button.dataset.deletePlant)));
  $$('[data-delete-amr]').forEach((button) => button.addEventListener("click", () => deleteAmr(button.dataset.deleteAmr)));
}
function setDiscoveryPoint(point, status, source, command, gap) {
  state.discovery = state.discovery.map((item) => item.point === point ? { ...item, status, source, command, gap } : item);
}
function normalizeRdsCoreResponse(payload, plant = "Shelbyville") {
  const core = payload?.data;
  const reports = Array.isArray(core?.report) ? core.report : [];
  if (!core || reports.length === 0) throw new Error("No data.report array found in RDS core JSON.");
  const positions = reports.map((item) => item.rbk_report).filter(Boolean).filter((rbk) => Number.isFinite(Number(rbk.x)) && Number.isFinite(Number(rbk.y)));
  const xs = positions.map((rbk) => Number(rbk.x));
  const ys = positions.map((rbk) => Number(rbk.y));
  const minX = Math.min(...xs), maxX = Math.max(...xs), minY = Math.min(...ys), maxY = Math.max(...ys);
  const scale = (value, min, max) => max === min ? 50 : 10 + ((Number(value) - min) / (max - min)) * 80;
  const importedAt = new Date().toISOString();
  const source = `${plant} RDS Core`;
  const rdsHost = rdsHostForPlant(plant);
  const amrs = reports.map((item) => {
    const rbk = item.rbk_report || {};
    const basic = item.basic_info || {};
    const reason = item.undispatchable_reason || {};
    const name = item.uuid || item.vehicle_id || item.current_order?.vehicle || "Unknown AMR";
    const disconnected = Number(item.connection_status) === 0 || reason.disconnect === true;
    const emergency = rbk.emergency === true;
    const status = disconnected ? "Disconnected" : "Online";
    const warnings = [...(item.warnings || []), ...(rbk.warnings || []), ...(rbk.alarms?.warnings || [])];
    const errors = [...(item.errors || []), ...(rbk.errors || []), ...(rbk.alarms?.errors || [])];
    const currentArea = (basic.current_area || []).join(", ");
    const currentStation = rbk.current_station || item.current_order?.blocks?.[0]?.location || "No station reported";
    const issue = disconnected ? "RDS reports robot disconnected" : emergency ? "Emergency stop active" : errors.length ? "RDS error present" : warnings.length ? warnings[0].desc || warnings[0].describe || "RDS warning present" : "No active RDS issue";
    return {
      id: `rds-${slug(name)}`,
      name,
      plant,
      ip: basic.ip || "unknown",
      status,
      reconnects: 0,
      disconnects: disconnected ? 1 : 0,
      offline: disconnected ? "Disconnected now" : "0m",
      worstDrop: currentStation || currentArea || `x ${rbk.x}, y ${rbk.y}`,
      rssi: -60,
      ap: "RDS Core position only",
      ssid: "unknown",
      channel: "unknown",
      band: "unknown",
      imported: true,
      source,
      battery: Number.isFinite(Number(rbk.battery_level)) ? `${Math.round(Number(rbk.battery_level) * 100)}%` : "unknown",
      rdsX: rbk.x,
      rdsY: rbk.y,
      rdsAngle: rbk.angle,
      currentStation,
      currentArea,
      currentMap: rbk.current_map || basic.current_map || "unknown",
      mapMd5: rbk.current_map_md5 || core.scene_md5 || "unknown",
      modelMd5: core.model_md5 || "unknown",
      confidence: rbk.confidence,
      networkDelay: item.network_delay,
      dispatchable: item.dispatchable === true,
      emergency,
      issue,
      importedAt
    };
  });
  const points = reports.map((item) => {
    const rbk = item.rbk_report || {};
    const basic = item.basic_info || {};
    const name = item.uuid || item.vehicle_id || item.current_order?.vehicle || "Unknown AMR";
    const disconnected = Number(item.connection_status) === 0 || item.undispatchable_reason?.disconnect === true;
    const critical = disconnected || rbk.emergency === true || item.is_error === true;
    const x = Number.isFinite(Number(rbk.x)) ? scale(rbk.x, minX, maxX) : 50;
    const y = Number.isFinite(Number(rbk.y)) ? scale(rbk.y, minY, maxY) : 50;
    return {
      plant,
      amr: name,
      x: Math.max(5, Math.min(95, x)),
      y: Math.max(5, Math.min(95, y)),
      rssi: -60,
      quality: critical ? "Critical" : "Good",
      ap: "RDS Core position only",
      ssid: "unknown",
      channel: "unknown",
      band: "unknown",
      reconnect: false,
      disconnect: disconnected,
      offline: disconnected,
      roaming: false,
      time: core.create_on || importedAt,
      imported: true,
      source,
      rdsX: rbk.x,
      rdsY: rbk.y,
      station: rbk.current_station || item.current_order?.blocks?.[0]?.location || "unknown",
      map: rbk.current_map || basic.current_map || "unknown"
    };
  });
  const logs = [];
  const pushCoreLog = (entry, severity, topic, fallback) => logs.push({
    time: core.create_on || importedAt,
    plant,
    amr: entry.desc?.match(/\[(.*?)\]/)?.[1] || entry.desc?.match(/(AMR-[0-9]+)/)?.[1] || "RDS Core",
    server: rdsHost,
    host: `${plant} RDS`,
    vm: "",
    source: "RDS Core",
    category: "RDS",
    severity,
    topic,
    message: entry.desc || entry.describe || fallback,
    imported: true
  });
  [...(core.warnings || []), ...(core.alarms?.warnings || [])].forEach((warning, index) => pushCoreLog(warning, "High", "RDS Core issue", `RDS warning ${warning.code || index}`));
  [...(core.errors || []), ...(core.alarms?.errors || [])].forEach((error, index) => pushCoreLog(error, "High", "RDS Core issue", `RDS error ${error.code || index}`));
  [...(core.fatals || []), ...(core.alarms?.fatals || [])].forEach((fatal, index) => pushCoreLog(fatal, "High", "RDS Core issue", `RDS fatal ${fatal.code || index}`));
  reports.forEach((item) => {
    const rbk = item.rbk_report || {};
    const name = item.uuid || item.vehicle_id || item.current_order?.vehicle || "Unknown AMR";
    const warnings = [...(item.warnings || []), ...(rbk.warnings || []), ...(rbk.alarms?.warnings || [])];
    const disconnected = Number(item.connection_status) === 0 || item.undispatchable_reason?.disconnect === true;
    if (disconnected) logs.push({ time: core.create_on || importedAt, plant, amr: name, server: rdsHost, host: `${plant} RDS`, vm: "", source: "RDS Core", category: "AMR", severity: "High", topic: "Robot offline / disconnect", message: `${name} is disconnected in RDS core feed. IP ${item.basic_info?.ip || "unknown"}.`, imported: true });
    if (rbk.emergency) logs.push({ time: core.create_on || importedAt, plant, amr: name, server: rdsHost, host: `${plant} RDS`, vm: "", source: "AMR Robot", category: "AMR", severity: "High", topic: "Application crash", message: `${name} reports emergency stop active.`, imported: true });
    warnings.forEach((warning) => logs.push({ time: core.create_on || importedAt, plant, amr: name, server: rdsHost, host: `${plant} RDS`, vm: "", source: "AMR Robot", category: "AMR", severity: item.is_error ? "High" : "Medium", topic: "RDS Core issue", message: warning.desc || warning.describe || `RDS warning ${warning.code || "unknown"}`, imported: true }));
  });
  return {
    amrs,
    points,
    logs,
    summary: {
      plant,
      source,
      importedAt,
      createdOn: core.create_on || "unknown",
      modelMd5: core.model_md5 || "unknown",
      sceneMd5: core.scene_md5 || "unknown",
      robots: amrs.length,
      disconnected: amrs.filter((amr) => amr.status === "Disconnected").length,
      warnings: logs.length
    }
  };
}
function mergeRdsCoreImport(normalized) {
  const source = normalized.summary.source;
  state.amrs = state.amrs.filter((item) => !(item.imported && item.source === source)).concat(normalized.amrs);
  state.wifiPoints = state.wifiPoints.filter((item) => !(item.imported && item.source === source)).concat(normalized.points);
  state.logs = state.logs.filter((item) => !(item.imported && item.source === source)).concat(normalized.logs);
  state.rdsImportNote = `Imported ${normalized.summary.robots} ${normalized.summary.plant} AMRs from RDS core (${normalized.summary.createdOn}). Disconnected: ${normalized.summary.disconnected}. Model MD5: ${normalized.summary.modelMd5}. Scene MD5: ${normalized.summary.sceneMd5}.`;
  setDiscoveryPoint("AMR live position", "Available", "RDS Core", "GET /api/agv-report/core", `Imported robot live status and position from ${normalized.summary.plant} core feed`);
  setDiscoveryPoint("AMR map X/Y coordinates", "Available", "RDS Core", "rbk_report.x / rbk_report.y", "Coordinates imported; map scale still needs scene geometry alignment");
  setDiscoveryPoint("Disconnect and reconnect events", "Partial", "RDS Core", "connection_status and undispatchable_reason.disconnect", "Disconnect state is available; reconnect count still needs historical logs");
  setDiscoveryPoint("RDS map and model data", "Available", "RDS Core", "model_md5, scene_md5, current_map_md5", "Map/model metadata imported from core feed");
}
function handleRdsCoreImport(event) {
  const file = event.target.files[0];
  if (!file) return;
  const reader = new FileReader();
  reader.onload = () => {
    try {
      const payload = JSON.parse(reader.result);
      const selectedImportPlant = $("#rdsImportPlant")?.value || "Shelbyville";
      const normalized = normalizeRdsCoreResponse(payload, selectedImportPlant);
      mergeRdsCoreImport(normalized);
      saveState();
      refreshAll();
      showView("dashboard");
    } catch (error) {
      state.rdsImportNote = `Import failed: ${error.message}`;
      renderAdmin();
    } finally {
      event.target.value = "";
    }
  };
  reader.readAsText(file);
}
function resetImportedRdsData() {
  state.amrs = state.amrs.filter((item) => !item.imported);
  state.wifiPoints = state.wifiPoints.filter((item) => !item.imported);
  state.logs = state.logs.filter((item) => !item.imported);
  state.rdsImportNote = "Imported RDS data cleared.";
  saveState();
  refreshAll();
}
function bindForms() {
  $("#plantForm").addEventListener("submit", (event) => {
    event.preventDefault();
    const data = Object.fromEntries(new FormData(event.currentTarget));
    state.plants.push({ id: slug(data.name), name: data.name, fleetManager: data.fleetManager, roboshop: data.roboshop, logPath: data.logPath });
    event.currentTarget.reset();
    saveState();
    refreshAll();
  });
  $("#amrForm").addEventListener("submit", (event) => {
    event.preventDefault();
    const data = Object.fromEntries(new FormData(event.currentTarget));
    state.amrs.push({ id: slug(`${data.plant}-${data.name}`), name: data.name, plant: data.plant, ip: data.ip, status: data.status, reconnects: 0, disconnects: 0, offline: "0m", worstDrop: "No recent drop", rssi: -60, ap: "Unassigned", ssid: "AMR-Fleet", channel: "unknown", band: "unknown" });
    event.currentTarget.reset();
    saveState();
    refreshAll();
  });
  $("#commandForm").addEventListener("submit", (event) => {
    event.preventDefault();
    const data = Object.fromEntries(new FormData(event.currentTarget));
    state.commands.push({ id: slug(data.name), ...data, active: Boolean(data.active) });
    event.currentTarget.reset();
    saveState();
    refreshAll();
  });
  $("#thresholdForm").addEventListener("submit", (event) => {
    event.preventDefault();
    const data = Object.fromEntries(new FormData(event.currentTarget));
    state.thresholds = { good: Number(data.good), weak: Number(data.weak), poor: Number(data.poor), interval: Number(data.interval) };
    saveState();
    renderAdmin();
  });
  $("#mapUpload").addEventListener("change", handleMapUpload);
  $("#rdsCoreImport").addEventListener("change", handleRdsCoreImport);
  $("#resetImportedRds").addEventListener("click", resetImportedRdsData);
  $("#resetMap").addEventListener("click", () => { state.uploadedMap = ""; saveState(); renderHeatMap(); });
  $("#runDiscovery").addEventListener("click", runDiscovery);
  $("#refreshDashboard").addEventListener("click", renderDashboard);
  $("#clearLogFilters").addEventListener("click", clearLogFilters);
}
function bindFilters() {
  ["#globalPlantFilter", "#globalSearch", "#signalFilter", "#showDisconnects", "#showReconnects", "#showOffline", "#showRoaming"].forEach((selector) => $(selector).addEventListener("input", refreshAll));
  ["#logTopicFilter", "#logAmrFilter", "#logServerFilter", "#logHostFilter", "#logVmFilter", "#logSourceFilter", "#logCategoryFilter", "#logSeverityFilter", "#logFromFilter", "#logToFilter", "#logKeywordFilter"].forEach((selector) => $(selector).addEventListener("input", renderLogs));
}
function handleMapUpload(event) {
  const file = event.target.files[0];
  if (!file) return;
  const reader = new FileReader();
  reader.onload = () => { state.uploadedMap = reader.result; saveState(); renderHeatMap(); };
  reader.readAsDataURL(file);
}
function deletePlant(name) { state.plants = state.plants.filter((plant) => plant.name !== name); saveState(); refreshAll(); }
function deleteAmr(id) { state.amrs = state.amrs.filter((amr) => amr.id !== id); saveState(); refreshAll(); }
function clearLogFilters() {
  ["#logVmFilter", "#logFromFilter", "#logToFilter", "#logKeywordFilter"].forEach((selector) => $(selector).value = "");
  ["#logTopicFilter", "#logAmrFilter", "#logServerFilter", "#logHostFilter", "#logSourceFilter", "#logCategoryFilter", "#logSeverityFilter"].forEach((selector) => $(selector).value = "All");
  renderLogs();
}
document.addEventListener("DOMContentLoaded", init);
