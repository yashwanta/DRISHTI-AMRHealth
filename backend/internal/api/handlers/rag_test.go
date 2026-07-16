package handlers

import (
	"strings"
	"testing"
	"time"
)

func TestPatchInventoryQuestionWithoutRunsDoesNotUseGenericLogs(t *testing.T) {
	if !isPatchInventoryQuestion("Which servers missing patching?") {
		t.Fatal("expected missing patching question to use patch inventory path")
	}

	answer := buildPatchInventoryAnswer(nil)
	if !strings.Contains(answer, "I do not have patch inventory yet") {
		t.Fatalf("expected no-inventory answer, got %q", answer)
	}
	if strings.Contains(answer, "Based on the current SiteOps logs") {
		t.Fatalf("patch inventory answer should not use generic log wording: %q", answer)
	}
}

func TestBuildPatchInventoryAnswerSummarizesRuns(t *testing.T) {
	checkedAt := time.Date(2026, 6, 11, 14, 30, 0, 0, time.UTC)
	answer := buildPatchInventoryAnswer([]patchRunSummary{
		{
			ServerName: "Hop-Fleetmanager",
			Action:     "package_list_upgrades",
			Status:     "success",
			Output:     "Listing...\nopenssl/stable-security 3.0 upgradable\n",
			CreatedAt:  checkedAt,
		},
		{
			ServerName: "Spr-PVE",
			Action:     "package_upgrade_dry_run",
			Status:     "success",
			Output:     "0 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.",
			CreatedAt:  checkedAt.Add(-time.Hour),
		},
	})

	for _, want := range []string{
		"patch inventory for 2 server(s)",
		"Likely missing patches: Hop-Fleetmanager.",
		"No available upgrades detected: Spr-PVE.",
	} {
		if !strings.Contains(answer, want) {
			t.Fatalf("answer missing %q: %q", want, answer)
		}
	}
}

func TestBuildSiteOpsAnswerExplainsRobotDisconnect(t *testing.T) {
	events := []ragSourceEvent{
		{
			ServerName: "Hop-Fleetmanager",
			Timestamp:  time.Date(2026, 6, 11, 14, 37, 22, 0, time.UTC),
			EventType:  "error",
			Severity:   "high",
			Message:    "Open file failed[/opt/Roboshop/bin/location/appInfo/robots///models/robot.cp]:No such file or directory",
			Source:     "roboshop_app",
			RawLine:    "[20260611 064813.141][Roboshop][0][warning][writeJsonFile:337] Open file failed[/opt/Roboshop/bin/location/appInfo/robots///models/robot.cp]:No such file or directory",
		},
		{
			ServerName:        "Hop-Fleetmanager",
			Timestamp:         time.Date(2026, 6, 11, 14, 37, 21, 0, time.UTC),
			EventType:         "robot_offline",
			Severity:          "high",
			Message:           "[20260611 064338.615][Roboshop][19204][info][slotTcpStateChange] [Local::0][Server:10.216.35.5:19204][Tcp:none] SocketState:UnconnectedState",
			Source:            "roboshop_app",
			RawLine:           "[20260611 064338.615][Roboshop][19204][info][slotTcpStateChange] [Local::0][Server:10.216.35.5:19204][Tcp:none] SocketState:UnconnectedState",
			PlainEnglish:      "Robot 10.216.35.5 is not connected to the server.",
			RecommendedAction: "Verify robot power, network cabling or Wi-Fi, and the robot service state.",
		},
		{
			ServerName: "Hop-Fleetmanager",
			Timestamp:  time.Date(2026, 6, 11, 14, 37, 20, 0, time.UTC),
			EventType:  "robot_offline",
			Severity:   "high",
			Message:    "[20260611 064338.614][Roboshop][0][warning][setLastError:1622] Add device failed:[10.216.35.5:19204]",
			Source:     "roboshop_app",
			RawLine:    "[20260611 064338.614][Roboshop][0][warning][setLastError:1622] Add device failed:[10.216.35.5:19204]",
		},
	}

	answer := buildSiteOpsAnswer("Why Robot was Disconnected?", events)
	for _, want := range []string{
		"Robot 10.216.35.5:19204 appears disconnected",
		"FleetManager reported the TCP socket as unconnected",
		"FleetManager could not add or reconnect the robot device",
		"Raw logs are kept below",
	} {
		if !strings.Contains(answer, want) {
			t.Fatalf("answer missing %q: %q", want, answer)
		}
	}
	if strings.Contains(answer, "The strongest signal is") {
		t.Fatalf("robot disconnect answer should not use generic wording: %q", answer)
	}
}

func TestRankEventsForQuestionPrioritizesRobotEvidence(t *testing.T) {
	events := []ragSourceEvent{
		{
			ID:        1,
			EventType: "error",
			Severity:  "high",
			Timestamp: time.Date(2026, 6, 11, 14, 37, 22, 0, time.UTC),
			Message:   "Open file failed[/opt/Roboshop/bin/location/appInfo/robots///models/robot.cp]:No such file or directory",
			RawLine:   "Open file failed[/opt/Roboshop/bin/location/appInfo/robots///models/robot.cp]:No such file or directory",
		},
		{
			ID:        2,
			EventType: "robot_offline",
			Severity:  "high",
			Timestamp: time.Date(2026, 6, 11, 14, 37, 21, 0, time.UTC),
			Message:   "[Server:10.216.35.5:19204][Tcp:none] SocketState:UnconnectedState",
			RawLine:   "[Server:10.216.35.5:19204][Tcp:none] SocketState:UnconnectedState",
		},
		{
			ID:        3,
			EventType: "robot_offline",
			Severity:  "high",
			Timestamp: time.Date(2026, 6, 11, 14, 37, 20, 0, time.UTC),
			Message:   "Add device failed:[10.216.35.5:19204]",
			RawLine:   "Add device failed:[10.216.35.5:19204]",
		},
	}

	ranked := rankEventsForQuestion("Why Robot was Disconnected?", events)
	if ranked[0].ID != 2 {
		t.Fatalf("expected socket disconnect evidence first, got event ID %d", ranked[0].ID)
	}
	if ranked[1].ID != 3 {
		t.Fatalf("expected add-device failure second, got event ID %d", ranked[1].ID)
	}
}

func TestBuildRAGSuggestionsUsesAvailableEventTypes(t *testing.T) {
	suggestions := buildRAGSuggestions(map[string]int{
		"warlink_failure":  42,
		"rds_core_issue":   12,
		"vm_killed_by_oom": 8,
		"rds_map_update":   3,
	})
	if len(suggestions) == 0 {
		t.Fatal("expected suggestions")
	}
	if suggestions[0].EventType != "warlink_failure" {
		t.Fatalf("expected WarLink suggestion first, got %q", suggestions[0].EventType)
	}
	foundOOM := false
	foundRDS := false
	for _, suggestion := range suggestions {
		if suggestion.EventType == "vm_killed_by_oom" && strings.Contains(suggestion.Question, "OOM") {
			foundOOM = true
		}
		if suggestion.EventType == "rds_core_issue" && strings.Contains(suggestion.Question, "RDS") {
			foundRDS = true
		}
	}
	if !foundOOM {
		t.Fatalf("expected OOM suggestion in %+v", suggestions)
	}
	if !foundRDS {
		t.Fatalf("expected RDS suggestion in %+v", suggestions)
	}
}
