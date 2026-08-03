package auth

import (
	"testing"
	"time"
)

func TestPasswordHashing(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckPassword(hash, "correct horse battery staple"); err != nil {
		t.Fatalf("valid password rejected: %v", err)
	}
	if err := CheckPassword(hash, "wrong password"); err != ErrInvalidCredentials {
		t.Fatalf("wrong password error = %v", err)
	}
}

func TestTokenManagerRejectsWrongSecret(t *testing.T) {
	manager, err := NewTokenManager("01234567890123456789012345678901", "fileservice", time.Minute, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	manager.now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	tokens, err := manager.Issue("user", "session")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := manager.ValidateAccessToken(tokens.AccessToken)
	if err != nil || claims.Subject != "user" || claims.ID != "session" {
		t.Fatalf("validated claims = %+v, error = %v", claims, err)
	}
	if HashRefreshToken(tokens.RefreshToken) == tokens.RefreshToken {
		t.Fatal("refresh token must not be stored in plaintext")
	}
}
