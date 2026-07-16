package parser

import (
	"strings"
	"time"

	"drishti-amr-health/internal/models"
)

type rule struct {
	keywords  []string
	eventType string
	severity  string
}

var rules = []rule{
	// Robot connectivity.
	{[]string{"UnconnectedState"}, "robot_offline", "high"},
	{[]string{"ClosingState"}, "robot_offline", "medium"},
	{[]string{"remote host closed the connection"}, "robot_offline", "high"},
	{[]string{"Connect timeout"}, "robot_offline", "high"},
	{[]string{"Add device failed"}, "robot_offline", "high"},
	{[]string{"Not connected"}, "robot_offline", "medium"},
	{[]string{"slotTcpError", "setLastError"}, "robot_offline", "medium"},
	{[]string{"ConnectedState"}, "robot_online", "info"},

	// Ubuntu server shutdown / reboot.
	{[]string{"systemd-shutdown", "Reached target System Power Off", "Reached target Power-Off", "Power Down", "System is going down"}, "ubuntu_server_shutdown", "high"},
	{[]string{"systemd-logind: System is powering down", "systemd[1]: Powering Off", "Stopped target Multi-User System"}, "ubuntu_server_shutdown", "high"},
	{[]string{"systemd-logind: System is rebooting", "Reached target Reboot", "Rebooting", "reboot: Restarting system"}, "ubuntu_server_reboot", "high"},
	{[]string{"systemd[1]: Rebooting", "Starting Reboot", "Stopped target Graphical Interface"}, "ubuntu_server_reboot", "medium"},

	// System reaching a default/graphical/multi-user target after boot â€” often
	// coincides with a Roboshop/RDS config reload or service restart.
	{[]string{"Reached target Default", "Reached target Graphical", "Reached target Multi-User"}, "system_boot", "low"},

	// Proxmox host shutdown / reboot.
	{[]string{"host is going down", "host shutdown", "node shutdown", "pve host shutdown"}, "proxmox_host_shutdown", "high"},
	{[]string{"host reboot", "node reboot", "pve host reboot", "proxmox ve reboot"}, "proxmox_host_reboot", "high"},
	{[]string{"proxmox ve reboot", "proxmox-ve reboot", "pve-manager reboot"}, "proxmox_host_reboot", "medium"},

	// VM shutdown / reboot events as reported by QEMU/libvirt/Proxmox.
	{[]string{"qm shutdown", "guest-shutdown", "vm shutdown", "qemu: terminating on signal", "acpi shutdown"}, "vm_stopped", "medium"},
	{[]string{"qm stop", "stopping vm", "vm stopped", "status: stopped", "stop vm"}, "vm_stopped", "high"},
	{[]string{"qm start", "starting vm", "vm started", "status: running", "start vm"}, "vm_started", "info"},
	{[]string{"qm reboot", "guest reboot", "vm reboot", "resetting vm", "system_reset"}, "vm_reboot", "medium"},
	{[]string{"killed process", "kill process", "oom-kill", "out of memory: kill process"}, "vm_killed_by_oom", "critical"},

	// Proxmox memory / backup / HA.
	{[]string{"out of memory", "oom killer", "oom_kill_process", "memory allocation failure"}, "host_memory_exhaustion", "critical"},
	{[]string{"swap full", "swap is full", "no swap space", "swap usage 100"}, "swap_full", "critical"},
	{[]string{"backup found vm stopped", "not running - VM is stopped", "vm is stopped", "guest is not running"}, "backup_found_vm_stopped", "high"},
	{[]string{"vzdump", "backup job", "proxmox backup", "pbs", "backup started", "backup finished"}, "backup_job", "medium"},
	{[]string{"ha-manager", "pve-ha-crm", "pve-ha-lrm", "service migrated", "fence", "recovering service"}, "ha_action", "high"},

	// Power and network events.
	{[]string{"AC power", "UPS", "on battery", "power lost", "power restored", "Power button pressed"}, "power_network_event", "high"},
	{[]string{"dhcp failed", "no dhcpoffers", "network unreachable", "temporary failure in name resolution"}, "network_dhcp_failure", "high"},
	{[]string{"NETDEV WATCHDOG", "transmit timeout", "link is down", "Link is Down", "link becomes ready", "carrier lost"}, "network_dhcp_failure", "medium"},

	// Crashes & kernel panics.
	{[]string{"kernel panic", "Kernel panic"}, "crash", "critical"},
	{[]string{"BUG:", "OOPS:", "oops:"}, "crash", "critical"},
	{[]string{"Out of memory", "oom_kill_process", "OOM killer", "oom-killer"}, "crash", "critical"},
	{[]string{"segfault", "segmentation fault"}, "crash", "high"},
	{[]string{"Call Trace:", "Call trace:"}, "crash", "high"},
	{[]string{"general protection fault"}, "crash", "critical"},
	{[]string{"RIP:", "RSP:"}, "crash", "high"},
	{[]string{"Oops:"}, "crash", "critical"},
	{[]string{"watchdog: BUG: soft lockup"}, "crash", "critical"},
	{[]string{"core dumped", "Aborted (core"}, "crash", "high"},

	// Disk / filesystem errors.
	{[]string{"I/O error", "EXT4-fs error", "XFS (", "BTRFS error"}, "disk_error", "high"},
	{[]string{"Buffer I/O error", "end_request"}, "disk_error", "high"},
	{[]string{"filesystem error", "disk error"}, "disk_error", "high"},
	{[]string{"SCSI error", "No space left"}, "disk_error", "high"},
	{[]string{"smart overall-health", "SMART Health Status", "zpool status", "read error", "write error"}, "disk_smart_issue", "high"},

	// Service failures.
	{[]string{"Failed to start", "failed with result", "Service entered failed state"}, "error", "high"},
	{[]string{"systemd[1]: Failed"}, "error", "high"},
	{[]string{"startup_robod", "RoboShopPro", "rdscore"}, "error", "medium"},
	{[]string{"failed unit", "service failed", "main process exited", "unit entered failed state"}, "service_failure", "high"},

	// Ubuntu log gaps / auth activity.
	{[]string{"journal begins", "logs begin at", "rotated", "time jump", "clock jump"}, "ubuntu_log_gap", "medium"},
	{[]string{"sshd", "accepted password", "accepted publickey", "failed password", "session opened", "sudo:"}, "ssh_login_activity", "low"},

	// Hardware errors.
	{[]string{"MCE", "Machine check events logged", "hardware error"}, "error", "critical"},
	{[]string{"EDAC", "corrected memory error", "uncorrected memory error"}, "error", "high"},
	{[]string{"NMI:"}, "error", "critical"},

	// AMR / Roboshop application errors.
	{[]string{"[Fatal]", "[FATAL]"}, "error", "critical"},
	{[]string{"[Error]", "[ERROR]", "exception", "Exception"}, "error", "high"},
	{[]string{"scene load failed", "smap load failed", "robot.cp"}, "error", "high"},
	{[]string{"addr2line"}, "crash", "high"},

	// Update / dependency warnings.
	{[]string{"update available", "Update available", "apt-get upgrade", "needs update"}, "update", "low"},
	{[]string{"security update", "Security update"}, "update", "medium"},

	// General errors / warnings.
	{[]string{" error ", " ERROR ", "Error:", "error:"}, "error", "medium"},
	{[]string{" warning ", " WARNING ", "Warning:"}, "warning", "low"},
	{[]string{"critical", "CRITICAL"}, "error", "high"},
}

