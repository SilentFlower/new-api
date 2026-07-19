#!/usr/bin/env python3
"""Trellis auto loop 的可恢复流程控制器。"""

from __future__ import annotations

import argparse
import json
import os
import socket
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

SCHEMA_VERSION = 1
DEFAULT_PROFILE = "commit-only"
MAX_FIX_RECHECK = 3
DECISION_LOG_LIMIT = 20
VALID_IMPLEMENT_ROUTES = {"inline", "subagent"}
VALID_CHECK_ROUTES = {"check-all-inline", "check-all-subagent"}
VALID_CHECK_DEPTHS = {"auto", "light", "full"}
RECOVERABLE_BLOCK_REASONS = {
    "missing-prd",
    "open-questions",
    "incomplete-complex-artifacts",
    "missing-implement-context",
    "missing-check-context",
    "unknown-step",
}
STEP_ACTIONS = {
    "refresh_brief": "refresh_brief",
    "start_task": "start_task",
    "implement": "run_implement",
    "check": "run_check_all",
    "fix": "run_fix",
    "recheck": "run_recheck",
    "spec_update": "run_spec_update",
    "commit_only": "commit_only",
}
ROUTE_ACTION_TARGETS = {
    "run_implement": "implement",
    "run_fix": "implement",
    "run_check_all": "check",
    "run_recheck": "check",
}


def _utc_now() -> str:
    """返回秒级 UTC 时间字符串。"""
    return datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def _repo_root() -> Path | None:
    """从当前目录向上寻找 Trellis 项目根。"""
    current = Path.cwd().resolve()
    while True:
        if (current / ".trellis").is_dir():
            return current
        if current == current.parent:
            return None
        current = current.parent


def _print(data: dict[str, Any]) -> int:
    """输出给 agent 消费的紧凑 JSON。"""
    print(json.dumps(data, ensure_ascii=False, sort_keys=True))
    return 0


def _read_json(path: Path) -> dict[str, Any]:
    """读取 JSON 对象，失败返回空对象。"""
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except (FileNotFoundError, json.JSONDecodeError, OSError):
        return {}
    return data if isinstance(data, dict) else {}


