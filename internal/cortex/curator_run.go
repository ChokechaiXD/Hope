package cortex

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func (hub *Hub) Curate(ctx context.Context, command CurateCommand) (CuratorReport, error) {
	if !hub.isAdmin(command.ActorID) {
		return CuratorReport{}, ErrForbidden
	}
	settings, err := hub.curatorSettings(ctx)
	if err != nil {
		return CuratorReport{}, err
	}
	runID, err := newID("cur")
	if err != nil {
		return CuratorReport{}, err
	}
	trigger := strings.TrimSpace(command.Trigger)
	if trigger == "" {
		trigger = "manual"
	}
	report, err := hub.PreviewCuration(ctx, command.ActorID)
	if err != nil {
		hub.recordCuratorRun(ctx, runID, settings.Mode, trigger, command.ActorID, report, err)
		return report, err
	}
	if command.ApplySafe {
		for _, suggestion := range report.Suggestions {
			if !suggestion.SafeToApply {
				continue
			}
			_, reviewErr := hub.Review(ctx, ReviewCommand{
				IdempotencyKey: "curator/" + runID + "/" + suggestion.Memory.ID,
				MemoryID:       suggestion.Memory.ID,
				ActorID:        command.ActorID,
				Decision:       ReviewApprove,
				Reason:         "curator: independent agreement with evidence",
			})
			if reviewErr != nil {
				hub.recordCuratorRun(ctx, runID, settings.Mode, trigger, command.ActorID, report, reviewErr)
				return report, reviewErr
			}
			report.Applied++
		}
	}
	if err := hub.recordCuratorRun(ctx, runID, settings.Mode, trigger, command.ActorID, report, nil); err != nil {
		return report, err
	}
	return report, nil
}

func (hub *Hub) maybeCurate(ctx context.Context) {
	settings, err := hub.curatorSettings(ctx)
	if err != nil || settings.Mode == CuratorManual {
		return
	}
	lastRun, err := hub.lastCuratorRun(ctx)
	if err != nil {
		return
	}
	since := ""
	if lastRun != nil {
		since = lastRun.CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	var pending int
	err = hub.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM memory_events
WHERE event_type IN ('created', 'imported', 'observed', 'revised')
  AND created_at > ?`, since).Scan(&pending)
	if err != nil || pending < settings.RunEveryCandidates {
		return
	}
	actorID := hub.adminAgents[0]
	_, _ = hub.Curate(ctx, CurateCommand{
		ActorID: actorID, Trigger: "candidate_threshold",
		ApplySafe: settings.Mode == CuratorAutomatic,
	})
}

func (hub *Hub) recordCuratorRun(
	ctx context.Context,
	runID string,
	mode CuratorMode,
	trigger string,
	actorID string,
	report CuratorReport,
	runErr error,
) error {
	errorText := ""
	if runErr != nil {
		errorText = runErr.Error()
		if len(errorText) > 500 {
			errorText = errorText[:500]
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := hub.db.ExecContext(ctx, `
INSERT INTO curator_runs(
    id, mode, trigger_type, analyzed_count, ready_count, attention_count,
    applied_count, actor_id, error_text, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, runID, mode, trigger, report.Analyzed,
		report.Ready, report.Attention, report.Applied, actorID, errorText, now)
	if err != nil {
		return fmt.Errorf("record curator run: %w", err)
	}
	return nil
}
