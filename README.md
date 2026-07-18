# Hope HUB

Hope HUB is a standalone, local-first shared memory system for P Choke's agents.
It keeps the useful Cortex memory kernel and keeps Skill Mem so agents can load
relevant skills, but it no longer tries to be an operating hub for launching
9Router, Hermes gateways, Telegram, work modes, projects, or automations.

## What it owns

- `hope_mem.db`: memories, revisions, recall events, feedback, review lifecycle,
  curator settings, and model-review notes
- `hope_skill.db`: Skill Mem only — skill metadata, deterministic skill routing,
  context-pack tracking, and skill success/failure feedback
- A local dashboard for reviewing knowledge, running the curator, configuring
  optional model review, and setting agent memory budgets
- A Hermes connector so existing agents can use the same memory source

The executable name remains `cortex.exe` for compatibility with existing
shortcuts, service entries, and connector scripts. Product text and the browser
experience are branded as Hope HUB.

## Semantic search

Recall is semantic, not just keyword. Each memory is embedded when written and
the embedding is stored alongside the row. At search time the query is embedded
and cosine similarity blends with the lexical (FTS5) score so a memory that
matches by *meaning* (paraphrase, synonym) surfaces even when the keywords
differ.

The embedder is offline-first:

- **Primary:** the local 9Router `/v1/embeddings` endpoint with the free
  `openrouter/nvidia/llama-nemotron-embed-vl-1b-v2:free` model (2048-dim, no
  cost, no external network — 9Router runs on this machine).
- **Fallback:** a dependency-free hashing embedder (feature hashing of unigrams
  and bigrams) so search still works if 9Router is down. The two have different
  dimensions; recall only blends when lengths match.

Override the endpoint/model via env vars `HOPE_EMBED_URL` / `HOPE_EMBED_MODEL`
or the `cortex embed-backfill --embed-url --embed-model` flags.

## What it does not own

- No inference loop
- No Telegram bot runtime
- No 9Router or model-provider lifecycle
- No Hermes gateway launcher
- No project/workspace launcher
- No cron/automation scheduler

Those systems can keep running independently. Hope HUB only provides memory and
skill context to agents.

## Core behavior

- Agent-written memories start as `candidate`
- Reviewed memories become `active`
- Stable rules can be promoted to `canonical`
- Rejected, superseded, and archived memories remain auditable
- Truth and utility scores are tracked separately
- Failed attempts are useful warnings, not bad memories
- All writes and feedback use idempotency keys
- No cloud service, LLM, embedding model, Redis, PostgreSQL, Docker, or Node.js
  runtime is required (9Router is local and optional)

Optional model review can use a loopback OpenAI-compatible endpoint such as
9Router, but it is manual and cannot approve, edit, or replace memory.

## Build

```powershell
go build -trimpath -o bin\cortex.exe .\cmd\cortex
```

Go 1.26 or newer is required.

## First run

Double-click `Install Hope HUB.bat` if present, or run:

```powershell
bin\cortex.exe init
bin\cortex.exe connector sync hermes --home "$env:LOCALAPPDATA\hermes"
bin\cortex.exe service install
bin\cortex.exe service start
bin\cortex.exe open
```

The default data directory is `%LOCALAPPDATA%\HopeHUB`.
The default listener is `127.0.0.1:7777`.

## Embedding backfill

After a fresh init or a schema migration, embed existing memories once:

```powershell
bin\cortex.exe embed-backfill --data-dir "$env:LOCALAPPDATA\HopeHUB"
```

Re-running is safe: already-embedded rows (and rows whose embedding dimension
already matches the active model) are skipped.

## Daily use

Open the dashboard and use it for:

- reviewing candidate memories
- searching shared knowledge (semantic + keyword)
- promoting or superseding rules
- running the curator (mode is `automatic`: only memories that pass the iron
  rules — sourced and independently supported — are auto-approved; the rest wait
  for a human)
- optionally asking a model for a second opinion
- configuring each agent's memory budget
- letting agents receive memory plus Skill Mem recommendations through
  `/v1/context-packs`

## HTTP protocol

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/v1/health` | Process health |
| `POST` | `/v1/dashboard/sessions` | Loopback launcher requests a one-time UI session |
| `GET` | `/v1/capabilities` | Protocol features |
| `POST` | `/v1/memories` | Create or revise candidate memory |
| `POST` | `/v1/recalls` | Search visible reviewed memory (semantic + lexical) |
| `POST` | `/v1/context-packs` | Recall memory plus relevant Skill Mem recommendations |
| `POST` | `/v1/context-packs/{pack}/skills/{skill}/feedback` | Track skill use/success/failure |
| `POST` | `/v1/memories/{id}/feedback` | Update truth or utility evidence |
| `POST` | `/v1/memories/{id}/review` | Governor lifecycle decision |
| `GET` | `/v1/memories/{id}/history` | Read append-only audit history |

Agent identity always comes from the bearer token, never from JSON supplied by
the caller.

## Development

```powershell
go test ./...
python -m unittest discover -s connectors\hermes\tests -p "test_*.py"
go vet ./...
```
