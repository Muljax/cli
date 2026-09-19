package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/muljax/cli/pkg/auth"
	"github.com/muljax/cli/pkg/config"
	"github.com/muljax/cli/pkg/storage"
)

func TestCheckRevocation(t *testing.T) {
	mockRawRevocationList := `# OpenSSH Revoked Keys
serial: 1001
serial: 2002
serial: 999999999
`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/ssh/ca/revoked-keys" && r.URL.Query().Get("format") == "raw" {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(mockRawRevocationList))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	cfg := &config.Config{
		Endpoint: server.URL,
	}
	c := New(cfg)

	// Test revoked serial
	isRevoked, lineNum, err := c.CheckRevocation("2002")
	if err != nil {
		t.Fatalf("CheckRevocation failed: %v", err)
	}
	if !isRevoked {
		t.Errorf("expected serial 2002 to be revoked")
	}
	if lineNum != 3 {
		t.Errorf("expected serial 2002 to be found on line 3, got %d", lineNum)
	}

	// Test non-revoked serial
	isRevoked, lineNum, err = c.CheckRevocation("12345")
	if err != nil {
		t.Fatalf("CheckRevocation failed: %v", err)
	}
	if isRevoked {
		t.Errorf("expected serial 12345 to NOT be revoked")
	}
	if lineNum != 0 {
		t.Errorf("expected lineNum 0 for non-revoked serial, got %d", lineNum)
	}
}

func TestGetValidAccessToken_Refresh(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/token" {
			_ = r.ParseForm()
			if r.Form.Get("refresh_token") == "lockdown_token" {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Cache-Control", "no-store")
				w.Header().Set("Pragma", "no-cache")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(auth.TokenResponse{
					Error:     auth.ErrCodeInvalidGrant,
					ErrorDesc: "Refresh token blocked for non-admin during lockdown",
				})
				return
			}
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	cfg := &config.Config{
		Endpoint: server.URL,
		ClientID: "test-client",
	}
	c := New(cfg)

	// Save expired token in storage
	ts := &storage.TokenStorage{
		AccessToken:  "expired_access_token",
		RefreshToken: "lockdown_token",
		ExpiresAt:    time.Now().Add(-1 * time.Hour),
	}
	if err := storage.SaveTokens(ts); err != nil {
		t.Fatalf("failed to save test tokens: %v", err)
	}
	defer func() { _ = storage.ClearTokens() }()

	_, err := c.GetValidAccessToken()
	if err == nil {
		t.Fatalf("expected error from GetValidAccessToken during lockdown refresh, got nil")
	}
	if !strings.Contains(err.Error(), "session expired, revoked, or restricted during lockdown") {
		t.Errorf("unexpected error message: %v", err)
	}
}
