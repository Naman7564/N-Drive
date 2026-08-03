package middleware

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestIDPreservesValidHeader(t *testing.T) {
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := RequestIDFromContext(r); got != "client-request" {
			t.Errorf("context request ID = %q", got)
		}
	}))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-Request-ID", "client-request")
	handler.ServeHTTP(recorder, request)

	if got := recorder.Header().Get("X-Request-ID"); got != "client-request" {
		t.Fatalf("response request ID = %q", got)
	}
}

func TestRequestIDRejectsUnsafeHeader(t *testing.T) {
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if RequestIDFromContext(r) == "bad value" {
			t.Error("unsafe request ID was preserved")
		}
	}))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-Request-ID", "bad value")
	handler.ServeHTTP(recorder, request)

	if got := recorder.Header().Get("X-Request-ID"); got == "bad value" || got == "" {
		t.Fatalf("request ID = %q, want generated safe ID", got)
	}
}

func TestRecoverReturnsInternalServerError(t *testing.T) {
	handler := Recover(slog.Default())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
}
