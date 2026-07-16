package httpapi

import (
	"context"
	"embed"
	"html/template"
	"net/http"
	"strings"
	"time"

	"cortex.local/cortex/internal/controlcenter"
	"cortex.local/cortex/internal/cortex"
	"cortex.local/cortex/internal/intelligence"
)

//go:embed templates/*.html static/*.css
var dashboardAssets embed.FS

var dashboardTemplates = template.Must(template.ParseFS(dashboardAssets, "templates/*.html"))

type dashboardView struct {
	AgentID             string
	CSRFToken           string
	Advanced            bool
	Total               int
	Candidate           int
	Active              int
	Canonical           int
	Memories            []dashboardMemory
	Filters             dashboardFilters
	Matched             int
	System              *dashboardSystem
	SystemErr           string
	Curator             *dashboardCurator
	CuratorErr          string
	CuratorNotice       string
	Advisor             *dashboardAdvisor
	AdvisorErr          string
	AdvisorNotice       string
	AgentSettings       []dashboardAgentSettings
	AgentSettingsErr    string
	AgentSettingsNotice string
}

type dashboardFilters struct {
	Text      string
	Lifecycle string
	Kind      string
	Scope     string
	ScopeKey  string
	CreatedBy string
}

type dashboardSystem struct {
	Version string
	Listen  string
	PID     int
	DataDir string
	Uptime  string
	Pending controlcenter.Action
	Syncing bool
}

func (server *Server) dashboard(writer http.ResponseWriter, request *http.Request) {
	setDashboardHeaders(writer)
	_, session, ok := server.sessions.fromRequest(request)
	if !ok {
		writer.Header().Set("Cache-Control", "no-store")
		if err := dashboardTemplates.ExecuteTemplate(writer, "login.html", nil); err != nil {
			http.Error(writer, "render login", http.StatusInternalServerError)
		}
		return
	}
	filters := dashboardFilters{
		Text:      strings.TrimSpace(request.URL.Query().Get("q")),
		Lifecycle: strings.TrimSpace(request.URL.Query().Get("lifecycle")),
		Kind:      strings.TrimSpace(request.URL.Query().Get("kind")),
		Scope:     strings.TrimSpace(request.URL.Query().Get("scope")),
		ScopeKey:  strings.TrimSpace(request.URL.Query().Get("scope_key")),
		CreatedBy: strings.TrimSpace(request.URL.Query().Get("created_by")),
	}
	counts, err := server.hub.Counts(request.Context(), session.AgentID)
	if err != nil {
		writeDomainError(writer, err)
		return
	}
	browsed, err := server.hub.Browse(request.Context(), cortex.BrowseQuery{
		AgentID: session.AgentID, Text: filters.Text, Lifecycle: cortex.Lifecycle(filters.Lifecycle),
		Kind: cortex.MemoryKind(filters.Kind), Scope: cortex.Scope(filters.Scope), ScopeKey: filters.ScopeKey,
		CreatedBy: filters.CreatedBy, Limit: 200,
	})
	if err != nil {
		writeDomainError(writer, err)
		return
	}
	view := dashboardView{
		AgentID:   session.AgentID,
		CSRFToken: session.CSRFToken,
		Advanced:  request.URL.Query().Get("view") == "advanced",
		Candidate: counts[cortex.LifecycleCandidate],
		Active:    counts[cortex.LifecycleActive],
		Canonical: counts[cortex.LifecycleCanonical],
		Memories:  make([]dashboardMemory, 0, len(browsed.Memories)),
		Filters:   filters, Matched: browsed.Total,
	}
	for _, count := range counts {
		view.Total += count
	}
	for _, memory := range browsed.Memories {
		view.Memories = append(view.Memories, presentMemory(memory))
	}
	switch request.URL.Query().Get("curator") {
	case "settings":
		view.CuratorNotice = "บันทึกวิธีทำงานของผู้ช่วยจัดระเบียบแล้ว"
	case "ran":
		view.CuratorNotice = "วิเคราะห์คลังความรู้รอบล่าสุดแล้ว"
	}
	if request.URL.Query().Get("advisor") == "settings" {
		view.AdvisorNotice = "บันทึกการตั้งค่าผู้ช่วยโมเดลแล้ว"
	}
	if request.URL.Query().Get("agents") == "saved" {
		view.AgentSettingsNotice = "บันทึกการเรียนรู้ของเอเจนต์แล้ว เริ่มใช้ใน session ถัดไป"
	}
	curatorStatus, curatorErr := server.hub.CuratorStatus(request.Context(), session.AgentID)
	if curatorErr == nil {
		curatorReport, previewErr := server.hub.PreviewCuration(request.Context(), session.AgentID)
		if previewErr == nil {
			presented := presentCurator(curatorStatus, curatorReport)
			view.Curator = &presented
		} else {
			view.CuratorErr = previewErr.Error()
		}
	} else {
		view.CuratorErr = curatorErr.Error()
	}
	if server.advisor != nil {
		advisorStatus, advisorErr := server.hub.AdvisorStatus(request.Context(), session.AgentID)
		if advisorErr != nil {
			view.AdvisorErr = advisorErr.Error()
		} else {
			var models []intelligence.Model
			var modelsErr error
			modelsLoaded := request.URL.Query().Get("refresh_models") == "1"
			if modelsLoaded {
				modelsCtx, cancelModels := context.WithTimeout(request.Context(), 5*time.Second)
				models, modelsErr = server.advisor.Models(modelsCtx, advisorStatus.Settings.Endpoint)
				cancelModels()
			}
			presented := presentAdvisor(advisorStatus, models, modelsLoaded, modelsErr)
			view.Advisor = &presented
		}
	}
	if server.control != nil {
		status, statusErr := server.control.Status(request.Context())
		if statusErr != nil {
			view.SystemErr = statusErr.Error()
		} else {
			view.System = &dashboardSystem{
				Version: status.Version, Listen: status.Listen, PID: status.PID,
				DataDir: status.DataDir, Uptime: status.Uptime.Round(time.Second).String(),
				Pending: status.Pending, Syncing: status.Syncing,
			}
		}
		if settingsControl, ok := server.control.(agentSettingsControl); ok {
			settings, settingsErr := settingsControl.AgentSettings(request.Context())
			if settingsErr != nil {
				view.AgentSettingsErr = settingsErr.Error()
			} else {
				view.AgentSettings = presentAgentSettings(settings)
			}
		}
	}
	writer.Header().Set("Cache-Control", "no-store")
	if err := dashboardTemplates.ExecuteTemplate(writer, "dashboard.html", view); err != nil {
		http.Error(writer, "render dashboard", http.StatusInternalServerError)
	}
}
