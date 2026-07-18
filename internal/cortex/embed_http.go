package cortex

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"
)

// EmbedEndpoint is the 9Router (or OpenAI-compatible) embeddings endpoint.
// Configured at runtime via SetEmbedEndpoint; empty means "use the offline
// fallback only".
var (
	embedEndpointURL   string
	embedEndpointModel string
	embedHTTPClient    = &http.Client{Timeout: 20 * time.Second}
	embedCache         sync.Map // text -> []float64 (small, in-memory; backfill is one-shot)
)

// SetEmbedEndpoint enables remote semantic embeddings via a 9Router-compatible
// /v1/embeddings endpoint. Pass model "openrouter/nvidia/llama-nemotron-embed-vl-1b-v2:free"
// for the free tier. An empty url disables remote embedding (offline fallback only).
func SetEmbedEndpoint(url, model string) {
	embedEndpointURL = url
	embedEndpointModel = model
}

// EmbedDim matches the remote model's dimension. If the remote model is
// unavailable we fall back to the offline hashing embedder, which uses its own
// fixed dimension; callers must compare lengths before blending.
const RemoteEmbedDim = 2048

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// embedRemote calls the configured embeddings endpoint for one text. Returns
// nil (no error) when remote embedding is disabled or fails, so the caller can
// transparently fall back to the offline embedder.
func embedRemote(text string) []float64 {
	if embedEndpointURL == "" {
		return nil
	}
	if cached, ok := embedCache.Load(text); ok {
		return cached.([]float64)
	}
	body, err := json.Marshal(embedRequest{Model: embedEndpointModel, Input: []string{text}})
	if err != nil {
		return nil
	}
	req, err := http.NewRequest(http.MethodPost, embedEndpointURL, bytes.NewReader(body))
	if err != nil {
		return nil
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := embedHTTPClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}
	var parsed embedResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil
	}
	if parsed.Error != nil {
		return nil
	}
	if len(parsed.Data) == 0 || len(parsed.Data[0].Embedding) == 0 {
		return nil
	}
	vec := parsed.Data[0].Embedding
	embedCache.Store(text, vec)
	return vec
}

// embedText returns a semantic vector for text. It prefers the remote embedder
// (real semantic similarity) and falls back to the offline hashing embedder so
// the system keeps working if 9Router is down. The two have different dimensions;
// recall compares only when lengths match.
func embedText(text string) []float64 {
	if vec := embedRemote(text); vec != nil {
		return vec
	}
	return embedOffline(text)
}
