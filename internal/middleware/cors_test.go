package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORSMiddleware(t *testing.T) {
	handler := NewCORS([]string{"https://files.example.com"}).Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("allowed origin gets CORS headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/files", nil)
		req.Header.Set("Origin", "https://files.example.com")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://files.example.com" {
			t.Fatalf("Allow-Origin = %q", got)
		}
		if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
			t.Fatalf("Allow-Credentials = %q", got)
		}
	})

	t.Run("preflight is answered without reaching the handler", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/api/auth/login", nil)
		req.Header.Set("Origin", "https://files.example.com")
		req.Header.Set("Access-Control-Request-Method", "POST")
		req.Header.Set("Access-Control-Request-Headers", "content-type,authorization")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("preflight status = %d, want 204", rec.Code)
		}
		if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
			t.Fatal("preflight missing Allow-Methods")
		}
		if got := rec.Header().Get("Access-Control-Allow-Headers"); got == "" {
			t.Fatal("preflight missing Allow-Headers")
		}
	})

	t.Run("disallowed origin gets no CORS headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/files", nil)
		req.Header.Set("Origin", "https://evil.example.com")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("disallowed origin leaked Allow-Origin %q", got)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	})

	t.Run("origin with a port is matched exactly", func(t *testing.T) {
		// Dev servers commonly run on a port; the origin must match including it.
		dev := NewCORS([]string{"https://files.example.com:5173"}).Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
		req := httptest.NewRequest(http.MethodGet, "/api/files", nil)
		req.Header.Set("Origin", "https://files.example.com:5173")
		rec := httptest.NewRecorder()
		dev.ServeHTTP(rec, req)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://files.example.com:5173" {
			t.Fatalf("Allow-Origin = %q", got)
		}
		req2 := httptest.NewRequest(http.MethodGet, "/api/files", nil)
		req2.Header.Set("Origin", "https://files.example.com")
		rec2 := httptest.NewRecorder()
		dev.ServeHTTP(rec2, req2)
		if got := rec2.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("portless origin should not match a port-scoped policy: %q", got)
		}
	})

	t.Run("request without Origin passes through", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/files", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("unexpected Allow-Origin %q", got)
		}
	})

	t.Run("empty config disables CORS", func(t *testing.T) {
		disabled := NewCORS(nil).Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
		req := httptest.NewRequest(http.MethodOptions, "/api/auth/login", nil)
		req.Header.Set("Origin", "https://files.example.com")
		req.Header.Set("Access-Control-Request-Method", "POST")
		rec := httptest.NewRecorder()
		disabled.ServeHTTP(rec, req)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("empty config leaked Allow-Origin %q", got)
		}
	})
}
