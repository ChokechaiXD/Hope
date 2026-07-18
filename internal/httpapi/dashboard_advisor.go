package httpapi

import (
	"net/http"
	"strings"

	"cortex.local/cortex/internal/cortex"
	"cortex.local/cortex/internal/intelligence"
)

type dashboardAdvisor struct {
	Settings     cortex.AdvisorSettings
	Models       []intelligence.Model
	ModelsLoaded bool
	ModelsError  string
	EffortAuto   bool
	EffortLow    bool
	EffortMedium bool
	EffortHigh   bool
	LastRun      *dashboardAdvisorRun
}

type dashboardAdvisorRun struct {
	When         string
	Model        string
	InputTokens  int
	OutputTokens int
	Status       string
	Summary      string
	Error        string
}

type advisorResultView struct {
	Summary      string
	Model        string
	InputTokens  int
	OutputTokens int
	Items        []advisorResultItem
}

type advisorResultItem struct {
	MemoryID string
	Title    string
	Verdict  string
	Reason   string
}

func presentAdvisor(
	status cortex.AdvisorStatus,
	models []intelligence.Model,
	modelsLoaded bool,
	modelsErr error,
) dashboardAdvisor {
	view := dashboardAdvisor{
		Settings: status.Settings, Models: models, ModelsLoaded: modelsLoaded,
		EffortAuto:   status.Settings.Effort == cortex.AdvisorEffortAuto,
		EffortLow:    status.Settings.Effort == cortex.AdvisorEffortLow,
		EffortMedium: status.Settings.Effort == cortex.AdvisorEffortMedium,
		EffortHigh:   status.Settings.Effort == cortex.AdvisorEffortHigh,
	}
	if modelsErr != nil {
		view.ModelsError = modelsErr.Error()
	}
	if status.LastRun != nil {
		view.LastRun = &dashboardAdvisorRun{
			When: relativeTime(status.LastRun.CreatedAt), Model: status.LastRun.Model,
			InputTokens: status.LastRun.InputTokens, OutputTokens: status.LastRun.OutputTokens,
			Status: status.LastRun.Status, Summary: status.LastRun.Summary, Error: status.LastRun.Error,
		}
	}
	return view
}

func (server *Server) advisorSettings(writer http.ResponseWriter, request *http.Request) {
	session, ok := server.curatorDashboardRequest(writer, request)
	if !ok {
		return
	}
	if server.advisor == nil {
		http.Error(writer, "advisor is unavailable", http.StatusServiceUnavailable)
		return
	}
	inputBudget, err := formInteger(request, "input_token_budget")
	if err != nil {
		http.Error(writer, "invalid advisor input budget", http.StatusBadRequest)
		return
	}
	outputBudget, err := formInteger(request, "output_token_budget")
	if err != nil {
		http.Error(writer, "invalid advisor output budget", http.StatusBadRequest)
		return
	}
	enabled := request.FormValue("enabled") == "yes"
	endpoint := strings.TrimSpace(request.FormValue("endpoint"))
	if enabled || endpoint != "" {
		var err error
		endpoint, err = intelligence.ValidateEndpoint(endpoint)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
	}
	model := strings.TrimSpace(request.FormValue("model"))
	if enabled && model == "" {
		http.Error(writer, "model is required when advisor is enabled", http.StatusBadRequest)
		return
	}
	_, err = server.hub.UpdateAdvisorSettings(request.Context(), cortex.UpdateAdvisorSettingsCommand{
		ActorID: session.AgentID, Enabled: enabled, Endpoint: endpoint, Model: model,
		InputTokenBudget: inputBudget, OutputTokenBudget: outputBudget,
		Effort: cortex.AdvisorEffort(request.FormValue("effort")),
	})
	if err != nil {
		writeDomainError(writer, err)
		return
	}
	http.Redirect(writer, request, "/knowledge?advisor=settings", http.StatusSeeOther)
}

