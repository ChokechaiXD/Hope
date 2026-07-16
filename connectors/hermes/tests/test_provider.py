from __future__ import annotations

import json
import sys
import types
import unittest
from pathlib import Path


agent_module = types.ModuleType("agent")
memory_provider_module = types.ModuleType("agent.memory_provider")
memory_provider_module.MemoryProvider = object
tools_module = types.ModuleType("tools")
registry_module = types.ModuleType("tools.registry")
registry_module.tool_error = lambda message: json.dumps({"error": message})
sys.modules.setdefault("agent", agent_module)
sys.modules.setdefault("agent.memory_provider", memory_provider_module)
sys.modules.setdefault("tools", tools_module)
sys.modules.setdefault("tools.registry", registry_module)
sys.path.insert(0, str(Path(__file__).parents[1]))

from provider import CortexError, CortexMemoryProvider, RECALL_SCHEMA  # noqa: E402


class FakeClient:
    def __init__(self):
        self.payloads: list[dict] = []

    def recall(self, payload: dict) -> dict:
        self.payloads.append(payload)
        return {
            "id": "rec_1",
            "items": [
                {
                    "memory": {
                        "id": "mem_1",
                        "kind": "fact",
                        "content": "Use the canonical output.",
                        "truth_score": 0.9,
                        "utility_score": 0.8,
                    }
                }
            ],
            "token_budget": payload["token_budget"],
            "estimated_tokens": 140,
            "truncated": True,
        }


class AutoCaptureClient:
    def __init__(self):
        self.requests: list[tuple[dict, str]] = []

    def remember(self, payload: dict, idempotency_key: str = "") -> dict:
        self.requests.append((payload, idempotency_key))
        return {"id": "mem_auto", "lifecycle": "candidate"}


class RecoveringAutoCaptureClient(AutoCaptureClient):
    def __init__(self):
        super().__init__()
        self.failures_remaining = 1

    def remember(self, payload: dict, idempotency_key: str = "") -> dict:
        if self.failures_remaining:
            self.failures_remaining -= 1
            raise CortexError("temporary restart")
        return super().remember(payload, idempotency_key)


class CortexProviderBudgetTest(unittest.TestCase):
    def test_prefetch_and_tool_recall_send_bounded_token_budgets(self):
        provider = CortexMemoryProvider(
            {"prefetch_token_budget": 700, "recall_token_budget": 1200}
        )
        client = FakeClient()
        provider._client = client

        rendered = provider.prefetch("canonical output")
        self.assertEqual(client.payloads[0]["token_budget"], 700)
        self.assertIn("140/700 tokens", rendered)
        self.assertIn("trimmed", rendered)

        response = json.loads(
            provider.handle_tool_call(
                "cortex_recall", {"query": "canonical output", "token_budget": 240}
            )
        )
        self.assertEqual(client.payloads[1]["token_budget"], 240)
        self.assertEqual(response["token_budget"], 240)

    def test_recall_schema_exposes_safe_budget_range(self):
        properties = RECALL_SCHEMA["parameters"]["properties"]
        self.assertEqual(properties["token_budget"]["minimum"], 100)
        self.assertEqual(properties["token_budget"]["maximum"], 4000)


class CortexProviderAutoCaptureTest(unittest.TestCase):
    def _provider(self, **config) -> tuple[CortexMemoryProvider, AutoCaptureClient]:
        provider = CortexMemoryProvider(
            {
                "agent_id": "sora",
                "default_project": "cortex",
                "auto_capture_enabled": True,
                "auto_capture_every_turns": 3,
                **config,
            }
        )
        client = AutoCaptureClient()
        provider._client = client
        provider._agent_id = "sora"
        provider._session_id = "session-1"
        return provider, client

    def test_auto_capture_waits_for_interval_and_stores_only_marked_lesson(self):
        provider, client = self._provider()

        provider.sync_turn("question", "Lesson to remember: Prefer FTS5 first.")
        provider.sync_turn("question", "Routine answer without a durable lesson.")
        self.assertEqual(client.requests, [])

        provider.sync_turn("question", "Third routine answer.")

        self.assertEqual(len(client.requests), 1)
        payload, idempotency_key = client.requests[0]
        self.assertEqual(payload["content"], "Prefer FTS5 first.")
        self.assertEqual(payload["scope"], "project")
        self.assertEqual(payload["scope_key"], "cortex")
        self.assertTrue(idempotency_key.startswith("hermes/auto/sora/solution/"))

    def test_session_end_flushes_pending_lesson(self):
        provider, client = self._provider(auto_capture_every_turns=5)
        provider.sync_turn("question", "Decision to remember: Keep Cortex standalone.")

        provider.on_session_end([])

        self.assertEqual(len(client.requests), 1)
        self.assertEqual(client.requests[0][0]["kind"], "decision")

    def test_rewind_discards_pending_lesson(self):
        provider, client = self._provider(auto_capture_every_turns=5)
        provider.sync_turn("question", "Working solution: This answer will be rewound.")

        provider.on_session_switch("session-2", rewound=True)
        provider.on_session_end([])

        self.assertEqual(client.requests, [])

    def test_auto_capture_can_be_disabled(self):
        provider, client = self._provider(auto_capture_enabled=False, auto_capture_every_turns=1)

        provider.sync_turn("question", "Lesson to remember: Do not store this.")
        provider.on_session_end([])

        self.assertEqual(client.requests, [])

    def test_temporary_failure_is_retried_on_next_interval(self):
        provider, _ = self._provider(auto_capture_every_turns=1)
        client = RecoveringAutoCaptureClient()
        provider._client = client

        provider.sync_turn("question", "Lesson to remember: Retry after restart.")
        self.assertEqual(client.requests, [])

        provider.sync_turn("question", "Routine answer.")

        self.assertEqual(len(client.requests), 1)
        self.assertEqual(client.requests[0][0]["content"], "Retry after restart.")

    def test_system_prompt_explains_the_visible_capture_gate(self):
        provider, _ = self._provider()

        prompt = provider.system_prompt_block()

        self.assertIn("บทเรียนที่ควรจำ:", prompt)
        self.assertIn("Do not add it on routine turns", prompt)

    def test_auto_capture_uses_configured_domain_without_project(self):
        provider, client = self._provider(
            default_project="", default_domain="coding", auto_capture_every_turns=1
        )

        provider.sync_turn("question", "Lesson to remember: Prefer deterministic tests.")

        self.assertEqual(client.requests[0][0]["scope"], "domain")
        self.assertEqual(client.requests[0][0]["scope_key"], "coding")


if __name__ == "__main__":
    unittest.main()
