#!/usr/bin/env python3
"""只读核验 Trellis 物理 GC 提交是否为纯归档移动。"""

from __future__ import annotations

import argparse
from datetime import datetime, timezone
import json
from pathlib import Path
import re
import subprocess
import sys
from typing import Any


GC_MESSAGE = b"chore(task): gc closed tasks\n"
TASK_ROOT = b".trellis/tasks/"
ARCHIVE_ROOT = TASK_ROOT + b"archive/"
COMMIT_RE = re.compile(r"[0-9a-fA-F]{40}(?:[0-9a-fA-F]{24})?\Z")


class GcCommitAuditError(Exception):
    """表示提交缺少默认放行所需的确定性证据。"""


def _git(repo_root: Path, *args: str) -> bytes:
    """以字节形式读取 Git，保留路径中的制表符及非 ASCII 字节。"""
    result = subprocess.run(
        ["git", "-C", str(repo_root), *args],
        capture_output=True,
        check=False,
    )
    if result.returncode != 0:
        detail = result.stderr.decode("utf-8", errors="replace").strip()
        raise GcCommitAuditError(f"Git 查询失败：{detail or '未知错误'}")
    return result.stdout


def _tree(repo_root: Path, commit: str) -> dict[bytes, tuple[bytes, bytes]]:
    """读取任务目录的路径、文件模式和 blob ID。"""
    raw = _git(repo_root, "ls-tree", "-r", "-z", commit, "--", ".trellis/tasks")
    entries: dict[bytes, tuple[bytes, bytes]] = {}
    for record in raw.split(b"\0"):
        if not record:
            continue
        try:
            header, path = record.split(b"\t", 1)
            mode, kind, oid = header.split(b" ")
        except ValueError as error:
            raise GcCommitAuditError("Git 树条目格式损坏") from error
        if kind != b"blob" or path in entries:
            raise GcCommitAuditError("任务树包含非文件或重复路径")
        entries[path] = (mode, oid)
    return entries


def _changes(repo_root: Path, parent: str, commit: str) -> tuple[set[bytes], set[bytes]]:
    """读取无重命名推断的新增和删除路径。"""
    raw = _git(
        repo_root,
        "diff-tree",
        "--no-commit-id",
        "--no-renames",
        "--ignore-submodules=none",
        "--name-status",
        "-r",
        "-z",
        parent,
        commit,
    )
    records = raw.split(b"\0")
    if not records or records[-1] != b"" or len(records[:-1]) % 2:
        raise GcCommitAuditError("Git 差异记录格式损坏")
    deleted: set[bytes] = set()
    added: set[bytes] = set()
    for index in range(0, len(records) - 1, 2):
        status, path = records[index:index + 2]
        if not path or path in deleted or path in added:
            raise GcCommitAuditError("Git 差异包含空路径或重复路径")
        if status == b"D":
            deleted.add(path)
        elif status == b"A":
            added.add(path)
        else:
            raise GcCommitAuditError("提交包含非归档移动的文件修改")
    if not deleted:
        raise GcCommitAuditError("提交没有顶层任务文件删除")
    return deleted, added


def _source_name(path: bytes) -> bytes:
    """从被删除文件中提取顶层任务名。"""
    if not path.startswith(TASK_ROOT):
        raise GcCommitAuditError("提交修改了任务目录以外的路径")
    remainder = path[len(TASK_ROOT):]
    name, separator, relative = remainder.partition(b"/")
    if not separator or not name or name == b"archive" or not relative:
        raise GcCommitAuditError("被删除文件不属于顶层任务")
    return name


