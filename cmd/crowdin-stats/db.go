package main

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

func openDB(path string) (*sql.DB, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create db directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite has no concurrent-writer support; a single connection avoids
	// "database is locked" errors under WAL with multiple writers.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	// CREATE TABLE IF NOT EXISTS doesn't add new columns to a projects table
	// that already existed before revoke_token_hash was introduced — add it
	// here, tolerating the "duplicate column name" error on DBs where the
	// column already exists (there's no migration framework in this project).
	if _, err := db.Exec(`ALTER TABLE projects ADD COLUMN revoke_token_hash TEXT`); err != nil &&
		!strings.Contains(err.Error(), "duplicate column name") {
		db.Close()
		return nil, fmt.Errorf("migrate projects.revoke_token_hash: %w", err)
	}

	// Must run after the migration above — on a DB that predates
	// revoke_token_hash, creating this index any earlier would fail with
	// "no such column" since the column wouldn't exist yet at that point.
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_projects_revoke_token_hash ON projects (revoke_token_hash)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create revoke_token_hash index: %w", err)
	}

	return db, nil
}
