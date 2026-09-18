package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/muljax/cli/pkg/config"
	"github.com/muljax/cli/pkg/storage"
	"github.com/muljax/cli/pkg/ui"
)

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	IDToken      string `json:"id_token,omitempty"`
	Error        string `json:"error,omitempty"`
	ErrorDesc    string `json:"error_description,omitempty"`
}

func Login(cfg *config.Config) (*storage.TokenStorage, error) {
	pkce, err := GeneratePKCE()
	if err != nil {
		return nil, fmt.Errorf("failed to generate PKCE: %w", err)
	}

	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		return nil, fmt.Errorf("failed to generate state: %w", err)
	}
	state := hex.EncodeToString(stateBytes)

	// Bind ephemeral listener on loopback
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to bind local loopback listener: %w", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	authURL, err := url.Parse(strings.TrimRight(cfg.Endpoint, "/") + "/oauth/authorize")
	if err != nil {
		return nil, fmt.Errorf("invalid endpoint URL: %w", err)
	}

	q := authURL.Query()
	q.Set("response_type", "code")
	q.Set("client_id", cfg.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", "openid profile email offline_access ssh:cert:issue ssh:ca:read ssh:keys:manage")
	q.Set("state", state)
	q.Set("code_challenge", pkce.Challenge)
	q.Set("code_challenge_method", pkce.Method)
	authURL.RawQuery = q.Encode()

	codeChan := make(chan string, 1)
	errChan := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		reqState := r.URL.Query().Get("state")
		if reqState != state {
			http.Error(w, "Invalid state parameter", http.StatusBadRequest)
			errChan <- errors.New("OAuth state mismatch: potential CSRF attack")
			return
		}

		if errParam := r.URL.Query().Get("error"); errParam != "" {
			desc := r.URL.Query().Get("error_description")
			http.Error(w, fmt.Sprintf("Authorization error: %s (%s)", errParam, desc), http.StatusBadRequest)
			errChan <- fmt.Errorf("authorization failed: %s: %s", errParam, desc)
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "Missing authorization code", http.StatusBadRequest)
			errChan <- errors.New("missing authorization code in callback")
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<!DOCTYPE html>
<html>
<head><title>Muljax ID - Authenticated</title></head>
<body style="font-family: system-ui, sans-serif; text-align: center; padding: 40px; background: #0f172a; color: #f8fafc;">
  <div style="max-width: 480px; margin: 0 auto; background: #1e293b; padding: 32px; border-radius: 12px; box-shadow: 0 4px 6px -1px rgb(0 0 0 / 0.1);">
    <h2 style="color: #38bdf8; margin-bottom: 12px;">Authentication Successful</h2>
    <p>You may now close this browser window and return to your terminal.</p>
  </div>
</body>
</html>`))

		codeChan <- code
	})

	server := &http.Server{
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		if serveErr := server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errChan <- serveErr
		}
	}()

	fmt.Printf("%s Opening browser for Muljax authentication...\n", ui.InfoIcon())
	fmt.Printf("  If your browser did not open automatically, visit:\n  %s\n\n", ui.Cyan(authURL.String()))

	_ = openBrowser(authURL.String())

	var code string
	select {
	case code = <-codeChan:
	case err := <-errChan:
		_ = server.Shutdown(context.Background())
		return nil, err
	case <-time.After(3 * time.Minute):
		_ = server.Shutdown(context.Background())
		return nil, errors.New("authentication timed out after 3 minutes")
	}

	_ = server.Shutdown(context.Background())

	// Exchange code for tokens
	tokens, err := exchangeCode(cfg, code, pkce.Verifier, redirectURI)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange code for tokens: %w", err)
	}

	expiresIn := tokens.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}

	ts := &storage.TokenStorage{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		TokenType:    tokens.TokenType,
		ExpiresAt:    time.Now().Add(time.Duration(expiresIn) * time.Second),
		IDToken:      tokens.IDToken,
	}

	if err := storage.SaveTokens(ts); err != nil {
		return nil, fmt.Errorf("failed to save tokens: %w", err)
	}

	return ts, nil
}

func RefreshAccessToken(cfg *config.Config, ts *storage.TokenStorage) (*storage.TokenStorage, error) {
	if ts.RefreshToken == "" {
		return nil, errors.New("no refresh token available, re-authentication required")
	}

	tokenURL := strings.TrimRight(cfg.Endpoint, "/") + "/oauth/token"

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", cfg.ClientID)
	form.Set("refresh_token", ts.RefreshToken)

	req, err := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to refresh token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var tr TokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("invalid token refresh response (status %d): %s", resp.StatusCode, string(body))
	}

	if tr.Error != "" {
		return nil, fmt.Errorf("refresh failed: %s - %s", tr.Error, tr.ErrorDesc)
	}
	if tr.AccessToken == "" {
		return nil, fmt.Errorf("no access token in refresh response (status %d)", resp.StatusCode)
	}

	expiresIn := tr.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}

	ts.AccessToken = tr.AccessToken
	if tr.RefreshToken != "" {
		ts.RefreshToken = tr.RefreshToken
	}
	ts.TokenType = tr.TokenType
	ts.ExpiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)

	if err := storage.SaveTokens(ts); err != nil {
		return nil, fmt.Errorf("failed to persist refreshed tokens: %w", err)
	}

	return ts, nil
}

func exchangeCode(cfg *config.Config, code, verifier, redirectURI string) (*TokenResponse, error) {
	tokenURL := strings.TrimRight(cfg.Endpoint, "/") + "/oauth/token"

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", cfg.ClientID)
	form.Set("code", code)
	form.Set("code_verifier", verifier)
	form.Set("redirect_uri", redirectURI)

	req, err := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var tr TokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("invalid token exchange response: %s", string(body))
	}

	if tr.Error != "" {
		return nil, fmt.Errorf("token exchange failed: %s: %s", tr.Error, tr.ErrorDesc)
	}
	if tr.AccessToken == "" {
		return nil, fmt.Errorf("no access token returned (status %d)", resp.StatusCode)
	}

	return &tr, nil
}
