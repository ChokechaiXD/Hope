# Cortex model advisor

The model advisor is an optional second opinion for the human governor. It is
not a memory extractor, evidence source, or lifecycle authority.

## No-code flow

1. Open the Cortex dashboard.
2. Expand **Model and token settings** in the second-review panel.
3. Keep the default loopback endpoint or enter another local OpenAI-compatible
   API base path.
4. Press **Load current models**, select one, set budgets, and save.
5. Press **Ask the model to summarize now** only when a second opinion is useful.

The model catalog is fetched on demand and cached in memory for one minute. It
is never copied into Cortex configuration, so model/provider changes in 9Router
do not require a Cortex upgrade.

## Trust boundary

- Only `http://localhost/...` and loopback IP endpoints are accepted.
- Cortex stores no router API key.
- The advisor receives at most 12 Curator suggestions, not full sessions.
- Memory text is marked as untrusted data in the system instruction.
- Input and output budgets are validated before a request.
- Responses are capped at 2 MiB and parsed into a strict JSON shape.
- Invented memory IDs, duplicate assessments, unsupported verdicts, and unknown
  response fields are rejected.
- The result is advice only. It cannot call tools or change lifecycle state.

9Router can still route a request to an external provider if the user configured
that behavior. Cortex itself connects only to the loopback router endpoint.

## Cost and effort

The advisor is manual-only. Normal capture, recall, scoring, and Curator runs
spend no model tokens. `auto` effort resolves from the number of selected items:

- 1-3 items: low
- 4-8 items: medium
- 9-12 items: high

The resolved level is contextual guidance, not a provider-specific API field,
so the same adapter remains compatible with heterogeneous models behind
9Router. Actual or estimated input/output usage is stored with every run and
shown on the dashboard.
