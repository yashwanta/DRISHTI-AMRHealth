package parser

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParseLineLogReviewCategories(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		source    string
		eventType string
		severity  string
	}{
		{
			name:      "robot offline",
			line:      "[9301][Warn][slotTcpError] [Server:10.1.2.3:9301][Tcp:Connect timeout] SocketState:UnconnectedState",
			source:    "journald_amr",
			eventType: "robot_offline",
			severity:  "high",
		},
		{
			name:      "ubuntu reboot",
			line:      "2026-06-08T12:00:00Z amr-01 systemd-logind: System is rebooting",
			source:    "journald",
			eventType: "ubuntu_server_reboot",
			severity:  "high",
		},
		{
			name:      "ubuntu shutdown",
			line:      "2026-06-08T12:00:00Z amr-01 systemd[1]: Reached target Power-Off",
			source:    "journald",
			eventType: "ubuntu_server_shutdown",
			severity:  "high",
		},
		{
			name:      "proxmox host reboot",
			line:      "Jun  8 12:00:00 pve01 pvedaemon restart requested after node reboot",
			source:    "syslog",
			eventType: "proxmox_host_reboot",
			severity:  "high",
		},
		{
			name:      "proxmox host shutdown",
			line:      "Jun  8 12:00:00 pve01 pvedaemon[123]: PVE host shutdown requested",
			source:    "syslog",
			eventType: "proxmox_host_shutdown",
			severity:  "high",
		},
		{
			name:      "vm reboot",
			line:      "Jun  8 12:00:00 pve01 qm reboot 104 --timeout 60",
			source:    "syslog",
			eventType: "vm_reboot",
			severity:  "medium",
		},
		{
			name:      "vm shutdown",
			line:      "Jun  8 12:00:00 pve01 qm shutdown 104 --timeout 60",
			source:    "syslog",
			eventType: "vm_stopped",
			severity:  "medium",
		},
		{
			name:      "power network event",
			line:      "Jun  8 12:00:00 amr-01 kernel: eth0: link is down",
			source:    "kern.log",
			eventType: "network_dhcp_failure",
			severity:  "medium",
		},
		{
			name:      "unknown event",
			line:      "Jun  8 12:00:00 amr-01 app[123]: operator opened diagnostics panel",
			source:    "syslog",
			eventType: "unknown",
			severity:  "low",
		},
		{
			name:      "rds map update success",
			line:      `2026-06-15T10:11:12Z rds map push success user=operator1 client=10.2.1.60 map=LineA.smap`,
			source:    "rds_file_logs",
			eventType: "rds_map_update",
			severity:  "info",
		},
		{
			name:      "rds map update failure",
			line:      `2026-06-15T10:11:12Z rds scene upload failed user=operator1 source=10.2.1.60 map=LineA.smap`,
			source:    "rds_file_logs",
			eventType: "rds_scene_map_error",
			severity:  "high",
		},
		{
			name:      "rds core issue",
			line:      `2026-06-15T10:12:12Z rdscore[912]: API returned 500 while saving robot state: database timeout`,
			source:    "rds_file_logs",
			eventType: "rds_core_issue",
			severity:  "high",
		},
		{
			name:      "rds model md5 update",
			line:      `2026-06-15T10:13:12Z Roboshop model file updated user=operator1 source=10.2.1.60 model=/opt/Roboshop/bin/location/appInfo/robots/A01/models/robot.cp md5=0123456789abcdef0123456789abcdef`,
			source:    "roboshop_app",
			eventType: "rds_model_update",
			severity:  "info",
		},
		{
			name:      "rds model file failure",
			line:      `[20260611 064813.141][Roboshop][0][warning][writeJsonFile:337] Open file failed[/opt/Roboshop/bin/location/appInfo/robots///models/robot.cp]:No such file or directory`,
			source:    "roboshop_app",
			eventType: "rds_model_update",
			severity:  "high",
		},
		{
			name:      "roboshop charge command failure",
			line:      `2026-06-15T10:14:12Z Roboshop charge command failed robot=AMR-17 user=operator1 client=10.2.1.60 error=timeout`,
			source:    "roboshop_app",
			eventType: "amr_charge_command",
			severity:  "high",
		},
		{
			name:      "roboshop chargedi broke charging",
			line:      `2026-06-11T10:14:12Z Roboshop chargeDI model/DI trigger applied source=10.2.1.50 effect="broke charging"`,
			source:    "rds_audit_logs",
			eventType: "roboshop_chargedi_change",
			severity:  "high",
		},
		{
			name:      "roboshop chargedi reapplied",
			line:      `2026-06-12T14:33:00Z RDS chargeDI re-applied across robots source_ip=10.2.1.65 user=admin`,
			source:    "rds_audit_logs",
			eventType: "roboshop_chargedi_change",
			severity:  "info",
		},
		{
			name:      "warlink plc write failure",
			line:      `2026-06-15T10:31:52-05:00 ITPI shingo-edge[2387]: outage_log.go:73: countgroup: heartbeat write to PLC Battery (deadman will trip if sustained) still failing for 1h8m36s (4111 attempts): WarLink POST Battery/write tag=Shingo_Alive returned 500: WriteTag: SendUnitDataTransaction: SendUnitDataTransaction: not connected`,
			source:    "journald_amr",
			eventType: "warlink_failure",
			severity:  "high",
		},
		{
			name:      "warlink application crash",
			line:      `2026-06-15T10:32:00-05:00 ITPI shingo-edge[2387]: fatal panic in WarLink worker: core dumped`,
			source:    "journald_warlink",
			eventType: "warlink_failure",
			severity:  "critical",
		},
		{
			name:      "ubuntu unattended upgrade history",
			line:      `/var/log/apt/history.log:104:Commandline: /usr/bin/unattended-upgrade`,
			source:    "rds_audit_logs",
			eventType: "update",
			severity:  "low",
		},
		{
			name:      "executed charge command",
			line:      `2026-06-15T10:41:00Z Roboshop.desktop Send:[2011]robot_other_setchargingrelay_req {"robot":"AMR01"}`,
			source:    "roboshop_app",
			eventType: "amr_charge_command",
			severity:  "info",
		},
		{
			name:      "go target station command",
			line:      `2026-06-15T10:42:00Z Robod Client To Server: 函数:[robot_task_gotarget_req] {"id":"PP66"}`,
			source:    "journald_robod",
			eventType: "amr_gotarget_station",
			severity:  "medium",
		},
		{
			name:      "admin grep evidence search",
			line:      `Jun 15 19:40:52 host sudo: admin : TTY=pts/0 ; PWD=/root ; USER=root ; COMMAND=/usr/bin/grep -R charge /opt/Roboshop`,
			source:    "auth.log",
			eventType: "admin_evidence_search",
			severity:  "low",
		},
		{
			name:      "admin grep evidence search with battery keywords",
			line:      `Jun 15 11:33:00 host sudo[1234]: operator : TTY=pts/0 ; PWD=/root ; USER=root ; COMMAND=/usr/bin/grep -RniE 'AMR|vehicle|battery|model|config|default|reset|charge|dock' /opt/Roboshop/bin/location/appInfo/log`,
			source:    "auth.log",
			eventType: "admin_evidence_search",
			severity:  "low",
		},
		{
			name:      "admin bash wrapped grep evidence search",
			line:      `Jun 15 11:33:01 host sudo[1235]: operator : TTY=pts/0 ; PWD=/root ; USER=root ; COMMAND=/usr/bin/bash -c 'grep -RniE "battery|charge|dock|reset|default|config" /opt/Roboshop'`,
			source:    "auth.log",
			eventType: "admin_evidence_search",
			severity:  "low",
		},
		{
			name:      "admin bash grep pipeline ignores excluded battery keyword",
			line:      `fleetmanager : PWD=/home/fleetmanager ; USER=root ; COMMAND=/usr/bin/bash -c 'grep -hE '2026-06-11' /opt/Roboshop/bin/location/appInfo/log/Roboshop_*.log 2>/dev/null | grep -iE 'AMR-0[2-7]|vehicle|model|config|setParams|restore|default' | grep -ivE 'status|battery|position' | head -40'`,
			source:    "auth.log",
			eventType: "admin_evidence_search",
			severity:  "low",
		},
		{
			name:      "sudo process grep without command field",
			line:      `Jun 15 11:33:02 host sudo[1236]: grep -RniE "battery|charge|dock|reset|default|config" /opt/Roboshop`,
			source:    "auth.log",
			eventType: "admin_evidence_search",
			severity:  "low",
		},
		{
			name:      "template charge reference",
			line:      `/opt/Roboshop/bin/appInfo/setting/Editor/seer-task/template.json: robot_other_setchargingrelay_req`,
			source:    "rds_file_logs",
			eventType: "template_code_reference",
			severity:  "low",
		},
		{
			name:      "real runtime gotarget still classifies",
			line:      `/opt/.data/robod/appInfo/log/robod.log:2026-06-15 Robod Client To Server: 函数:[robot_task_gotarget_req] {"id":"PP66"}`,
			source:    "rds_file_logs",
			eventType: "amr_gotarget_station",
			severity:  "medium",
		},
		{
			name:      "real runtime upgrade still classifies",
			line:      `/opt/.data/rds/logs/rds.log:2026-06-15 Roboshop.desktop Send:[2451]robot_core_upgrade_robot_req upgrade.zip`,
			source:    "rds_file_logs",
			eventType: "rds_upgrade_reset",
			severity:  "medium",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := ParseLine(tt.line, tt.source, 7)
			if ev == nil {
				t.Fatal("expected event, got nil")
			}
			if ev.EventType != tt.eventType {
				t.Fatalf("event type = %q, want %q", ev.EventType, tt.eventType)
			}
			if ev.Severity != tt.severity {
				t.Fatalf("severity = %q, want %q", ev.Severity, tt.severity)
			}
		})
	}
}

