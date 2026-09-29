#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Trellis 任务关闭、旧数据收敛与物理 GC 的确定性运行时。"""

from __future__ import annotations

import argparse
import contextlib
import hashlib
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import time
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any, Iterator, Optional

from common.git import run_git
from common.io import read_json, write_json
from common.paths import FILE_TASK_JSON, get_repo_root, get_tasks_dir
from common.task_utils import run_task_hooks
from common.tasks import normalize_closeout, pending_closeout


DEFAULT_GC_AGE = "3d"
LOCK_STALE_SECONDS = 10 * 60
_DURATION_RE = re.compile(r"^(?P<value>[1-9][0-9]*)(?P<unit>[mhd])$")
GC_JOURNAL_SCHEMA_VERSION = 1
GC_JOURNAL_RELATIVE = ".trellis/.runtime/task-gc-transaction.json"
EXACT_COMMIT_JOURNAL_SCHEMA_VERSION = 1
EXACT_COMMIT_JOURNAL_RELATIVE = ".trellis/.runtime/task-maintenance-commit.json"
RECOMPUTABLE_BLOCKER_CODES = {
    "ambiguous-child",
    "decision-log-invalid",
    "decision-review-required",
    "delivery-recovery-required",
    "missing-child",
    "open-children",
    "work-not-completed",
}


class TaskLifecycleError(RuntimeError):
    """表示 Close、迁移或 GC 无法安全继续。"""

    def __init__(self, reason: str, message: str) -> None:
        """初始化带稳定原因码的生命周期错误。

        Args:
            reason: 机器可读的稳定原因码。
            message: 面向用户的错误说明。
        """
        super().__init__(message)
        self.reason = reason


def utc_now() -> str:
    """返回秒级 UTC ISO-8601 时间。

    Returns:
        以 ``Z`` 结尾的 UTC 时间字符串。
    """
    return datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def is_task_closed(task_data: dict[str, Any], physical_archive: bool = False) -> bool:
    """判断任务是否已经逻辑关闭。

    Args:
        task_data: task.json 对象。
        physical_archive: 任务是否位于历史物理 archive。

    Returns:
        仅合法 closed 或历史物理 archive 返回 True。
    """
    if physical_archive:
        return True
    closeout = normalize_closeout(task_data)
    return bool(closeout["valid"] and closeout["status"] == "closed")


def _blocker(code: str, owner: str, message: str, detail: Any = None) -> dict[str, Any]:
    """构造稳定的 Close blocker。"""
    value: dict[str, Any] = {"code": code, "owner": owner, "message": message}
    if detail is not None:
        value["detail"] = detail
    return value


def _task_relative(repo_root: Path, task_dir: Path) -> str:
    """返回任务的仓库相对路径。"""
    try:
        return task_dir.relative_to(repo_root).as_posix()
    except ValueError:
        return str(task_dir)


def _normalize_task_ref(value: str) -> str:
    """规范化 runtime 中的仓库相对任务引用。"""
    normalized = value.strip().replace("\\", "/")
    while normalized.startswith("./"):
        normalized = normalized[2:]
    return normalized.rstrip("/")


def _resolve_top_level_task(repo_root: Path, task_ref: str) -> Path:
    """解析顶层任务引用并拒绝 archive、软链和路径越界。"""
    raw = task_ref.strip() if isinstance(task_ref, str) else ""
    if not raw:
        raise TaskLifecycleError("task-reference-empty", "任务引用不能为空")
    tasks_dir = get_tasks_dir(repo_root).resolve()
    normalized = raw.replace("\\", "/")
    while normalized.startswith("./"):
        normalized = normalized[2:]
    candidate = Path(raw)
    if candidate.is_absolute() or "/" in normalized:
        candidate = candidate if candidate.is_absolute() else repo_root / normalized
        try:
            resolved = candidate.resolve(strict=True)
        except (OSError, RuntimeError) as error:
            raise TaskLifecycleError("task-not-found", f"任务不存在：{task_ref}") from error
        if candidate.is_symlink() or resolved.parent != tasks_dir or resolved.name == "archive":
            raise TaskLifecycleError("unsafe-task-path", f"任务必须位于活动 tasks 根目录：{task_ref}")
        return resolved
    exact = tasks_dir / raw
    if exact.is_dir() and not exact.is_symlink():
        return exact.resolve()
    matches = []
    try:
        for path in sorted(tasks_dir.iterdir()):
            if path.is_dir() and not path.is_symlink() and path.name != "archive" and path.name.endswith(f"-{raw}"):
                matches.append(path.resolve())
    except OSError as error:
        raise TaskLifecycleError("task-scan-failed", "无法读取活动任务目录") from error
    if len(matches) == 1:
        return matches[0]
    if len(matches) > 1:
        raise TaskLifecycleError(
            "task-reference-ambiguous",
            f"任务引用存在歧义：{task_ref}；候选：{', '.join(path.name for path in matches)}",
        )
    raise TaskLifecycleError("task-not-found", f"任务不存在：{task_ref}")


def _find_tasks_anywhere(tasks_dir: Path, task_name: str) -> list[Path]:
    """按完整目录名查找全部顶层或历史归档任务。"""
    matches: list[Path] = []
    active = tasks_dir / task_name
    if active.is_dir() and not active.is_symlink():
        matches.append(active)
    archive = tasks_dir / "archive"
    if not archive.is_dir():
        return matches
    for bucket in sorted(archive.iterdir()):
        candidate = bucket / task_name
        if bucket.is_dir() and candidate.is_dir() and not candidate.is_symlink():
            matches.append(candidate)
    return matches


def _git_is_repository(repo_root: Path) -> bool:
    """判断目录是否位于 Git worktree。"""
    code, output, _ = run_git(["rev-parse", "--is-inside-work-tree"], cwd=repo_root)
    return code == 0 and output.strip() == "true"


def _git_path_status(repo_root: Path, paths: list[str]) -> str:
    """读取精确路径的 porcelain 状态。"""
    if not paths:
        return ""
    code, output, error = run_git(
        ["status", "--porcelain=v1", "-z", "--untracked-files=all", "--", *paths],
        cwd=repo_root,
    )
    if code != 0:
        raise TaskLifecycleError("git-status-failed", error.strip() or "无法读取 Git 状态")
    return output


def _delivery_is_clean(repo_root: Path, task_dir: Path) -> bool:
    """判断任务路径是否没有未提交交付差异。"""
    if not _git_is_repository(repo_root):
        return True
    return not _git_path_status(repo_root, [_task_relative(repo_root, task_dir)])


def evaluate_close(
    task_dir: Path,
    repo_root: Path,
    task_data: Optional[dict[str, Any]] = None,
    *,
    delivery_verified: bool = False,
    explicit_blockers: Optional[list[dict[str, Any]]] = None,
    resolved_blocker_codes: Optional[set[str]] = None,
    closed_at: Optional[str] = None,
) -> dict[str, Any]:
    """确定性评估任务 Close，不写文件。

    Args:
        task_dir: 顶层任务目录。
        repo_root: 项目根目录。
        task_data: 可选的内存 task.json；auto-loop 用它保持单次原子写入。
        delivery_verified: 调用方是否已由 Push/runner 验证交付证据。
        explicit_blockers: 上游 owner 明确传入的语义 blocker。
        resolved_blocker_codes: 上游 owner 明确声明已解决的持久化 blocker code。
        closed_at: 测试或迁移使用的固定关闭时间。

    Returns:
        结构化 close 结果；``closeout`` 可直接写入 task.json。
    """
    data = task_data if isinstance(task_data, dict) else read_json(task_dir / FILE_TASK_JSON)
    task_ref = _task_relative(repo_root, task_dir)
    if not isinstance(data, dict):
        return {
            "status": "error",
            "task": task_ref,
            "closedAt": None,
            "blockers": [_blocker("invalid-task-json", "task.py", "task.json 缺失或损坏")],
        }
    current = normalize_closeout(data)
    if current["valid"] and current["status"] == "closed":
        return {
            "status": "already-closed",
            "task": task_ref,
            "closedAt": current["closedAt"],
            "blockers": [],
            "closeout": {
                "status": "closed",
                "closedAt": current["closedAt"],
                "blockers": [],
            },
        }
    resolved_codes = resolved_blocker_codes or set()
    blockers = []
    if current["valid"] and current["status"] == "blocked":
        # 结构条件每次重新计算；release/父任务整合等语义 blocker 只能由 owner 显式解除。
        blockers.extend(
            blocker
            for blocker in current["blockers"]
            if blocker.get("code") not in RECOMPUTABLE_BLOCKER_CODES
            and blocker.get("code") not in resolved_codes
        )
    blockers.extend(explicit_blockers or [])
    if data.get("status") != "completed":
        blockers.append(_blocker("work-not-completed", "task_progress.py", "任务工作状态尚未 completed"))

    try:
        from decision_log import decision_review_status

        decision_status = decision_review_status(task_dir)
        if not decision_status.get("close_allowed"):
            blockers.append(_blocker(
                "decision-review-required",
                "decision_log.py",
                "当前 AI 决策摘要尚未获得 accepted review",
                {"decisionDigest": decision_status.get("decision_digest")},
            ))
    except (ImportError, OSError, ValueError) as error:
        blockers.append(_blocker("decision-log-invalid", "decision_log.py", str(error)))

    tasks_dir = get_tasks_dir(repo_root)
    children = data.get("children") if isinstance(data.get("children"), list) else []
    for child_name in children:
        if not isinstance(child_name, str) or not child_name:
            blockers.append(_blocker("missing-child", "task.py", "任务包含非法 child 引用"))
            continue
        child_matches = _find_tasks_anywhere(tasks_dir, child_name)
        if not child_matches:
            blockers.append(_blocker("missing-child", "task.py", f"无法解析 child：{child_name}"))
            continue
        if len(child_matches) > 1:
            blockers.append(_blocker(
                "ambiguous-child",
                "task.py",
                f"child 身份重复：{child_name}",
                {"matches": [_task_relative(repo_root, path) for path in child_matches]},
            ))
            continue
        child_dir = child_matches[0]
        child_data = read_json(child_dir / FILE_TASK_JSON)
        if not isinstance(child_data, dict) or child_data.get("status") != "completed":
            blockers.append(_blocker("open-children", "task.py", f"child 尚未完成：{child_name}"))

    if not delivery_verified and data.get("status") == "completed" and not _delivery_is_clean(repo_root, task_dir):
        blockers.append(_blocker(
            "delivery-recovery-required",
            "trellis-push",
            "任务目录存在未闭合的交付差异，请先由 trellis-push 恢复",
        ))

    deduplicated: list[dict[str, Any]] = []
    seen_blockers: set[tuple[str, str, str]] = set()
    for blocker in blockers:
        key = (
            str(blocker.get("code") or ""),
            str(blocker.get("owner") or ""),
            str(blocker.get("message") or ""),
        )
        if key not in seen_blockers:
            seen_blockers.add(key)
            deduplicated.append(blocker)
    blockers = deduplicated
    if blockers:
        closeout = {"status": "blocked", "closedAt": None, "blockers": blockers}
        return {
            "status": "blocked",
            "task": task_ref,
            "closedAt": None,
            "blockers": blockers,
            "closeout": closeout,
        }
    timestamp = closed_at or utc_now()
    closeout = {"status": "closed", "closedAt": timestamp, "blockers": []}
    return {
        "status": "closed",
        "task": task_ref,
        "closedAt": timestamp,
        "blockers": [],
        "closeout": closeout,
    }


