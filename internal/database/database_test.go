package database_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"fileservice/internal/auth"
	"fileservice/internal/database"
)

// queryUser returns the single row of the users table.
func queryUser(t *testing.T, db *sql.DB) (id, username, passwordHash string) {
	t.Helper()
	if err := db.QueryRow(`SELECT id, username, password_hash FROM users`).Scan(&id, &username, &passwordHash); err != nil {
		t.Fatalf("query user: %v", err)
	}
	return id, username, passwordHash
}

func TestSeedCreatesUserWhenEmpty(t *testing.T) {
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"), database.SeedCredentials{Username: "alice", Password: "hunter2-secret"})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	id, username, passwordHash := queryUser(t, db)
	if id != database.SingleUserID {
		t.Fatalf("user id = %q, want %q", id, database.SingleUserID)
	}
	if username != "alice" {
		t.Fatalf("username = %q, want alice", username)
	}
	if err := auth.CheckPassword(passwordHash, "hunter2-secret"); err != nil {
		t.Fatalf("seeded password does not verify: %v", err)
	}
}

// TestSeedDoesNotOverwriteExistingUser proves the critical security property:
// reopening a database with different credentials must never delete or replace
// the existing account.
func TestSeedDoesNotOverwriteExistingUser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := database.Open(context.Background(), path, database.SeedCredentials{Username: "alice", Password: "alice-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db2, err := database.Open(context.Background(), path, database.SeedCredentials{Username: "mallory", Password: "mallory-secret"})
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()

	_, username, passwordHash := queryUser(t, db2)
	if username != "alice" {
		t.Fatalf("username = %q, want alice (existing account must not be overwritten)", username)
	}
	if err := auth.CheckPassword(passwordHash, "alice-secret"); err != nil {
		t.Fatalf("existing password no longer verifies: %v", err)
	}
	if err := auth.CheckPassword(passwordHash, "mallory-secret"); err == nil {
		t.Fatal("new credentials were applied over an existing account")
	}
}

func TestSeedRejectsEmptyCredentials(t *testing.T) {
	if _, err := database.Open(context.Background(), ":memory:", database.SeedCredentials{}); err == nil {
		t.Fatal("Open() error = nil, want error for empty seed credentials")
	}
	if _, err := database.Open(context.Background(), ":memory:", database.SeedCredentials{Username: "alice", Password: ""}); err == nil {
		t.Fatal("Open() error = nil, want error for empty seed password")
	}
}
