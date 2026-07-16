package httpapi

import (
	"net"
	"net/http"
	"net/url"
	"time"

	"cortex.local/cortex/internal/localauth"
)

func (server *Server) issueDashboardSession(writer http.ResponseWriter, request *http.Request) {
	setDashboardHeaders(writer)
	writer.Header().Set("Cache-Control", "no-store")
	if server.launcher == nil {
		http.NotFound(writer, request)
		return
	}
	if !requestFromLoopback(request) ||
		!server.launcher.Authorize(localauth.ProofFromHeader(request.Header), time.Now().UTC()) {
		http.Error(writer, "local launcher authorization required", http.StatusUnauthorized)
		return
	}
	code, err := server.launcher.Issue(time.Now().UTC())
	if err != nil {
		http.Error(writer, "create dashboard code", http.StatusInternalServerError)
		return
	}
	writeJSON(writer, http.StatusCreated, map[string]string{
		"path": "/ui/session?code=" + url.QueryEscape(code),
	})
}

func (server *Server) consumeDashboardSession(writer http.ResponseWriter, request *http.Request) {
	setDashboardHeaders(writer)
	writer.Header().Set("Cache-Control", "no-store")
	if server.launcher == nil {
		http.NotFound(writer, request)
		return
	}
	agentID, ok := server.launcher.Consume(request.URL.Query().Get("code"), time.Now().UTC())
	if !ok {
		http.Error(writer, "dashboard code is invalid or expired", http.StatusUnauthorized)
		return
	}
	if err := server.establishDashboardSession(writer, request, agentID); err != nil {
		http.Error(writer, "create session", http.StatusInternalServerError)
	}
}

func requestFromLoopback(request *http.Request) bool {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
