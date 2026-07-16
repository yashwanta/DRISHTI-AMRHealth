package agent

import (
	"strings"
	"testing"
	"time"
)

func TestParseFindingJSONValid(t *testing.T) {
	raw := `{"root_cause":"battery low","confidence":"high","factors":["a","b"],"timeline":[{"timestamp":"2026-06-18T10:00:00Z","source":"db","event":"drop"}],"prevention":"replace battery"}`
	shape, err := parseFindingJSON(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if shape.RootCause != "battery low" {
		t.Errorf("root_cause = %q", shape.RootCause)
	}
	if shape.Confidence != "high" || len(shape.Factors) != 2 {
		t.Errorf("confidence/factors wrong: %+v", shape)
	}
}

func TestParseFindingJSONFencedAndNoisy(t *testing.T) {
	raw := "Here you go:\n```json\n{\n  \"root_cause\": \"OOM\",\n  \"confidence\": \"medium\"\n}\n```\nHope that helps."
	shape, err := parseFindingJSON(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if shape.RootCause != "OOM" {
		t.Errorf("root_cause = %q", shape.RootCause)
	}
}

func TestParseFindingJSONInvalid(t *testing.T) {
	if _, err := parseFindingJSON("not json at all"); err == nil {
		t.Fatal("expected error for non-json")
	}
	if _, err := parseFindingJSON(`{"confidence":"high"}`); err == nil {
		t.Fatal("expected error for missing root_cause")
	}
}

func TestNormalizeConfidence(t *testing.T) {
	cases := map[string]string{"HIGH": "high", "Medium": "medium", "low": "low", "bogus": "low", "": "low"}
	for in, want := range cases {
		if got := normalizeConfidence(in); got != want {
			t.Errorf("normalizeConfidence(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFallbackFindingOffline(t *testing.T) {
	job := &AgentJob{
		InvestigationType: "Robot Offline",
		LogBundle: []LogEntry{
			{Message: "slotTcpError SocketState:UnconnectedState"},
			{Message: "remote host closed the connection"},
			{Message: "normal status line"},
		},
	}
	f := FallbackFinding(job)
	if !strings.Contains(strings.ToLower(f.RootCause), "connection") && !strings.Contains(strings.ToLower(f.RootCause), "offline") {
		t.Errorf("root cause should mention connection/offline, got %q", f.RootCause)
	}
	if f.Confidence != "medium" {
		t.Errorf("confidence = %q, want medium", f.Confidence)
	}
	if f.Via != "rules" {
		t.Errorf("via = %q, want rules", f.Via)
	}
}

func TestFallbackFindingEmpty(t *testing.T) {
	job := &AgentJob{InvestigationType: "General Log Analysis", LogBundle: nil}
	f := FallbackFinding(job)
	if !strings.Contains(strings.ToLower(f.RootCause), "no events") {
		t.Errorf("empty bundle should say no events, got %q", f.RootCause)
	}
}

func TestFallbackFindingPreventionPerType(t *testing.T) {
	for _, inv := range []string{
		"Config Reset / Factory Default", "Robot Offline", "Connectivity Loss",
		"Battery Error", "RDS Map Update Failure", "General Log Analysis",
	} {
		p := preventionFor(inv)
		if len(p) < 10 {
			t.Errorf("prevention for %q too short: %q", inv, p)
		}
	}
}

func TestNormalizeRDSLinesFiltersRobot(t *testing.T) {
	lines := []string{
		`{"robotId":"AMR-007","voltage":12.1}`,
		`{"robotId":"AMR-008","voltage":13.0}`,
		`{"robotId":"AMR-007","msg":"charging"}`,
	}
	got := normalizeRDSLines(lines, "amr-007")
	if len(got) != 2 {
		t.Fatalf("expected 2 entries for amr-007, got %d", len(got))
	}
	for _, e := range got {
		if !strings.Contains(e.Message, "AMR-007") {
			t.Errorf("entry not robot-scoped: %q", e.Message)
		}
	}
}

func TestSummarizeSources(t *testing.T) {
	t.Run("all done", func(t *testing.T) {
		s := []SourceStatus{{State: StateDone, Count: 5}, {State: StateDone, Count: 3}}
		total, allUnavail := summarizeSources(s)
		if total != 8 || allUnavail {
			t.Errorf("got total=%d allUnavail=%v", total, allUnavail)
		}
	})
	t.Run("all unavailable", func(t *testing.T) {
		s := []SourceStatus{{State: StateUnavailable}, {State: StateUnavailable}}
		total, allUnavail := summarizeSources(s)
		if total != 0 || !allUnavail {
			t.Errorf("got total=%d allUnavail=%v", total, allUnavail)
		}
	})
	t.Run("empty", func(t *testing.T) {
		total, allUnavail := summarizeSources(nil)
		if total != 0 || !allUnavail {
			t.Errorf("empty sources should be allUnavailable, got %v", allUnavail)
		}
	})
}

func TestCollectorSourceStatusTransitions(t *testing.T) {
	// Use fakes implementing the collector interfaces.
	store := NewStore()
	job := store.New("Springfield", "AMR-1", "Robot Offline", "config reset",
		time.Now().Add(-1*time.Hour), time.Now())

	c := NewCollector(
		&rdsFake{lines: []string{`{"robotId":"AMR-1","x":1}`}},
		&sshFake{out: "Jun 18 10:00:00 host robot AMR-1 disconnected"},
		&dbFake{entries: []LogEntry{{Message: "robot AMR-1 battery low"}}},
		"10.0.0.1",
	)

	bundle := c.Collect(nil, store, job.ID, "AMR-1", time.Now().Add(-1*time.Hour), time.Now())
	if len(bundle) == 0 {
		t.Fatal("expected collected entries, got none")
	}

	statuses := store.SnapshotSourceStatus(job.ID)
	if len(statuses) != 4 {
		t.Fatalf("expected 4 source statuses, got %d", len(statuses))
	}
	for _, s := range statuses {
		if s.State == StatePending || s.State == StateInProgress {
			t.Errorf("source %s still %s after collect", s.Source, s.State)
		}
	}
}

// fakes

type rdsFake struct{ lines []string }

func (f *rdsFake) FetchLogs(from, to time.Time) ([]string, error) { return f.lines, nil }

type sshFake struct{ out string }

func (f *sshFake) Run(cmd string) (string, error) { return f.out, nil }

type dbFake struct{ entries []LogEntry }

func (f *dbFake) RobotEvents(robotID string, from, to time.Time) ([]LogEntry, error) {
	return f.entries, nil
}

func (f *dbFake) SourceEvents(kind, robotID string, from, to time.Time) ([]LogEntry, error) {
	return f.entries, nil
}
