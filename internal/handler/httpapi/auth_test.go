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
	"testing"
	"time"

	"fileservice/internal/config"
)

func testConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := config.Config{
		Environment: "test",
		HTTP:        config.HTTPConfig{Address: ":0", ReadHeaderTimeout: 1, ReadTimeout: 1, WriteTimeout: 1, IdleTimeout: 1, ShutdownTimeout: 1, MaxHeaderBytes: 4096},
		Auth:        config.AuthConfig{JWTSecret: "01234567890123456789012345678901", JWTIssuer: "fileservice-test", AccessTokenTTL: time.Minute, RefreshTokenTTL: time.Hour, LoginRateLimit: 20, LoginRateWindow: time.Minute, RefreshRateLimit: 20, RefreshRateWindow: time.Minute},
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

	body := bytes.NewBufferString(`{"email":"user@example.com","password":"correct horse battery staple"}`)
	request, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/register", body)
	request.Header.Set("Content-Type", "application/json")
	response, err := httpClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(response.Body)
		t.Fatalf("register status = %d, body = %s", response.StatusCode, data)
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
