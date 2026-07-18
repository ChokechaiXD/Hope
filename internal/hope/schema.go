package hope

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

const pragmas = `
PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;
PRAGMA busy_timeout = 5000;
`

const schemaV1 = `
CREATE TABLE agents (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    role TEXT NOT NULL,
    profile TEXT NOT NULL UNIQUE,
    telegram_url TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE work_modes (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL,
    integrations_json TEXT NOT NULL,
    agents_json TEXT NOT NULL,
    open_telegram INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE project_roots (
    path TEXT PRIMARY KEY
);

CREATE TABLE projects (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    path TEXT NOT NULL UNIQUE,
    kind TEXT NOT NULL,
    active INTEGER NOT NULL DEFAULT 1,
    updated_at TEXT NOT NULL
);

CREATE TABLE skills (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL,
    path TEXT NOT NULL,
    source TEXT NOT NULL,
    keywords_json TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT '',
    project TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 1,
    use_count INTEGER NOT NULL DEFAULT 0,
    success_count INTEGER NOT NULL DEFAULT 0,
    failure_count INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL
);

CREATE VIRTUAL TABLE skill_fts USING fts5(
    skill_id UNINDEXED,
    name,
    description,
    keywords
);

CREATE TABLE managed_processes (
    process_key TEXT PRIMARY KEY,
    pid INTEGER NOT NULL,
    command TEXT NOT NULL,
    started_at TEXT NOT NULL
);

CREATE TABLE action_events (
    id TEXT PRIMARY KEY,
    target TEXT NOT NULL,
    action TEXT NOT NULL,
    status TEXT NOT NULL,
    message TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX action_events_created_idx ON action_events(created_at DESC);
`

const schemaV2 = `
CREATE TABLE context_packs (
    id TEXT PRIMARY KEY,
    request_key TEXT NOT NULL UNIQUE,
    agent_id TEXT NOT NULL,
    session_id TEXT NOT NULL DEFAULT '',
    query TEXT NOT NULL,
    project_id TEXT NOT NULL DEFAULT '',
    router TEXT NOT NULL DEFAULT 'deterministic',
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);

CREATE TABLE context_pack_skills (
    pack_id TEXT NOT NULL REFERENCES context_packs(id) ON DELETE CASCADE,
    skill_id TEXT NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    position INTEGER NOT NULL,
    score REAL NOT NULL,
    reason TEXT NOT NULL,
    outcome TEXT NOT NULL DEFAULT '',
    feedback_at TEXT NOT NULL DEFAULT '',
    PRIMARY KEY(pack_id, skill_id)
);

CREATE TABLE skill_feedback_events (
    request_key TEXT PRIMARY KEY,
    pack_id TEXT NOT NULL REFERENCES context_packs(id) ON DELETE CASCADE,
    skill_id TEXT NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    agent_id TEXT NOT NULL,
    outcome TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX context_packs_agent_created_idx ON context_packs(agent_id, created_at DESC);
CREATE INDEX skill_feedback_skill_created_idx ON skill_feedback_events(skill_id, created_at DESC);
`

const schemaV3 = `
ALTER TABLE agents ADD COLUMN avatar_path TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN summary TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN capabilities_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE agents ADD COLUMN persona_path TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN persona_note TEXT NOT NULL DEFAULT '';

ALTER TABLE projects RENAME TO projects_v2;
CREATE TABLE projects (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    path TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    goal TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active',
    progress INTEGER NOT NULL DEFAULT 0 CHECK(progress >= 0 AND progress <= 100),
    current_state TEXT NOT NULL DEFAULT '',
    next_action TEXT NOT NULL DEFAULT '',
    active INTEGER NOT NULL DEFAULT 1,
    updated_at TEXT NOT NULL
);
INSERT INTO projects(id,name,path,kind,active,updated_at)
SELECT id,name,path,kind,active,updated_at FROM projects_v2;
DROP TABLE projects_v2;
CREATE UNIQUE INDEX projects_path_unique_idx ON projects(path) WHERE path <> '';
CREATE INDEX projects_status_idx ON projects(status, updated_at DESC);

CREATE TABLE project_agents (
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    PRIMARY KEY(project_id, agent_id)
);

ALTER TABLE skills ADD COLUMN source_url TEXT NOT NULL DEFAULT '';
`

const schemaV4 = `
UPDATE agents
SET role = 'Deputy Orchestrator',
    summary = CASE WHEN summary = '' THEN 'รองผู้ประสานงานของ P Choke สำหรับ orchestrate agent และส่งต่องาน' ELSE summary END,
    capabilities_json = CASE WHEN capabilities_json = '[]' THEN '["orchestration","handoff","review"]' ELSE capabilities_json END
WHERE id = 'mika' AND role = 'Orchestrator';

UPDATE agents
SET role = 'Coding Operator'
WHERE id = 'sora' AND role = 'Coding';

UPDATE agents
SET role = 'Research Operator'
WHERE id = 'nua' AND role = 'Research';

UPDATE agents
SET role = 'Image Operator'
WHERE id = 'aura' AND role = 'Image';

UPDATE agents
SET role = 'Assistant Operator'
WHERE id = 'nari' AND role = 'Assistant';

UPDATE work_modes
SET description = 'MIKA ทำหน้าที่ deputy และเปิด 9Router สำหรับงานทั่วไป'
WHERE id = 'daily' AND description = 'MIKA พร้อม 9Router สำหรับงานทั่วไป';
`

const schemaV5 = `
DROP TABLE IF EXISTS project_agents;
DROP TABLE IF EXISTS projects;
DROP TABLE IF EXISTS project_roots;
DROP TABLE IF EXISTS work_modes;
DROP TABLE IF EXISTS agents;
DROP TABLE IF EXISTS managed_processes;
`

var migrations = []string{schemaV1, schemaV2, schemaV3, schemaV4, schemaV5}

func openDatabase(ctx context.Context, path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open HOPE database: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping HOPE database: %w", err)
	}
	if _, err := db.ExecContext(ctx, pragmas); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure HOPE database: %w", err)
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
		return fmt.Errorf("read HOPE schema version: %w", err)
	}
	if version > len(migrations) {
		return fmt.Errorf("HOPE schema version %d is newer than supported version %d", version, len(migrations))
	}
	for index := version; index < len(migrations); index++ {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin HOPE migration %d: %w", index+1, err)
		}
		if _, err := tx.ExecContext(ctx, migrations[index]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply HOPE migration %d: %w", index+1, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", index+1)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record HOPE migration %d: %w", index+1, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit HOPE migration %d: %w", index+1, err)
		}
	}
	return nil
}
