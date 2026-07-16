# Cortex

Cortex is a standalone, local-first shared memory hub for AI agents. It keeps
memory records, scores, usage events, reviews, and connector credentials under
the user's control. Agent frameworks are adapters; they never own the database.

## What v0.3 provides

- One Go executable and one SQLite database
- WAL mode, FTS5 search, versioned schema migrations
- Global, project, domain, and private scopes
- Candidate → active → canonical review lifecycle
- Separate truth and utility scores
- Idempotent writes and recalls
- Stable memory keys with immutable revisions instead of duplicate records
- Append-only audit events
- Bearer-token identity with SHA-256 hashes at rest
- No-code local dashboard with opaque sessions, CSRF protection, runtime controls,
  one-click Hermes agent discovery, and per-memory history
- Memory Explorer with FTS5 search and lifecycle, kind, scope, project/domain,
  and creator filters
- Token-budgeted recall before usage is recorded, so trimmed memories do not
  distort feedback or learning scores
- User-level Windows autostart with no administrator permission required
- Start Menu shortcut that starts Cortex only when needed and signs into the
  dashboard with a short-lived, one-time local code
- Embedded Hermes connector installer
- Read-only Holographic importer; imported facts stay candidates
- Deterministic Curator with manual, assisted, and guarded automatic modes
- Durable repeated-observation evidence tied to the current memory revision
- Optional, manual second-opinion summaries through a loopback 9Router or other
  OpenAI-compatible endpoint, with live model discovery and token budgets

No cloud account, LLM, embedding model, Redis, PostgreSQL, Docker, or Node.js
runtime is required. The optional model advisor is disabled by default and is
never required for capture, recall, scoring, review, or automatic curation.

## Build

```powershell
go build -trimpath -o bin\cortex.exe .\cmd\cortex
```

Go 1.26 or newer is required to build. The resulting executable carries the
dashboard and Hermes connector assets.

## First run

```powershell
bin\cortex.exe init
bin\cortex.exe connector sync hermes --home "$env:LOCALAPPDATA\hermes"
bin\cortex.exe service install
bin\cortex.exe service start
```

`init` prints the initial administrator token once. Keep it; Cortex stores only
its SHA-256 hash. The default data directory is `%LOCALAPPDATA%\Cortex`, and the
default listener is `127.0.0.1:7777`.

After the one-time setup, open **Cortex Dashboard** from the Windows Start Menu.
It reuses the configured loopback port, starts Cortex only when health checks say
it is down, and opens the browser already signed in. The launcher proves its
identity with a replay-protected HMAC request; Cortex exchanges a 30-second,
single-use code for an opaque browser session. Agent bearer tokens never enter
the URL, browser storage, or launcher logs. The manual token form remains an
emergency fallback.

`Start Cortex.bat` in the repository provides the same one-click flow for users
who prefer a batch file. It calls the installed launcher, reuses the configured
port, and exits immediately.

Daily work is dashboard-only: search and review memories, restart or stop Cortex,
and press **Discover & connect agents** whenever a new Hermes profile appears.
The connector action creates a timestamped rollback snapshot before changing any
profile. No terminal is needed for these operations.

The dashboard also exposes **Cortex Curator**. Its free deterministic gate can
organize review work after a configurable number of durable memory events. In
automatic mode it can approve only sourced project/domain candidates confirmed
by distinct agents. Global/private data, preferences, project state, imported
records, and canonical rules always remain human decisions.

If a second opinion is useful, enable the model advisor in the same dashboard,
load the current model list from `http://127.0.0.1:20128/v1`, choose a model, and
set input/output budgets. Cortex contacts only the loopback endpoint and stores
no model catalog or API key. The router may still use whichever local or remote
provider the user configured behind it. Model output is recorded as advice and
cannot approve, edit, or supersede memory.

Issue a fresh dashboard token without opening any connector config:

```powershell
bin\cortex.exe agent token --id mika
```

For a simpler direct-browser fallback, set a 4–8 digit dashboard-only PIN. It
is hashed at rest and cannot authenticate agent API calls:

```powershell
bin\cortex.exe dashboard pin --value 4826
```

Existing valid profile tokens are reused. New profiles receive isolated tokens
and the same standalone Cortex endpoint. A running Cortex server reloads new
regular-agent credentials automatically; restart only when granting a new
governor role. Connector sync restores every affected profile automatically if
any profile fails.

## Import Holographic memory

Stop writing to the old provider first, then import each database with its
owner and optional project scope:

```powershell
bin\cortex.exe import holographic `
  --database "$env:LOCALAPPDATA\hermes\memory_store.db" `
  --agent mika
```

The importer opens the legacy database with SQLite `mode=ro`, preserves legacy
trust and usage as score/provenance metadata, and never promotes imported facts
automatically. Repeating an unchanged import is idempotent.

## HTTP protocol

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/v1/health` | Process health |
| `POST` | `/v1/dashboard/sessions` | Loopback launcher requests a one-time UI session |
| `GET` | `/v1/capabilities` | Protocol features |
| `POST` | `/v1/memories` | Create or revise candidate memory |
| `POST` | `/v1/recalls` | Search visible reviewed memory |
| `POST` | `/v1/memories/{id}/feedback` | Update truth or utility evidence |
| `POST` | `/v1/memories/{id}/review` | Governor lifecycle decision |
| `GET` | `/v1/memories/{id}/history` | Read append-only audit history |

Mutation requests and tracked recalls require an `Idempotency-Key` header.
Agent identity always comes from the bearer token, never from JSON supplied by
the caller.

## Development

```powershell
go test ./...
python -m unittest discover -s connectors\hermes\tests -p "test_*.py"
go vet ./...
```

See [Architecture](docs/architecture.md), [core specification](docs/spec.md),
[Curator](docs/curator.md), and [Model advisor](docs/model-advisor.md) for module
boundaries and invariants. See [Operations](docs/operations.md) for live status,
agent onboarding, upgrades, and rollback.
