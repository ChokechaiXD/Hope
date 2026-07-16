package cortex

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestSchemaMigrationsTrackVersion(t *testing.T) {
	t.Parallel()

	hub, err := Open(Config{DatabasePath: filepath.Join(t.TempDir(), "cortex.db")})
	if err != nil {
		t.Fatalf("open Cortex: %v", err)
	}
	defer hub.Close()
	var version int
	if err := hub.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != 3 {
		t.Fatalf("schema version = %d, want 3", version)
	}
}

func TestOpenRejectsNewerSchema(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "future.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open future db: %v", err)
	}
	if _, err := db.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatalf("set future version: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close future db: %v", err)
	}
	if hub, err := Open(Config{DatabasePath: path}); err == nil {
		_ = hub.Close()
		t.Fatal("opened a database created by a newer Cortex version")
	}
}
