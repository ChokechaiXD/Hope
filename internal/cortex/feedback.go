package cortex

import (
	"context"
	"fmt"
	"time"
)

func (hub *Hub) Feedback(ctx context.Context, cmd FeedbackCommand) (Memory, error) {
	if err := validateFeedback(cmd); err != nil {
		return Memory{}, err
	}
	tx, err := hub.db.BeginTx(ctx, nil)
	if err != nil {
		return Memory{}, fmt.Errorf("begin feedback: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	requestKey := scopedRequestKey(cmd.AgentID, cmd.IdempotencyKey)
	if memoryID, found, err := requestResource(ctx, tx, requestKey, "feedback"); err != nil {
		return Memory{}, fmt.Errorf("check feedback request: %w", err)
	} else if found {
		memory, err := getMemory(ctx, tx, memoryID)
		if err != nil {
			return Memory{}, err
		}
		if !hub.canFeedback(memory, cmd.AgentID) {
			return Memory{}, ErrForbidden
		}
		return memory, nil
	}
	memory, err := getMemory(ctx, tx, cmd.MemoryID)
	if err != nil {
		return Memory{}, err
	}
	if !hub.canFeedback(memory, cmd.AgentID) {
		return Memory{}, ErrForbidden
	}
	truth, utility, eventType := applyFeedback(memory.TruthScore, memory.UtilityScore, cmd.Outcome)
	metadataJSON, err := encodeJSON(map[string]any{"revision": memory.Revision})
	if err != nil {
		return Memory{}, fmt.Errorf("encode feedback metadata: %w", err)
	}
	eventID, err := newID("evt")
	if err != nil {
		return Memory{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `
UPDATE memories SET truth_score = ?, utility_score = ?, updated_at = ? WHERE id = ?`,
		truth, utility, now, cmd.MemoryID); err != nil {
		return Memory{}, fmt.Errorf("update scores: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO memory_events(
    id, memory_id, event_type, actor_id, session_id, reason, metadata_json, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, eventID, cmd.MemoryID, eventType,
		cmd.AgentID, cmd.SessionID, cmd.Reason, metadataJSON, now); err != nil {
		return Memory{}, fmt.Errorf("record feedback event: %w", err)
	}
	if err := recordRequest(ctx, tx, requestKey, "feedback", cmd.MemoryID, now); err != nil {
		return Memory{}, fmt.Errorf("record feedback request: %w", err)
	}
	memory, err = getMemory(ctx, tx, cmd.MemoryID)
	if err != nil {
		return Memory{}, err
	}
	if err := tx.Commit(); err != nil {
		return Memory{}, fmt.Errorf("commit feedback: %w", err)
	}
	return memory, nil
}

func (hub *Hub) canFeedback(memory Memory, agentID string) bool {
	if hub.isAdmin(agentID) || memory.CreatedBy == agentID {
		return true
	}
	if memory.Scope == ScopePrivate {
		return false
	}
	return memory.Lifecycle == LifecycleActive || memory.Lifecycle == LifecycleCanonical
}
