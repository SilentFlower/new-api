#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""读取和写入 Trellis 任务的 last_push_snapshot。"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any


DIR_WORKFLOW = ".trellis"
DIR_TASKS = "tasks"
FILE_TASK_JSON = "task.json"
REQUIRED_FIELDS = {
    "snapshot_at",
    "branch",
    "pushed_commits",
    "completed_steps",
    "next_step",
}
OPTIONAL_STRING_FIELDS = {"partial_step", "notes"}


def _find_repo_root(start: Path) -> Path | None:
    """从当前位置向上查找 Trellis 项目根目录。"""
    current = start.resolve()
    if current.is_file():
        current = current.parent
    while True:
        if (current / DIR_WORKFLOW).is_dir():
            return current
        if current == current.parent:
            return None
        current = current.parent


def _scripts_dir(repo_root: Path) -> Path:
    """返回当前项目的 Trellis scripts 目录。"""
    return repo_root / DIR_WORKFLOW / "scripts"


def _load_common_modules(repo_root: Path) -> None:
    """把 `.trellis/scripts` 加入 import path 以复用 Trellis 公共模块。"""
    scripts_dir = _scripts_dir(repo_root)
    if str(scripts_dir) not in sys.path:
        sys.path.insert(0, str(scripts_dir))


def _read_json(path: Path) -> dict[str, Any] | None:
    """读取 JSON 对象，失败或非对象时返回 None。"""
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except (FileNotFoundError, json.JSONDecodeError, OSError):
        return None
    return data if isinstance(data, dict) else None


def _write_json(path: Path, data: dict[str, Any]) -> None:
    """用 Trellis 任务文件常见格式写回 JSON。"""
    path.write_text(
        json.dumps(data, indent=2, ensure_ascii=False) + "\n",
        encoding="utf-8",
    )


def _rel_path(repo_root: Path, path: Path) -> str:
    """尽量输出 repo-root 相对路径。"""
    try:
        return path.relative_to(repo_root).as_posix()
    except ValueError:
        return str(path)


def _resolve_task_dir(repo_root: Path, task_ref: str) -> Path:
    """解析任务目录引用。"""
    _load_common_modules(repo_root)
    from common.task_utils import resolve_task_dir  # type: ignore[import-not-found]

    return resolve_task_dir(task_ref, repo_root)


def _current_task_dir(repo_root: Path) -> Path | None:
    """读取当前 session 的 active task 目录。"""
    _load_common_modules(repo_root)
    from common.active_task import resolve_active_task  # type: ignore[import-not-found]

    active = resolve_active_task(repo_root)
    if not active.task_path:
        return None
    task_dir = Path(active.task_path)
    if task_dir.is_absolute():
        return task_dir
    return repo_root / task_dir


def _task_json_path(task_dir: Path) -> Path:
    """返回任务 task.json 路径。"""
    return task_dir / FILE_TASK_JSON


def _snapshot_summary(snapshot: dict[str, Any]) -> dict[str, Any]:
    """提取恢复提示需要的精简 snapshot 字段。"""
    return {
        "completed_steps": snapshot.get("completed_steps", []),
        "partial_step": snapshot.get("partial_step"),
        "next_step": snapshot.get("next_step"),
    }


def _load_task_snapshot(repo_root: Path, task_dir: Path) -> dict[str, Any]:
    """读取单个任务的 snapshot 状态。"""
    task_json = _task_json_path(task_dir)
    data = _read_json(task_json)
    task_rel = _rel_path(repo_root, task_dir)
    if data is None:
        return {
            "status": "error",
            "reason": "invalid-task-json",
            "task": task_rel,
            "path": _rel_path(repo_root, task_json),
        }
    snapshot = data.get("last_push_snapshot")
    if not isinstance(snapshot, dict):
        return {"status": "no-snapshot", "task": task_rel}
    return {
        "status": "ok",
        "task": task_rel,
        "snapshot": snapshot,
        "summary": _snapshot_summary(snapshot),
    }


def _iter_active_task_dirs(repo_root: Path) -> list[Path]:
    """列出 active task tree 下的一层任务目录。"""
    tasks_dir = repo_root / DIR_WORKFLOW / DIR_TASKS
    try:
        return [
            path for path in sorted(tasks_dir.iterdir())
            if path.is_dir() and path.name != "archive"
        ]
    except OSError:
        return []


