package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestTPLinkDSReadingSuccessfulLogin(t *testing.T) {
	var loginCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/stok=null/ds":
			atomic.AddInt32(&loginCount, 1)
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode login payload: %v", err)
			}
			if payload["method"] != "do" {
				t.Fatalf("expected login method do, got %#v", payload["method"])
			}
			http.SetCookie(w, &http.Cookie{Name: "TPSESSION", Value: "present"})
			_, _ = fmt.Fprint(w, `{"error_code":0,"stok":"good-token"}`)
		case "/stok=good-token/ds":
			_, _ = fmt.Fprint(w, `{"error_code":0,"wireless":{"wlan_wds_status":[{"ssid":"US-SPR-AMR","bssid":"F4-E1-FC-F0-4C-22","rssi":"-66/-62 dBm","snr":34,"channel":11}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	reading, err := (&Server{}).fetchTPLinkReading(WifiSource{Method: "TP-Link Web UI", Host: server.URL + "/#/home", Username: "admin", SecretRef: "secret"})
	if err != nil {
		t.Fatalf("fetchTPLinkReading returned error: %v", err)
	}
	if reading.RSSI == nil || *reading.RSSI != -66 {
		t.Fatalf("expected RSSI -66, got %#v", reading.RSSI)
	}
	if reading.SNR == nil || *reading.SNR != 34 {
		t.Fatalf("expected SNR 34, got %#v", reading.SNR)
	}
	if reading.SSID != "US-SPR-AMR" {
		t.Fatalf("expected SSID US-SPR-AMR, got %q", reading.SSID)
	}
	if got := atomic.LoadInt32(&loginCount); got != 1 {
		t.Fatalf("expected one login request, got %d", got)
	}
}

func TestTPLinkIncorrectPasswordReturnsAuthFailed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"error_code":-40401,"data":{"code":-40401,"group":0}}`)
	}))
	defer server.Close()

	_, err := (&Server{}).fetchTPLinkReading(WifiSource{Method: "TP-Link Web UI", Host: server.URL, Username: "admin", SecretRef: "wrong"})
	var tpErr *tpLinkIntegrationError
	if err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("expected auth failure, got %v", err)
	}
	if !errorAs(err, &tpErr) || tpErr.Status != "auth_failed" {
		t.Fatalf("expected auth_failed status, got %#v", err)
	}
}

func TestTPLinkTemporaryLockoutStopsSecondLogin(t *testing.T) {
	var loginCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&loginCount, 1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"error_code":-40401,"data":{"code":-40401,"max_time":20,"time":19,"group":0}}`)
	}))
	defer server.Close()

	s := &Server{}
	source := WifiSource{Method: "TP-Link Web UI", Host: server.URL, Username: "admin", SecretRef: "wrong"}
	_, firstErr := s.fetchTPLinkReading(source)
	if firstErr == nil || !strings.Contains(firstErr.Error(), "temporarily locked") {
		t.Fatalf("expected first lockout error, got %v", firstErr)
	}
	_, secondErr := s.fetchTPLinkReading(source)
	if secondErr == nil || !strings.Contains(secondErr.Error(), "temporarily locked") {
		t.Fatalf("expected cached lockout error, got %v", secondErr)
	}
	if got := atomic.LoadInt32(&loginCount); got != 1 {
		t.Fatalf("expected one backend login attempt before lockout cache, got %d", got)
	}
}

func TestTPLinkExpiredSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/stok=null/ds":
			_, _ = fmt.Fprint(w, `{"error_code":0,"stok":"expired-token"}`)
		case "/stok=expired-token/ds":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = fmt.Fprint(w, `{"error_code":-40101}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := (&Server{}).fetchTPLinkReading(WifiSource{Method: "TP-Link Web UI", Host: server.URL, Username: "admin", SecretRef: "secret"})
	if err == nil || !strings.Contains(err.Error(), "session was rejected or expired") {
		t.Fatalf("expected expired session error, got %v", err)
	}
}

func TestTPLinkMissingRSSIFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/stok=null/ds":
			_, _ = fmt.Fprint(w, `{"error_code":0,"stok":"good-token"}`)
		case "/stok=good-token/ds":
			_, _ = fmt.Fprint(w, `{"error_code":0,"wireless":{"wlan_wds_status":[{"ssid":"US-SPR-AMR"}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := (&Server{}).fetchTPLinkReading(WifiSource{Method: "TP-Link Web UI", Host: server.URL, Username: "admin", SecretRef: "secret"})
	if err == nil || !strings.Contains(err.Error(), "RSSI/SNR fields") {
		t.Fatalf("expected missing RSSI/SNR fields error, got %v", err)
	}
}

func errorAs(err error, target any) bool {
	switch typed := target.(type) {
	case **tpLinkIntegrationError:
		if err == nil {
			return false
		}
		if value, ok := err.(*tpLinkIntegrationError); ok {
			*typed = value
			return true
		}
	}
	return false
}
