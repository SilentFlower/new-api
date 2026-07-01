#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""从 `.trellis/spec/` 发现相关项目 SOP/spec 文件。

这个 helper 刻意保持轻量：只返回候选路径和命中原因，让 AI 在执行流程性或
高影响动作前读取匹配文件；它不把完整文档注入上下文。
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any


MAX_BODY_CHARS = 8000
DEFAULT_LIMIT = 3
MIN_SCORE = 3
MIN_BODY_ONLY_HITS = 5
MIN_HEADING_BODY_HITS = 3
BODY_WEAK_TOKENS = {
    "action",
    "actions",
    "after",
    "before",
    "command",
    "commands",
    "context",
    "current",
    "file",
    "files",
    "match",
    "matched",
    "matches",
    "matching",
    "normal",
    "path",
    "paths",
    "project",
    "read",
    "reason",
    "reasons",
    "relevant",
    "sop",
    "spec",
    "status",
    "task",
    "tasks",
    "workflow",
}
TOKEN_RE = re.compile(r"[A-Za-z0-9_.@/-]+|[\u4e00-\u9fff]{2,}")
HEADER_RE = re.compile(r"^\s{0,3}#{1,3}\s+(.+?)\s*$", re.MULTILINE)
FRONTMATTER_BOUNDARY_RE = re.compile(r"^---\s*$")


@dataclass
class Candidate:
    """带分数的项目知识候选项。"""

    path: str
    score: int
    kind: str
    load: str
    priority: str
    reasons: list[str]


def find_trellis_root(start: Path) -> Path | None:
    """查找最近的 `.trellis/` 所在项目根目录。

    Args:
        start: 开始查找的目录或文件路径。

    Returns:
        项目根目录；找不到 `.trellis/` 时返回 None。
    """
    current = start.resolve()
    if current.is_file():
        current = current.parent
    while True:
        if (current / ".trellis").is_dir():
            return current
        if current == current.parent:
            return None
        current = current.parent


def parse_scalar(value: str) -> str:
    """解析简单 YAML 风格标量值。

    Args:
        value: `key:` 后面的原始值。

    Returns:
        去掉引号后的标量文本。
    """
    value = value.strip()
    if (
        (value.startswith('"') and value.endswith('"'))
        or (value.startswith("'") and value.endswith("'"))
    ):
        return value[1:-1]
    return value


def parse_frontmatter(text: str) -> tuple[dict[str, Any], str]:
    """解析 Markdown 文件中的简单 frontmatter。

    只支持 `key: value` 和 `key:` 后接 `- item` 列表。为了避免新增依赖，
    刻意不支持复杂 YAML。

    Args:
        text: Markdown 文件内容。

    Returns:
        `(元数据, 去除 frontmatter 后的正文)`。
    """
    lines = text.splitlines()
    if not lines or not FRONTMATTER_BOUNDARY_RE.match(lines[0]):
        return {}, text

    end_index = None
    for idx in range(1, len(lines)):
        if FRONTMATTER_BOUNDARY_RE.match(lines[idx]):
            end_index = idx
            break
    if end_index is None:
        return {}, text

    metadata: dict[str, Any] = {}
    current_key: str | None = None
    for raw_line in lines[1:end_index]:
        line = raw_line.rstrip()
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue

        if current_key and stripped.startswith("- "):
            value = parse_scalar(stripped[2:])
            if value:
                metadata.setdefault(current_key, []).append(value)
            continue

        if ":" not in stripped:
            current_key = None
            continue

        key, value = stripped.split(":", 1)
        key = key.strip()
        if not key:
            current_key = None
            continue

        if value.strip():
            metadata[key] = parse_scalar(value)
            current_key = None
        else:
            metadata[key] = []
            current_key = key

    body = "\n".join(lines[end_index + 1 :])
    return metadata, body


def as_list(value: Any) -> list[str]:
    """把 frontmatter 值转换成字符串列表。

    Args:
        value: 解析后的元数据值。

    Returns:
        非空字符串列表。
    """
    if isinstance(value, list):
        return [str(item).strip() for item in value if str(item).strip()]
    if value is None:
        return []
    text = str(value).strip()
    return [text] if text else []


