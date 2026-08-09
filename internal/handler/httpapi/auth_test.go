package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"fileservice/internal/config"
)

func testConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := config.Config{
		Environment: "test",
		HTTP:        config.HTTPConfig{Address: ":0", ReadHeaderTimeout: 1, ReadTimeout: 1, WriteTimeout: 1, IdleTimeout: 1, ShutdownTimeout: 1, MaxHeaderBytes: 4096},
		Auth:        config.AuthConfig{JWTSecret: "01234567890123456789012345678901", JWTIssuer: "fileservice-test", AccessTokenTTL: time.Minute, RefreshTokenTTL: time.Hour, LoginRateLimit: 20, LoginRateWindow: time.Minute, RefreshRateLimit: 20, RefreshRateWindow: time.Minute, Username: "Naman", Password: "7564"},
		Database:    config.DatabaseConfig{Path: ":memory:"},
		Storage:     config.StorageConfig{Root: t.TempDir(), MaxBytes: 1 << 20, AllowedMIMEs: []string{"text/plain"}},
	}
	return cfg
}

func TestAuthHTTPFlow(t *testing.T) {
	handler, closeDatabase := NewRouterWithCloser(context.Background(), slog.Default(), testConfig(t))
	defer closeDatabase()
	ts := httptest.NewServer(handler)
	defer ts.Close()
	client, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	httpClient := ts.Client()
	httpClient.Jar = client

	body := bytes.NewBufferString(`{"username":"Naman","password":"7564"}`)
	request, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/login", body)
	request.Header.Set("Content-Type", "application/json")
	response, err := httpClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(response.Body)
		t.Fatalf("login status = %d, body = %s", response.StatusCode, data)
	}
	var payload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil || payload.AccessToken == "" {
		t.Fatalf("missing access token: %+v, error = %v", payload, err)
	}

	logout, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/logout", nil)
	logout.Header.Set("Authorization", "Bearer "+payload.AccessToken)
	logoutResponse, err := http.DefaultClient.Do(logout)
	if err != nil {
		t.Fatal(err)
	}
	defer logoutResponse.Body.Close()
	if logoutResponse.StatusCode != http.StatusNoContent {
		data, _ := io.ReadAll(logoutResponse.Body)
		t.Fatalf("logout status = %d, body = %s", logoutResponse.StatusCode, data)
	}
}

func TestAuthMe(t *testing.T) {
	handler, closeDatabase := NewRouterWithCloser(context.Background(), slog.Default(), testConfig(t))
	defer closeDatabase()
	ts := httptest.NewServer(handler)
	defer ts.Close()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	httpClient := ts.Client()
	httpClient.Jar = jar

	loginBody := bytes.NewBufferString(`{"username":"Naman","password":"7564"}`)
	loginRequest, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/login", loginBody)
	loginRequest.Header.Set("Content-Type", "application/json")
	loginResponse, err := httpClient.Do(loginRequest)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, loginResponse.Body)
	loginResponse.Body.Close()
	if loginResponse.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d", loginResponse.StatusCode)
	}

	meRequest, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/auth/me", nil)
	meResponse, err := httpClient.Do(meRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer meResponse.Body.Close()
	if meResponse.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(meResponse.Body)
		t.Fatalf("me status = %d, body = %s", meResponse.StatusCode, data)
	}
	var me struct {
		UserID   string `json:"user_id"`
		Username string `json:"username"`
	}
	if err := json.NewDecoder(meResponse.Body).Decode(&me); err != nil {
		t.Fatal(err)
	}
	if me.Username != "Naman" || me.UserID == "" {
		t.Fatalf("me payload = %+v", me)
	}
	if !strings.Contains(meResponse.Header.Get("Set-Cookie"), "csrf_token") {
		t.Fatal("me did not refresh the CSRF cookie")
	}

	// Clients without a session are rejected. Note ts.Client() returns a
	// shared client, so a brand-new client is required for a truly
	// cookie-free request.
	anonClient := &http.Client{}
	anonRequest, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/auth/me", nil)
	anonResponse, err := anonClient.Do(anonRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer anonResponse.Body.Close()
	if anonResponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous me status = %d", anonResponse.StatusCode)
	}
}

