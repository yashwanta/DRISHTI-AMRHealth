package handlers

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"drishti-amr-health/internal/models"
)

type proxmoxAccessDetails struct {
	ClientIP     string
	User         string
	Time         string
	Method       string
	Path         string
	ResourceType string
	ResourceID   string
	Action       string
}

type rdsMapDetails struct {
	Action string
	Status string
	User   string
	IP     string
	MAC    string
	Map    string
}

type rdsModelDetails struct {
	Status string
	User   string
	IP     string
	Model  string
	MD5    string
}

type chargeCommandDetails struct {
	Status string
	User   string
	IP     string
	Robot  string
	Action string
}

type chargeDIDetails struct {
	Effect string
	User   string
	IP     string
	Model  string
}

func enrichLogEvent(ev *models.LogEvent) {
	if ev == nil {
		return
	}
	normalizeEvidenceEventType(ev)
	ev.PlainEnglish = PlainEnglishLog(*ev)
	ev.RecommendedAction = RecommendedAction(*ev)
	class, confidence, badges, targets := AMREvidenceClassification(*ev)
	ev.EvidenceClass = class
	ev.EvidenceConfidence = confidence
	ev.EvidenceBadges = badges
	ev.TargetIDs = targets
	if class != "" {
		executed := class == "executed_command"
		ev.ExecutionEvidence = &executed
	}
}

func normalizeEvidenceEventType(ev *models.LogEvent) {
	raw := strings.TrimSpace(ev.RawLine)
	if raw == "" {
		raw = strings.TrimSpace(ev.Message)
	}
	lower := strings.ToLower(raw)
	if isAdminEvidenceOnly(lower) {
		ev.EventType = "admin_evidence_search"
		ev.Severity = "low"
		return
	}
	if isTemplateOrCodeOnly(lower) {
		ev.EventType = "template_code_reference"
		ev.Severity = "low"
	}
}

