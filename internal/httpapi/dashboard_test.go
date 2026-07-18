package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"cortex.local/cortex/internal/cortex"
	"cortex.local/cortex/internal/intelligence"
)

type fakeDashboardAdvisor struct {
	modelCalls int
	lastInput  intelligence.AdviceRequest
}

func (advisor *fakeDashboardAdvisor) Models(context.Context, string) ([]intelligence.Model, error) {
	advisor.modelCalls++
	return []intelligence.Model{{ID: "reviewer", OwnedBy: "9router"}}, nil
}

func (advisor *fakeDashboardAdvisor) Advise(
	_ context.Context,
	input intelligence.AdviceRequest,
) (intelligence.Advice, error) {
	advisor.lastInput = input
	return intelligence.Advice{
		Summary: "ควรเปิดดูหลักฐานก่อนรับเป็นความรู้ร่วม",
		Assessments: []intelligence.Assessment{{
			MemoryID: input.Suggestions[0].MemoryID,
			Verdict:  "support",
			Reason:   "มีเอเจนต์อิสระเห็นตรงกันและมีแหล่งอ้างอิง",
		}},
		InputTokens: 410, OutputTokens: 92,
		RawJSON: `{"summary":"ควรเปิดดูหลักฐานก่อนรับเป็นความรู้ร่วม","items":[]}`,
	}, nil
}

