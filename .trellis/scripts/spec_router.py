#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""从 `.trellis/spec/` 发现相关项目 SOP/spec 文件。

这个 helper 刻意保持轻量：只返回候选路径、置信度和命中原因，让 AI 在项目
局部知识可能影响做法的决策边界前读取匹配文件；它不把完整文档注入上下文。
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
MIN_ANCHORED_BODY_HITS = 2
WEAK_TOKENS = {
    "action",
    "actions",
    "after",
    "and",
    "before",
    "change",
    "changes",
    "cli",
    "command",
    "commands",
    "commit",
    "context",
    "current",
    "data",
    "documentation",
    "edit",
    "file",
    "files",
    "flow",
    "flower",
    "for",
    "from",
    "guide",
    "guides",
    "in",
    "index",
    "match",
    "matched",
    "matches",
    "matching",
    "md",
    "normal",
    "of",
    "or",
    "path",
    "paths",
    "project",
    "py",
    "read",
    "readme",
    "reason",
    "reasons",
    "relevant",
    "run",
    "simple",
    "small",
    "sop",
    "spec",
    "status",
    "task",
    "tasks",
    "the",
    "to",
    "trellis",
    "typo",
    "update",
    "with",
    "workflow",
}
TOKEN_RE = re.compile(r"[A-Za-z0-9@]+|[\u4e00-\u9fff]+")
CJK_RE = re.compile(r"^[\u4e00-\u9fff]+$")
HEADER_RE = re.compile(r"^\s{0,3}#{1,3}\s+(.+?)\s*$", re.MULTILINE)
FRONTMATTER_BOUNDARY_RE = re.compile(r"^---\s*$")
MARKDOWN_LINK_RE = re.compile(r"\[([^\]]+)\]\(([^)]+\.md(?:#[^)]+)?)\)")


@dataclass
class Candidate:
    """带分数的项目知识候选项。"""

    path: str
    score: int
    kind: str
    load: str
    priority: str
    confidence: str
    reasons: list[str]
    action: str


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


def add_token(tokens: list[str], seen: set[str], token: str) -> None:
    """按原始顺序追加去重 token。

    Args:
        tokens: 正在构造的 token 列表。
        seen: 已追加 token 集合。
        token: 待追加 token。

    Returns:
        None。
    """
    if len(token) < 2 or token in seen:
        return
    seen.add(token)
    tokens.append(token)


def add_cjk_tokens(tokens: list[str], seen: set[str], text: str) -> None:
    """为连续中文文本追加有限 n-gram token。

    中文没有空格分词。这里生成 2 到 6 字的 n-gram，用 token 集合匹配替代旧
    版任意子串匹配，同时保留 `发版` 命中 `发版流程` 这类常见能力。

    Args:
        tokens: 正在构造的 token 列表。
        seen: 已追加 token 集合。
        text: 连续中文文本。

    Returns:
        None。
    """
    max_size = min(6, len(text))
    for size in range(2, max_size + 1):
        for start in range(0, len(text) - size + 1):
            add_token(tokens, seen, text[start : start + size])


def normalize_tokens(text: str) -> list[str]:
    """提取查询或文档 token，用于确定性的轻量匹配。

    Args:
        text: 查询或可搜索文本。

    Returns:
        去重后的小写 token，保留原始顺序。
    """
    seen: set[str] = set()
    tokens: list[str] = []
    for match in TOKEN_RE.finditer(text.lower()):
        token = match.group(0)
        if CJK_RE.match(token):
            add_cjk_tokens(tokens, seen, token)
        else:
            add_token(tokens, seen, token)
    return tokens


def significant_hits(query_tokens: list[str], target_tokens: list[str]) -> list[str]:
    """计算非弱词 token 命中，保留查询 token 顺序。

    Args:
        query_tokens: 查询 token。
        target_tokens: 待匹配文本 token。

    Returns:
        非弱词命中列表。
    """
    target_set = set(target_tokens)
    return [
        token
        for token in query_tokens
        if token not in WEAK_TOKENS and token in target_set
    ]


def collect_index_descriptions(spec_dir: Path) -> dict[str, list[str]]:
    """从 `index.md` 链接行收集目标文档的路由描述。

    Args:
        spec_dir: `.trellis/spec` 目录。

    Returns:
        以 spec 相对路径为 key 的描述文本列表。
    """
    descriptions: dict[str, list[str]] = {}
    spec_root = spec_dir.resolve()
    for index_path in iter_spec_files(spec_dir):
        if index_path.name != "index.md":
            continue

        text = read_markdown(index_path)
        if text is None:
            continue
        _, body = parse_frontmatter(text)

        for line in body.splitlines():
            matches = list(MARKDOWN_LINK_RE.finditer(line))
            if not matches:
                continue

            clean_line = MARKDOWN_LINK_RE.sub(lambda item: item.group(1), line).strip()
            for match in matches:
                link_target = match.group(2).split("#", 1)[0].strip()
                if "://" in link_target or link_target.startswith("#"):
                    continue

                target_path = (index_path.parent / link_target).resolve()
                try:
                    target_rel_path = target_path.relative_to(spec_root).as_posix()
                except ValueError:
                    continue

                if not target_path.is_file() or target_path == index_path.resolve():
                    continue

                description = f"{match.group(1)} {clean_line}".strip()
                if description:
                    descriptions.setdefault(target_rel_path, []).append(description)
    return descriptions