func PlainEnglishLog(ev models.LogEvent) string {
	raw := strings.TrimSpace(ev.RawLine)
	if raw == "" {
		raw = strings.TrimSpace(ev.Message)
	}
	lower := strings.ToLower(raw)

	if isPackageUpdateLog(lower) {
		switch {
		case strings.Contains(lower, "unattended-upgrade"):
			return "Ubuntu automatic updates ran on this server."
		case strings.Contains(lower, "apt-get") || strings.Contains(lower, "/var/log/apt/") || strings.Contains(lower, "dpkg"):
			return "Ubuntu package management activity was recorded on this server."
		case strings.Contains(lower, "dnf ") || strings.Contains(lower, "yum "):
			return "Linux package management activity was recorded on this server."
		}
	}
	if access := parseProxmoxAccessDetails(raw); access != nil {
		if access.Action == "console" && access.ResourceType != "" && access.ResourceID != "" {
			return fmt.Sprintf("Someone using %s opened the Proxmox console/VNC session for %s %s from IP %s on %s.", access.User, access.ResourceType, access.ResourceID, access.ClientIP, access.Time)
		}
		return fmt.Sprintf("Someone using %s made a Proxmox API request from IP %s on %s.", access.User, access.ClientIP, access.Time)
	}
	if ev.EventType == "rds_map_update" {
		details := parseRDSMapDetails(raw)
		parts := []string{"An RDS map update was recorded"}
		if details.Status != "" {
			parts = append(parts, "with status "+details.Status)
		}
		if details.User != "" {
			parts = append(parts, "by "+details.User)
		}
		if details.IP != "" {
			parts = append(parts, "from IP "+details.IP)
		}
		if details.MAC != "" {
			parts = append(parts, "with MAC "+details.MAC)
		}
		if details.Map != "" {
			parts = append(parts, "for map "+details.Map)
		}
		return strings.Join(parts, " ") + "."
	}
	if ev.EventType == "rds_model_update" {
		details := parseRDSModelDetails(raw)
		parts := []string{"An RDS/Roboshop model-file change was recorded"}
		if details.Status != "" {
			parts = append(parts, "with status "+details.Status)
		}
		if details.User != "" {
			parts = append(parts, "by "+details.User)
		}
		if details.IP != "" {
			parts = append(parts, "from IP "+details.IP)
		}
		if details.Model != "" {
			parts = append(parts, "for model file "+details.Model)
		}
		if details.MD5 != "" {
			parts = append(parts, "with MD5 "+details.MD5)
		}
		return strings.Join(parts, " ") + "."
	}
	if ev.EventType == "roboshop_charge_command" {
		details := parseChargeCommandDetails(raw)
		parts := []string{"A Roboshop/RDS charge command was recorded"}
		if details.Status != "" {
			parts = append(parts, "with status "+details.Status)
		}
		if details.Action != "" {
			parts = append(parts, "for "+details.Action)
		}
		if details.Robot != "" {
			parts = append(parts, "on robot "+details.Robot)
		}
		if details.User != "" {
			parts = append(parts, "by "+details.User)
		}
		if details.IP != "" {
			parts = append(parts, "from IP "+details.IP)
		}
		return strings.Join(parts, " ") + "."
	}
	if ev.EventType == "roboshop_chargedi_change" {
		details := parseChargeDIDetails(raw)
		parts := []string{"A Roboshop/RDS chargeDI change was recorded"}
		if details.Effect != "" {
			parts = append(parts, "with effect "+details.Effect)
		}
		if details.User != "" {
			parts = append(parts, "by "+details.User)
		}
		if details.IP != "" {
			parts = append(parts, "from IP "+details.IP)
		}
		if details.Model != "" {
			parts = append(parts, "for model/config "+details.Model)
		}
		return strings.Join(parts, " ") + "."
	}
	if ev.EventType == "rds_core_issue" {
		reason := "RDS logged a core application issue"
		switch {
		case strings.Contains(lower, "database") || strings.Contains(lower, "mysql") || strings.Contains(lower, "postgres"):
			reason = "RDS appears to be having database trouble"
		case strings.Contains(lower, "timeout") || strings.Contains(lower, "timed out"):
			reason = "RDS operation timed out"
		case strings.Contains(lower, "returned 500") || strings.Contains(lower, "api"):
			reason = "RDS API returned an error"
		case strings.Contains(lower, "not connected") || strings.Contains(lower, "connection refused") || strings.Contains(lower, "disconnect"):
			reason = "RDS lost or could not establish a connection"
		case strings.Contains(lower, "fatal") || strings.Contains(lower, "panic") || strings.Contains(lower, "core dumped"):
			reason = "RDS application process crashed or hit a fatal error"
		}
		return reason + "."
	}
	if ev.EventType == "warlink_failure" {
		details := parseWarLinkDetails(raw)
		parts := []string{"WarLink could not complete a PLC communication"}
		if details.Operation != "" {
			parts = append(parts, "for "+details.Operation)
		}
		if details.Tag != "" {
			parts = append(parts, "tag "+details.Tag)
		}
		if details.Group != "" {
			parts = append(parts, "in group "+details.Group)
		}
		if details.Duration != "" {
			parts = append(parts, "after failing for "+details.Duration)
		}
		if details.Attempts != "" {
			parts = append(parts, "across "+details.Attempts+" attempts")
		}
		if details.Reason != "" {
			parts = append(parts, "because "+details.Reason)
		}
		return strings.Join(parts, " ") + "."
	}
	if strings.HasPrefix(ev.EventType, "amr_") ||
		strings.HasPrefix(ev.EventType, "battery_") ||
		strings.HasPrefix(ev.EventType, "rds_settings_") ||
		ev.EventType == "rds_upgrade_reset" ||
		ev.EventType == "rds_core_activation_issue" ||
		ev.EventType == "rds_scene_map_error" ||
		ev.EventType == "admin_evidence_search" ||
		ev.EventType == "template_code_reference" ||
		ev.EventType == "not_execution_evidence" {
		return amrRDSPlainEnglish(ev, raw)
	}

	if robotIP := extractRobotIP(raw); ev.EventType == "robot_offline" && robotIP != "" {
		if strings.Contains(lower, "connection refused") {
			return fmt.Sprintf("Robot %s refused the TCP connection.", robotIP)
		}
		if strings.Contains(lower, "remote host closed") {
			return fmt.Sprintf("Robot %s closed the connection unexpectedly.", robotIP)
		}
		if strings.Contains(lower, "timeout") {
			return fmt.Sprintf("The server timed out while trying to reach robot %s.", robotIP)
		}
		return fmt.Sprintf("Robot %s is not connected to the server.", robotIP)
	}
	if ev.EventType == "robot_offline" {
		if endpoint := extractBracketEndpoint(raw); endpoint != "" && strings.Contains(lower, "add device failed") {
			return fmt.Sprintf("FleetManager could not add or reconnect robot %s.", endpoint)
		}
	}

	switch ev.EventType {
	case "ubuntu_server_shutdown":
		return "The Ubuntu server recorded a shutdown sequence."
	case "ubuntu_server_reboot":
		return "The Ubuntu server recorded a reboot sequence."
	case "proxmox_host_shutdown":
		return "The Proxmox host recorded a shutdown-related event."
	case "proxmox_host_reboot":
		return "The Proxmox host recorded a reboot-related event."
	case "vm_stopped":
		return "A virtual machine was stopped or received a shutdown event."
	case "vm_started":
		return "A virtual machine started or returned to running state."
	case "vm_reboot":
		return "A virtual machine recorded or received a reboot event."
	case "vm_killed_by_oom":
		if ev.OOMAnalysis != nil && ev.OOMAnalysis.KilledVMID != "" {
			label := "VM " + ev.OOMAnalysis.KilledVMID
			if ev.OOMAnalysis.KilledVMName != "" {
				label += " (" + ev.OOMAnalysis.KilledVMName + ")"
			}
			return label + " was killed by the Proxmox OOM killer."
		}
		return "A VM process appears to have been killed during an out-of-memory condition."
	case "host_memory_exhaustion":
		return "The host reported memory exhaustion."
	case "swap_full":
		return "The host reported full or exhausted swap."
	case "backup_job":
		return "A backup job or backup-system event was recorded."
	case "backup_found_vm_stopped":
		return "A backup job found the VM was already stopped or not running."
	case "ha_action":
		return "A Proxmox HA action was recorded."
	case "disk_smart_issue", "disk_error":
		return "Storage, disk, filesystem, or SMART health evidence was recorded."
	case "network_dhcp_failure":
		return "A network, DHCP, link, or reachability failure was recorded."
	case "ssh_login_activity":
		return "SSH, sudo, login, or Proxmox access activity was recorded."
	case "rds_core_issue":
		return "RDS core logged an API, database, timeout, service, or connection issue."
	case "rds_map_update":
		return "An RDS map update, upload, deploy, or push event was recorded."
	case "rds_model_update":
		return "An RDS or Roboshop model file, MD5, or checksum change event was recorded."
	case "roboshop_charge_command":
		return "A Roboshop or RDS robot charge/dock command event was recorded."
	case "roboshop_chargedi_change":
		return "A Roboshop or RDS chargeDI edit, apply, trigger, model, or comment event was recorded."
	case "warlink_failure":
		return "WarLink reported a PLC communication failure."
	case "battery_error":
		return "A battery warning, fault, low-power, voltage, or SOC problem was recorded."
	case "battery_status":
		return "Battery status or battery level information was recorded."
	case "amr_charge_command":
		return "An AMR charge-related command was found in the logs."
	case "amr_dock_command":
		return "An AMR dock-related command was found in the logs."
	case "amr_gotarget_station":
		return "An AMR go-target command was found; confirm the target is a charger or station before calling it a charge command."
	case "rds_settings_reset":
		return "RDS or Robod settings reset activity was recorded."
	case "rds_settings_defaulted":
		return "RDS or Robod settings appear to have returned to defaults."
	case "rds_upgrade_reset":
		return "RDS or Robod upgrade/reset activity was recorded."
	case "rds_core_activation_issue":
		return "RDS Core activation or license evidence was recorded."
	case "rds_scene_map_error":
		return "RDS scene/map upload or validation error evidence was recorded."
	case "admin_evidence_search":
		return "An administrator searched logs for evidence; this is not robot execution."
	case "template_code_reference":
		return "A template, code, or config reference matched; this is not robot execution."
	case "not_execution_evidence":
		return "This line is evidence context only, not an executed AMR command."
	case "service_failure":
		return "A system service failed or entered a failed state."
	case "ubuntu_log_gap":
		return "Ubuntu logs show a gap, rotation, or time discontinuity."
	case "power_network_event":
		return "A power or network signal was recorded."
	case "crash":
		return "A crash, kernel panic, segfault, or core dump event was recorded."
	case "update":
		return "Package update or package manager activity was recorded."
	case "robot_online":
		return "A robot connection returned to an online state."
	case "unknown":
		return "This log line did not match a known category rule."
	}

	if strings.Contains(lower, "segfault") {
		return "A process stopped after a memory access fault."
	}
	if strings.Contains(lower, "out of memory") || strings.Contains(lower, "oom") {
		return "The system reported memory pressure or an OOM kill."
	}
	return "This event was recorded by SiteOps."
}

