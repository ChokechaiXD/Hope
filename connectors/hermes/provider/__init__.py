"""Hermes memory-provider adapter for the standalone Cortex service."""

from __future__ import annotations

import json
import logging
import os
from pathlib import Path
from typing import Any, Dict, List

from agent.memory_provider import MemoryProvider
from tools.registry import tool_error

from .client import CortexClient, CortexError, write_private_json
from .extraction import LessonBuffer, LessonProposal, build_memory_request

logger = logging.getLogger(__name__)

CONFIG_NAME = "cortex.json"


REMEMBER_SCHEMA = {
    "name": "cortex_remember",
    "description": "Store a durable lesson in Cortex. New memories are candidates until reviewed.",
    "parameters": {
        "type": "object",
        "properties": {
            "kind": {
                "type": "string",
                "enum": ["fact", "preference", "decision", "failed_attempt", "solution", "project_state"],
            },
            "scope": {"type": "string", "enum": ["global", "project", "domain", "private"]},
            "scope_key": {"type": "string"},
            "memory_key": {"type": "string", "description": "Stable dotted key for deduplication and review."},
            "title": {"type": "string"},
            "content": {"type": "string"},
            "tags": {"type": "array", "items": {"type": "string"}},
            "source_ref": {"type": "string", "description": "Optional file, commit, URL, or artifact reference."},
        },
        "required": ["kind", "scope", "memory_key", "title", "content"],
    },
}

RECALL_SCHEMA = {
    "name": "cortex_recall",
    "description": "Search reviewed shared memory and relevant private memory.",
    "parameters": {
        "type": "object",
        "properties": {
            "query": {"type": "string"},
            "project": {"type": "string"},
            "domain": {"type": "string"},
            "limit": {"type": "integer", "minimum": 1, "maximum": 20},
            "token_budget": {
                "type": "integer",
                "minimum": 100,
                "maximum": 4000,
                "description": "Maximum estimated context tokens returned by Cortex.",
            },
            "include_candidates": {"type": "boolean", "description": "Inspection only; candidates are excluded by default."},
        },
        "required": ["query"],
    },
}

FEEDBACK_SCHEMA = {
    "name": "cortex_feedback",
    "description": "Report whether a recalled memory was true, useful, or harmful.",
    "parameters": {
        "type": "object",
        "properties": {
            "memory_id": {"type": "string"},
            "outcome": {
                "type": "string",
                "enum": ["confirmed", "contradicted", "helpful", "unhelpful", "applied"],
            },
            "reason": {"type": "string"},
        },
        "required": ["memory_id", "outcome"],
    },
}

REVIEW_SCHEMA = {
    "name": "cortex_review",
    "description": "Governor-only lifecycle review for a Cortex memory.",
    "parameters": {
        "type": "object",
        "properties": {
            "memory_id": {"type": "string"},
            "decision": {"type": "string", "enum": ["approve", "promote", "reject", "supersede", "archive"]},
            "reason": {"type": "string"},
        },
        "required": ["memory_id", "decision"],
    },
}


