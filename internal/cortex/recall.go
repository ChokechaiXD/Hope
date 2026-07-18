package cortex

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var searchToken = regexp.MustCompile(`[\p{L}\p{N}_]+`)

func (hub *Hub) Recall(ctx context.Context, query RecallQuery) (RecallResult, error) {
	if err := validateRecall(query); err != nil {
		return RecallResult{}, err
	}
	limit := query.Limit
	if limit == 0 {
		limit = 8
	}
	ftsQuery := buildFTSQuery(query.Text)
	if ftsQuery == "" {
		return RecallResult{Items: []RecallItem{}}, nil
	}
	queryVec := embedText(query.Text)
	semanticEnabled := len(queryVec) > 0
	tx, err := hub.db.BeginTx(ctx, nil)
	if err != nil {
		return RecallResult{}, fmt.Errorf("begin recall: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if query.IdempotencyKey != "" {
		requestKey := scopedRequestKey(query.AgentID, query.IdempotencyKey)
		if recallID, found, err := requestResource(ctx, tx, requestKey, "recall"); err != nil {
			return RecallResult{}, fmt.Errorf("check recall request: %w", err)
		} else if found {
			stored, err := loadRecall(ctx, tx, recallID)
			if err != nil {
				return RecallResult{}, err
			}
			visible := stored.Items[:0]
			for _, item := range stored.Items {
				if hub.canRecall(item.Memory, query) {
					visible = append(visible, item)
				}
			}
			stored, err = budgetRecallResult(stored.ID, visible, query.TokenBudget)
			if err != nil {
				return RecallResult{}, err
			}
			return stored, nil
		}
	}

	recallID, err := newID("rec")
	if err != nil {
		return RecallResult{}, err
	}
	rows, err := tx.QueryContext(ctx, `
SELECT m.id, bm25(memory_fts) AS rank
FROM memory_fts
JOIN memories m ON m.id = memory_fts.memory_id
WHERE memory_fts MATCH ?
  AND (m.lifecycle IN ('active', 'canonical') OR (? AND m.lifecycle = 'candidate'))
  AND (
    m.scope = 'global'
    OR (m.scope = 'project' AND ? != '' AND m.scope_key = ?)
    OR (m.scope = 'domain' AND ? != '' AND m.scope_key = ?)
    OR (m.scope = 'private' AND (m.created_by = ? OR ?))
  )
ORDER BY rank
LIMIT 100`, ftsQuery, query.IncludeCandidates,
		query.Project, query.Project, query.Domain, query.Domain,
		query.AgentID, hub.isAdmin(query.AgentID))
	if err != nil {
		return RecallResult{}, fmt.Errorf("search memories: %w", err)
	}
	type rankedID struct {
		id   string
		rank float64
	}
	var ranked []rankedID
	for rows.Next() {
		var item rankedID
		if err := rows.Scan(&item.id, &item.rank); err != nil {
			_ = rows.Close()
			return RecallResult{}, fmt.Errorf("scan search result: %w", err)
		}
		ranked = append(ranked, item)
	}
	if err := rows.Close(); err != nil {
		return RecallResult{}, fmt.Errorf("close search results: %w", err)
	}

	// Semantic pass: when embeddings exist, also consider memories the FTS5
	// query missed entirely (paraphrase / synonym). Cheap at this corpus size;
	// only memories that already pass the scope/lifecycle filter are scored.
	if semanticEnabled {
		semRows, err := tx.QueryContext(ctx, `
SELECT m.id, m.embedding
FROM memories m
WHERE length(m.embedding) = ?
  AND (m.lifecycle IN ('active', 'canonical') OR (? AND m.lifecycle = 'candidate'))
  AND (
    m.scope = 'global'
    OR (m.scope = 'project' AND ? != '' AND m.scope_key = ?)
    OR (m.scope = 'domain' AND ? != '' AND m.scope_key = ?)
    OR (m.scope = 'private' AND (m.created_by = ? OR ?))
  )`, len(queryVec)*8, query.IncludeCandidates,
			query.Project, query.Project, query.Domain, query.Domain,
			query.AgentID, hub.isAdmin(query.AgentID))
		if err != nil {
			return RecallResult{}, fmt.Errorf("semantic scan: %w", err)
		}
		seen := make(map[string]bool, len(ranked))
		for _, r := range ranked {
			seen[r.id] = true
		}
		for semRows.Next() {
			var id string
			var blob []byte
			if err := semRows.Scan(&id, &blob); err != nil {
				_ = semRows.Close()
				return RecallResult{}, fmt.Errorf("scan semantic: %w", err)
			}
			if seen[id] {
				continue
			}
			vec := decodeVector(blob)
			if len(vec) != len(queryVec) {
				continue
			}
			sim := cosineSimilarity(queryVec, vec)
			if sim < 0.30 {
				continue
			}
			ranked = append(ranked, rankedID{id: id, rank: -sim}) // negative rank = semantic signal (higher sim -> "better")
			seen[id] = true
		}
		if err := semRows.Close(); err != nil {
			return RecallResult{}, fmt.Errorf("close semantic: %w", err)
		}
	}

	result := RecallResult{
		ID: recallID, Items: make([]RecallItem, 0, limit), TokenBudget: query.TokenBudget,
	}
	remoteDim := len(queryVec) >= RemoteEmbedDim
	for _, candidate := range ranked {
		memory, err := getMemory(ctx, tx, candidate.id)
		if err != nil {
			return RecallResult{}, err
		}
		if !hub.canRecall(memory, query) {
			continue
		}
		var score float64
		if candidate.rank < 0 {
			// Pure semantic hit (FTS5 missed it). Score tracks cosine
			// similarity directly so ordering reflects meaning, not a
			// capped blend that collapses everything to 1.0.
			sim := -candidate.rank
			score = 0.80*sim + 0.20*memory.TruthScore
		} else {
			textScore := 1 / (1 + max(0, -candidate.rank))
			if textScore > 1 {
				textScore = 1
			}
			score = 0.65*textScore + 0.20*memory.TruthScore + 0.15*memory.UtilityScore
			if semanticEnabled && len(memory.Embedding) == len(queryVec) {
				semantic := cosineSimilarity(queryVec, memory.Embedding)
				weight := 0.30
				if !remoteDim {
					weight = 0.15
				}
				score = (1-weight)*score + weight*semantic
			}
		}
		added, err := appendRecallItem(&result, RecallItem{Memory: memory, Score: score})
		if err != nil {
			return RecallResult{}, err
		}
		if !added {
			continue
		}
		if len(result.Items) == limit {
			break
		}
	}
	if err := finalizeRecallEstimate(&result); err != nil {
		return RecallResult{}, err
	}
	if err := hub.persistRecall(ctx, tx, result, query); err != nil {
		return RecallResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return RecallResult{}, fmt.Errorf("commit recall: %w", err)
	}
	return result, nil
}

func budgetRecallResult(id string, items []RecallItem, tokenBudget int) (RecallResult, error) {
	result := RecallResult{ID: id, Items: make([]RecallItem, 0, len(items)), TokenBudget: tokenBudget}
	for _, item := range items {
		if _, err := appendRecallItem(&result, item); err != nil {
			return RecallResult{}, err
		}
	}
	if err := finalizeRecallEstimate(&result); err != nil {
		return RecallResult{}, err
	}
	return result, nil
}

func appendRecallItem(result *RecallResult, item RecallItem) (bool, error) {
	if result.TokenBudget == 0 {
		result.Items = append(result.Items, item)
		return true, nil
	}
	candidate := *result
	candidate.Items = append(result.Items[:len(result.Items):len(result.Items)], item)
	tokens, err := estimateRecallResultTokens(candidate)
	if err != nil {
		return false, err
	}
	if tokens > result.TokenBudget {
		result.Truncated = true
		return false, nil
	}
	candidate.EstimatedTokens = tokens
	*result = candidate
	return true, nil
}

func finalizeRecallEstimate(result *RecallResult) error {
	if result.TokenBudget == 0 {
		return nil
	}
	tokens, err := estimateRecallResultTokens(*result)
	if err != nil {
		return err
	}
	result.EstimatedTokens = tokens
	return nil
}

func estimateRecallResultTokens(result RecallResult) (int, error) {
	estimate := 0
	for range 4 {
		result.EstimatedTokens = estimate
		raw, err := json.Marshal(result)
		if err != nil {
			return 0, fmt.Errorf("estimate recall context: %w", err)
		}
		next := max((utf8.RuneCount(raw)+1)/2, (len(raw)+3)/4)
		if next == estimate {
			return next, nil
		}
		estimate = next
	}
	return estimate, nil
}

func (hub *Hub) persistRecall(ctx context.Context, tx *sql.Tx, result RecallResult, query RecallQuery) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO recalls(id, query_text, agent_id, session_id, created_at)
VALUES (?, ?, ?, ?, ?)`, result.ID, query.Text, query.AgentID, query.SessionID, now); err != nil {
		return fmt.Errorf("record recall: %w", err)
	}
	for index, item := range result.Items {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO recall_items(recall_id, memory_id, rank, score)
VALUES (?, ?, ?, ?)`, result.ID, item.Memory.ID, index+1, item.Score); err != nil {
			return fmt.Errorf("record recall item: %w", err)
		}
	}
	if err := hub.recordRecallEvents(ctx, tx, result, query, now); err != nil {
		return err
	}
	if query.IdempotencyKey != "" {
		requestKey := scopedRequestKey(query.AgentID, query.IdempotencyKey)
		if err := recordRequest(ctx, tx, requestKey, "recall", result.ID, now); err != nil {
			return fmt.Errorf("record recall request: %w", err)
		}
	}
	return nil
}

