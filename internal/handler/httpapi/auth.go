package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"fileservice/internal/auth"
	"fileservice/internal/middleware"
	"fileservice/internal/service"
)

type authHandler struct {
	service       *service.AuthService
	secureCookies bool
	cookieDomain  string
}

type credentialsRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type authResponse struct {
	UserID      string    `json:"user_id"`
	Username    string    `json:"username"`
	AccessToken string    `json:"access_token"`
	ExpiresAt   time.Time `json:"expires_at"`
}

func (h *authHandler) login(w http.ResponseWriter, r *http.Request) {
	var request credentialsRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	user, tokens, err := h.service.Login(r.Context(), request.Username, request.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	h.writeTokens(w, user, tokens)
}

func (h *authHandler) refresh(w http.ResponseWriter, r *http.Request) {
	if _, cookieErr := r.Cookie("refresh_token"); cookieErr == nil && r.Header.Get("Authorization") != "" {
		writeError(w, http.StatusUnauthorized, "mixed token transport is not allowed")
		return
	}
	if _, cookieErr := r.Cookie("refresh_token"); cookieErr == nil && !middleware.CSRFTokenValid(r) {
		writeError(w, http.StatusUnauthorized, "csrf validation failed")
		return
	}
	refreshToken := tokenFromRequest(r, "refresh_token")
	if refreshToken == "" {
		writeError(w, http.StatusUnauthorized, "refresh token required")
		return
	}
	tokens, err := h.service.Refresh(r.Context(), refreshToken)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}
	h.setAccessCookie(w, tokens.AccessToken, tokens.ExpiresAt)
	h.setRefreshCookie(w, tokens.RefreshToken, tokens.RefreshExpiresAt)
	writeJSON(w, http.StatusOK, map[string]any{"access_token": tokens.AccessToken, "expires_at": tokens.ExpiresAt})
}

func (h *authHandler) me(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	user, err := h.service.Me(r.Context(), claims.Subject)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	// Re-issue the CSRF cookie so cookie-auth clients keep a fresh token
	// across reloads instead of letting it lapse within the refresh lifetime.
	h.setCSRFToken(w)
	writeJSON(w, http.StatusOK, authResponse{UserID: user.ID, Username: user.Username, ExpiresAt: claims.ExpiresAt.Time})
}

func (h *authHandler) logout(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if err := h.service.Logout(r.Context(), claims.ID); err != nil {
		writeError(w, http.StatusUnauthorized, "session is invalid")
		return
	}
	h.clearCookies(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *authHandler) writeTokens(w http.ResponseWriter, user auth.User, tokens auth.Tokens) {
	h.setCSRFToken(w)
	h.setAccessCookie(w, tokens.AccessToken, tokens.ExpiresAt)
	h.setRefreshCookie(w, tokens.RefreshToken, tokens.RefreshExpiresAt)
	writeJSON(w, http.StatusOK, authResponse{UserID: user.ID, Username: user.Username, AccessToken: tokens.AccessToken, ExpiresAt: tokens.ExpiresAt})
}

func (h *authHandler) setCSRFToken(w http.ResponseWriter) {
	token, err := middleware.NewCSRFToken()
	if err != nil {
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "csrf_token", Value: token, Path: "/", Domain: h.cookieDomain, MaxAge: 7 * 24 * 60 * 60, Secure: h.secureCookies, SameSite: http.SameSiteLaxMode})
}

func (h *authHandler) setAccessCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{Name: "access_token", Value: token, Path: "/", Domain: h.cookieDomain, Expires: expires, MaxAge: int(time.Until(expires).Seconds()), HttpOnly: true, Secure: h.secureCookies, SameSite: http.SameSiteLaxMode})
}

func (h *authHandler) setRefreshCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{Name: "refresh_token", Value: token, Path: "/api/auth/refresh", Domain: h.cookieDomain, Expires: expires, MaxAge: int(time.Until(expires).Seconds()), HttpOnly: true, Secure: h.secureCookies, SameSite: http.SameSiteStrictMode})
}

func (h *authHandler) clearCookies(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: "access_token", Value: "", Path: "/", Domain: h.cookieDomain, MaxAge: -1, HttpOnly: true, Secure: h.secureCookies, SameSite: http.SameSiteLaxMode})
	http.SetCookie(w, &http.Cookie{Name: "refresh_token", Value: "", Path: "/api/auth/refresh", Domain: h.cookieDomain, MaxAge: -1, HttpOnly: true, Secure: h.secureCookies, SameSite: http.SameSiteStrictMode})
	http.SetCookie(w, &http.Cookie{Name: "csrf_token", Value: "", Path: "/", Domain: h.cookieDomain, MaxAge: -1, Secure: h.secureCookies, SameSite: http.SameSiteLaxMode})
}

func tokenFromRequest(r *http.Request, cookieName string) string {
	if cookie, err := r.Cookie(cookieName); err == nil && cookie.Value != "" {
		return cookie.Value
	}
	header := r.Header.Get("Authorization")
	if strings.HasPrefix(header, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	}
	return ""
}

func decodeJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "content type must be application/json")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}