def normalize_tokens(text: str) -> list[str]:
    """提取查询 token，用于确定性的轻量匹配。

    Args:
        text: 查询或可搜索文本。

    Returns:
        去重后的小写 token，保留原始顺序。
    """
    seen: set[str] = set()
    tokens: list[str] = []
    for match in TOKEN_RE.finditer(text.lower()):
        token = match.group(0).strip("._-/")
        if len(token) < 2 or token in seen:
            continue
        seen.add(token)
        tokens.append(token)
    return tokens


def read_markdown(path: Path) -> str | None:
    """以容错 UTF-8 方式读取 Markdown 文件。

    Args:
        path: Markdown 文件路径。

    Returns:
        文件内容；读取失败时返回 None。
    """
    try:
        return path.read_text(encoding="utf-8", errors="replace")
    except OSError:
        return None


def iter_spec_files(spec_dir: Path) -> list[Path]:
    """列出 `.trellis/spec/` 下的 Markdown 文件，包括 guides。

    Args:
        spec_dir: `.trellis/spec` 目录。

    Returns:
        排序后的 Markdown 路径列表。
    """
    if not spec_dir.is_dir():
        return []

    result: list[Path] = []
    for path in spec_dir.rglob("*.md"):
        rel_parts = path.relative_to(spec_dir).parts
        if any(part.startswith(".") for part in rel_parts):
            continue
        result.append(path)
    return sorted(result)


def score_file(root: Path, path: Path, query: str, query_tokens: list[str]) -> Candidate | None:
    """按查询为一个 Markdown spec 文件打分。

    Args:
        root: 项目根目录。
        path: Markdown 文件路径。
        query: 原始查询文本。
        query_tokens: 标准化后的查询 token。

    Returns:
        分数达到阈值时返回候选项，否则返回 None。
    """
    text = read_markdown(path)
    if text is None:
        return None

    metadata, body = parse_frontmatter(text)
    rel_path = path.relative_to(root).as_posix()
    spec_rel_path = path.relative_to(root / ".trellis" / "spec").as_posix()
    spec_rel_lower = spec_rel_path.lower()
    body_sample = body[:MAX_BODY_CHARS]
    body_lower = body_sample.lower()
    headers = HEADER_RE.findall(body)
    header_text = " ".join(headers).lower()

    kind = str(metadata.get("kind") or "").strip()
    load = str(metadata.get("load") or "").strip()
    priority = str(metadata.get("priority") or "").strip()
    triggers = as_list(metadata.get("triggers"))

    score = 0
    reasons: list[str] = []
    query_lower = query.lower()

    matched_triggers: list[str] = []
    for trigger in triggers:
        trigger_lower = trigger.lower()
        if trigger_lower and trigger_lower in query_lower:
            matched_triggers.append(trigger)
    if matched_triggers:
        score += 8 * len(matched_triggers)
        reasons.append(f"matched triggers: {', '.join(matched_triggers[:5])}")

    path_hits = [token for token in query_tokens if token in spec_rel_lower]
    if path_hits:
        score += 4 * len(path_hits)
        reasons.append(f"matched path tokens: {', '.join(path_hits[:5])}")

    header_hits = [token for token in query_tokens if token in header_text]
    if header_hits:
        score += 3 * len(header_hits)
        reasons.append(f"matched headings: {', '.join(header_hits[:5])}")

    raw_body_hits = [token for token in query_tokens if token in body_lower]
    body_hits = [token for token in raw_body_hits if token not in BODY_WEAK_TOKENS]
    if body_hits:
        score += len(body_hits)
        reasons.append(f"matched body tokens: {', '.join(body_hits[:5])}")

    # 避免 `json` / `output` / `spec` 这类泛词把只有正文弱命中的文件全部拉进上下文。
    strong_match = (
        bool(matched_triggers)
        or bool(path_hits)
        or len(header_hits) >= 2
        or (bool(header_hits) and len(body_hits) >= MIN_HEADING_BODY_HITS)
        or len(body_hits) >= MIN_BODY_ONLY_HITS
    )
    if not strong_match:
        return None

    if kind.lower() in {"sop", "procedure", "guide", "thinking-guide"} and score > 0:
        score += 2
    if load.lower() in {"before_action", "before-acting", "before_acting"} and score > 0:
        score += 2
    if priority.lower() == "high" and score > 0:
        score += 2

    if score < MIN_SCORE:
        return None

    return Candidate(
        path=rel_path,
        score=score,
        kind=kind or ("thinking-guide" if "/guides/" in f"/{rel_path}" else "spec"),
        load=load,
        priority=priority,
        reasons=reasons,
    )