def _snapshot_candidates(repo_root: Path) -> list[dict[str, Any]]:
    """扫描 in_progress 且带 last_push_snapshot 的任务候选。"""
    candidates: list[dict[str, Any]] = []
    for task_dir in _iter_active_task_dirs(repo_root):
        task_json = _task_json_path(task_dir)
        data = _read_json(task_json)
        if data is None:
            continue
        if data.get("status") != "in_progress":
            continue
        snapshot = data.get("last_push_snapshot")
        if not isinstance(snapshot, dict):
            continue
        candidates.append({
            "task": _rel_path(repo_root, task_dir),
            **_snapshot_summary(snapshot),
        })
    return candidates


def _validate_string(value: Any, field: str, errors: list[str]) -> None:
    """校验字符串字段。"""
    if not isinstance(value, str) or not value.strip():
        errors.append(f"{field} 必须是非空字符串")


def _validate_string_or_object(value: Any, field: str, errors: list[str]) -> None:
    """校验字符串或对象字段。"""
    if isinstance(value, str) and value.strip():
        return
    if isinstance(value, dict):
        return
    errors.append(f"{field} 必须是非空字符串或对象")


def _validate_snapshot(snapshot: Any) -> tuple[dict[str, Any] | None, list[str]]:
    """校验 last_push_snapshot schema。"""
    if not isinstance(snapshot, dict):
        return None, ["snapshot 必须是 JSON 对象"]

    errors: list[str] = []
    for field in sorted(REQUIRED_FIELDS):
        if field not in snapshot:
            errors.append(f"缺少必填字段 {field}")

    if "snapshot_at" in snapshot:
        _validate_string(snapshot.get("snapshot_at"), "snapshot_at", errors)
    if "branch" in snapshot:
        _validate_string_or_object(snapshot.get("branch"), "branch", errors)
    if "pushed_commits" in snapshot:
        _validate_string_or_object(
            snapshot.get("pushed_commits"),
            "pushed_commits",
            errors,
        )
    completed_steps = snapshot.get("completed_steps")
    if "completed_steps" in snapshot and (
        not isinstance(completed_steps, list)
        or any(not isinstance(item, str) for item in completed_steps)
    ):
        errors.append("completed_steps 必须是字符串数组")
    if "next_step" in snapshot:
        _validate_string(snapshot.get("next_step"), "next_step", errors)

    for field in sorted(OPTIONAL_STRING_FIELDS):
        if field in snapshot and snapshot.get(field) is not None and not isinstance(snapshot.get(field), str):
            errors.append(f"{field} 存在时必须是字符串")

    if errors:
        return None, errors
    return snapshot, []


def _print_json(data: dict[str, Any], exit_code: int = 0) -> int:
    """输出紧凑 JSON。"""
    print(json.dumps(data, ensure_ascii=False, sort_keys=True))
    return exit_code


def _print_status_text(data: dict[str, Any]) -> int:
    """输出给人和 AI 快速阅读的状态文本。"""
    status = data.get("status")
    if status == "ok":
        summary = data.get("summary") if isinstance(data.get("summary"), dict) else {}
        print(f"任务：{data.get('task')}")
        print("snapshot：存在")
        print(f"completed_steps: {summary.get('completed_steps', [])}")
        print(f"partial_step: {summary.get('partial_step') or '(none)'}")
        print(f"next_step: {summary.get('next_step') or '(none)'}")
        return 0
    if status == "candidates":
        candidates = data.get("candidates")
        print("无活动任务。snapshot 候选：")
        if isinstance(candidates, list) and candidates:
            for item in candidates:
                print(f"- {item.get('task')}: next_step={item.get('next_step') or '(none)'}")
        else:
            print("(none)")
        return 0
    if status == "no-snapshot":
        print(f"任务没有 last_push_snapshot：{data.get('task')}")
        return 0
    if status == "no-current-task":
        print("没有活动任务，也没有可用的 snapshot 候选。")
        return 0
    print(f"错误：{data.get('reason') or status}", file=sys.stderr)
    return 1