func RecommendedAction(ev models.LogEvent) string {
	raw := strings.TrimSpace(ev.RawLine)
	if raw == "" {
		raw = strings.TrimSpace(ev.Message)
	}
	lower := strings.ToLower(raw)

	if access := parseProxmoxAccessDetails(raw); access != nil {
		return fmt.Sprintf("Reference only. Concern only if you did not do it, do not recognize %s, or %s should not have been used.", access.ClientIP, access.User)
	}
	if ev.EventType == "update" || isPackageUpdateLog(lower) {
		if strings.Contains(lower, "unattended-upgrade") {
			return "Reference only. Automatic Ubuntu updates are normal unless services broke, packages failed, or the server rebooted unexpectedly afterward."
		}
		return "Review only if this package activity was unexpected or happened right before a service issue."
	}
	if ev.EventType == "rds_map_update" {
		details := parseRDSMapDetails(raw)
		if details.Status == "failed" || details.Status == "broken" {
			return "Review the RDS map update result, confirm which user/IP pushed it, and verify robots can load or use the updated map."
		}
		return "Reference only. Confirm the user/IP was expected and verify robot behavior after the map update."
	}
	if ev.EventType == "rds_model_update" {
		return "Reference only if expected. Confirm who changed the model file or MD5/checksum, verify the source IP/user, and confirm robots can load the intended model after the change."
	}
	if ev.EventType == "roboshop_charge_command" {
		return "Confirm whether the charge/dock command was expected, which robot received it, and whether the command succeeded. If it failed, check robot reachability, charger/dock state, and Roboshop/RDS command logs."
	}
	if ev.EventType == "roboshop_chargedi_change" {
		return "Review the chargeDI change timeline, confirm the source IP/user was expected, and compare nearby robot charging behavior to see whether this change broke or restored charging."
	}
	if ev.EventType == "rds_core_issue" {
		return "Check rdscore/RDS service status, recent RDS application logs, database connectivity, disk space, and API health. Keep the raw log for vendor or engineering review."
	}
	if ev.EventType == "warlink_failure" {
		return "Most likely reason: WarLink does not currently have an established PLC connection. Check PLC power/network reachability from Springfield Edge, the shingo-edge/WarLink service connection state, and the affected PLC route/tag before restarting the service."
	}
	if action := amrRDSRecommendedAction(ev, lower); action != "" {
		return action
	}

	if ev.EventType == "robot_offline" {
		if strings.Contains(lower, "timeout") {
			return "Check robot power and network reachability from the server."
		}
		if strings.Contains(lower, "remote host closed") {
			return "Confirm whether the robot was restarted or intentionally disconnected."
		}
		return "Verify robot power, network cabling or Wi-Fi, and the robot service state."
	}
	if ev.EventType == "vm_killed_by_oom" || ev.EventType == "host_memory_exhaustion" || ev.EventType == "swap_full" {
		if ev.OOMAnalysis != nil && ev.OOMAnalysis.Recommendation != "" {
			return ev.OOMAnalysis.Recommendation
		}
		return "Review Proxmox host memory pressure, VM reservations, ballooning, and high-memory processes."
	}
	if strings.Contains(ev.EventType, "shutdown") || strings.Contains(ev.EventType, "reboot") || ev.EventType == "vm_stopped" {
		return "Confirm whether this was planned maintenance. If not, compare nearby power, UPS, and network events."
	}
	if ev.EventType == "ssh_login_activity" {
		return "Confirm whether this was expected administrative activity."
	}
	return ""
}

