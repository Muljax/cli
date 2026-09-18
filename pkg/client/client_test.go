package client

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/muljax/cli/pkg/config"
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
