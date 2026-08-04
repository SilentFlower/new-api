#!/usr/bin/env python3
"""准备 linked worktree 中的 Trellis 入口投影。"""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
import tempfile
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


SCHEMA_VERSION = 1
MANIFEST_NAME = ".trellis-worktree.json"
ENTRY_PATHS = (".trellis", ".agents", ".codex", ".claude")


class WorktreeSetupError(Exception):
    """携带稳定 reason code 的 worktree 准备错误。"""

    def __init__(self, reason: str, message: str, **details: Any) -> None:
        """初始化结构化准备错误。

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


def _path_text(path: Path) -> str:
    """返回稳定的绝对路径字符串，但不跟随最终 symlink。"""
    return os.path.abspath(os.fspath(path.expanduser()))


def _git_output(start: Path, *args: str) -> str | None:
    """执行只读 git 命令并返回 stdout，失败时返回 None。"""
    try:
        result = subprocess.run(
            ["git", "-C", str(start), *args],
            check=False,
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            text=True,
            timeout=2,
        )
    except (OSError, subprocess.SubprocessError):
        return None
    if result.returncode != 0:
        return None
    output = result.stdout.strip()
    return output or None


def _resolve_start(path_text: str | None) -> Path:
    """解析用户传入的 target，允许传入 worktree 子目录。"""
    path = Path(path_text or os.getcwd()).expanduser()
    if not path.exists():
        raise WorktreeSetupError("target-missing", "目标路径不存在", target=str(path))
    resolved = path.resolve()
    return resolved.parent if resolved.is_file() else resolved


def _git_toplevel(start: Path) -> Path | None:
    """返回当前 Git worktree 根目录。"""
    output = _git_output(start, "rev-parse", "--path-format=absolute", "--show-toplevel")
    if output is None:
        output = _git_output(start, "rev-parse", "--show-toplevel")
    return Path(output).expanduser().resolve() if output else None


def _git_common_dir(start: Path) -> Path | None:
    """返回当前 Git 仓库 common dir，兼容旧 git 的相对路径输出。"""
    output = _git_output(start, "rev-parse", "--path-format=absolute", "--git-common-dir")
    if output is None:
        output = _git_output(start, "rev-parse", "--git-common-dir")
    if output is None:
        return None
    common_dir = Path(output)
    if common_dir.is_absolute():
        return common_dir.resolve()
    top_level = _git_toplevel(start)
    if top_level is None:
        return None
    return (top_level / common_dir).resolve()


def _manifest_path(target_root: Path) -> Path:
    """返回目标 worktree 的投影 manifest 路径。"""
    return target_root / MANIFEST_NAME


def _read_manifest(target_root: Path) -> tuple[str, dict[str, Any] | None]:
    """读取 manifest，并保留缺失和损坏差异。"""
    path = _manifest_path(target_root)
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except FileNotFoundError:
        return "missing", None
    except (json.JSONDecodeError, OSError):
        return "invalid", None
    if not isinstance(data, dict):
        return "invalid", None
    return "ok", data


def _manifest_source(target_root: Path, manifest: dict[str, Any] | None) -> Path | None:
    """从有效 manifest 中读取 sourceRoot。"""
    if not isinstance(manifest, dict) or manifest.get("schemaVersion") != SCHEMA_VERSION:
        return None
    target_text = manifest.get("targetRoot")
    source_text = manifest.get("sourceRoot")
    if not isinstance(target_text, str) or not isinstance(source_text, str):
        return None
    if Path(target_text).expanduser().resolve() != target_root:
        return None
    source_root = Path(source_text).expanduser().resolve()
    return source_root if (source_root / ".trellis").is_dir() else None


def _manifest_paths(manifest: dict[str, Any] | None) -> set[str]:
    """返回 manifest 记录的受管相对路径集合。"""
    links = manifest.get("links") if isinstance(manifest, dict) else None
    if not isinstance(links, list):
        return set()
    result: set[str] = set()
    for item in links:
        if not isinstance(item, dict):
            continue
        path = item.get("path")
        if isinstance(path, str) and path in ENTRY_PATHS:
            result.add(path)
    return result


def _source_from_trellis_symlink(target_root: Path) -> Path | None:
    """从目标 .trellis symlink 反推出主 worktree 根。"""
    trellis_path = target_root / ".trellis"
    if not trellis_path.is_symlink():
        return None
    source_path = trellis_path.resolve(strict=False)
    if source_path.name != ".trellis":
        return None
    source_root = source_path.parent
    return source_root if source_path.is_dir() else None


def _source_from_git(target_root: Path) -> Path | None:
    """从同一个 Git worktree 集合中寻找带 .trellis 的主 worktree。"""
    common_dir = _git_common_dir(target_root)
    if common_dir is not None:
        candidate = common_dir.parent
        if candidate != target_root and (candidate / ".trellis").is_dir():
            return candidate

    output = _git_output(target_root, "worktree", "list", "--porcelain")
    if output is None:
        return None
    fallback: Path | None = None
    for line in output.splitlines():
        if not line.startswith("worktree "):
            continue
        candidate = Path(line.split(" ", 1)[1]).expanduser().resolve()
        if not (candidate / ".trellis").is_dir():
            continue
        if candidate != target_root:
            return candidate
        fallback = candidate
    return fallback


def _resolve_source_root(
    target_root: Path,
    manifest: dict[str, Any] | None,
) -> tuple[Path, str]:
    """解析承载 .trellis 的源 worktree。"""
    source = _manifest_source(target_root, manifest)
    if source is not None:
        return source, "manifest"

    source = _source_from_trellis_symlink(target_root)
    if source is not None:
        return source, "trellis-symlink"

    source = _source_from_git(target_root)
    if source is not None:
        return source, "git-worktree"

    if (target_root / ".trellis").is_dir():
        return target_root, "target-root"

    raise WorktreeSetupError(
        "source-not-found",
        "无法在当前 Git worktree 集合中找到承载 .trellis 的主 worktree",
        targetRoot=_path_text(target_root),
    )


def _same_symlink_target(link: Path, source: Path) -> bool:
    """判断 symlink 是否已经指向期望源路径。"""
    try:
        raw_target = Path(os.readlink(link))
    except OSError:
        return False
    actual = raw_target if raw_target.is_absolute() else (link.parent / raw_target)
    return actual.resolve(strict=False) == source.resolve(strict=False)


def _classify_link(
    source_root: Path,
    target_root: Path,
    rel_path: str,
    managed_paths: set[str],
) -> dict[str, Any]:
    """判断单个入口路径的投影状态。"""
    source = source_root / rel_path
    target = target_root / rel_path
    item = {
        "path": rel_path,
        "source": _path_text(source),
        "target": _path_text(target),
    }

    if not source.exists() and not source.is_symlink():
        return {**item, "state": "source-missing"}
    if source_root == target_root:
        return {**item, "state": "ready"}
    if target.is_symlink():
        if _same_symlink_target(target, source):
            return {**item, "state": "ready"}
        if rel_path in managed_paths:
            return {**item, "state": "repair"}
        return {**item, "state": "conflict", "reason": "symlink-target-mismatch"}
    if target.exists():
        return {**item, "state": "conflict", "reason": "target-exists"}
    return {**item, "state": "create"}


def _analyze(target_arg: str | None) -> dict[str, Any]:
    """生成 worktree 准备计划，不写盘。"""
    start = _resolve_start(target_arg)
    target_root = _git_toplevel(start)
    if target_root is None:
        raise WorktreeSetupError("not-git-worktree", "目标路径不在 Git worktree 中", target=str(start))

    manifest_status, manifest = _read_manifest(target_root)
    source_root, source = _resolve_source_root(target_root, manifest)
    managed_paths = _manifest_paths(manifest)
    links = [
        _classify_link(source_root, target_root, rel_path, managed_paths)
        for rel_path in ENTRY_PATHS
    ]
    actions = [link for link in links if link["state"] in {"create", "repair"}]
    conflicts = [link for link in links if link["state"] == "conflict"]
    missing_sources = [link for link in links if link["state"] == "source-missing"]

    status = "ready"
    if conflicts:
        status = "blocked"
    elif actions:
        status = "needs-prepare"

    return {
        "status": status,
        "targetRoot": _path_text(target_root),
        "sourceRoot": _path_text(source_root),
        "source": source,
        "manifest": {
            "path": _path_text(_manifest_path(target_root)),
            "status": manifest_status,
        },
        "links": links,
        "actions": actions,
        "conflicts": conflicts,
        "missingSources": missing_sources,
    }


def _desired_manifest(plan: dict[str, Any]) -> dict[str, Any]:
    """构造当前投影的 manifest 内容。"""
    links = [
        {
            "path": link["path"],
            "source": link["source"],
            "target": link["target"],
        }
        for link in plan["links"]
        if link["state"] != "source-missing"
    ]
    return {
        "schemaVersion": SCHEMA_VERSION,
        "sourceRoot": plan["sourceRoot"],
        "targetRoot": plan["targetRoot"],
        "links": links,
    }


def _manifest_matches(path: Path, desired: dict[str, Any]) -> bool:
    """判断现有 manifest 是否已经与期望投影一致，忽略 updatedAt。"""
    try:
        current = json.loads(path.read_text(encoding="utf-8"))
    except (FileNotFoundError, json.JSONDecodeError, OSError):
        return False
    if not isinstance(current, dict):
        return False
    comparable = {key: current.get(key) for key in ("schemaVersion", "sourceRoot", "targetRoot", "links")}
    return comparable == desired


def _write_manifest(path: Path, desired: dict[str, Any]) -> None:
    """原子写入 worktree 投影 manifest。"""
    data = {**desired, "updatedAt": _utc_now()}
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


def _apply_plan(plan: dict[str, Any]) -> dict[str, Any]:
    """执行准备计划并返回写盘结果。"""
    if plan["status"] == "blocked":
        raise WorktreeSetupError(
            "projection-conflict",
            "目标 worktree 中存在非 trellis-worktree 管理的入口路径，已拒绝覆盖",
            conflicts=plan["conflicts"],
        )

    changed_links: list[str] = []
    for action in plan["actions"]:
        source = Path(action["source"])
        target = Path(action["target"])
        if action["state"] == "repair":
            target.unlink()
        os.symlink(source, target, target_is_directory=source.is_dir())
        changed_links.append(action["path"])

    manifest_written = False
    if plan["sourceRoot"] != plan["targetRoot"]:
        manifest_path = Path(plan["manifest"]["path"])
        desired = _desired_manifest(plan)
        if changed_links or not _manifest_matches(manifest_path, desired):
            _write_manifest(manifest_path, desired)
            manifest_written = True

    status = "prepared" if changed_links or manifest_written else "ready"
    return {
        **plan,
        "status": status,
        "changed": bool(changed_links or manifest_written),
        "changedLinks": changed_links,
        "manifestWritten": manifest_written,
    }


def _emit(payload: dict[str, Any], *, compact: bool) -> None:
    """向 stdout 输出 JSON。"""
    print(json.dumps(payload, ensure_ascii=False, sort_keys=True if compact else False, indent=None if compact else 2))


def build_parser() -> argparse.ArgumentParser:
    """构造 worktree setup CLI parser。"""
    parser = argparse.ArgumentParser(description="Trellis linked worktree setup helper")
    subparsers = parser.add_subparsers(dest="command", required=True)

    for command in ("status", "prepare"):
        command_parser = subparsers.add_parser(command)
        command_parser.add_argument("--target", help="linked worktree path; defaults to cwd")
        command_parser.add_argument("--json", action="store_true", help="emit compact stable JSON")
    return parser


def main(argv: list[str] | None = None) -> int:
    """执行 worktree setup CLI。"""
    args = build_parser().parse_args(argv)
    compact = bool(getattr(args, "json", False))
    try:
        plan = _analyze(args.target)
        payload = _apply_plan(plan) if args.command == "prepare" else plan
        _emit(payload, compact=compact)
        return 0
    except (WorktreeSetupError, OSError) as error:
        if isinstance(error, WorktreeSetupError):
            payload = {
                "status": "error",
                "reason": error.reason,
                "message": str(error),
                **error.details,
            }
        else:
            payload = {"status": "error", "reason": "worktree-setup-failed", "message": str(error)}
        _emit(payload, compact=compact)
        return 1


if __name__ == "__main__":
    sys.exit(main())
