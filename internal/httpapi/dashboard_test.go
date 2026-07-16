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
)

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
	for _, expected := range []string{"คลังความรู้", "รอตรวจ", "ข้อตัดสินใจ", "โปรเจกต์ · novelclaw", "รายละเอียดขั้นสูง"} {
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
	if strings.Contains(filteredBody, other.Title) {
		t.Fatalf("filtered dashboard included unrelated memory: %s", filteredBody)
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
