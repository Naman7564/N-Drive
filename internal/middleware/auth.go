package middleware

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"fileservice/internal/auth"
)

const ClaimsKey contextKey = "claims"

// Auth validates access tokens supplied by bearer headers or cookies.
type SessionValidator interface {
	ValidateSession(context.Context, string, string, time.Time) error
}

// Auth validates access tokens and their active sessions.
type Auth struct {
	Tokens   *auth.TokenManager
	Sessions SessionValidator
}

// RequireAccessToken protects a route with a valid access token.
func (a Auth) RequireAccessToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := tokenFromRequest(r)
		claims, err := a.Tokens.ValidateAccessToken(raw)
		if err != nil {
			writeAuthError(w)
			return
		}
		_, cookieErr := r.Cookie("access_token")
		hasCookie := cookieErr == nil
		hasBearer := strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ")
		if hasCookie && hasBearer {
			writeAuthError(w)
			return
		}
		if !hasCookie && !hasBearer {
			writeAuthError(w)
			return
		}
		if hasCookie && !hasBearer && !safeCookieMutation(r) {
			writeAuthError(w)
			return
		}
		if a.Sessions == nil || a.Sessions.ValidateSession(r.Context(), claims.ID, claims.Subject, time.Now().UTC()) != nil {
			writeAuthError(w)
			return
		}
		next.ServeHTTP(w, r.WithContext(contextWithClaims(r, claims)))
	})
}

func tokenFromRequest(r *http.Request) string {
	if cookie, err := r.Cookie("access_token"); err == nil && cookie.Value != "" {
		return cookie.Value
	}
	header := r.Header.Get("Authorization")
	if strings.HasPrefix(header, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	}
	return ""
}

func safeCookieMutation(r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
		return true
	}
	return CSRFTokenValid(r)
}

// CSRFTokenValid verifies the header token against the non-HttpOnly CSRF cookie.
func CSRFTokenValid(r *http.Request) bool {
	cookie, err := r.Cookie("csrf_token")
	if err != nil || cookie.Value == "" {
		return false
	}
	header := r.Header.Get("X-CSRF-Token")
	if header == "" || len(header) != len(cookie.Value) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(header), []byte(cookie.Value)) == 1
}

func contextWithClaims(r *http.Request, claims jwt.RegisteredClaims) context.Context {
	return context.WithValue(r.Context(), ClaimsKey, claims)
}

// RateLimiter limits requests per client key in a fixed time window.
type RateLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	entries map[string]rateEntry
	now     func() time.Time
}

type rateEntry struct {
	started time.Time
	count   int
}

// NewRateLimiter creates an in-memory limiter suitable for one API instance.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{limit: limit, window: window, entries: make(map[string]rateEntry), now: time.Now}
}

// Middleware applies rate limiting by remote IP.
func (l *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.RemoteAddr
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			key = host
		}
		if !l.allow(key) {
			w.Header().Set("Retry-After", strconv.Itoa(int(l.window.Seconds())))
			writeRateLimitError(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (l *RateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	for storedKey, storedEntry := range l.entries {
		if now.Sub(storedEntry.started) >= l.window {
			delete(l.entries, storedKey)
		}
	}
	entry := l.entries[key]
	if entry.started.IsZero() || now.Sub(entry.started) >= l.window {
		l.entries[key] = rateEntry{started: now, count: 1}
		return true
	}
	if entry.count >= l.limit {
		return false
	}
	entry.count++
	l.entries[key] = entry
	return true
}

func writeAuthError(w http.ResponseWriter) {
	http.Error(w, "authentication required", http.StatusUnauthorized)
}

func writeRateLimitError(w http.ResponseWriter) {
	http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
}

// NewCSRFToken returns a random token for browser clients that use cookie auth.
func NewCSRFToken() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value[:]), nil
}
