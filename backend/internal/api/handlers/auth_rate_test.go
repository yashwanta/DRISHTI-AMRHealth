package handlers

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestAllowLoginAttemptBlocksEleventhRequest(t *testing.T) {
	loginRateEntries = sync.Map{}
	loginCleanupOnce = sync.Once{}

	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
		req.RemoteAddr = "192.0.2.10:12345"
		if !allowLoginAttempt(req) {
			t.Fatalf("request %d was blocked, want allowed", i+1)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req.RemoteAddr = "192.0.2.10:12345"
	if allowLoginAttempt(req) {
		t.Fatalf("request 11 was allowed, want blocked")
	}
}

func TestLoginClientIPPrefersXRealIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req.RemoteAddr = "192.0.2.10:12345"
	req.Header.Set("X-Real-IP", "198.51.100.7")

	if got := loginClientIP(req); got != "198.51.100.7" {
		t.Fatalf("loginClientIP = %q, want %q", got, "198.51.100.7")
	}
}