func (server *Server) advisorRun(writer http.ResponseWriter, request *http.Request) {
	setDashboardHeaders(writer)
	session, ok := server.curatorDashboardRequest(writer, request)
	if !ok {
		return
	}
	if server.advisor == nil {
		http.Error(writer, "advisor is unavailable", http.StatusServiceUnavailable)
		return
	}
	status, err := server.hub.AdvisorStatus(request.Context(), session.AgentID)
	if err != nil {
		writeDomainError(writer, err)
		return
	}
	if !status.Settings.Enabled {
		http.Error(writer, "เปิดผู้ช่วยโมเดลก่อนใช้งาน", http.StatusConflict)
		return
	}
	report, err := server.hub.PreviewCuration(request.Context(), session.AgentID)
	if err != nil {
		writeDomainError(writer, err)
		return
	}
	suggestions, titles := advisorSuggestions(report)
	if len(suggestions) == 0 {
		http.Error(writer, "ยังไม่มีรายการที่ต้องให้โมเดลช่วยสรุป", http.StatusConflict)
		return
	}
	advice, adviceErr := server.advisor.Advise(request.Context(), intelligence.AdviceRequest{
		Endpoint: status.Settings.Endpoint, Model: status.Settings.Model,
		Effort: string(status.Settings.Effort), InputTokenBudget: status.Settings.InputTokenBudget,
		OutputTokenBudget: status.Settings.OutputTokenBudget, Suggestions: suggestions,
	})
	if adviceErr != nil {
		errorText := boundedText(adviceErr.Error(), 500)
		_, _ = server.hub.RecordAdvisorRun(request.Context(), cortex.AdvisorRunRecord{
			ActorID: session.AgentID, Model: status.Settings.Model, Status: "error", Error: errorText,
		})
		http.Error(writer, "ผู้ช่วยโมเดลทำงานไม่สำเร็จ: "+errorText, http.StatusBadGateway)
		return
	}
	_, err = server.hub.RecordAdvisorRun(request.Context(), cortex.AdvisorRunRecord{
		ActorID: session.AgentID, Model: status.Settings.Model,
		InputTokens: advice.InputTokens, OutputTokens: advice.OutputTokens,
		Status: "success", Summary: advice.Summary, ResponseJSON: advice.RawJSON,
	})
	if err != nil {
		writeDomainError(writer, err)
		return
	}
	view := advisorResultView{
		Summary: advice.Summary, Model: status.Settings.Model,
		InputTokens: advice.InputTokens, OutputTokens: advice.OutputTokens,
		Items: make([]advisorResultItem, 0, len(advice.Assessments)),
	}
	for _, assessment := range advice.Assessments {
		view.Items = append(view.Items, advisorResultItem{
			MemoryID: assessment.MemoryID, Title: titles[assessment.MemoryID],
			Verdict: advisorVerdictLabel(assessment.Verdict), Reason: assessment.Reason,
		})
	}
	writer.Header().Set("Cache-Control", "no-store")
	if err := dashboardTemplates.ExecuteTemplate(writer, "advisor_result.html", view); err != nil {
		http.Error(writer, "render advisor result", http.StatusInternalServerError)
	}
}

func advisorSuggestions(report cortex.CuratorReport) ([]intelligence.Suggestion, map[string]string) {
	suggestions := make([]intelligence.Suggestion, 0, min(12, len(report.Suggestions)))
	titles := make(map[string]string)
	for _, suggestion := range report.Suggestions {
		if len(suggestions) == 12 {
			break
		}
		presented := dashboardCuratorSuggestion{
			Reason: curatorReasonLabel(suggestion), Evidence: curatorEvidenceLabel(suggestion),
		}
		suggestions = append(suggestions, intelligence.Suggestion{
			MemoryID: suggestion.Memory.ID, Title: suggestion.Memory.Title,
			Content: suggestion.Memory.Content, Scope: string(suggestion.Memory.Scope),
			Kind: string(suggestion.Memory.Kind), Category: string(suggestion.Category),
			Reason: presented.Reason, Evidence: presented.Evidence,
		})
		titles[suggestion.Memory.ID] = suggestion.Memory.Title
	}
	return suggestions, titles
}

func advisorVerdictLabel(verdict string) string {
	switch verdict {
	case "support":
		return "เห็นด้วยกับคำแนะนำ"
	case "challenge":
		return "ควรทบทวนคำแนะนำ"
	default:
		return "หลักฐานยังไม่พอ"
	}
}

func boundedText(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit])
}