var rebootSkipSources = map[string]bool{
	"system_info": true,
}

var shutdownRebootTypes = map[string]bool{
	"ubuntu_server_shutdown": true,
	"ubuntu_server_reboot":   true,
	"proxmox_host_shutdown":  true,
	"proxmox_host_reboot":    true,
	"vm_stopped":             true,
	"vm_reboot":              true,
}

func ParseLine(line, source string, serverID int) *models.LogEvent {
	if strings.TrimSpace(line) == "" {
		return nil
	}
	if strings.TrimSpace(line) == "-- No entries --" {
		return nil
	}

	if strings.HasPrefix(strings.TrimSpace(line), "reboot") &&
		strings.Contains(line, "system boot") {
		return nil
	}

	if strings.Contains(line, "Failed to make thread") && strings.Contains(line, "realtime scheduled") {
		return nil
	}
	if strings.Contains(line, "RealtimeKit1") {
		return nil
	}
	if strings.Contains(line, "Normal Shutdown") || strings.Contains(line, "normal disconnect") {
		return nil
	}
	if strings.Contains(line, "SSL_shutdown") || strings.Contains(line, "CrowdStrike") {
		return nil
	}
	ts := extractTimestamp(line)
	matchLine := strings.ToLower(line)
	if isAdminEvidenceSearch(matchLine) {
		return newEvent(serverID, ts, "admin_evidence_search", "low", line, source)
	}
	if strings.Contains(line, "TTY=pts") && strings.Contains(line, "COMMAND=") {
		return nil
	}
	if strings.Contains(line, "TTY=tty") && strings.Contains(line, "COMMAND=") {
		return nil
	}
	if strings.Contains(line, "(command continued)") {
		return nil
	}

	if source == "rds_network_neighbors" {
		return newEvent(serverID, ts, "unknown", "low", line, source)
	}
	if isTemplateCodeReference(matchLine) {
		return newEvent(serverID, ts, "template_code_reference", "low", line, source)
	}
	if severity, ok := classifyPackageUpdate(matchLine, source); ok {
		return newEvent(serverID, ts, "update", severity, line, source)
	}
	if severity, ok := classifyWarLinkFailure(matchLine, source); ok {
		return newEvent(serverID, ts, "warlink_failure", severity, line, source)
	}
	if severity, ok := classifyBattery(matchLine, source); ok {
		if hasAny(matchLine, "error", "fault", "low", "power low", "failed", "voltage") {
			return newEvent(serverID, ts, "battery_error", severity, line, source)
		}
		return newEvent(serverID, ts, "battery_status", severity, line, source)
	}
	if severity, ok := classifyAMRGoTargetStation(matchLine, source); ok {
		return newEvent(serverID, ts, "amr_gotarget_station", severity, line, source)
	}
	if severity, ok := classifyAMRDockCommand(matchLine, source); ok {
		return newEvent(serverID, ts, "amr_dock_command", severity, line, source)
	}
	if severity, ok := classifyAMRChargeCommand(matchLine, source); ok {
		return newEvent(serverID, ts, "amr_charge_command", severity, line, source)
	}
	if severity, ok := classifySettingsDefaulted(matchLine, source); ok {
		return newEvent(serverID, ts, "rds_settings_defaulted", severity, line, source)
	}
	if severity, ok := classifySettingsReset(matchLine, source); ok {
		return newEvent(serverID, ts, "rds_settings_reset", severity, line, source)
	}
	if severity, ok := classifyRDSUpgradeReset(matchLine, source); ok {
		return newEvent(serverID, ts, "rds_upgrade_reset", severity, line, source)
	}
	if severity, ok := classifyRDSActivationIssue(matchLine, source); ok {
		return newEvent(serverID, ts, "rds_core_activation_issue", severity, line, source)
	}
	if severity, ok := classifyRDSSceneMapError(matchLine, source); ok {
		return newEvent(serverID, ts, "rds_scene_map_error", severity, line, source)
	}
	if severity, ok := classifyChargeDIChange(matchLine, source); ok {
		return newEvent(serverID, ts, "roboshop_chargedi_change", severity, line, source)
	}
	if severity, ok := classifyRDSModelUpdate(matchLine, source); ok {
		return newEvent(serverID, ts, "rds_model_update", severity, line, source)
	}
	if severity, ok := classifyRoboshopChargeCommand(matchLine, source); ok {
		return newEvent(serverID, ts, "roboshop_charge_command", severity, line, source)
	}
	if severity, ok := classifyRDSMapUpdate(matchLine, source); ok {
		return newEvent(serverID, ts, "rds_map_update", severity, line, source)
	}
	if severity, ok := classifyRDSCoreIssue(matchLine, source); ok {
		return newEvent(serverID, ts, "rds_core_issue", severity, line, source)
	}
	if strings.HasPrefix(source, "proxmox") && isProxmoxAccessLog(matchLine) {
		// Most pveproxy access-log lines are routine dashboard read polling
		// (GET /api2/json/.../config, /pending, /status/current). Only treat a
		// line as login/admin activity when it is a write method or a
		// console/login endpoint; otherwise drop the polling noise entirely.
		if isProxmoxAdminAction(matchLine) {
			return newEvent(serverID, ts, "ssh_login_activity", "low", line, source)
		}
		return nil
	}
	if strings.HasPrefix(source, "proxmox") && !strings.Contains(source, "root_history") && hasAny(matchLine, "oom", "out of memory", "killed process", "oom-killer", "oom-kill") {
		if hasAny(matchLine, "qemu", "kvm", "qemu.slice", ".scope", "vm ") {
			return newEvent(serverID, ts, "vm_killed_by_oom", "critical", line, source)
		}
		return newEvent(serverID, ts, "host_memory_exhaustion", "critical", line, source)
	}

	for _, r := range rules {
		if shutdownRebootTypes[r.eventType] && rebootSkipSources[source] {
			continue
		}

		for _, kw := range r.keywords {
			if strings.Contains(matchLine, strings.ToLower(kw)) {
				return newEvent(serverID, ts, r.eventType, r.severity, line, source)
			}
		}
	}

	return newEvent(serverID, ts, "unknown", "low", line, source)
}

