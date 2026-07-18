package httpapi

import (
	"context"
	"net/http"
	"time"

	"cortex.local/cortex/internal/cortex"
	"cortex.local/cortex/internal/hope"
	"cortex.local/cortex/internal/intelligence"
)

type contextPackRequest struct {
	Text              string `json:"text"`
	Project           string `json:"project,omitempty"`
	Domain            string `json:"domain,omitempty"`
	SessionID         string `json:"session_id,omitempty"`
	MemoryLimit       int    `json:"memory_limit,omitempty"`
	MemoryTokenBudget int    `json:"memory_token_budget,omitempty"`
	SkillLimit        int    `json:"skill_limit,omitempty"`
}

type contextPackSkill struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Score       float64 `json:"score"`
	Reason      string  `json:"reason"`
}

type contextPackResponse struct {
	ID      string              `json:"id"`
	Memory  cortex.RecallResult `json:"memory"`
	Skills  []contextPackSkill  `json:"skills"`
	Routing skillRouteEvidence  `json:"routing"`
}

type skillRouteEvidence struct {
	Strategy     string `json:"strategy"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
}

type contextSkillFeedbackRequest struct {
	Outcome string `json:"outcome"`
}

type skillMemory interface {
	RouteSkills(context.Context, hope.RouteRequest) ([]hope.SkillMatch, error)
	SaveSkillRoute(context.Context, hope.ContextPack) (string, error)
	ContextSkillFeedback(context.Context, hope.SkillFeedback) error
}

func (server *Server) contextPack(writer http.ResponseWriter, request *http.Request) {
	key, ok := idempotencyKey(writer, request)
	if !ok {
		return
	}
	var input contextPackRequest
	if err := decodeJSON(writer, request, &input); err != nil {
		writeAPIError(writer, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	agentID := identityFromRequest(request)
	memory, err := server.hub.Recall(request.Context(), cortex.RecallQuery{
		IdempotencyKey: key + "/memory", AgentID: agentID, SessionID: input.SessionID,
		Text: input.Text, Project: input.Project, Domain: input.Domain,
		Limit: input.MemoryLimit, TokenBudget: input.MemoryTokenBudget,
	})
	if err != nil {
		writeDomainError(writer, err)
		return
	}
	response := contextPackResponse{Memory: memory, Routing: skillRouteEvidence{Strategy: "deterministic"}}
	skillMem := server.activeSkillMem()
	if skillMem != nil {
		routeLimit := input.SkillLimit
		if routeLimit < 1 || routeLimit > 5 {
			routeLimit = 3
		}
		matches, routeErr := skillMem.RouteSkills(request.Context(), hope.RouteRequest{
			Query: input.Text, AgentID: agentID, ProjectID: input.Project, Limit: 6,
		})
		if routeErr == nil {
			matches, response.Routing = server.rankAmbiguousSkills(request.Context(), agentID, input.Text, matches)
			if len(matches) > routeLimit {
				matches = matches[:routeLimit]
			}
			for _, match := range matches {
				response.Skills = append(response.Skills, contextPackSkill{
					ID: match.Skill.ID, Name: match.Skill.Name, Description: match.Skill.Description,
					Score: match.Score, Reason: match.Reason,
				})
			}
			response.ID, err = skillMem.SaveSkillRoute(request.Context(), hope.ContextPack{
				IdempotencyKey: key,
				AgentID:        agentID,
				SessionID:      input.SessionID,
				Query:          input.Text,
				ProjectID:      input.Project,
				Router:         response.Routing.Strategy,
				InputTokens:    response.Routing.InputTokens,
				OutputTokens:   response.Routing.OutputTokens,
				Skills:         matches,
			})
			if err != nil {
				writeAPIError(writer, http.StatusInternalServerError, "context_pack_tracking_failed", "could not track skill recommendations")
				return
			}
		}
	}
	writeJSON(writer, http.StatusOK, response)
}

func (server *Server) contextSkillFeedback(writer http.ResponseWriter, request *http.Request) {
	skillMem := server.activeSkillMem()
	if skillMem == nil {
		writeAPIError(writer, http.StatusServiceUnavailable, "hope_unavailable", "HOPE is unavailable")
		return
	}
	key, ok := idempotencyKey(writer, request)
	if !ok {
		return
	}
	var input contextSkillFeedbackRequest
	if err := decodeJSON(writer, request, &input); err != nil {
		writeAPIError(writer, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	feedback := hope.SkillFeedback{
		IdempotencyKey: key,
		PackID:         request.PathValue("packID"),
		SkillID:        request.PathValue("skillID"),
		AgentID:        identityFromRequest(request),
		Outcome:        input.Outcome,
	}
	if err := skillMem.ContextSkillFeedback(request.Context(), feedback); err != nil {
		writeAPIError(writer, http.StatusBadRequest, "invalid_skill_feedback", err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (server *Server) activeSkillMem() skillMemory {
	return server.skillMem
}

func (server *Server) rankAmbiguousSkills(ctx context.Context, agentID, task string, matches []hope.SkillMatch) ([]hope.SkillMatch, skillRouteEvidence) {
	evidence := skillRouteEvidence{Strategy: "deterministic"}
	if len(matches) < 2 || matches[0].Score-matches[1].Score >= 0.75 {
		return matches, evidence
	}
	router, ok := server.advisor.(intelligence.SkillRouter)
	if !ok {
		return matches, evidence
	}
	status, err := server.hub.AdvisorStatus(ctx, agentID)
	if err != nil || !status.Settings.Enabled || status.Settings.Model == "" {
		return matches, evidence
	}
	request := intelligence.SkillRouteRequest{
		Endpoint: status.Settings.Endpoint, Model: status.Settings.Model, Task: task,
		InputTokenBudget:  min(max(status.Settings.InputTokenBudget, 300), 1600),
		OutputTokenBudget: min(max(status.Settings.OutputTokenBudget, 100), 300),
	}
	byID := make(map[string]hope.SkillMatch, len(matches))
	for _, match := range matches {
		byID[match.Skill.ID] = match
		request.Candidates = append(request.Candidates, intelligence.SkillCandidate{ID: match.Skill.ID, Name: match.Skill.Name, Description: match.Skill.Description, RuleScore: match.Score})
	}
	routeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	route, err := router.RankSkills(routeCtx, request)
	if err != nil || len(route.Selected) == 0 {
		return matches, evidence
	}
	evidence = skillRouteEvidence{Strategy: "ai-tiebreaker", InputTokens: route.InputTokens, OutputTokens: route.OutputTokens}
	selected := make([]hope.SkillMatch, 0, len(matches))
	used := map[string]bool{}
	for _, item := range route.Selected {
		match := byID[item.ID]
		match.Reason += " · AI tie-breaker: " + item.Reason
		selected = append(selected, match)
		used[item.ID] = true
	}
	for _, match := range matches {
		if !used[match.Skill.ID] {
			selected = append(selected, match)
		}
	}
	return selected, evidence
}