class CortexMemoryProvider(MemoryProvider):
    def __init__(self, config: dict[str, Any] | None = None):
        self._config = config or {}
        self._client: CortexClient | None = None
        self._session_id = ""
        self._agent_id = ""
        self._capture_enabled = _as_bool(self._config.get("auto_capture_enabled"), True)
        self._lesson_buffer = LessonBuffer(
            every_turns=_bounded_int(
                self._config.get("auto_capture_every_turns"), 5, 1, 50
            ),
            max_chars=_bounded_int(
                self._config.get("auto_capture_max_chars"), 1000, 100, 4000
            ),
        )

    @property
    def name(self) -> str:
        return "cortex"

    def is_available(self) -> bool:
        config = self._config or _load_config()
        return bool(config.get("url") and config.get("token"))

    def initialize(self, session_id: str, **kwargs) -> None:
        hermes_home = str(kwargs.get("hermes_home") or "")
        config = dict(self._config or _load_config(hermes_home))
        self._session_id = session_id
        self._agent_id = str(config.get("agent_id") or kwargs.get("agent_identity") or "").strip()
        self._config = config
        self._capture_enabled = _as_bool(config.get("auto_capture_enabled"), True)
        self._lesson_buffer = LessonBuffer(
            every_turns=_bounded_int(config.get("auto_capture_every_turns"), 5, 1, 50),
            max_chars=_bounded_int(config.get("auto_capture_max_chars"), 1000, 100, 4000),
        )
        self._client = CortexClient(
            str(config.get("url") or "http://127.0.0.1:7777"),
            str(config.get("token") or ""),
            timeout=float(config.get("timeout_seconds") or 2.0),
        )

    def get_config_schema(self) -> List[Dict[str, Any]]:
        return [
            {"key": "url", "description": "Cortex server URL", "default": "http://127.0.0.1:7777"},
            {
                "key": "token",
                "description": "Cortex agent token",
                "secret": True,
                "required": True,
                "env_var": "CORTEX_TOKEN",
            },
            {"key": "agent_id", "description": "Stable Cortex agent id", "required": True},
            {
                "key": "prefetch_token_budget",
                "description": "Maximum estimated tokens injected automatically",
                "default": 700,
            },
            {
                "key": "recall_token_budget",
                "description": "Default maximum estimated tokens returned by cortex_recall",
                "default": 1200,
            },
            {
                "key": "default_project",
                "description": "Optional project scope for shared auto-captured lessons",
                "default": "",
            },
            {
                "key": "default_domain",
                "description": "Optional domain used when no project is configured",
                "default": "",
            },
            {
                "key": "auto_capture_enabled",
                "description": "Capture only explicitly marked durable lessons",
                "default": True,
            },
            {
                "key": "auto_capture_every_turns",
                "description": "Store queued lessons every N turns",
                "default": 5,
            },
            {
                "key": "auto_capture_max_chars",
                "description": "Maximum characters stored from one marked lesson",
                "default": 1000,
            },
        ]

    def save_config(self, values: Dict[str, Any], hermes_home: str) -> None:
        path = Path(hermes_home) / CONFIG_NAME
        existing = _read_json(path)
        existing.update(values)
        write_private_json(path, existing)

    def system_prompt_block(self) -> str:
        if not self._client:
            return ""
        return (
            "# Cortex Shared Memory\n"
            "Use cortex_recall before repeating prior project work. Store verified lessons, decisions, "
            "failed attempts, and solutions with cortex_remember. New records are candidates; do not "
            "treat them as shared truth until reviewed. Report outcomes with cortex_feedback. "
            "Only when a durable tested lesson emerges, append at most one short visible section headed "
            "บทเรียนที่ควรจำ:, วิธีที่ไม่ควรทำซ้ำ:, Working solution:, or Decision to remember:. "
            "Do not add it on routine turns. Cortex captures only that marked section as a candidate."
        )

    def prefetch(self, query: str, *, session_id: str = "") -> str:
        if not self._client or not query.strip():
            return ""
        payload = {
            "text": query,
            "project": self._config.get("default_project", ""),
            "domain": self._config.get("default_domain", ""),
            "limit": _bounded_int(self._config.get("prefetch_limit"), 5, 1, 20),
            "token_budget": _bounded_int(
                self._config.get("prefetch_token_budget"), 700, 100, 4000
            ),
        }
        try:
            result = self._client.recall(payload)
        except CortexError as exc:
            logger.debug("Cortex prefetch failed: %s", exc)
            return ""
        return _format_recall(result)

    def sync_turn(self, user_content: str, assistant_content: str, *, session_id: str = "", messages=None) -> None:
        if not self._capture_enabled:
            return None
        self._flush_auto_capture(self._lesson_buffer.observe(assistant_content))
        return None

    def on_session_end(self, messages) -> None:
        if self._capture_enabled:
            self._flush_auto_capture(self._lesson_buffer.drain())

    def on_session_switch(self, new_session_id: str, **kwargs) -> None:
        if self._capture_enabled:
            if kwargs.get("rewound"):
                self._lesson_buffer.drain()
            else:
                self._flush_auto_capture(self._lesson_buffer.drain())
        self._session_id = new_session_id

    def shutdown(self) -> None:
        if self._capture_enabled:
            self._flush_auto_capture(self._lesson_buffer.drain())

    def _flush_auto_capture(self, proposals: list[LessonProposal]) -> None:
        if not self._client:
            return
        for index, proposal in enumerate(proposals):
            payload, idempotency_key = build_memory_request(
                proposal,
                agent_id=self._agent_id,
                session_id=self._session_id,
                default_project=str(self._config.get("default_project") or ""),
                default_domain=str(self._config.get("default_domain") or ""),
            )
            try:
                self._client.remember(payload, idempotency_key)
            except CortexError as exc:
                logger.warning("Cortex auto-capture failed: %s", exc)
                self._lesson_buffer.restore(proposals[index:])
                break

    def get_tool_schemas(self) -> List[Dict[str, Any]]:
        return [REMEMBER_SCHEMA, RECALL_SCHEMA, FEEDBACK_SCHEMA, REVIEW_SCHEMA]

    def handle_tool_call(self, tool_name: str, args: Dict[str, Any], **kwargs) -> str:
        if not self._client:
            return tool_error("Cortex is not initialized")
        try:
            if tool_name == "cortex_remember":
                payload = dict(args)
                payload["session_id"] = self._session_id
                payload.setdefault("scope_key", "")
                if payload.get("scope") == "private" and not payload["scope_key"]:
                    payload["scope_key"] = self._agent_id
                return json.dumps(self._client.remember(payload), ensure_ascii=False)
            if tool_name == "cortex_recall":
                payload = {
                    "text": str(args.get("query") or ""),
                    "project": str(args.get("project") or self._config.get("default_project") or ""),
                    "domain": str(args.get("domain") or self._config.get("default_domain") or ""),
                    "limit": _bounded_int(args.get("limit"), 5, 1, 20),
                    "token_budget": _bounded_int(
                        args.get("token_budget", self._config.get("recall_token_budget")),
                        1200,
                        100,
                        4000,
                    ),
                    "include_candidates": bool(args.get("include_candidates", False)),
                    "session_id": self._session_id,
                }
                return json.dumps(self._client.recall(payload), ensure_ascii=False)
            if tool_name == "cortex_feedback":
                memory_id = str(args.get("memory_id") or "").strip()
                payload = {"outcome": args.get("outcome"), "reason": args.get("reason", ""), "session_id": self._session_id}
                return json.dumps(self._client.feedback(memory_id, payload), ensure_ascii=False)
            if tool_name == "cortex_review":
                memory_id = str(args.get("memory_id") or "").strip()
                payload = {"decision": args.get("decision"), "reason": args.get("reason", "")}
                return json.dumps(self._client.review(memory_id, payload), ensure_ascii=False)
        except (CortexError, ValueError, TypeError) as exc:
            return tool_error(str(exc))
        return tool_error(f"Unknown tool: {tool_name}")


