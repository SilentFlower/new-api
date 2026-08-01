#!/usr/bin/env python3
"""管理当前 session 的无任务 direct-edit 完成链。"""

from __future__ import annotations

import argparse
import json
import os
import sys
import tempfile
import uuid
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from common.active_task import resolve_context_key
from git_evidence import GitEvidenceError, capture_workspace_evidence


STATE_KEY = "untracked_flow"
STATE_VERSION = 1
VALID_SOURCES = {"inferred", "user-explicit"}
VALID_STAGES = {"inspect", "implement", "check", "spec", "push"}
VALID_CLEAR_REASONS = {
    "completed",
    "abandoned",
    "adopted",
    "baseline-restored",
    "invalidated",
}
CHECK_RESULTS = {"pass", "findings", "partial", "blocked"}
SPEC_RESULTS = {"no-op", "written", "needs-review"}
VALIDATION_RESULTS = {"pass", "fail", "partial"}


class UntrackedFlowError(Exception):
    """携带稳定 reason code 的 untracked 状态错误。"""

    def __init__(self, reason: str, message: str, **details: Any) -> None:
        """初始化结构化状态错误。

        Args:
            reason: 稳定机器错误码。
            message: 中文诊断说明。
            **details: 与错误相关的状态详情。
        """
        super().__init__(message)
        self.reason = reason
        self.details = details


def _utc_now() -> str:
    """返回秒级 UTC ISO-8601 时间。"""
    return datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def _find_repo_root(start: Path | None = None) -> Path | None:
    """从当前路径向上查找 Trellis 项目根目录。"""
    current = (start or Path.cwd()).resolve()
    if current.is_file():
        current = current.parent
    while True:
        if (current / ".trellis").is_dir():
            return current
        if current == current.parent:
            return None
        current = current.parent


def _session_path(repo_root: Path, context_key: str) -> Path:
    """返回指定 session runtime 文件路径。"""
    return repo_root / ".trellis/.runtime/sessions" / f"{context_key}.json"


def _read_json_result(path: Path) -> dict[str, Any]:
    """读取 JSON，并保留缺失、损坏和 I/O 错误差异。"""
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except FileNotFoundError:
        return {"status": "missing", "data": None, "error": None}
    except json.JSONDecodeError as error:
        return {"status": "corrupt", "data": None, "error": str(error)}
    except OSError as error:
        return {"status": "io_error", "data": None, "error": str(error)}
    if not isinstance(data, dict):
        return {"status": "corrupt", "data": None, "error": "JSON 根节点不是对象"}
    return {"status": "ok", "data": data, "error": None}


def _write_json(path: Path, data: dict[str, Any]) -> None:
    """使用同目录临时文件原子替换 session runtime。"""
    path.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temp_name = tempfile.mkstemp(
        prefix=f".{path.name}.",
        suffix=".tmp",
        dir=path.parent,
    )
    temp_path = Path(temp_name)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as handle:
            handle.write(json.dumps(data, indent=2, ensure_ascii=False) + "\n")
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temp_path, path)
    except Exception:
        try:
            temp_path.unlink()
        except FileNotFoundError:
            pass
        raise


def _runtime_scope(
    repo_root: Path,
    platform_input: dict[str, Any] | None = None,
    platform: str | None = None,
    *,
    allow_active_task: bool = False,
) -> dict[str, Any]:
    """解析严格绑定当前 session 的 runtime 上下文。"""
    context_key = resolve_context_key(platform_input, platform)
    if not context_key:
        raise UntrackedFlowError("no-session-context", "无法解析当前 AI session")
    path = _session_path(repo_root, context_key)
    result = _read_json_result(path)
    if result["status"] in {"corrupt", "io_error"}:
        raise UntrackedFlowError(
            f"session-runtime-{result['status']}",
            "当前 session runtime 无法安全读取",
            error=result.get("error"),
        )
    context = result["data"] if isinstance(result.get("data"), dict) else {}
    current_task = context.get("current_task")
    if not allow_active_task and isinstance(current_task, str) and current_task.strip():
        raise UntrackedFlowError(
            "active-task-present",
            "当前 session 已绑定活动 task，不能创建无任务事项",
            task=current_task,
        )
    return {
        "context_key": context_key,
        "path": path,
        "context": context,
        "current_task": current_task,
    }


