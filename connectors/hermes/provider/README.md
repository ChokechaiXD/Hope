# HOPE Context Bridge for Hermes

This adapter translates Hermes memory lifecycle calls and tools to the Cortex
v1 HTTP protocol. HOPE Mem remains the memory data owner; Hermes continues to
own inference and tool execution.

Install and configure all profiles with:

```text
cortex connector sync hermes --home <HERMES_HOME>
```

The installer writes the provider under `$HERMES_HOME/plugins/cortex/` and a
profile-specific `$HERMES_HOME/cortex.json`.

Raw conversation turns and built-in `MEMORY.md` writes are never mirrored.
Instead, the provider captures only an explicit short lesson block requested by
its system prompt. Marked lessons are queued, deduplicated, and stored as
candidates every five turns or at session end. Without `default_project`, they
stay private to the originating agent. Configure a project to share reviewed
lessons with other agents working in that project.

Before a turn, the provider requests one Context Pack. It contains a
token-budgeted Cortex recall plus up to five HOPE skill metadata recommendations.
Hermes still opens the selected skill body through its native `skill_view` tool.
The existing `cortex_feedback` tool accepts either memory feedback or a
`context_pack_id` + `skill_id` with `used`, `success`, or `failure`. This keeps
skill routing history inspectable without adding another tool schema.

Optional connector settings:

```json
{
  "default_project": "cortex",
  "default_domain": "coding",
  "auto_capture_enabled": true,
  "auto_capture_every_turns": 5,
  "auto_capture_max_chars": 1000,
  "skill_route_limit": 3
}
```
