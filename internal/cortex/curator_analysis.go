package cortex

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

type curatorEvidence struct {
	supporters     []string
	confirmations  int
	contradictions int
	hasSource      bool
	imported       bool
}

func (hub *Hub) PreviewCuration(ctx context.Context, actorID string) (CuratorReport, error) {
	if !hub.isAdmin(actorID) {
		return CuratorReport{}, ErrForbidden
	}
	settings, err := hub.curatorSettings(ctx)
	if err != nil {
		return CuratorReport{}, err
	}
	report := CuratorReport{Suggestions: make([]CuratorSuggestion, 0, settings.BatchLimit)}
	candidates, err := hub.Browse(ctx, BrowseQuery{
		AgentID: actorID, Lifecycle: LifecycleCandidate, Limit: settings.BatchLimit,
	})
	if err != nil {
		return CuratorReport{}, err
	}
	memories := slices.Clone(candidates.Memories)
	riskRows, err := hub.db.QueryContext(ctx, `
SELECT id FROM memories
WHERE lifecycle IN ('active', 'canonical')
  AND (
    truth_score <= 0.30
    OR utility_score <= 0.20
    OR EXISTS (
      SELECT 1 FROM memory_events
      WHERE memory_events.memory_id = memories.id
        AND memory_events.event_type = 'contradicted'
    )
  )
ORDER BY updated_at DESC, rowid DESC
LIMIT ?`, settings.BatchLimit)
	if err != nil {
		return CuratorReport{}, fmt.Errorf("query reviewed memories at risk: %w", err)
	}
	riskIDs := make([]string, 0, settings.BatchLimit)
	for riskRows.Next() {
		var memoryID string
		if err := riskRows.Scan(&memoryID); err != nil {
			_ = riskRows.Close()
			return CuratorReport{}, fmt.Errorf("scan reviewed memory at risk: %w", err)
		}
		riskIDs = append(riskIDs, memoryID)
	}
	if err := riskRows.Err(); err != nil {
		_ = riskRows.Close()
		return CuratorReport{}, fmt.Errorf("iterate reviewed memories at risk: %w", err)
	}
	if err := riskRows.Close(); err != nil {
		return CuratorReport{}, fmt.Errorf("close reviewed memories at risk: %w", err)
	}
	for _, memoryID := range riskIDs {
		memory, err := getMemory(ctx, hub.db, memoryID)
		if err != nil {
			return CuratorReport{}, err
		}
		memories = append(memories, memory)
	}
	for _, memory := range memories {
		evidence, err := hub.curatorEvidence(ctx, actorID, memory)
		if err != nil {
			return CuratorReport{}, err
		}
		suggestion, include := curateMemory(memory, evidence, settings)
		report.Analyzed++
		if include {
			report.Suggestions = append(report.Suggestions, suggestion)
			countCuratorCategory(&report, suggestion.Category)
		}
	}
	slices.SortStableFunc(report.Suggestions, func(left, right CuratorSuggestion) int {
		return curatorCategoryOrder(left.Category) - curatorCategoryOrder(right.Category)
	})
	return report, nil
}

func (hub *Hub) curatorEvidence(
	ctx context.Context,
	actorID string,
	memory Memory,
) (curatorEvidence, error) {
	events, err := hub.History(ctx, HistoryQuery{MemoryID: memory.ID, AgentID: actorID})
	if err != nil {
		return curatorEvidence{}, err
	}
	supporters := make(map[string]struct{})
	evidence := curatorEvidence{hasSource: strings.TrimSpace(memory.SourceRef) != ""}
	for _, event := range events {
		switch event.Type {
		case EventCreated, EventImported:
			if memory.Revision != 1 {
				continue
			}
			clear(supporters)
			evidence.confirmations = 0
			evidence.contradictions = 0
			evidence.imported = event.Type == EventImported
			evidence.hasSource = strings.TrimSpace(memory.SourceRef) != ""
			supporters[event.ActorID] = struct{}{}
		case EventRevised:
			if eventRevision(event) != memory.Revision {
				continue
			}
			clear(supporters)
			evidence.confirmations = 0
			evidence.contradictions = 0
			evidence.imported = false
			evidence.hasSource = strings.TrimSpace(memory.SourceRef) != ""
			supporters[event.ActorID] = struct{}{}
		case EventObserved:
			if eventRevision(event) == memory.Revision {
				supporters[event.ActorID] = struct{}{}
				if source, ok := event.Metadata["source_ref"].(string); ok && strings.TrimSpace(source) != "" {
					evidence.hasSource = true
				}
			}
		case EventConfirmed:
			if !eventMatchesRevision(event, memory.Revision) {
				continue
			}
			evidence.confirmations++
			supporters[event.ActorID] = struct{}{}
		case EventContradicted:
			if !eventMatchesRevision(event, memory.Revision) {
				continue
			}
			evidence.contradictions++
		}
	}
	evidence.supporters = make([]string, 0, len(supporters))
	for supporter := range supporters {
		evidence.supporters = append(evidence.supporters, supporter)
	}
	slices.Sort(evidence.supporters)
	return evidence, nil
}