def find_candidates(root: Path, query: str, limit: int) -> list[Candidate]:
    """按查询查找项目知识候选项。

    Args:
        root: 项目根目录。
        query: 描述意图动作的短查询。
        limit: 最大候选数量。

    Returns:
        排序后的候选项列表。
    """
    query_tokens = normalize_tokens(query)
    if not query_tokens:
        return []

    candidates: list[Candidate] = []
    for path in iter_spec_files(root / ".trellis" / "spec"):
        candidate = score_file(root, path, query, query_tokens)
        if candidate:
            candidates.append(candidate)

    candidates.sort(key=lambda item: (-item.score, item.path))
    return candidates[:limit]


def format_markdown(candidates: list[Candidate]) -> str:
    """把候选项格式化为给 AI 阅读的紧凑 Markdown。

    Args:
        candidates: 已打分候选项。

    Returns:
        Markdown 输出。
    """
    lines = ["## Relevant Project Knowledge", ""]
    if not candidates:
        lines.append("No relevant project SOP/spec matched. Continue with the normal workflow.")
        return "\n".join(lines)

    for candidate in candidates:
        lines.append(f"- {candidate.path}")
        lines.append(f"  kind: {candidate.kind}")
        lines.append(f"  score: {candidate.score}")
        if candidate.load:
            lines.append(f"  load: {candidate.load}")
        if candidate.priority:
            lines.append(f"  priority: {candidate.priority}")
        if candidate.reasons:
            lines.append(f"  reason: {'; '.join(candidate.reasons)}")
        lines.append("  action: read before acting")
    return "\n".join(lines)


def format_json(candidates: list[Candidate]) -> str:
    """把候选项格式化为 JSON。

    Args:
        candidates: 已打分候选项。

    Returns:
        JSON 字符串。
    """
    payload = [
        {
            "file": candidate.path,
            "kind": candidate.kind,
            "score": candidate.score,
            "load": candidate.load,
            "priority": candidate.priority,
            "reason": candidate.reasons,
            "action": "read before acting",
        }
        for candidate in candidates
    ]
    return json.dumps(payload, ensure_ascii=False, indent=2)


def parse_args(argv: list[str]) -> argparse.Namespace:
    """解析 CLI 参数。

    Args:
        argv: 不包含可执行文件名的命令行参数。

    Returns:
        解析后的参数。
    """
    parser = argparse.ArgumentParser(
        description="Discover relevant project SOP/spec files from .trellis/spec/."
    )
    parser.add_argument(
        "query",
        nargs="*",
        help="Short query describing the intended action.",
    )
    parser.add_argument(
        "--limit",
        type=int,
        default=DEFAULT_LIMIT,
        help=f"Maximum number of matches to return (default: {DEFAULT_LIMIT}).",
    )
    parser.add_argument(
        "--json",
        action="store_true",
        help="Output JSON instead of Markdown.",
    )
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    """运行项目知识发现命令。

    Args:
        argv: 可选命令行参数，不包含可执行文件名。

    Returns:
        进程退出码。
    """
    args = parse_args(sys.argv[1:] if argv is None else argv)
    query = " ".join(args.query).strip()
    root = find_trellis_root(Path.cwd()) or find_trellis_root(Path(__file__).resolve())
    if root is None:
        print("## Relevant Project Knowledge\n\nNo .trellis/ directory found. Continue with the normal workflow.")
        return 0

    candidates = find_candidates(root, query, max(1, args.limit))
    print(format_json(candidates) if args.json else format_markdown(candidates))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
