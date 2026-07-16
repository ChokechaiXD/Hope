package cortex

import (
	"context"
	"fmt"
)

func (hub *Hub) Remember(ctx context.Context, cmd RememberCommand) (Memory, error) {
	if err := validateRemember(cmd); err != nil {
		return Memory{}, err
	}
	tx, err := hub.db.BeginTx(ctx, nil)
	if err != nil {
		return Memory{}, fmt.Errorf("begin remember: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	memory, _, err := insertCandidate(ctx, tx, candidateInput{
		idempotencyKey: scopedRequestKey(cmd.AgentID, cmd.IdempotencyKey),
		operation:      "remember",
		kind:           cmd.Kind,
		scope:          cmd.Scope,
		scopeKey:       cmd.ScopeKey,
		memoryKey:      cmd.MemoryKey,
		title:          cmd.Title,
		content:        cmd.Content,
		tags:           cmd.Tags,
		agentID:        cmd.AgentID,
		sessionID:      cmd.SessionID,
		sourceRef:      cmd.SourceRef,
		truthScore:     0.5,
		utilityScore:   0.5,
		eventType:      EventCreated,
	})
	if err != nil {
		return Memory{}, err
	}
	if err := tx.Commit(); err != nil {
		return Memory{}, fmt.Errorf("commit remember: %w", err)
	}
	hub.maybeCurate(ctx)
	return memory, nil
}