def apply_close(
    task_dir: Path,
    repo_root: Path,
    *,
    delivery_verified: bool = False,
    explicit_blockers: Optional[list[dict[str, Any]]] = None,
    resolved_blocker_codes: Optional[set[str]] = None,
    closed_at: Optional[str] = None,
) -> dict[str, Any]:
    """评估并原子写入 Close 结果。

    Args:
        task_dir: 顶层任务目录。
        repo_root: 项目根目录。
        delivery_verified: 调用方是否已验证交付证据。
        explicit_blockers: 上游明确传入的 blocker。
        resolved_blocker_codes: 上游明确声明已经解决的 blocker code。
        closed_at: 测试或迁移固定时间。

    Returns:
        结构化 Close 结果。
    """
    task_json = task_dir / FILE_TASK_JSON
    data = read_json(task_json)
    result = evaluate_close(
        task_dir,
        repo_root,
        data,
        delivery_verified=delivery_verified,
        explicit_blockers=explicit_blockers,
        resolved_blocker_codes=resolved_blocker_codes,
        closed_at=closed_at,
    )
    if result["status"] in {"error", "already-closed"}:
        return result
    if not isinstance(data, dict):
        return result
    if data.get("closeout") == result["closeout"]:
        return result
    data["closeout"] = result["closeout"]
    if not write_json(task_json, data):
        return {
            "status": "error",
            "task": result["task"],
            "closedAt": None,
            "blockers": [_blocker("write-failed", "task.py", "无法原子写入 task.json")],
        }
    return finalize_close_effects(task_dir, repo_root, result)


def finalize_close_effects(
    task_dir: Path,
    repo_root: Path,
    result: dict[str, Any],
) -> dict[str, Any]:
    """在 Close 已持久化后清理 Session 并执行 ``after_close``。

    Args:
        task_dir: 已写入关闭结果的顶层任务目录。
        repo_root: 项目根目录。
        result: ``evaluate_close`` 或 ``apply_close`` 返回的结构化结果。

    Returns:
        附带 best-effort 副作用诊断的原结果。
    """
    if result.get("status") != "closed":
        return result
    try:
        from common.active_task import clear_task_from_sessions

        result["sessionsCleared"] = clear_task_from_sessions(result["task"], repo_root)
    except (ImportError, OSError, ValueError) as error:
        result["sessionCleanupWarning"] = str(error)
    try:
        run_task_hooks("after_close", task_dir / FILE_TASK_JSON, repo_root)
    except (OSError, ValueError) as error:
        result["hookWarning"] = str(error)
    return result


def cmd_close(args: argparse.Namespace) -> int:
    """执行 ``task.py close``。

    Args:
        args: 含 task、resolve_blocker 与 json 的 argparse 参数。

    Returns:
        closed/already-closed 为 0，blocked/error 为 1。
    """
    repo_root = get_repo_root()
    try:
        task_dir = _resolve_top_level_task(repo_root, args.task)
        result = apply_close(
            task_dir,
            repo_root,
            resolved_blocker_codes=set(getattr(args, "resolve_blocker", []) or []),
        )
    except TaskLifecycleError as error:
        result = {"status": "error", "reason": error.reason, "message": str(error)}
    if getattr(args, "json", False):
        print(json.dumps(result, ensure_ascii=False, sort_keys=True))
    elif result["status"] in {"closed", "already-closed"}:
        print(f"✓ 任务已关闭：{result.get('task')}")
    else:
        print(f"无法关闭任务：{result.get('message') or result.get('status')}", file=sys.stderr)
        for blocker in result.get("blockers", []):
            print(f"- {blocker.get('code')}: {blocker.get('message')}", file=sys.stderr)
    return 0 if result["status"] in {"closed", "already-closed"} else 1


def _parse_timestamp(value: Any) -> Optional[datetime]:
    """严格解析带时区的 ISO-8601 时间。"""
    if not isinstance(value, str) or not value.strip():
        return None
    try:
        parsed = datetime.fromisoformat(value.strip().replace("Z", "+00:00"))
    except ValueError:
        return None
    if parsed.tzinfo is None:
        return None
    return parsed.astimezone(timezone.utc)


def parse_duration(value: str) -> timedelta:
    """解析 GC 使用的严格分钟/小时/天 duration。

    Args:
        value: 例如 ``30m``、``12h`` 或 ``3d``。

    Returns:
        对应的 timedelta。

    Raises:
        TaskLifecycleError: 格式非法。
    """
    match = _DURATION_RE.fullmatch(value.strip() if isinstance(value, str) else "")
    if not match:
        raise TaskLifecycleError("invalid-duration", "--before 必须为正整数加 m/h/d，例如 3d")
    amount = int(match.group("value"))
    unit = match.group("unit")
    if unit == "m":
        return timedelta(minutes=amount)
    if unit == "h":
        return timedelta(hours=amount)
    return timedelta(days=amount)