func isPackageUpdateLog(lower string) bool {
	return strings.Contains(lower, "/var/log/apt/history.log") ||
		strings.Contains(lower, "/var/log/apt/term.log") ||
		strings.Contains(lower, "unattended-upgrade") ||
		strings.Contains(lower, "apt-get") ||
		strings.Contains(lower, "apt ") ||
		strings.Contains(lower, "dpkg") ||
		strings.Contains(lower, "dnf ") ||
		strings.Contains(lower, "yum ")
}

func AMREvidenceClassification(ev models.LogEvent) (string, string, []string, []string) {
	raw := strings.TrimSpace(ev.RawLine)
	if raw == "" {
		raw = strings.TrimSpace(ev.Message)
	}
	lower := strings.ToLower(raw)
	targets := extractTargetIDs(raw)
	badges := []string{}

	switch {
	case ev.EventType == "admin_evidence_search" || isAdminEvidenceOnly(lower):
		return "admin_evidence_search", "low", []string{"Admin search only", "Not execution evidence"}, targets
	case ev.EventType == "template_code_reference" || isTemplateOrCodeOnly(lower):
		return "template_code_reference", "low", []string{"Template/code only", "Not execution evidence"}, targets
	case ev.EventType == "not_execution_evidence":
		return "not_execution_evidence", "low", []string{"Not execution evidence"}, targets
	}

	if ev.EventType == "battery_error" {
		badges = append(badges, "Battery issue")
	}
	if ev.EventType == "battery_status" {
		badges = append(badges, "Battery status")
	}
	if ev.EventType == "amr_gotarget_station" {
		badges = append(badges, "Possible station target")
	}
	if ev.EventType == "rds_upgrade_reset" {
		badges = append(badges, "Upgrade/reset event")
	}
	if ev.EventType == "rds_settings_defaulted" {
		badges = append(badges, "Settings defaulted")
	}
	if ev.EventType == "rds_settings_reset" {
		badges = append(badges, "Settings reset")
	}
	if ev.EventType == "rds_scene_map_error" {
		badges = append(badges, "Scene/map issue")
	}
	if ev.EventType == "rds_core_activation_issue" {
		badges = append(badges, "Activation issue")
	}

	if isExecutedRobotCommand(lower) {
		return "executed_command", "high", prependBadge("Executed command", badges), targets
	}
	if isAMRRuntimeEvidence(ev.EventType, lower, ev.Source) {
		if ev.EventType == "amr_charge_command" || ev.EventType == "amr_dock_command" {
			badges = prependBadge("Possible executed command", badges)
		}
		return "runtime_evidence", "medium", badges, targets
	}
	if len(badges) > 0 || len(targets) > 0 {
		return "supporting_evidence", "medium", badges, targets
	}
	return "", "", nil, targets
}