func classifyWarLinkFailure(line, source string) (string, bool) {
	source = strings.ToLower(source)
	if strings.Contains(source, "proxmox_host_memory") && hasAny(line, "/usr/bin/kvm", "qemu-server", " -name ") {
		return "", false
	}
	if !strings.Contains(line, "warlink") && !strings.Contains(line, "sendunitdatatransaction") && !strings.Contains(line, "writetag") {
		return "", false
	}
	if hasAny(line, "panic", "segfault", "core dumped", "fatal") {
		return "critical", true
	}
	if hasAny(line,
		"returned 500", "returned 502", "returned 503", "returned 504",
		"not connected", "sendunitdatatransaction", "deadman", "still failing",
		"failed", "failing", "connection refused", "writetag",
	) {
		return "high", true
	}
	if hasAny(line, "returned 4", "returned 5", "error") || (strings.Contains(line, "timeout") && hasAny(line, "warlink", "writetag", "readmultiple")) {
		return "medium", true
	}
	return "", false
}

func classifyPackageUpdate(line, source string) (string, bool) {
	source = strings.ToLower(source)
	if hasAny(line,
		"/var/log/apt/history.log", "/var/log/apt/term.log", "unattended-upgrade",
		"apt-get", "apt ", "aptitude", "dpkg", "dnf ", "yum ",
	) || hasAny(source, "apt", "dpkg", "package") {
		if hasAny(line, "fail", "failed", "error", "dpkg error", "sub-process") {
			return "medium", true
		}
		if hasAny(line, "security", "unattended-upgrade", "upgrade") {
			return "low", true
		}
		return "info", true
	}
	return "", false
}

