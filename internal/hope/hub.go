package hope

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Hub struct {
	db *sql.DB
}

func Open(path, defaultProjectRoot string) (*Hub, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("HOPE database path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create HOPE data directory: %w", err)
	}
	db, err := openDatabase(context.Background(), path)
	if err != nil {
		return nil, err
	}
	return &Hub{db: db}, nil
}

func (hub *Hub) Close() error { return hub.db.Close() }

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nowText() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func parseTime(value string) (time.Time, error) { return time.Parse(time.RFC3339Nano, value) }
