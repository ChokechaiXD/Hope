package cortex

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const selectMemorySQL = `
SELECT m.id, m.kind, m.scope, m.scope_key, m.memory_key, m.lifecycle,
       r.title, r.content, r.tags_json,
       m.truth_score, m.utility_score, m.created_by,
       r.session_id, r.source_ref, m.current_revision,
       m.created_at, m.updated_at, m.embedding
FROM memories m
JOIN memory_revisions r
  ON r.memory_id = m.id AND r.revision = m.current_revision
WHERE m.id = ?`

type rowScanner interface {
	Scan(dest ...any) error
}

type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func scanMemory(row rowScanner) (Memory, error) {
	var memory Memory
	var kind, scope, lifecycle, tagsJSON, createdAt, updatedAt string
	var embeddingBlob []byte
	err := row.Scan(
		&memory.ID, &kind, &scope, &memory.ScopeKey, &memory.MemoryKey, &lifecycle,
		&memory.Title, &memory.Content, &tagsJSON,
		&memory.TruthScore, &memory.UtilityScore, &memory.CreatedBy,
		&memory.SessionID, &memory.SourceRef, &memory.Revision,
		&createdAt, &updatedAt, &embeddingBlob,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Memory{}, ErrNotFound
	}
	if err != nil {
		return Memory{}, err
	}
	memory.Kind = MemoryKind(kind)
	memory.Scope = Scope(scope)
	memory.Lifecycle = Lifecycle(lifecycle)
	if err := json.Unmarshal([]byte(tagsJSON), &memory.Tags); err != nil {
		return Memory{}, fmt.Errorf("decode tags: %w", err)
	}
	if vec := decodeVector(embeddingBlob); vec != nil {
		memory.Embedding = vec
	}
	memory.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return Memory{}, fmt.Errorf("decode created_at: %w", err)
	}
	memory.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return Memory{}, fmt.Errorf("decode updated_at: %w", err)
	}
	return memory, nil
}

func getMemory(ctx context.Context, queryer queryRower, memoryID string) (Memory, error) {
	return scanMemory(queryer.QueryRowContext(ctx, selectMemorySQL, memoryID))
}

func requestResource(ctx context.Context, tx *sql.Tx, key, operation string) (string, bool, error) {
	var storedOperation, resourceID string
	err := tx.QueryRowContext(ctx,
		"SELECT operation, resource_id FROM requests WHERE idempotency_key = ?", key,
	).Scan(&storedOperation, &resourceID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if storedOperation != operation {
		return "", false, fmt.Errorf("%w: idempotency key already used for %s", ErrConflict, storedOperation)
	}
	return resourceID, true, nil
}

func recordRequest(ctx context.Context, tx *sql.Tx, key, operation, resourceID, timestamp string) error {
	_, err := tx.ExecContext(ctx,
		"INSERT INTO requests(idempotency_key, operation, resource_id, created_at) VALUES (?, ?, ?, ?)",
		key, operation, resourceID, timestamp,
	)
	return err
}

func encodeJSON(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
