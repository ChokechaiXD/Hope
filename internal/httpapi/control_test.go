package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"cortex.local/cortex/internal/controlcenter"
	"cortex.local/cortex/internal/cortex"
)

type fakeControlCenter struct {
	status    controlcenter.Status
	requested []controlcenter.Action
	synced    int
	settings  []controlcenter.AgentSettings
	updated   *controlcenter.AgentSettings
}

func (fake *fakeControlCenter) SyncHermes(context.Context) (controlcenter.SyncResult, error) {
	fake.synced++
	return controlcenter.SyncResult{
		Agents: []string{"mika", "aura", "nari", "nua", "sora"}, BackupDir: `C:\Cortex\backups\sync`,
	}, nil
}

func (fake *fakeControlCenter) Status(context.Context) (controlcenter.Status, error) {
	return fake.status, nil
}

func (fake *fakeControlCenter) Request(action controlcenter.Action) error {
	fake.requested = append(fake.requested, action)
	return nil
}

func (fake *fakeControlCenter) AgentSettings(context.Context) ([]controlcenter.AgentSettings, error) {
	return fake.settings, nil
}

func (fake *fakeControlCenter) UpdateAgentSettings(
	_ context.Context,
	settings controlcenter.AgentSettings,
) (controlcenter.AgentSettingsResult, error) {
	fake.updated = &settings
	return controlcenter.AgentSettingsResult{Settings: settings, BackupFile: `C:\Cortex\backups\sora.json`}, nil
}

func TestDashboardShowsRuntimeAndSafelyRequestsRestartOrStop(t *testing.T) {
	t.Parallel()

	hub, err := cortex.Open(cortex.Config{
		DatabasePath: filepath.Join(t.TempDir(), "cortex.db"),
		AdminAgents:  []string{"mika"},
	})
	if err != nil {
		t.Fatalf("open Cortex: %v", err)
	}
	t.Cleanup(func() { _ = hub.Close() })
	control := &fakeControlCenter{status: controlcenter.Status{
		Running: true, Version: "0.2.0", Listen: "127.0.0.1:7777", Port: 7777,
		PID: 4242, DataDir: `C:\Cortex`, Uptime: 2_000_000_000,
	}}
	handler := NewWithControl(hub, StaticAuthenticator{"mika-token": "mika"}, control)

	loginForm := url.Values{"token": {"mika-token"}}
	loginRequest := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(loginForm.Encode()))
	loginRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	login := httptest.NewRecorder()
	handler.ServeHTTP(login, loginRequest)
	cookies := login.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("login cookies = %#v", cookies)
	}

	dashboardRequest := httptest.NewRequest(http.MethodGet, "/?view=advanced", nil)
	dashboardRequest.AddCookie(cookies[0])
	dashboard := httptest.NewRecorder()
	handler.ServeHTTP(dashboard, dashboardRequest)
	body := dashboard.Body.String()
	for _, expected := range []string{"ระบบพร้อมใช้งาน", "กำลังทำงานในเครื่อง", "127.0.0.1:7777", "PID 4242", "เริ่ม Cortex ใหม่", "ปิด Cortex", "ค้นหาและเชื่อมเอเจนต์"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("dashboard omitted %q: %s", expected, body)
		}
	}
	csrfMatch := regexp.MustCompile(`name="csrf" value="([^"]+)"`).FindStringSubmatch(body)
	if len(csrfMatch) != 2 {
		t.Fatal("dashboard omitted CSRF token")
	}
	unsafeSync := httptest.NewRequest(http.MethodPost, "/ui/hermes/sync", strings.NewReader(""))
	unsafeSync.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	unsafeSync.AddCookie(cookies[0])
	unsafe := httptest.NewRecorder()
	handler.ServeHTTP(unsafe, unsafeSync)
	if unsafe.Code != http.StatusForbidden || control.synced != 0 {
		t.Fatalf("sync without CSRF status=%d calls=%d", unsafe.Code, control.synced)
	}
	syncForm := url.Values{"csrf": {csrfMatch[1]}}
	syncRequest := httptest.NewRequest(http.MethodPost, "/ui/hermes/sync", strings.NewReader(syncForm.Encode()))
	syncRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	syncRequest.AddCookie(cookies[0])
	synced := httptest.NewRecorder()
	handler.ServeHTTP(synced, syncRequest)
	if synced.Code != http.StatusOK || control.synced != 1 ||
		!strings.Contains(synced.Body.String(), "เชื่อมเอเจนต์แล้ว 5 ตัว") ||
		!strings.Contains(synced.Body.String(), `C:\Cortex\backups\sync`) {
		t.Fatalf("sync status=%d calls=%d body=%s", synced.Code, control.synced, synced.Body.String())
	}

	restartForm := url.Values{"csrf": {csrfMatch[1]}, "action": {"restart"}}
	restartRequest := httptest.NewRequest(http.MethodPost, "/ui/system/action", strings.NewReader(restartForm.Encode()))
	restartRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	restartRequest.AddCookie(cookies[0])
	restart := httptest.NewRecorder()
	handler.ServeHTTP(restart, restartRequest)
	if restart.Code != http.StatusAccepted || !strings.Contains(restart.Body.String(), "กำลังเริ่ม Cortex ใหม่") ||
		len(control.requested) != 1 || control.requested[0] != controlcenter.ActionRestart {
		t.Fatalf("restart status=%d body=%s actions=%#v", restart.Code, restart.Body.String(), control.requested)
	}

	stopForm := url.Values{"csrf": {csrfMatch[1]}, "action": {"stop"}}
	stopRequest := httptest.NewRequest(http.MethodPost, "/ui/system/action", strings.NewReader(stopForm.Encode()))
	stopRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	stopRequest.AddCookie(cookies[0])
	stop := httptest.NewRecorder()
	handler.ServeHTTP(stop, stopRequest)
	if stop.Code != http.StatusBadRequest || len(control.requested) != 1 {
		t.Fatalf("unconfirmed stop status=%d actions=%#v", stop.Code, control.requested)
	}

	stopForm.Set("confirm", "stop")
	confirmedRequest := httptest.NewRequest(http.MethodPost, "/ui/system/action", strings.NewReader(stopForm.Encode()))
	confirmedRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	confirmedRequest.AddCookie(cookies[0])
	confirmed := httptest.NewRecorder()
	handler.ServeHTTP(confirmed, confirmedRequest)
	if confirmed.Code != http.StatusAccepted || !strings.Contains(confirmed.Body.String(), "กำลังปิด Cortex") ||
		len(control.requested) != 2 || control.requested[1] != controlcenter.ActionStop {
		t.Fatalf("confirmed stop status=%d body=%s actions=%#v", confirmed.Code, confirmed.Body.String(), control.requested)
	}
}

