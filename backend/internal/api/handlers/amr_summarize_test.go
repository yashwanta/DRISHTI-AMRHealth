package handlers

import (
	"strings"
	"testing"
	"time"
)

func TestSummaryNeedsFallbackRejectsStableLLMWhenDropsExist(t *testing.T) {
	facts := summarizeFacts{
		Robot:     "AMR-02",
		Plant:     "Springfield",
		DropCount: 11,
		Resolved:  11,
		WorstSec:  249720,
		TotalSec:  550800,
	}

	bad := "The AMR-02 fleet has demonstrated exceptional reliability, with no disconnect/reconnect events reported and zero total offline time."
	if !summaryNeedsFallback(bad, facts) {
		t.Fatal("expected contradictory stable/no-disconnect summary to be rejected")
	}
}

func TestSummaryNeedsFallbackRejectsNonActionableLLMWhenDropsExist(t *testing.T) {
	facts := summarizeFacts{
		Robot:     "AMR-02",
		Plant:     "Springfield",
		DropCount: 12,
		Resolved:  12,
		WorstSec:  249720,
		TotalSec:  550800,
	}

	bland := "AMR-02 experienced significant downtime and reliability was negatively impacted, but the cause is unclear."
	if !summaryNeedsFallback(bland, facts) {
		t.Fatal("expected non-actionable outage summary to be rejected")
	}
}
func TestSummaryWithRulesReportsRecordedDrops(t *testing.T) {
	start := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	end := time.Date(2026, 7, 13, 13, 0, 0, 0, time.UTC)
	facts := summarizeFacts{
		Robot:      "AMR-02",
		Plant:      "Springfield",
		DropCount:  1,
		Resolved:   1,
		WorstSec:   int(end.Sub(start).Seconds()),
		TotalSec:   int(end.Sub(start).Seconds()),
		AvgSec:     int(end.Sub(start).Seconds()),
		FirstStart: &start,
		LastStart:  &start,
	}

	summary := summarizeWithRules(facts)
	lower := strings.ToLower(summary)
	if !strings.Contains(lower, "amr-02 had 1 disconnect") {
		t.Fatalf("expected recorded disconnect in summary, got %q", summary)
	}
	if strings.Contains(lower, "no disconnect") || strings.Contains(lower, "stable") || strings.Contains(lower, "reliability") {
		t.Fatalf("summary contradicted recorded outage facts: %q", summary)
	}
	if !strings.Contains(lower, "recommended action") || !strings.Contains(lower, "check") {
		t.Fatalf("summary should include operator action guidance: %q", summary)
	}
}