func TestDashboardLoginAndReview(t *testing.T) {
	t.Parallel()

	hub, err := cortex.Open(cortex.Config{
		DatabasePath: filepath.Join(t.TempDir(), "cortex.db"),
		AdminAgents:  []string{"mika"},
	})
	if err != nil {
		t.Fatalf("open Cortex: %v", err)
	}
	t.Cleanup(func() { _ = hub.Close() })
	memory, err := hub.Remember(context.Background(), cortex.RememberCommand{
		IdempotencyKey: "dashboard/candidate-1",
		Kind:           cortex.KindDecision,
		Scope:          cortex.ScopeProject,
		ScopeKey:       "novelclaw",
		MemoryKey:      "novelclaw.output-format",
		Title:          "Canonical output uses .th.json",
		Content:        "Translation output must use canonical .th.json files.",
		AgentID:        "sola",
	})
	if err != nil {
		t.Fatalf("create dashboard candidate: %v", err)
	}
	other, err := hub.Remember(context.Background(), cortex.RememberCommand{
		IdempotencyKey: "dashboard/other-1", Kind: cortex.KindFact, Scope: cortex.ScopeGlobal,
		MemoryKey: "research.sources", Title: "Unrelated research source",
		Content: "Research sources should be cited.", AgentID: "nua",
	})
	if err != nil {
		t.Fatalf("create unrelated candidate: %v", err)
	}
	handler := New(hub, StaticAuthenticator{"mika-token": "mika"})

	loginPage := httptest.NewRecorder()
	handler.ServeHTTP(loginPage, httptest.NewRequest(http.MethodGet, "/", nil))
	if loginPage.Code != http.StatusOK || !strings.Contains(loginPage.Body.String(), "เข้าสู่ระบบ") {
		t.Fatalf("login page status=%d body=%s", loginPage.Code, loginPage.Body.String())
	}

	form := url.Values{"token": {"mika-token"}}
	loginRequest := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	loginRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	login := httptest.NewRecorder()
	handler.ServeHTTP(login, loginRequest)
	if login.Code != http.StatusSeeOther {
		t.Fatalf("login status=%d body=%s", login.Code, login.Body.String())
	}
	response := login.Result()
	cookies := response.Cookies()
	_ = response.Body.Close()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("login cookies = %#v", cookies)
	}
	if cookies[0].Value == "mika-token" {
		t.Fatal("dashboard cookie contains the raw bearer token")
	}

	dashboardRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	dashboardRequest.AddCookie(cookies[0])
	dashboard := httptest.NewRecorder()
	handler.ServeHTTP(dashboard, dashboardRequest)
	if dashboard.Code != http.StatusOK || !strings.Contains(dashboard.Body.String(), memory.Title) {
		t.Fatalf("dashboard status=%d body=%s", dashboard.Code, dashboard.Body.String())
	}
	dashboardBody := dashboard.Body.String()
	for _, expected := range []string{
		"คลังความรู้", "รอตรวจ", "ข้อตัดสินใจ", "โปรเจกต์ · novelclaw",
		"รายละเอียดขั้นสูง", "HOPE แนะนำให้ดูอะไร", "รอหลักฐาน",
	} {
		if !strings.Contains(dashboardBody, expected) {
			t.Fatalf("human dashboard omitted %q: %s", expected, dashboardBody)
		}
	}
	if strings.Contains(dashboardBody, "Truth score") || strings.Contains(dashboardBody, "Memory key") {
		t.Fatalf("simple dashboard exposed advanced fields: %s", dashboardBody)
	}
	csrfMatch := regexp.MustCompile(`name="csrf" value="([^"]+)"`).FindStringSubmatch(dashboard.Body.String())
	if len(csrfMatch) != 2 {
		t.Fatalf("dashboard did not render a CSRF token: %s", dashboard.Body.String())
	}
	csrfToken := csrfMatch[1]
	settingsForm := url.Values{
		"csrf": {csrfToken}, "mode": {"assisted"}, "run_every_candidates": {"5"},
		"batch_limit": {"25"}, "min_agreement": {"3"},
	}
	settingsRequest := httptest.NewRequest(
		http.MethodPost, "/ui/curator/settings", strings.NewReader(settingsForm.Encode()),
	)
	settingsRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	settingsRequest.AddCookie(cookies[0])
	settingsResponse := httptest.NewRecorder()
	handler.ServeHTTP(settingsResponse, settingsRequest)
	if settingsResponse.Code != http.StatusSeeOther {
		t.Fatalf("curator settings status=%d body=%s", settingsResponse.Code, settingsResponse.Body.String())
	}
	curatorStatus, err := hub.CuratorStatus(context.Background(), "mika")
	if err != nil || curatorStatus.Settings.Mode != cortex.CuratorAssisted || curatorStatus.Settings.MinAgreement != 3 {
		t.Fatalf("curator settings = %#v error=%v", curatorStatus.Settings, err)
	}

	runForm := url.Values{"csrf": {csrfToken}}
	runRequest := httptest.NewRequest(http.MethodPost, "/ui/curator/run", strings.NewReader(runForm.Encode()))
	runRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	runRequest.AddCookie(cookies[0])
	runResponse := httptest.NewRecorder()
	handler.ServeHTTP(runResponse, runRequest)
	if runResponse.Code != http.StatusSeeOther {
		t.Fatalf("curator run status=%d body=%s", runResponse.Code, runResponse.Body.String())
	}
	curatorStatus, err = hub.CuratorStatus(context.Background(), "mika")
	if err != nil || curatorStatus.LastRun == nil || curatorStatus.LastRun.Trigger != "dashboard" {
		t.Fatalf("curator last run = %#v error=%v", curatorStatus.LastRun, err)
	}

	filteredRequest := httptest.NewRequest(
		http.MethodGet,
		"/?view=advanced&q=canonical+output&lifecycle=candidate&kind=decision&scope=project&scope_key=novelclaw&created_by=sola",
		nil,
	)
	filteredRequest.AddCookie(cookies[0])
	filtered := httptest.NewRecorder()
	handler.ServeHTTP(filtered, filteredRequest)
	filteredBody := filtered.Body.String()
	for _, expected := range []string{
		"คลังความรู้", "พบ 1 รายการ", memory.Title, "Truth score", "Memory key",
		`value="canonical output"`, `value="candidate" selected`, `value="decision" selected`,
		`value="project" selected`, `value="novelclaw"`, `value="sola"`,
	} {
		if filtered.Code != http.StatusOK || !strings.Contains(filteredBody, expected) {
			t.Fatalf("filtered dashboard omitted %q: status=%d body=%s", expected, filtered.Code, filteredBody)
		}
	}
	memoryListStart := strings.Index(filteredBody, `<section class="memory-list"`)
	if memoryListStart < 0 || strings.Contains(filteredBody[memoryListStart:], other.Title) {
		t.Fatalf("filtered memory list included unrelated memory: %s", filteredBody)
	}

	invalidFilter := httptest.NewRequest(http.MethodGet, "/?lifecycle=unknown", nil)
	invalidFilter.AddCookie(cookies[0])
	invalid := httptest.NewRecorder()
	handler.ServeHTTP(invalid, invalidFilter)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid filter status=%d body=%s", invalid.Code, invalid.Body.String())
	}

	cookieOnlyAPI := httptest.NewRequest(http.MethodGet, "/v1/capabilities", nil)
	cookieOnlyAPI.AddCookie(cookies[0])
	cookieOnly := httptest.NewRecorder()
	handler.ServeHTTP(cookieOnly, cookieOnlyAPI)
	if cookieOnly.Code != http.StatusUnauthorized {
		t.Fatalf("dashboard cookie authenticated API request: status=%d", cookieOnly.Code)
	}

	missingCSRFForm := url.Values{"decision": {"approve"}}
	missingCSRFRequest := httptest.NewRequest(
		http.MethodPost,
		"/ui/memories/"+memory.ID+"/review",
		strings.NewReader(missingCSRFForm.Encode()),
	)
	missingCSRFRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	missingCSRFRequest.AddCookie(cookies[0])
	missingCSRF := httptest.NewRecorder()
	handler.ServeHTTP(missingCSRF, missingCSRFRequest)
	if missingCSRF.Code != http.StatusForbidden {
		t.Fatalf("review without CSRF status=%d, want 403", missingCSRF.Code)
	}

	reviewForm := url.Values{
		"csrf": {csrfToken}, "decision": {"approve"}, "reason": {"Reviewed in dashboard"},
	}
	reviewRequest := httptest.NewRequest(
		http.MethodPost,
		"/ui/memories/"+memory.ID+"/review",
		strings.NewReader(reviewForm.Encode()),
	)
	reviewRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reviewRequest.AddCookie(cookies[0])
	review := httptest.NewRecorder()
	handler.ServeHTTP(review, reviewRequest)
	if review.Code != http.StatusSeeOther {
		body, _ := io.ReadAll(review.Result().Body)
		t.Fatalf("dashboard review status=%d body=%s", review.Code, body)
	}

	recalled, err := hub.Recall(context.Background(), cortex.RecallQuery{
		AgentID: "nua", Text: "canonical output", Project: "novelclaw", Limit: 5,
	})
	if err != nil {
		t.Fatalf("recall approved dashboard memory: %v", err)
	}
	if len(recalled.Items) != 1 || recalled.Items[0].Memory.Lifecycle != cortex.LifecycleActive {
		t.Fatalf("dashboard review did not approve memory: %#v", recalled.Items)
	}

	detailRequest := httptest.NewRequest(http.MethodGet, "/ui/memories/"+memory.ID, nil)
	detailRequest.AddCookie(cookies[0])
	detail := httptest.NewRecorder()
	handler.ServeHTTP(detail, detailRequest)
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), "สรุปก่อนตัดสินใจ") ||
		!strings.Contains(detail.Body.String(), "การใช้งานและการเปลี่ยนแปลง") ||
		!strings.Contains(detail.Body.String(), "รับไปใช้งาน") {
		t.Fatalf("detail status=%d body=%s", detail.Code, detail.Body.String())
	}
}

