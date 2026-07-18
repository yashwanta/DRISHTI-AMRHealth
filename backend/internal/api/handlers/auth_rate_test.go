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

func TestValidatePasswordComplexity(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{name: "valid", password: "FleetHealth2026", wantErr: false},
		{name: "too short", password: "Health2026", wantErr: true},
		{name: "no number", password: "FleetHealthOnly", wantErr: true},
		{name: "no letter", password: "123456789012", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePasswordComplexity(tt.password)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validatePasswordComplexity() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
