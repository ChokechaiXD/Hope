package cortex

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestAdvisorSettingsAndRunsRemainGovernorOwned(t *testing.T) {
	t.Parallel()
	hub, err := Open(Config{
		DatabasePath: filepath.Join(t.TempDir(), "cortex.db"),
		AdminAgents:  []string{"mika"},
	})
	if err != nil {
		t.Fatalf("open Cortex: %v", err)
	}
	defer hub.Close()

	status, err := hub.AdvisorStatus(context.Background(), "mika")
	if err != nil {
		t.Fatalf("read default advisor status: %v", err)
	}
	if status.Settings.Enabled || status.Settings.Endpoint != "http://127.0.0.1:20128/v1" || status.LastRun != nil {
		t.Fatalf("default advisor status = %#v", status)
	}
	if _, err := hub.AdvisorStatus(context.Background(), "sola"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-governor advisor status error = %v, want forbidden", err)
	}

	settings, err := hub.UpdateAdvisorSettings(context.Background(), UpdateAdvisorSettingsCommand{
		ActorID: "mika", Enabled: true, Endpoint: "http://127.0.0.1:20128/v1",
		Model: "reviewer", InputTokenBudget: 1200, OutputTokenBudget: 350,
		Effort: AdvisorEffortAuto,
	})
	if err != nil {
		t.Fatalf("update advisor settings: %v", err)
	}
	if !settings.Enabled || settings.Model != "reviewer" || settings.UpdatedBy != "mika" {
		t.Fatalf("advisor settings = %#v", settings)
	}

	run, err := hub.RecordAdvisorRun(context.Background(), AdvisorRunRecord{
		ActorID: "mika", Model: "reviewer", InputTokens: 400, OutputTokens: 80,
		Status: "success", Summary: "Review this evidence.",
		ResponseJSON: `{"summary":"Review this evidence.","items":[]}`,
	})
	if err != nil {
		t.Fatalf("record advisor run: %v", err)
	}
	status, err = hub.AdvisorStatus(context.Background(), "mika")
	if err != nil {
		t.Fatalf("read advisor status: %v", err)
	}
	if status.LastRun == nil || status.LastRun.ID != run.ID || status.LastRun.InputTokens != 400 {
		t.Fatalf("last advisor run = %#v", status.LastRun)
	}
	if _, err := hub.RecordAdvisorRun(context.Background(), AdvisorRunRecord{
		ActorID: "mika", Model: "reviewer", Status: "success", ResponseJSON: "not-json",
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid advisor response error = %v, want invalid input", err)
	}
}
