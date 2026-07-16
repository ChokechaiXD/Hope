package cortex

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (hub *Hub) CuratorStatus(ctx context.Context, actorID string) (CuratorStatus, error) {
	if !hub.isAdmin(actorID) {
		return CuratorStatus{}, ErrForbidden
	}
	settings, err := hub.curatorSettings(ctx)
	if err != nil {
		return CuratorStatus{}, err
	}
	lastRun, err := hub.lastCuratorRun(ctx)
	if err != nil {
		return CuratorStatus{}, err
	}
	return CuratorStatus{Settings: settings, LastRun: lastRun}, nil
}

func (hub *Hub) UpdateCuratorSettings(
	ctx context.Context,
	command UpdateCuratorSettingsCommand,
) (CuratorSettings, error) {
	if !hub.isAdmin(command.ActorID) {
		return CuratorSettings{}, ErrForbidden
	}
	if err := validateCuratorSettings(command); err != nil {
		return CuratorSettings{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := hub.db.ExecContext(ctx, `
UPDATE curator_settings
SET mode = ?, run_every_candidates = ?, batch_limit = ?, min_agreement = ?,
    updated_by = ?, updated_at = ?
WHERE id = 1`, command.Mode, command.RunEveryCandidates, command.BatchLimit,
		command.MinAgreement, command.ActorID, now); err != nil {
		return CuratorSettings{}, fmt.Errorf("update curator settings: %w", err)
	}
	return hub.curatorSettings(ctx)
}

func (hub *Hub) curatorSettings(ctx context.Context) (CuratorSettings, error) {
	var settings CuratorSettings
	var mode, updatedAt string
	err := hub.db.QueryRowContext(ctx, `
SELECT mode, run_every_candidates, batch_limit, min_agreement, updated_by, updated_at
FROM curator_settings WHERE id = 1`).Scan(
		&mode, &settings.RunEveryCandidates, &settings.BatchLimit, &settings.MinAgreement,
		&settings.UpdatedBy, &updatedAt,
	)
	if err != nil {
		return CuratorSettings{}, fmt.Errorf("read curator settings: %w", err)
	}
	settings.Mode = CuratorMode(mode)
	settings.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return CuratorSettings{}, fmt.Errorf("decode curator settings time: %w", err)
	}
	return settings, nil
}

func (hub *Hub) lastCuratorRun(ctx context.Context) (*CuratorRun, error) {
	var run CuratorRun
	var mode, createdAt string
	err := hub.db.QueryRowContext(ctx, `
SELECT id, mode, trigger_type, analyzed_count, ready_count, attention_count,
       applied_count, actor_id, error_text, created_at
FROM curator_runs
ORDER BY created_at DESC, rowid DESC
LIMIT 1`).Scan(&run.ID, &mode, &run.Trigger, &run.Analyzed, &run.Ready,
		&run.Attention, &run.Applied, &run.ActorID, &run.Error, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read last curator run: %w", err)
	}
	run.Mode = CuratorMode(mode)
	run.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, fmt.Errorf("decode curator run time: %w", err)
	}
	return &run, nil
}

func validateCuratorSettings(command UpdateCuratorSettingsCommand) error {
	if strings.TrimSpace(command.ActorID) == "" || !validCuratorMode(command.Mode) {
		return fmt.Errorf("%w: actor_id and a supported curator mode are required", ErrInvalidInput)
	}
	if command.RunEveryCandidates < 1 || command.RunEveryCandidates > 1000 {
		return fmt.Errorf("%w: run_every_candidates must be between 1 and 1000", ErrInvalidInput)
	}
	if command.BatchLimit < 10 || command.BatchLimit > 200 {
		return fmt.Errorf("%w: batch_limit must be between 10 and 200", ErrInvalidInput)
	}
	if command.MinAgreement < 2 || command.MinAgreement > 5 {
		return fmt.Errorf("%w: min_agreement must be between 2 and 5", ErrInvalidInput)
	}
	return nil
}

func validCuratorMode(mode CuratorMode) bool {
	switch mode {
	case CuratorManual, CuratorAssisted, CuratorAutomatic:
		return true
	default:
		return false
	}
}
