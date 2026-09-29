#!/usr/bin/env python3
"""
Task utility functions.

Provides:
    is_safe_task_path   - Validate task path is safe to operate on
    find_task_by_name   - Find task directory by name
    resolve_task_dir    - Resolve task directory from name, relative, or absolute path
    run_task_hooks      - Run lifecycle hooks for task events
"""

from __future__ import annotations

import sys
from pathlib import Path

from .paths import get_repo_root, get_tasks_dir


# =============================================================================
# Path Safety
# =============================================================================

def is_safe_task_path(task_path: str, repo_root: Path | None = None) -> bool:
    """Check if a relative task path is safe to operate on.

    Args:
        task_path: Task path (relative to repo_root).
        repo_root: Repository root path. Defaults to auto-detected.

    Returns:
        True if safe, False if dangerous.
    """
    if repo_root is None:
        repo_root = get_repo_root()

    normalized = task_path.replace("\\", "/")

    # Check empty or null
    if not normalized or normalized == "null":
        print("Error: empty or null task path", file=sys.stderr)
        return False

    # Reject absolute paths
    if Path(task_path).is_absolute():
        print(f"Error: absolute path not allowed: {task_path}", file=sys.stderr)
        return False

    # Reject ".", "..", paths starting with "./" or "../", or containing ".."
    if normalized in (".", "..") or normalized.startswith("./") or normalized.startswith("../") or ".." in normalized:
        print(f"Error: path traversal not allowed: {task_path}", file=sys.stderr)
        return False

    # Final check: ensure resolved path is not the repo root
    abs_path = repo_root / Path(normalized)
    if abs_path.exists():
        try:
            resolved = abs_path.resolve()
            root_resolved = repo_root.resolve()
            if resolved == root_resolved:
                print(f"Error: path resolves to repo root: {task_path}", file=sys.stderr)
                return False
        except (OSError, IOError):
            pass

    return True


def find_task_by_name(task_name: str, tasks_dir: Path) -> Path | None:
    """Find task directory by name (exact or suffix match).

    Args:
        task_name: Task name to find.
        tasks_dir: Tasks directory path.

    Returns:
        Absolute path to task directory, or None if not found.
    """
    if not task_name or not tasks_dir or not tasks_dir.is_dir():
        return None

    # Try exact match first
    exact_match = tasks_dir / task_name
    if exact_match.is_dir():
        return exact_match

    # Try suffix match (e.g., "my-task" matches "01-21-my-task")
    for d in tasks_dir.iterdir():
        if d.is_dir() and d.name.endswith(f"-{task_name}"):
            return d

    return None


# =============================================================================
# Task Directory Resolution
# =============================================================================

def resolve_task_dir(target_dir: str, repo_root: Path) -> Path:
    """Resolve task directory to absolute path.

    Supports:
    - Absolute path: /path/to/task
    - Relative path: .trellis/tasks/01-31-my-task
    - Task name: my-task (uses find_task_by_name for lookup)

    Args:
        target_dir: Task directory specification.
        repo_root: Repository root path.

    Returns:
        Resolved absolute path.
    """
    if not target_dir:
        return Path()

    normalized = target_dir.replace("\\", "/")
    while normalized.startswith("./"):
        normalized = normalized[2:]

    # Absolute path
    if Path(target_dir).is_absolute():
        return Path(target_dir)

    # Relative path (contains path separator or starts with .trellis)
    if "/" in normalized or normalized.startswith(".trellis"):
        return repo_root / Path(normalized)

    # Task name - try to find in tasks directory
    tasks_dir = get_tasks_dir(repo_root)
    found = find_task_by_name(target_dir, tasks_dir)
    if found:
        return found

    # Fallback to treating as relative path
    return repo_root / Path(normalized)
# BEGIN skill-garden patch task-reference-resolution v0.6


