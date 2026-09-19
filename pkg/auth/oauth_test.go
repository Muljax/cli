package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/muljax/cli/pkg/config"
	"github.com/muljax/cli/pkg/storage"
)

func TestOAuthErrorHelpers(t *testing.T) {
	tempUnavail := &OAuthError{
		Code:        ErrCodeTemporarilyUnavailable,
		Description: "Instance undergoing maintenance",
		StatusCode:  http.StatusServiceUnavailable,
	}
	if !tempUnavail.IsTemporarilyUnavailable() {
		t.Errorf("expected IsTemporarilyUnavailable to be true")
	}
	if !strings.Contains(tempUnavail.FriendlyMessage(), "Instance unavailable") {
		t.Errorf("unexpected friendly message: %s", tempUnavail.FriendlyMessage())
	}
	if !strings.Contains(tempUnavail.Error(), "temporarily_unavailable") {
		t.Errorf("unexpected error string: %s", tempUnavail.Error())
	}

	accessDenied := &OAuthError{
		Code:        ErrCodeAccessDenied,
		Description: "Non-admin access restricted during lockdown",
		StatusCode:  http.StatusForbidden,
	}
	if !accessDenied.IsAccessDenied() {
		t.Errorf("expected IsAccessDenied to be true")
	}
	if !strings.Contains(accessDenied.FriendlyMessage(), "Access denied") {
		t.Errorf("unexpected friendly message: %s", accessDenied.FriendlyMessage())
	}

	loginReq := &OAuthError{
		Code:        ErrCodeLoginRequired,
		Description: "Admin key required for silent auth",
		StatusCode:  http.StatusBadRequest,
	}
	if !loginReq.IsLoginRequired() {
		t.Errorf("expected IsLoginRequired to be true")
	}
	if !strings.Contains(loginReq.FriendlyMessage(), "Interactive") {
		t.Errorf("unexpected friendly message: %s", loginReq.FriendlyMessage())
	}

	invalidGrant := &OAuthError{
		Code:        ErrCodeInvalidGrant,
		Description: "Refresh token blocked for non-admin",
		StatusCode:  http.StatusBadRequest,
	}
	if !invalidGrant.IsInvalidGrant() {
		t.Errorf("expected IsInvalidGrant to be true")
	}
	if !strings.Contains(invalidGrant.FriendlyMessage(), "Invalid or restricted grant") {
		t.Errorf("unexpected friendly message: %s", invalidGrant.FriendlyMessage())
	}
}

func TestRenderErrorHTML(t *testing.T) {
	rec := httptest.NewRecorder()
	oauthErr := &OAuthError{
		Code:        ErrCodeTemporarilyUnavailable,
		Description: "Lockdown in progress",
	}

	renderErrorHTML(rec, oauthErr)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status code %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
	if cacheCtrl := rec.Header().Get("Cache-Control"); !strings.Contains(cacheCtrl, "no-store") {
		t.Errorf("expected Cache-Control no-store, got %s", cacheCtrl)
	}
	if pragma := rec.Header().Get("Pragma"); pragma != "no-cache" {
		t.Errorf("expected Pragma no-cache, got %s", pragma)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "temporarily_unavailable") {
		t.Errorf("expected body to contain error code, got: %s", body)
	}
	if !strings.Contains(body, "Lockdown in progress") {
		t.Errorf("expected body to contain description, got: %s", body)
	}
}

