package cortex

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
)

func TestRepeatedObservationPersistsAndBecomesCuratorReady(t *testing.T) {
	t.Parallel()
	hub, err := Open(Config{
		DatabasePath: filepath.Join(t.TempDir(), "cortex.db"),
		AdminAgents:  []string{"mika"},
	})
	if err != nil {
		t.Fatalf("open Cortex: %v", err)
	}
	defer hub.Close()

	first := rememberCuratorCandidate(t, hub, "sola", "sola/1")
	second := rememberCuratorCandidate(t, hub, "nua", "nua/1")
	if first.ID != second.ID {
		t.Fatalf("repeated observation created a duplicate: %s != %s", first.ID, second.ID)
	}
	events, err := hub.History(context.Background(), HistoryQuery{MemoryID: first.ID, AgentID: "mika"})
	if err != nil {
		t.Fatalf("read observation history: %v", err)
	}
	if got, want := eventTypes(events), []EventType{EventCreated, EventObserved}; !equalEventTypes(got, want) {
		t.Fatalf("observation events = %v, want %v", got, want)
	}

	report, err := hub.PreviewCuration(context.Background(), "mika")
	if err != nil {
		t.Fatalf("preview curation: %v", err)
	}
	if report.Ready != 1 || len(report.Suggestions) != 1 || !report.Suggestions[0].SafeToApply {
		t.Fatalf("curator report = %#v, want one safe suggestion", report)
	}
	if got := report.Suggestions[0].Supporters; len(got) != 2 || got[0] != "nua" || got[1] != "sola" {
		t.Fatalf("supporters = %#v, want nua and sola", got)
	}
}

func TestCuratorIgnoresFeedbackFromSupersededRevision(t *testing.T) {
	t.Parallel()
	hub, err := Open(Config{
		DatabasePath: filepath.Join(t.TempDir(), "cortex.db"),
		AdminAgents:  []string{"mika"},
	})
	if err != nil {
		t.Fatalf("open Cortex: %v", err)
	}
	defer hub.Close()

	first := rememberCuratorCandidate(t, hub, "sola", "sola/revision-1")
	if _, err := hub.Feedback(context.Background(), FeedbackCommand{
		IdempotencyKey: "mika/contradict-revision-1", MemoryID: first.ID,
		AgentID: "mika", Outcome: FeedbackContradicted,
	}); err != nil {
		t.Fatalf("contradict revision 1: %v", err)
	}
	for agent, key := range map[string]string{"sola": "sola/revision-2", "nua": "nua/revision-2"} {
		if _, err := hub.Remember(context.Background(), RememberCommand{
			IdempotencyKey: key, Kind: KindSolution, Scope: ScopeProject,
			ScopeKey: "novelclaw", MemoryKey: "translation.output",
			Title:   "Write canonical output safely",
			Content: "Validate the canonical output before replacing the prior file.",
			AgentID: agent, SourceRef: "quality_gate.go",
		}); err != nil {
			t.Fatalf("remember revision 2 as %s: %v", agent, err)
		}
	}

	report, err := hub.PreviewCuration(context.Background(), "mika")
	if err != nil {
		t.Fatalf("preview curation: %v", err)
	}
	if report.Ready != 1 || report.Attention != 0 || report.Suggestions[0].Contradictions != 0 {
		t.Fatalf("revision 2 inherited stale feedback: %#v", report)
	}
}

func TestCuratorFlagsCanonicalContradictionEvenWhenTruthScoreRemainsHigh(t *testing.T) {
	t.Parallel()
	hub, err := Open(Config{
		DatabasePath: filepath.Join(t.TempDir(), "cortex.db"),
		AdminAgents:  []string{"mika"},
	})
	if err != nil {
		t.Fatalf("open Cortex: %v", err)
	}
	defer hub.Close()

	memory := rememberCuratorCandidate(t, hub, "sola", "sola/canonical")
	for index := range 5 {
		if _, err := hub.Feedback(context.Background(), FeedbackCommand{
			IdempotencyKey: fmt.Sprintf("mika/confirm/%d", index), MemoryID: memory.ID,
			AgentID: "mika", Outcome: FeedbackConfirmed,
		}); err != nil {
			t.Fatalf("confirm candidate: %v", err)
		}
	}
	if _, err := hub.Review(context.Background(), ReviewCommand{
		IdempotencyKey: "mika/approve", MemoryID: memory.ID, ActorID: "mika",
		Decision: ReviewApprove,
	}); err != nil {
		t.Fatalf("approve memory: %v", err)
	}
	if _, err := hub.Review(context.Background(), ReviewCommand{
		IdempotencyKey: "mika/promote", MemoryID: memory.ID, ActorID: "mika",
		Decision: ReviewPromote,
	}); err != nil {
		t.Fatalf("promote memory: %v", err)
	}
	contradicted, err := hub.Feedback(context.Background(), FeedbackCommand{
		IdempotencyKey: "nua/contradict", MemoryID: memory.ID,
		AgentID: "nua", Outcome: FeedbackContradicted,
	})
	if err != nil {
		t.Fatalf("contradict canonical memory: %v", err)
	}
	if contradicted.TruthScore <= 0.30 {
		t.Fatalf("test requires a high truth score, got %.2f", contradicted.TruthScore)
	}

	report, err := hub.PreviewCuration(context.Background(), "mika")
	if err != nil {
		t.Fatalf("preview curation: %v", err)
	}
	if report.Attention != 1 || len(report.Suggestions) != 1 ||
		report.Suggestions[0].Reason != CuratorContradicted {
		t.Fatalf("canonical contradiction was not flagged: %#v", report)
	}
}

