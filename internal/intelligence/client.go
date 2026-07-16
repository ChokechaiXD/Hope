package intelligence

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

const maxExternalResponseBytes = 2 << 20

type Client struct {
	httpClient *http.Client
	mu         sync.Mutex
	models     modelCache
}

func NewClient() *Client {
	return &Client{httpClient: &http.Client{Timeout: 12 * time.Second}}
}

func (client *Client) Models(ctx context.Context, rawEndpoint string) ([]Model, error) {
	endpoint, err := ValidateEndpoint(rawEndpoint)
	if err != nil {
		return nil, err
	}
	client.mu.Lock()
	if client.models.Endpoint == endpoint && time.Now().Before(client.models.Expires) {
		models := slices.Clone(client.models.Models)
		client.mu.Unlock()
		return models, nil
	}
	client.mu.Unlock()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("create model request: %w", err)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("list advisor models: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, externalStatusError("list advisor models", response)
	}
	raw, err := readBounded(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read advisor models: %w", err)
	}
	var payload struct {
		Data []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode advisor models: %w", err)
	}
	if len(payload.Data) > 2000 {
		return nil, fmt.Errorf("advisor returned too many models")
	}
	seen := make(map[string]struct{}, len(payload.Data))
	models := make([]Model, 0, len(payload.Data))
	for _, item := range payload.Data {
		id := strings.TrimSpace(item.ID)
		owner := strings.TrimSpace(item.OwnedBy)
		if id == "" || len(id) > 256 || len(owner) > 128 {
			return nil, fmt.Errorf("advisor returned an invalid model entry")
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		models = append(models, Model{ID: id, OwnedBy: owner})
	}
	slices.SortFunc(models, func(left, right Model) int {
		return strings.Compare(strings.ToLower(left.ID), strings.ToLower(right.ID))
	})
	client.mu.Lock()
	client.models = modelCache{Endpoint: endpoint, Models: slices.Clone(models), Expires: time.Now().Add(time.Minute)}
	client.mu.Unlock()
	return models, nil
}

func ValidateEndpoint(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("parse advisor endpoint: %w", err)
	}
	if parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("advisor endpoint must be a plain local HTTP URL")
	}
	hostname := parsed.Hostname()
	address := net.ParseIP(hostname)
	if !strings.EqualFold(hostname, "localhost") && (address == nil || !address.IsLoopback()) {
		return "", fmt.Errorf("advisor endpoint must use localhost or a loopback IP")
	}
	path := strings.TrimRight(parsed.EscapedPath(), "/")
	if path == "" {
		return "", fmt.Errorf("advisor endpoint must include its API base path")
	}
	parsed.Path = path
	parsed.RawPath = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func readBounded(reader io.Reader) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, maxExternalResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxExternalResponseBytes {
		return nil, fmt.Errorf("external response exceeds %d bytes", maxExternalResponseBytes)
	}
	return raw, nil
}

func externalStatusError(operation string, response *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 512))
	message := strings.TrimSpace(string(raw))
	if message == "" {
		return fmt.Errorf("%s returned %s", operation, response.Status)
	}
	return fmt.Errorf("%s returned %s: %s", operation, response.Status, message)
}