def _bucket(repo_root: Path, task_json: tuple[bytes, bytes]) -> bytes:
    """校验关闭态，并返回生成器使用的 UTC 归档月份。"""
    mode, oid = task_json
    if mode not in {b"100644", b"100755"}:
        raise GcCommitAuditError("task.json 不是普通文件")
    try:
        data = json.loads(_git(repo_root, "cat-file", "blob", oid.decode("ascii")).decode("utf-8"))
    except (UnicodeError, json.JSONDecodeError) as error:
        raise GcCommitAuditError("task.json 不是有效的 UTF-8 JSON") from error
    if not isinstance(data, dict) or data.get("status") != "completed":
        raise GcCommitAuditError("归档任务不是 completed")
    closeout = data.get("closeout")
    if not isinstance(closeout, dict) or closeout.get("status") != "closed" or closeout.get("blockers") != []:
        raise GcCommitAuditError("归档任务没有合法的 closed 状态")
    closed_at = closeout.get("closedAt")
    if not isinstance(closed_at, str):
        raise GcCommitAuditError("归档任务缺少关闭时间")
    try:
        parsed = datetime.fromisoformat(closed_at.strip().replace("Z", "+00:00"))
    except ValueError as error:
        raise GcCommitAuditError("归档任务的关闭时间无效") from error
    if parsed.tzinfo is None:
        raise GcCommitAuditError("归档任务的关闭时间缺少时区")
    return parsed.astimezone(timezone.utc).strftime("%Y-%m").encode("ascii")


def verify_gc_commit(repo_root: Path, commit: str) -> dict[str, Any]:
    """验证单个提交仅将已关闭任务原字节移入归档。

    Args:
        repo_root: 待检查的 Git 仓库根目录。
        commit: 完整的 40 或 64 位提交哈希。

    Returns:
        已验证提交、任务名与移动文件数的结构化摘要。

    Raises:
        GcCommitAuditError: 提交不满足纯 GC 归档契约或 Git 证据不可读。
    """
    if not COMMIT_RE.fullmatch(commit):
        raise GcCommitAuditError("必须提供完整提交哈希")
    resolved = _git(repo_root, "rev-parse", "--verify", f"{commit}^{{commit}}").strip().decode("ascii")
    header, separator, message = _git(repo_root, "cat-file", "-p", resolved).partition(b"\n\n")
    parents = [line[7:].decode("ascii") for line in header.splitlines() if line.startswith(b"parent ")]
    if not separator or len(parents) != 1 or message != GC_MESSAGE:
        raise GcCommitAuditError("提交父节点或消息不符合自动 GC 契约")
    parent = parents[0]
    deleted, added = _changes(repo_root, parent, resolved)
    before = _tree(repo_root, parent)
    after = _tree(repo_root, resolved)
    names = {_source_name(path) for path in deleted}
    expected_added: set[bytes] = set()
    reported_names: list[str] = []
    for name in sorted(names):
        source_prefix = TASK_ROOT + name + b"/"
        source_files = {path for path in before if path.startswith(source_prefix)}
        if not source_files or deleted.intersection(source_files) != source_files:
            raise GcCommitAuditError("顶层任务未完整移走")
        task_json = before.get(source_prefix + b"task.json")
        if task_json is None:
            raise GcCommitAuditError("顶层任务缺少 task.json")
        destination_prefix = ARCHIVE_ROOT + _bucket(repo_root, task_json) + b"/" + name + b"/"
        if any(path.startswith(source_prefix) for path in after):
            raise GcCommitAuditError("提交后仍残留顶层任务文件")
        for source in source_files:
            destination = destination_prefix + source[len(source_prefix):]
            if after.get(destination) != before[source]:
                raise GcCommitAuditError("归档文件与原文件内容或模式不同")
            if destination not in before:
                expected_added.add(destination)
        try:
            reported_names.append(name.decode("utf-8"))
        except UnicodeDecodeError as error:
            raise GcCommitAuditError("任务名不是 UTF-8") from error
    if added != expected_added:
        raise GcCommitAuditError("提交包含未归属的新增路径")
    return {"status": "verified", "commit": resolved, "tasks": reported_names, "files": len(deleted)}


def main(argv: list[str] | None = None) -> int:
    """解析只读校验参数并输出机器可读的结果。

    Args:
        argv: 测试可传入的命令行参数；缺省使用进程参数。

    Returns:
        验证通过返回 0，证据不足或 Git 查询失败返回 1。
    """
    parser = argparse.ArgumentParser(description="核验 Trellis 纯归档 GC 提交")
    parser.add_argument("--repo", type=Path, required=True)
    parser.add_argument("--commit", required=True)
    args = parser.parse_args(argv)
    try:
        result = verify_gc_commit(args.repo, args.commit)
    except GcCommitAuditError as error:
        result = {"status": "rejected", "commit": args.commit, "reason": str(error)}
    print(json.dumps(result, ensure_ascii=False, sort_keys=True))
    return 0 if result["status"] == "verified" else 1


if __name__ == "__main__":
    sys.exit(main())
