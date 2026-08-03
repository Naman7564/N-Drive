package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"fileservice/internal/config"
)

func TestHealth(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Database.Path = t.TempDir() + "/health.db"
	cfg.Storage.Root = t.TempDir()
	handler, closeDatabase := NewRouterWithCloser(context.Background(), slog.Default(), cfg)
	defer closeDatabase()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health", nil)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if recorder.Body.String() != "{\"status\":\"ok\"}\n" {
		t.Fatalf("body = %q", recorder.Body.String())
	}
	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
	if recorder.Header().Get("X-Request-ID") == "" {
		t.Fatal("X-Request-ID is missing")
	}
}