func classifyBattery(line, source string) (string, bool) {
	if !isAMRSourceOrLine(line, source) {
		return "", false
	}
	if !hasAny(line,
		"battery", "battery_level", "batterylevel", "getbatterylevel",
		"robot_status_battery_req", "robot_status_battery_req_simple",
		"voltage", "soc=", "soc:", "power low",
	) {
		return "", false
	}
	if hasAny(line, "battery low", "battery fault", "battery error", "power low", "failed", "fault", "error") {
		return "high", true
	}
	return "info", true
}

func classifyAMRChargeCommand(line, source string) (string, bool) {
	if !isAMRSourceOrLine(line, source) {
		return "", false
	}
	if !hasAny(line,
		"robot_other_setchargingrelay_req", "setchargingrelay", "chargingrelay",
		"charge_req", "gocharge", "go_charge", "charging command",
	) && !(hasAny(line, "charge", "charging", "charger") && hasAny(line, "command", "cmd", "request", "req", "send", "sent")) {
		return "", false
	}
	if hasAny(line, "fail", "failed", "failure", "error", "timeout", "denied", "reject", "rejected", "returned 4", "returned 5") {
		return "high", true
	}
	return "info", true
}

func classifyAMRDockCommand(line, source string) (string, bool) {
	if !isAMRSourceOrLine(line, source) {
		return "", false
	}
	if !hasAny(line, "dock_req", "docking", "dock command", "go dock", "return dock", "charger dock") {
		return "", false
	}
	if hasAny(line, "fail", "failed", "failure", "error", "timeout", "denied", "reject", "rejected", "returned 4", "returned 5") {
		return "high", true
	}
	return "info", true
}