func TestParseLineSkipsHistoricalRebootOutput(t *testing.T) {
	ev := ParseLine("reboot   system boot  6.8.0-110-generic  Wed Apr 29 10:49 - 15:16 (20+04:27)", "system_info", 7)
	if ev != nil {
		t.Fatalf("expected historical reboot output to be skipped, got %q", ev.EventType)
	}
}

func TestParseLineSkipsNoEntriesOutput(t *testing.T) {
	ev := ParseLine("-- No entries --", "journald_warlink", 7)
	if ev != nil {
		t.Fatalf("expected empty journal output to be skipped, got %q", ev.EventType)
	}
}

func TestParseLineClassifiesProxmoxOOM(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		eventType string
	}{
		{
			name:      "qemu scope killed",
			line:      "Jun  5 22:06:01 pve kernel: Out of memory: Killed process 12345 (kvm) total-vm:17500000kB task_memcg:/qemu.slice/113.scope",
			eventType: "vm_killed_by_oom",
		},
		{
			name:      "host oom",
			line:      "Jun  5 22:05:59 pve kernel: node invoked oom-killer: gfp_mask=0x140cca",
			eventType: "host_memory_exhaustion",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := ParseLine(tt.line, "proxmox_host_memory", 7)
			if ev == nil {
				t.Fatal("expected event, got nil")
			}
			if ev.EventType != tt.eventType {
				t.Fatalf("event type = %q, want %q", ev.EventType, tt.eventType)
			}
		})
	}
}

