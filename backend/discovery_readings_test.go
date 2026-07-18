package main

import (
	"path/filepath"
	"testing"
)

func TestSavedTPLinkReadingLoadsForDiscovery(t *testing.T) {
	rssi := -58
	snr := 31
	s := &Server{discoveryPath: filepath.Join(t.TempDir(), "wifi-readings.json")}
	result := WifiDiscoverResult{OK: true, Plant: "Springfield", AMR: "AMR-11", RSSI: &rssi, SNR: &snr, SSID: "US-SPR-AMR", Channel: "11", Band: "2.4G"}
	if err := s.saveDiscoveryReading(result); err != nil {
		t.Fatalf("save reading: %v", err)
	}
	readings, err := s.loadDiscoveryReadings("Springfield")
	if err != nil {
		t.Fatalf("load readings: %v", err)
	}
	if len(readings) != 1 || readings[0].RSSIDBM == nil || *readings[0].RSSIDBM != -58 {
		t.Fatalf("unexpected saved readings: %#v", readings)
	}
	if readings[0].Source != "TP-Link Web UI" {
		t.Fatalf("unexpected source: %q", readings[0].Source)
	}
}

func TestZeroRSSIIsNotPersisted(t *testing.T) {
	rssi := 0
	s := &Server{discoveryPath: filepath.Join(t.TempDir(), "wifi-readings.json")}
	if err := s.saveDiscoveryReading(WifiDiscoverResult{OK: true, Plant: "Springfield", AMR: "AMR-11", RSSI: &rssi}); err != nil {
		t.Fatalf("save invalid reading: %v", err)
	}
	readings, err := s.loadDiscoveryReadings("Springfield")
	if err != nil {
		t.Fatalf("load readings: %v", err)
	}
	if len(readings) != 0 {
		t.Fatalf("expected no persisted readings, got %#v", readings)
	}
}