func TestDashboardConfiguresAndRunsOptionalAdvisor(t *testing.T) {
	t.Parallel()
	hub, err := cortex.Open(cortex.Config{
		DatabasePath: filepath.Join(t.TempDir(), "cortex.db"),
		AdminAgents:  []string{"mika"},
	})
	if err != nil {
		t.Fatalf("open Cortex: %v", err)
	}
	t.Cleanup(func() { _ = hub.Close() })

	var memory cortex.Memory
	for agent, requestID := range map[string]string{"sola": "advisor/sola", "nua": "advisor/nua"} {
		memory, err = hub.Remember(context.Background(), cortex.RememberCommand{
			IdempotencyKey: requestID, Kind: cortex.KindSolution, Scope: cortex.ScopeProject,
			ScopeKey: "novelclaw", MemoryKey: "novelclaw.safe-output",
			Title: "Validate output before replace", Content: "Run the quality gate before replacing output.",
			AgentID: agent, SourceRef: "quality_gate.go",
		})
		if err != nil {
			t.Fatalf("create advisor candidate as %s: %v", agent, err)
		}
	}

	advisor := &fakeDashboardAdvisor{}
	handler := NewWithControlLauncherAndAdvisor(
		hub, StaticAuthenticator{"mika-token": "mika"}, nil, nil, advisor,
	)
	cookie := loginDashboardForTest(t, handler, "mika-token")
	dashboard := requestDashboardForTest(t, handler, cookie)
	csrfToken := dashboardCSRFForTest(t, dashboard)

	settingsForm := url.Values{
		"csrf": {csrfToken}, "enabled": {"yes"},
		"endpoint": {"http://127.0.0.1:20128/v1"}, "model": {"reviewer"},
		"input_token_budget": {"1200"}, "output_token_budget": {"350"}, "effort": {"auto"},
	}
	settingsRequest := httptest.NewRequest(
		http.MethodPost, "/ui/advisor/settings", strings.NewReader(settingsForm.Encode()),
	)
	settingsRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	settingsRequest.AddCookie(cookie)
	settingsResponse := httptest.NewRecorder()
	handler.ServeHTTP(settingsResponse, settingsRequest)
	if settingsResponse.Code != http.StatusSeeOther || advisor.modelCalls != 0 {
		t.Fatalf("advisor settings status=%d model_calls=%d body=%s",
			settingsResponse.Code, advisor.modelCalls, settingsResponse.Body.String())
	}

	runForm := url.Values{"csrf": {csrfToken}}
	runRequest := httptest.NewRequest(http.MethodPost, "/ui/advisor/run", strings.NewReader(runForm.Encode()))
	runRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	runRequest.AddCookie(cookie)
	runResponse := httptest.NewRecorder()
	handler.ServeHTTP(runResponse, runRequest)
	if runResponse.Code != http.StatusOK ||
		!strings.Contains(runResponse.Body.String(), "ควรเปิดดูหลักฐานก่อนรับเป็นความรู้ร่วม") ||
		!strings.Contains(runResponse.Body.String(), memory.Title) {
		t.Fatalf("advisor run status=%d body=%s", runResponse.Code, runResponse.Body.String())
	}
	if advisor.lastInput.Model != "reviewer" || advisor.lastInput.InputTokenBudget != 1200 ||
		len(advisor.lastInput.Suggestions) != 1 {
		t.Fatalf("advisor input = %#v", advisor.lastInput)
	}

	status, err := hub.AdvisorStatus(context.Background(), "mika")
	if err != nil || status.LastRun == nil || status.LastRun.InputTokens != 410 {
		t.Fatalf("advisor status=%#v error=%v", status, err)
	}
	updatedDashboard := requestDashboardForTest(t, handler, cookie)
	if !strings.Contains(updatedDashboard, "สรุปล่าสุด:") ||
		!strings.Contains(updatedDashboard, "ควรเปิดดูหลักฐานก่อนรับเป็นความรู้ร่วม") {
		t.Fatalf("dashboard omitted the latest advisor summary: %s", updatedDashboard)
	}
}

