package httpapi

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"strings"

	"cortex.local/cortex/internal/controlcenter"
	"cortex.local/cortex/internal/cortex"
	"cortex.local/cortex/internal/localauth"
)

type Server struct {
	hub      *cortex.Hub
	auth     Authenticator
	sessions *dashboardSessions
	control  runtimeControl
	launcher *localauth.Broker
}

func New(hub *cortex.Hub, auth Authenticator) http.Handler {
	return NewWithControl(hub, auth, nil)
}

type runtimeControl interface {
	Status(context.Context) (controlcenter.Status, error)
	Request(controlcenter.Action) error
	SyncHermes(context.Context) (controlcenter.SyncResult, error)
}

func NewWithControl(hub *cortex.Hub, auth Authenticator, control runtimeControl) http.Handler {
	return NewWithControlAndLauncher(hub, auth, control, nil)
}

func NewWithControlAndLauncher(
	hub *cortex.Hub,
	auth Authenticator,
	control runtimeControl,
	launcher *localauth.Broker,
) http.Handler {
	server := &Server{
		hub: hub, auth: auth, sessions: newDashboardSessions(), control: control, launcher: launcher,
	}
	mux := http.NewServeMux()
	staticFiles, _ := fs.Sub(dashboardAssets, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFiles))))
	mux.HandleFunc("GET /", server.dashboard)
	mux.HandleFunc("POST /login", server.login)
	mux.HandleFunc("POST /logout", server.logout)
	mux.HandleFunc("POST /v1/dashboard/sessions", server.issueDashboardSession)
	mux.HandleFunc("GET /ui/session", server.consumeDashboardSession)
	mux.HandleFunc("POST /ui/system/action", server.systemAction)
	mux.HandleFunc("POST /ui/hermes/sync", server.hermesSync)
	mux.HandleFunc("GET /ui/memories/{memoryID}", server.dashboardDetail)
	mux.HandleFunc("POST /ui/memories/{memoryID}/review", server.dashboardReview)
	mux.HandleFunc("GET /v1/health", server.health)
	mux.Handle("GET /v1/capabilities", server.authenticated(http.HandlerFunc(server.capabilities)))
	mux.Handle("POST /v1/memories", server.authenticated(http.HandlerFunc(server.remember)))
	mux.Handle("POST /v1/recalls", server.authenticated(http.HandlerFunc(server.recall)))
	mux.Handle("POST /v1/memories/{memoryID}/feedback", server.authenticated(http.HandlerFunc(server.feedback)))
	mux.Handle("POST /v1/memories/{memoryID}/review", server.authenticated(http.HandlerFunc(server.review)))
	mux.Handle("GET /v1/memories/{memoryID}/history", server.authenticated(http.HandlerFunc(server.history)))
	return mux
}

func (server *Server) authenticated(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		token := bearerToken(request)
		agentID, ok := server.auth.Authenticate(token)
		if !ok || strings.TrimSpace(agentID) == "" {
			writeAPIError(writer, http.StatusUnauthorized, "unauthorized", "valid bearer token required")
			return
		}
		next.ServeHTTP(writer, request.WithContext(withIdentity(request.Context(), agentID)))
	})
}

func (server *Server) health(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (server *Server) capabilities(writer http.ResponseWriter, request *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{
		"version":    "v1",
		"agent_id":   identityFromRequest(request),
		"operations": []string{"remember", "recall", "feedback", "review", "history"},
		"search":     []string{"fts5"},
		"scopes":     []cortex.Scope{cortex.ScopeGlobal, cortex.ScopeProject, cortex.ScopeDomain, cortex.ScopePrivate},
	})
}

func idempotencyKey(writer http.ResponseWriter, request *http.Request) (string, bool) {
	key := strings.TrimSpace(request.Header.Get("Idempotency-Key"))
	if key == "" {
		writeAPIError(writer, http.StatusBadRequest, "missing_idempotency_key", "Idempotency-Key header is required")
		return "", false
	}
	return key, true
}

func writeDomainError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, cortex.ErrInvalidInput):
		writeAPIError(writer, http.StatusBadRequest, "invalid_input", err.Error())
	case errors.Is(err, cortex.ErrForbidden):
		writeAPIError(writer, http.StatusForbidden, "forbidden", "operation is not permitted")
	case errors.Is(err, cortex.ErrNotFound):
		writeAPIError(writer, http.StatusNotFound, "not_found", "memory not found")
	case errors.Is(err, cortex.ErrConflict):
		writeAPIError(writer, http.StatusConflict, "conflict", err.Error())
	default:
		writeAPIError(writer, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