func classifyAMRGoTargetStation(line, source string) (string, bool) {
	if !isAMRSourceOrLine(line, source) {
		return "", false
	}
	if !hasAny(line, "robot_task_gotarget_req", "gotarget", "go target") {
		return "", false
	}
	if hasAny(line, "fail", "failed", "failure", "error", "timeout", "denied", "reject", "rejected") {
		return "high", true
	}
	return "medium", true
}

func classifySettingsDefaulted(line, source string) (string, bool) {
	if !isAMRSourceOrLine(line, source) {
		return "", false
	}
	if hasAny(line, "settings default", "defaulted", "factory default", "active:false", `echoid:""`, `echoid=""`, "features active:false") {
		return "high", true
	}
	return "", false
}

func classifySettingsReset(line, source string) (string, bool) {
	if !isAMRSourceOrLine(line, source) {
		return "", false
	}
	if hasAny(line, "config reset", "model reset", "settings reset", "reloadrobodmakeini", "empty config", "restore", "recover") ||
		(hasAny(line, "reset", "factory") && hasAny(line, "setting", "settings", "config", "model", "robod", "rds")) {
		return "high", true
	}
	return "", false
}

func classifyRDSUpgradeReset(line, source string) (string, bool) {
	if !isAMRSourceOrLine(line, source) {
		return "", false
	}
	if hasAny(line,
		"robot_core_upgrade_robot_req", "upgrade.zip", "upgradestatus",
		"startup.sh stop", "startup.sh start", "upgrade succeeded", "upgrade failed",
		"robod upgrade", "rds upgrade",
	) {
		if hasAny(line, "failed", "failure", "error") {
			return "high", true
		}
		return "medium", true
	}
	return "", false
}

func classifyRDSActivationIssue(line, source string) (string, bool) {
	if !isAMRSourceOrLine(line, source) {
		return "", false
	}
	if hasAny(line, "core is not activated", "rdscoreÃ¦Å“ÂªÃ¦Â¿â‚¬Ã¦Â´Â»", "license inactive", "activation failed", "active:false", `echoid:""`, `echoid=""`) {
		return "high", true
	}
	return "", false
}

func classifyRDSSceneMapError(line, source string) (string, bool) {
	if !isAMRSourceOrLine(line, source) {
		return "", false
	}
	if hasAny(line, "scene.zip error", "don't contain rds.scene", "doesn't contain rds.scene", "rds.scene", "scene cannot be uploaded during task execution", "map md5", "model_md5") ||
		(hasAny(line, "map upload", "scene upload", "smap", "scene") && hasAny(line, "fail", "failed", "failure", "error", "cannot")) {
		return "high", true
	}
	return "", false
}

func classifyRDSMapUpdate(line, source string) (string, bool) {
	source = strings.ToLower(source)
	sourceOK := strings.Contains(source, "rds") ||
		strings.Contains(source, "roboshop") ||
		strings.Contains(source, "journald_amr") ||
		strings.Contains(line, "roboshop") ||
		strings.Contains(line, "rds")
	if !sourceOK {
		return "", false
	}
	hasSubject := hasAny(line, " map", "map=", "map:", ".map", "smap", "scene")
	hasAction := hasAny(line, "push", "upload", "update", "deploy", "load", "save", "publish", "import")
	if !hasSubject || !hasAction {
		return "", false
	}
	if hasAny(line, "fail", "failed", "failure", "error", "break", "broken", "rollback") {
		return "high", true
	}
	return "info", true
}

func classifyRDSModelUpdate(line, source string) (string, bool) {
	source = strings.ToLower(source)
	sourceOK := strings.Contains(source, "rds") ||
		strings.Contains(source, "roboshop") ||
		strings.Contains(source, "journald_amr") ||
		strings.Contains(line, "roboshop") ||
		strings.Contains(line, "rds") ||
		strings.Contains(line, "rdscore")
	if !sourceOK {
		return "", false
	}
	hasModelSubject := hasAny(line,
		"model file", "models/", "/models/", "robot.cp", ".cp",
		"model=", "model:", "model name", "robot model", "md5", "checksum",
	)
	hasAction := hasAny(line,
		"modify", "modified", "change", "changed", "update", "updated",
		"save", "saved", "write", "written", "load", "loaded",
		"open file failed", "no such file", "delete", "deleted",
	)
	if !hasModelSubject || !hasAction {
		return "", false
	}
	if hasAny(line, "fail", "failed", "failure", "error", "no such file", "denied", "rollback") {
		return "high", true
	}
	return "info", true
}

