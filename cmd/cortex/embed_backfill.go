package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"cortex.local/cortex/internal/config"
	"cortex.local/cortex/internal/cortex"
)

// runEmbedBackfill fills the embedding column for any memory row that lacks one.
// It reuses the exact same embedder used at write time (9Router remote when
// configured, offline hashing fallback otherwise), so scores stay consistent.
// Safe to re-run: rows already embedded are skipped.
func runEmbedBackfill(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("embed-backfill", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dataDir := flags.String("data-dir", config.DefaultDataDir(), "Hope HUB data directory")
	limit := flags.Int("limit", 0, "max rows to backfill (0 = all)")
	embedURL := flags.String("embed-url", "", "embeddings endpoint (default 9Router local)")
	embedModel := flags.String("embed-model", "", "embedding model (default free nemotron)")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *embedURL == "" {
		if v := os.Getenv("HOPE_EMBED_URL"); v != "" {
			*embedURL = v
		} else {
			*embedURL = "http://127.0.0.1:20128/v1/embeddings"
		}
	}
	if *embedModel == "" {
		if v := os.Getenv("HOPE_EMBED_MODEL"); v != "" {
			*embedModel = v
		} else {
			*embedModel = "openrouter/nvidia/llama-nemotron-embed-vl-1b-v2:free"
		}
	}
	cortex.SetEmbedEndpoint(*embedURL, *embedModel)
	dbPath := config.DatabasePath(*dataDir)
	db, err := openBackfillDB(dbPath)
	if err != nil {
		fmt.Fprintf(stderr, "open database: %v\n", err)
		return 1
	}
	defer db.Close()

	ctx := context.Background()
	rows, err := db.QueryContext(ctx, `
SELECT m.id, r.title, r.content, r.tags_json, m.current_revision
FROM memories m
JOIN memory_revisions r ON r.memory_id = m.id AND r.revision = m.current_revision
WHERE length(m.embedding) = 0 OR length(m.embedding) != ?
ORDER BY m.updated_at ASC`, cortex.RemoteEmbedDim*8)
	if err != nil {
		fmt.Fprintf(stderr, "query memories: %v\n", err)
		return 1
	}
	type row struct {
		id, title, content, tags string
	}
	var pending []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.title, &r.content, &r.tags, new(int)); err != nil {
			_ = rows.Close()
			fmt.Fprintf(stderr, "scan memory: %v\n", err)
			return 1
		}
		pending = append(pending, r)
	}
	if err := rows.Close(); err != nil {
		fmt.Fprintf(stderr, "close rows: %v\n", err)
		return 1
	}
	if *limit > 0 && *limit < len(pending) {
		pending = pending[:*limit]
	}
	total := len(pending)
	for i, r := range pending {
		vec := cortex.EmbedTextForBackfill(r.title + "\n" + r.content + "\n" + strings.Trim(r.tags, "[]\""))
		blob := cortex.EncodeVectorForBackfill(vec)
		if _, err := db.ExecContext(ctx,
			"UPDATE memories SET embedding = ? WHERE id = ?", blob, r.id); err != nil {
			fmt.Fprintf(stderr, "update %s: %v\n", r.id, err)
			return 1
		}
		fmt.Fprintf(stdout, "\rbackfilled %d/%d", i+1, total)
	}
	fmt.Fprintf(stdout, "\nHope HUB: embedded %d memories\n", total)
	return 0
}

func openBackfillDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}