func TestParseLineParsesProxmoxOffsetTimestamp(t *testing.T) {
	ev := ParseLine("2026-06-05T22:06:01-0500 pve kernel: oom-kill:task_memcg=/qemu.slice/113.scope,task=kvm,pid=2915632", "proxmox_journal", 7)
	if ev == nil {
		t.Fatal("expected event, got nil")
	}
	want := time.Date(2026, 6, 6, 3, 6, 1, 0, time.UTC)
	if !ev.Timestamp.Equal(want) {
		t.Fatalf("timestamp = %s, want %s", ev.Timestamp, want)
	}
	if ev.EventType != "vm_killed_by_oom" {
		t.Fatalf("event type = %q, want vm_killed_by_oom", ev.EventType)
	}
}

func TestParseLineClassifiesRootHistorySearchCommandsAsEvidence(t *testing.T) {
	ev := ParseLine(`/root/.bash_history:498:journalctl --since "2026-06-05 22:05:30" --until "2026-06-05 22:06:30" --no-pager | egrep -i "oom|out of memory|killed process|113.scope|qemu.slice|kvm"`, "proxmox_root_history@10.222.10.50", 7)
	if ev == nil {
		t.Fatal("expected admin evidence search event, got nil")
	}
	if ev.EventType != "admin_evidence_search" {
		t.Fatalf("event type = %q, want admin_evidence_search", ev.EventType)
	}
}

