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

	// Clients without a session are rejected.
	anonClient := ts.Client()
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
