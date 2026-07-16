package cortex

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (hub *Hub) AdvisorStatus(ctx context.Context, actorID string) (AdvisorStatus, error) {
	if !hub.isAdmin(actorID) {
		return AdvisorStatus{}, ErrForbidden
	}
	settings, err := hub.advisorSettings(ctx)
	if err != nil {
		return AdvisorStatus{}, err
	}
	lastRun, err := hub.lastAdvisorRun(ctx)
	if err != nil {
		return AdvisorStatus{}, err
	}
	return AdvisorStatus{Settings: settings, LastRun: lastRun}, nil
}

func (hub *Hub) UpdateAdvisorSettings(
	ctx context.Context,
	command UpdateAdvisorSettingsCommand,
) (AdvisorSettings, error) {
	if !hub.isAdmin(command.ActorID) {
		return AdvisorSettings{}, ErrForbidden
	}
	if err := validateAdvisorSettings(command); err != nil {
		return AdvisorSettings{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	enabled := 0
	if command.Enabled {
		enabled = 1
	}
	if _, err := hub.db.ExecContext(ctx, `
UPDATE advisor_settings
SET enabled = ?, endpoint = ?, model = ?, input_token_budget = ?,
    output_token_budget = ?, effort = ?, updated_by = ?, updated_at = ?
WHERE id = 1`, enabled, strings.TrimSpace(command.Endpoint), strings.TrimSpace(command.Model),
		command.InputTokenBudget, command.OutputTokenBudget, command.Effort,
		command.ActorID, now); err != nil {
		return AdvisorSettings{}, fmt.Errorf("update advisor settings: %w", err)
	}
	return hub.advisorSettings(ctx)
}

func (hub *Hub) RecordAdvisorRun(ctx context.Context, record AdvisorRunRecord) (AdvisorRun, error) {
	if !hub.isAdmin(record.ActorID) {
		return AdvisorRun{}, ErrForbidden
	}
	if err := validateAdvisorRun(record); err != nil {
		return AdvisorRun{}, err
	}
	runID, err := newID("adv")
	if err != nil {
		return AdvisorRun{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	responseJSON := strings.TrimSpace(record.ResponseJSON)
	if responseJSON == "" {
		responseJSON = "{}"
	}
	_, err = hub.db.ExecContext(ctx, `
INSERT INTO advisor_runs(
    id, model, input_tokens, output_tokens, status, summary,
    response_json, error_text, actor_id, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, runID, strings.TrimSpace(record.Model),
		record.InputTokens, record.OutputTokens, record.Status, strings.TrimSpace(record.Summary),
		responseJSON, strings.TrimSpace(record.Error), record.ActorID, now)
	if err != nil {
		return AdvisorRun{}, fmt.Errorf("record advisor run: %w", err)
	}
	return hub.advisorRun(ctx, runID)
}

func (hub *Hub) advisorSettings(ctx context.Context) (AdvisorSettings, error) {
	var settings AdvisorSettings
	var enabled int
	var effort, updatedAt string
	err := hub.db.QueryRowContext(ctx, `
SELECT enabled, endpoint, model, input_token_budget, output_token_budget,
       effort, updated_by, updated_at
FROM advisor_settings WHERE id = 1`).Scan(&enabled, &settings.Endpoint, &settings.Model,
		&settings.InputTokenBudget, &settings.OutputTokenBudget, &effort,
		&settings.UpdatedBy, &updatedAt)
	if err != nil {
		return AdvisorSettings{}, fmt.Errorf("read advisor settings: %w", err)
	}
	settings.Enabled = enabled == 1
	settings.Effort = AdvisorEffort(effort)
	settings.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return AdvisorSettings{}, fmt.Errorf("decode advisor settings time: %w", err)
	}
	return settings, nil
}

func (hub *Hub) lastAdvisorRun(ctx context.Context) (*AdvisorRun, error) {
	var runID string
	err := hub.db.QueryRowContext(ctx, `
SELECT id FROM advisor_runs ORDER BY created_at DESC, rowid DESC LIMIT 1`).Scan(&runID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read last advisor run: %w", err)
	}
	run, err := hub.advisorRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func (hub *Hub) advisorRun(ctx context.Context, runID string) (AdvisorRun, error) {
	var run AdvisorRun
	var createdAt string
	err := hub.db.QueryRowContext(ctx, `
SELECT id, model, input_tokens, output_tokens, status, summary,
       response_json, error_text, actor_id, created_at
FROM advisor_runs WHERE id = ?`, runID).Scan(&run.ID, &run.Model, &run.InputTokens,
		&run.OutputTokens, &run.Status, &run.Summary, &run.ResponseJSON, &run.Error,
		&run.ActorID, &createdAt)
	if err != nil {
		return AdvisorRun{}, fmt.Errorf("read advisor run: %w", err)
	}
	run.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return AdvisorRun{}, fmt.Errorf("decode advisor run time: %w", err)
	}
	return run, nil
}

func validateAdvisorSettings(command UpdateAdvisorSettingsCommand) error {
	if strings.TrimSpace(command.ActorID) == "" || len(command.Endpoint) > 512 || len(command.Model) > 256 {
		return fmt.Errorf("%w: invalid advisor identity or connection value", ErrInvalidInput)
	}
	if command.Enabled && (strings.TrimSpace(command.Endpoint) == "" || strings.TrimSpace(command.Model) == "") {
		return fmt.Errorf("%w: enabled advisor requires endpoint and model", ErrInvalidInput)
	}
	if command.InputTokenBudget < 300 || command.InputTokenBudget > 4000 {
		return fmt.Errorf("%w: input_token_budget must be between 300 and 4000", ErrInvalidInput)
	}
	if command.OutputTokenBudget < 100 || command.OutputTokenBudget > 1000 {
		return fmt.Errorf("%w: output_token_budget must be between 100 and 1000", ErrInvalidInput)
	}
	switch command.Effort {
	case AdvisorEffortAuto, AdvisorEffortLow, AdvisorEffortMedium, AdvisorEffortHigh:
		return nil
	default:
		return fmt.Errorf("%w: unsupported advisor effort", ErrInvalidInput)
	}
}

func validateAdvisorRun(record AdvisorRunRecord) error {
	if strings.TrimSpace(record.ActorID) == "" || len(record.Model) > 256 ||
		record.InputTokens < 0 || record.OutputTokens < 0 ||
		len(record.Summary) > 4000 || len(record.ResponseJSON) > 65536 || len(record.Error) > 500 {
		return fmt.Errorf("%w: invalid advisor run", ErrInvalidInput)
	}
	if record.Status != "success" && record.Status != "error" {
		return fmt.Errorf("%w: unsupported advisor run status", ErrInvalidInput)
	}
	if raw := strings.TrimSpace(record.ResponseJSON); raw != "" && !json.Valid([]byte(raw)) {
		return fmt.Errorf("%w: advisor response must be valid JSON", ErrInvalidInput)
	}
	return nil
}
