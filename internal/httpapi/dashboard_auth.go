package httpapi

import (
	"net/http"
	"strings"
	"time"
)

func (server *Server) login(writer http.ResponseWriter, request *http.Request) {
	request.Body = http.MaxBytesReader(writer, request.Body, 4096)
	if err := request.ParseForm(); err != nil {
		http.Error(writer, "invalid login", http.StatusBadRequest)
		return
	}
	token := strings.TrimSpace(request.FormValue("token"))
	agentID, ok := authenticateDashboard(server.auth, token)
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
