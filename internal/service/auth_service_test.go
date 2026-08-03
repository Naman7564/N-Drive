package service

import (
	"context"
	"testing"
	"time"

	"fileservice/internal/auth"
	"fileservice/internal/database"
	"fileservice/internal/repository"
)

func newTestService(t *testing.T) *AuthService {
	t.Helper()
	db, err := database.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	tokens, err := auth.NewTokenManager("01234567890123456789012345678901", "fileservice", time.Minute, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return NewAuthService(repository.NewAuthRepository(db), tokens)
}

func TestAuthServiceRefreshRotatesToken(t *testing.T) {
	service := newTestService(t)
	_, first, err := service.Register(context.Background(), "User@Example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Refresh(context.Background(), first.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if second.RefreshToken == first.RefreshToken {
		t.Fatal("refresh token was not rotated")
	}
	if _, err := service.Refresh(context.Background(), first.RefreshToken); err == nil {
		t.Fatal("revoked refresh token was accepted")
	}
}

func TestAuthServiceInvalidLoginIsGeneric(t *testing.T) {
	service := newTestService(t)
	if _, _, err := service.Login(context.Background(), "nobody@example.com", "wrong password"); err != auth.ErrInvalidCredentials {
		t.Fatalf("error = %v, want invalid credentials", err)
	}
}
