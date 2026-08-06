package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"fileservice/internal/auth"
	"fileservice/internal/database"
)

func newPruneDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := database.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestPruneSessions(t *testing.T) {
	db := newPruneDB(t)
	repo := NewAuthRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()

	sessions := []auth.Session{
		{ID: "active", TokenHash: auth.HashRefreshToken("active-token"), ExpiresAt: now.Add(time.Hour), CreatedAt: now},
		{ID: "expired", TokenHash: auth.HashRefreshToken("expired-token"), ExpiresAt: now.Add(-time.Minute), CreatedAt: now.Add(-2 * time.Hour)},
		{ID: "revoked-old", TokenHash: auth.HashRefreshToken("revoked-old-token"), ExpiresAt: now.Add(time.Hour), CreatedAt: now.Add(-9 * 24 * time.Hour)},
		{ID: "revoked-recent", TokenHash: auth.HashRefreshToken("revoked-recent-token"), ExpiresAt: now.Add(time.Hour), CreatedAt: now},
	}
	for _, session := range sessions {
		if err := repo.CreateSession(ctx, session); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.RevokeSession(ctx, "revoked-old", now.Add(-8*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := repo.RevokeSession(ctx, "revoked-recent", now.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}

	deleted, err := repo.PruneSessions(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 {
		t.Fatalf("deleted = %d, want 2 (expired + revoked beyond retention)", deleted)
	}
	for _, id := range []string{"active", "revoked-recent"} {
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE id = ?`, id).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("session %q was pruned", id)
		}
	}
	for _, id := range []string{"expired", "revoked-old"} {
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE id = ?`, id).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("session %q was not pruned", id)
		}
	}
}

func TestPruneAudit(t *testing.T) {
	db := newPruneDB(t)
	repo := NewFileRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()

	events := []AuditEvent{
		{ID: "old", Action: "upload", CreatedAt: now.Add(-100 * 24 * time.Hour)},
		{ID: "recent", Action: "upload", CreatedAt: now.Add(-time.Hour)},
	}
	for _, event := range events {
		if err := repo.Audit(ctx, event); err != nil {
			t.Fatal(err)
		}
	}

	deleted, err := repo.PruneAudit(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
	var remaining int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 1 {
		t.Fatalf("remaining audit events = %d, want 1", remaining)
	}
}