def _resolve_task_reference_from_records(
    task_ref: str,
    repo_root: Path,
    records,
    scope_name: str,
) -> Path:
    """Resolve one task reference from an explicit lifecycle record scope."""
    raw = task_ref.strip() if isinstance(task_ref, str) else ""
    if not raw:
        raise ValueError("任务引用不能为空")

    tasks_dir = get_tasks_dir(repo_root).resolve()
    normalized = raw.replace("\\", "/")
    while normalized.startswith("./"):
        normalized = normalized[2:]

    candidate = Path(raw)
    path_reference = candidate.is_absolute() or "/" in normalized or normalized.startswith(".trellis")
    if path_reference:
        candidate = candidate if candidate.is_absolute() else repo_root / Path(normalized)
        try:
            resolved = candidate.resolve()
        except (OSError, RuntimeError) as error:
            raise ValueError(f"无法解析任务引用：{task_ref}") from error
        if candidate.is_symlink() or resolved.parent != tasks_dir or resolved.name == "archive":
            raise ValueError(f"任务引用必须指向{scope_name}：{task_ref}")
        if not resolved.is_dir():
            raise ValueError(f"任务不存在：{task_ref}")
        matches = [task.directory.resolve() for task in records if task.directory.resolve() == resolved]
    else:
        matches = [
            task.directory.resolve()
            for task in records
            if task.dir_name == raw or task.dir_name.endswith(f"-{raw}")
        ]

    matches = sorted(set(matches))
    if len(matches) == 1:
        return matches[0]
    if len(matches) > 1:
        names = ", ".join(path.name for path in matches)
        raise ValueError(f"任务引用存在歧义：{task_ref}；候选：{names}；请使用完整目录名")
    if path_reference or (tasks_dir / raw).is_dir():
        raise ValueError(f"任务引用必须指向{scope_name}：{task_ref}")
    raise ValueError(f"任务不存在：{task_ref}")


def resolve_active_task_reference(task_ref: str, repo_root: Path) -> Path:
    """Resolve an existing active task reference deterministically.

    Args:
        task_ref: Exact task name, unique suffix, relative path, or absolute path.
        repo_root: Repository root path.

    Returns:
        Resolved active task directory.

    Raises:
        ValueError: The reference is empty, ambiguous, missing, or outside active tasks.
    """
    from .tasks import iter_active_tasks

    tasks_dir = get_tasks_dir(repo_root).resolve()
    try:
        records = list(iter_active_tasks(tasks_dir))
    except OSError as error:
        raise ValueError(f"无法读取活动任务目录：{tasks_dir}") from error
    return _resolve_task_reference_from_records(task_ref, repo_root, records, "活动任务目录")


def resolve_top_level_task_reference(task_ref: str, repo_root: Path) -> Path:
    """Resolve an existing top-level task, including a logical closed task.

    Args:
        task_ref: Exact task name, unique suffix, relative path, or absolute path.
        repo_root: Repository root path.

    Returns:
        Resolved top-level task directory.

    Raises:
        ValueError: The reference is empty, ambiguous, missing, or physically archived.
    """
    from .tasks import iter_top_level_tasks

    tasks_dir = get_tasks_dir(repo_root).resolve()
    try:
        records = list(iter_top_level_tasks(tasks_dir))
    except OSError as error:
        raise ValueError(f"无法读取顶层任务目录：{tasks_dir}") from error
    return _resolve_task_reference_from_records(task_ref, repo_root, records, "顶层任务目录")


def resolve_task_reference(task_ref: str, repo_root: Path) -> Path:
    """Resolve an active task reference for backward-compatible callers.

    Args:
        task_ref: Exact task name, unique suffix, relative path, or absolute path.
        repo_root: Repository root path.

    Returns:
        Resolved active task directory.

    Raises:
        ValueError: The reference is not an unambiguous active task.
    """
    return resolve_active_task_reference(task_ref, repo_root)
# END skill-garden patch task-reference-resolution v0.6


# =============================================================================
# Lifecycle Hooks
# =============================================================================

def run_task_hooks(event: str, task_json_path: Path, repo_root: Path) -> None:
    """Run lifecycle hooks for a task event.

    Args:
        event: Event name (e.g. "after_create").
        task_json_path: Absolute path to the task's task.json.
        repo_root: Repository root for cwd and config lookup.
    """
    import os
    import subprocess

    from .config import get_hooks
    from .log import Colors, colored

    commands = get_hooks(event, repo_root)
    if not commands:
        return

    env = {**os.environ, "TASK_JSON_PATH": str(task_json_path)}

    for cmd in commands:
        try:
            result = subprocess.run(
                cmd,
                shell=True,
                cwd=repo_root,
                env=env,
                capture_output=True,
                text=True,
                encoding="utf-8",
                errors="replace",
            )
            if result.returncode != 0:
                print(
                    colored(f"[WARN] Hook failed ({event}): {cmd}", Colors.YELLOW),
                    file=sys.stderr,
                )
                if result.stderr.strip():
                    print(f"  {result.stderr.strip()}", file=sys.stderr)
        except Exception as e:
            print(
                colored(f"[WARN] Hook error ({event}): {cmd} — {e}", Colors.YELLOW),
                file=sys.stderr,
            )


# =============================================================================
# Main Entry (for testing)
# =============================================================================

if __name__ == "__main__":
    repo = get_repo_root()
    tasks = get_tasks_dir(repo)

    print(f"Tasks dir: {tasks}")
    print(f"is_safe_task_path('.trellis/tasks/test'): {is_safe_task_path('.trellis/tasks/test', repo)}")
    print(f"is_safe_task_path('../test'): {is_safe_task_path('../test', repo)}")
