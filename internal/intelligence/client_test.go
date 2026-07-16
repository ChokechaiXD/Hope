package intelligence

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateEndpointAllowsOnlyLoopbackAPIPaths(t *testing.T) {
	t.Parallel()
	for _, endpoint := range []string{
		"http://127.0.0.1:20128/v1",
		"http://localhost:20128/v1/",
		"http://[::1]:20128/v1",
	} {
		if _, err := ValidateEndpoint(endpoint); err != nil {
			t.Errorf("ValidateEndpoint(%q): %v", endpoint, err)
		}
	}
	for _, endpoint := range []string{
		"https://127.0.0.1:20128/v1",
		"http://example.com/v1",
		"http://127.0.0.1:20128",
		"http://user@127.0.0.1:20128/v1",
		"http://127.0.0.1:20128/v1?key=secret",
	} {
		if _, err := ValidateEndpoint(endpoint); err == nil {
			t.Errorf("ValidateEndpoint(%q) succeeded, want rejection", endpoint)
		}
	}
}

func TestClientCachesModelsAndRequestsConstrainedAdvice(t *testing.T) {
	modelCalls := 0
	adviceCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/models":
			modelCalls++
			_ = json.NewEncoder(writer).Encode(map[string]any{"data": []map[string]string{
				{"id": "reviewer", "owned_by": "9router"},
				{"id": "reviewer", "owned_by": "duplicate"},
			}})
		case "/v1/chat/completions":
			adviceCalls++
			var body struct {
				Model     string `json:"model"`
				MaxTokens int    `json:"max_tokens"`
				Messages  []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Errorf("decode request: %v", err)
			}
			if body.Model != "reviewer" || body.MaxTokens != 350 || len(body.Messages) != 2 {
				t.Errorf("advisor request = %#v", body)
			}
			if !strings.Contains(body.Messages[0].Content, "untrusted data") ||
				!strings.Contains(body.Messages[1].Content, "mem_1") {
				t.Errorf("advisor prompts did not preserve the trust boundary: %#v", body.Messages)
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"choices": []map[string]any{{"message": map[string]string{
					"content": "```json\n{\"summary\":\"ควรตรวจแหล่งอ้างอิงก่อน\",\"items\":[{\"memory_id\":\"mem_1\",\"verdict\":\"uncertain\",\"reason\":\"มีเพียงสองแหล่ง\"}]}\n```",
				}}},
				"usage": map[string]int{"prompt_tokens": 321, "completion_tokens": 87},
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client := NewClient()
	endpoint := server.URL + "/v1"
	models, err := client.Models(context.Background(), endpoint)
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	if _, err := client.Models(context.Background(), endpoint); err != nil {
		t.Fatalf("list cached models: %v", err)
	}
	if len(models) != 1 || models[0].ID != "reviewer" || modelCalls != 1 {
		t.Fatalf("models=%#v model_calls=%d", models, modelCalls)
	}

	advice, err := client.Advise(context.Background(), AdviceRequest{
		Endpoint: endpoint, Model: "reviewer", Effort: "auto",
		InputTokenBudget: 1200, OutputTokenBudget: 350,
		Suggestions: []Suggestion{{
			MemoryID: "mem_1", Title: "Output format", Content: "Use canonical JSON.",
			Scope: "project", Kind: "decision", Category: "ready",
			Reason: "two agents agree", Evidence: "source: repository",
		}},
	})
	if err != nil {
		t.Fatalf("request advice: %v", err)
	}
	if adviceCalls != 1 || advice.InputTokens != 321 || advice.OutputTokens != 87 ||
		len(advice.Assessments) != 1 || advice.Assessments[0].MemoryID != "mem_1" {
		t.Fatalf("advice=%#v advice_calls=%d", advice, adviceCalls)
	}
}

func TestParseAdviceRejectsInventedMemoryIDs(t *testing.T) {
	t.Parallel()
	_, err := parseAdvice(
		`{"summary":"review","items":[{"memory_id":"invented","verdict":"support","reason":"none"}]}`,
		map[string]struct{}{"mem_1": {}},
	)
	if err == nil {
		t.Fatal("parseAdvice accepted a memory ID that was never sent")
	}
}
