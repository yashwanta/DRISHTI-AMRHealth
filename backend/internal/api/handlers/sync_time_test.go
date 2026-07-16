package handlers

import (
	"testing"
	"time"
)

func TestParseFlexibleTimeSyncTreatsBareRDSDatetimeAsControllerTime(t *testing.T) {
	got, err := parseFlexibleTimeSync("2026-07-14 02:52:10")
	if err != nil {
		t.Fatalf("parseFlexibleTimeSync returned error: %v", err)
	}
	want := time.Date(2026, 7, 13, 18, 52, 10, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("expected %s, got %s", want.Format(time.RFC3339), got.Format(time.RFC3339))
	}
}

func TestParseFlexibleTimeSyncKeepsRFC3339Instants(t *testing.T) {
	got, err := parseFlexibleTimeSync("2026-07-13T18:52:10Z")
	if err != nil {
		t.Fatalf("parseFlexibleTimeSync returned error: %v", err)
	}
	want := time.Date(2026, 7, 13, 18, 52, 10, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("expected %s, got %s", want.Format(time.RFC3339), got.Format(time.RFC3339))
	}
}