func amrRDSPlainEnglish(ev models.LogEvent, raw string) string {
	_, confidence, _, targets := AMREvidenceClassification(ev)
	targetText := ""
	if len(targets) > 0 {
		targetText = " Target ID found: " + strings.Join(targets, ", ") + "."
	}
	confText := ""
	if confidence != "" {
		confText = " Confidence: " + confidence + "."
	}
	switch ev.EventType {
	case "battery_error":
		return "Battery error or low-power evidence was found." + confText
	case "battery_status":
		return "Battery level/status evidence was found." + confText
	case "amr_charge_command":
		return "A charge command match was found in runtime AMR/RDS evidence." + confText
	case "amr_dock_command":
		return "A dock command match was found in runtime AMR/RDS evidence." + confText
	case "amr_gotarget_station":
		return "A go-target command was issued to a possible station/charger target." + targetText + confText
	case "rds_settings_reset":
		return "RDS/Robod settings reset activity was detected." + confText
	case "rds_settings_defaulted":
		return "RDS/Robod settings appear to have defaulted or become inactive." + confText
	case "rds_upgrade_reset":
		return "RDS/Robod upgrade or reset activity was detected near this log." + confText
	case "rds_core_activation_issue":
		return "RDS Core activation/license evidence was found." + confText
	case "rds_scene_map_error":
		return "RDS scene/map upload or validation error evidence was found." + confText
	case "admin_evidence_search":
		return "This row is an administrator log search command. It is useful for investigation history, but it is not evidence that the robot executed a battery, charge, dock, go-target, reset, or default command."
	case "template_code_reference":
		return "A template, source-code, or config file matched the keyword; this is reference evidence only, not robot execution."
	case "not_execution_evidence":
		return "This line is context evidence only and should not be counted as an executed AMR command."
	}
	return "AMR/RDS investigation evidence was recorded."
}

func amrRDSRecommendedAction(ev models.LogEvent, lower string) string {
	switch ev.EventType {
	case "admin_evidence_search":
		return "Do not count this as an AMR action. It only shows an administrator searched logs with grep or journalctl."
	case "template_code_reference", "not_execution_evidence":
		return "Use this as reference only. It matched code, config, or template text and does not prove a robot command executed."
	case "amr_gotarget_station":
		targets := extractTargetIDs(ev.RawLine + " " + ev.Message)
		if len(targets) > 0 {
			return "A go-target command was issued to target " + strings.Join(targets, ", ") + ". Confirm whether this target is configured as a charger/station point before calling it a charge command."
		}
		return "Confirm the target metadata before treating this go-target event as a charge/station command."
	case "rds_upgrade_reset":
		if strings.Contains(lower, "reset") || strings.Contains(lower, "default") || strings.Contains(lower, "active:false") {
			return "RDS/Robod upgrade/reset activity was detected near reset/default indicators. This is more likely to explain settings returning to default than a normal charge command."
		}
		return "Review upgrade status, startup.sh stop/start events, and nearby settings/default logs."
	case "rds_settings_reset", "rds_settings_defaulted":
		return "Check recent RDS/Robod upgrade, reset, restore, or config reload activity and compare robot settings before and after this timestamp."
	case "rds_core_activation_issue":
		return "Check RDS Core license/activation state, echoid, and whether active:false appeared after an upgrade or reset."
	case "rds_scene_map_error":
		return "Review the scene/map package contents, rds.scene presence, MD5/model_md5 values, and whether robots were executing tasks during upload."
	case "battery_error":
		return "Check robot battery voltage/SOC, charger contact, battery fault codes, and nearby charge/dock commands."
	case "battery_status":
		return "Use this as supporting battery context around charge/dock or disconnect events."
	case "amr_charge_command", "amr_dock_command":
		return "Verify the evidence confidence. High confidence requires a runtime Send or Client To Server robot request marker; template/admin-search matches should not count."
	}
	return ""
}

func isExecutedRobotCommand(lower string) bool {
	return regexp.MustCompile(`(?i)send:\[\d+\].*robot_[a-z0-9_]+_req`).MatchString(lower) ||
		regexp.MustCompile(`(?i)client to server:.*robot_[a-z0-9_]+_req`).MatchString(lower) ||
		((strings.Contains(lower, "roboshop.desktop") || strings.Contains(lower, "robod")) &&
			hasAnyLocal(lower, "send", "sending", "client to server") &&
			strings.Contains(lower, "robot_") &&
			strings.Contains(lower, "_req"))
}

func isAMRRuntimeEvidence(eventType, lower, source string) bool {
	source = strings.ToLower(source)
	if strings.Contains(source, "auth.log") || isAdminEvidenceOnly(lower) || isTemplateOrCodeOnly(lower) {
		return false
	}
	return strings.HasPrefix(eventType, "amr_") ||
		strings.HasPrefix(eventType, "battery_") ||
		strings.HasPrefix(eventType, "rds_") ||
		strings.Contains(lower, "rdscore") ||
		strings.Contains(lower, "roboshop") ||
		strings.Contains(lower, "robod") ||
		strings.Contains(lower, "robot_") ||
		strings.Contains(lower, "warlink")
}

