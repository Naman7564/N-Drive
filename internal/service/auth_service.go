package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"fileservice/internal/auth"
	"fileservice/internal/repository"
)

// Single user ID used as the access-token subject. The application has
// exactly one predefined account (database.SingleUserUsername).
const singleUserID = "single"

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

// Login verifies the single user's credentials and starts a session.
func (s *AuthService) Login(ctx context.Context, username, password string) (auth.User, auth.Tokens, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return auth.User{}, auth.Tokens{}, auth.ErrInvalidCredentials
	}
	user, err := s.repository.FindUserByUsername(ctx, username)
	if err != nil {
		return auth.User{}, auth.Tokens{}, auth.ErrInvalidCredentials
	}
	if err := auth.CheckPassword(user.PasswordHash, password); err != nil {
		return auth.User{}, auth.Tokens{}, auth.ErrInvalidCredentials
	}
	tokens, err := s.issueSession(ctx)
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
	tokens, replacement, err := s.newSession()
	if err != nil {
		return auth.Tokens{}, err
	}
	if err := s.repository.RotateSession(ctx, session.ID, replacement, s.now().UTC()); err != nil {
		return auth.Tokens{}, errors.New("invalid refresh token")
	}
	return tokens, nil
}

// Me returns the identity for a validated session's user.
func (s *AuthService) Me(ctx context.Context, userID string) (auth.User, error) {
	if strings.TrimSpace(userID) == "" {
		return auth.User{}, auth.ErrInvalidToken
	}
	user, err := s.repository.FindUserByID(ctx, userID)
	if err != nil {
		return auth.User{}, err
	}
	return user, nil
}

// Logout revokes the session represented by the access token subject/session ID.
func (s *AuthService) Logout(ctx context.Context, sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return auth.ErrInvalidToken
	}
	return s.repository.RevokeSession(ctx, sessionID, s.now().UTC())
}

func (s *AuthService) issueSession(ctx context.Context) (auth.Tokens, error) {
	tokens, session, err := s.newSession()
	if err != nil {
		return auth.Tokens{}, err
	}
	if err := s.repository.CreateSession(ctx, session); err != nil {
		return auth.Tokens{}, err
	}
	return tokens, nil
}

func (s *AuthService) newSession() (auth.Tokens, auth.Session, error) {
	sessionID := uuid.NewString()
	tokens, err := s.tokens.Issue(singleUserID, sessionID)
	if err != nil {
		return auth.Tokens{}, auth.Session{}, err
	}
	return tokens, auth.Session{ID: sessionID, TokenHash: auth.HashRefreshToken(tokens.RefreshToken), ExpiresAt: tokens.RefreshExpiresAt, CreatedAt: s.now().UTC()}, nil
}