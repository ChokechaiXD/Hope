# Cortex architecture

## Boundary

Cortex is the memory owner. Hermes, future agents, dashboards, and importers are
clients of a stable protocol. The core never imports an agent framework.

```text
Hermes / future agents
        │ connector
        ▼
HTTP v1 + token identity
        │
        ▼
Remember · Recall · Feedback · Review · History
        │
        ▼
SQLite WAL + FTS5 + append-only events
```

## Module map

```text
cmd/cortex/                      CLI composition and process lifecycle
connectors/hermes/               embedded, replaceable Hermes adapter
internal/autostart/              Windows user-level startup adapter
internal/config/                 local config and hashed agent credentials
internal/controlcenter/          serialized local runtime and connector controls
internal/cortex/                 memory domain and deep-module interface
internal/hermes/                 connector discovery, install, activation
internal/httpapi/                HTTP translation and management dashboard
internal/importer/holographic/   read-only legacy adapter
internal/launcher/               validated Windows dashboard opener
internal/localauth/              HMAC launcher proof and one-time UI codes
docs/                            product and architecture contracts
```

Each operation has its own focused implementation file. `service.go` only
composes the hub; it does not accumulate endpoint, SQL, and connector logic.
CLI commands are split by identity, integration, process, and autostart seams;
`main.go` contains dispatch and usage only.

## Storage model

- `memories` holds identity, scope, lifecycle, current revision, and scores.
- `memory_revisions` holds immutable content revisions.
- `memory_events` is the append-only usage and governance ledger.
- `memory_fts` indexes only current content for keyword recall.
- `recalls` and `recall_items` make tracked recalls durable and idempotent.
- `requests` maps idempotency keys to resources.
- `PRAGMA user_version` drives ordered schema migrations.

The database is opened with foreign keys, WAL, one writer connection, and a
busy timeout. A binary refuses to open a database whose schema version is newer
than it understands.

## Invariants

1. New and imported memories are candidates.
2. Candidates do not enter ordinary cross-agent recall.
3. Only configured governors can change lifecycle state.
4. Private memory is visible only to its owner and governors.
5. Caller-supplied `agent_id` never overrides authenticated identity.
6. Truth evidence and utility evidence update different scores.
7. Failed attempts remain useful warnings; failure content is not low utility
   by definition.
8. Mutating operations are idempotent.
9. Stable memory keys append revisions; prior content is never overwritten.
10. Project and domain memory require an exact recall scope.
11. Browser sessions cannot authenticate HTTP API requests.
12. Legacy databases are imported read-only and remain untouched.
13. Recall token budgets are enforced before recall items and usage events are
    persisted; omitted context cannot influence learning scores.
14. The local launcher never transmits its long-lived key; signed proofs and UI
    codes expire after 30 seconds and cannot be replayed.

## Evolution points

Add capabilities behind the existing operations before adding new public
surface area:

- Semantic search can become an optional recall scorer beside FTS5.
- Session extraction can become an optional connector/extractor adapter.
- Project membership and richer ACLs can extend visibility checks.
- Procedures that repeatedly succeed can produce reviewable skill proposals.
- Other frameworks install separate connectors against HTTP v1.

The initial implementation intentionally omits automatic raw-turn mirroring,
automatic skill mutation, vector databases, and distributed coordination. Add
them only when measured usage shows the simpler path is insufficient.