func TestRefreshAccessToken_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/token" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "refresh_token" {
			http.Error(w, "invalid grant_type", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		w.WriteHeader(http.StatusOK)

		resp := TokenResponse{
			AccessToken:  "new_access_token_123",
			RefreshToken: "new_refresh_token_456",
			TokenType:    "Bearer",
			ExpiresIn:    3600,
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := &config.Config{
		Endpoint: server.URL,
		ClientID: "test-client",
	}
	ts := &storage.TokenStorage{
		AccessToken:  "old_access_token",
		RefreshToken: "old_refresh_token",
		ExpiresAt:    time.Now().Add(-10 * time.Minute),
	}

	refreshed, err := RefreshAccessToken(cfg, ts)
	if err != nil {
		t.Fatalf("RefreshAccessToken failed: %v", err)
	}
	if refreshed.AccessToken != "new_access_token_123" {
		t.Errorf("expected access token 'new_access_token_123', got '%s'", refreshed.AccessToken)
	}
	if refreshed.RefreshToken != "new_refresh_token_456" {
		t.Errorf("expected refresh token 'new_refresh_token_456', got '%s'", refreshed.RefreshToken)
	}
}

func TestRefreshAccessToken_InvalidGrant(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		w.WriteHeader(http.StatusBadRequest)

		resp := TokenResponse{
			Error:     ErrCodeInvalidGrant,
			ErrorDesc: "Refresh token blocked for non-admin during lockdown",
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := &config.Config{
		Endpoint: server.URL,
		ClientID: "test-client",
	}
	ts := &storage.TokenStorage{
		AccessToken:  "old_access_token",
		RefreshToken: "revoked_refresh_token",
		ExpiresAt:    time.Now().Add(-10 * time.Minute),
	}

	_, err := RefreshAccessToken(cfg, ts)
	if err == nil {
		t.Fatalf("expected error from RefreshAccessToken, got nil")
	}

	oauthErr, ok := err.(*OAuthError)
	if !ok {
		t.Fatalf("expected error to be *OAuthError, got %T: %v", err, err)
	}
	if !oauthErr.IsInvalidGrant() {
		t.Errorf("expected IsInvalidGrant to be true")
	}
	if oauthErr.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status code 400, got %d", oauthErr.StatusCode)
	}
}

func TestExchangeCode_InvalidGrant(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		w.WriteHeader(http.StatusBadRequest)

		resp := TokenResponse{
			Error:     ErrCodeInvalidGrant,
			ErrorDesc: "Authorization code blocked for non-admin",
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := &config.Config{
		Endpoint: server.URL,
		ClientID: "test-client",
	}

	_, err := exchangeCode(cfg, "auth_code_123", "verifier_456", "http://127.0.0.1:9999/callback")
	if err == nil {
		t.Fatalf("expected error from exchangeCode, got nil")
	}

	oauthErr, ok := err.(*OAuthError)
	if !ok {
		t.Fatalf("expected error to be *OAuthError, got %T: %v", err, err)
	}
	if !oauthErr.IsInvalidGrant() {
		t.Errorf("expected IsInvalidGrant to be true")
	}
	if oauthErr.Description != "Authorization code blocked for non-admin" {
		t.Errorf("unexpected description: %s", oauthErr.Description)
	}
}

func TestClientCredentialsToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "client_credentials" {
			http.Error(w, "invalid grant_type", http.StatusBadRequest)
			return
		}
		if r.Form.Get("client_id") != "m2m-service" || r.Form.Get("client_secret") != "m2m-secret" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(TokenResponse{
				Error:     ErrCodeInvalidClient,
				ErrorDesc: "Client authentication failed",
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(TokenResponse{
			AccessToken: "m2m_access_token_789",
			TokenType:   "Bearer",
			ExpiresIn:   7200,
			Scope:       "ssh:ca:read",
		})
	}))
	defer server.Close()

	cfg := &config.Config{
		Endpoint: server.URL,
	}

	// Successful M2M token request
	resp, err := ClientCredentialsToken(cfg, "m2m-service", "m2m-secret", "ssh:ca:read")
	if err != nil {
		t.Fatalf("ClientCredentialsToken failed: %v", err)
	}
	if resp.AccessToken != "m2m_access_token_789" {
		t.Errorf("expected access token 'm2m_access_token_789', got '%s'", resp.AccessToken)
	}

	// Failed M2M token request with invalid secret
	_, err = ClientCredentialsToken(cfg, "m2m-service", "wrong-secret", "ssh:ca:read")
	if err == nil {
		t.Fatalf("expected error with wrong secret, got nil")
	}
	oauthErr, ok := err.(*OAuthError)
	if !ok {
		t.Fatalf("expected *OAuthError, got %T: %v", err, err)
	}
	if oauthErr.Code != ErrCodeInvalidClient {
		t.Errorf("expected error code %s, got %s", ErrCodeInvalidClient, oauthErr.Code)
	}
}
