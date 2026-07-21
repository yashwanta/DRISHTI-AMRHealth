package handlers

import (
	"strings"
	"testing"
	"time"
)

func validHeatmapPoint() WifiScanPoint {
	now := time.Now().UTC()
	return WifiScanPoint{PlantID: "Springfield", SourcePlant: "Springfield", MapID: "map-1", MapVersion: "map-1", AMRID: "AMR-1", WifiAMRID: "AMR-1", X: 10, Y: 20, RSSIDBM: -67, BSSID: "aa:bb:cc:dd:ee:ff", Channel: 11, Band: "2.4 GHz", Connected: true, SourceID: "AMR SSH", PositionTimestamp: now, WifiTimestamp: now.Add(-2 * time.Second)}
}

func TestValidateScanPointAcceptsSynchronizedRecord(t *testing.T) {
	p := validHeatmapPoint()
	if err := validateScanPoint(&p, 15*time.Second); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if p.Timestamp.IsZero() {
		t.Fatal("canonical timestamp was not populated")
	}
}

func TestValidateScanPointRejectsIdentityAndPlantMismatch(t *testing.T) {
	p := validHeatmapPoint()
	p.SourcePlant = "Shelbyville"
	if err := validateScanPoint(&p, 15*time.Second); err == nil || !strings.Contains(err.Error(), "plant mismatch") {
		t.Fatalf("expected plant mismatch, got %v", err)
	}
	p = validHeatmapPoint()
	p.WifiAMRID = "AMR-2"
	if err := validateScanPoint(&p, 15*time.Second); err == nil || !strings.Contains(err.Error(), "AMR mismatch") {
		t.Fatalf("expected AMR mismatch, got %v", err)
	}
}

func TestValidateScanPointRejectsTimestampMismatch(t *testing.T) {
	p := validHeatmapPoint()
	p.WifiTimestamp = p.PositionTimestamp.Add(-30 * time.Second)
	if err := validateScanPoint(&p, 15*time.Second); err == nil || !strings.Contains(err.Error(), "timestamp mismatch") {
		t.Fatalf("expected timestamp mismatch, got %v", err)
	}
}

func TestFingerprintDeduplicatesOnlySameCapture(t *testing.T) {
	a, b := validHeatmapPoint(), validHeatmapPoint()
	session := int64(8)
	a.SessionID, b.SessionID = &session, &session
	b.Timestamp = a.Timestamp
	if fingerprint(a) != fingerprint(b) {
		t.Fatal("the same capture should deduplicate")
	}
	b.Timestamp = a.Timestamp.Add(time.Hour)
	if fingerprint(a) == fingerprint(b) {
		t.Fatal("a later observation should not deduplicate")
	}
	newSession := int64(9)
	b.Timestamp = a.Timestamp
	b.SessionID = &newSession
	if fingerprint(a) == fingerprint(b) {
		t.Fatal("a new survey session should not inherit duplicate state")
	}
}