def cmd_status(args: argparse.Namespace, repo_root: Path) -> int:
    """执行 status 子命令。"""
    if args.task:
        task_dir = _resolve_task_dir(repo_root, args.task)
        result = _load_task_snapshot(repo_root, task_dir)
    else:
        task_dir = _current_task_dir(repo_root)
        if task_dir is None:
            candidates = _snapshot_candidates(repo_root)
            result = {
                "status": "candidates" if candidates else "no-current-task",
                "candidates": candidates,
            }
        else:
            result = _load_task_snapshot(repo_root, task_dir)

    exit_code = 1 if result.get("status") == "error" else 0
    if args.json:
        return _print_json(result, exit_code)
    return _print_status_text(result)


def cmd_write(args: argparse.Namespace, repo_root: Path) -> int:
    """执行 write 子命令。"""
    task_dir = _resolve_task_dir(repo_root, args.task)
    task_json = _task_json_path(task_dir)
    data = _read_json(task_json)
    if data is None:
        result = {
            "status": "error",
            "reason": "invalid-task-json",
            "task": _rel_path(repo_root, task_dir),
        }
        if args.json:
            return _print_json(result, 1)
        print(f"错误：{result['reason']}", file=sys.stderr)
        return 1

    try:
        raw_snapshot = json.loads(args.snapshot_json)
    except json.JSONDecodeError as exc:
        result = {
            "status": "error",
            "reason": "invalid-snapshot-json",
            "message": str(exc),
        }
        if args.json:
            return _print_json(result, 1)
        print(f"错误：{result['reason']}：{result['message']}", file=sys.stderr)
        return 1

    snapshot, errors = _validate_snapshot(raw_snapshot)
    if snapshot is None:
        result = {
            "status": "error",
            "reason": "invalid-snapshot-schema",
            "errors": errors,
        }
        if args.json:
            return _print_json(result, 1)
        print("错误：invalid-snapshot-schema", file=sys.stderr)
        for error in errors:
            print(f"- {error}", file=sys.stderr)
        return 1

    data["last_push_snapshot"] = snapshot
    try:
        _write_json(task_json, data)
    except OSError as exc:
        result = {
            "status": "error",
            "reason": "write-failed",
            "message": str(exc),
            "task": _rel_path(repo_root, task_dir),
        }
        if args.json:
            return _print_json(result, 1)
        print(f"错误：write-failed：{exc}", file=sys.stderr)
        return 1

    result = {
        "status": "written",
        "task": _rel_path(repo_root, task_dir),
        "path": _rel_path(repo_root, task_json),
        "summary": _snapshot_summary(snapshot),
    }
    if args.json:
        return _print_json(result)
    print(f"✓ 已更新 last_push_snapshot：{result['path']}")
    print(f"next_step: {result['summary'].get('next_step') or '(none)'}")
    return 0


def build_parser() -> argparse.ArgumentParser:
    """创建命令行解析器。"""
    parser = argparse.ArgumentParser(
        description="读取或写入 Trellis 任务的 last_push_snapshot。",
    )
    subparsers = parser.add_subparsers(dest="command", required=True)

    status = subparsers.add_parser("status", help="读取 last_push_snapshot")
    status.add_argument("--task", help="任务目录、路径或任务名")
    status.add_argument("--json", action="store_true", help="输出 JSON")

    write = subparsers.add_parser("write", help="写入 last_push_snapshot")
    write.add_argument("--task", required=True, help="任务目录、路径或任务名")
    write.add_argument("--snapshot-json", required=True, help="snapshot JSON 对象")
    write.add_argument("--json", action="store_true", help="输出 JSON")

    return parser


def main() -> int:
    """命令入口。"""
    repo_root = _find_repo_root(Path.cwd())
    if repo_root is None:
        print("错误：不是 Trellis 项目（缺少 .trellis/）", file=sys.stderr)
        return 1

    parser = build_parser()
    args = parser.parse_args()
    if args.command == "status":
        return cmd_status(args, repo_root)
    if args.command == "write":
        return cmd_write(args, repo_root)
    parser.print_help()
    return 1


if __name__ == "__main__":
    sys.exit(main())