func TestCuratorAppliesOnlySafeCandidatesAndNeverCreatesCanonicalRules(t *testing.T) {
	t.Parallel()
	hub, err := Open(Config{
		DatabasePath: filepath.Join(t.TempDir(), "cortex.db"),
		AdminAgents:  []string{"mika"},
	})
	if err != nil {
		t.Fatalf("open Cortex: %v", err)
	}
	defer hub.Close()

	safe := rememberCuratorCandidate(t, hub, "sola", "sola/1")
	_ = rememberCuratorCandidate(t, hub, "nua", "nua/1")
	protected, err := hub.Remember(context.Background(), RememberCommand{
		IdempotencyKey: "mika/global", Kind: KindPreference, Scope: ScopeGlobal,
		MemoryKey: "user.answer_style", Title: "Answer style", Content: "Prefer concise Thai.",
		AgentID: "mika", SourceRef: "USER.md",
	})
	if err != nil {
		t.Fatalf("remember protected candidate: %v", err)
	}

	report, err := hub.Curate(context.Background(), CurateCommand{
		ActorID: "mika", Trigger: "test", ApplySafe: true,
	})
	if err != nil {
		t.Fatalf("run curator: %v", err)
	}
	if report.Applied != 1 {
		t.Fatalf("applied = %d, want 1", report.Applied)
	}
	safeAfter, _, err := hub.Inspect(context.Background(), HistoryQuery{MemoryID: safe.ID, AgentID: "mika"})
	if err != nil {
		t.Fatalf("inspect safe memory: %v", err)
	}
	protectedAfter, _, err := hub.Inspect(context.Background(), HistoryQuery{MemoryID: protected.ID, AgentID: "mika"})
	if err != nil {
		t.Fatalf("inspect protected memory: %v", err)
	}
	if safeAfter.Lifecycle != LifecycleActive {
		t.Fatalf("safe lifecycle = %q, want active", safeAfter.Lifecycle)
	}
	if protectedAfter.Lifecycle != LifecycleCandidate {
		t.Fatalf("protected lifecycle = %q, want candidate", protectedAfter.Lifecycle)
	}
}

func TestAutomaticCuratorUsesCandidateThreshold(t *testing.T) {
	t.Parallel()
	hub, err := Open(Config{
		DatabasePath: filepath.Join(t.TempDir(), "cortex.db"),
		AdminAgents:  []string{"mika"},
	})
	if err != nil {
		t.Fatalf("open Cortex: %v", err)
	}
	defer hub.Close()
	_, err = hub.UpdateCuratorSettings(context.Background(), UpdateCuratorSettingsCommand{
		ActorID: "mika", Mode: CuratorAutomatic, RunEveryCandidates: 2,
		BatchLimit: 25, MinAgreement: 2,
	})
	if err != nil {
		t.Fatalf("enable automatic curator: %v", err)
	}

	memory := rememberCuratorCandidate(t, hub, "sola", "sola/1")
	_ = rememberCuratorCandidate(t, hub, "nua", "nua/1")
	after, _, err := hub.Inspect(context.Background(), HistoryQuery{MemoryID: memory.ID, AgentID: "mika"})
	if err != nil {
		t.Fatalf("inspect automatically reviewed memory: %v", err)
	}
	if after.Lifecycle != LifecycleActive {
		t.Fatalf("automatic lifecycle = %q, want active", after.Lifecycle)
	}
	status, err := hub.CuratorStatus(context.Background(), "mika")
	if err != nil {
		t.Fatalf("read curator status: %v", err)
	}
	if status.LastRun == nil || status.LastRun.Applied != 1 || status.LastRun.Trigger != "candidate_threshold" {
		t.Fatalf("last curator run = %#v", status.LastRun)
	}
}

func rememberCuratorCandidate(t *testing.T, hub *Hub, agentID, requestID string) Memory {
	t.Helper()
	memory, err := hub.Remember(context.Background(), RememberCommand{
		IdempotencyKey: requestID, Kind: KindSolution, Scope: ScopeProject,
		ScopeKey: "novelclaw", MemoryKey: "translation.output",
		Title: "Canonical translation output", Content: "Write chapters to canonical .th.json files.",
		AgentID: agentID, SourceRef: "commit:abc123",
	})
	if err != nil {
		t.Fatalf("remember curator candidate for %s: %v", agentID, err)
	}
	return memory
}
