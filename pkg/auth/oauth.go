package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
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
	ErrorURI     string `json:"error_uri,omitempty"`
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
			oauthErr := &OAuthError{
				Code:        "state_mismatch",
				Description: "OAuth state mismatch: potential CSRF attack",
			}
			renderErrorHTML(w, oauthErr)
			errChan <- oauthErr
			return
		}

		if errParam := r.URL.Query().Get("error"); errParam != "" {
			desc := r.URL.Query().Get("error_description")
			uri := r.URL.Query().Get("error_uri")
			oauthErr := &OAuthError{
				Code:        errParam,
				Description: desc,
				URI:         uri,
				State:       reqState,
			}
			renderErrorHTML(w, oauthErr)
			errChan <- oauthErr
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			oauthErr := &OAuthError{
				Code:        ErrCodeInvalidRequest,
				Description: "Missing authorization code in callback response",
			}
			renderErrorHTML(w, oauthErr)
			errChan <- errors.New("missing authorization code in callback")
			return
		}

		renderSuccessHTML(w)
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
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")

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

	if resp.StatusCode != http.StatusOK || tr.Error != "" {
		if tr.Error != "" {
			return nil, &OAuthError{
				Code:        tr.Error,
				Description: tr.ErrorDesc,
				URI:         tr.ErrorURI,
				StatusCode:  resp.StatusCode,
			}
		}
		return nil, fmt.Errorf("token refresh failed with status %d: %s", resp.StatusCode, string(body))
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
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")

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
		return nil, fmt.Errorf("invalid token exchange response (status %d): %s", resp.StatusCode, string(body))
	}

	if resp.StatusCode != http.StatusOK || tr.Error != "" {
		if tr.Error != "" {
			return nil, &OAuthError{
				Code:        tr.Error,
				Description: tr.ErrorDesc,
				URI:         tr.ErrorURI,
				StatusCode:  resp.StatusCode,
			}
		}
		return nil, fmt.Errorf("token exchange failed with status %d: %s", resp.StatusCode, string(body))
	}

	if tr.AccessToken == "" {
		return nil, fmt.Errorf("no access token returned (status %d)", resp.StatusCode)
	}

	return &tr, nil
}

// ClientCredentialsToken retrieves an OAuth2 token using the client_credentials grant type
// per RFC 6749 §4.4 (machine-to-machine authentication without resource owner context).
func ClientCredentialsToken(cfg *config.Config, clientID, clientSecret, scope string) (*TokenResponse, error) {
	tokenURL := strings.TrimRight(cfg.Endpoint, "/") + "/oauth/token"

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	if clientID != "" {
		form.Set("client_id", clientID)
	} else {
		form.Set("client_id", cfg.ClientID)
	}
	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}
	if scope != "" {
		form.Set("scope", scope)
	}

	req, err := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute client credentials request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var tr TokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("invalid client credentials response (status %d): %s", resp.StatusCode, string(body))
	}

	if resp.StatusCode != http.StatusOK || tr.Error != "" {
		if tr.Error != "" {
			return nil, &OAuthError{
				Code:        tr.Error,
				Description: tr.ErrorDesc,
				URI:         tr.ErrorURI,
				StatusCode:  resp.StatusCode,
			}
		}
		return nil, fmt.Errorf("client credentials request failed with status %d: %s", resp.StatusCode, string(body))
	}

	if tr.AccessToken == "" {
		return nil, fmt.Errorf("no access token in client credentials response (status %d)", resp.StatusCode)
	}

	return &tr, nil
}

