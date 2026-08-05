package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/bcrypt"

	_ "modernc.org/sqlite"
)

// Single-user identity. The only predefined account in the application.
const (
	SingleUserID       = "single"
	SingleUserUsername = "Naman"
	SingleUserPassword = "7564"
)

// Open opens the SQLite database and applies the current schema.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable sqlite foreign keys: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite database: %w", err)
	}
	if err := Migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// Migrate applies the single-user schema, migrating legacy multi-user
// databases in place, and guarantees the single predefined user exists.
func Migrate(ctx context.Context, db *sql.DB) error {
	legacy, err := isLegacySchema(ctx, db)
	if err != nil {
		return err
	}
	if legacy {
		if err := migrateLegacy(ctx, db); err != nil {
			return err
		}
	}
	if _, err := db.ExecContext(ctx, singleUserSchema); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}
	if err := seedSingleUser(ctx, db); err != nil {
		return err
	}
	_, _ = db.ExecContext(ctx, `PRAGMA user_version = 1`)
	return nil
}

const singleUserSchema = `
CREATE TABLE IF NOT EXISTS users (id TEXT PRIMARY KEY,username TEXT NOT NULL UNIQUE,password_hash TEXT NOT NULL,created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS sessions (id TEXT PRIMARY KEY,token_hash TEXT NOT NULL UNIQUE,expires_at TEXT NOT NULL,revoked_at TEXT,created_at TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS idx_sessions_token_hash ON sessions(token_hash);
CREATE TABLE IF NOT EXISTS folders (id TEXT PRIMARY KEY,parent_id TEXT REFERENCES folders(id) ON DELETE CASCADE,name TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL,UNIQUE(parent_id,name));
CREATE UNIQUE INDEX IF NOT EXISTS idx_folders_root_name ON folders(name) WHERE parent_id IS NULL;
CREATE TABLE IF NOT EXISTS files (id TEXT PRIMARY KEY,folder_id TEXT REFERENCES folders(id) ON DELETE SET NULL,storage_key TEXT NOT NULL UNIQUE,name TEXT NOT NULL,content_type TEXT NOT NULL,size INTEGER NOT NULL,checksum TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL,deleted_at TEXT);
CREATE INDEX IF NOT EXISTS idx_files_folder_deleted ON files(folder_id,deleted_at);
CREATE INDEX IF NOT EXISTS idx_files_checksum ON files(checksum);
CREATE TABLE IF NOT EXISTS audit_events (id TEXT PRIMARY KEY,action TEXT NOT NULL,resource_id TEXT,metadata TEXT,created_at TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_events(created_at);
`

// isLegacySchema reports whether the database still uses the old multi-user
// schema (users table keyed by email with user_id ownership columns).
func isLegacySchema(ctx context.Context, db *sql.DB) (bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT name FROM pragma_table_info('users') WHERE name = 'email'`)
	if err != nil {
		return false, fmt.Errorf("inspect schema: %w", err)
	}
	defer rows.Close()
	return rows.Next(), nil
}

// migrateLegacy rebuilds the multi-user tables without user ownership.
// Standard SQLite 12-step migration; foreign keys must be toggled outside
// the transaction, which is why this runs as its own call.
func migrateLegacy(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return fmt.Errorf("disable foreign keys for migration: %w", err)
	}
	const rebuild = `
BEGIN;
CREATE TABLE users_new (id TEXT PRIMARY KEY,username TEXT NOT NULL UNIQUE,password_hash TEXT NOT NULL,created_at TEXT NOT NULL);
CREATE TABLE sessions_new (id TEXT PRIMARY KEY,token_hash TEXT NOT NULL UNIQUE,expires_at TEXT NOT NULL,revoked_at TEXT,created_at TEXT NOT NULL);
CREATE TABLE folders_new (id TEXT PRIMARY KEY,parent_id TEXT REFERENCES folders(id) ON DELETE CASCADE,name TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL,UNIQUE(parent_id,name));
CREATE TABLE files_new (id TEXT PRIMARY KEY,folder_id TEXT REFERENCES folders(id) ON DELETE SET NULL,storage_key TEXT NOT NULL UNIQUE,name TEXT NOT NULL,content_type TEXT NOT NULL,size INTEGER NOT NULL,checksum TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL,deleted_at TEXT);
CREATE TABLE audit_events_new (id TEXT PRIMARY KEY,action TEXT NOT NULL,resource_id TEXT,metadata TEXT,created_at TEXT NOT NULL);
INSERT INTO sessions_new (id,token_hash,expires_at,revoked_at,created_at) SELECT id,token_hash,expires_at,revoked_at,created_at FROM sessions;
INSERT INTO folders_new (id,parent_id,name,created_at,updated_at) SELECT id,parent_id,name,created_at,updated_at FROM folders;
INSERT INTO files_new (id,folder_id,storage_key,name,content_type,size,checksum,created_at,updated_at,deleted_at) SELECT id,folder_id,storage_key,name,content_type,size,checksum,created_at,updated_at,deleted_at FROM files;
INSERT INTO audit_events_new (id,action,resource_id,metadata,created_at) SELECT id,action,resource_id,metadata,created_at FROM audit_events;
DROP TABLE users;
DROP TABLE sessions;
DROP TABLE folders;
DROP TABLE files;
DROP TABLE audit_events;
ALTER TABLE users_new RENAME TO users;
ALTER TABLE sessions_new RENAME TO sessions;
ALTER TABLE folders_new RENAME TO folders;
ALTER TABLE files_new RENAME TO files;
ALTER TABLE audit_events_new RENAME TO audit_events;
COMMIT;
`
	if _, err := db.ExecContext(ctx, rebuild); err != nil {
		return fmt.Errorf("migrate legacy schema: %w", err)
	}
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		return fmt.Errorf("re-enable foreign keys after migration: %w", err)
	}
	return nil
}

// seedSingleUser replaces any user rows with the single predefined account.
func seedSingleUser(ctx context.Context, db *sql.DB) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(SingleUserPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash single-user password: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.ExecContext(ctx, `DELETE FROM users; INSERT INTO users (id,username,password_hash,created_at) VALUES (?,?,?,?)`, SingleUserID, SingleUserUsername, string(hash), now); err != nil {
		return fmt.Errorf("seed single user: %w", err)
	}
	return nil
}