def _normalized_state(value: Any) -> dict[str, Any] | None:
    """校验并归一化 untracked 状态，非法时返回 None。"""
    if not isinstance(value, dict):
        return None
    if (
        value.get("version") != STATE_VERSION
        or value.get("mode") != "direct_edit"
        or value.get("source") not in VALID_SOURCES
        or value.get("stage") not in VALID_STAGES
        or not isinstance(value.get("id"), str)
        or not value["id"].strip()
        or not isinstance(value.get("summary"), str)
        or not value["summary"].strip()
    ):
        return None
    normalized = dict(value)
    normalized["scope"] = sorted(
        {
            str(path).replace("\\", "/")
            for path in value.get("scope", [])
            if isinstance(path, str) and path.strip()
        }
    )
    normalized["evidence"] = (
        dict(value["evidence"]) if isinstance(value.get("evidence"), dict) else {}
    )
    return normalized


def _state_summary(state: dict[str, Any]) -> dict[str, Any]:
    """返回默认输出需要的最小状态字段。"""
    return {
        "workId": state.get("id"),
        "summary": state.get("summary"),
        "source": state.get("source"),
        "stage": state.get("stage"),
        "scope": state.get("scope", []),
        "baselineCaptured": isinstance(state.get("baseline"), dict),
        "workspaceFingerprint": state.get("workspaceFingerprint"),
        "evidence": state.get("evidence", {}),
    }


def _persist(scope: dict[str, Any], state: dict[str, Any] | None) -> None:
    """保存或清理当前 session 的 untracked 字段。"""
    context = dict(scope["context"])
    context.setdefault(
        "platform",
        scope["context_key"].split("_", 1)[0] if "_" in scope["context_key"] else "session",
    )
    context["last_seen_at"] = _utc_now()
    if state is None:
        context.pop(STATE_KEY, None)
    else:
        context[STATE_KEY] = state
    _write_json(scope["path"], context)


def _normalize_paths(repo_root: Path, values: list[str]) -> list[str]:
    """校验并归一化事项文件范围。"""
    normalized: set[str] = set()
    root = repo_root.resolve()
    for raw in values:
        value = raw.strip()
        if not value:
            continue
        candidate = Path(value)
        absolute = candidate.resolve() if candidate.is_absolute() else (root / candidate).resolve()
        try:
            relative = absolute.relative_to(root).as_posix()
        except ValueError as exc:
            raise UntrackedFlowError(
                "scope-path-invalid",
                "事项范围路径逃逸项目根目录",
                path=value,
            ) from exc
        normalized.add(relative)
    if not normalized:
        raise UntrackedFlowError("scope-empty", "首次修改前必须提供至少一个事项路径")
    return sorted(normalized)


def _capture(repo_root: Path) -> dict[str, Any]:
    """捕获 workspace 证据并转换底层错误。"""
    try:
        return capture_workspace_evidence(repo_root)
    except GitEvidenceError as error:
        raise UntrackedFlowError(error.reason, str(error), **error.details) from error


def _ensure_workspace_matches(repo_root: Path, state: dict[str, Any]) -> dict[str, Any]:
    """校验当前 workspace 与状态记录的有效 fingerprint 一致。"""
    current = _capture(repo_root)
    expected = state.get("workspaceFingerprint")
    if isinstance(expected, str) and expected and current["fingerprint"] != expected:
        raise UntrackedFlowError(
            "workspace-drift",
            "当前 workspace 与 untracked 状态记录不匹配",
            expected=expected,
            actual=current["fingerprint"],
        )
    return current