// TestCrossOriginAPIWithBearerTokens proves a UI hosted on another origin can
// use the API end to end: preflight is answered with CORS headers, login
// returns a refresh token for the remote UI to store, bearer mutations work
// without any cookies, and the refresh token rotates via the bearer path.
func TestCrossOriginAPIWithBearerTokens(t *testing.T) {
	cfg := testConfig(t)
	cfg.CORS.AllowedOrigins = []string{"https://files.example.com"}
	handler, closeDatabase := NewRouterWithCloser(context.Background(), slog.Default(), cfg)
	defer closeDatabase()
	ts := httptest.NewServer(handler)
	defer ts.Close()

	// Preflight for a cross-origin login.
	preflight, _ := http.NewRequest(http.MethodOptions, ts.URL+"/api/auth/login", nil)
	preflight.Header.Set("Origin", "https://files.example.com")
	preflight.Header.Set("Access-Control-Request-Method", "POST")
	preflight.Header.Set("Access-Control-Request-Headers", "content-type,authorization")
	preflightResponse, err := http.DefaultClient.Do(preflight)
	if err != nil {
		t.Fatal(err)
	}
	preflightResponse.Body.Close()
	if preflightResponse.StatusCode != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", preflightResponse.StatusCode)
	}
	if got := preflightResponse.Header.Get("Access-Control-Allow-Origin"); got != "https://files.example.com" {
		t.Fatalf("preflight Allow-Origin = %q", got)
	}

	// Cross-origin login returns CORS headers and a refresh token for the UI.
	loginBody := bytes.NewBufferString(`{"username":"Naman","password":"7564"}`)
	login, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/login", loginBody)
	login.Header.Set("Content-Type", "application/json")
	login.Header.Set("Origin", "https://files.example.com")
	loginResponse, err := http.DefaultClient.Do(login)
	if err != nil {
		t.Fatal(err)
	}
	defer loginResponse.Body.Close()
	if loginResponse.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d", loginResponse.StatusCode)
	}
	if got := loginResponse.Header.Get("Access-Control-Allow-Origin"); got != "https://files.example.com" {
		t.Fatalf("login Allow-Origin = %q", got)
	}
	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(loginResponse.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.AccessToken == "" || payload.RefreshToken == "" {
		t.Fatalf("login payload missing tokens: %+v", payload)
	}

	// A bearer mutation from the UI origin succeeds with no cookies.
	createBody := bytes.NewBufferString(`{"name":"FromRemoteUI"}`)
	create, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/folders", createBody)
	create.Header.Set("Content-Type", "application/json")
	create.Header.Set("Origin", "https://files.example.com")
	create.Header.Set("Authorization", "Bearer "+payload.AccessToken)
	createResponse, err := http.DefaultClient.Do(create)
	if err != nil {
		t.Fatal(err)
	}
	createResponse.Body.Close()
	if createResponse.StatusCode != http.StatusCreated {
		t.Fatalf("create folder status = %d, want 201", createResponse.StatusCode)
	}

	// The refresh token rotates through the bearer path.
	refresh, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/refresh", nil)
	refresh.Header.Set("Origin", "https://files.example.com")
	refresh.Header.Set("Authorization", "Bearer "+payload.RefreshToken)
	refreshResponse, err := http.DefaultClient.Do(refresh)
	if err != nil {
		t.Fatal(err)
	}
	defer refreshResponse.Body.Close()
	if refreshResponse.StatusCode != http.StatusOK {
		t.Fatalf("refresh status = %d", refreshResponse.StatusCode)
	}
	var rotated struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(refreshResponse.Body).Decode(&rotated); err != nil {
		t.Fatal(err)
	}
	if rotated.AccessToken == "" || rotated.RefreshToken == "" {
		t.Fatalf("refresh payload missing tokens: %+v", rotated)
	}
}

// TestWebHomeListsRemoteServers proves the multi-server workspace mode: the
// page injects the remote server list and the CSP allows every remote origin.
func TestWebHomeListsRemoteServers(t *testing.T) {
	cfg := testConfig(t)
	cfg.UI.RemoteServers = []config.RemoteServer{
		{Name: "Media", Base: "http://130.210.21.47:8080"},
		{Name: "Backup", Base: "https://backup.example.com"},
	}
	handler, closeDatabase := NewRouterWithCloser(context.Background(), slog.Default(), cfg)
	defer closeDatabase()
	ts := httptest.NewServer(handler)
	defer ts.Close()

	response, err := http.DefaultClient.Get(ts.URL + "/app")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(body), `window.NDRIVE_SERVERS=[{"base":"http://130.210.21.47:8080","name":"Media"},{"base":"https://backup.example.com","name":"Backup"}];`) {
		t.Fatal("page does not inject the remote server list")
	}
	csp := response.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "connect-src 'self' http://130.210.21.47:8080 https://backup.example.com") {
		t.Fatalf("CSP does not allow the remote origins: %q", csp)
	}

	// Without remote servers the page does not inject the list.
	handler2, closeDatabase2 := NewRouterWithCloser(context.Background(), slog.Default(), testConfig(t))
	defer closeDatabase2()
	ts2 := httptest.NewServer(handler2)
	defer ts2.Close()
	response2, err := http.DefaultClient.Get(ts2.URL + "/app")
	if err != nil {
		t.Fatal(err)
	}
	defer response2.Body.Close()
	body2, _ := io.ReadAll(response2.Body)
	if strings.Contains(string(body2), "window.NDRIVE_SERVERS=") {
		t.Fatal("page should not inject a remote server list by default")
	}
}

// TestWebHomePointsAtRemoteAPI proves the embedded workspace can be served
// from one origin while targeting a remote API: the API base is injected into
// the page and the CSP allows the remote origin.
func TestWebHomePointsAtRemoteAPI(t *testing.T) {
	cfg := testConfig(t)
	cfg.UI.APIBase = "https://api.example.com"
	handler, closeDatabase := NewRouterWithCloser(context.Background(), slog.Default(), cfg)
	defer closeDatabase()
	ts := httptest.NewServer(handler)
	defer ts.Close()

	response, err := http.DefaultClient.Get(ts.URL + "/app")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(body), `window.NDRIVE_API_BASE="https://api.example.com";`) {
		t.Fatal("page does not inject the remote API base")
	}
	csp := response.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "connect-src 'self' https://api.example.com") {
		t.Fatalf("CSP does not allow the remote API: %q", csp)
	}

	// Without UI_API_BASE the page stays same-origin.
	handler2, closeDatabase2 := NewRouterWithCloser(context.Background(), slog.Default(), testConfig(t))
	defer closeDatabase2()
	ts2 := httptest.NewServer(handler2)
	defer ts2.Close()
	response2, err := http.DefaultClient.Get(ts2.URL + "/app")
	if err != nil {
		t.Fatal(err)
	}
	defer response2.Body.Close()
	body2, _ := io.ReadAll(response2.Body)
	if strings.Contains(string(body2), `window.NDRIVE_API_BASE="https://api.example.com";`) {
		t.Fatal("page should not inject a remote API base by default")
	}
	if csp2 := response2.Header.Get("Content-Security-Policy"); !strings.Contains(csp2, "connect-src 'self'") || strings.Contains(csp2, "https://api.example.com") {
		t.Fatalf("default CSP = %q", csp2)
	}
}