func renderSuccessHTML(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>Muljax ID - Authenticated</title>
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <style>
    body {
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
      background: #0f172a;
      color: #f8fafc;
      display: flex;
      justify-content: center;
      align-items: center;
      min-height: 100vh;
      margin: 0;
      padding: 20px;
      box-sizing: border-box;
    }
    .card {
      max-width: 480px;
      width: 100%;
      background: #1e293b;
      border: 1px solid #334155;
      padding: 36px 32px;
      border-radius: 16px;
      text-align: center;
      box-shadow: 0 10px 25px -5px rgba(0, 0, 0, 0.3), 0 8px 10px -6px rgba(0, 0, 0, 0.3);
    }
    .icon {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      width: 52px;
      height: 52px;
      border-radius: 50%;
      background: rgba(56, 189, 248, 0.15);
      color: #38bdf8;
      font-size: 26px;
      margin-bottom: 20px;
    }
    h2 {
      color: #38bdf8;
      margin: 0 0 12px;
      font-size: 1.5rem;
      font-weight: 600;
    }
    p {
      color: #94a3b8;
      line-height: 1.5;
      margin: 0;
      font-size: 0.95rem;
    }
  </style>
</head>
<body>
  <div class="card">
    <div class="icon">✓</div>
    <h2>Authentication Successful</h2>
    <p>You may now close this browser window and return to your terminal.</p>
  </div>
</body>
</html>`))
}

func renderErrorHTML(w http.ResponseWriter, oauthErr *OAuthError) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Header().Set("Pragma", "no-cache")

	statusCode := http.StatusBadRequest
	if oauthErr != nil {
		if oauthErr.IsTemporarilyUnavailable() {
			statusCode = http.StatusServiceUnavailable
		} else if oauthErr.IsAccessDenied() {
			statusCode = http.StatusForbidden
		}
	}
	w.WriteHeader(statusCode)

	title := "Authentication Error"
	detail := "The authorization request could not be completed."
	if oauthErr != nil {
		if oauthErr.IsTemporarilyUnavailable() {
			title = "Service Temporarily Unavailable"
			detail = "The Muljax authorization server is currently undergoing maintenance or is in lockdown mode."
		} else if oauthErr.IsAccessDenied() {
			title = "Access Denied"
			detail = "Access was rejected by the authorization server or administrative policy."
		} else if oauthErr.IsLoginRequired() {
			title = "Authentication Required"
			detail = "Interactive login or administrator key credentials are required."
		}
		if oauthErr.Description != "" {
			detail = oauthErr.Description
		}
	}

	codeBadge := ""
	if oauthErr != nil && oauthErr.Code != "" {
		codeBadge = html.EscapeString(oauthErr.Code)
	}

	htmlContent := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>Muljax ID - %s</title>
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <style>
    body {
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
      background: #0f172a;
      color: #f8fafc;
      display: flex;
      justify-content: center;
      align-items: center;
      min-height: 100vh;
      margin: 0;
      padding: 20px;
      box-sizing: border-box;
    }
    .card {
      max-width: 480px;
      width: 100%%;
      background: #1e293b;
      border: 1px solid #ef4444;
      padding: 36px 32px;
      border-radius: 16px;
      text-align: center;
      box-shadow: 0 10px 25px -5px rgba(0, 0, 0, 0.3), 0 8px 10px -6px rgba(0, 0, 0, 0.3);
    }
    .icon {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      width: 52px;
      height: 52px;
      border-radius: 50%%;
      background: rgba(239, 68, 68, 0.15);
      color: #ef4444;
      font-size: 26px;
      margin-bottom: 20px;
    }
    h2 {
      color: #f87171;
      margin: 0 0 12px;
      font-size: 1.5rem;
      font-weight: 600;
    }
    .badge {
      display: inline-block;
      font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
      font-size: 0.8rem;
      background: #334155;
      color: #cbd5e1;
      padding: 3px 8px;
      border-radius: 6px;
      margin-bottom: 14px;
    }
    p {
      color: #94a3b8;
      line-height: 1.5;
      margin: 0 0 18px;
      font-size: 0.95rem;
    }
    .hint {
      font-size: 0.85rem;
      color: #64748b;
      margin-top: 16px;
      border-top: 1px solid #334155;
      padding-top: 14px;
    }
  </style>
</head>
<body>
  <div class="card">
    <div class="icon">⚠</div>
    <h2>%s</h2>
    <div class="badge">%s</div>
    <p>%s</p>
    <div class="hint">Please check your terminal for more details and instructions.</div>
  </div>
</body>
</html>`, html.EscapeString(title), html.EscapeString(title), codeBadge, html.EscapeString(detail))

	_, _ = w.Write([]byte(htmlContent))
}
