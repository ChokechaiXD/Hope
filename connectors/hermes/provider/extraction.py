"""Strict, dependency-free extraction for explicitly marked Hermes lessons."""

from __future__ import annotations

import hashlib
import re
import unicodedata
from dataclasses import dataclass


_MARKERS = {
    "บทเรียนที่ควรจำ": "solution",
    "การตัดสินใจที่ควรจำ": "decision",
    "ข้อตัดสินใจที่ควรจำ": "decision",
    "วิธีที่ใช้ได้": "solution",
    "วิธีที่ไม่ควรทำซ้ำ": "failed_attempt",
    "Lesson to remember": "solution",
    "Decision to remember": "decision",
    "Working solution": "solution",
    "Failed attempt to remember": "failed_attempt",
    "Fact to remember": "fact",
    "Preference to remember": "preference",
}
_MARKER_PATTERN = re.compile(
    r"^\s*(?:#{1,6}\s*)?(?:\*\*)?(?P<label>"
    + "|".join(re.escape(label) for label in sorted(_MARKERS, key=len, reverse=True))
    + r")(?:\*\*)?\s*[:：]\s*(?P<inline>.*)$",
    re.IGNORECASE,
)
_HEADING_PATTERN = re.compile(r"^\s*#{1,6}\s+")


@dataclass(frozen=True)
class LessonProposal:
    kind: str
    content: str


def extract_lessons(text: str, *, max_chars: int = 1000) -> list[LessonProposal]:
    """Return only contiguous blocks introduced by an explicit lesson marker."""
    if not text or max_chars < 1:
        return []
    lines = text.splitlines()
    proposals: list[LessonProposal] = []
    index = 0
    while index < len(lines):
        match = _MARKER_PATTERN.match(lines[index])
        if not match:
            index += 1
            continue

        parts: list[str] = []
        inline = match.group("inline").strip()
        if inline:
            parts.append(inline)
        index += 1

        while index < len(lines):
            line = lines[index]
            if _MARKER_PATTERN.match(line) or _HEADING_PATTERN.match(line):
                break
            if not line.strip():
                if parts:
                    break
                index += 1
                continue
            parts.append(line.strip())
            index += 1

        content = _truncate_words(" ".join(parts), max_chars)
        if content:
            label = match.group("label")
            kind = next(
                value for key, value in _MARKERS.items() if key.casefold() == label.casefold()
            )
            proposals.append(LessonProposal(kind=kind, content=content))
    return proposals


class LessonBuffer:
    """Hold a small number of explicit lessons and release them every N turns."""

    def __init__(self, *, every_turns: int = 5, max_chars: int = 1000, max_pending: int = 8):
        self.every_turns = max(1, min(50, int(every_turns)))
        self.max_chars = max(1, min(4000, int(max_chars)))
        self.max_pending = max(1, min(32, int(max_pending)))
        self._turns = 0
        self._pending: list[LessonProposal] = []
        self._signatures: set[tuple[str, str]] = set()

    def observe(self, assistant_content: str) -> list[LessonProposal]:
        self._turns += 1
        for proposal in extract_lessons(assistant_content, max_chars=self.max_chars):
            signature = (proposal.kind, _normalize(proposal.content))
            if signature in self._signatures:
                continue
            if len(self._pending) >= self.max_pending:
                self._pending.pop(0)
            self._pending.append(proposal)
            self._signatures = {
                (item.kind, _normalize(item.content)) for item in self._pending
            }
        if self._turns % self.every_turns == 0:
            return self.drain()
        return []

    def drain(self) -> list[LessonProposal]:
        proposals = self._pending
        self._pending = []
        self._signatures.clear()
        return proposals

    def restore(self, proposals: list[LessonProposal]) -> None:
        """Return unsaved proposals to the front of the bounded queue."""
        combined = proposals + self._pending
        restored: list[LessonProposal] = []
        signatures: set[tuple[str, str]] = set()
        for proposal in combined:
            signature = (proposal.kind, _normalize(proposal.content))
            if signature in signatures:
                continue
            restored.append(proposal)
            signatures.add(signature)
            if len(restored) >= self.max_pending:
                break
        self._pending = restored
        self._signatures = signatures


def build_memory_request(
    proposal: LessonProposal,
    *,
    agent_id: str,
    session_id: str,
    default_project: str = "",
    default_domain: str = "",
) -> tuple[dict[str, object], str]:
    """Build a candidate request and a stable idempotency key."""
    agent = _safe_key_part(agent_id) or "agent"
    project = default_project.strip()
    domain = default_domain.strip()
    scope = "project" if project else "domain" if domain else "private"
    scope_key = project or domain or agent_id.strip()
    signature = f"{scope}|{scope_key}|{proposal.kind}|{_normalize(proposal.content)}"
    digest = hashlib.sha256(signature.encode("utf-8")).hexdigest()[:16]
    memory_key = f"auto.{agent}.{proposal.kind}.{digest}"
    payload: dict[str, object] = {
        "kind": proposal.kind,
        "scope": scope,
        "scope_key": scope_key,
        "memory_key": memory_key,
        "title": f"Auto-captured {proposal.kind.replace('_', ' ')}",
        "content": proposal.content,
        "tags": ["auto-captured", "hermes"],
        "session_id": session_id,
        "source_ref": f"session:{session_id}" if session_id else "hermes:auto-capture",
    }
    return payload, f"hermes/auto/{agent}/{proposal.kind}/{digest}"


def _normalize(value: str) -> str:
    return " ".join(unicodedata.normalize("NFKC", value).casefold().split())


def _safe_key_part(value: str) -> str:
    return re.sub(r"[^a-z0-9_-]+", "-", value.strip().casefold()).strip("-")


def _truncate_words(value: str, limit: int) -> str:
    compact = " ".join(value.split())
    if len(compact) <= limit:
        return compact
    prefix = compact[: limit + 1]
    if len(prefix) > limit and not prefix[limit].isspace():
        prefix = prefix.rsplit(" ", 1)[0]
    return prefix[:limit].rstrip()
