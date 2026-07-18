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
		case "/":
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
	if firstErr == nil || !strings.Contains(firstErr.Error(), "Retry later") {
		t.Fatalf("expected first lockout error, got %v", firstErr)
	}
	_, secondErr := s.fetchTPLinkReading(source)
	if secondErr == nil || !strings.Contains(secondErr.Error(), "Retry later") {
		t.Fatalf("expected cached lockout error, got %v", secondErr)
	}
	if got := atomic.LoadInt32(&loginCount); got != 1 {
		t.Fatalf("expected one backend login attempt before lockout cache, got %d", got)
	}
}

func TestTPLinkExpiredSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
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
	if err == nil || !strings.Contains(err.Error(), "session expired") {
		t.Fatalf("expected expired session error, got %v", err)
	}
}

func TestTPLinkSessionTokenIsReusedAcrossPolls(t *testing.T) {
	var loginCount int32
	var statusCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			atomic.AddInt32(&loginCount, 1)
			_, _ = fmt.Fprint(w, `{"error_code":0,"stok":"cached-token"}`)
		case "/stok=cached-token/ds":
			atomic.AddInt32(&statusCount, 1)
			_, _ = fmt.Fprint(w, `{"error_code":0,"wireless":{"wlan_wds_status":[{"ssid":"AMR","rssi":"-58 dBm","snr":31}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	s := &Server{}
	source := WifiSource{Method: "TP-Link Web UI", Host: server.URL, Username: "admin", SecretRef: "secret", SessionKey: "springfield|amr-01|10.222.42.31"}
	for i := 0; i < 2; i++ {
		if _, err := s.fetchTPLinkReading(source); err != nil {
			t.Fatalf("poll %d failed: %v", i+1, err)
		}
	}
	if got := atomic.LoadInt32(&loginCount); got != 1 {
		t.Fatalf("expected cached session to make one login, got %d", got)
	}
	if got := atomic.LoadInt32(&statusCount); got != 2 {
		t.Fatalf("expected two telemetry polls, got %d", got)
	}
}

func TestTPLinkExpiredCachedSessionReauthenticatesOnce(t *testing.T) {
	var loginCount int32
	var tokenOnePolls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			login := atomic.AddInt32(&loginCount, 1)
			_, _ = fmt.Fprintf(w, `{"error_code":0,"stok":"token-%d"}`, login)
		case "/stok=token-1/ds":
			if atomic.AddInt32(&tokenOnePolls, 1) == 1 {
				_, _ = fmt.Fprint(w, `{"error_code":0,"wireless":{"wlan_wds_status":[{"rssi":"-60 dBm"}]}}`)
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = fmt.Fprint(w, `{"error_code":-40101}`)
		case "/stok=token-2/ds":
			_, _ = fmt.Fprint(w, `{"error_code":0,"wireless":{"wlan_wds_status":[{"rssi":"-61 dBm"}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	s := &Server{}
	source := WifiSource{Method: "TP-Link Web UI", Host: server.URL, Username: "admin", SecretRef: "secret", SessionKey: "springfield|amr-02|10.222.42.32"}
	if _, err := s.fetchTPLinkReading(source); err != nil {
		t.Fatalf("initial poll failed: %v", err)
	}
	if _, err := s.fetchTPLinkReading(source); err != nil {
		t.Fatalf("poll after session expiry failed: %v", err)
	}
	if got := atomic.LoadInt32(&loginCount); got != 2 {
		t.Fatalf("expected one initial login and one refresh login, got %d", got)
	}
}

func TestTPLinkMissingRSSIFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
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

func TestTPLinkZeroRSSIIsRejected(t *testing.T) {
	reading := parseTPLinkReading(`{"wireless":{"wlan_wds_status":[{"rssi":0,"channel":11}]}}`)
	if reading.RSSI != nil {
		t.Fatalf("expected invalid 0 dBm RSSI to be rejected, got %d", *reading.RSSI)
	}
}

func TestTPLinkBandInferredFromChannel(t *testing.T) {
	reading := parseTPLinkReading(`{"wireless":{"wlan_wds_status":[{"wlan_wds_status_1":{"rssi":"-75","snr":"21","channel":"1"}},{"wlan_wds_status_2":{"rssi":"-75","snr":"21","channel":"48"}}]}}`)
	if reading.Band != "2.4 GHz" {
		t.Fatalf("expected channel 1 to map to 2.4 GHz, got %q", reading.Band)
	}
}

func TestTPLinkPreservesBothRadioBands(t *testing.T) {
	reading := parseTPLinkReading(`{"wireless":{"wlan_wds_status":[{"wlan_wds_status_1":{"radio_id":"1","rssi":"-62","channel":"11","bssid":"aa-bb"}},{"wlan_wds_status_2":{"radio_id":"2","rssi":"-64","channel":"153","bssid":"cc-dd"}}]}}`)
	if reading.Band24 != "-62 dBm / ch 11 / aa-bb" {
		t.Fatalf("unexpected 2.4 GHz summary %q", reading.Band24)
	}
	if reading.Band5 != "-64 dBm / ch 153 / cc-dd" {
		t.Fatalf("unexpected 5 GHz summary %q", reading.Band5)
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
