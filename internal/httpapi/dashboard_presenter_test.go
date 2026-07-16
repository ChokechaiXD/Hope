package httpapi

import (
	"strings"
	"testing"

	"cortex.local/cortex/internal/cortex"
)

func TestPresentMemoryUsesHumanLabelsAndConservativeGuidance(t *testing.T) {
	t.Parallel()

	presented := presentMemory(cortex.Memory{
		Kind: cortex.KindDecision, Scope: cortex.ScopeProject, ScopeKey: "novelclaw",
		Lifecycle: cortex.LifecycleCandidate, TruthScore: 0.7, UtilityScore: 0.5,
		SourceRef: "commit:abc123",
	})

	if presented.LifecycleLabel != "รอตรวจ" || presented.KindLabel != "ข้อตัดสินใจ" ||
		presented.AppliesTo != "โปรเจกต์ · novelclaw" {
		t.Fatalf("human labels = %#v", presented)
	}
	if presented.TrustLabel != "ค่อนข้างน่าเชื่อถือ" || presented.UtilityLabel != "ยังมีข้อมูลไม่พอ" {
		t.Fatalf("score labels truth=%q utility=%q", presented.TrustLabel, presented.UtilityLabel)
	}
	if !strings.Contains(presented.EvidenceLabel, "commit:abc123") ||
		!strings.Contains(presented.Guidance, "ควรตรวจเนื้อหา") {
		t.Fatalf("evidence=%q guidance=%q", presented.EvidenceLabel, presented.Guidance)
	}
	if !presented.CanApprove || !presented.CanReject || presented.CanPromote {
		t.Fatalf("candidate actions = %#v", presented)
	}
}

func TestReviewGuidanceKeepsGoldenRulesRevisable(t *testing.T) {
	t.Parallel()

	guidance := reviewGuidance(cortex.Memory{Lifecycle: cortex.LifecycleCanonical})
	if !strings.Contains(guidance, "ยังแทนที่ได้") {
		t.Fatalf("canonical guidance = %q", guidance)
	}
	if got := eventLabel(cortex.EventPromoted); got != "เลื่อนเป็นกฎหลัก" {
		t.Fatalf("event label = %q", got)
	}
}
