"""Shared task loading and lifecycle-aware views."""

from __future__ import annotations

from collections.abc import Iterator
from pathlib import Path
from typing import Any

from .io import read_json
from .paths import FILE_TASK_JSON
from .types import TaskInfo


CLOSEOUT_STATUSES = {"pending", "blocked", "closed"}


def pending_closeout() -> dict[str, Any]:
    """Return the canonical pending closeout value.

    Returns:
        A closeout object suitable for a new or legacy top-level task.
    """
    return {"status": "pending", "closedAt": None, "blockers": []}


def normalize_closeout(
    task_data: dict[str, Any],
    physical_archive: bool = False,
) -> dict[str, Any]:
    """Normalize and validate one task closeout without mutating its data.

    Args:
        task_data: Parsed task.json object.
        physical_archive: Whether the task lives below the physical archive root.

    Returns:
        Normalized status, timestamp, blockers, compatibility flags, and validity.
    """
    raw = task_data.get("closeout")
    if raw is None:
        if physical_archive:
            return {
                "status": "closed",
                "closedAt": None,
                "blockers": [],
                "legacy": True,
                "valid": True,
                "historical": True,
            }
        return {**pending_closeout(), "legacy": True, "valid": True, "historical": False}
    if not isinstance(raw, dict):
        return {
            "status": "closed" if physical_archive else "pending",
            "closedAt": None,
            "blockers": [],
            "legacy": False,
            "valid": False,
            "historical": physical_archive,
        }

    status = raw.get("status")
    closed_at = raw.get("closedAt")
    blockers = raw.get("blockers")
    blockers_valid = isinstance(blockers, list) and all(
        isinstance(blocker, dict)
        and all(
            isinstance(blocker.get(field), str) and bool(blocker[field].strip())
            for field in ("code", "owner", "message")
        )
        for blocker in blockers
    )
    # 损坏的 JSON 可能把 status 写成 list/dict；先做类型收窄，避免成员判断抛出 TypeError
    # 并拖垮所有共享任务视图。
    valid = isinstance(status, str) and status in CLOSEOUT_STATUSES and blockers_valid
    if status == "closed":
        valid = valid and isinstance(closed_at, str) and bool(closed_at.strip()) and not blockers
    elif status == "blocked":
        valid = valid and closed_at is None and bool(blockers)
    elif status == "pending":
        valid = valid and closed_at is None and not blockers
    if not valid or (physical_archive and status != "closed"):
        return {
            "status": "closed" if physical_archive else "pending",
            "closedAt": None,
            "blockers": [],
            "legacy": False,
            "valid": False,
            "historical": physical_archive,
        }
    return {
        "status": status,
        "closedAt": closed_at,
        "blockers": blockers,
        "legacy": False,
        "valid": True,
        "historical": physical_archive,
    }


def load_task(task_dir: Path) -> TaskInfo | None:
    """Load one task directory without applying a view filter.

    Args:
        task_dir: Absolute task directory.

    Returns:
        Parsed TaskInfo, or None when task.json is missing or invalid.
    """
    task_json = task_dir / FILE_TASK_JSON
    if not task_json.is_file():
        return None
    data = read_json(task_json)
    if not isinstance(data, dict):
        return None
    return TaskInfo(
        dir_name=task_dir.name,
        directory=task_dir,
        title=data.get("title") or data.get("name") or "unknown",
        status=data.get("status", "unknown"),
        assignee=data.get("assignee", ""),
        priority=data.get("priority", "P2"),
        children=tuple(data.get("children", [])),
        parent=data.get("parent"),
        package=data.get("package"),
        raw=data,
    )


def _is_physical_archive(task: TaskInfo, tasks_dir: Path) -> bool:
    """Return whether a task lives below the physical archive root."""
    try:
        relative = task.directory.relative_to(tasks_dir)
    except ValueError:
        return False
    return len(relative.parts) >= 3 and relative.parts[0] == "archive"


def is_closed_task(task: TaskInfo, tasks_dir: Path) -> bool:
    """Return whether a task belongs to the closed view.

    Args:
        task: Loaded task record.
        tasks_dir: Root tasks directory.

    Returns:
        True for explicit logical closeout or historical physical archive.
    """
    if _is_physical_archive(task, tasks_dir):
        return True
    closeout = normalize_closeout(task.raw)
    return bool(closeout["valid"] and closeout["status"] == "closed")