func TestParseLineClassifiesProxmoxConsoleAccessAsLoginActivity(t *testing.T) {
	ev := ParseLine(`/var/log/pveproxy/access.log.1:15474:::ffff:10.2.1.60 - root@pam [08/06/2026:16:23:59 -0500] "GET /api2/json/nodes/pve/lxc/109/vncwebsocket?port=5900&vncticket=PVEVNC%3A6A2732EF%3A%3AOoM9JCEP5eSfKoyVT6uA3mHkMO556aOdmfOU0bZZO HTTP/1.1" 101 0`, "proxmox_tasks@10.222.10.50", 7)
	if ev == nil {
		t.Fatal("expected event, got nil")
	}
	if ev.EventType != "ssh_login_activity" {
		t.Fatalf("event type = %q, want ssh_login_activity", ev.EventType)
	}
}

func TestParseLineDropsProxmoxReadPolling(t *testing.T) {
	// Routine dashboard read polling (GET config/pending/status/current) is not
	// an event — it floods the table with noise and buries real activity.
	ev := ParseLine(`/var/log/pveproxy/access.log.1:29286:::ffff:10.216.4.52 - root@pam [17/07/2026:23:56:42 -0400] "GET /api2/json/nodes/AMR/qemu/105/config HTTP/1.1" 200 123`, "proxmox_tasks@10.216.4.55", 7)
	if ev != nil {
		t.Fatalf("expected routine read polling to be dropped, got %q", ev.EventType)
	}
}

func TestParseLineClassifiesProxmoxWriteAsLoginActivity(t *testing.T) {
	ev := ParseLine(`/var/log/pveproxy/access.log:2539:::ffff:10.2.1.76 - root@pam [15/06/2026:14:28:17 -0400] "POST /api2/json/nodes/USSHBAMRPVE/qemu/100/status/shutdown HTTP/1.1" 200 0`, "proxmox_tasks@10.216.4.55", 7)
	if ev == nil {
		t.Fatal("expected event for admin write action, got nil")
	}
	if ev.EventType != "ssh_login_activity" {
		t.Fatalf("event type = %q, want ssh_login_activity", ev.EventType)
	}
}

func TestParseLineClassifiesProxmoxAccessTicketAsLoginActivity(t *testing.T) {
	ev := ParseLine(`/var/log/pveproxy/access.log:13397:::ffff:10.216.28.126 - root@pam [15/06/2026:08:58:53 -0400] "POST /api2/json/access/ticket HTTP/1.1" 200 93`, "proxmox_api_proxy@10.216.4.55", 7)
	if ev == nil {
		t.Fatal("expected event for access/ticket login, got nil")
	}
	if ev.EventType != "ssh_login_activity" {
		t.Fatalf("event type = %q, want ssh_login_activity", ev.EventType)
	}
}

func TestParseLineSkipsCollectionCommandContinuation(t *testing.T) {
	ev := ParseLine(`2026-06-08T07:25:53-05:00 host sudo[322080]: fleetmanager : (command continued) "shutdown|reboot|oom|out of memory|killed process"`, "journald_amr", 7)
	if ev != nil {
		t.Fatalf("expected collection command continuation to be skipped, got %q", ev.EventType)
	}
}

func TestParseOutputCapsUnknownEvents(t *testing.T) {
	var lines []string
	for i := 0; i < 150; i++ {
		lines = append(lines, "2026-06-08T12:00:00Z pve process informational line")
	}
	events := ParseOutput(strings.Join(lines, "\n"), "proxmox_journal", 7)
	if len(events) != 1 {
		t.Fatalf("dedupe should collapse identical unknown events to 1, got %d", len(events))
	}

	lines = lines[:0]
	for i := 0; i < 150; i++ {
		lines = append(lines, "2026-06-08T12:00:00Z pve process informational line "+strconv.Itoa(i))
	}
	events = ParseOutput(strings.Join(lines, "\n"), "proxmox_journal", 7)
	if len(events) != 100 {
		t.Fatalf("unknown event cap = %d, want 100", len(events))
	}
}
