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
	_, first, err := service.Login(context.Background(), "Naman", "7564")
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

func TestAuthServiceMe(t *testing.T) {
	service := newTestService(t)
	user, err := service.Me(context.Background(), singleUserID)
	if err != nil {
		t.Fatalf("me: %v", err)
	}
	if user.Username != "Naman" {
		t.Fatalf("username = %q, want Naman", user.Username)
	}
	if _, err := service.Me(context.Background(), "missing-user"); err == nil {
		t.Fatal("me for unknown user succeeded")
	}
	if _, err := service.Me(context.Background(), ""); err == nil {
		t.Fatal("me for empty user succeeded")
	}
}

func TestAuthServiceInvalidLoginIsGeneric(t *testing.T) {
	service := newTestService(t)
	if _, _, err := service.Login(context.Background(), "nobody", "wrong password"); err != auth.ErrInvalidCredentials {
		t.Fatalf("error = %v, want invalid credentials", err)
	}
}
