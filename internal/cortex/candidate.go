package cortex

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

type candidateInput struct {
	idempotencyKey string
	operation      string
	kind           MemoryKind
	scope          Scope
	scopeKey       string
	memoryKey      string
	title          string
	content        string
	tags           []string
	agentID        string
	sessionID      string
	sourceRef      string
	truthScore     float64
	utilityScore   float64
	eventType      EventType
	metadata       map[string]any
}

func insertCandidate(ctx context.Context, tx *sql.Tx, input candidateInput) (Memory, bool, error) {
	input.scopeKey = normalizedScopeKey(input.scope, input.scopeKey, input.agentID)
	if memoryID, found, err := requestResource(ctx, tx, input.idempotencyKey, input.operation); err != nil {
		return Memory{}, false, fmt.Errorf("check %s request: %w", input.operation, err)
	} else if found {
		memory, err := getMemory(ctx, tx, memoryID)
		return memory, false, err
	}
	existing, found, err := findStableMemory(ctx, tx, input)
	if err != nil {
		return Memory{}, false, err
	}
	if found {
		return reviseOrObserveCandidate(ctx, tx, existing, input)
	}
	memoryID, err := newID("mem")
	if err != nil {
		return Memory{}, false, err
	}
	eventID, err := newID("evt")
	if err != nil {
		return Memory{}, false, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tagsJSON, err := encodeJSON(input.tags)
	if err != nil {
		return Memory{}, false, fmt.Errorf("encode tags: %w", err)
	}
	metadataJSON, err := encodeJSON(input.metadata)
	if err != nil {
		return Memory{}, false, fmt.Errorf("encode import metadata: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO memories(
    id, kind, scope, scope_key, memory_key, lifecycle,
    truth_score, utility_score, created_by, current_revision, created_at, updated_at, embedding
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?)`,
		memoryID, input.kind, input.scope, strings.TrimSpace(input.scopeKey), strings.TrimSpace(input.memoryKey),
		LifecycleCandidate, clamp(input.truthScore), clamp(input.utilityScore), input.agentID, now, now,
		encodeVector(embedText(input.title+"\n"+input.content+"\n"+strings.Join(input.tags, " "))),
	)
	if err != nil {
		return Memory{}, false, fmt.Errorf("insert memory: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO memory_revisions(
    memory_id, revision, title, content, tags_json, session_id,
    source_ref, created_by, created_at
) VALUES (?, 1, ?, ?, ?, ?, ?, ?, ?)`,
		memoryID, strings.TrimSpace(input.title), strings.TrimSpace(input.content), tagsJSON,
		input.sessionID, input.sourceRef, input.agentID, now,
	)
	if err != nil {
		return Memory{}, false, fmt.Errorf("insert revision: %w", err)
	}
	_, err = tx.ExecContext(ctx,
		"INSERT INTO memory_fts(memory_id, title, content, tags) VALUES (?, ?, ?, ?)",
		memoryID, input.title, input.content, strings.Join(input.tags, " "),
	)
	if err != nil {
		return Memory{}, false, fmt.Errorf("index memory: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO memory_events(id, memory_id, event_type, actor_id, session_id, metadata_json, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`, eventID, memoryID, input.eventType, input.agentID,
		input.sessionID, metadataJSON, now)
	if err != nil {
		return Memory{}, false, fmt.Errorf("record %s event: %w", input.eventType, err)
	}
	if err := recordRequest(ctx, tx, input.idempotencyKey, input.operation, memoryID, now); err != nil {
		return Memory{}, false, fmt.Errorf("record %s request: %w", input.operation, err)
	}
	memory, err := getMemory(ctx, tx, memoryID)
	if err != nil {
		return Memory{}, false, fmt.Errorf("read candidate memory: %w", err)
	}
	return memory, true, nil
}

func normalizedScopeKey(scope Scope, scopeKey, agentID string) string {
	switch scope {
	case ScopeGlobal:
		return ""
	case ScopePrivate:
		return strings.TrimSpace(agentID)
	default:
		return strings.TrimSpace(scopeKey)
	}
}

func findStableMemory(ctx context.Context, tx *sql.Tx, input candidateInput) (Memory, bool, error) {
	scopeKey := strings.TrimSpace(input.scopeKey)
	memoryKey := strings.TrimSpace(input.memoryKey)
	query := `
SELECT id FROM memories
WHERE scope = ? AND scope_key = ? AND memory_key = ?
  AND (? != 'private' OR created_by = ?)
ORDER BY created_at, rowid
LIMIT 1`
	var memoryID string
	err := tx.QueryRowContext(ctx, query, input.scope, scopeKey, memoryKey, input.scope, input.agentID).Scan(&memoryID)
	if errors.Is(err, sql.ErrNoRows) {
		return Memory{}, false, nil
	}
	if err != nil {
		return Memory{}, false, fmt.Errorf("find stable memory: %w", err)
	}
	memory, err := getMemory(ctx, tx, memoryID)
	if err != nil {
		return Memory{}, false, err
	}
	return memory, true, nil
}

func reviseOrObserveCandidate(
	ctx context.Context,
	tx *sql.Tx,
	existing Memory,
	input candidateInput,
) (Memory, bool, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	title := strings.TrimSpace(input.title)
	content := strings.TrimSpace(input.content)
	unchanged := existing.Kind == input.kind && existing.Title == title && existing.Content == content && slices.Equal(existing.Tags, input.tags)
	if unchanged {
		metadata := copyMetadata(input.metadata)
		metadata["revision"] = existing.Revision
		if sourceRef := strings.TrimSpace(input.sourceRef); sourceRef != "" {
			metadata["source_ref"] = sourceRef
		}
		if err := appendCandidateEvent(ctx, tx, existing.ID, EventObserved, input, metadata, now); err != nil {
			return Memory{}, false, err
		}
		if err := recordRequest(ctx, tx, input.idempotencyKey, input.operation, existing.ID, now); err != nil {
			return Memory{}, false, fmt.Errorf("record %s request: %w", input.operation, err)
		}
		return existing, false, nil
	}

	tagsJSON, err := encodeJSON(input.tags)
	if err != nil {
		return Memory{}, false, fmt.Errorf("encode tags: %w", err)
	}
	revision := existing.Revision + 1
	_, err = tx.ExecContext(ctx, `
INSERT INTO memory_revisions(
    memory_id, revision, title, content, tags_json, session_id,
    source_ref, created_by, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		existing.ID, revision, title, content, tagsJSON,
		input.sessionID, input.sourceRef, input.agentID, now,
	)
	if err != nil {
		return Memory{}, false, fmt.Errorf("insert revised memory: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE memories
SET kind = ?, lifecycle = ?, truth_score = ?, utility_score = ?, current_revision = ?, updated_at = ?, embedding = ?
WHERE id = ?`, input.kind, LifecycleCandidate, clamp(input.truthScore), clamp(input.utilityScore), revision, now,
		encodeVector(embedText(title+"\n"+content+"\n"+strings.Join(input.tags, " "))), existing.ID); err != nil {
		return Memory{}, false, fmt.Errorf("activate revised memory: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM memory_fts WHERE memory_id = ?", existing.ID); err != nil {
		return Memory{}, false, fmt.Errorf("remove prior search index: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO memory_fts(memory_id, title, content, tags) VALUES (?, ?, ?, ?)",
		existing.ID, title, content, strings.Join(input.tags, " "),
	); err != nil {
		return Memory{}, false, fmt.Errorf("index revised memory: %w", err)
	}
	metadata := copyMetadata(input.metadata)
	metadata["previous_lifecycle"] = existing.Lifecycle
	metadata["previous_revision"] = existing.Revision
	metadata["revision"] = revision
	if err := appendCandidateEvent(ctx, tx, existing.ID, EventRevised, input, metadata, now); err != nil {
		return Memory{}, false, err
	}
	if err := recordRequest(ctx, tx, input.idempotencyKey, input.operation, existing.ID, now); err != nil {
		return Memory{}, false, fmt.Errorf("record %s request: %w", input.operation, err)
	}
	memory, err := getMemory(ctx, tx, existing.ID)
	if err != nil {
		return Memory{}, false, err
	}
	return memory, true, nil
}

func appendCandidateEvent(
	ctx context.Context,
	tx *sql.Tx,
	memoryID string,
	eventType EventType,
	input candidateInput,
	metadata map[string]any,
	now string,
) error {
	eventID, err := newID("evt")
	if err != nil {
		return err
	}
	metadataJSON, err := encodeJSON(metadata)
	if err != nil {
		return fmt.Errorf("encode %s metadata: %w", eventType, err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO memory_events(id, memory_id, event_type, actor_id, session_id, metadata_json, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`, eventID, memoryID, eventType, input.agentID, input.sessionID, metadataJSON, now); err != nil {
		return fmt.Errorf("record %s event: %w", eventType, err)
	}
	return nil
}

func copyMetadata(source map[string]any) map[string]any {
	result := make(map[string]any, len(source)+3)
	for key, value := range source {
		result[key] = value
	}
	return result
}
