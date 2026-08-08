package database_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"fileservice/internal/auth"
	"fileservice/internal/database"

	_ "modernc.org/sqlite"
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

// TestMigrateAddsMountColumnsToExistingDatabase proves that a database created
// before multi-disk support is upgraded in place: the mount column is added to
// files and folders with the legacy default, and the root-folder uniqueness
// index now allows the same root folder name on different disks.
func TestMigrateAddsMountColumnsToExistingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// The pre-multi-disk single-user schema, without any mount columns.
	const legacySchema = `
CREATE TABLE users (id TEXT PRIMARY KEY,username TEXT NOT NULL UNIQUE,password_hash TEXT NOT NULL,created_at TEXT NOT NULL);
CREATE TABLE sessions (id TEXT PRIMARY KEY,token_hash TEXT NOT NULL UNIQUE,expires_at TEXT NOT NULL,revoked_at TEXT,created_at TEXT NOT NULL);
CREATE TABLE folders (id TEXT PRIMARY KEY,parent_id TEXT REFERENCES folders(id) ON DELETE CASCADE,name TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL,UNIQUE(parent_id,name));
CREATE UNIQUE INDEX idx_folders_root_name ON folders(name) WHERE parent_id IS NULL;
CREATE TABLE files (id TEXT PRIMARY KEY,folder_id TEXT REFERENCES folders(id) ON DELETE SET NULL,storage_key TEXT NOT NULL UNIQUE,name TEXT NOT NULL,content_type TEXT NOT NULL,size INTEGER NOT NULL,checksum TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL,deleted_at TEXT);
CREATE INDEX idx_files_folder_deleted ON files(folder_id,deleted_at);
CREATE TABLE audit_events (id TEXT PRIMARY KEY,action TEXT NOT NULL,resource_id TEXT,metadata TEXT,created_at TEXT NOT NULL);
`
	if _, err := raw.Exec(legacySchema); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := database.Open(context.Background(), path, database.SeedCredentials{Username: "alice", Password: "alice-secret"})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// The mount columns must exist after the upgrade.
	for _, table := range []string{"files", "folders"} {
		var found bool
		if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM pragma_table_info(?) WHERE name = 'mount')`, table).Scan(&found); err != nil {
			t.Fatal(err)
		}
		if !found {
			t.Fatalf("%s.mount column was not added by the migration", table)
		}
	}

	// Legacy inserts that do not name a mount get the default mount.
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO folders (id,parent_id,name,mount,created_at,updated_at) VALUES ('f1',NULL,'Docs','default',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO files (id,folder_id,storage_key,name,content_type,size,checksum,created_at,updated_at) VALUES ('x','f1','k','a.txt','text/plain',1,'c',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	var mount string
	if err := db.QueryRow(`SELECT mount FROM files WHERE id='x'`).Scan(&mount); err != nil {
		t.Fatal(err)
	}
	if mount != "default" {
		t.Fatalf("legacy file mount = %q, want default", mount)
	}

	// The same root folder name must be allowed on different disks.
	if _, err := db.Exec(`INSERT INTO folders (id,parent_id,name,mount,created_at,updated_at) VALUES ('f2',NULL,'Docs','media',?,?)`, now, now); err != nil {
		t.Fatalf("root folder 'Docs' on a second disk was rejected: %v", err)
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
