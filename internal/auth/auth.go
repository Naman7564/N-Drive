package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidToken       = errors.New("invalid token")
	ErrSessionRevoked     = errors.New("session revoked")
)

// User is the authentication identity persisted by the application.
type User struct {
	ID           string
	Username     string
	PasswordHash string
	CreatedAt    time.Time
}

// Session represents a refresh-token-backed login session.
type Session struct {
	ID        string
	TokenHash string
	ExpiresAt time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
}

// Tokens contains the short-lived access token and one-time refresh token.
type Tokens struct {
	AccessToken      string
	RefreshToken     string
	ExpiresAt        time.Time
	RefreshExpiresAt time.Time
}

// TokenManager creates and validates signed access tokens.
type TokenManager struct {
	secret     []byte
	issuer     string
	accessTTL  time.Duration
	refreshTTL time.Duration
	now        func() time.Time
}

// NewTokenManager creates an HMAC-SHA256 token manager.
func NewTokenManager(secret, issuer string, accessTTL, refreshTTL time.Duration) (*TokenManager, error) {
	if len(secret) < 32 {
		return nil, fmt.Errorf("JWT secret must be at least 32 bytes")
	}
	if strings.TrimSpace(issuer) == "" {
		return nil, fmt.Errorf("JWT issuer must not be empty")
	}
	if accessTTL <= 0 || refreshTTL <= accessTTL {
		return nil, fmt.Errorf("token lifetimes are invalid")
	}
	return &TokenManager{secret: []byte(secret), issuer: issuer, accessTTL: accessTTL, refreshTTL: refreshTTL, now: time.Now}, nil
}

// HashPassword returns a bcrypt password hash.
func HashPassword(password string) (string, error) {
	if password == "" {
		return "", fmt.Errorf("password must not be empty")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

// CheckPassword verifies a plaintext password against a bcrypt hash.
func CheckPassword(hash, password string) error {
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return ErrInvalidCredentials
	}
	return nil
}

// Issue creates an access token and cryptographically random refresh token.
func (m *TokenManager) Issue(userID, sessionID string) (Tokens, error) {
	now := m.now().UTC()
	expiresAt := now.Add(m.accessTTL)
	claims := jwt.RegisteredClaims{
		Issuer:    m.issuer,
		Subject:   userID,
		ID:        sessionID,
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(expiresAt),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	accessToken, err := token.SignedString(m.secret)
	if err != nil {
		return Tokens{}, fmt.Errorf("sign access token: %w", err)
	}
	refreshToken, err := randomToken()
	if err != nil {
		return Tokens{}, fmt.Errorf("create refresh token: %w", err)
	}
	return Tokens{AccessToken: accessToken, RefreshToken: refreshToken, ExpiresAt: expiresAt, RefreshExpiresAt: now.Add(m.refreshTTL)}, nil
}

// ValidateAccessToken validates signature, issuer, expiry, and signing algorithm.
func (m *TokenManager) ValidateAccessToken(raw string) (jwt.RegisteredClaims, error) {
	var claims jwt.RegisteredClaims
	token, err := jwt.ParseWithClaims(raw, &claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, ErrInvalidToken
		}
		return m.secret, nil
	}, jwt.WithIssuer(m.issuer), jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithTimeFunc(m.now))
	if err != nil || !token.Valid || claims.Subject == "" || claims.ID == "" {
		return jwt.RegisteredClaims{}, ErrInvalidToken
	}
	return claims, nil
}

// RefreshTTL returns the configured refresh-token lifetime.
func (m *TokenManager) RefreshTTL() time.Duration { return m.refreshTTL }

// HashRefreshToken returns a non-reversible digest suitable for persistence.
func HashRefreshToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func randomToken() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value[:]), nil
}