def _write_json(path: Path, data: dict[str, Any]) -> None:
    """写入格式化 JSON。"""
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(data, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")


def _rel_path(repo_root: Path, path: Path) -> str:
    """尽量返回相对项目根的 POSIX 路径。"""
    try:
        return path.relative_to(repo_root).as_posix()
    except ValueError:
        return str(path)


def _auto_dir(repo_root: Path) -> Path:
    """返回 auto-loop runtime 目录。"""
    return repo_root / ".trellis/.runtime/auto-loop"


def _current_pointer(repo_root: Path) -> Path:
    """返回当前 auto run 指针文件。"""
    return _auto_dir(repo_root) / "current.json"


def _read_route_prefs(repo_root: Path) -> dict[str, str]:
    """读取个人 route 默认配置，用于 start gate 判断是否需要 JSONL context。"""
    path = repo_root / ".trellis/.route-prefs.tmp"
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except OSError:
        return {}

    prefs: dict[str, str] = {}
    for raw in lines:
        if "=" not in raw:
            continue
        key, value = raw.split("=", 1)
        key = key.strip()
        value = value.strip()
        if key == "implement" and value in VALID_IMPLEMENT_ROUTES:
            prefs[key] = value
        elif key == "check" and value in VALID_CHECK_ROUTES:
            prefs[key] = value
    return prefs


def _route_state_helper(repo_root: Path) -> Path | None:
    """返回本地 trellis-route helper 路径，缺失时返回 None。"""
    candidates = [
        repo_root / ".agents/skills/trellis-route/scripts/route_state.py",
        repo_root / ".claude/skills/trellis-route/scripts/route_state.py",
    ]
    for path in candidates:
        if path.is_file():
            return path
    return None


def _current_task_ref(repo_root: Path) -> str | None:
    """读取当前活动任务路径，失败时返回 None。"""
    result = subprocess.run(
        ["python3", str(repo_root / ".trellis/scripts/task.py"), "current"],
        cwd=repo_root,
        check=False,
        text=True,
        capture_output=True,
    )
    for raw in result.stdout.splitlines():
        line = raw.strip()
        if line.startswith(".trellis/tasks/"):
            return line
        if "Current task:" in line:
            value = line.split("Current task:", 1)[1].strip()
            if value.startswith(".trellis/tasks/"):
                return value
    return None


def _resolve_existing_route(repo_root: Path, task_ref: str, target: str) -> str | None:
    """通过 trellis-route helper 解析已有 runtime/prefs route，失败时不阻断。"""
    helper = _route_state_helper(repo_root)
    if helper is None:
        return None
    if _current_task_ref(repo_root) != task_ref:
        return None
    result = subprocess.run(
        ["python3", str(helper), "resolve", "--target", target],
        cwd=repo_root,
        check=False,
        text=True,
        capture_output=True,
    )
    try:
        data = json.loads(result.stdout)
    except json.JSONDecodeError:
        return None
    if not isinstance(data, dict) or data.get("status") != "hit":
        return None
    if data.get("task") != task_ref:
        return None
    mode = data.get("mode")
    if target == "implement" and mode in VALID_IMPLEMENT_ROUTES:
        return str(mode)
    if target == "check" and mode in VALID_CHECK_ROUTES:
        return str(mode)
    return None


def _effective_route_authorization(repo_root: Path, task_ref: str, route_authorization: Any) -> dict[str, str]:
    """按 route 优先级估算 start gate 需要的 context 类型。"""
    effective: dict[str, str] = {}
    if isinstance(route_authorization, dict):
        implement = route_authorization.get("implement")
        check = route_authorization.get("check")
        if implement in VALID_IMPLEMENT_ROUTES:
            effective["implement"] = str(implement)
        if check in VALID_CHECK_ROUTES:
            effective["check"] = str(check)

    # 个人默认优先于 auto 临时授权；start gate 的 JSONL 判断也要遵守同一优先级，
    # 否则可能在个人 subagent 默认下误放行，或在个人 inline 默认下误阻塞。
    effective.update(_read_route_prefs(repo_root))
    for target in ("implement", "check"):
        if target not in effective:
            mode = _resolve_existing_route(repo_root, task_ref, target)
            if mode:
                effective[target] = mode
    return effective


def _requested_check_depth(state: dict[str, Any]) -> str:
    """读取 run 请求的检查深度，旧状态或非法值按 full 兼容。"""
    value = state.get("check_depth")
    return str(value) if value in VALID_CHECK_DEPTHS else "full"


def _append_item_decision(
    item: dict[str, Any],
    event_type: str,
    summary: str,
    data: dict[str, Any] | None = None,
) -> None:
    """向当前队列项追加精简决策日志。"""
    log = item.get("decision_log")
    if not isinstance(log, list):
        log = []
    log.append({
        "at": _utc_now(),
        "type": event_type,
        "task": item.get("task"),
        "summary": summary,
        "data": data or {},
    })
    item["decision_log"] = log[-DECISION_LOG_LIMIT:]


def _decision_tail(state: dict[str, Any], limit: int = 8, include_data: bool = True) -> list[dict[str, Any]]:
    """返回 run 内最近的关键决策摘要。"""
    queue = state.get("queue") if isinstance(state.get("queue"), list) else []
    events: list[dict[str, Any]] = []
    for item in queue:
        if not isinstance(item, dict):
            continue
        log = item.get("decision_log")
        if isinstance(log, list):
            events.extend(event for event in log if isinstance(event, dict))
    tail = events[-limit:]
    if include_data:
        return tail
    compact: list[dict[str, Any]] = []
    for event in tail:
        compact.append({
            key: event.get(key)
            for key in ("at", "type", "task", "summary")
            if event.get(key) is not None
        })
    return compact


def _run_path(repo_root: Path, run_id: str) -> Path:
    """返回指定 auto run 状态文件。"""
    return _auto_dir(repo_root) / f"{run_id}.json"


def _run_paths(repo_root: Path) -> list[Path]:
    """返回按 run id 从新到旧排序的 auto run 文件。"""
    return sorted(_auto_dir(repo_root).glob("auto-*.json"), reverse=True)


def _new_run_id() -> str:
    """生成短小稳定的 run id。"""
    return "auto-" + datetime.now(timezone.utc).strftime("%Y%m%d%H%M%S")


def _normalize_task_ref(repo_root: Path, task_ref: str) -> str:
    """把任务引用规范化为 `.trellis/tasks/<dir>`。"""
    raw = task_ref.strip()
    path_obj = Path(raw)
    candidates: list[Path]
    if path_obj.is_absolute():
        candidates = [path_obj]
    elif raw.startswith(".trellis/"):
        candidates = [repo_root / raw]
    elif raw.startswith("tasks/"):
        candidates = [repo_root / ".trellis" / raw]
    else:
        candidates = [repo_root / ".trellis/tasks" / raw, repo_root / raw]

    for candidate in candidates:
        if candidate.is_dir():
            return _rel_path(repo_root, candidate)
    raise ValueError(f"任务不存在:{task_ref}")


def _task_dir(repo_root: Path, task_ref: str) -> Path:
    """把规范化任务引用解析为绝对路径。"""
    if task_ref.startswith(".trellis/"):
        return repo_root / task_ref
    return repo_root / ".trellis/tasks" / task_ref


def _load_task_json(repo_root: Path, task_ref: str) -> dict[str, Any]:
    """读取任务 task.json。"""
    return _read_json(_task_dir(repo_root, task_ref) / "task.json")


def _task_status(repo_root: Path, task_ref: str) -> str:
    """读取任务状态。"""
    return str(_load_task_json(repo_root, task_ref).get("status") or "unknown")


def _has_real_jsonl_entries(path: Path, repo_root: Path) -> bool:
    """判断 JSONL 上下文清单是否至少有一条真实且存在的 file entry。"""
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except OSError:
        return False

    for line in lines:
        if not line.strip():
            continue
        try:
            data = json.loads(line)
        except json.JSONDecodeError:
            continue
        file_path = data.get("file")
        if not isinstance(file_path, str) or not file_path.strip():
            continue
        if (repo_root / file_path).exists():
            return True
    return False


def _open_questions(text: str) -> list[str]:
    """提取 PRD 中仍存在的 open question 条目。"""
    lines = text.splitlines()
    collecting = False
    questions: list[str] = []
    for line in lines:
        if line.startswith("## "):
            collecting = line.strip().lower() == "## open questions"
            continue
        if collecting and line.strip().startswith("-"):
            item = line.strip().lstrip("-").strip()
            if item and item.upper() not in {"TBD", "N/A"}:
                questions.append(item)
    return questions


def _start_gate(
    repo_root: Path,
    task_ref: str,
    route_authorization: dict[str, Any] | None = None,
) -> tuple[str, dict[str, Any]]:
    """检查 planning -> start 前置条件。"""
    task_dir = _task_dir(repo_root, task_ref)
    prd = task_dir / "prd.md"
    if not prd.is_file():
        return "blocked", {"reason": "missing-prd", "message": "缺少 prd.md"}

    questions = _open_questions(prd.read_text(encoding="utf-8"))
    if questions:
        return "blocked", {
            "reason": "open-questions",
            "message": "PRD 仍有阻塞性 Open Questions",
            "questions": questions,
        }

    design = task_dir / "design.md"
    implement = task_dir / "implement.md"
    if design.exists() != implement.exists():
        missing = "implement.md" if design.exists() else "design.md"
        return "blocked", {"reason": "incomplete-complex-artifacts", "message": f"复杂任务缺少 {missing}"}

    auth = route_authorization if isinstance(route_authorization, dict) else {}
    needs_implement_context = auth.get("implement") != "inline"
    needs_check_context = auth.get("check") != "check-all-inline"
    if needs_implement_context and not _has_real_jsonl_entries(task_dir / "implement.jsonl", repo_root):
        return "blocked", {
            "reason": "missing-implement-context",
            "message": "implement.jsonl 未 curated（当前 route 可能需要 sub-agent context）",
        }
    if needs_check_context and not _has_real_jsonl_entries(task_dir / "check.jsonl", repo_root):
        return "blocked", {
            "reason": "missing-check-context",
            "message": "check.jsonl 未 curated（当前 route 可能需要 sub-agent context）",
        }

    if not (task_dir / "brief.md").is_file():
        return "action", {
            "action": "refresh_brief",
            "message": "缺少 brief.md，需先用 trellis-task-brief 生成任务摘要",
        }

    return "ok", {"message": "start gate satisfied"}


def _current_session_key(repo_root: Path) -> str | None:
    """尽力解析当前 session key，用于把 auto run 绑定给 route helper。"""
    override = os.environ.get("TRELLIS_CONTEXT_ID")
    if override:
        return override.strip() or None

    result = subprocess.run(
        ["python3", str(repo_root / ".trellis/scripts/task.py"), "current", "--source"],
        cwd=repo_root,
        check=False,
        text=True,
        capture_output=True,
    )
    for line in result.stdout.splitlines():
        if line.startswith("Source: "):
            source = line.split(": ", 1)[1].strip()
            if source.startswith(("session:", "session-fallback:")):
                return source.split(":", 1)[1].strip()
    return None


def _link_session_run(repo_root: Path, run_id: str) -> None:
    """把当前 auto run 写入 session runtime，方便 route_state.py 精确恢复。"""
    context_key = _current_session_key(repo_root)
    if not context_key:
        return
    path = repo_root / ".trellis/.runtime/sessions" / f"{context_key}.json"
    context = _read_json(path)
    context.setdefault("platform", context_key.split("_", 1)[0] if "_" in context_key else "session")
    context["last_seen_at"] = _utc_now()
    context["current_auto_run"] = run_id
    _write_json(path, context)


def _load_current_state(repo_root: Path, run_id: str | None = None) -> tuple[Path, dict[str, Any]]:
    """加载指定或当前 auto run 状态。"""
    if run_id:
        path = _run_path(repo_root, run_id)
        state = _read_json(path)
        if not state:
            raise ValueError(f"auto run 不存在:{run_id}")
        return path, state

    pointer = _read_json(_current_pointer(repo_root))
    current = pointer.get("run_id")
    if isinstance(current, str) and current:
        path = _run_path(repo_root, current)
        state = _read_json(path)
        if state and state.get("status") == "running":
            return path, state
        if state and state.get("status") in {"completed", "stopped"}:
            _clear_pointer_if_current(repo_root, current)

    running: list[tuple[Path, dict[str, Any]]] = []
    for path in _run_paths(repo_root):
        state = _read_json(path)
        if state.get("status") == "running":
            running.append((path, state))
    if len(running) == 1:
        return running[0]
    if isinstance(current, str) and current:
        path = _run_path(repo_root, current)
        state = _read_json(path)
        if state:
            return path, state
    raise ValueError("没有可恢复的唯一 auto run")


def _load_blocked_state(repo_root: Path, run_id: str | None = None) -> tuple[Path, dict[str, Any]]:
    """加载可重试的 blocked auto run。"""
    if run_id:
        path, state = _load_current_state(repo_root, run_id)
        if _blocked_items(state):
            return path, state
        raise ValueError(f"auto run 没有 blocked 队列项:{run_id}")

    try:
        path, state = _load_current_state(repo_root)
        if _blocked_items(state):
            return path, state
    except ValueError:
        pass

    blocked_runs: list[tuple[Path, dict[str, Any]]] = []
    for path in _run_paths(repo_root):
        state = _read_json(path)
        if state and _blocked_items(state):
            blocked_runs.append((path, state))
    if len(blocked_runs) == 1:
        return blocked_runs[0]
    if not blocked_runs:
        raise ValueError("没有可重试的 blocked auto run")
    run_ids = ", ".join(str(state.get("run_id") or path.stem) for path, state in blocked_runs[:8])
    raise ValueError(f"存在多个 blocked auto run，请指定 --run-id。候选:{run_ids}")


def _recent_run_summaries(repo_root: Path, limit: int = 8) -> list[dict[str, Any]]:
    """返回最近 auto run 的轻量状态列表。"""
    runs: list[dict[str, Any]] = []
    for path in _run_paths(repo_root)[:limit]:
        state = _read_json(path)
        if not state:
            continue
        counts = _queue_counts(state)
        current = _current_queue_item(state)
        runs.append({
            "run_id": state.get("run_id") or path.stem,
            "path": _rel_path(repo_root, path),
            "run_status": state.get("status"),
            "profile": state.get("profile"),
            "check_depth": _requested_check_depth(state),
            "updated_at": state.get("updated_at"),
            "completed": counts["completed"],
            "blocked": counts["blocked"],
            "remaining": counts["remaining"],
            "current_task": current.get("task") if current else None,
            "next_step": current.get("current_step") if current else "done",
        })
    return runs


def _write_state(path: Path, state: dict[str, Any]) -> None:
    """刷新 auto run 状态和更新时间。"""
    state["updated_at"] = _utc_now()
    state.pop("resume_capsule", None)
    _write_json(path, state)


def _write_pointer(repo_root: Path, run_id: str) -> None:
    """写入当前 auto run 指针。"""
    _write_json(_current_pointer(repo_root), {"run_id": run_id, "updated_at": _utc_now()})


def _clear_pointer_if_current(repo_root: Path, run_id: str | None) -> None:
    """当 current 指针仍指向本 run 时删除，避免 stale pointer 影响后续恢复。"""
    if not run_id:
        return
    pointer_path = _current_pointer(repo_root)
    pointer = _read_json(pointer_path)
    if pointer.get("run_id") != run_id:
        return
    try:
        pointer_path.unlink()
    except FileNotFoundError:
        pass


def _resume_capsule(state: dict[str, Any]) -> dict[str, Any]:
    """生成短小的人类可读恢复摘要。"""
    queue = _queue_items(state)
    current = _current_queue_item(state)
    auto_completed = [item.get("task") for item in queue if item.get("status") == "completed"]
    blocked = [item.get("task") for item in queue if item.get("status") == "blocked"]
    counts = _queue_counts(state)
    return {
        "run_id": state.get("run_id"),
        "status": state.get("status"),
        "check_depth": _requested_check_depth(state),
        "current_task": current.get("task") if current else None,
        "next_step": current.get("current_step") if current else "done",
        "completed": counts["completed"],
        "blocked": counts["blocked"],
        "remaining": counts["remaining"],
        "auto_completed_tasks": auto_completed,
        "task_lifecycle_note": "auto-loop completed means local commit completed; run finish-work/archive explicitly when ready" if auto_completed else None,
        "blocked_tasks": blocked,
    }


def _terminal_status(queue: list[Any]) -> str:
    """根据队列终态区分全完成和带阻塞结束。"""
    has_blocked = any(isinstance(item, dict) and item.get("status") == "blocked" for item in queue)
    return "blocked" if has_blocked else "completed"


def _queue_items(state: dict[str, Any]) -> list[dict[str, Any]]:
    """返回合法队列项。"""
    queue = state.get("queue") if isinstance(state.get("queue"), list) else []
    return [item for item in queue if isinstance(item, dict)]


def _queue_counts(state: dict[str, Any]) -> dict[str, int]:
    """返回队列状态计数。"""
    queue = _queue_items(state)
    return {
        "total": len(queue),
        "completed": sum(1 for item in queue if item.get("status") == "completed"),
        "blocked": sum(1 for item in queue if item.get("status") == "blocked"),
        "remaining": sum(1 for item in queue if item.get("status") in {"pending", "running"}),
    }


def _current_queue_item(state: dict[str, Any]) -> dict[str, Any] | None:
    """返回当前待处理队列项。"""
    for item in _queue_items(state):
        if item.get("status") in {"pending", "running"}:
            return item
    return None


def _blocked_items(state: dict[str, Any]) -> list[dict[str, Any]]:
    """返回 blocked 队列项。"""
    return [item for item in _queue_items(state) if item.get("status") == "blocked"]


def _outstanding_action(state: dict[str, Any]) -> dict[str, Any] | None:
    """返回当前等待 record 回写的 action。"""
    for item in _queue_items(state):
        if item.get("status") != "running":
            continue
        last_action = item.get("last_action")
        if isinstance(last_action, dict):
            return {
                "task": item.get("task"),
                "action": last_action.get("action"),
                "current_step": last_action.get("current_step"),
                "issued_at": last_action.get("issued_at"),
            }
        return None
    return None


def _completed_task_summaries(state: dict[str, Any]) -> list[dict[str, Any]]:
    """返回已完成任务的紧凑摘要。"""
    completed: list[dict[str, Any]] = []
    for item in _queue_items(state):
        if item.get("status") != "completed":
            continue
        task = {"task": item.get("task")}
        if item.get("commit"):
            task["commit"] = item.get("commit")
        completed.append(task)
    return completed


def _blocked_task_summaries(state: dict[str, Any], include_detail: bool = False) -> list[dict[str, Any]]:
    """返回 blocked 任务摘要，默认不带 detail。"""
    blocked_tasks: list[dict[str, Any]] = []
    for item in _blocked_items(state):
        blocked = item.get("blocked")
        blocked_data = blocked if isinstance(blocked, dict) else {}
        task = {
            "task": item.get("task"),
            "reason": blocked_data.get("reason"),
            "summary": blocked_data.get("summary"),
            "blocked_at": blocked_data.get("blocked_at"),
        }
        if include_detail:
            task["detail"] = blocked_data.get("detail") or {}
        blocked_tasks.append(task)
    return blocked_tasks


def _pending_task_summaries(state: dict[str, Any], include_status: bool = False) -> list[dict[str, Any]]:
    """返回未完成任务摘要。"""
    pending: list[dict[str, Any]] = []
    for item in _queue_items(state):
        if item.get("status") not in {"pending", "running"}:
            continue
        task = {
            "task": item.get("task"),
            "status": item.get("status"),
            "current_step": item.get("current_step"),
        }
        if include_status:
            task["last_failure"] = item.get("last_failure")
            task["attempts"] = item.get("attempts")
        pending.append(task)
    return pending


def _compact_summary(state: dict[str, Any]) -> dict[str, Any]:
    """返回默认给 agent 消费的紧凑状态摘要。"""
    current = _current_queue_item(state)
    completed_tasks = _completed_task_summaries(state)
    summary = {
        "run_id": state.get("run_id"),
        "run_status": state.get("status"),
        "profile": state.get("profile"),
        "check_depth": _requested_check_depth(state),
        "current_index": state.get("current_index"),
        "current_task": current.get("task") if current else None,
        "next_step": current.get("current_step") if current else "done",
        "outstanding_action": _outstanding_action(state),
        "queue_counts": _queue_counts(state),
        "completed_tasks": completed_tasks,
        "blocked_tasks": _blocked_task_summaries(state),
        "pending_tasks": _pending_task_summaries(state),
        "recent_decisions": _decision_tail(state, 3, include_data=False),
    }
    if completed_tasks:
        summary["task_lifecycle_note"] = "completed 仅表示 auto-loop item 已本地提交；任务归档仍需显式 finish-work/archive"
    return summary


def _summary(state: dict[str, Any]) -> dict[str, Any]:
    """返回 verbose 诊断状态摘要。"""
    summary = _compact_summary(state)
    summary.update({
        "blocked_tasks": _blocked_task_summaries(state, include_detail=True),
        "pending_tasks": _pending_task_summaries(state, include_status=True),
        "recent_decisions": _decision_tail(state, DECISION_LOG_LIMIT, include_data=True),
        "resume_capsule": _resume_capsule(state),
    })
    return summary


def _format_summary(state: dict[str, Any], args: argparse.Namespace | None = None) -> dict[str, Any]:
    """根据 --verbose 返回紧凑或详细摘要。"""
    if args is not None and getattr(args, "verbose", False):
        return _summary(state)
    return _compact_summary(state)


def _make_item(repo_root: Path, task_ref: str) -> dict[str, Any]:
    """构造队列任务项。"""
    normalized = _normalize_task_ref(repo_root, task_ref)
    status = _task_status(repo_root, normalized)
    step = "start_task" if status == "planning" else "implement"
    return {
        "task": normalized,
        "status": "pending",
        "task_status": status,
        "current_step": step,
        "attempts": {"fix_recheck": 0},
        "last_failure": None,
        "last_check": None,
        "last_action": None,
        "commit": None,
        "blocked": None,
        "decision_log": [],
        "updated_at": _utc_now(),
    }


def _action(action: str, item: dict[str, Any], extra: dict[str, Any] | None = None) -> dict[str, Any]:
    """构造下一步动作对象。"""
    task = item.get("task")
    base: dict[str, Any] = {
        "status": "action",
        "action": action,
        "task": task,
        "current_step": item.get("current_step"),
    }
    if action == "start_task":
        task_name = Path(str(task)).name
        base["command"] = f"python3 ./.trellis/scripts/task.py start {task_name}"
    elif action == "refresh_brief":
        base["instruction"] = "运行 trellis-task-brief 生成 brief.md，然后 record --result ok。"
    elif action == "run_implement":
        base["instruction"] = "进入 Phase 2.1 implement route，并执行实现。"
    elif action == "run_check_all":
        base["instruction"] = "进入 Phase 2.2 check route，按 requested_check_depth 执行统一 Check-All；完成后立即 record + next。"
    elif action == "run_fix":
        base["instruction"] = "根据最近失败摘要修复问题。"
    elif action == "run_recheck":
        base["instruction"] = "修复后重新执行统一 Check-All，不得低于 minimum_check_depth；完成后立即 record + next。"
    elif action == "run_spec_update":
        base["instruction"] = (
            "执行 trellis-update-spec 并读取 spec_update_result：no-op/written 时 "
            "record --action run_spec_update --result ok 后立即 next；needs-review 时 "
            "record --action run_spec_update --result blocked --failure-type spec-needs-review。"
        )
    elif action == "commit_only":
        base["instruction"] = "进入 trellis-push commit-only 语义：AI 先生成当前任务提交计划并复核 Git 状态，只提交可归属文件，不 push。"
    if extra:
        base.update(extra)
    return base


def _remember_action(item: dict[str, Any], action_data: dict[str, Any]) -> dict[str, Any]:
    """记录 runner 已发出的待回写 action。"""
    item["last_action"] = {
        "action": action_data.get("action"),
        "current_step": action_data.get("current_step"),
        "issued_at": _utc_now(),
    }
    item["updated_at"] = _utc_now()
    return action_data


def _outstanding_action_name(item: dict[str, Any]) -> str | None:
    """返回当前任务等待 record 回写的 action 名。"""
    last_action = item.get("last_action")
    if isinstance(last_action, dict) and isinstance(last_action.get("action"), str):
        return last_action["action"]
    return None


def _minimum_check_depth(item: dict[str, Any], action: str) -> str | None:
    """返回检查续跑的最小有效深度；旧 recheck 状态按 full 兼容。"""
    if action not in {"run_check_all", "run_recheck"}:
        return None
    last_check = item.get("last_check")
    if not isinstance(last_check, dict):
        return "full" if action == "run_recheck" else None
    effective = last_check.get("effective_depth")
    return str(effective) if effective in {"light", "full"} else "full"


def _block_item(item: dict[str, Any], reason: str, summary: str, detail: dict[str, Any] | None = None) -> None:
    """把当前任务标记为 blocked。"""
    item["status"] = "blocked"
    item["blocked"] = {
        "reason": reason,
        "summary": summary,
        "detail": detail or {},
        "blocked_at": _utc_now(),
    }
    item["updated_at"] = _utc_now()
    _append_item_decision(
        item,
        "blocked",
        summary,
        {"reason": reason, "detail": detail or {}, "queue_continues": True},
    )


def _next_item(repo_root: Path, state: dict[str, Any]) -> tuple[dict[str, Any] | None, dict[str, Any]]:
    """计算并更新队列中的下一步动作。"""
    queue = state.get("queue") if isinstance(state.get("queue"), list) else []
    for index, item in enumerate(queue):
        if not isinstance(item, dict) or item.get("status") not in {"pending", "running"}:
            continue

        state["current_index"] = index
        item["status"] = "running"
        task = str(item.get("task"))
        item["task_status"] = _task_status(repo_root, task)

        if item["task_status"] == "planning":
            gate_status, gate = _start_gate(repo_root, task, _effective_route_authorization(repo_root, task, state.get("route_authorization")))
            if gate_status == "blocked":
                _block_item(item, gate["reason"], gate["message"], gate)
                continue
            if gate_status == "action":
                item["current_step"] = "refresh_brief"
                return item, _remember_action(item, _action("refresh_brief", item, gate))
            item["current_step"] = "start_task"
            return item, _remember_action(item, _action("start_task", item))

        step = item.get("current_step") or "implement"
        if step in {"start_task", "implement"}:
            item["current_step"] = "implement"
            return item, _remember_action(item, _action("run_implement", item))
        if step == "check":
            return item, _remember_action(
                item,
                _action(
                    "run_check_all",
                    item,
                    {
                        "requested_check_depth": _requested_check_depth(state),
                        "minimum_check_depth": _minimum_check_depth(item, "run_check_all"),
                    },
                ),
            )
        if step == "fix":
            attempts = item.setdefault("attempts", {}).get("fix_recheck", 0)
            if attempts >= MAX_FIX_RECHECK:
                _block_item(item, "retry-budget-exhausted", "fix/recheck 已达到默认 3 轮预算")
                continue
            return item, _remember_action(
                item,
                _action("run_fix", item, {"attempt": attempts, "max_attempts": MAX_FIX_RECHECK}),
            )
        if step == "recheck":
            return item, _remember_action(
                item,
                _action(
                    "run_recheck",
                    item,
                    {
                        "requested_check_depth": _requested_check_depth(state),
                        "minimum_check_depth": _minimum_check_depth(item, "run_recheck"),
                    },
                ),
            )
        if step == "spec_update":
            return item, _remember_action(item, _action("run_spec_update", item))
        if step == "commit_only":
            return item, _remember_action(item, _action("commit_only", item))

        _block_item(item, "unknown-step", f"未知 current_step:{step}")

    state["status"] = _terminal_status(queue)
    return None, {
        "status": "blocked" if state["status"] == "blocked" else "done",
        "finish_work_required_for_archive": True,
        "instruction": (
            "auto-loop 队列存在 blocked 项；补齐条件后运行 retry-blocked 继续同一个 run。"
            if state["status"] == "blocked"
            else "auto-loop 队列已结束；如需归档任务，请用户显式运行 trellis-finish-work。"
        ),
        "summary": _compact_summary(state),
    }


def cmd_start(args: argparse.Namespace) -> int:
    """创建 auto run。"""
    repo_root = _repo_root()
    if repo_root is None:
        return _print({"status": "error", "reason": "not-trellis-project"})

    try:
        current_path, current = _load_current_state(repo_root)
        if current.get("status") == "running" and not args.force:
            return _print({
                "status": "error",
                "reason": "auto-run-already-running",
                "run_id": current.get("run_id"),
                "path": _rel_path(repo_root, current_path),
            })
        if current.get("status") == "blocked" and not args.force:
            return _print({
                "status": "error",
                "reason": "auto-run-blocked-retry-available",
                "run_id": current.get("run_id"),
                "path": _rel_path(repo_root, current_path),
                "suggested_command": f"python3 ./.trellis/scripts/auto_loop.py retry-blocked --run-id {current.get('run_id')}",
                "summary": _format_summary(current, args),
            })
    except ValueError:
        pass

    route_authorization: dict[str, str] = {}
    if args.route_implement:
        route_authorization["implement"] = args.route_implement
    if args.route_check:
        route_authorization["check"] = args.route_check

    run_id = args.run_id or _new_run_id()
    queue = [_make_item(repo_root, task) for task in args.tasks]
    now = _utc_now()
    state = {
        "schema_version": SCHEMA_VERSION,
        "run_id": run_id,
        "status": "running",
        "profile": args.profile,
        "check_depth": args.check_depth,
        "created_at": now,
        "updated_at": now,
        "owner": {"host": socket.gethostname(), "pid": os.getpid()},
        "current_index": 0,
        "route_authorization": route_authorization,
        "queue": queue,
    }
    path = _run_path(repo_root, run_id)
    _write_state(path, state)
    _write_pointer(repo_root, run_id)
    _link_session_run(repo_root, run_id)
    return _print({"status": "started", "path": _rel_path(repo_root, path), **_format_summary(state, args)})


def cmd_resume(args: argparse.Namespace) -> int:
    """恢复 auto run 摘要。"""
    repo_root = _repo_root()
    if repo_root is None:
        return _print({"status": "error", "reason": "not-trellis-project"})
    try:
        path, state = _load_current_state(repo_root, args.run_id)
    except ValueError as exc:
        return _print({"status": "error", "reason": "resume-failed", "message": str(exc)})
    _link_session_run(repo_root, str(state.get("run_id")))
    output = {"status": "resumed", "path": _rel_path(repo_root, path), **_format_summary(state, args)}
    if getattr(args, "verbose", False):
        output["resume_capsule"] = _resume_capsule(state)
    return _print(output)


def cmd_next(args: argparse.Namespace) -> int:
    """计算下一步动作。"""
    repo_root = _repo_root()
    if repo_root is None:
        return _print({"status": "error", "reason": "not-trellis-project"})
    try:
        path, state = _load_current_state(repo_root, args.run_id)
    except ValueError as exc:
        return _print({"status": "error", "reason": "next-failed", "message": str(exc)})
    _link_session_run(repo_root, str(state.get("run_id")))
    if state.get("status") != "running":
        output_status = "done" if state.get("status") == "completed" else state.get("status") or "unknown"
        return _print({"run_id": state.get("run_id"), "status": output_status, "summary": _format_summary(state, args)})
    _, action = _next_item(repo_root, state)
    _write_state(path, state)
    if state.get("status") == "completed":
        _clear_pointer_if_current(repo_root, str(state.get("run_id") or ""))
    if action.get("status") in {"done", "blocked"}:
        action["summary"] = _format_summary(state, args)
    return _print({"run_id": state.get("run_id"), **action})


def _apply_route_authorization_args(state: dict[str, Any], args: argparse.Namespace) -> dict[str, str]:
    """把命令行 route 参数合并进 run 的临时授权。"""
    route_authorization = state.get("route_authorization")
    if not isinstance(route_authorization, dict):
        route_authorization = {}
    if getattr(args, "route_implement", None):
        route_authorization["implement"] = args.route_implement
    if getattr(args, "route_check", None):
        route_authorization["check"] = args.route_check
    state["route_authorization"] = route_authorization
    return {str(key): str(value) for key, value in route_authorization.items()}


def _apply_check_depth_arg(state: dict[str, Any], args: argparse.Namespace) -> tuple[str, str, bool]:
    """把 retry 参数中的检查深度合并到 run 状态。"""
    previous = _requested_check_depth(state)
    requested = getattr(args, "check_depth", None)
    if requested in VALID_CHECK_DEPTHS:
        state["check_depth"] = requested
    current = _requested_check_depth(state)
    return previous, current, previous != current


def _blocked_reason(item: dict[str, Any]) -> str:
    """读取队列项 blocked reason。"""
    blocked = item.get("blocked")
    if isinstance(blocked, dict):
        reason = blocked.get("reason")
        if isinstance(reason, str):
            return reason
    return ""


def cmd_retry_blocked(args: argparse.Namespace) -> int:
    """把可恢复的 blocked 队列项重置为 pending，复用同一个 auto run。"""
    repo_root = _repo_root()
    if repo_root is None:
        return _print({"status": "error", "reason": "not-trellis-project"})
    try:
        path, state = _load_blocked_state(repo_root, args.run_id)
    except ValueError as exc:
        return _print({
            "status": "error",
            "reason": "retry-blocked-failed",
            "message": str(exc),
            "runs": _recent_run_summaries(repo_root),
        })

    task = _normalize_task_ref(repo_root, args.task) if args.task else None
    route_authorization = _apply_route_authorization_args(state, args)
    previous_check_depth, check_depth, check_depth_changed = _apply_check_depth_arg(state, args)
    queue = state.get("queue") if isinstance(state.get("queue"), list) else []
    reset: list[str] = []
    skipped: list[dict[str, str]] = []

    for index, item in enumerate(queue):
        if not isinstance(item, dict) or item.get("status") != "blocked":
            continue
        if task and item.get("task") != task:
            continue
        reason = _blocked_reason(item)
        if not args.all and not task and reason not in RECOVERABLE_BLOCK_REASONS:
            skipped.append({"task": str(item.get("task")), "reason": reason or "unknown"})
            continue
        item["status"] = "pending"
        item["blocked"] = None
        item["last_action"] = None
        item["task_status"] = _task_status(repo_root, str(item.get("task")))
        item["updated_at"] = _utc_now()
        state["current_index"] = min(int(state.get("current_index") or index), index)
        reset.append(str(item.get("task")))
        _append_item_decision(
            item,
            "retry_unblocked",
            "blocked 队列项已重置，将在同一个 auto run 内重试",
            {
                "previous_reason": reason,
                "route_authorization": route_authorization,
                "check_depth": check_depth,
                "previous_check_depth": previous_check_depth if check_depth_changed else None,
            },
        )

    if not reset:
        return _print({
            "status": "error",
            "reason": "no-retryable-blocked-items",
            "message": "没有可自动重试的 blocked 队列项；如确认要重试，可加 --all 或指定 --task。",
            "skipped": skipped,
            "summary": _format_summary(state, args),
        })

    state["status"] = "running"
    _write_state(path, state)
    _write_pointer(repo_root, str(state.get("run_id")))
    _link_session_run(repo_root, str(state.get("run_id")))
    return _print({
        "status": "retry-ready",
        "run_id": state.get("run_id"),
        "path": _rel_path(repo_root, path),
        "reset": reset,
        "skipped": skipped,
        "summary": _format_summary(state, args),
    })


def _find_record_item(state: dict[str, Any], task_ref: str | None) -> dict[str, Any] | None:
    """找到 record 要更新的队列项。"""
    queue = state.get("queue") if isinstance(state.get("queue"), list) else []
    if task_ref:
        for item in queue:
            if isinstance(item, dict) and item.get("task") == task_ref:
                return item
    for item in queue:
        if isinstance(item, dict) and item.get("status") == "running":
            return item
    return None


def _advance_after_ok(item: dict[str, Any], action: str, args: argparse.Namespace) -> None:
    """根据成功动作推进 current_step。"""
    if action in ROUTE_ACTION_TARGETS:
        target = ROUTE_ACTION_TARGETS[action]
        mode = getattr(args, "route_mode", None)
        source = getattr(args, "route_source", None)
        data = {"target": target}
        if mode:
            data["mode"] = mode
        if source:
            data["source"] = source
        summary = f"{target} route resolved"
        if mode:
            summary += f" to {mode}"
        if source:
            summary += f" from {source}"
        _append_item_decision(item, "route_resolved", summary, data)

    if action == "refresh_brief":
        item["current_step"] = "start_task"
    elif action == "start_task":
        item["current_step"] = "implement"
    elif action in {"run_implement", "run_fix"}:
        item["current_step"] = "check" if action == "run_implement" else "recheck"
    elif action in {"run_check_all", "run_recheck"}:
        item["current_step"] = "spec_update"
    elif action == "run_spec_update":
        item["current_step"] = "commit_only"
    elif action == "commit_only":
        item["status"] = "completed"
        item["current_step"] = "done"
        item["commit"] = args.commit
        if args.summary:
            item["commit_summary"] = args.summary
        _append_item_decision(
            item,
            "commit_plan",
            args.summary or "trellis-push commit-only 计划已执行",
            {
                "planned_files": args.files or [],
                "retained_files": args.retained_files or [],
                "commit_message": args.commit_message,
                "snapshot_commit": args.snapshot_commit,
            },
        )
        _append_item_decision(
            item,
            "commit_completed",
            "trellis-push commit-only 本地提交完成",
            {
                "commit": args.commit,
                "commit_message": args.commit_message,
                "snapshot_commit": args.snapshot_commit,
            },
        )
        _append_item_decision(
            item,
            "task_auto_completed",
            "auto-loop item 已完成本地提交；任务生命周期仍等待 finish-work/archive",
            {
                "commit": args.commit,
                "summary": args.summary,
                "task_status": item.get("task_status"),
            },
        )
    item["updated_at"] = _utc_now()


def _record_failure(item: dict[str, Any], action: str, args: argparse.Namespace) -> None:
    """记录失败并决定是否进入 fix 或 blocked。"""
    item["last_failure"] = {
        "action": action,
        "failure_type": args.failure_type,
        "summary": args.summary,
        "files": args.files or [],
        "failed_at": _utc_now(),
    }
    if action in {"run_implement", "run_check_all", "run_fix", "run_recheck"}:
        attempts = item.setdefault("attempts", {})
        attempts["fix_recheck"] = int(attempts.get("fix_recheck", 0)) + 1
        if attempts["fix_recheck"] > MAX_FIX_RECHECK:
            _block_item(item, "retry-budget-exhausted", args.summary or "fix/recheck 达到默认 3 轮预算")
        else:
            item["current_step"] = "fix"
            item["updated_at"] = _utc_now()
        _append_item_decision(
            item,
            "warning",
            args.summary or f"{action} 执行失败，进入修复流程",
            {
                "action": action,
                "failure_type": args.failure_type,
                "files": args.files or [],
                "attempts": item.setdefault("attempts", {}).get("fix_recheck", 0),
            },
        )
        return
    _block_item(item, args.failure_type or "action-failed", args.summary or f"{action} 执行失败")


def _check_record_error(
    state: dict[str, Any],
    item: dict[str, Any],
    action: str,
    args: argparse.Namespace,
) -> dict[str, Any] | None:
    """校验检查结果没有低于请求或重检要求的最小深度。"""
    if action not in {"run_check_all", "run_recheck"}:
        return None
    effective = getattr(args, "effective_check_depth", None) or "full"
    requested = _requested_check_depth(state)
    minimum = _minimum_check_depth(item, action)
    if effective == "light" and (requested == "full" or minimum == "full"):
        return {
            "status": "error",
            "reason": "check-depth-below-minimum",
            "requested_check_depth": requested,
            "minimum_check_depth": minimum,
            "effective_check_depth": effective,
            "message": "检查结果低于 run 请求或重检要求的最小深度，请按 full 完成 Check-All 后重新 record。",
        }
    return None


def _record_check_result(
    state: dict[str, Any],
    item: dict[str, Any],
    action: str,
    args: argparse.Namespace,
) -> None:
    """保存 Check-All 实际深度与结果，供审计和后续 recheck 使用。"""
    if action not in {"run_check_all", "run_recheck"}:
        return
    provided_effective = getattr(args, "effective_check_depth", None)
    effective = provided_effective or "full"
    if provided_effective is None:
        reason = "legacy-default-full"
    else:
        reason = getattr(args, "check_depth_reason", None) or "未提供深度原因"
    last_check = {
        "action": action,
        "requested_depth": _requested_check_depth(state),
        "minimum_depth": _minimum_check_depth(item, action),
        "effective_depth": effective,
        "reason": reason,
        "result": args.result,
        "recorded_at": _utc_now(),
    }
    item["last_check"] = last_check
    _append_item_decision(
        item,
        "check_recorded",
        f"Check-All {args.result}: requested={last_check['requested_depth']} effective={effective}",
        last_check,
    )


def cmd_record(args: argparse.Namespace) -> int:
    """记录 agent 执行结果。"""
    repo_root = _repo_root()
    if repo_root is None:
        return _print({"status": "error", "reason": "not-trellis-project"})
    try:
        path, state = _load_current_state(repo_root, args.run_id)
    except ValueError as exc:
        return _print({"status": "error", "reason": "record-failed", "message": str(exc)})
    if state.get("status") != "running":
        return _print({
            "status": "error",
            "reason": "auto-run-not-running",
            "run_status": state.get("status"),
            "summary": _format_summary(state, args),
        })

    task = _normalize_task_ref(repo_root, args.task) if args.task else None
    item = _find_record_item(state, task)
    if item is None:
        return _print({"status": "error", "reason": "no-running-task"})

    if not args.action:
        return _print({"status": "error", "reason": "missing-action", "message": "record 必须显式传入 --action"})
    expected_action = _outstanding_action_name(item)
    if expected_action is None:
        return _print({
            "status": "error",
            "reason": "no-outstanding-action",
            "message": "没有等待回写的 action；请先运行 next 获取下一步。",
            "task": item.get("task"),
            "current_step": item.get("current_step"),
        })
    if args.action != expected_action:
        return _print({
            "status": "error",
            "reason": "action-mismatch",
            "expected_action": expected_action,
            "actual_action": args.action,
            "task": item.get("task"),
            "current_step": item.get("current_step"),
        })

    action = args.action
    check_error = _check_record_error(state, item, action, args)
    if check_error is not None:
        check_error.update({"task": item.get("task"), "action": action})
        return _print(check_error)

    _record_check_result(state, item, action, args)
    item["last_action"] = None
    if args.result == "ok":
        _advance_after_ok(item, action, args)
    elif args.result == "failed":
        _record_failure(item, action, args)
    else:
        _block_item(item, args.failure_type or "blocked", args.summary or "agent 标记 blocked")
        if action == "commit_only":
            _append_item_decision(
                item,
                "task_skipped",
                args.summary or "commit-only 预检不安全，当前任务已跳过，队列继续",
                {"failure_type": args.failure_type, "files": args.files or []},
            )

    _write_state(path, state)
    output = {
        "status": "recorded",
        "run_id": state.get("run_id"),
        "task": item.get("task"),
        "item_status": item.get("status"),
        "current_step": item.get("current_step"),
        "commit": item.get("commit"),
        "summary": _format_summary(state, args),
    }
    if getattr(args, "verbose", False):
        output["item"] = item
    return _print(output)


def cmd_status(args: argparse.Namespace) -> int:
    """输出 auto run 状态。"""
    repo_root = _repo_root()
    if repo_root is None:
        return _print({"status": "error", "reason": "not-trellis-project"})
    try:
        path, state = _load_current_state(repo_root, args.run_id)
    except ValueError as exc:
        return _print({
            "status": "ok",
            "run_status": "no-current-run",
            "reason": "status-list",
            "message": str(exc),
            "runs": _recent_run_summaries(repo_root),
        })
    return _print({"status": "ok", "path": _rel_path(repo_root, path), **_format_summary(state, args)})


def cmd_stop(args: argparse.Namespace) -> int:
    """停止 auto run。"""
    repo_root = _repo_root()
    if repo_root is None:
        return _print({"status": "error", "reason": "not-trellis-project"})
    try:
        path, state = _load_current_state(repo_root, args.run_id)
    except ValueError as exc:
        return _print({"status": "error", "reason": "stop-failed", "message": str(exc)})
    state["status"] = "stopped"
    state["stop_reason"] = args.reason
    _write_state(path, state)
    _clear_pointer_if_current(repo_root, str(state.get("run_id") or ""))
    return _print({"status": "stopped", "run_id": state.get("run_id"), "path": _rel_path(repo_root, path), "reason": args.reason})


def build_parser() -> argparse.ArgumentParser:
    """构造命令行解析器。"""
    parser = argparse.ArgumentParser(description="Trellis auto loop runner.")
    subparsers = parser.add_subparsers(dest="command", required=True)

    start = subparsers.add_parser("start", help="create an auto loop run")
    start.add_argument("--tasks", nargs="+", required=True)
    start.add_argument("--run-id")
    start.add_argument("--profile", choices=(DEFAULT_PROFILE,), default=DEFAULT_PROFILE)
    start.add_argument("--check-depth", choices=sorted(VALID_CHECK_DEPTHS), default="auto")
    start.add_argument("--route-implement", choices=sorted(VALID_IMPLEMENT_ROUTES))
    start.add_argument("--route-check", choices=sorted(VALID_CHECK_ROUTES))
    start.add_argument("--force", action="store_true")
    start.add_argument("--verbose", action="store_true")
    start.set_defaults(func=cmd_start)

    for name, func in (("resume", cmd_resume), ("next", cmd_next), ("status", cmd_status)):
        sub = subparsers.add_parser(name)
        sub.add_argument("--run-id")
        sub.add_argument("--verbose", action="store_true")
        sub.set_defaults(func=func)

    record = subparsers.add_parser("record")
    record.add_argument("--run-id")
    record.add_argument("--task")
    record.add_argument("--action")
    record.add_argument("--result", choices=("ok", "failed", "blocked"), required=True)
    record.add_argument("--failure-type")
    record.add_argument("--summary")
    record.add_argument("--files", nargs="*")
    record.add_argument("--retained-files", nargs="*")
    record.add_argument("--commit")
    record.add_argument("--commit-message")
    record.add_argument("--snapshot-commit")
    record.add_argument("--route-mode")
    record.add_argument("--route-source")
    record.add_argument("--effective-check-depth", choices=("light", "full"))
    record.add_argument("--check-depth-reason")
    record.add_argument("--verbose", action="store_true")
    record.set_defaults(func=cmd_record)

    retry = subparsers.add_parser("retry-blocked")
    retry.add_argument("--run-id")
    retry.add_argument("--task")
    retry.add_argument("--route-implement", choices=sorted(VALID_IMPLEMENT_ROUTES))
    retry.add_argument("--route-check", choices=sorted(VALID_CHECK_ROUTES))
    retry.add_argument("--check-depth", choices=sorted(VALID_CHECK_DEPTHS))
    retry.add_argument("--all", action="store_true")
    retry.add_argument("--verbose", action="store_true")
    retry.set_defaults(func=cmd_retry_blocked)

    stop = subparsers.add_parser("stop")
    stop.add_argument("--run-id")
    stop.add_argument("--reason")
    stop.set_defaults(func=cmd_stop)

    return parser


def main() -> int:
    """脚本入口。"""
    parser = build_parser()
    args = parser.parse_args()
    try:
        return args.func(args)
    except ValueError as exc:
        return _print({"status": "error", "reason": "invalid-input", "message": str(exc)})


if __name__ == "__main__":
    sys.exit(main())