func eventMatchesRevision(event Event, revision int) bool {
	eventRevision := eventRevision(event)
	// Feedback written before revision-aware events belongs only to revision 1.
	return eventRevision == revision || (eventRevision == 0 && revision == 1)
}

func curateMemory(
	memory Memory,
	evidence curatorEvidence,
	settings CuratorSettings,
) (CuratorSuggestion, bool) {
	suggestion := CuratorSuggestion{
		Memory: memory, Supporters: evidence.supporters,
		Confirmations: evidence.confirmations, Contradictions: evidence.contradictions,
		HasSource: evidence.hasSource,
	}
	if memory.Lifecycle == LifecycleCanonical || memory.Lifecycle == LifecycleActive {
		switch {
		case evidence.contradictions > 0:
			suggestion.Category = CuratorAttention
			suggestion.Reason = CuratorContradicted
			suggestion.Recommendation = ReviewSupersede
			return suggestion, true
		case memory.TruthScore <= 0.30:
			suggestion.Category = CuratorAttention
			suggestion.Reason = CuratorLowTruth
			suggestion.Recommendation = ReviewSupersede
			return suggestion, true
		case memory.UtilityScore <= 0.20:
			suggestion.Category = CuratorAttention
			suggestion.Reason = CuratorLowUtility
			suggestion.Recommendation = ReviewArchive
			return suggestion, true
		default:
			return CuratorSuggestion{}, false
		}
	}

	switch {
	case evidence.imported:
		suggestion.Category = CuratorProtected
		suggestion.Reason = CuratorImported
	case memory.Scope == ScopeGlobal || memory.Scope == ScopePrivate:
		suggestion.Category = CuratorProtected
		suggestion.Reason = CuratorProtectedScope
	case memory.Kind == KindPreference || memory.Kind == KindProjectState:
		suggestion.Category = CuratorProtected
		suggestion.Reason = CuratorProtectedKind
	case evidence.contradictions > 0:
		suggestion.Category = CuratorAttention
		suggestion.Reason = CuratorContradicted
	case memory.TruthScore < 0.50:
		suggestion.Category = CuratorAttention
		suggestion.Reason = CuratorLowTruth
	case !evidence.hasSource:
		suggestion.Category = CuratorWaiting
		suggestion.Reason = CuratorNeedsSource
	case len(evidence.supporters) < settings.MinAgreement:
		suggestion.Category = CuratorWaiting
		suggestion.Reason = CuratorNeedsAgreement
	default:
		suggestion.Category = CuratorReady
		suggestion.Reason = CuratorAgreement
		suggestion.Recommendation = ReviewApprove
		suggestion.SafeToApply = true
	}
	return suggestion, true
}

func eventRevision(event Event) int {
	value, ok := event.Metadata["revision"]
	if !ok {
		return 0
	}
	switch revision := value.(type) {
	case float64:
		return int(revision)
	case int:
		return revision
	default:
		return 0
	}
}

func countCuratorCategory(report *CuratorReport, category CuratorCategory) {
	switch category {
	case CuratorReady:
		report.Ready++
	case CuratorAttention:
		report.Attention++
	case CuratorWaiting:
		report.Waiting++
	case CuratorProtected:
		report.Protected++
	default:
		panic(fmt.Sprintf("unsupported curator category %q", category))
	}
}

func curatorCategoryOrder(category CuratorCategory) int {
	switch category {
	case CuratorAttention:
		return 0
	case CuratorReady:
		return 1
	case CuratorWaiting:
		return 2
	case CuratorProtected:
		return 3
	default:
		return 4
	}
}
