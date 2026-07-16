package httpapi

import (
	"testing"

	"cortex.local/cortex/internal/cortex"
)

func TestCandidateInboxGroupsImportedBacklogByReviewIntent(t *testing.T) {
	t.Parallel()

	memories := []cortex.Memory{
		{ID: "progress", Title: `active_task ที่อยู่: "NovelClaw"`, Tags: []string{"holographic"}},
		{ID: "snapshot", Title: "# USER PROFILE (Quick Cache)", Tags: []string{"holographic"}},
		{ID: "project", Title: "NovelClaw quality loop", Tags: []string{"holographic"}},
		{ID: "agent", Title: "MIKA orchestration pipeline", Tags: []string{"holographic"}},
		{ID: "other", Title: "Unclassified imported fact", Tags: []string{"holographic"}},
	}

	groups := candidateInboxGroups(memories)
	if len(groups) != 5 {
		t.Fatalf("groups = %#v", groups)
	}
	for _, group := range groups {
		if group.Count != 1 || !group.Imported {
			t.Fatalf("group = %#v", group)
		}
	}
	filtered := filterCandidateInbox(memories, "progress")
	if len(filtered) != 1 || filtered[0].ID != "progress" {
		t.Fatalf("progress filter = %#v", filtered)
	}
}
