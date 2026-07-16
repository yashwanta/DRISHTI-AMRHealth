package handlers

import (
	"testing"
	"time"

	"drishti-amr-health/internal/models"
)

func TestAnalyzeOOMIdentifiesKilledVMAndTopMemoryVM(t *testing.T) {
	events := []models.IncidentEvidence{
		{
			Timestamp: time.Date(2026, 6, 6, 3, 6, 1, 0, time.UTC),
			EventType: "vm_killed_by_oom",
			Source:    "proxmox_journal@10.222.10.50",
			Message:   "2026-06-05T22:06:01-0500 pve kernel: oom-kill:constraint=CONSTRAINT_NONE,task_memcg=/qemu.slice/113.scope,task=kvm,pid=2915632,uid=0",
		},
		{
			Timestamp: time.Date(2026, 6, 6, 3, 6, 1, 0, time.UTC),
			EventType: "vm_killed_by_oom",
			Source:    "proxmox_journal@10.222.10.50",
			Message:   "2026-06-05T22:06:01-0500 pve kernel: Out of memory: Killed process 2915632 (kvm) total-vm:18017820kB, anon-rss:14109780kB, file-rss:2688kB",
		},
		{
			Timestamp: time.Date(2026, 6, 8, 20, 53, 16, 0, time.UTC),
			EventType: "unknown",
			Source:    "proxmox_host_memory@10.222.10.50",
			Message:   "VMID=112 NAME=small-vm PID=222 RSS_GB=2.50 CONFIG_MB=4096",
		},
		{
			Timestamp: time.Date(2026, 6, 8, 20, 53, 16, 0, time.UTC),
			EventType: "unknown",
			Source:    "proxmox_host_memory@10.222.10.50",
			Message:   "VMID=113 NAME=fleetmanager PID=2915632 RSS_GB=13.46 CONFIG_MB=16384",
		},
	}

	got := analyzeOOM(events)
	if got == nil {
		t.Fatal("expected OOM analysis, got nil")
	}
	if got.KilledVMID != "113" {
		t.Fatalf("KilledVMID = %q, want 113", got.KilledVMID)
	}
	if got.TopVMID != "113" {
		t.Fatalf("TopVMID = %q, want 113", got.TopVMID)
	}
	if got.KilledPID != "2915632" || got.KilledProcess != "kvm" {
		t.Fatalf("killed process = %q/%q, want kvm/2915632", got.KilledProcess, got.KilledPID)
	}
	if got.KilledAnonGB != 13.46 {
		t.Fatalf("KilledAnonGB = %.2f, want 13.46", got.KilledAnonGB)
	}
	if got.Confidence != "high" {
		t.Fatalf("Confidence = %q, want high", got.Confidence)
	}
	if got.ProxmoxHost != "10.222.10.50" {
		t.Fatalf("ProxmoxHost = %q, want 10.222.10.50", got.ProxmoxHost)
	}
}

func TestAnalyzeOOMUsesTopVMWhenScopeMissing(t *testing.T) {
	events := []models.IncidentEvidence{
		{
			EventType: "host_memory_exhaustion",
			Source:    "proxmox_journal@10.222.10.50",
			Message:   "pve kernel: node invoked oom-killer",
		},
		{
			EventType: "unknown",
			Source:    "proxmox_host_memory@10.222.10.50",
			Message:   "VMID=201 NAME=db PID=100 RSS_GB=11.00 CONFIG_MB=12288",
		},
		{
			EventType: "unknown",
			Source:    "proxmox_host_memory@10.222.10.50",
			Message:   "VMID=202 NAME=worker PID=101 RSS_GB=15.25 CONFIG_MB=16384",
		},
	}

	got := analyzeOOM(events)
	if got == nil {
		t.Fatal("expected OOM analysis, got nil")
	}
	if got.KilledVMID != "" {
		t.Fatalf("KilledVMID = %q, want empty", got.KilledVMID)
	}
	if got.TopVMID != "202" || got.TopVMName != "worker" {
		t.Fatalf("top VM = %q/%q, want 202/worker", got.TopVMID, got.TopVMName)
	}
	if got.Confidence != "medium" {
		t.Fatalf("Confidence = %q, want medium", got.Confidence)
	}
}

func TestShouldAnalyzeOOMRowSkipsAdminEvidenceSearch(t *testing.T) {
	ev := models.LogEvent{
		EventType: "admin_evidence_search",
		Message:   `/root/.bash_history:495:journalctl --since "2026-06-05" | egrep -i "oom|out of memory|killed process|qemu.slice|kvm"`,
		RawLine:   `/root/.bash_history:495:journalctl --since "2026-06-05" | egrep -i "oom|out of memory|killed process|qemu.slice|kvm"`,
		Source:    "proxmox_root_history@10.222.10.50",
	}
	if shouldAnalyzeOOMRow(ev) {
		t.Fatal("admin evidence search should not trigger OOM analysis")
	}
}
