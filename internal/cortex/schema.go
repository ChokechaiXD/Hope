package cortex

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

const connectionPragmas = `
PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;
PRAGMA busy_timeout = 5000;
`

const schemaV1 = `
CREATE TABLE IF NOT EXISTS memories (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    scope TEXT NOT NULL,
    scope_key TEXT NOT NULL DEFAULT '',
    memory_key TEXT NOT NULL,
    lifecycle TEXT NOT NULL,
    truth_score REAL NOT NULL,
    utility_score REAL NOT NULL,
    created_by TEXT NOT NULL,
    current_revision INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS memories_scope_idx
ON memories(scope, scope_key, lifecycle);

CREATE TABLE IF NOT EXISTS memory_revisions (
    memory_id TEXT NOT NULL REFERENCES memories(id),
    revision INTEGER NOT NULL,
    title TEXT NOT NULL,
    content TEXT NOT NULL,
    tags_json TEXT NOT NULL,
    session_id TEXT NOT NULL DEFAULT '',
    source_ref TEXT NOT NULL DEFAULT '',
    created_by TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY(memory_id, revision)
);

CREATE TABLE IF NOT EXISTS memory_events (
    id TEXT PRIMARY KEY,
    memory_id TEXT NOT NULL REFERENCES memories(id),
    event_type TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    session_id TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS memory_events_history_idx
ON memory_events(memory_id, created_at);

CREATE TABLE IF NOT EXISTS requests (
    idempotency_key TEXT PRIMARY KEY,
    operation TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS recalls (
    id TEXT PRIMARY KEY,
    query_text TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    session_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS recall_items (
    recall_id TEXT NOT NULL REFERENCES recalls(id),
    memory_id TEXT NOT NULL REFERENCES memories(id),
    rank INTEGER NOT NULL,
    score REAL NOT NULL,
    PRIMARY KEY(recall_id, memory_id)
);

CREATE VIRTUAL TABLE IF NOT EXISTS memory_fts USING fts5(
    memory_id UNINDEXED,
    title,
    content,
    tags
);
`

const schemaV2 = `
CREATE INDEX IF NOT EXISTS memories_stable_key_idx
ON memories(scope, scope_key, memory_key, created_by);
`

const schemaV3 = `
CREATE TABLE IF NOT EXISTS curator_settings (
    id INTEGER PRIMARY KEY CHECK(id = 1),
    mode TEXT NOT NULL,
    run_every_candidates INTEGER NOT NULL,
    batch_limit INTEGER NOT NULL,
    min_agreement INTEGER NOT NULL,
    updated_by TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

INSERT OR IGNORE INTO curator_settings(
    id, mode, run_every_candidates, batch_limit, min_agreement, updated_by, updated_at
) VALUES (
    1, 'manual', 10, 50, 2, 'system', strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
);

CREATE TABLE IF NOT EXISTS curator_runs (
    id TEXT PRIMARY KEY,
    mode TEXT NOT NULL,
    trigger_type TEXT NOT NULL,
    analyzed_count INTEGER NOT NULL,
    ready_count INTEGER NOT NULL,
    attention_count INTEGER NOT NULL,
    applied_count INTEGER NOT NULL,
    actor_id TEXT NOT NULL,
    error_text TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS curator_runs_created_idx
ON curator_runs(created_at DESC);

CREATE TABLE IF NOT EXISTS advisor_settings (
    id INTEGER PRIMARY KEY CHECK(id = 1),
    enabled INTEGER NOT NULL,
    endpoint TEXT NOT NULL,
    model TEXT NOT NULL,
    input_token_budget INTEGER NOT NULL,
    output_token_budget INTEGER NOT NULL,
    effort TEXT NOT NULL,
    updated_by TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

INSERT OR IGNORE INTO advisor_settings(
    id, enabled, endpoint, model, input_token_budget, output_token_budget,
    effort, updated_by, updated_at
) VALUES (
    1, 0, 'http://127.0.0.1:20128/v1', '', 1200, 350,
    'auto', 'system', strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
);

CREATE TABLE IF NOT EXISTS advisor_runs (
    id TEXT PRIMARY KEY,
    model TEXT NOT NULL,
    input_tokens INTEGER NOT NULL,
    output_tokens INTEGER NOT NULL,
    status TEXT NOT NULL,
    summary TEXT NOT NULL DEFAULT '',
    response_json TEXT NOT NULL DEFAULT '{}',
    error_text TEXT NOT NULL DEFAULT '',
    actor_id TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS advisor_runs_created_idx
ON advisor_runs(created_at DESC);
`

var schemaMigrations = []string{schemaV1, schemaV2, schemaV3}

func openDatabase(ctx context.Context, path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if _, err := db.ExecContext(ctx, connectionPragmas); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure sqlite: %w", err)
	}
	if err := applyMigrations(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func applyMigrations(ctx context.Context, db *sql.DB) error {
	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version > len(schemaMigrations) {
		return fmt.Errorf("database schema version %d is newer than supported version %d", version, len(schemaMigrations))
	}
	for index := version; index < len(schemaMigrations); index++ {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin schema migration %d: %w", index+1, err)
		}
		if _, err := tx.ExecContext(ctx, schemaMigrations[index]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply schema migration %d: %w", index+1, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", index+1)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record schema migration %d: %w", index+1, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit schema migration %d: %w", index+1, err)
		}
	}
	return nil
}
