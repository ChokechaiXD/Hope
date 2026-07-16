from __future__ import annotations

import sys
import unittest
from pathlib import Path


sys.path.insert(0, str(Path(__file__).parents[1] / "provider"))

from extraction import LessonBuffer, build_memory_request, extract_lessons  # noqa: E402


class LessonExtractionTest(unittest.TestCase):
    def test_ignores_unmarked_conversation(self):
        self.assertEqual(
            extract_lessons("The test passed. I also updated the documentation."),
            [],
        )

    def test_extracts_supported_thai_and_english_markers(self):
        text = """Routine answer.

### วิธีที่ไม่ควรทำซ้ำ:
Do not force-write canonical files before checking git status.

### Decision to remember:
Keep Cortex standalone from Hermes.
"""

        proposals = extract_lessons(text)

        self.assertEqual([proposal.kind for proposal in proposals], ["failed_attempt", "decision"])
        self.assertEqual(
            proposals[0].content,
            "Do not force-write canonical files before checking git status.",
        )
        self.assertEqual(proposals[1].content, "Keep Cortex standalone from Hermes.")

    def test_extracts_only_the_marked_block_and_bounds_content(self):
        text = """บทเรียนที่ควรจำ: Use the canonical output.
Keep the source artifact as evidence.

This unrelated explanation must not be stored.
"""

        proposal = extract_lessons(text, max_chars=36)[0]

        self.assertEqual(proposal.kind, "solution")
        self.assertEqual(proposal.content, "Use the canonical output. Keep the")
        self.assertNotIn("unrelated", proposal.content)

    def test_buffer_releases_only_on_configured_turn_or_manual_drain(self):
        buffer = LessonBuffer(every_turns=3)

        self.assertEqual(buffer.observe("Lesson to remember: Prefer FTS5 first."), [])
        self.assertEqual(buffer.observe("No durable lesson."), [])
        released = buffer.observe("Still routine." )

        self.assertEqual([item.content for item in released], ["Prefer FTS5 first."])
        self.assertEqual(buffer.drain(), [])

        self.assertEqual(buffer.observe("Working solution: Reuse port 7777."), [])
        self.assertEqual([item.content for item in buffer.drain()], ["Reuse port 7777."])

    def test_buffer_restores_unsaved_lessons_without_duplicates(self):
        buffer = LessonBuffer(every_turns=1)
        proposals = buffer.observe("Working solution: Retry after restart.")

        buffer.restore(proposals)
        buffer.restore(proposals)

        self.assertEqual(
            [item.content for item in buffer.drain()],
            ["Retry after restart."],
        )

    def test_memory_request_is_private_without_project_and_idempotent(self):
        proposal = extract_lessons("Working solution: Reuse port 7777.")[0]

        first_payload, first_key = build_memory_request(
            proposal,
            agent_id="sora",
            session_id="session-1",
            default_project="",
        )
        second_payload, second_key = build_memory_request(
            proposal,
            agent_id="sora",
            session_id="session-2",
            default_project="",
        )

        self.assertEqual(first_payload["scope"], "private")
        self.assertEqual(first_payload["scope_key"], "sora")
        self.assertEqual(first_payload["tags"], ["auto-captured", "hermes"])
        self.assertEqual(first_payload["memory_key"], second_payload["memory_key"])
        self.assertEqual(first_key, second_key)

    def test_memory_request_uses_project_scope_when_configured(self):
        proposal = extract_lessons("Decision to remember: Use one shared source.")[0]

        payload, _ = build_memory_request(
            proposal,
            agent_id="mika",
            session_id="session-9",
            default_project="cortex",
        )

        self.assertEqual(payload["scope"], "project")
        self.assertEqual(payload["scope_key"], "cortex")
        self.assertEqual(payload["source_ref"], "session:session-9")

    def test_memory_request_uses_domain_when_project_is_not_configured(self):
        proposal = extract_lessons("Lesson to remember: Prefer deterministic tests.")[0]

        payload, _ = build_memory_request(
            proposal,
            agent_id="sora",
            session_id="session-10",
            default_domain="coding",
        )

        self.assertEqual(payload["scope"], "domain")
        self.assertEqual(payload["scope_key"], "coding")


if __name__ == "__main__":
    unittest.main()