def _baseline_restored(state: dict[str, Any], current: dict[str, Any]) -> bool:
    """判断已实际推进的事项是否回到原始 baseline。"""
    baseline = state.get("baseline")
    if not isinstance(baseline, dict) or current.get("fingerprint") != baseline.get("fingerprint"):
        return False
    evidence = state.get("evidence")
    return (
        state.get("stage") in {"check", "spec", "push"}
        or isinstance(state.get("workspaceFingerprint"), str)
        or isinstance(evidence, dict) and bool(evidence)
        or state.get("preparedFingerprint") != baseline.get("fingerprint")
    )


def begin_untracked_work(
    repo_root: Path,
    summary: str,
    source: str,
    platform_input: dict[str, Any] | None = None,
    platform: str | None = None,
) -> dict[str, Any]:
    """为当前 session 创建单一 untracked work item。

    Args:
        repo_root: Trellis 项目根目录。
        summary: 当前事项摘要。
        source: ``inferred`` 或 ``user-explicit``。
        platform_input: 可选平台 hook 输入。
        platform: 可选平台名称。

    Returns:
        创建或命中同一事项的结构化结果。
    """
    clean_summary = summary.strip()
    if not clean_summary:
        raise UntrackedFlowError("summary-empty", "无任务事项摘要不能为空")
    if source not in VALID_SOURCES:
        raise UntrackedFlowError("invalid-source", "无任务事项来源不合法", source=source)
    scope = _runtime_scope(repo_root, platform_input, platform)
    existing = _normalized_state(scope["context"].get(STATE_KEY))
    if existing and existing.get("baseline") is not None:
        current = _capture(repo_root)
        baseline = existing["baseline"]
        if (
            isinstance(baseline, dict)
            and current["fingerprint"] == baseline.get("fingerprint")
            and (existing["summary"] != clean_summary or _baseline_restored(existing, current))
        ):
            _persist(scope, None)
            scope["context"] = {
                key: value for key, value in scope["context"].items() if key != STATE_KEY
            }
            existing = None
    if existing:
        if existing["summary"] == clean_summary:
            return {"status": "hit", **_state_summary(existing)}
        raise UntrackedFlowError(
            "active-work-conflict",
            "当前 session 已有另一个活跃无任务事项",
            activeWorkId=existing["id"],
            activeSummary=existing["summary"],
        )
    if STATE_KEY in scope["context"]:
        raise UntrackedFlowError("invalid-state", "当前 session 的无任务状态已损坏")

    now = _utc_now()
    state = {
        "version": STATE_VERSION,
        "id": f"uw-{uuid.uuid4().hex[:12]}",
        "mode": "direct_edit",
        "source": source,
        "summary": clean_summary,
        "stage": "inspect",
        "baseline": None,
        "scope": [],
        "preparedFingerprint": None,
        "workspaceFingerprint": None,
        "evidence": {},
        "createdAt": now,
        "updatedAt": now,
    }
    _persist(scope, state)
    return {"status": "created", **_state_summary(state)}


