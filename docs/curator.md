# Cortex Curator

Curator reduces review work without turning an LLM into the source of truth.
Its first gate is deterministic, local, and free of API or token cost.

## Modes

- `manual`: analyze only when requested; never change lifecycle automatically.
- `assisted`: analyze after a configurable number of new memory events and
  prepare a human-readable queue; never approve automatically.
- `automatic`: run on the same threshold and approve only candidates that pass
  every hard rule below.

The threshold counts created, revised, and repeated-observation events rather
than chat turns. This avoids spending work on conversations that produced no
durable knowledge.

## Hard rules

Automatic approval requires all of the following:

1. The memory is still a candidate.
2. Its scope is `project` or `domain`.
3. It is not a preference or mutable project-state snapshot.
4. It was not imported from a legacy provider.
5. It has a source reference such as a file, commit, or document.
6. It has no contradiction evidence and its truth score is not degraded.
7. The configured number of distinct agents have independently observed or
   confirmed the current revision.

Curator never promotes a memory to `canonical`. Global, private, preference,
project-state, and imported memories always remain human decisions. These are
code-level invariants, not prompt instructions.

## Revisable rules

Canonical does not mean permanent. Feedback continues to change truth and
utility scores. Curator flags active or canonical memories when contradiction
evidence appears, truth falls to `0.30` or lower, or utility falls to `0.20` or
lower. It recommends superseding or archiving them but never performs that
destructive governance step automatically.

## Storage and audit

Settings and run summaries live in SQLite beside memory data:

- `curator_settings` is a single validated local policy record.
- `curator_runs` records trigger, mode, analyzed count, recommendations,
  applied approvals, and bounded error text.
- Normal review events remain the authoritative lifecycle history.

Suggestions are derived from current memory and event history rather than
stored as a second competing source of truth.

## Optional model gate

The implemented model advisor can summarize or challenge deterministic
suggestions after the user explicitly requests it. It sits behind a replaceable
adapter and can never bypass hard rules. The default remains disabled, and
Cortex remains fully usable without the adapter, 9Router, an embedding model,
or network access. See [Model advisor](model-advisor.md).