func isAdminEvidenceOnly(lower string) bool {
	return strings.Contains(lower, "command=/usr/bin/grep") ||
		strings.Contains(lower, "command=/bin/grep") ||
		strings.Contains(lower, "command=/usr/bin/journalctl") ||
		strings.Contains(lower, "command=/bin/journalctl") ||
		(strings.Contains(lower, "command=/usr/bin/bash") && hasAnyLocal(lower, " grep ", "'grep", "\"grep", " journalctl ", "'journalctl", "\"journalctl")) ||
		(strings.Contains(lower, "command=/bin/bash") && hasAnyLocal(lower, " grep ", "'grep", "\"grep", " journalctl ", "'journalctl", "\"journalctl")) ||
		(strings.Contains(lower, "sudo") && hasAnyLocal(lower, " grep ", "'grep", "\"grep", " journalctl ", "'journalctl", "\"journalctl")) ||
		(strings.Contains(lower, "sudo[") && hasAnyLocal(lower, "grep", "journalctl")) ||
		hasAnyLocal(lower,
			"journalctl ", "journalctl --since",
			" grep ", " egrep ", " zgrep ",
			"grep -r", "grep -h", "grep -i", "grep -e",
			"grep -ie", "grep -he", "grep -iv", "grep -rni",
		)
}

