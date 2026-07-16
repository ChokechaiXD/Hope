package httpapi

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"cortex.local/cortex/internal/controlcenter"
	"cortex.local/cortex/internal/cortex"
)

//go:embed templates/*.html static/*.css
var dashboardAssets embed.FS

var dashboardTemplates = template.Must(template.ParseFS(dashboardAssets, "templates/*.html"))

type dashboardView struct {
	AgentID   string
	CSRFToken string
	Advanced  bool
	Total     int
	Candidate int
	Active    int
	Canonical int
	Memories  []dashboardMemory
	Filters   dashboardFilters
	Matched   int
	System    *dashboardSystem
	SystemErr string
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

type dashboardDetailView struct {
	AgentID   string
	CSRFToken string
	Advanced  bool
	Memory    dashboardMemory
	Events    []dashboardEvent
}

type dashboardEvent struct {
	cortex.Event
	Label        string
	MetadataJSON string
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
	}
	writer.Header().Set("Cache-Control", "no-store")
	if err := dashboardTemplates.ExecuteTemplate(writer, "dashboard.html", view); err != nil {
		http.Error(writer, "render dashboard", http.StatusInternalServerError)
	}
}

type hermesSyncView struct {
	Count     int
	Agents    []string
	BackupDir string
}

func (server *Server) hermesSync(writer http.ResponseWriter, request *http.Request) {
	setDashboardHeaders(writer)
	_, session, ok := server.sessions.fromRequest(request)
	if !ok {
		http.Redirect(writer, request, "/", http.StatusSeeOther)
		return
	}
	if server.control == nil || !server.hub.CanGovern(session.AgentID) {
		http.Error(writer, "Hermes sync is not permitted", http.StatusForbidden)
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, 4096)
	if err := request.ParseForm(); err != nil {
		http.Error(writer, "invalid Hermes sync request", http.StatusBadRequest)
		return
	}
	if !validCSRF(session.CSRFToken, request.FormValue("csrf")) {
		http.Error(writer, "invalid csrf token", http.StatusForbidden)
		return
	}
	result, err := server.control.SyncHermes(request.Context())
	if errors.Is(err, controlcenter.ErrActionPending) {
		http.Error(writer, "another Cortex operation is already running", http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(writer, "sync Hermes agents: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	if err := dashboardTemplates.ExecuteTemplate(writer, "hermes_sync.html", hermesSyncView{
		Count: len(result.Agents), Agents: result.Agents, BackupDir: result.BackupDir,
	}); err != nil {
		http.Error(writer, "render Hermes sync", http.StatusInternalServerError)
	}
}

type systemActionView struct {
	Title   string
	Message string
	Restart bool
}

func (server *Server) systemAction(writer http.ResponseWriter, request *http.Request) {
	setDashboardHeaders(writer)
	_, session, ok := server.sessions.fromRequest(request)
	if !ok {
		http.Redirect(writer, request, "/", http.StatusSeeOther)
		return
	}
	if server.control == nil || !server.hub.CanGovern(session.AgentID) {
		http.Error(writer, "system control is not permitted", http.StatusForbidden)
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, 4096)
	if err := request.ParseForm(); err != nil {
		http.Error(writer, "invalid system action", http.StatusBadRequest)
		return
	}
	if !validCSRF(session.CSRFToken, request.FormValue("csrf")) {
		http.Error(writer, "invalid csrf token", http.StatusForbidden)
		return
	}
	action := controlcenter.Action(request.FormValue("action"))
	view := systemActionView{}
	switch action {
	case controlcenter.ActionRestart:
		view = systemActionView{Title: "กำลังเริ่ม Cortex ใหม่", Message: "Cortex จะกลับมาทำงานที่พอร์ตเดิมโดยอัตโนมัติ", Restart: true}
	case controlcenter.ActionStop:
		if request.FormValue("confirm") != "stop" {
			http.Error(writer, "stop confirmation is required", http.StatusBadRequest)
			return
		}
		view = systemActionView{Title: "กำลังปิด Cortex", Message: "เปิดอีกครั้งได้จาก Cortex Dashboard ในเมนู Start"}
	default:
		http.Error(writer, "unknown system action", http.StatusBadRequest)
		return
	}
	if err := server.control.Request(action); err != nil {
		http.Error(writer, "system action already pending", http.StatusConflict)
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(http.StatusAccepted)
	if err := dashboardTemplates.ExecuteTemplate(writer, "system_action.html", view); err != nil {
		return
	}
}

func (server *Server) login(writer http.ResponseWriter, request *http.Request) {
	request.Body = http.MaxBytesReader(writer, request.Body, 4096)
	if err := request.ParseForm(); err != nil {
		http.Error(writer, "invalid login", http.StatusBadRequest)
		return
	}
	token := strings.TrimSpace(request.FormValue("token"))
	agentID, ok := server.auth.Authenticate(token)
	if !ok {
		http.Error(writer, "invalid token", http.StatusUnauthorized)
		return
	}
	if err := server.establishDashboardSession(writer, request, agentID); err != nil {
		http.Error(writer, "create session", http.StatusInternalServerError)
	}
}

func (server *Server) establishDashboardSession(
	writer http.ResponseWriter,
	request *http.Request,
	agentID string,
) error {
	sessionID, session, err := server.sessions.create(agentID)
	if err != nil {
		return err
	}
	http.SetCookie(writer, &http.Cookie{
		Name:     dashboardCookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Expires:  session.ExpiresAt,
		MaxAge:   int(dashboardSessionTTL / time.Second),
	})
	http.Redirect(writer, request, "/", http.StatusSeeOther)
	return nil
}

func (server *Server) logout(writer http.ResponseWriter, request *http.Request) {
	sessionID, session, ok := server.sessions.fromRequest(request)
	if !ok {
		http.Redirect(writer, request, "/", http.StatusSeeOther)
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, 4096)
	if err := request.ParseForm(); err != nil || !validCSRF(session.CSRFToken, request.FormValue("csrf")) {
		http.Error(writer, "invalid csrf token", http.StatusForbidden)
		return
	}
	server.sessions.delete(sessionID)
	http.SetCookie(writer, &http.Cookie{
		Name: dashboardCookieName, Value: "", Path: "/", HttpOnly: true,
		SameSite: http.SameSiteStrictMode, MaxAge: -1,
	})
	http.Redirect(writer, request, "/", http.StatusSeeOther)
}

func (server *Server) dashboardReview(writer http.ResponseWriter, request *http.Request) {
	_, session, ok := server.sessions.fromRequest(request)
	if !ok {
		http.Redirect(writer, request, "/", http.StatusSeeOther)
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, 8192)
	if err := request.ParseForm(); err != nil {
		http.Error(writer, "invalid review", http.StatusBadRequest)
		return
	}
	if !validCSRF(session.CSRFToken, request.FormValue("csrf")) {
		http.Error(writer, "invalid csrf token", http.StatusForbidden)
		return
	}
	requestID, err := dashboardRequestID()
	if err != nil {
		http.Error(writer, "create review id", http.StatusInternalServerError)
		return
	}
	_, err = server.hub.Review(request.Context(), cortex.ReviewCommand{
		IdempotencyKey: requestID,
		MemoryID:       request.PathValue("memoryID"),
		ActorID:        session.AgentID,
		Decision:       cortex.ReviewDecision(request.FormValue("decision")),
		Reason:         strings.TrimSpace(request.FormValue("reason")),
	})
	if err != nil {
		writeDomainError(writer, err)
		return
	}
	redirectTo := "/"
	if expected := "/ui/memories/" + request.PathValue("memoryID"); request.FormValue("return_to") == expected {
		redirectTo = expected
	}
	http.Redirect(writer, request, redirectTo, http.StatusSeeOther)
}

func (server *Server) dashboardDetail(writer http.ResponseWriter, request *http.Request) {
	setDashboardHeaders(writer)
	_, session, ok := server.sessions.fromRequest(request)
	if !ok {
		http.Redirect(writer, request, "/", http.StatusSeeOther)
		return
	}
	memory, events, err := server.hub.Inspect(request.Context(), cortex.HistoryQuery{
		MemoryID: request.PathValue("memoryID"), AgentID: session.AgentID,
	})
	if err != nil {
		writeDomainError(writer, err)
		return
	}
	view := dashboardDetailView{
		AgentID: session.AgentID, CSRFToken: session.CSRFToken,
		Advanced: request.URL.Query().Get("view") == "advanced", Memory: presentMemory(memory),
	}
	for _, event := range events {
		metadata, _ := json.Marshal(event.Metadata)
		view.Events = append(view.Events, dashboardEvent{
			Event: event, Label: eventLabel(event.Type), MetadataJSON: string(metadata),
		})
	}
	writer.Header().Set("Cache-Control", "no-store")
	if err := dashboardTemplates.ExecuteTemplate(writer, "detail.html", view); err != nil {
		http.Error(writer, "render memory detail", http.StatusInternalServerError)
	}
}

func dashboardRequestID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate dashboard request id: %w", err)
	}
	return "dashboard/review/" + hex.EncodeToString(raw[:]), nil
}

func validCSRF(expected, supplied string) bool {
	return expected != "" && subtle.ConstantTimeCompare([]byte(expected), []byte(supplied)) == 1
}

func setDashboardHeaders(writer http.ResponseWriter) {
	writer.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("Referrer-Policy", "no-referrer")
}
