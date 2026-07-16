package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cortex.local/cortex/internal/cortex"
	"cortex.local/cortex/internal/localauth"
)

func TestLocalLauncherCreatesOneTimeAdminDashboardSession(t *testing.T) {
	t.Parallel()

	hub, err := cortex.Open(cortex.Config{
		DatabasePath: filepath.Join(t.TempDir(), "cortex.db"),
		AdminAgents:  []string{"mika"},
	})
	if err != nil {
		t.Fatalf("open Cortex: %v", err)
	}
	t.Cleanup(func() { _ = hub.Close() })
	secret := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32))
	launcher, err := localauth.NewBroker(secret, "mika")
	if err != nil {
		t.Fatalf("new launcher broker: %v", err)
	}
	handler := NewWithControlAndLauncher(
		hub, StaticAuthenticator{"mika-token": "mika"}, nil, launcher,
	)

	nonLoopback := httptest.NewRequest(http.MethodPost, "/v1/dashboard/sessions", nil)
	nonLoopback.RemoteAddr = "192.0.2.10:1234"
	proof, err := localauth.NewProof(secret, time.Now().UTC())
	if err != nil {
		t.Fatalf("new launcher proof: %v", err)
	}
	proof.Apply(nonLoopback.Header)
	blocked := httptest.NewRecorder()
	handler.ServeHTTP(blocked, nonLoopback)
	if blocked.Code != http.StatusUnauthorized {
		t.Fatalf("non-loopback launcher status=%d body=%s", blocked.Code, blocked.Body.String())
	}

	issue := httptest.NewRequest(http.MethodPost, "/v1/dashboard/sessions", nil)
	issue.RemoteAddr = "127.0.0.1:1234"
	proof, err = localauth.NewProof(secret, time.Now().UTC())
	if err != nil {
		t.Fatalf("new launcher proof: %v", err)
	}
	proof.Apply(issue.Header)
	issued := httptest.NewRecorder()
	handler.ServeHTTP(issued, issue)
	if issued.Code != http.StatusCreated {
		t.Fatalf("launcher issue status=%d body=%s", issued.Code, issued.Body.String())
	}
	var payload struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(issued.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode launcher response: %v", err)
	}
	if !strings.HasPrefix(payload.Path, "/ui/session?code=") {
		t.Fatalf("launcher path=%q", payload.Path)
	}

	exchange := httptest.NewRequest(http.MethodGet, payload.Path, nil)
	exchange.RemoteAddr = "127.0.0.1:1234"
	exchanged := httptest.NewRecorder()
	handler.ServeHTTP(exchanged, exchange)
	if exchanged.Code != http.StatusSeeOther {
		t.Fatalf("launcher exchange status=%d body=%s", exchanged.Code, exchanged.Body.String())
	}
	cookies := exchanged.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("launcher cookies=%#v", cookies)
	}
	if strings.Contains(payload.Path, cookies[0].Value) || strings.Contains(cookies[0].Value, secret) {
		t.Fatal("dashboard cookie exposed launcher credentials")
	}

	replay := httptest.NewRecorder()
	handler.ServeHTTP(replay, httptest.NewRequest(http.MethodGet, payload.Path, nil))
	if replay.Code != http.StatusUnauthorized {
		t.Fatalf("replayed launcher code status=%d body=%s", replay.Code, replay.Body.String())
	}

	dashboardRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	dashboardRequest.AddCookie(cookies[0])
	dashboard := httptest.NewRecorder()
	handler.ServeHTTP(dashboard, dashboardRequest)
	if dashboard.Code != http.StatusOK || !strings.Contains(dashboard.Body.String(), "Memory explorer") {
		t.Fatalf("launched dashboard status=%d body=%s", dashboard.Code, dashboard.Body.String())
	}
}
