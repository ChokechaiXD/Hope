package httpapi

import (
	"errors"
	"net/http"

	"cortex.local/cortex/internal/controlcenter"
)

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
	_ = dashboardTemplates.ExecuteTemplate(writer, "system_action.html", view)
}