func TestDashboardSavesDisabledAdvisorWithoutEndpoint(t *testing.T) {
	t.Parallel()
	hub, err := cortex.Open(cortex.Config{
		DatabasePath: filepath.Join(t.TempDir(), "cortex.db"),
		AdminAgents:  []string{"mika"},
	})
	if err != nil {
		t.Fatalf("open Cortex: %v", err)
	}
	t.Cleanup(func() { _ = hub.Close() })

	handler := NewWithControlLauncherAndAdvisor(
		hub, StaticAuthenticator{"mika-token": "mika"}, nil, nil, &fakeDashboardAdvisor{},
	)
	cookie := loginDashboardForTest(t, handler, "mika-token")
	csrfToken := dashboardCSRFForTest(t, requestDashboardForTest(t, handler, cookie))

	form := url.Values{
		"csrf": {csrfToken}, "endpoint": {""}, "model": {""},
		"input_token_budget": {"600"}, "output_token_budget": {"200"}, "effort": {"auto"},
	}
	request := httptest.NewRequest(http.MethodPost, "/ui/advisor/settings", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("disabled advisor settings status=%d body=%s", response.Code, response.Body.String())
	}

	status, err := hub.AdvisorStatus(context.Background(), "mika")
	if err != nil {
		t.Fatalf("read advisor status: %v", err)
	}
	if status.Settings.Enabled || status.Settings.Endpoint != "" || status.Settings.Model != "" {
		t.Fatalf("disabled advisor settings = %#v", status.Settings)
	}
}

func loginDashboardForTest(t *testing.T, handler http.Handler, token string) *http.Cookie {
	t.Helper()
	form := url.Values{"token": {token}}
	request := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther || len(response.Result().Cookies()) != 1 {
		t.Fatalf("dashboard login status=%d body=%s", response.Code, response.Body.String())
	}
	return response.Result().Cookies()[0]
}

type dashboardPINAuthenticator struct{}

func (dashboardPINAuthenticator) Authenticate(token string) (string, bool) {
	if token == "agent-token" {
		return "mika", true
	}
	return "", false
}

func (dashboardPINAuthenticator) AuthenticateDashboard(secret string) (string, bool) {
	if secret == "4826" {
		return "mika", true
	}
	return "", false
}

func TestDashboardPINCannotAuthenticateAgentAPI(t *testing.T) {
	t.Parallel()

	hub, err := cortex.Open(cortex.Config{
		DatabasePath: filepath.Join(t.TempDir(), "cortex.db"),
		AdminAgents:  []string{"mika"},
	})
	if err != nil {
		t.Fatalf("open Cortex: %v", err)
	}
	t.Cleanup(func() { _ = hub.Close() })
	handler := New(hub, dashboardPINAuthenticator{})
	cookie := loginDashboardForTest(t, handler, "4826")
	if cookie == nil {
		t.Fatal("dashboard PIN did not create a browser session")
	}

	request := httptest.NewRequest(http.MethodGet, "/v1/capabilities", nil)
	request.Header.Set("Authorization", "Bearer 4826")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("dashboard PIN authenticated API request: status=%d body=%s", response.Code, response.Body.String())
	}
}

func requestDashboardForTest(t *testing.T, handler http.Handler, cookie *http.Cookie) string {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("dashboard status=%d body=%s", response.Code, response.Body.String())
	}
	return response.Body.String()
}

func dashboardCSRFForTest(t *testing.T, body string) string {
	t.Helper()
	match := regexp.MustCompile(`name="csrf" value="([^"]+)"`).FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatalf("dashboard omitted CSRF token: %s", body)
	}
	return match[1]
}