func classifyRoboshopChargeCommand(line, source string) (string, bool) {
	source = strings.ToLower(source)
	sourceOK := strings.Contains(source, "roboshop") ||
		strings.Contains(source, "rds") ||
		strings.Contains(source, "journald_amr") ||
		strings.Contains(line, "roboshop") ||
		strings.Contains(line, "rdscore") ||
		strings.Contains(line, "rds")
	if !sourceOK {
		return "", false
	}
	hasCharge := hasAny(line, "charge", "charging", "charger", "dock", "docking")
	hasCommand := hasAny(line,
		"command", "cmd", "order", "task", "mission", "dispatch",
		"send", "sent", "request", "requested", "post", "api", "execute",
	)
	if !hasCharge || !hasCommand {
		return "", false
	}
	if hasAny(line, "fail", "failed", "failure", "error", "timeout", "denied", "reject", "rejected", "returned 4", "returned 5") {
		return "high", true
	}
	return "info", true
}

func classifyChargeDIChange(line, source string) (string, bool) {
	source = strings.ToLower(source)
	sourceOK := strings.Contains(source, "roboshop") ||
		strings.Contains(source, "rds") ||
		strings.Contains(source, "journald_amr") ||
		strings.Contains(line, "roboshop") ||
		strings.Contains(line, "rdscore") ||
		strings.Contains(line, "rds")
	if !sourceOK {
		return "", false
	}
	hasChargeDI := hasAny(line, "chargedi", "charge_di", "charge-di", "charge di", "chargingdi", "charging_di", "charging di")
	hasAction := hasAny(line,
		"edit", "edited", "apply", "applied", "set", "change", "changed",
		"update", "updated", "trigger", "triggered", "model", "config",
		"comment", "commented", "save", "saved", "write", "written",
	)
	if !hasChargeDI || !hasAction {
		return "", false
	}
	if hasAny(line, "break", "broke", "bad", "fail", "failed", "failure", "error", "rollback") {
		return "high", true
	}
	return "info", true
}

func classifyRDSCoreIssue(line, source string) (string, bool) {
	source = strings.ToLower(source)
	sourceOK := strings.Contains(source, "rds") ||
		strings.Contains(source, "roboshop") ||
		strings.Contains(source, "journald_amr") ||
		strings.Contains(line, "rdscore") ||
		strings.Contains(line, "rds") ||
		strings.Contains(line, "roboshop")
	if !sourceOK {
		return "", false
	}
	if hasAny(line,
		"unconnectedstate", "closingstate", "slottcperror", "add device failed",
		"warlink", "sendunitdatatransaction", "writetag",
	) {
		return "", false
	}
	if hasAny(line, "map push", "map upload", "scene upload", "smap") {
		return "", false
	}
	if hasAny(line, "panic", "segfault", "core dumped", "fatal") {
		return "critical", true
	}
	if hasAny(line,
		"failed", "failure", "exception", "error", "timeout", "timed out",
		"connection refused", "remote host closed", "not connected", "disconnect",
		"database", "mysql", "postgres", "api returned 5", "returned 500",
	) {
		return "high", true
	}
	if hasAny(line, "warning", "retry", "unavailable", "degraded") {
		return "medium", true
	}
	return "", false
}

func isAMRSourceOrLine(line, source string) bool {
	source = strings.ToLower(source)
	return strings.Contains(source, "rds") ||
		strings.Contains(source, "roboshop") ||
		strings.Contains(source, "robod") ||
		strings.Contains(source, "journald_amr") ||
		strings.Contains(line, "rds") ||
		strings.Contains(line, "rdscore") ||
		strings.Contains(line, "roboshop") ||
		strings.Contains(line, "robod") ||
		strings.Contains(line, "robot_")
}