func TestDashboardEditsHermesAgentSettingsWithoutExposingTokens(t *testing.T) {
	t.Parallel()

	hub, err := cortex.Open(cortex.Config{
		DatabasePath: filepath.Join(t.TempDir(), "cortex.db"), AdminAgents: []string{"mika"},
	})
	if err != nil {
		t.Fatalf("open Cortex: %v", err)
	}
	t.Cleanup(func() { _ = hub.Close() })
	control := &fakeControlCenter{
		status: controlcenter.Status{Running: true, Version: "0.3.2", Listen: "127.0.0.1:7777"},
		settings: []controlcenter.AgentSettings{{
			AgentID: "sora", DefaultProject: "cortex", DefaultDomain: "coding",
			AutoCaptureEnabled: true, AutoCaptureEveryTurns: 5, AutoCaptureMaxChars: 1000,
			PrefetchTokenBudget: 700, RecallTokenBudget: 1200,
		}},
	}
	handler := NewWithControl(hub, StaticAuthenticator{"mika-token": "mika"}, control)
	cookie := loginDashboardForTest(t, handler, "mika-token")
	dashboard := requestDashboardForTest(t, handler, cookie)
	for _, expected := range []string{"การเรียนรู้ของแต่ละเอเจนต์", "SORA", "cortex", "coding", "ทุก 5 เทิร์น"} {
		if !strings.Contains(dashboard, expected) {
			t.Fatalf("agent settings omitted %q: %s", expected, dashboard)
		}
	}
	if strings.Contains(dashboard, "mika-token") || strings.Contains(dashboard, "Bearer") {
		t.Fatalf("agent settings exposed a credential: %s", dashboard)
	}

	csrf := dashboardCSRFForTest(t, dashboard)
	form := url.Values{
		"csrf": {csrf}, "agent_id": {"sora"}, "default_project": {"novelclaw"},
		"default_domain": {"coding"}, "auto_capture_enabled": {"yes"},
		"auto_capture_every_turns": {"10"}, "auto_capture_max_chars": {"2000"},
		"prefetch_token_budget": {"500"}, "recall_token_budget": {"900"},
	}
	request := httptest.NewRequest(http.MethodPost, "/ui/hermes/settings", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther || control.updated == nil {
		t.Fatalf("settings update status=%d updated=%#v body=%s", response.Code, control.updated, response.Body.String())
	}
	if control.updated.DefaultProject != "novelclaw" || control.updated.AutoCaptureEveryTurns != 10 ||
		control.updated.PrefetchTokenBudget != 500 {
		t.Fatalf("updated settings = %#v", control.updated)
	}
}
