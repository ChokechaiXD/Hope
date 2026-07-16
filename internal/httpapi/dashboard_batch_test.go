package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"cortex.local/cortex/internal/cortex"
)

func TestDashboardBulkReviewsSelectedCandidates(t *testing.T) {
	t.Parallel()

	hub, err := cortex.Open(cortex.Config{
		DatabasePath: filepath.Join(t.TempDir(), "cortex.db"), AdminAgents: []string{"mika"},
	})
	if err != nil {
		t.Fatalf("open Cortex: %v", err)
	}
	t.Cleanup(func() { _ = hub.Close() })
	memories := make([]cortex.Memory, 0, 3)
	for index := 0; index < 3; index++ {
		memory, err := hub.Remember(context.Background(), cortex.RememberCommand{
			IdempotencyKey: fmt.Sprintf("dashboard/bulk/create/%d", index), Kind: cortex.KindFact,
			Scope: cortex.ScopeProject, ScopeKey: "cortex", MemoryKey: fmt.Sprintf("bulk.%d", index),
			Title: fmt.Sprintf("Imported candidate %d", index), Content: "Review this imported fact.",
			AgentID: "sora", SourceRef: "holographic:test",
		})
		if err != nil {
			t.Fatalf("create candidate: %v", err)
		}
		memories = append(memories, memory)
	}
	handler := New(hub, StaticAuthenticator{"mika-token": "mika"})
	cookie := loginDashboardForTest(t, handler, "mika-token")
	dashboard := requestDashboardPathForTest(t, handler, cookie, "/?lifecycle=candidate")
	for _, expected := range []string{
		"กล่องตรวจความรู้เก่า", "ควรตรวจแยก", "จัดการหลายรายการ",
		"เลือกเฉพาะรายการที่ตรวจแล้ว", `value="` + memories[0].ID + `"`,
	} {
		if !strings.Contains(dashboard, expected) {
			t.Fatalf("bulk review UI omitted %q: %s", expected, dashboard)
		}
	}
	groupedDashboard := requestDashboardPathForTest(t, handler, cookie, "/?candidate_group=review")
	if !strings.Contains(groupedDashboard, "candidate-group-active") ||
		!strings.Contains(groupedDashboard, memories[2].Title) {
		t.Fatalf("candidate group filter was not active: %s", groupedDashboard)
	}
	csrf := dashboardCSRFForTest(t, dashboard)
	unsafeForm := url.Values{
		"memory_id": {memories[0].ID}, "decision": {"approve"}, "confirm": {"yes"},
	}
	unsafeRequest := httptest.NewRequest(http.MethodPost, "/ui/memories/review-batch", strings.NewReader(unsafeForm.Encode()))
	unsafeRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	unsafeRequest.AddCookie(cookie)
	unsafeResponse := httptest.NewRecorder()
	handler.ServeHTTP(unsafeResponse, unsafeRequest)
	if unsafeResponse.Code != http.StatusForbidden {
		t.Fatalf("batch without CSRF status=%d", unsafeResponse.Code)
	}

	unconfirmedForm := url.Values{
		"csrf": {csrf}, "memory_id": {memories[0].ID}, "decision": {"approve"},
	}
	unconfirmedRequest := httptest.NewRequest(http.MethodPost, "/ui/memories/review-batch", strings.NewReader(unconfirmedForm.Encode()))
	unconfirmedRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	unconfirmedRequest.AddCookie(cookie)
	unconfirmedResponse := httptest.NewRecorder()
	handler.ServeHTTP(unconfirmedResponse, unconfirmedRequest)
	if unconfirmedResponse.Code != http.StatusBadRequest {
		t.Fatalf("unconfirmed batch status=%d", unconfirmedResponse.Code)
	}

	form := url.Values{
		"csrf": {csrf}, "memory_id": {memories[0].ID, memories[1].ID},
		"decision": {"approve"}, "reason": {"Reviewed as one imported group"}, "confirm": {"yes"},
	}
	request := httptest.NewRequest(http.MethodPost, "/ui/memories/review-batch", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("bulk review status=%d body=%s", response.Code, response.Body.String())
	}

	browsed, err := hub.Browse(context.Background(), cortex.BrowseQuery{AgentID: "mika", Limit: 10})
	if err != nil {
		t.Fatalf("browse reviewed memories: %v", err)
	}
	lifecycles := make(map[string]cortex.Lifecycle, len(browsed.Memories))
	for _, memory := range browsed.Memories {
		lifecycles[memory.ID] = memory.Lifecycle
	}
	if lifecycles[memories[0].ID] != cortex.LifecycleActive ||
		lifecycles[memories[1].ID] != cortex.LifecycleActive ||
		lifecycles[memories[2].ID] != cortex.LifecycleCandidate {
		t.Fatalf("bulk review changed the wrong records: %#v", lifecycles)
	}
}

func requestDashboardPathForTest(
	t *testing.T,
	handler http.Handler,
	cookie *http.Cookie,
	path string,
) string {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("dashboard status=%d body=%s", response.Code, response.Body.String())
	}
	return response.Body.String()
}
