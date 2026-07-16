package cortex

import (
	"context"
	"fmt"
)

func (hub *Hub) ImportCandidate(ctx context.Context, command ImportCommand) (Memory, bool, error) {
	if err := validateRemember(RememberCommand{
		IdempotencyKey: command.IdempotencyKey,
		Kind:           command.Kind,
		Scope:          command.Scope,
		ScopeKey:       command.ScopeKey,
		MemoryKey:      command.MemoryKey,
		Title:          command.Title,
		Content:        command.Content,
		AgentID:        command.AgentID,
	}); err != nil {
		return Memory{}, false, err
	}
	if command.SourceRef == "" {
		return Memory{}, false, fmt.Errorf("%w: source_ref is required for import", ErrInvalidInput)
	}
	tx, err := hub.db.BeginTx(ctx, nil)
	if err != nil {
		return Memory{}, false, fmt.Errorf("begin import: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	memory, created, err := insertCandidate(ctx, tx, candidateInput{
		idempotencyKey: scopedRequestKey(command.AgentID, command.IdempotencyKey),
		operation:      "import",
		kind:           command.Kind,
		scope:          command.Scope,
		scopeKey:       command.ScopeKey,
		memoryKey:      command.MemoryKey,
		title:          command.Title,
		content:        command.Content,
		tags:           command.Tags,
		agentID:        command.AgentID,
		sessionID:      command.SessionID,
		sourceRef:      command.SourceRef,
		truthScore:     command.TruthScore,
		utilityScore:   command.UtilityScore,
		eventType:      EventImported,
		metadata:       command.Metadata,
	})
	if err != nil {
		return Memory{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Memory{}, false, fmt.Errorf("commit import: %w", err)
	}
	hub.maybeCurate(ctx)
	return memory, created, nil
}