def prepare_edit(
    repo_root: Path,
    paths: list[str],
    platform_input: dict[str, Any] | None = None,
    platform: str | None = None,
) -> dict[str, Any]:
    """在实际文件写入前捕获 baseline 并进入 implement。

    Args:
        repo_root: Trellis 项目根目录。
        paths: 本次准备修改的项目相对路径。
        platform_input: 可选平台 hook 输入。
        platform: 可选平台名称。

    Returns:
        更新后的事项状态与 baseline 摘要。
    """
    scope = _runtime_scope(repo_root, platform_input, platform)
    state = _normalized_state(scope["context"].get(STATE_KEY))
    if state is None:
        raise UntrackedFlowError("no-active-work", "当前 session 没有活跃无任务事项")

    current = _capture(repo_root)
    expected = state.get("workspaceFingerprint")
    if isinstance(expected, str):
        if current["fingerprint"] != expected:
            raise UntrackedFlowError(
                "workspace-drift",
                "进入新一轮修改前 workspace 已偏离已验证证据",
                expected=expected,
                actual=current["fingerprint"],
            )
    elif state["stage"] in {"check", "spec", "push"}:
        raise UntrackedFlowError(
            "workspace-drift",
            "进入新一轮修改前缺少有效 workspace 证据",
            expected=expected,
            actual=current["fingerprint"],
        )

    normalized_paths = _normalize_paths(repo_root, paths)
    if state.get("baseline") is None:
        state["baseline"] = current
    state["scope"] = sorted(set(state.get("scope", [])) | set(normalized_paths))
    state["stage"] = "implement"
    state["preparedFingerprint"] = current["fingerprint"]
    state["workspaceFingerprint"] = None
    state["evidence"] = {}
    state["updatedAt"] = _utc_now()
    _persist(scope, state)
    return {
        "status": "prepared",
        **_state_summary(state),
        "baselineFingerprint": state["baseline"].get("fingerprint"),
        "preparedFingerprint": current["fingerprint"],
    }


def record_validation(
    repo_root: Path,
    result: str,
    summary: str,
    platform_input: dict[str, Any] | None = None,
    platform: str | None = None,
) -> dict[str, Any]:
    """记录 focused validation 结果和当前 workspace fingerprint。

    Args:
        repo_root: Trellis 项目根目录。
        result: ``pass``、``fail`` 或 ``partial``。
        summary: 定向验证摘要。
        platform_input: 可选平台 hook 输入。
        platform: 可选平台名称。

    Returns:
        记录后的事项状态摘要。
    """
    if result not in VALIDATION_RESULTS:
        raise UntrackedFlowError("invalid-validation-result", "定向验证结果不合法")
    scope = _runtime_scope(repo_root, platform_input, platform)
    state = _normalized_state(scope["context"].get(STATE_KEY))
    if state is None or state["stage"] != "implement":
        raise UntrackedFlowError("stage-mismatch", "只有 implement 阶段可以记录定向验证")
    current = _capture(repo_root)
    now = _utc_now()
    state["workspaceFingerprint"] = current["fingerprint"]
    state["evidence"] = {
        "focusedValidation": {
            "result": result,
            "summary": summary.strip(),
            "fingerprint": current["fingerprint"],
            "recordedAt": now,
        }
    }
    state["updatedAt"] = now
    _persist(scope, state)
    return {"status": "recorded", **_state_summary(state)}


def advance_stage(
    repo_root: Path,
    stage: str,
    platform_input: dict[str, Any] | None = None,
    platform: str | None = None,
) -> dict[str, Any]:
    """在前置证据满足时推进 untracked 完成链阶段。

    Args:
        repo_root: Trellis 项目根目录。
        stage: ``check``、``spec`` 或 ``push``。
        platform_input: 可选平台 hook 输入。
        platform: 可选平台名称。

    Returns:
        推进后的事项状态摘要。
    """
    if stage not in {"check", "spec", "push"}:
        raise UntrackedFlowError("invalid-stage", "目标阶段不合法", stage=stage)
    scope = _runtime_scope(repo_root, platform_input, platform)
    state = _normalized_state(scope["context"].get(STATE_KEY))
    if state is None:
        raise UntrackedFlowError("no-active-work", "当前 session 没有活跃无任务事项")
    _ensure_workspace_matches(repo_root, state)
    evidence = state["evidence"]
    if stage == "check":
        focused = evidence.get("focusedValidation")
        if state["stage"] != "implement" or not isinstance(focused, dict) or focused.get("result") != "pass":
            raise UntrackedFlowError("focused-validation-required", "进入 Check-All 前需要通过定向验证")
    elif stage == "spec":
        checked = evidence.get("checkAll")
        if state["stage"] != "check" or not isinstance(checked, dict) or checked.get("result") != "pass":
            raise UntrackedFlowError("check-all-required", "进入 Update-Spec 前需要 Check-All 严格通过")
    else:
        spec = evidence.get("updateSpec")
        if (
            state["stage"] != "spec"
            or not isinstance(spec, dict)
            or spec.get("result") not in {"no-op", "written"}
        ):
            raise UntrackedFlowError("update-spec-required", "进入 Push 前需要完成 Update-Spec")
    state["stage"] = stage
    state["updatedAt"] = _utc_now()
    _persist(scope, state)
    return {"status": "advanced", **_state_summary(state)}