def _load_config(hermes_home: str = "") -> dict[str, Any]:
    config: dict[str, Any] = {}
    if not hermes_home:
        try:
            from hermes_constants import get_hermes_home

            hermes_home = str(get_hermes_home())
        except Exception:
            hermes_home = ""
    if hermes_home:
        config.update(_read_json(Path(hermes_home) / CONFIG_NAME))
    if os.environ.get("CORTEX_URL"):
        config["url"] = os.environ["CORTEX_URL"]
    if os.environ.get("CORTEX_TOKEN"):
        config["token"] = os.environ["CORTEX_TOKEN"]
    if os.environ.get("CORTEX_AGENT_ID"):
        config["agent_id"] = os.environ["CORTEX_AGENT_ID"]
    return config


def _read_json(path: Path) -> dict[str, Any]:
    if not path.exists():
        return {}
    try:
        value = json.loads(path.read_text(encoding="utf-8-sig"))
        return value if isinstance(value, dict) else {}
    except (OSError, ValueError):
        return {}


def _format_recall(result: dict[str, Any]) -> str:
    items = result.get("items") or []
    if not items:
        return ""
    lines = []
    for item in items:
        memory = item.get("memory") or {}
        lines.append(
            f"- [{memory.get('kind', 'memory')}] {memory.get('content', '')} "
            f"(id={memory.get('id', '')}, truth={memory.get('truth_score', 0):.2f}, "
            f"utility={memory.get('utility_score', 0):.2f})"
        )
    heading = "## Cortex Recall"
    token_budget = int(result.get("token_budget") or 0)
    estimated_tokens = int(result.get("estimated_tokens") or 0)
    if token_budget:
        suffix = f"{estimated_tokens}/{token_budget} tokens"
        if result.get("truncated"):
            suffix += ", trimmed"
        heading += f" ({suffix})"
    return heading + "\n" + "\n".join(lines)


def _bounded_int(value: Any, default: int, minimum: int, maximum: int) -> int:
    try:
        parsed = int(value)
    except (TypeError, ValueError):
        parsed = default
    return max(minimum, min(maximum, parsed))


def _as_bool(value: Any, default: bool) -> bool:
    if isinstance(value, bool):
        return value
    if value is None:
        return default
    if isinstance(value, str):
        normalized = value.strip().casefold()
        if normalized in {"true", "1", "yes", "on"}:
            return True
        if normalized in {"false", "0", "no", "off"}:
            return False
    return bool(value)


def register(ctx) -> None:
    ctx.register_memory_provider(CortexMemoryProvider())
