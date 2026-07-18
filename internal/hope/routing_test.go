package hope

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSkillRouteFeedbackIsTrackedOnce(t *testing.T) {
	t.Parallel()
	hub, err := Open(filepath.Join(t.TempDir(), "hope.db"), "")
	if err != nil {
		t.Fatalf("open HOPE: %v", err)
	}
	t.Cleanup(func() { _ = hub.Close() })
	ctx := context.Background()
	if err := hub.SaveSkill(ctx, Skill{
		ID: "safe-deploy", Name: "Safe deploy", Description: "Deploy with rollback checks",
		Keywords: []string{"deploy", "rollback"}, Role: "sora", Project: "cortex", Enabled: true,
	}); err != nil {
		t.Fatalf("save skill: %v", err)
	}
	matches, err := hub.RouteSkills(ctx, RouteRequest{Query: "deploy cortex with rollback", AgentID: "sora", ProjectID: "cortex", Limit: 3})
	if err != nil || len(matches) != 1 || matches[0].Skill.ID != "safe-deploy" {
		t.Fatalf("route skills = %#v, err=%v", matches, err)
	}
	pack := ContextPack{
		IdempotencyKey: "prefetch-1", AgentID: "sora", SessionID: "session-1",
		Query: "deploy cortex with rollback", ProjectID: "cortex", Router: "deterministic", Skills: matches,
	}
	packID, err := hub.SaveSkillRoute(ctx, pack)
	if err != nil || packID == "" {
		t.Fatalf("save route id=%q err=%v", packID, err)
	}
	replayedID, err := hub.SaveSkillRoute(ctx, pack)
	if err != nil || replayedID != packID {
		t.Fatalf("replay route id=%q err=%v, want %q", replayedID, err, packID)
	}
	pack.Query = "another task"
	if _, err := hub.SaveSkillRoute(ctx, pack); err == nil {
		t.Fatal("reusing a context-pack key for another query succeeded")
	}
	feedback := SkillFeedback{
		IdempotencyKey: "feedback-1", PackID: packID, SkillID: "safe-deploy", AgentID: "sora", Outcome: "success",
	}
	if err := hub.ApplySkillFeedback(ctx, feedback); err != nil {
		t.Fatalf("apply feedback: %v", err)
	}
	if err := hub.ApplySkillFeedback(ctx, feedback); err != nil {
		t.Fatalf("replay feedback: %v", err)
	}
	feedback.Outcome = "failure"
	if err := hub.ApplySkillFeedback(ctx, feedback); err == nil {
		t.Fatal("reusing a feedback key for another outcome succeeded")
	}
	feedback.IdempotencyKey, feedback.AgentID, feedback.Outcome = "feedback-2", "nua", "used"
	if err := hub.ApplySkillFeedback(ctx, feedback); err == nil {
		t.Fatal("feedback from another agent succeeded")
	}
	skill, err := hub.Skill(ctx, "safe-deploy")
	if err != nil {
		t.Fatalf("reload skill: %v", err)
	}
	if skill.UseCount != 1 || skill.SuccessCount != 1 || skill.FailureCount != 0 {
		t.Fatalf("skill counts = use:%d success:%d failure:%d", skill.UseCount, skill.SuccessCount, skill.FailureCount)
	}
	var version int
	if err := hub.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version != 5 {
		t.Fatalf("schema version=%d err=%v, want 5", version, err)
	}
}