def record_check(
    repo_root: Path,
    result: str,
    summary: str,
    platform_input: dict[str, Any] | None = None,
    platform: str | None = None,
) -> dict[str, Any]:
    """记录 Check-All 结果并绑定当前 workspace fingerprint。

    Args:
        repo_root: Trellis 项目根目录。
        result: Check-All 结果类别。
        summary: Check-All 结果摘要。
        platform_input: 可选平台 hook 输入。
        platform: 可选平台名称。

    Returns:
        记录后的事项状态摘要。
    """
    if result not in CHECK_RESULTS:
        raise UntrackedFlowError("invalid-check-result", "Check-All 结果不合法")
    scope = _runtime_scope(repo_root, platform_input, platform)
    state = _normalized_state(scope["context"].get(STATE_KEY))
    if state is None or state["stage"] != "check":
        raise UntrackedFlowError("stage-mismatch", "只有 check 阶段可以记录 Check-All")
    current = _ensure_workspace_matches(repo_root, state)
    now = _utc_now()
    state["evidence"]["checkAll"] = {
        "result": result,
        "summary": summary.strip(),
        "fingerprint": current["fingerprint"],
        "recordedAt": now,
    }
    state["updatedAt"] = now
    _persist(scope, state)
    return {"status": "recorded", **_state_summary(state)}


def record_spec(
    repo_root: Path,
    result: str,
    summary: str,
    platform_input: dict[str, Any] | None = None,
    platform: str | None = None,
) -> dict[str, Any]:
    """记录 Update-Spec 结果并绑定当前 workspace fingerprint。

    Args:
        repo_root: Trellis 项目根目录。
        result: Update-Spec 结果类别。
        summary: Update-Spec 结果摘要。
        platform_input: 可选平台 hook 输入。
        platform: 可选平台名称。

    Returns:
        记录后的事项状态摘要。
    """
    if result not in SPEC_RESULTS:
        raise UntrackedFlowError("invalid-spec-result", "Update-Spec 结果不合法")
    scope = _runtime_scope(repo_root, platform_input, platform)
    state = _normalized_state(scope["context"].get(STATE_KEY))
    if state is None or state["stage"] != "spec":
        raise UntrackedFlowError("stage-mismatch", "只有 spec 阶段可以记录 Update-Spec")
    current = _ensure_workspace_matches(repo_root, state)
    now = _utc_now()
    state["evidence"]["updateSpec"] = {
        "result": result,
        "summary": summary.strip(),
        "fingerprint": current["fingerprint"],
        "recordedAt": now,
    }
    state["updatedAt"] = now
    _persist(scope, state)
    return {"status": "recorded", **_state_summary(state)}