def task_closeout_view(task: TaskInfo, tasks_dir: Path) -> dict:
    """Return the normalized closeout shown by lifecycle views.

    Args:
        task: Loaded task record.
        tasks_dir: Root tasks directory.

    Returns:
        A normalized closeout object without mutating historical task data.
    """
    return normalize_closeout(task.raw, _is_physical_archive(task, tasks_dir))


def iter_top_level_tasks(tasks_dir: Path) -> Iterator[TaskInfo]:
    """Iterate valid top-level task records without applying lifecycle filters.

    Args:
        tasks_dir: Root tasks directory.

    Yields:
        Valid top-level task records in stable directory order.
    """
    if not tasks_dir.is_dir():
        return
    for task_dir in sorted(tasks_dir.iterdir()):
        if not task_dir.is_dir() or task_dir.is_symlink() or task_dir.name == "archive":
            continue
        task = load_task(task_dir)
        if task is not None:
            yield task


def iter_task_records(tasks_dir: Path) -> Iterator[TaskInfo]:
    """Iterate top-level and historical task records once per directory identity.

    Args:
        tasks_dir: Root tasks directory.

    Yields:
        Top-level records first, followed by non-conflicting archive records.
    """
    seen: set[str] = set()
    for task in iter_top_level_tasks(tasks_dir):
        seen.add(task.dir_name)
        yield task
    archive_dir = tasks_dir / "archive"
    if not archive_dir.is_dir() or archive_dir.is_symlink():
        return
    for bucket in sorted(archive_dir.iterdir()):
        if not bucket.is_dir() or bucket.is_symlink():
            continue
        for task_dir in sorted(bucket.iterdir()):
            if not task_dir.is_dir() or task_dir.is_symlink():
                continue
            if task_dir.name in seen:
                raise ValueError(f"Duplicate task identity across active/archive views: {task_dir.name}")
            task = load_task(task_dir)
            if task is not None:
                seen.add(task.dir_name)
                yield task


def iter_active_tasks(tasks_dir: Path) -> Iterator[TaskInfo]:
    """Iterate the canonical active view.

    Args:
        tasks_dir: Root tasks directory.

    Yields:
        Valid top-level tasks whose closeout is not explicitly closed.
    """
    for task in iter_top_level_tasks(tasks_dir):
        if not is_closed_task(task, tasks_dir):
            yield task


def iter_invalid_top_level_tasks(tasks_dir: Path) -> Iterator[Path]:
    """Iterate top-level task directories whose task.json cannot be loaded.

    Args:
        tasks_dir: Root tasks directory.

    Yields:
        Invalid top-level task directories for diagnostic-only consumers.
    """
    if not tasks_dir.is_dir():
        return
    for task_dir in sorted(tasks_dir.iterdir()):
        if not task_dir.is_dir() or task_dir.is_symlink() or task_dir.name == "archive":
            continue
        if load_task(task_dir) is None:
            yield task_dir


def iter_closed_tasks(tasks_dir: Path) -> Iterator[TaskInfo]:
    """Iterate the unified logical and historical closed view.

    Args:
        tasks_dir: Root tasks directory.

    Yields:
        Closed TaskInfo records, deduplicated by directory identity.
    """
    for task in iter_task_records(tasks_dir):
        if is_closed_task(task, tasks_dir):
            yield task


def get_all_statuses(tasks_dir: Path) -> dict[str, str]:
    """Return work statuses across active, closed, and physical task locations.

    Args:
        tasks_dir: Root tasks directory.

    Returns:
        Directory-name to work-status mapping.
    """
    return {task.dir_name: task.status for task in iter_task_records(tasks_dir)}


def children_progress(
    children: tuple[str, ...] | list[str],
    all_statuses: dict[str, str],
) -> str:
    """Format completed child progress without guessing missing records.

    Args:
        children: Declared child directory names.
        all_statuses: Full task status index from get_all_statuses().

    Returns:
        Formatted progress suffix, or an empty string without children.
    """
    if not children:
        return ""
    done = sum(1 for child in children if all_statuses.get(child) in ("completed", "done"))
    return f" [{done}/{len(children)} done]"
