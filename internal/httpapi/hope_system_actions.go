package httpapi

import (
	"context"
	"fmt"
	"net/http"

	"cortex.local/cortex/internal/controlplane"
	"cortex.local/cortex/internal/hope"
	"cortex.local/cortex/internal/integrationhub"
)

func (server *Server) requireHOPEGovernor(writer http.ResponseWriter, request *http.Request) (dashboardSession, bool) {
	setDashboardHeaders(writer)
	_, session, ok := server.sessions.fromRequest(request)
	if !ok {
		http.Redirect(writer, request, "/", http.StatusSeeOther)
		return dashboardSession{}, false
	}
	if server.hope == nil || !server.hub.CanGovern(session.AgentID) {
		http.Error(writer, "HOPE control is not permitted", http.StatusForbidden)
		return dashboardSession{}, false
	}
	request.Body = http.MaxBytesReader(writer, request.Body, 64<<10)
	if err := request.ParseForm(); err != nil || !validCSRF(session.CSRFToken, request.FormValue("csrf")) {
		http.Error(writer, "invalid request", http.StatusForbidden)
		return dashboardSession{}, false
	}
	return session, true
}

func (server *Server) hopeWorkMode(writer http.ResponseWriter, request *http.Request) {
	if _, ok := server.requireHOPEGovernor(writer, request); !ok {
		return
	}
	action := request.FormValue("action")
	if err := controlplane.ValidateAction(action, "start", "stop"); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	result, err := server.hope.WorkMode(request.Context(), request.PathValue("modeID"), action)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = dashboardTemplates.ExecuteTemplate(writer, "hope_action.html", result)
}

func (server *Server) hopeSaveWorkMode(writer http.ResponseWriter, request *http.Request) {
	if _, ok := server.requireHOPEGovernor(writer, request); !ok {
		return
	}
	mode := hope.WorkMode{
		ID:           request.FormValue("id"),
		Name:         request.FormValue("name"),
		Description:  request.FormValue("description"),
		Integrations: request.Form["integrations"],
		Agents:       request.Form["agents"],
		OpenTelegram: request.FormValue("open_telegram") == "yes",
	}
	if err := server.hope.SaveWorkMode(request.Context(), mode); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(writer, request, "/?section=today&notice=mode-saved", http.StatusSeeOther)
}

func (server *Server) hopeIntegrationAction(writer http.ResponseWriter, request *http.Request) {
	if _, ok := server.requireHOPEGovernor(writer, request); !ok {
		return
	}
	action := request.FormValue("action")
	if err := controlplane.ValidateAction(action, "start", "stop", "open"); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	result := server.hope.IntegrationAction(request.Context(), integrationhub.ActionRequest{Integration: request.PathValue("integrationID"), Action: action})
	_ = dashboardTemplates.ExecuteTemplate(writer, "hope_action.html", struct {
		Mode     hope.WorkMode
		Steps    []integrationhub.ActionResult
		OpenURLs []string
	}{Steps: []integrationhub.ActionResult{result}, OpenURLs: nonEmpty(result.OpenURL)})
}

func (server *Server) hopeAgentAction(writer http.ResponseWriter, request *http.Request) {
	if _, ok := server.requireHOPEGovernor(writer, request); !ok {
		return
	}
	agent, err := server.hopeOverviewAgent(request.Context(), request.PathValue("agentID"))
	if err != nil {
		http.Error(writer, err.Error(), http.StatusNotFound)
		return
	}
	action := request.FormValue("action")
	if err := controlplane.ValidateAction(action, "start", "stop"); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	result := server.hope.IntegrationAction(request.Context(), integrationhub.ActionRequest{Integration: "hermes", Action: action, Target: agent.Profile})
	if result.Err != nil {
		http.Error(writer, result.Err.Error(), http.StatusConflict)
		return
	}
	http.Redirect(writer, request, "/?section=agents", http.StatusSeeOther)
}

func (server *Server) hopeOverviewAgent(ctx context.Context, id string) (hope.Agent, error) {
	agent, err := server.hope.Agent(ctx, id)
	if err != nil {
		return hope.Agent{}, fmt.Errorf("agent not found")
	}
	return agent, nil
}

func (server *Server) hopeSaveAgent(writer http.ResponseWriter, request *http.Request) {
	if _, ok := server.requireHOPEGovernor(writer, request); !ok {
		return
	}
	agent := hope.Agent{
		ID: request.FormValue("id"), Name: request.FormValue("name"), Role: request.FormValue("role"),
		Profile: request.FormValue("profile"), TelegramURL: request.FormValue("telegram_url"),
		AvatarPath: request.FormValue("avatar_path"), Summary: request.FormValue("summary"),
		Capabilities: controlplane.ParseKeywords(request.FormValue("capabilities")),
		PersonaPath:  request.FormValue("persona_path"), PersonaNote: request.FormValue("persona_note"),
		Enabled: true,
	}
	createProfile := request.FormValue("create_profile") == "yes"
	if err := server.hope.SaveAgent(request.Context(), agent, createProfile); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	if createProfile && server.control != nil {
		if _, err := server.control.SyncHermes(request.Context()); err != nil {
			http.Error(writer, "Agent was created, but HOPE connector sync failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}
	http.Redirect(writer, request, "/?section=agents&notice=agent-saved", http.StatusSeeOther)
}

type pinControl interface{ UpdateDashboardPIN(bool, string) error }

func (server *Server) hopeSecurity(writer http.ResponseWriter, request *http.Request) {
	if _, ok := server.requireHOPEGovernor(writer, request); !ok {
		return
	}
	control, ok := server.control.(pinControl)
	if !ok {
		http.Error(writer, "security settings unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := control.UpdateDashboardPIN(request.FormValue("pin_enabled") == "yes", request.FormValue("pin")); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(writer, request, "/?section=settings&notice=security-saved", http.StatusSeeOther)
}

func nonEmpty(value string) []string {
	if value == "" {
		return nil
	}
	return []string{value}
}