func isAdminEvidenceSearch(line string) bool {
	return strings.Contains(line, "command=/usr/bin/grep") ||
		strings.Contains(line, "command=/bin/grep") ||
		strings.Contains(line, "command=/usr/bin/journalctl") ||
		strings.Contains(line, "command=/bin/journalctl") ||
		(strings.Contains(line, "command=/usr/bin/bash") && hasAny(line, " grep ", "'grep", "\"grep", " journalctl ", "'journalctl", "\"journalctl")) ||
		(strings.Contains(line, "command=/bin/bash") && hasAny(line, " grep ", "'grep", "\"grep", " journalctl ", "'journalctl", "\"journalctl")) ||
		(strings.Contains(line, "sudo") && hasAny(line, " grep ", "'grep", "\"grep", " journalctl ", "'journalctl", "\"journalctl")) ||
		(strings.Contains(line, "sudo[") && hasAny(line, "grep", "journalctl")) ||
		hasAny(line,
			"journalctl ", "journalctl --since",
			" grep ", " egrep ", " zgrep ",
			"grep -r", "grep -h", "grep -i", "grep -e",
			"grep -ie", "grep -he", "grep -iv", "grep -rni",
		)
}

func isTemplateCodeReference(line string) bool {
	return hasAny(line,
		"/opt/roboshop/bin/appinfo/setting/editor/seer-task/",
		"python-sdk/rbk/rbklib.py",
		"project-templates",
		"static/js/",
		"/assets/index-",
		"config/block",
		"template task",
		"task template",
	)
}

func isProxmoxAccessLog(line string) bool {
	return strings.Contains(line, "pveproxy/access.log") ||
		strings.Contains(line, "/api2/json/") ||
		strings.Contains(line, "/api2/extjs/") ||
		strings.Contains(line, "/api2/html/")
}

// isProxmoxAdminAction reports whether a Proxmox pveproxy access-log line is a
// genuine administrative action worth surfacing, as opposed to routine dashboard
// read polling. It matches either a mutating HTTP method or a console/login
// endpoint (which can be issued over GET, e.g. VNC websocket setup).
func isProxmoxAdminAction(line string) bool {
	if hasAny(line,
		"\"post ", "\"put ", "\"delete ", "\"patch ") {
		return true
	}
	return hasAny(line,
		"access/ticket",
		"vncproxy",
		"vncwebsocket",
		"vncticket",
		"termproxy",
		"spiceproxy",
		"/console",
		"/vnc")
}

func hasAny(s string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

func newEvent(serverID int, ts time.Time, eventType, severity, line, source string) *models.LogEvent {
	msg := strings.TrimSpace(line)
	if len(msg) > 500 {
		msg = msg[:500]
	}
	return &models.LogEvent{
		ServerID:  serverID,
		Timestamp: ts,
		EventType: eventType,
		Severity:  severity,
		Message:   msg,
		Source:    source,
		RawLine:   line,
	}
}

func ParseOutput(output, source string, serverID int) []models.LogEvent {
	var events []models.LogEvent
	seen := make(map[string]bool)
	unknownCount := 0

	for _, line := range strings.Split(output, "\n") {
		ev := ParseLine(line, source, serverID)
		if ev == nil {
			continue
		}
		if ev.EventType == "unknown" {
			unknownCount++
			if unknownCount > 100 {
				continue
			}
		}
		key := ev.EventType + ev.Message
		if seen[key] {
			continue
		}
		seen[key] = true
		events = append(events, *ev)
	}
	return events
}

var isoFormats = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05.000000-0700",
	"2006-01-02T15:04:05-0700",
	"2006-01-02T15:04:05+0000",
	"2006-01-02 15:04:05",
}

func extractTimestamp(line string) time.Time {
	now := time.Now().UTC()
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return now
	}

	for _, f := range isoFormats {
		if t, err := time.Parse(f, parts[0]); err == nil {
			return t.UTC()
		}
		if len(parts) > 1 {
			if t, err := time.Parse(f, parts[0]+" "+parts[1]); err == nil {
				return t.UTC()
			}
		}
	}

	if len(parts) >= 3 {
		day := parts[1]
		var raw string
		if len(day) == 1 {
			raw = parts[0] + "  " + day + " " + parts[2]
		} else {
			raw = parts[0] + " " + day + " " + parts[2]
		}
		for _, f := range []string{"Jan  2 15:04:05", "Jan 02 15:04:05"} {
			if t, err := time.Parse(f, raw); err == nil {
				t = time.Date(now.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.UTC)
				if t.After(now.Add(24 * time.Hour)) {
					t = t.AddDate(-1, 0, 0)
				}
				return t
			}
		}
	}

	return now
}
