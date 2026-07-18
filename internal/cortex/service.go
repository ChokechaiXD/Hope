package cortex

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"slices"
)

type Config struct {
	DatabasePath  string
	AdminAgents   []string
	EmbedEndpoint string
	EmbedModel    string
}

type Hub struct {
	db          *sql.DB
	adminAgents []string
}

func Open(config Config) (*Hub, error) {
	if config.DatabasePath == "" {
		return nil, fmt.Errorf("%w: database path is required", ErrInvalidInput)
	}
	db, err := openDatabase(context.Background(), config.DatabasePath)
	if err != nil {
		return nil, err
	}
	admins := slices.Clone(config.AdminAgents)
	if len(admins) == 0 {
		admins = []string{"mika"}
	}
	// Wire the semantic embedder. Default to the local 9Router free embedding
	// model; override via config or env. Empty url disables remote embedding
	// and the offline fallback takes over.
	embedURL := config.EmbedEndpoint
	embedModel := config.EmbedModel
	if embedURL == "" {
		if v := os.Getenv("HOPE_EMBED_URL"); v != "" {
			embedURL = v
		} else {
			embedURL = "http://127.0.0.1:20128/v1/embeddings"
		}
	}
	if embedModel == "" {
		if v := os.Getenv("HOPE_EMBED_MODEL"); v != "" {
			embedModel = v
		} else {
			embedModel = "openrouter/nvidia/llama-nemotron-embed-vl-1b-v2:free"
		}
	}
	SetEmbedEndpoint(embedURL, embedModel)
	return &Hub{db: db, adminAgents: admins}, nil
}

func (hub *Hub) Close() error {
	return hub.db.Close()
}

func (hub *Hub) isAdmin(agentID string) bool {
	return slices.Contains(hub.adminAgents, agentID)
}

func (hub *Hub) CanGovern(agentID string) bool {
	return hub.isAdmin(agentID)
}
