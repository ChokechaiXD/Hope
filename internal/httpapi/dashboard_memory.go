package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"cortex.local/cortex/internal/cortex"
)

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
