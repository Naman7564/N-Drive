package service

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"

	"fileservice/internal/auth"
	"fileservice/internal/repository"
)

// AuthService coordinates authentication business rules.
type AuthService struct {
	repository *repository.AuthRepository
	tokens     *auth.TokenManager
	now        func() time.Time
}

// NewAuthService creates an authentication service.
func NewAuthService(repository *repository.AuthRepository, tokens *auth.TokenManager) *AuthService {
	return &AuthService{repository: repository, tokens: tokens, now: time.Now}
}

// Register creates a user and starts a session.
func (s *AuthService) Register(ctx context.Context, email, password string) (auth.User, auth.Tokens, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		return auth.User{}, auth.Tokens{}, err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return auth.User{}, auth.Tokens{}, err
	}
	user := auth.User{ID: uuid.NewString(), Email: email, PasswordHash: hash, CreatedAt: s.now().UTC()}
	tokens, session, err := s.newSession(user.ID)
	if err != nil {
		return auth.User{}, auth.Tokens{}, err
	}
	if err := s.repository.CreateUserAndSession(ctx, user, session); err != nil {
		return auth.User{}, auth.Tokens{}, err
	}
	return user, tokens, nil
}

// Login verifies credentials and starts a session.
func (s *AuthService) Login(ctx context.Context, email, password string) (auth.User, auth.Tokens, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		return auth.User{}, auth.Tokens{}, auth.ErrInvalidCredentials
	}
	user, err := s.repository.FindUserByEmail(ctx, email)
	if err != nil {
		return auth.User{}, auth.Tokens{}, auth.ErrInvalidCredentials
	}
	if err := auth.CheckPassword(user.PasswordHash, password); err != nil {
		return auth.User{}, auth.Tokens{}, auth.ErrInvalidCredentials
	}
	tokens, err := s.issueSession(ctx, user.ID)
	if err != nil {
		return auth.User{}, auth.Tokens{}, err
	}
	return user, tokens, nil
}

// Refresh rotates a refresh token and revokes the previous session.
func (s *AuthService) Refresh(ctx context.Context, refreshToken string) (auth.Tokens, error) {
	if strings.TrimSpace(refreshToken) == "" {
		return auth.Tokens{}, auth.ErrInvalidToken
	}
	session, err := s.repository.FindActiveSessionByTokenHash(ctx, auth.HashRefreshToken(refreshToken), s.now().UTC())
	if err != nil {
		return auth.Tokens{}, auth.ErrInvalidToken
	}
	tokens, replacement, err := s.newSession(session.UserID)
	if err != nil {
		return auth.Tokens{}, err
	}
	if err := s.repository.RotateSession(ctx, session.ID, replacement, s.now().UTC()); err != nil {
		return auth.Tokens{}, fmt.Errorf("rotate refresh session: %w", err)
	}
	return tokens, nil
}

// Logout revokes the session represented by the access token subject/session ID.
func (s *AuthService) Logout(ctx context.Context, sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return auth.ErrInvalidToken
	}
	return s.repository.RevokeSession(ctx, sessionID, s.now().UTC())
}

func (s *AuthService) issueSession(ctx context.Context, userID string) (auth.Tokens, error) {
	tokens, session, err := s.newSession(userID)
	if err != nil {
		return auth.Tokens{}, err
	}
	if err := s.repository.CreateSession(ctx, session); err != nil {
		return auth.Tokens{}, err
	}
	return tokens, nil
}

func (s *AuthService) newSession(userID string) (auth.Tokens, auth.Session, error) {
	sessionID := uuid.NewString()
	tokens, err := s.tokens.Issue(userID, sessionID)
	if err != nil {
		return auth.Tokens{}, auth.Session{}, err
	}
	return tokens, auth.Session{ID: sessionID, UserID: userID, TokenHash: auth.HashRefreshToken(tokens.RefreshToken), ExpiresAt: tokens.RefreshExpiresAt, CreatedAt: s.now().UTC()}, nil
}

func normalizeEmail(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Address != value || len(value) > 254 {
		return "", errors.New("valid email is required")
	}
	return value, nil
}
