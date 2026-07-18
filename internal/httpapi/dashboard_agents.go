package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"cortex.local/cortex/internal/controlcenter"
)

type agentSettingsControl interface {
	AgentSettings(context.Context) ([]controlcenter.AgentSettings, error)
	UpdateAgentSettings(context.Context, controlcenter.AgentSettings) (controlcenter.AgentSettingsResult, error)
}

type dashboardAgentSettings struct {
	controlcenter.AgentSettings
	Name string
}

func presentAgentSettings(settings []controlcenter.AgentSettings) []dashboardAgentSettings {
	presented := make([]dashboardAgentSettings, 0, len(settings))
	for _, item := range settings {
		presented = append(presented, dashboardAgentSettings{
			AgentSettings: item,
			Name:          strings.ToUpper(item.AgentID),
		})
	}
	return presented
}

func (server *Server) hermesSettings(writer http.ResponseWriter, request *http.Request) {
	_, session, ok := server.sessions.fromRequest(request)
	if !ok {
		http.Redirect(writer, request, "/", http.StatusSeeOther)
		return
	}
	if !server.hub.CanGovern(session.AgentID) {
		http.Error(writer, "Hermes settings are not permitted", http.StatusForbidden)
		return
	}
	control, ok := server.control.(agentSettingsControl)
	if !ok {
		http.Error(writer, "Hermes settings are unavailable", http.StatusServiceUnavailable)
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, 8192)
	if err := request.ParseForm(); err != nil {
		http.Error(writer, "invalid Hermes settings", http.StatusBadRequest)
		return
	}
	if !validCSRF(session.CSRFToken, request.FormValue("csrf")) {
		http.Error(writer, "invalid csrf token", http.StatusForbidden)
		return
	}
	everyTurns, everyErr := formInteger(request, "auto_capture_every_turns")
	maxChars, maxErr := formInteger(request, "auto_capture_max_chars")
	prefetchBudget, prefetchErr := formInteger(request, "prefetch_token_budget")
	recallBudget, recallErr := formInteger(request, "recall_token_budget")
	if everyErr != nil || maxErr != nil || prefetchErr != nil || recallErr != nil {
		http.Error(writer, "invalid Hermes settings", http.StatusBadRequest)
		return
	}
	_, err := control.UpdateAgentSettings(request.Context(), controlcenter.AgentSettings{
		AgentID:               strings.TrimSpace(request.FormValue("agent_id")),
		DefaultProject:        strings.TrimSpace(request.FormValue("default_project")),
		DefaultDomain:         strings.TrimSpace(request.FormValue("default_domain")),
		AutoCaptureEnabled:    request.FormValue("auto_capture_enabled") == "yes",
		AutoCaptureEveryTurns: everyTurns, AutoCaptureMaxChars: maxChars,
		PrefetchTokenBudget: prefetchBudget, RecallTokenBudget: recallBudget,
	})
	if errors.Is(err, controlcenter.ErrActionPending) {
		http.Error(writer, "another HOPE operation is already running", http.StatusConflict)
		return
	}
	if errors.Is(err, controlcenter.ErrInvalidAgentSettings) {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(writer, "save Hermes settings: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(writer, request, "/knowledge?agents=saved#agent-settings", http.StatusSeeOther)
}