def classify_confidence(
    matched_triggers: list[str],
    path_hits: list[str],
    header_hits: list[str],
    index_hits: list[str],
    body_hits: list[str],
) -> str | None:
    """根据强锚点和弱证据判断候选置信度。

    Args:
        matched_triggers: frontmatter trigger 命中。
        path_hits: 路径命中。
        header_hits: 标题命中。
        index_hits: index 描述命中。
        body_hits: 正文样本命中。

    Returns:
        `high` / `medium`；证据不足时返回 None。
    """
    if matched_triggers:
        return "high"

    anchor_groups = [path_hits, header_hits, index_hits]
    anchor_count = sum(1 for group in anchor_groups if group)
    if len(path_hits) >= 2 or len(header_hits) >= 2 or len(index_hits) >= 2:
        return "high"
    if anchor_count >= 2:
        return "high"
    if anchor_count == 1 and len(body_hits) >= MIN_ANCHORED_BODY_HITS:
        return "high"
    if anchor_count == 1:
        return "medium"
    if len(body_hits) >= MIN_BODY_ONLY_HITS:
        return "medium"
    return None


def action_for_confidence(confidence: str) -> str:
    """按置信度返回读取建议。

    Args:
        confidence: 候选置信度。

    Returns:
        面向 AI 的行动建议。
    """
    if confidence == "high":
        return "read before acting"
    return "read if clearly relevant"


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


def score_file(
    root: Path,
    path: Path,
    query: str,
    query_tokens: list[str],
    index_descriptions: dict[str, list[str]],
) -> Candidate | None:
    """按查询为一个 Markdown spec 文件打分。

    Args:
        root: 项目根目录。
        path: Markdown 文件路径。
        query: 原始查询文本。
        query_tokens: 标准化后的查询 token。
        index_descriptions: 从 index.md 收集的目标文档描述。

    Returns:
        分数达到阈值时返回候选项，否则返回 None。
    """
    text = read_markdown(path)
    if text is None:
        return None

    metadata, body = parse_frontmatter(text)
    rel_path = path.relative_to(root).as_posix()
    spec_rel_path = path.relative_to(root / ".trellis" / "spec").as_posix()
    spec_rel_tokens = normalize_tokens(spec_rel_path)
    body_sample = body[:MAX_BODY_CHARS]
    body_tokens = normalize_tokens(body_sample)
    headers = HEADER_RE.findall(body)
    header_tokens = normalize_tokens(" ".join(headers))
    index_text = " ".join(index_descriptions.get(spec_rel_path, []))
    index_tokens = normalize_tokens(index_text)

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

    path_hits = significant_hits(query_tokens, spec_rel_tokens)
    if path_hits:
        score += 5 * len(path_hits)
        reasons.append(f"matched path tokens: {', '.join(path_hits[:5])}")

    header_hits = significant_hits(query_tokens, header_tokens)
    if header_hits:
        score += 3 * len(header_hits)
        reasons.append(f"matched headings: {', '.join(header_hits[:5])}")

    index_hits = significant_hits(query_tokens, index_tokens)
    if index_hits:
        score += 4 * len(index_hits)
        reasons.append(f"matched index descriptions: {', '.join(index_hits[:5])}")

    body_hits = significant_hits(query_tokens, body_tokens)
    if body_hits:
        score += len(body_hits)
        reasons.append(f"matched body tokens: {', '.join(body_hits[:5])}")

    # 避免 `to` / `flow` / `commit` 这类泛词或少量正文词把无关文件拉进上下文。
    confidence = classify_confidence(
        matched_triggers,
        path_hits,
        header_hits,
        index_hits,
        body_hits,
    )
    if confidence is None:
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
        confidence=confidence,
        reasons=reasons,
        action=action_for_confidence(confidence),
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
    spec_dir = root / ".trellis" / "spec"
    index_descriptions = collect_index_descriptions(spec_dir)
    for path in iter_spec_files(spec_dir):
        candidate = score_file(root, path, query, query_tokens, index_descriptions)
        if candidate:
            candidates.append(candidate)

    candidates.sort(
        key=lambda item: (
            0 if item.confidence == "high" else 1,
            -item.score,
            item.path,
        )
    )
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
        lines.append(f"  confidence: {candidate.confidence}")
        if candidate.load:
            lines.append(f"  load: {candidate.load}")
        if candidate.priority:
            lines.append(f"  priority: {candidate.priority}")
        if candidate.reasons:
            lines.append(f"  reason: {'; '.join(candidate.reasons)}")
        lines.append(f"  action: {candidate.action}")
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
            "confidence": candidate.confidence,
            "load": candidate.load,
            "priority": candidate.priority,
            "reason": candidate.reasons,
            "action": candidate.action,
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