func loadRecall(ctx context.Context, tx *sql.Tx, recallID string) (RecallResult, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT memory_id, score
FROM recall_items
WHERE recall_id = ?
ORDER BY rank`, recallID)
	if err != nil {
		return RecallResult{}, fmt.Errorf("load recall: %w", err)
	}
	type storedItem struct {
		memoryID string
		score    float64
	}
	var stored []storedItem
	for rows.Next() {
		var item storedItem
		if err := rows.Scan(&item.memoryID, &item.score); err != nil {
			_ = rows.Close()
			return RecallResult{}, fmt.Errorf("scan recall item: %w", err)
		}
		stored = append(stored, item)
	}
	if err := rows.Close(); err != nil {
		return RecallResult{}, fmt.Errorf("close recall items: %w", err)
	}
	result := RecallResult{ID: recallID, Items: make([]RecallItem, 0, len(stored))}
	for _, item := range stored {
		memory, err := getMemory(ctx, tx, item.memoryID)
		if err != nil {
			return RecallResult{}, err
		}
		result.Items = append(result.Items, RecallItem{Memory: memory, Score: item.score})
	}
	return result, nil
}

func (hub *Hub) canRecall(memory Memory, query RecallQuery) bool {
	if memory.Lifecycle != LifecycleActive && memory.Lifecycle != LifecycleCanonical &&
		!(query.IncludeCandidates && memory.Lifecycle == LifecycleCandidate) {
		return false
	}
	if memory.Scope == ScopePrivate && memory.CreatedBy != query.AgentID && !hub.isAdmin(query.AgentID) {
		return false
	}
	switch memory.Scope {
	case ScopeProject:
		return query.Project != "" && memory.ScopeKey == query.Project
	case ScopeDomain:
		return query.Domain != "" && memory.ScopeKey == query.Domain
	default:
		return true
	}
}

func (hub *Hub) recordRecallEvents(ctx context.Context, tx *sql.Tx, result RecallResult, query RecallQuery, now string) error {
	for index, item := range result.Items {
		eventID, err := newID("evt")
		if err != nil {
			return err
		}
		metadata, err := encodeJSON(map[string]any{
			"recall_id": result.ID,
			"rank":      index + 1,
			"score":     item.Score,
		})
		if err != nil {
			return fmt.Errorf("encode recall metadata: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO memory_events(id, memory_id, event_type, actor_id, session_id, metadata_json, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`, eventID, item.Memory.ID, EventRecalled,
			query.AgentID, query.SessionID, metadata, now); err != nil {
			return fmt.Errorf("record recall event: %w", err)
		}
	}
	return nil
}

func buildFTSQuery(text string) string {
	tokens := searchToken.FindAllString(strings.ToLower(text), -1)
	quoted := make([]string, 0, len(tokens))
	for _, token := range tokens {
		quoted = append(quoted, `"`+token+`"`)
	}
	return strings.Join(quoted, " OR ")
}