def read_untracked_state(
    repo_root: Path,
    platform_input: dict[str, Any] | None = None,
    platform: str | None = None,
    *,
    allow_active_task: bool = False,
    validate_workspace: bool = True,
) -> dict[str, Any]:
    """读取并校验当前 session 的 untracked 状态。

    Args:
        repo_root: Trellis 项目根目录。
        platform_input: 可选平台 hook 输入。
        platform: 可选平台名称。
        allow_active_task: 是否允许 session 同时已有 task，供 adoption 清理使用。
        validate_workspace: 是否校验已稳定阶段的 workspace fingerprint。

    Returns:
        ``hit`` 或 ``miss`` 的结构化状态。
    """
    scope = _runtime_scope(
        repo_root,
        platform_input,
        platform,
        allow_active_task=allow_active_task,
    )
    raw = scope["context"].get(STATE_KEY)
    if raw is None:
        return {"status": "miss", "reason": "no-active-work", "contextKey": scope["context_key"]}
    state = _normalized_state(raw)
    if state is None:
        raise UntrackedFlowError("invalid-state", "当前 session 的无任务状态已损坏")
    if validate_workspace and isinstance(state.get("baseline"), dict):
        current = _capture(repo_root)
        if _baseline_restored(state, current):
            _persist(scope, None)
            return {
                "status": "miss",
                "reason": "baseline-restored",
                "contextKey": scope["context_key"],
            }
    if validate_workspace and state["stage"] in {"check", "spec", "push"}:
        _ensure_workspace_matches(repo_root, state)
    if (
        validate_workspace
        and state["stage"] == "implement"
        and isinstance(state.get("evidence", {}).get("focusedValidation"), dict)
    ):
        _ensure_workspace_matches(repo_root, state)
    return {
        "status": "hit",
        **_state_summary(state),
        "contextKey": scope["context_key"],
        "path": scope["path"],
        "state": state,
    }


def clear_untracked_state(
    repo_root: Path,
    reason: str,
    platform_input: dict[str, Any] | None = None,
    platform: str | None = None,
    *,
    work_id: str | None = None,
    allow_active_task: bool = True,
) -> dict[str, Any]:
    """按明确原因清理当前 session 的 untracked 状态。

    Args:
        repo_root: Trellis 项目根目录。
        reason: 完成、放弃、纳管、恢复 baseline 或失效原因。
        platform_input: 可选平台 hook 输入。
        platform: 可选平台名称。
        work_id: 可选的精确事项 ID 防误清。
        allow_active_task: 是否允许 session 已绑定 task。

    Returns:
        清理结果和被清理事项 ID。
    """
    if reason not in VALID_CLEAR_REASONS:
        raise UntrackedFlowError("invalid-clear-reason", "无任务状态清理原因不合法")
    scope = _runtime_scope(
        repo_root,
        platform_input,
        platform,
        allow_active_task=allow_active_task,
    )
    state = _normalized_state(scope["context"].get(STATE_KEY))
    if state is None:
        return {"status": "cleared", "existed": False, "reason": reason}
    if work_id and state["id"] != work_id:
        raise UntrackedFlowError(
            "work-id-mismatch",
            "待清理事项与当前 session 状态不匹配",
            expected=work_id,
            actual=state["id"],
        )
    _persist(scope, None)
    return {
        "status": "cleared",
        "existed": True,
        "reason": reason,
        "workId": state["id"],
    }


def session_start_hint(
    repo_root: Path,
    platform_input: dict[str, Any] | None = None,
    platform: str | None = None,
) -> str | None:
    """返回适合 SessionStart 的紧凑 untracked 恢复提示。

    Args:
        repo_root: Trellis 项目根目录。
        platform_input: 可选平台 hook 输入。
        platform: 可选平台名称。

    Returns:
        命中时返回一行提示，否则返回 None。
    """
    try:
        result = read_untracked_state(
            repo_root,
            platform_input,
            platform,
            validate_workspace=False,
        )
    except UntrackedFlowError:
        return None
    if result.get("status") != "hit":
        return None
    return (
        f"Untracked work: {result['workId']}; stage={result['stage']}; "
        f"summary={result['summary']}."
    )


def _emit(payload: dict[str, Any], *, include_state: bool = False) -> None:
    """向 stdout 输出稳定 JSON。"""
    serializable = {
        key: str(value) if isinstance(value, Path) else value
        for key, value in payload.items()
        if include_state or key != "state"
    }
    print(json.dumps(serializable, ensure_ascii=False, sort_keys=True))