def _integration_in_progress(repo_root: Path) -> list[str]:
    """返回正在进行的 Git 集成状态。"""
    names = []
    for name in ("MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "REBASE_HEAD"):
        code, output, _ = run_git(["rev-parse", "--git-path", name], cwd=repo_root)
        git_path = Path(output.strip())
        if not git_path.is_absolute():
            git_path = repo_root / git_path
        if code == 0 and output.strip() and git_path.exists():
            names.append(name.lower())
    for name in ("rebase-merge", "rebase-apply"):
        code, output, _ = run_git(["rev-parse", "--git-path", name], cwd=repo_root)
        git_path = Path(output.strip())
        if not git_path.is_absolute():
            git_path = repo_root / git_path
        if code == 0 and output.strip() and git_path.exists():
            names.append(name)
    code, output, _ = run_git(["diff", "--name-only", "--diff-filter=U"], cwd=repo_root)
    if code != 0 or output.strip():
        names.append("unmerged")
    return sorted(set(names))


def _git_preflight(repo_root: Path) -> dict[str, str]:
    """验证精确本地提交的最小 Git 前提并固定分支引用与 HEAD。"""
    if not _git_is_repository(repo_root):
        raise TaskLifecycleError("git-required", "任务 maintenance 需要 Git worktree")
    code, branch_ref, _ = run_git(["symbolic-ref", "--quiet", "HEAD"], cwd=repo_root)
    if code != 0 or not branch_ref.strip():
        raise TaskLifecycleError("detached-head", "detached HEAD 下不创建 maintenance commit")
    integration = _integration_in_progress(repo_root)
    if integration:
        raise TaskLifecycleError("git-integration-active", f"Git 集成尚未结束：{', '.join(integration)}")
    code, head, error = run_git(["rev-parse", "HEAD"], cwd=repo_root)
    if code != 0 or not head.strip():
        raise TaskLifecycleError("git-head-invalid", error.strip() or "无法读取 HEAD")
    return {"head": head.strip(), "branchRef": branch_ref.strip()}


def _assert_git_baseline(repo_root: Path, baseline: dict[str, str]) -> None:
    """确认 maintenance 仍位于事务开始时的完整分支引用和 HEAD。"""
    code, branch_ref, _ = run_git(["symbolic-ref", "--quiet", "HEAD"], cwd=repo_root)
    if code != 0 or branch_ref.strip() != baseline["branchRef"]:
        raise TaskLifecycleError("branch-changed", "maintenance 期间当前分支已变化")
    code, head, _ = run_git(["rev-parse", "HEAD"], cwd=repo_root)
    if code != 0 or head.strip() != baseline["head"]:
        raise TaskLifecycleError("head-changed", "maintenance 期间 HEAD 已变化")


def _index_fingerprint(repo_root: Path, excluded: set[str]) -> str:
    """计算候选外 Git index 的稳定摘要。"""
    code, output, error = run_git(["ls-files", "-s", "-z"], cwd=repo_root)
    if code != 0:
        raise TaskLifecycleError("git-index-read-failed", error.strip() or "无法读取 Git index")
    kept = []
    for entry in output.split("\0"):
        if not entry:
            continue
        path = entry.split("\t", 1)[1] if "\t" in entry else ""
        if any(path == item or path.startswith(f"{item}/") for item in excluded):
            continue
        kept.append(entry)
    return hashlib.sha256("\0".join(kept).encode("utf-8")).hexdigest()


def _status_records(output: str) -> list[tuple[str, tuple[str, ...]]]:
    """解析 ``git status --porcelain=v1 -z`` 记录。"""
    parts = output.split("\0")
    records: list[tuple[str, tuple[str, ...]]] = []
    index = 0
    while index < len(parts):
        raw = parts[index]
        index += 1
        if not raw:
            continue
        status = raw[:2]
        paths = [raw[3:]]
        if ("R" in status or "C" in status) and index < len(parts) and parts[index]:
            paths.append(parts[index])
            index += 1
        records.append((status, tuple(paths)))
    return records


def _path_is_within(path: str, roots: set[str]) -> bool:
    """判断 Git 路径是否落在任一精确候选根内。"""
    return any(path == root or path.startswith(f"{root}/") for root in roots)


def _worktree_fingerprint(repo_root: Path, excluded: set[str]) -> str:
    """计算候选外 staged、unstaged 与 untracked 内容摘要。"""
    code, output, error = run_git(
        ["status", "--porcelain=v1", "-z", "--untracked-files=all"],
        cwd=repo_root,
    )
    if code != 0:
        raise TaskLifecycleError("git-status-failed", error.strip() or "无法读取 Git 状态")
    digest = hashlib.sha256()
    for status, paths in _status_records(output):
        if all(_path_is_within(path, excluded) for path in paths):
            continue
        digest.update(status.encode("utf-8"))
        digest.update(b"\0")
        for relative in paths:
            digest.update(relative.encode("utf-8"))
            digest.update(b"\0")
            if _path_is_within(relative, excluded):
                continue
            candidate = repo_root / relative
            try:
                if candidate.is_symlink():
                    digest.update(b"symlink\0")
                    digest.update(os.readlink(candidate).encode("utf-8"))
                elif candidate.is_file():
                    digest.update(b"file\0")
                    digest.update(hashlib.sha256(candidate.read_bytes()).digest())
                else:
                    digest.update(b"missing\0")
            except OSError as error:
                raise TaskLifecycleError("worktree-fingerprint-failed", str(error)) from error
            digest.update(b"\0")
    return digest.hexdigest()


def _tracked_files(repo_root: Path, relative: str) -> list[str]:
    """列出路径下已跟踪文件。"""
    code, output, error = run_git(["ls-files", "-z", "--", relative], cwd=repo_root)
    if code != 0:
        raise TaskLifecycleError("git-ls-files-failed", error.strip() or "无法读取已跟踪文件")
    return sorted(path for path in output.split("\0") if path)


def _untracked_files(repo_root: Path, relative: str) -> list[str]:
    """列出路径下可由 Git 纳管的未跟踪文件。"""
    code, output, error = run_git(
        ["ls-files", "-z", "--others", "--exclude-standard", "--", relative],
        cwd=repo_root,
    )
    if code != 0:
        raise TaskLifecycleError("git-ls-files-failed", error.strip() or "无法读取未跟踪文件")
    return sorted(path for path in output.split("\0") if path)


def _run_git_with_index(
    args: list[str],
    repo_root: Path,
    index_file: Path,
) -> tuple[int, str, str]:
    """使用隔离 index 执行 Git 命令。

    Args:
        args: 不含 ``git`` 的参数列表。
        repo_root: Git worktree 根目录。
        index_file: 本次精确提交专用的临时 index 路径。

    Returns:
        ``(returncode, stdout, stderr)`` 三元组。
    """
    env = os.environ.copy()
    env["GIT_INDEX_FILE"] = str(index_file)
    try:
        result = subprocess.run(
            ["git", "-c", "i18n.logOutputEncoding=UTF-8", *args],
            cwd=repo_root,
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
            env=env,
            check=False,
        )
        return result.returncode, result.stdout, result.stderr
    except Exception as error:
        return 1, "", str(error)


def _exact_commit_journal_path(repo_root: Path) -> Path:
    """返回共享精确提交收尾记录路径。"""
    return repo_root / EXACT_COMMIT_JOURNAL_RELATIVE


def _safe_exact_commit_path(path: Any) -> bool:
    """判断共享提交记录中的路径是否位于合法任务范围。"""
    if not isinstance(path, str) or not path or "\\" in path:
        return False
    parts = path.split("/")
    if len(parts) < 3 or parts[:2] != [".trellis", "tasks"] or any(part in {"", ".", ".."} for part in parts):
        return False
    if parts[2] != "archive":
        return True
    return len(parts) >= 5 and re.fullmatch(r"[0-9]{4}-[0-9]{2}", parts[3]) is not None


def _write_exact_commit_journal(
    repo_root: Path,
    baseline: dict[str, str],
    new_head: str,
    paths: list[str],
    expected_files: set[str],
    index_fingerprint: str,
    worktree_fingerprint: str,
    transaction: dict[str, Any],
) -> dict[str, Any]:
    """在更新分支引用前持久化共享提交收尾证据。

    Args:
        repo_root: Git worktree 根目录。
        baseline: 事务开始时固定的完整分支引用与 HEAD。
        new_head: 已由 ``commit-tree`` 创建的新提交。
        paths: 精确候选 pathspec。
        expected_files: 预期提交文件全集。
        index_fingerprint: 候选外 index 原始摘要。
        worktree_fingerprint: 候选外工作树原始摘要。
        transaction: 操作类型与可恢复结果。

    Returns:
        已持久化的 journal 对象。
    """
    operation = transaction.get("operation")
    result = transaction.get("result")
    if operation not in {"gc", "reconciliation", "restore"} or not isinstance(result, dict):
        raise TaskLifecycleError("git-transaction-invalid", "maintenance 提交事务元数据损坏")
    recovered_result = dict(result)
    recovered_result["commit"] = new_head
    journal = {
        "schemaVersion": EXACT_COMMIT_JOURNAL_SCHEMA_VERSION,
        "kind": "task-maintenance-exact-commit",
        "createdAt": utc_now(),
        "operation": operation,
        "head": baseline["head"],
        "branchRef": baseline["branchRef"],
        "newHead": new_head,
        "paths": sorted(set(paths)),
        "expectedFiles": sorted(expected_files),
        "indexFingerprint": index_fingerprint,
        "worktreeFingerprint": worktree_fingerprint,
        "result": recovered_result,
    }
    path = _exact_commit_journal_path(repo_root)
    if path.exists():
        raise TaskLifecycleError("git-transaction-recovery-required", "存在尚未收尾的 maintenance 提交事务")
    path.parent.mkdir(parents=True, exist_ok=True)
    payload = (json.dumps(journal, ensure_ascii=False, indent=2, sort_keys=True) + "\n").encode("utf-8")
    _atomic_write_bytes(path, payload)
    return journal


def _validated_exact_commit_journal(repo_root: Path) -> Optional[dict[str, Any]]:
    """读取并严格验证共享精确提交收尾记录。"""
    path = _exact_commit_journal_path(repo_root)
    if not path.exists():
        return None
    data = read_json(path)
    if (
        not isinstance(data, dict)
        or data.get("schemaVersion") != EXACT_COMMIT_JOURNAL_SCHEMA_VERSION
        or data.get("kind") != "task-maintenance-exact-commit"
        or data.get("operation") not in {"gc", "reconciliation", "restore"}
        or not isinstance(data.get("head"), str)
        or re.fullmatch(r"[0-9a-f]{40,64}", data["head"]) is None
        or not isinstance(data.get("newHead"), str)
        or re.fullmatch(r"[0-9a-f]{40,64}", data["newHead"]) is None
        or not isinstance(data.get("branchRef"), str)
        or not data["branchRef"].startswith("refs/heads/")
        or not isinstance(data.get("paths"), list)
        or not data["paths"]
        or data["paths"] != sorted(set(data["paths"]))
        or any(not _safe_exact_commit_path(path_value) for path_value in data["paths"])
        or not isinstance(data.get("expectedFiles"), list)
        or not data["expectedFiles"]
        or any(
            not _safe_exact_commit_path(path_value)
            or not _path_is_within(path_value, set(data["paths"]))
            for path_value in data["expectedFiles"]
        )
        or not isinstance(data.get("indexFingerprint"), str)
        or re.fullmatch(r"[0-9a-f]{64}", data["indexFingerprint"]) is None
        or not isinstance(data.get("worktreeFingerprint"), str)
        or re.fullmatch(r"[0-9a-f]{64}", data["worktreeFingerprint"]) is None
        or not isinstance(data.get("result"), dict)
    ):
        raise TaskLifecycleError("git-transaction-invalid", "maintenance 提交恢复记录损坏")
    return data


def _matches_exact_commit(repo_root: Path, commit: str, journal: dict[str, Any]) -> bool:
    """判断提交是否与共享收尾记录的 parent 和文件集完全一致。"""
    if commit != journal["newHead"]:
        return False
    code, parent, _ = run_git(["rev-parse", f"{commit}^"], cwd=repo_root)
    return bool(
        code == 0
        and parent.strip() == journal["head"]
        and _commit_files(repo_root, commit) == set(journal["expectedFiles"])
    )


def _worktree_matches_exact_commit(repo_root: Path, journal: dict[str, Any]) -> bool:
    """使用隔离 index 判断候选工作树是否仍与待应用提交一致。"""
    with tempfile.TemporaryDirectory(prefix="trellis-maintenance-recovery-index-") as temp_dir:
        index_file = Path(temp_dir) / "index"
        code, _, _ = _run_git_with_index(["read-tree", journal["head"]], repo_root, index_file)
        if code != 0:
            return False
        # read-tree 生成的临时 index 没有工作树 stat 缓存，直接 diff-files 会把
        # 内容相同的文件误判为修改；在隔离 index 重加候选后比较整棵 tree。
        code, _, _ = _run_git_with_index(
            ["add", "-A", "--", *journal["paths"]],
            repo_root,
            index_file,
        )
        if code != 0:
            return False
        code, tree, _ = _run_git_with_index(["write-tree"], repo_root, index_file)
        if code != 0 or not tree.strip():
            return False
        code, expected_tree, _ = run_git(["rev-parse", f"{journal['newHead']}^{{tree}}"], cwd=repo_root)
        return code == 0 and tree.strip() == expected_tree.strip()


def _maintenance_fingerprint_roots(paths: list[str]) -> set[str]:
    """返回维护事务指纹需要忽略的候选与内部 journal 路径。"""
    return {*paths, EXACT_COMMIT_JOURNAL_RELATIVE}


def _refresh_exact_commit_index(
    repo_root: Path,
    new_head: str,
    paths: list[str],
    index_fingerprint: Optional[str] = None,
    worktree_fingerprint: Optional[str] = None,
) -> None:
    """把候选路径的真实 index 对齐到提交，同时保持候选外状态不变。

    Args:
        repo_root: Git worktree 根目录。
        new_head: 候选路径已经提交到的 HEAD。
        paths: 需要收敛 staged 状态的精确 pathspec。
        index_fingerprint: 可选的候选外 index 预期摘要。
        worktree_fingerprint: 可选的候选外工作树预期摘要。
    """
    excluded = _maintenance_fingerprint_roots(paths)
    expected_index = index_fingerprint or _index_fingerprint(repo_root, excluded)
    expected_worktree = worktree_fingerprint or _worktree_fingerprint(repo_root, excluded)
    if _index_fingerprint(repo_root, excluded) != expected_index:
        raise TaskLifecycleError("unrelated-index-changed", "maintenance 恢复前候选外 staged/index 状态发生变化")
    if _worktree_fingerprint(repo_root, excluded) != expected_worktree:
        raise TaskLifecycleError("unrelated-worktree-changed", "maintenance 恢复前候选外 dirty 状态发生变化")
    code, _, error = run_git(
        ["reset", "--quiet", new_head, "--", *paths],
        cwd=repo_root,
    )
    if code != 0:
        raise TaskLifecycleError("git-index-refresh-failed", error.strip() or "无法刷新 maintenance 路径 index")
    code, staged, error = run_git(
        ["diff", "--cached", "--name-only", "--no-renames", "-z", "--", *paths],
        cwd=repo_root,
    )
    if code != 0 or any(path for path in staged.split("\0") if path):
        raise TaskLifecycleError("git-index-refresh-incomplete", error.strip() or "maintenance 候选暂存区未完全收敛")
    if _index_fingerprint(repo_root, excluded) != expected_index:
        raise TaskLifecycleError("unrelated-index-changed", "maintenance 恢复后候选外 staged/index 状态发生变化")
    if _worktree_fingerprint(repo_root, excluded) != expected_worktree:
        raise TaskLifecycleError("unrelated-worktree-changed", "maintenance 恢复后候选外 dirty 状态发生变化")


def _finalize_exact_commit_transaction(repo_root: Path, journal: dict[str, Any]) -> dict[str, Any]:
    """对齐真实 index、复核事务不变量并清理共享收尾记录。"""
    code, branch_ref, _ = run_git(["symbolic-ref", "--quiet", "HEAD"], cwd=repo_root)
    if code != 0 or branch_ref.strip() != journal["branchRef"]:
        raise TaskLifecycleError("git-transaction-branch-changed", "maintenance 提交恢复时当前分支已变化")
    code, head, _ = run_git(["rev-parse", "HEAD"], cwd=repo_root)
    if code != 0 or head.strip() != journal["newHead"]:
        raise TaskLifecycleError("git-transaction-head-changed", "maintenance 提交恢复时 HEAD 不匹配")
    if not _matches_exact_commit(repo_root, head.strip(), journal):
        raise TaskLifecycleError("git-transaction-commit-mismatch", "maintenance 提交恢复证据不匹配")
    if not _worktree_matches_exact_commit(repo_root, journal):
        raise TaskLifecycleError("git-transaction-worktree-mismatch", "maintenance 候选工作树与已提交内容不一致")
    _refresh_exact_commit_index(
        repo_root,
        journal["newHead"],
        journal["paths"],
        journal["indexFingerprint"],
        journal["worktreeFingerprint"],
    )
    _exact_commit_journal_path(repo_root).unlink()
    return {
        "status": "committed",
        "operation": journal["operation"],
        "commit": journal["newHead"],
        "result": dict(journal["result"]),
    }


def _recover_exact_commit_transaction(repo_root: Path) -> Optional[dict[str, Any]]:
    """恢复已创建提交对象但尚未完整收尾的共享 Git 事务。"""
    journal = _validated_exact_commit_journal(repo_root)
    if journal is None:
        return None
    code, branch_ref, _ = run_git(["symbolic-ref", "--quiet", "HEAD"], cwd=repo_root)
    if code != 0 or branch_ref.strip() != journal["branchRef"]:
        raise TaskLifecycleError("git-transaction-branch-changed", "maintenance 提交恢复需要回到事务开始分支")
    code, fixed_head, error = run_git(["rev-parse", journal["branchRef"]], cwd=repo_root)
    if code != 0 or not fixed_head.strip():
        raise TaskLifecycleError("git-transaction-ref-invalid", error.strip() or "无法读取 maintenance 固定分支")
    current_head = fixed_head.strip()
    if current_head == journal["head"]:
        excluded = _maintenance_fingerprint_roots(journal["paths"])
        if not _matches_exact_commit(repo_root, journal["newHead"], journal):
            raise TaskLifecycleError("git-transaction-commit-mismatch", "maintenance 待恢复提交证据不匹配")
        if _index_fingerprint(repo_root, excluded) != journal["indexFingerprint"]:
            raise TaskLifecycleError("unrelated-index-changed", "maintenance 恢复前候选外 staged/index 状态发生变化")
        if _worktree_fingerprint(repo_root, excluded) != journal["worktreeFingerprint"]:
            raise TaskLifecycleError("unrelated-worktree-changed", "maintenance 恢复前候选外 dirty 状态发生变化")
        if not _worktree_matches_exact_commit(repo_root, journal):
            raise TaskLifecycleError("git-transaction-worktree-mismatch", "maintenance 候选工作树与待恢复提交不一致")
        code, _, error = run_git(
            [
                "update-ref",
                "-m",
                "recover task maintenance commit",
                journal["branchRef"],
                journal["newHead"],
                journal["head"],
            ],
            cwd=repo_root,
        )
        if code != 0:
            raise TaskLifecycleError("git-ref-update-failed", error.strip() or "无法恢复固定 maintenance 分支")
    elif current_head != journal["newHead"]:
        raise TaskLifecycleError("git-transaction-head-changed", "maintenance 固定分支已发生非事务变化")
    return _finalize_exact_commit_transaction(repo_root, journal)


def _discard_unapplied_exact_commit_journal(repo_root: Path, baseline: dict[str, str]) -> None:
    """同步补偿已完成时清理尚未更新引用的共享提交记录。"""
    journal = _validated_exact_commit_journal(repo_root)
    if journal is None or journal["head"] != baseline["head"] or journal["branchRef"] != baseline["branchRef"]:
        return
    code, fixed_head, _ = run_git(["rev-parse", journal["branchRef"]], cwd=repo_root)
    if code == 0 and fixed_head.strip() == journal["head"]:
        _exact_commit_journal_path(repo_root).unlink()


def _commit_exact_paths(
    repo_root: Path,
    paths: list[str],
    expected_files: set[str],
    message: str,
    baseline: dict[str, str],
    transaction: dict[str, Any],
) -> str:
    """把给定 pathspec 原子提交到固定分支引用。

    Args:
        repo_root: Git worktree 根目录。
        paths: 精确 source/destination pathspec。
        expected_files: 预期出现在提交中的文件全集。
        message: commit message。
        baseline: 事务开始时固定的完整分支引用与 HEAD。
        transaction: 操作类型与可恢复结果。

    Returns:
        新提交哈希。
    """
    unique_paths = sorted(set(paths))
    old_head = baseline["head"]
    excluded = _maintenance_fingerprint_roots(unique_paths)
    index_before = _index_fingerprint(repo_root, excluded)
    worktree_before = _worktree_fingerprint(repo_root, excluded)
    _assert_git_baseline(repo_root, baseline)
    with tempfile.TemporaryDirectory(prefix="trellis-maintenance-index-") as temp_dir:
        index_file = Path(temp_dir) / "index"
        code, _, error = _run_git_with_index(["read-tree", old_head], repo_root, index_file)
        if code != 0:
            raise TaskLifecycleError("git-index-init-failed", error.strip() or "无法初始化 maintenance 临时 index")
        code, _, error = _run_git_with_index(
            ["add", "-A", "--", *unique_paths],
            repo_root,
            index_file,
        )
        if code != 0:
            raise TaskLifecycleError("git-add-failed", error.strip() or "无法暂存 maintenance 路径")
        code, staged, error = _run_git_with_index(
            ["diff", "--cached", "--name-only", "--no-renames", "-z", "--", *unique_paths],
            repo_root,
            index_file,
        )
        staged_files = {path for path in staged.split("\0") if path}
        if code != 0 or staged_files != expected_files:
            raise TaskLifecycleError(
                "git-fileset-mismatch",
                error.strip() or f"maintenance staged 文件集不匹配：{sorted(staged_files)}",
            )
        code, tree, error = _run_git_with_index(["write-tree"], repo_root, index_file)
        if code != 0 or not tree.strip():
            raise TaskLifecycleError("git-tree-write-failed", error.strip() or "无法创建 maintenance tree")
        code, new_head, error = run_git(
            ["commit-tree", tree.strip(), "-p", old_head, "-m", message],
            cwd=repo_root,
        )
        if code != 0 or not new_head.strip():
            raise TaskLifecycleError("git-commit-failed", error.strip() or "精确 maintenance commit 失败")
        new_head = new_head.strip()
        journal = _write_exact_commit_journal(
            repo_root,
            baseline,
            new_head,
            unique_paths,
            expected_files,
            index_before,
            worktree_before,
            transaction,
        )
        # commit-tree 只创建对象；update-ref 以旧 HEAD 为 CAS 前提，把提交原子绑定到
        # 事务开始时的分支，而不是提交瞬间可能已切换的当前 HEAD。
        _assert_git_baseline(repo_root, baseline)
        code, _, error = run_git(
            ["update-ref", "-m", message, baseline["branchRef"], new_head, old_head],
            cwd=repo_root,
        )
        if code != 0:
            _assert_git_baseline(repo_root, baseline)
            raise TaskLifecycleError("git-ref-update-failed", error.strip() or "无法更新固定 maintenance 分支")
    _finalize_exact_commit_transaction(repo_root, journal)
    return new_head


@contextlib.contextmanager
def maintenance_lock(repo_root: Path) -> Iterator[bool]:
    """获取跨进程 maintenance 锁；锁忙时 yield False。"""
    lock_path = repo_root / ".trellis/.runtime/task-gc.lock"
    lock_path.parent.mkdir(parents=True, exist_ok=True)
    acquired = False
    for _ in range(2):
        try:
            descriptor = os.open(str(lock_path), os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
        except FileExistsError:
            try:
                stale = time.time() - lock_path.stat().st_mtime > LOCK_STALE_SECONDS
            except OSError:
                stale = False
            if stale:
                try:
                    lock_path.unlink()
                except OSError:
                    pass
                continue
            break
        else:
            with os.fdopen(descriptor, "w", encoding="utf-8") as handle:
                handle.write(json.dumps({"pid": os.getpid(), "createdAt": utc_now()}) + "\n")
            acquired = True
            break
    try:
        yield acquired
    finally:
        if acquired:
            try:
                lock_path.unlink()
            except FileNotFoundError:
                pass


def _active_session_refs(repo_root: Path) -> tuple[set[str], bool]:
    """返回全部 session task 引用和 runtime 是否损坏。"""
    sessions = repo_root / ".trellis/.runtime/sessions"
    refs: set[str] = set()
    corrupt = False
    if not sessions.is_dir():
        return refs, corrupt
    for path in sorted(sessions.glob("*.json")):
        data = read_json(path)
        if not isinstance(data, dict):
            corrupt = True
            continue
        value = data.get("current_task")
        if isinstance(value, str) and value.strip():
            normalized = _normalize_task_ref(value)
            task_data = read_json(repo_root / normalized / FILE_TASK_JSON)
            if isinstance(task_data, dict) and is_task_closed(task_data):
                continue
            refs.add(normalized)
    return refs, corrupt


def _runtime_repository(repo_root: Path, value: Any) -> Optional[Path]:
    """把 runtime 仓库引用解析为项目内真实目录。"""
    if not isinstance(value, str) or not value or "\\" in value:
        return None
    if value == ".":
        return repo_root.resolve()
    parts = value.split("/")
    if any(not part or part in {".", ".."} for part in parts):
        return None
    try:
        candidate = repo_root.joinpath(*parts).resolve(strict=True)
        candidate.relative_to(repo_root.resolve())
    except (OSError, RuntimeError, ValueError):
        return None
    return candidate if candidate.is_dir() else None


def _has_valid_recorded_commits(repo_root: Path, item: dict[str, Any]) -> bool:
    """验证 completed item 的全部本地 commit 仍存在于声明仓库。"""
    commits = item.get("commits") if isinstance(item.get("commits"), list) else []
    entries = commits or ([{"repository": ".", "commit": item.get("commit")}] if item.get("commit") else [])
    if not entries:
        return False
    for entry in entries:
        if not isinstance(entry, dict):
            return False
        repository = _runtime_repository(repo_root, entry.get("repository", "."))
        commit = entry.get("commit")
        if repository is None or not isinstance(commit, str) or not commit:
            return False
        code, _, _ = run_git(["cat-file", "-e", f"{commit}^{{commit}}"], cwd=repository)
        if code != 0:
            return False
    return True


def _task_json_sha256(task_json: Path) -> Optional[str]:
    """返回 task.json 当前字节摘要；不可读时返回 None。"""
    try:
        return hashlib.sha256(task_json.read_bytes()).hexdigest()
    except OSError:
        return None


def _auto_loop_evidence(
    repo_root: Path,
) -> tuple[set[str], dict[str, str], dict[str, dict[str, Any]], set[str], bool]:
    """读取 runner 已 record、新旧完成证据和仍有未闭合 action 的任务。"""
    runtime = repo_root / ".trellis/.runtime/auto-loop"
    recorded: set[str] = set()
    closed_task_digests: dict[str, str] = {}
    legacy_completed: dict[str, dict[str, Any]] = {}
    outstanding: set[str] = set()
    corrupt = False
    if not runtime.is_dir():
        return recorded, closed_task_digests, legacy_completed, outstanding, corrupt
    for path in sorted(runtime.glob("*.json")):
        data = read_json(path)
        if not isinstance(data, dict):
            corrupt = True
            continue
        queue = data.get("queue") if isinstance(data.get("queue"), list) else []
        completed_in_run: dict[str, dict[str, Any]] = {}
        for item in queue:
            if not isinstance(item, dict):
                continue
            task = item.get("task")
            if not isinstance(task, str) or not task:
                continue
            normalized = _normalize_task_ref(task)
            if item.get("status") == "completed":
                if _has_valid_recorded_commits(repo_root, item):
                    recorded.add(normalized)
                    completed_in_run[normalized] = {
                        "run_id": data.get("run_id"),
                        "item": item,
                    }
                    if (
                        item.get("close_result") in {"closed", "already-closed"}
                        and not item.get("close_blockers")
                    ):
                        digest = item.get("task_json_sha256")
                        if digest is None:
                            continue
                        if not isinstance(digest, str) or re.fullmatch(r"[0-9a-f]{64}", digest) is None:
                            corrupt = True
                            continue
                        previous = closed_task_digests.get(normalized)
                        if previous is not None and previous != digest:
                            corrupt = True
                            continue
                        closed_task_digests[normalized] = digest
            if item.get("status") in {"pending", "running"} and isinstance(item.get("last_action"), dict):
                outstanding.add(normalized)
        legacy = data.get("pending_archive")
        if isinstance(legacy, dict):
            for task in legacy.get("tasks_awaiting_archive", []):
                if isinstance(task, str) and task:
                    normalized = _normalize_task_ref(task)
                    # 旧 handoff 只作定位提示；仍必须有 queue record + commit 证据。
                    if normalized not in recorded:
                        corrupt = True
                        continue
                    evidence = completed_in_run.get(normalized)
                    if evidence is None:
                        corrupt = True
                        continue
                    previous = legacy_completed.get(normalized)
                    if previous is not None and previous != evidence:
                        corrupt = True
                        continue
                    legacy_completed[normalized] = evidence
    return recorded, closed_task_digests, legacy_completed, outstanding, corrupt


def _legacy_commit_summaries(item: dict[str, Any], *, full: bool) -> list[str]:
    """重建旧 auto-loop progress 使用的稳定仓库提交摘要。"""
    values = item.get("commits") if isinstance(item.get("commits"), list) else []
    summaries: list[str] = []
    for entry in values:
        if not isinstance(entry, dict):
            continue
        repository = str(entry.get("repository") or ".")
        commit = str(entry.get("commit") or "")
        if commit:
            summaries.append(f"{repository}:{commit if full else commit[:7]}")
    return summaries


def _legacy_runner_bookkeeping_matches(
    repo_root: Path,
    task_ref: str,
    current: dict[str, Any],
    evidence: Optional[dict[str, Any]],
) -> bool:
    """验证旧 runner 只改写了可重建的完成态、进度与 snapshot 清理字段。"""
    if not isinstance(evidence, dict) or not isinstance(evidence.get("item"), dict):
        return False
    item = evidence["item"]
    code, base_text, _ = run_git(["show", f"HEAD:{task_ref}/{FILE_TASK_JSON}"], cwd=repo_root)
    if code != 0:
        return False
    try:
        base = json.loads(base_text)
    except (TypeError, json.JSONDecodeError):
        return False
    if not isinstance(base, dict) or base.get("status") not in {"in_progress", "completed"}:
        return False
    if current.get("status") != "completed" or not re.fullmatch(r"[0-9]{4}-[0-9]{2}-[0-9]{2}", str(current.get("completedAt") or "")):
        return False

    progress = current.get("progress")
    expected_progress_fields = {"updatedAt", "completedSteps", "partialStep", "nextStep", "notes"}
    if not isinstance(progress, dict) or set(progress) != expected_progress_fields:
        return False
    if _parse_timestamp(progress.get("updatedAt")) is None or progress.get("partialStep") is not None:
        return False
    if progress.get("nextStep") != "auto-loop 已本地提交并置为本地完成态；需要用户显式运行 finish-work/archive 完成归档":
        return False

    commit = str(item.get("commit") or "")
    short_commit = commit[:7] if commit else "unknown"
    summaries = _legacy_commit_summaries(item, full=False)
    completed_label = ", ".join(summaries) if summaries else short_commit
    if progress.get("completedSteps") != [f"auto-loop: 本地提交完成 {completed_label}"]:
        return False
    notes = progress.get("notes")
    if not isinstance(notes, str):
        return False
    note_fields = {}
    for part in notes.split("; "):
        key, separator, value = part.partition("=")
        if separator:
            note_fields[key] = value
    if note_fields.get("run_id") != str(evidence.get("run_id")) or note_fields.get("task") != task_ref:
        return False
    if note_fields.get("commit") != (commit or "unknown") or not note_fields.get("status"):
        return False
    full_summaries = _legacy_commit_summaries(item, full=True)
    if full_summaries and note_fields.get("commits") != ",".join(full_summaries):
        return False
    if not full_summaries and "commits" in note_fields:
        return False

    expected = dict(base)
    expected["status"] = "completed"
    if not expected.get("completedAt"):
        expected["completedAt"] = current["completedAt"]
    expected["progress"] = progress
    expected.pop("last_push_snapshot", None)
    return expected == current


def _write_batch_and_commit(
    repo_root: Path,
    writes: list[tuple[Path, dict[str, Any]]],
    message: str,
    baseline: dict[str, str],
    transaction: dict[str, Any],
    *,
    commit_paths: Optional[list[str]] = None,
    expected_files: Optional[set[str]] = None,
) -> str:
    """原子写入一批 JSON，并以可恢复的精确 pathspec 创建本地提交。

    Args:
        repo_root: Git worktree 根目录。
        writes: 需要原子写入的 JSON 路径与数据。
        message: maintenance commit 消息。
        baseline: 事务开始时固定的完整分支引用与 HEAD。
        transaction: 操作类型与可恢复结果。
        commit_paths: 可选的精确提交 pathspec；缺省仅提交写入的 JSON。
        expected_files: 可选的预期提交文件全集；缺省等于写入的 JSON。

    Returns:
        新创建并绑定到固定分支的提交哈希。
    """
    originals = [(path, path.read_bytes()) for path, _ in writes]
    relative_paths = [_task_relative(repo_root, path) for path, _ in writes]
    exact_paths = relative_paths if commit_paths is None else commit_paths
    exact_files = set(relative_paths) if expected_files is None else expected_files
    try:
        for path, data in writes:
            if not write_json(path, data):
                raise TaskLifecycleError("write-failed", f"无法写入：{_task_relative(repo_root, path)}")
        return _commit_exact_paths(
            repo_root,
            exact_paths,
            exact_files,
            message,
            baseline,
            transaction,
        )
    except Exception:
        code, head_now, _ = run_git(["rev-parse", "HEAD"], cwd=repo_root)
        if code == 0 and head_now.strip() == baseline["head"]:
            for path, payload in originals:
                _atomic_write_bytes(path, payload)
            _discard_unapplied_exact_commit_journal(repo_root, baseline)
        raise


def _atomic_write_bytes(path: Path, payload: bytes) -> None:
    """原子恢复文件原始字节。"""
    descriptor, temporary = tempfile.mkstemp(prefix=f".{path.name}.", suffix=".tmp", dir=str(path.parent))
    temp_path = Path(temporary)
    try:
        with os.fdopen(descriptor, "wb") as handle:
            handle.write(payload)
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(str(temp_path), str(path))
    except Exception:
        try:
            temp_path.unlink()
        except FileNotFoundError:
            pass
        raise


def reconcile_legacy_tasks(repo_root: Path, migration_time: Optional[str] = None) -> dict[str, Any]:
    """把缺少 closeout 的旧顶层任务收敛为显式新 schema。

    Args:
        repo_root: 项目根目录。
        migration_time: 测试使用的固定迁移时间。

    Returns:
        迁移、阻断、延后与 commit 摘要。
    """
    result: dict[str, Any] = {"migrated": [], "blocked": [], "deferred": [], "commit": None}
    if _exact_commit_journal_path(repo_root).exists():
        if _gc_journal_path(repo_root).exists():
            raise TaskLifecycleError("gc-recovery-required", "旧任务收敛前必须先恢复中断的 GC 事务")
        recovery = _recover_exact_commit_transaction(repo_root)
        if recovery and recovery["operation"] == "reconciliation":
            recovered_result = recovery["result"]
            for key in ("migrated", "blocked", "deferred"):
                if isinstance(recovered_result.get(key), list):
                    result[key].extend(recovered_result[key])
            result["commit"] = recovery["commit"]
            result["recovery"] = "committed"
    tasks_dir = get_tasks_dir(repo_root)
    if not tasks_dir.is_dir():
        return result
    legacy_dirs: list[tuple[Path, dict[str, Any]]] = []
    for task_dir in sorted(tasks_dir.iterdir()):
        if not task_dir.is_dir() or task_dir.is_symlink() or task_dir.name == "archive":
            continue
        task_json = task_dir / FILE_TASK_JSON
        data = read_json(task_json)
        task_ref = _task_relative(repo_root, task_dir)
        if not isinstance(data, dict):
            result["deferred"].append({"task": task_ref, "reason": "invalid-task-json"})
            continue
        if "closeout" in data:
            if not normalize_closeout(data)["valid"]:
                result["deferred"].append({"task": task_ref, "reason": "invalid-closeout"})
            continue
        legacy_dirs.append((task_dir, data))
    if not legacy_dirs:
        return result
    baseline = _git_preflight(repo_root)
    recorded, recorded_digests, legacy_completed, _, runtime_corrupt = _auto_loop_evidence(repo_root)
    if runtime_corrupt:
        result["deferred"].append({"reason": "auto-loop-runtime-corrupt"})
        return result
    timestamp = migration_time or utc_now()
    writes: list[tuple[Path, dict[str, Any]]] = []
    commit_paths: list[str] = []
    expected_files: set[str] = set()
    for task_dir, data in legacy_dirs:
        task_json = task_dir / FILE_TASK_JSON
        task_ref = _task_relative(repo_root, task_dir)
        task_json_ref = f"{task_ref}/{FILE_TASK_JSON}"
        path_status = _git_path_status(repo_root, [task_ref])
        status_records = _status_records(path_status)
        dirty_paths = {
            path
            for _, paths in status_records
            for path in paths
        }
        tracked_files = _tracked_files(repo_root, task_ref)
        untracked_files = set(_untracked_files(repo_root, task_ref)) if not tracked_files else set()
        entirely_untracked = bool(
            status_records
            and not tracked_files
            and task_json_ref in untracked_files
            and dirty_paths == untracked_files
            and all(git_status == "??" for git_status, _ in status_records)
        )
        runner_owned = (
            task_ref in recorded
            and dirty_paths == {task_json_ref}
            and (
                recorded_digests.get(task_ref) == _task_json_sha256(task_json)
                or _legacy_runner_bookkeeping_matches(
                    repo_root,
                    task_ref,
                    data,
                    legacy_completed.get(task_ref),
                )
            )
        )
        if path_status and not runner_owned and not entirely_untracked:
            result["deferred"].append({"task": task_ref, "reason": "candidate-dirty"})
            continue
        if not tracked_files and not entirely_untracked:
            result["deferred"].append({"task": task_ref, "reason": "candidate-untracked"})
            continue
        next_data = dict(data)
        status = data.get("status")
        if status in {"planning", "in_progress"}:
            next_data["closeout"] = pending_closeout()
            migration_status = "pending"
        elif status == "completed":
            evaluated = evaluate_close(
                task_dir,
                repo_root,
                next_data,
                delivery_verified=not path_status or runner_owned or entirely_untracked,
                closed_at=timestamp,
            )
            if evaluated["status"] == "error":
                result["deferred"].append({"task": task_ref, "reason": "close-evaluation-error"})
                continue
            next_data["closeout"] = evaluated["closeout"]
            migration_status = evaluated["closeout"]["status"]
            if migration_status == "blocked":
                result["blocked"].append({"task": task_ref, "blockers": evaluated["blockers"]})
        else:
            result["deferred"].append({"task": task_ref, "reason": "unknown-task-status"})
            continue
        writes.append((task_json, next_data))
        if entirely_untracked:
            # 新任务尚无 HEAD 基线，只能把可纳管的完整目录作为一个原子任务记录提交。
            commit_paths.append(task_ref)
            expected_files.update(untracked_files)
        else:
            commit_paths.append(task_json_ref)
            expected_files.add(task_json_ref)
        result["migrated"].append({"task": task_ref, "closeout": migration_status})
    if writes:
        result["commit"] = _write_batch_and_commit(
            repo_root,
            writes,
            "chore(task): reconcile closeout metadata",
            baseline,
            {"operation": "reconciliation", "result": result},
            commit_paths=commit_paths,
            expected_files=expected_files,
        )
    return result


def _directory_manifest(root: Path) -> dict[str, tuple[str, str]]:
    """计算目录内容清单，用于识别可幂等收敛的 GC 目标。

    Args:
        root: 待比较的真实目录。

    Returns:
        相对路径到节点类型与内容摘要的映射。

    Raises:
        TaskLifecycleError: 目录包含无法安全比较的特殊节点。
    """
    manifest: dict[str, tuple[str, str]] = {}
    for current, directories, files in os.walk(root, followlinks=False):
        current_path = Path(current)
        for name in list(directories):
            path = current_path / name
            if path.is_symlink():
                relative = path.relative_to(root).as_posix()
                manifest[relative] = ("symlink", os.readlink(path))
                directories.remove(name)
        for name in files:
            path = current_path / name
            relative = path.relative_to(root).as_posix()
            if path.is_symlink():
                manifest[relative] = ("symlink", os.readlink(path))
            elif path.is_file():
                manifest[relative] = ("file", hashlib.sha256(path.read_bytes()).hexdigest())
            else:
                raise TaskLifecycleError("unsupported-task-entry", f"任务包含特殊文件：{path}")
    return manifest


def _gc_candidates(repo_root: Path, before: timedelta, now: datetime) -> tuple[list[dict[str, Any]], list[dict[str, Any]]]:
    """计算物理 GC 候选与延后诊断。"""
    candidates: list[dict[str, Any]] = []
    deferred: list[dict[str, Any]] = []
    tasks_dir = get_tasks_dir(repo_root)
    session_refs, sessions_corrupt = _active_session_refs(repo_root)
    _, closed_task_digests, _, outstanding, runtime_corrupt = _auto_loop_evidence(repo_root)
    if sessions_corrupt or runtime_corrupt:
        return [], [{"reason": "runtime-corrupt"}]
    cutoff = now - before
    for task_dir in sorted(tasks_dir.iterdir()) if tasks_dir.is_dir() else []:
        if not task_dir.is_dir() or task_dir.is_symlink() or task_dir.name == "archive":
            continue
        data = read_json(task_dir / FILE_TASK_JSON)
        if not isinstance(data, dict):
            deferred.append({"task": _task_relative(repo_root, task_dir), "reason": "invalid-task-json"})
            continue
        closeout = normalize_closeout(data)
        task_ref = _task_relative(repo_root, task_dir)
        if not closeout["valid"]:
            deferred.append({"task": task_ref, "reason": "invalid-closeout"})
            continue
        if closeout["status"] != "closed":
            continue
        closed_at = _parse_timestamp(closeout["closedAt"])
        if closed_at is None:
            deferred.append({"task": task_ref, "reason": "invalid-closed-at"})
            continue
        if closed_at > now:
            deferred.append({"task": task_ref, "reason": "future-closed-at"})
            continue
        if closed_at > cutoff:
            continue
        if task_ref in session_refs:
            deferred.append({"task": task_ref, "reason": "active-session-reference"})
            continue
        if task_ref in outstanding:
            deferred.append({"task": task_ref, "reason": "auto-loop-action-outstanding"})
            continue
        path_status = _git_path_status(repo_root, [task_ref])
        if path_status:
            dirty_paths = {
                path
                for _, paths in _status_records(path_status)
                for path in paths
            }
            expected_digest = closed_task_digests.get(task_ref)
            if (
                dirty_paths != {f"{task_ref}/{FILE_TASK_JSON}"}
                or expected_digest is None
                or expected_digest != _task_json_sha256(task_dir / FILE_TASK_JSON)
            ):
                deferred.append({"task": task_ref, "reason": "candidate-dirty"})
                continue
        tracked = _tracked_files(repo_root, task_ref)
        if not tracked:
            deferred.append({"task": task_ref, "reason": "candidate-untracked"})
            continue
        bucket = closed_at.strftime("%Y-%m")
        destination = tasks_dir / "archive" / bucket / task_dir.name
        destination_ref = _task_relative(repo_root, destination)
        mode = "move"
        destination_tracked: set[str] = set()
        if destination.exists():
            if (
                not destination.is_dir()
                or destination.is_symlink()
                or _directory_manifest(task_dir) != _directory_manifest(destination)
            ):
                deferred.append({"task": task_ref, "reason": "destination-conflict", "destination": destination_ref})
                continue
            destination_status = _git_path_status(repo_root, [destination_ref])
            if destination_status and any(
                status != "??" for status, _ in _status_records(destination_status)
            ):
                deferred.append({"task": task_ref, "reason": "destination-dirty", "destination": destination_ref})
                continue
            destination_tracked = set(_tracked_files(repo_root, destination_ref))
            mode = "dedupe"
        expected = set(tracked)
        for source_file in tracked:
            suffix = source_file[len(task_ref):].lstrip("/")
            destination_file = f"{destination_ref}/{suffix}" if suffix else destination_ref
            if destination_file not in destination_tracked:
                expected.add(destination_file)
        candidates.append({
            "task": task_ref,
            "source": task_dir,
            "destination": destination,
            "destination_ref": destination_ref,
            "expected": expected,
            "mode": mode,
        })
    return candidates, deferred


def _gc_journal_path(repo_root: Path) -> Path:
    """返回物理 GC 的持久化事务记录路径。"""
    return repo_root / GC_JOURNAL_RELATIVE


def _write_gc_journal(
    repo_root: Path,
    baseline: dict[str, str],
    candidates: list[dict[str, Any]],
    paths: list[str],
    expected: set[str],
) -> None:
    """在任何目录移动前原子保存可恢复的最小 GC 事务证据。"""
    journal = {
        "schemaVersion": GC_JOURNAL_SCHEMA_VERSION,
        "kind": "task-closed-gc",
        "createdAt": utc_now(),
        "head": baseline["head"],
        "branchRef": baseline["branchRef"],
        "paths": sorted(set(paths)),
        "expectedFiles": sorted(expected),
        "candidates": [
            {
                "task": item["task"],
                "destination": item["destination_ref"],
                "mode": item["mode"],
            }
            for item in candidates
        ],
    }
    path = _gc_journal_path(repo_root)
    path.parent.mkdir(parents=True, exist_ok=True)
    payload = (json.dumps(journal, ensure_ascii=False, indent=2, sort_keys=True) + "\n").encode("utf-8")
    _atomic_write_bytes(path, payload)


def _validated_gc_journal(repo_root: Path) -> Optional[dict[str, Any]]:
    """读取并严格验证 GC journal；不存在时返回 None。"""
    path = _gc_journal_path(repo_root)
    if not path.exists():
        return None
    data = read_json(path)
    if (
        not isinstance(data, dict)
        or data.get("schemaVersion") != GC_JOURNAL_SCHEMA_VERSION
        or data.get("kind") != "task-closed-gc"
        or not isinstance(data.get("head"), str)
        or re.fullmatch(r"[0-9a-f]{40,64}", data["head"]) is None
        or not isinstance(data.get("branchRef"), str)
        or not data["branchRef"].startswith("refs/heads/")
        or not isinstance(data.get("paths"), list)
        or not isinstance(data.get("expectedFiles"), list)
        or not isinstance(data.get("candidates"), list)
        or not data["candidates"]
    ):
        raise TaskLifecycleError("gc-recovery-invalid", "物理 GC 恢复记录损坏")
    normalized_candidates = []
    derived_paths = []
    for item in data["candidates"]:
        if not isinstance(item, dict):
            raise TaskLifecycleError("gc-recovery-invalid", "物理 GC 恢复候选损坏")
        source_ref = item.get("task")
        destination_ref = item.get("destination")
        mode = item.get("mode")
        if not isinstance(source_ref, str) or not isinstance(destination_ref, str) or mode not in {"move", "dedupe"}:
            raise TaskLifecycleError("gc-recovery-invalid", "物理 GC 恢复候选字段损坏")
        source_parts = source_ref.split("/")
        destination_parts = destination_ref.split("/")
        if (
            len(source_parts) != 3
            or source_parts[:2] != [".trellis", "tasks"]
            or source_parts[2] in {"", ".", "..", "archive"}
            or len(destination_parts) != 5
            or destination_parts[:3] != [".trellis", "tasks", "archive"]
            or re.fullmatch(r"[0-9]{4}-[0-9]{2}", destination_parts[3]) is None
            or destination_parts[4] != source_parts[2]
            or "\\" in source_ref
            or "\\" in destination_ref
        ):
            raise TaskLifecycleError("gc-recovery-invalid", "物理 GC 恢复路径越界")
        normalized_candidates.append({
            "task": source_ref,
            "destination_ref": destination_ref,
            "source": repo_root / source_ref,
            "destination": repo_root / destination_ref,
            "mode": mode,
        })
        derived_paths.extend([source_ref, destination_ref])
    if data["paths"] != sorted(set(derived_paths)):
        raise TaskLifecycleError("gc-recovery-invalid", "物理 GC 恢复 pathspec 不匹配")
    allowed_roots = set(derived_paths)
    if any(
        not isinstance(path_value, str)
        or not path_value
        or not _path_is_within(path_value, allowed_roots)
        for path_value in data["expectedFiles"]
    ):
        raise TaskLifecycleError("gc-recovery-invalid", "物理 GC 恢复文件集损坏")
    data["candidates"] = normalized_candidates
    return data


def _commit_files(repo_root: Path, commit: str) -> set[str]:
    """以 NUL 分隔协议读取一个提交的完整文件集。"""
    code, output, error = run_git(
        ["diff-tree", "--no-commit-id", "--name-only", "--no-renames", "-r", "-z", commit],
        cwd=repo_root,
    )
    if code != 0:
        raise TaskLifecycleError("git-commit-verify-failed", error.strip() or "无法读取 maintenance commit")
    return {path for path in output.split("\0") if path}


def _matches_gc_commit(
    repo_root: Path,
    commit: str,
    journal: dict[str, Any],
) -> bool:
    """判断提交是否精确对应 journal 记录的 GC 事务。"""
    if not commit or commit == journal["head"]:
        return False
    code, parent, _ = run_git(["rev-parse", f"{commit}^"], cwd=repo_root)
    return bool(
        code == 0
        and parent.strip() == journal["head"]
        and _commit_files(repo_root, commit) == set(journal["expectedFiles"])
    )


def _rollback_gc_journal(repo_root: Path, journal: dict[str, Any], reset_head: str) -> None:
    """按 journal 精确恢复当前分支的 index 与目录移动。"""
    code, _, error = run_git(
        ["reset", "--quiet", reset_head, "--", *journal["paths"]],
        cwd=repo_root,
    )
    if code != 0:
        raise TaskLifecycleError("gc-recovery-reset-failed", error.strip() or "无法恢复 GC 暂存状态")
    for item in reversed(journal["candidates"]):
        source = item["source"]
        destination = item["destination"]
        if source.is_symlink() or destination.is_symlink():
            raise TaskLifecycleError("gc-recovery-invalid", "GC 恢复路径不能是软链")
        if item["mode"] == "move":
            if source.exists() and not destination.exists():
                continue
            if not source.exists() and destination.is_dir():
                source.parent.mkdir(parents=True, exist_ok=True)
                shutil.move(str(destination), str(source))
                continue
            raise TaskLifecycleError("gc-recovery-conflict", "GC 中断后的源/目标目录状态冲突")
        if not destination.is_dir():
            raise TaskLifecycleError("gc-recovery-conflict", "GC 去重目标在中断后缺失")
        if not source.exists():
            source.parent.mkdir(parents=True, exist_ok=True)
            shutil.copytree(destination, source, symlinks=True)
        elif not source.is_dir() or _directory_manifest(source) != _directory_manifest(destination):
            raise TaskLifecycleError("gc-recovery-conflict", "GC 去重源/目标在中断后不一致")


def _recover_gc_transaction(repo_root: Path) -> Optional[dict[str, Any]]:
    """在下一次 maintenance 中识别已提交事务，或补偿未提交的目录移动。"""
    journal = _validated_gc_journal(repo_root)
    if journal is None:
        return None
    baseline = {"head": journal["head"], "branchRef": journal["branchRef"]}
    branch_code, branch_ref, _ = run_git(["symbolic-ref", "--quiet", "HEAD"], cwd=repo_root)
    code, head, _ = run_git(["rev-parse", "HEAD"], cwd=repo_root)
    if code != 0 or not head.strip():
        raise TaskLifecycleError("git-head-invalid", "GC 中断后无法读取 HEAD")
    current_head = head.strip()
    journal_path = _gc_journal_path(repo_root)
    if branch_code != 0 or branch_ref.strip() != baseline["branchRef"]:
        code, fixed_head, _ = run_git(["rev-parse", baseline["branchRef"]], cwd=repo_root)
        # 固定分支 CAS 已经成功、但工作树随后切到同一旧 HEAD 的其它分支时，
        # 先恢复当前分支的目录视图，保留 journal 等切回原分支后确认提交。
        if (
            current_head == baseline["head"]
            and code == 0
            and _matches_gc_commit(repo_root, fixed_head.strip(), journal)
        ):
            _rollback_gc_journal(repo_root, journal, current_head)
        raise TaskLifecycleError("gc-recovery-branch-changed", "GC 中断后当前分支与事务开始时不一致")

    exact_journal = _validated_exact_commit_journal(repo_root)
    if exact_journal is not None:
        if (
            exact_journal["operation"] != "gc"
            or exact_journal["head"] != journal["head"]
            or exact_journal["branchRef"] != journal["branchRef"]
            or exact_journal["paths"] != journal["paths"]
            or set(exact_journal["expectedFiles"]) != set(journal["expectedFiles"])
        ):
            raise TaskLifecycleError("gc-recovery-invalid", "GC 与共享提交恢复记录不一致")
        if current_head not in {baseline["head"], exact_journal["newHead"]}:
            raise TaskLifecycleError("gc-recovery-head-changed", "GC 中断后 HEAD 已发生非事务变化")
        # GC journal 负责目录移动，共享 journal 负责分支引用与真实 index；后者成功
        # 收尾前不能清除前者，否则 reset/index.lock 失败后将失去正常重试入口。
        _recover_exact_commit_transaction(repo_root)
        code, head, _ = run_git(["rev-parse", "HEAD"], cwd=repo_root)
        if code != 0 or not head.strip():
            raise TaskLifecycleError("git-head-invalid", "GC 提交收尾后无法读取 HEAD")
        current_head = head.strip()
    if current_head != baseline["head"]:
        if _matches_gc_commit(repo_root, current_head, journal):
            if exact_journal is None:
                # 兼容修复前已经留下的 GC journal：按精确 pathspec 收敛 index，
                # 并在操作前后校验候选外 staged/dirty 状态没有变化。
                _refresh_exact_commit_index(repo_root, current_head, journal["paths"])
            journal_path.unlink()
            return {"status": "committed", "commit": current_head, "candidates": journal["candidates"]}
        raise TaskLifecycleError("gc-recovery-head-changed", "GC 中断后 HEAD 已发生非事务变化")

    _rollback_gc_journal(repo_root, journal, baseline["head"])
    _discard_unapplied_exact_commit_journal(repo_root, baseline)
    journal_path.unlink()
    return {"status": "rolled-back", "commit": None, "candidates": journal["candidates"]}


def _committed_gc_recovery_result(recovery: dict[str, Any]) -> dict[str, Any]:
    """把已提交 GC 事务转换为稳定的命令结果。

    Args:
        recovery: ``_recover_gc_transaction`` 返回的 committed 结果。

    Returns:
        与正常 GC 成功路径一致的摘要。
    """
    return {
        "moved": [
            {"task": item["task"], "destination": item["destination_ref"]}
            for item in recovery["candidates"]
        ],
        "deferred": [],
        "commit": recovery["commit"],
        "dryRun": False,
        "recovery": "committed",
    }


def gc_closed_tasks(
    repo_root: Path,
    before_value: str = DEFAULT_GC_AGE,
    *,
    dry_run: bool = False,
    now: Optional[datetime] = None,
) -> dict[str, Any]:
    """把到期 closed 任务移动到物理 archive，并创建精确本地提交。

    Args:
        repo_root: 项目根目录。
        before_value: 最小关闭时长。
        dry_run: 是否只计算候选。
        now: 测试使用的 UTC 当前时间。

    Returns:
        moved、deferred、commit 与 dryRun 摘要。
    """
    before = parse_duration(before_value)
    recovery = None
    if _gc_journal_path(repo_root).exists():
        if dry_run:
            raise TaskLifecycleError("gc-recovery-required", "存在中断的 GC 事务；请运行非 dry-run maintenance 恢复")
        recovery = _recover_gc_transaction(repo_root)
        if recovery and recovery["status"] == "committed":
            return _committed_gc_recovery_result(recovery)
    elif _exact_commit_journal_path(repo_root).exists():
        if dry_run:
            raise TaskLifecycleError(
                "git-transaction-recovery-required",
                "存在尚未收尾的 maintenance 提交事务；请运行非 dry-run maintenance 恢复",
            )
        exact_recovery = _recover_exact_commit_transaction(repo_root)
        if exact_recovery and exact_recovery["operation"] == "gc":
            recovered_result = dict(exact_recovery["result"])
            recovered_result["recovery"] = "committed"
            return recovered_result
    current = now or datetime.now(timezone.utc)
    if current.tzinfo is None:
        current = current.replace(tzinfo=timezone.utc)
    candidates, deferred = _gc_candidates(repo_root, before, current.astimezone(timezone.utc))
    result: dict[str, Any] = {
        "moved": [],
        "deferred": deferred,
        "commit": None,
        "dryRun": dry_run,
    }
    if recovery is not None:
        result["recovery"] = recovery["status"]
    if dry_run or not candidates:
        result["candidates"] = [
            {"task": item["task"], "destination": item["destination_ref"]}
            for item in candidates
        ]
        return result
    baseline = _git_preflight(repo_root)
    moved: list[dict[str, Any]] = []
    paths: list[str] = []
    expected: set[str] = set()
    for item in candidates:
        paths.extend([item["task"], item["destination_ref"]])
        expected.update(item["expected"])
    _write_gc_journal(repo_root, baseline, candidates, paths, expected)
    try:
        for item in candidates:
            item["destination"].parent.mkdir(parents=True, exist_ok=True)
            if item["mode"] == "dedupe":
                shutil.rmtree(item["source"])
            else:
                shutil.move(str(item["source"]), str(item["destination"]))
            moved.append(item)
        result["commit"] = _commit_exact_paths(
            repo_root,
            paths,
            expected,
            "chore(task): gc closed tasks",
            baseline,
            {
                "operation": "gc",
                "result": {
                    "moved": [
                        {"task": item["task"], "destination": item["destination_ref"]}
                        for item in candidates
                    ],
                    "deferred": deferred,
                    "commit": None,
                    "dryRun": False,
                },
            },
        )
        _gc_journal_path(repo_root).unlink()
    except Exception:
        recovered = _recover_gc_transaction(repo_root)
        if recovered and recovered["status"] == "committed":
            result["commit"] = recovered["commit"]
            result["recovery"] = "committed"
            result["moved"] = [
                {"task": item["task"], "destination": item["destination_ref"]}
                for item in recovered["candidates"]
            ]
            return result
        raise
    result["moved"] = [
        {"task": item["task"], "destination": item["destination_ref"]}
        for item in moved
    ]
    return result


def _find_archived_task(repo_root: Path, task_ref: str) -> Path:
    """解析历史 archive 中唯一任务引用。"""
    archive = get_tasks_dir(repo_root) / "archive"
    raw = task_ref.strip()
    matches = []
    if not archive.is_dir() or not raw:
        raise TaskLifecycleError("task-not-found", f"归档任务不存在：{task_ref}")
    for bucket in sorted(archive.iterdir()):
        if not bucket.is_dir() or bucket.is_symlink():
            continue
        for task_dir in sorted(bucket.iterdir()):
            if not task_dir.is_dir() or task_dir.is_symlink():
                continue
            rel = _task_relative(repo_root, task_dir)
            if raw in {task_dir.name, rel, f"{bucket.name}/{task_dir.name}"} or task_dir.name.endswith(f"-{raw}"):
                matches.append(task_dir)
    if len(matches) == 1:
        return matches[0]
    if len(matches) > 1:
        raise TaskLifecycleError("task-reference-ambiguous", f"归档任务引用存在歧义：{task_ref}")
    raise TaskLifecycleError("task-not-found", f"归档任务不存在：{task_ref}")


def _restore_recovery_matches(task_ref: str, result: dict[str, Any]) -> bool:
    """判断 restore 重试引用是否指向共享 journal 中的已恢复任务。"""
    source_ref = result.get("source")
    destination_ref = result.get("destination")
    if not isinstance(source_ref, str) or not isinstance(destination_ref, str):
        return False
    source_parts = source_ref.split("/")
    destination_parts = destination_ref.split("/")
    if len(source_parts) < 2 or not destination_parts:
        return False
    raw = _normalize_task_ref(task_ref)
    name = destination_parts[-1]
    return raw in {name, source_ref, "/".join(source_parts[-2:])} or name.endswith(f"-{raw}")


def restore_task(repo_root: Path, task_ref: str, *, dry_run: bool = False) -> dict[str, Any]:
    """把物理 archive 任务恢复到顶层，并显式保留其 closed 语义。

    Args:
        repo_root: 项目根目录。
        task_ref: 归档任务目录名、简写或仓库相对引用。
        dry_run: 是否只验证并返回恢复计划。

    Returns:
        source、destination、commit、status 与可选 recovery 摘要。
    """
    if _gc_journal_path(repo_root).exists():
        raise TaskLifecycleError("gc-recovery-required", "恢复任务前必须先恢复中断的 GC 事务")
    if _exact_commit_journal_path(repo_root).exists():
        if dry_run:
            raise TaskLifecycleError(
                "git-transaction-recovery-required",
                "存在尚未收尾的 maintenance 提交事务；请先运行非 dry-run maintenance 恢复",
            )
        recovery = _recover_exact_commit_transaction(repo_root)
        if (
            recovery
            and recovery["operation"] == "restore"
            and _restore_recovery_matches(task_ref, recovery["result"])
        ):
            recovered_result = dict(recovery["result"])
            recovered_result["recovery"] = "committed"
            return recovered_result
    baseline = _git_preflight(repo_root)
    source = _find_archived_task(repo_root, task_ref)
    destination = get_tasks_dir(repo_root) / source.name
    source_ref = _task_relative(repo_root, source)
    destination_ref = _task_relative(repo_root, destination)
    if destination.exists():
        raise TaskLifecycleError("destination-conflict", f"恢复目标已存在：{destination_ref}")
    if _git_path_status(repo_root, [source_ref]):
        raise TaskLifecycleError("candidate-dirty", f"归档任务路径存在未提交差异：{source_ref}")
    tracked = _tracked_files(repo_root, source_ref)
    if not tracked:
        raise TaskLifecycleError("candidate-untracked", f"归档任务未被 Git 跟踪：{source_ref}")
    task_json = source / FILE_TASK_JSON
    task_data = read_json(task_json)
    if not isinstance(task_data, dict):
        raise TaskLifecycleError("invalid-task-json", f"归档任务 task.json 损坏：{source_ref}")
    task_json_ref = f"{source_ref}/{FILE_TASK_JSON}"
    if task_json_ref not in tracked:
        raise TaskLifecycleError("candidate-untracked", f"归档任务 task.json 未被 Git 跟踪：{source_ref}")
    original_task_json = task_json.read_bytes()
    closeout = normalize_closeout(task_data)
    materialize_closed = not (closeout["valid"] and closeout["status"] == "closed")
    expected = set(tracked)
    for source_file in tracked:
        suffix = source_file[len(source_ref):].lstrip("/")
        expected.add(f"{destination_ref}/{suffix}" if suffix else destination_ref)
    result = {
        "status": "dry-run" if dry_run else "restored",
        "source": source_ref,
        "destination": destination_ref,
        "commit": None,
    }
    if dry_run:
        return result
    try:
        shutil.move(str(source), str(destination))
        if materialize_closed:
            restored_data = dict(task_data)
            # 旧物理 archive 的关闭时间未知；restore 时只把既有隐含 closed 语义显式化，
            # 不从目录分桶或 completedAt 反推历史时间。
            restored_data["closeout"] = {
                "status": "closed",
                "closedAt": utc_now(),
                "blockers": [],
            }
            if not write_json(destination / FILE_TASK_JSON, restored_data):
                raise TaskLifecycleError("write-failed", f"无法写入恢复任务：{destination_ref}")
        result["commit"] = _commit_exact_paths(
            repo_root,
            [source_ref, destination_ref],
            expected,
            f"chore(task): restore {source.name}",
            baseline,
            {"operation": "restore", "result": result},
        )
    except Exception:
        code, head_now, _ = run_git(["rev-parse", "HEAD"], cwd=repo_root)
        if code == 0 and head_now.strip() == baseline["head"] and destination.exists() and not source.exists():
            if materialize_closed:
                _atomic_write_bytes(destination / FILE_TASK_JSON, original_task_json)
            source.parent.mkdir(parents=True, exist_ok=True)
            shutil.move(str(destination), str(source))
            _discard_unapplied_exact_commit_journal(repo_root, baseline)
        raise
    return result


def run_session_maintenance(repo_root: Path, before_value: str = DEFAULT_GC_AGE) -> dict[str, Any]:
    """在同一锁内先恢复遗留 GC，再运行 reconciliation 与新 GC。"""
    with maintenance_lock(repo_root) as acquired:
        if not acquired:
            return {"status": "busy", "reconciliation": None, "gc": None}
        recovery = None
        if _gc_journal_path(repo_root).exists():
            try:
                # 遗留事务固定了旧 HEAD；任何新 maintenance commit 都会让它无法判定归属，
                # 因此恢复必须先于 reconciliation 和本轮新 GC。
                recovery = _recover_gc_transaction(repo_root)
            except TaskLifecycleError as error:
                gc_result = {
                    "moved": [],
                    "deferred": [{"reason": error.reason}],
                    "error": str(error),
                    "commit": None,
                }
                return {"status": "ok", "reconciliation": None, "gc": gc_result}
        try:
            reconciliation = reconcile_legacy_tasks(repo_root)
        except TaskLifecycleError as error:
            reconciliation = {"migrated": [], "blocked": [], "deferred": [{"reason": error.reason}], "error": str(error)}
            # GC 的候选解释依赖 reconciliation 已经成功提交；失败后继续会让同次 SessionStart
            # 在旧 schema 上移动目录，破坏“先收敛、再整理”的顺序保证。
            gc_result = _committed_gc_recovery_result(recovery) if recovery and recovery["status"] == "committed" else None
            return {"status": "ok", "reconciliation": reconciliation, "gc": gc_result}
        if recovery and recovery["status"] == "committed":
            return {
                "status": "ok",
                "reconciliation": reconciliation,
                "gc": _committed_gc_recovery_result(recovery),
            }
        try:
            gc_result = gc_closed_tasks(repo_root, before_value)
        except TaskLifecycleError as error:
            gc_result = {"moved": [], "deferred": [{"reason": error.reason}], "error": str(error), "commit": None}
        if recovery and recovery["status"] == "rolled-back":
            gc_result["recovery"] = "rolled-back"
        return {"status": "ok", "reconciliation": reconciliation, "gc": gc_result}


def cmd_gc(args: argparse.Namespace) -> int:
    """执行 ``task.py gc``。"""
    repo_root = get_repo_root()
    try:
        if not getattr(args, "closed", False):
            raise TaskLifecycleError("selector-required", "gc 必须显式传入 --closed")
        with maintenance_lock(repo_root) as acquired:
            if not acquired:
                raise TaskLifecycleError("maintenance-busy", "另一个 task maintenance 正在运行")
            result = gc_closed_tasks(repo_root, args.before, dry_run=args.dry_run)
    except TaskLifecycleError as error:
        result = {"status": "error", "reason": error.reason, "message": str(error)}
    if getattr(args, "json", False):
        print(json.dumps(result, ensure_ascii=False, sort_keys=True))
    elif result.get("status") == "error":
        print(f"GC 失败：{result['message']}", file=sys.stderr)
    else:
        if result.get("dryRun"):
            candidates = result.get("candidates", [])
            if candidates:
                for item in candidates:
                    print(f"候选 GC：{item['task']} -> {item['destination']}")
            else:
                print("没有符合条件的 GC 候选。")
        for item in result.get("moved", []):
            print(f"✓ GC：{item['task']} -> {item['destination']}")
        for item in result.get("deferred", []):
            print(f"延后：{item.get('task') or '(global)'} ({item.get('reason')})", file=sys.stderr)
    return 1 if result.get("status") == "error" else 0


def cmd_restore(args: argparse.Namespace) -> int:
    """执行 ``task.py restore``。"""
    repo_root = get_repo_root()
    try:
        with maintenance_lock(repo_root) as acquired:
            if not acquired:
                raise TaskLifecycleError("maintenance-busy", "另一个 task maintenance 正在运行")
            result = restore_task(repo_root, args.task, dry_run=args.dry_run)
    except TaskLifecycleError as error:
        result = {"status": "error", "reason": error.reason, "message": str(error)}
    if getattr(args, "json", False):
        print(json.dumps(result, ensure_ascii=False, sort_keys=True))
    elif result["status"] == "error":
        print(f"恢复失败：{result['message']}", file=sys.stderr)
    elif result["status"] == "dry-run":
        print(f"候选恢复：{result['source']} -> {result['destination']}")
    else:
        print(f"✓ 已恢复物理位置：{result['source']} -> {result['destination']}")
    return 1 if result["status"] == "error" else 0


def _build_parser() -> argparse.ArgumentParser:
    """构造独立 maintenance 入口 parser。"""
    parser = argparse.ArgumentParser(description="Trellis task lifecycle maintenance")
    subparsers = parser.add_subparsers(dest="command", required=True)
    maintenance = subparsers.add_parser("session-start")
    maintenance.add_argument("--before", default=DEFAULT_GC_AGE)
    maintenance.add_argument("--json", action="store_true")
    return parser


def main() -> int:
    """运行独立 SessionStart maintenance 入口。"""
    args = _build_parser().parse_args()
    repo_root = get_repo_root()
    try:
        result = run_session_maintenance(repo_root, args.before)
    except (OSError, ValueError, TaskLifecycleError) as error:
        reason = error.reason if isinstance(error, TaskLifecycleError) else "maintenance-failed"
        result = {"status": "error", "reason": reason, "message": str(error)}
    if args.json:
        print(json.dumps(result, ensure_ascii=False, sort_keys=True))
    return 1 if result.get("status") == "error" else 0


if __name__ == "__main__":
    sys.exit(main())