func isTemplateOrCodeOnly(lower string) bool {
	return hasAnyLocal(lower,
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

func extractTargetIDs(raw string) []string {
	seen := map[string]bool{}
	var out []string
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)"id"\s*:\s*"(PP[0-9A-Za-z_-]+)"`),
		regexp.MustCompile(`(?i)\btarget(?:_id|id|)\s*[=:]\s*"?([A-Z]{1,4}[0-9]{1,5})"?`),
		regexp.MustCompile(`\b(PP[0-9A-Za-z_-]+)\b`),
	}
	for _, pattern := range patterns {
		for _, match := range pattern.FindAllStringSubmatch(raw, -1) {
			if len(match) < 2 {
				continue
			}
			id := strings.ToUpper(match[1])
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

func prependBadge(first string, rest []string) []string {
	out := []string{first}
	for _, badge := range rest {
		if badge != "" && badge != first {
			out = append(out, badge)
		}
	}
	return out
}

func hasAnyLocal(s string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

type warLinkDetails struct {
	Operation string
	Tag       string
	Group     string
	Reason    string
	Duration  string
	Attempts  string
}

func parseWarLinkDetails(raw string) warLinkDetails {
	details := warLinkDetails{}
	if match := regexp.MustCompile(`(?i)WarLink\s+(GET|POST|PUT|PATCH|DELETE)\s+([^\s:]+)`).FindStringSubmatch(raw); match != nil {
		details.Operation = strings.TrimSpace(match[1] + " " + match[2])
	}
	if match := regexp.MustCompile(`(?i)\btag=([A-Za-z0-9_.:-]+)`).FindStringSubmatch(raw); match != nil {
		details.Tag = match[1]
	}
	if match := regexp.MustCompile(`(?i)\bgroup=([A-Za-z0-9_.:-]+)`).FindStringSubmatch(raw); match != nil {
		details.Group = match[1]
	}
	if match := regexp.MustCompile(`(?i)still failing for\s+([^\s]+)`).FindStringSubmatch(raw); match != nil {
		details.Duration = match[1]
	}
	if match := regexp.MustCompile(`(?i)\((\d+)\s+attempts\)`).FindStringSubmatch(raw); match != nil {
		details.Attempts = match[1]
	}
	lower := strings.ToLower(raw)
	switch {
	case strings.Contains(lower, "not connected"):
		details.Reason = "the PLC connection was not established from WarLink"
	case strings.Contains(lower, "returned 500"):
		details.Reason = "WarLink returned HTTP 500 while talking to the PLC"
	case strings.Contains(lower, "timeout"):
		details.Reason = "the request timed out"
	case strings.Contains(lower, "deadman"):
		details.Reason = "the heartbeat/deadman signal was at risk"
	case strings.Contains(lower, "sendunitdatatransaction"):
		details.Reason = "the EtherNet/IP transaction failed"
	}
	return details
}

func parseProxmoxAccessDetails(raw string) *proxmoxAccessDetails {
	log := strings.TrimSpace(raw)
	if !strings.Contains(log, "pveproxy/access.log") && !strings.Contains(log, "/api2/") {
		return nil
	}
	match := regexp.MustCompile(`([0-9]{1,3}(?:\.[0-9]{1,3}){3})\s+-\s+(\S+)\s+\[([^\]]+)\]\s+"([A-Z]+)\s+([^"\s]+)`).FindStringSubmatch(log)
	if match == nil {
		match = regexp.MustCompile(`(?:::ffff:)?([0-9a-fA-F:.]+)\s+-\s+(\S+)\s+\[([^\]]+)\]\s+"([A-Z]+)\s+([^"\s]+)`).FindStringSubmatch(log)
	}
	if match == nil {
		return nil
	}
	path, err := url.QueryUnescape(match[5])
	if err != nil {
		path = match[5]
	}
	out := &proxmoxAccessDetails{
		ClientIP: match[1],
		User:     match[2],
		Time:     formatProxmoxAccessTime(match[3]),
		Method:   match[4],
		Path:     path,
		Action:   "api",
	}
	route := regexp.MustCompile(`/api2/(?:json|extjs|html)/nodes/([^/]+)/(lxc|qemu)/([^/]+)/([^?/\s]+)`).FindStringSubmatch(path)
	if route != nil {
		if route[2] == "lxc" {
			out.ResourceType = "LXC container"
		} else if route[2] == "qemu" {
			out.ResourceType = "VM"
		}
		out.ResourceID = route[3]
		if strings.Contains(route[4], "vnc") {
			out.Action = "console"
		}
	}
	if strings.Contains(path, "vnc") {
		out.Action = "console"
	}
	return out
}

func parseRDSMapDetails(raw string) rdsMapDetails {
	lower := strings.ToLower(raw)
	out := rdsMapDetails{Action: "map update"}
	switch {
	case strings.Contains(lower, "fail") || strings.Contains(lower, "error") || strings.Contains(lower, "rollback"):
		out.Status = "failed"
	case strings.Contains(lower, "break") || strings.Contains(lower, "broken"):
		out.Status = "broken"
	case strings.Contains(lower, "success") || strings.Contains(lower, "complete") || strings.Contains(lower, "finished") || strings.Contains(lower, " ok"):
		out.Status = "successful"
	}
	out.User = firstRegex(raw,
		`(?i)\buser(?:name)?[=: ]+([A-Za-z0-9_.@-]+)`,
		`(?i)\boperator[=: ]+([A-Za-z0-9_.@-]+)`,
		`(?i)\baccount[=: ]+([A-Za-z0-9_.@-]+)`,
		`(?i)\bby\s+([A-Za-z0-9_.@-]+)`,
	)
	out.IP = firstRegex(raw,
		`(?i)\b(?:client|source|remote|from|ip)[=: ]+([0-9]{1,3}(?:\.[0-9]{1,3}){3})`,
	)
	out.MAC = firstRegex(raw,
		`(?i)\b(?:mac|hwaddr|lladdr)[=: ]+(([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2})`,
		`\b(([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2})\b`,
	)
	out.Map = firstRegex(raw,
		`(?i)\bmap\s+name:\[([^\]]+)`,
		`(?i)\b(?:map|smap|scene)[=: ]+([A-Za-z0-9_.@:/-]+)`,
		`(?i)\b([A-Za-z0-9_.@:/-]+\.(?:smap|map|json|zip))\b`,
	)
	return out
}

func parseRDSModelDetails(raw string) rdsModelDetails {
	lower := strings.ToLower(raw)
	out := rdsModelDetails{}
	switch {
	case strings.Contains(lower, "fail") || strings.Contains(lower, "error") || strings.Contains(lower, "no such file") || strings.Contains(lower, "rollback"):
		out.Status = "failed"
	case strings.Contains(lower, "success") || strings.Contains(lower, "complete") || strings.Contains(lower, "saved") || strings.Contains(lower, "updated") || strings.Contains(lower, " ok"):
		out.Status = "successful"
	case strings.Contains(lower, "modified") || strings.Contains(lower, "changed") || strings.Contains(lower, "written"):
		out.Status = "changed"
	}
	out.User = firstRegex(raw,
		`(?i)\buser(?:name)?[=: ]+([A-Za-z0-9_.@-]+)`,
		`(?i)\boperator[=: ]+([A-Za-z0-9_.@-]+)`,
		`(?i)\baccount[=: ]+([A-Za-z0-9_.@-]+)`,
		`(?i)\bby\s+([A-Za-z0-9_.@-]+)`,
	)
	out.IP = firstRegex(raw,
		`(?i)\b(?:client|source|remote|from|ip)[=: ]+([0-9]{1,3}(?:\.[0-9]{1,3}){3})`,
	)
	out.Model = firstRegex(raw,
		`(?i)\b(?:model|file|path)[=: ]+([A-Za-z0-9_.@:/-]+\.(?:cp|json|model|txt|xml))`,
		`(?i)\b([A-Za-z0-9_.@:/-]*models/[A-Za-z0-9_.@:/-]+)`,
		`(?i)\b([A-Za-z0-9_.@:/-]*robot\.cp)\b`,
	)
	out.MD5 = firstRegex(raw,
		`(?i)\bmd5(?:sum)?[=: ]+([a-f0-9]{32})`,
		`(?i)\bchecksum[=: ]+([a-f0-9]{32})`,
		`\b([a-f0-9]{32})\b`,
	)
	return out
}

func parseChargeCommandDetails(raw string) chargeCommandDetails {
	lower := strings.ToLower(raw)
	out := chargeCommandDetails{Action: "charge/dock command"}
	switch {
	case strings.Contains(lower, "fail") || strings.Contains(lower, "error") || strings.Contains(lower, "timeout") || strings.Contains(lower, "reject"):
		out.Status = "failed"
	case strings.Contains(lower, "success") || strings.Contains(lower, "accepted") || strings.Contains(lower, "complete") || strings.Contains(lower, " ok"):
		out.Status = "successful"
	case strings.Contains(lower, "sent") || strings.Contains(lower, "requested"):
		out.Status = "sent"
	}
	out.User = firstRegex(raw,
		`(?i)\buser(?:name)?[=: ]+([A-Za-z0-9_.@-]+)`,
		`(?i)\boperator[=: ]+([A-Za-z0-9_.@-]+)`,
		`(?i)\baccount[=: ]+([A-Za-z0-9_.@-]+)`,
		`(?i)\bby\s+([A-Za-z0-9_.@-]+)`,
	)
	out.IP = firstRegex(raw,
		`(?i)\b(?:client|source|remote|from|ip)[=: ]+([0-9]{1,3}(?:\.[0-9]{1,3}){3})`,
	)
	out.Robot = firstRegex(raw,
		`(?i)\b(?:robot|amr|vehicle|device)[=: ]+([A-Za-z0-9_.:@-]+)`,
		`(?i)\[Server:([0-9.]+:\d+)\]`,
	)
	out.Action = firstRegex(raw,
		`(?i)\b((?:charge|charging|charger|dock|docking)[A-Za-z0-9_.:/-]*\s+(?:command|cmd|task|mission|request))`,
		`(?i)\b((?:command|cmd|task|mission|request)[=: ]+[A-Za-z0-9_.:/-]*(?:charge|charging|charger|dock|docking)[A-Za-z0-9_.:/-]*)`,
	)
	if out.Action == "" {
		out.Action = "charge/dock command"
	}
	return out
}

func parseChargeDIDetails(raw string) chargeDIDetails {
	lower := strings.ToLower(raw)
	out := chargeDIDetails{}
	switch {
	case strings.Contains(lower, "broke") || strings.Contains(lower, "break") || strings.Contains(lower, "bad") || strings.Contains(lower, "fail") || strings.Contains(lower, "error"):
		out.Effect = "possible break or bad chargeDI/model change"
	case strings.Contains(lower, "re-applied") || strings.Contains(lower, "reapplied") || strings.Contains(lower, "restored") || strings.Contains(lower, "fix"):
		out.Effect = "possible fix or re-apply"
	case strings.Contains(lower, "applied") || strings.Contains(lower, "trigger"):
		out.Effect = "chargeDI applied or trigger changed"
	case strings.Contains(lower, "comment"):
		out.Effect = "comment or note was added after the fact"
	case strings.Contains(lower, "edit") || strings.Contains(lower, "change") || strings.Contains(lower, "update"):
		out.Effect = "chargeDI edited or updated"
	}
	out.User = firstRegex(raw,
		`(?i)\buser(?:name)?[=: ]+([A-Za-z0-9_.@-]+)`,
		`(?i)\boperator[=: ]+([A-Za-z0-9_.@-]+)`,
		`(?i)\baccount[=: ]+([A-Za-z0-9_.@-]+)`,
		`(?i)\bby\s+([A-Za-z0-9_.@-]+)`,
	)
	out.IP = firstRegex(raw,
		`(?i)\b(?:client|source|remote|from|ip|source ip)[=: ]+([0-9]{1,3}(?:\.[0-9]{1,3}){3})`,
		`(?i)\bfrom\s+([0-9]{1,3}(?:\.[0-9]{1,3}){3})\b`,
	)
	out.Model = firstRegex(raw,
		`(?i)\b(?:model|file|path|config)[=: ]+([A-Za-z0-9_.@:/-]+)`,
		`(?i)\b([A-Za-z0-9_.@:/-]*models/[A-Za-z0-9_.@:/-]+)`,
	)
	return out
}

func firstRegex(raw string, patterns ...string) string {
	for _, pattern := range patterns {
		match := regexp.MustCompile(pattern).FindStringSubmatch(raw)
		if len(match) > 1 {
			return strings.Trim(match[1], `"'[],;`)
		}
	}
	return ""
}

func formatProxmoxAccessTime(raw string) string {
	parsed, err := time.Parse("02/01/2006:15:04:05 -0700", raw)
	if err != nil {
		return raw
	}
	return parsed.Format("Jan 2, 2006 at 3:04:05 PM")
}

func extractRobotIP(raw string) string {
	match := regexp.MustCompile(`\[Server:([0-9.]+):`).FindStringSubmatch(raw)
	if match == nil {
		return ""
	}
	return match[1]
}

func extractBracketEndpoint(raw string) string {
	match := regexp.MustCompile(`\[([0-9.]+:[0-9]+)\]`).FindStringSubmatch(raw)
	if match == nil {
		return ""
	}
	return match[1]
}