def build_parser() -> argparse.ArgumentParser:
    """构造 untracked flow CLI parser。

    Returns:
        已配置全部状态命令的 parser。
    """
    parser = argparse.ArgumentParser(description="Trellis untracked workflow state helper")
    subparsers = parser.add_subparsers(dest="command", required=True)

    begin_parser = subparsers.add_parser("begin", help="create one untracked work item")
    begin_parser.add_argument("--summary", required=True)
    begin_parser.add_argument("--source", choices=sorted(VALID_SOURCES), required=True)

    prepare_parser = subparsers.add_parser("prepare-edit", help="capture baseline before editing")
    prepare_parser.add_argument("--paths", nargs="+", required=True)

    validation_parser = subparsers.add_parser("record-validation", help="record focused validation")
    validation_parser.add_argument("--result", choices=sorted(VALIDATION_RESULTS), required=True)
    validation_parser.add_argument("--summary", default="")

    advance_parser = subparsers.add_parser("advance", help="advance workflow stage")
    advance_parser.add_argument("--stage", choices=["check", "spec", "push"], required=True)

    check_parser = subparsers.add_parser("record-check", help="record Check-All result")
    check_parser.add_argument("--result", choices=sorted(CHECK_RESULTS), required=True)
    check_parser.add_argument("--summary", default="")

    spec_parser = subparsers.add_parser("record-spec", help="record Update-Spec result")
    spec_parser.add_argument("--result", choices=sorted(SPEC_RESULTS), required=True)
    spec_parser.add_argument("--summary", default="")

    status_parser = subparsers.add_parser("status", help="read current untracked work state")
    status_parser.add_argument("--verbose", action="store_true")
    subparsers.add_parser("session-start-hint", help="render compact recovery hint")

    clear_parser = subparsers.add_parser("clear", help="clear current untracked work state")
    clear_parser.add_argument("--reason", choices=sorted(VALID_CLEAR_REASONS), required=True)
    clear_parser.add_argument("--work-id")
    return parser


def main(argv: list[str] | None = None) -> int:
    """执行 untracked flow CLI。

    Args:
        argv: 可选命令参数，默认读取 ``sys.argv``。

    Returns:
        成功或 miss 为 0，状态错误为 1。
    """
    args = build_parser().parse_args(argv)
    repo_root = _find_repo_root()
    if repo_root is None:
        _emit({"status": "error", "reason": "not-trellis-project"})
        return 1
    try:
        if args.command == "begin":
            payload = begin_untracked_work(repo_root, args.summary, args.source)
        elif args.command == "prepare-edit":
            payload = prepare_edit(repo_root, args.paths)
        elif args.command == "record-validation":
            payload = record_validation(repo_root, args.result, args.summary)
        elif args.command == "advance":
            payload = advance_stage(repo_root, args.stage)
        elif args.command == "record-check":
            payload = record_check(repo_root, args.result, args.summary)
        elif args.command == "record-spec":
            payload = record_spec(repo_root, args.result, args.summary)
        elif args.command == "status":
            payload = read_untracked_state(repo_root)
        elif args.command == "session-start-hint":
            hint = session_start_hint(repo_root)
            payload = {"status": "hit", "hint": hint} if hint else {"status": "miss"}
        else:
            payload = clear_untracked_state(repo_root, args.reason, work_id=args.work_id)
        _emit(payload, include_state=args.command == "status" and args.verbose)
        return 0
    except (UntrackedFlowError, OSError) as error:
        if isinstance(error, UntrackedFlowError):
            payload = {
                "status": "error",
                "reason": error.reason,
                "message": str(error),
                **error.details,
            }
        else:
            payload = {"status": "error", "reason": "runtime-write-failed", "message": str(error)}
        _emit(payload)
        return 1


if __name__ == "__main__":
    sys.exit(main())
