#!/usr/bin/env python3
"""只读计算后台确认策略与冻结材料；不计时、不授权、不执行 Git 写操作。"""

from __future__ import annotations

import argparse
from contextvars import ContextVar
import hashlib
import json
import os
import queue
import re
import stat
import subprocess
import sys
import threading
import time
from pathlib import Path

from common.config import parse_simple_yaml
from common.active_task import resolve_active_task
from common.tasks import normalize_closeout


VERSION = 1
INPUT_BYTES = 1024 * 1024
DOCUMENT_BYTES = 512 * 1024
HASH_CHUNK = 64 * 1024
GIT_TIMEOUT = 10
SNAPSHOT_TIMEOUT = 30
_SNAPSHOT_DEADLINE = ContextVar("snapshot_deadline", default=None)
MAX_SECONDS = 86400
DEFAULTS = {
    "enabled": True,
    "brief": {"enabled": True, "base_seconds": 100, "threshold_chars": 1000,
              "increment_chars": 500, "increment_seconds": 30},
    "check": {"enabled": True, "base_seconds": 100, "increment_seconds": 30},
    "push": {"enabled": True, "seconds": 200},
}
ACTIONS = {"brief": "implement", "check": "repair_all_and_recheck", "push": "execute_git_plan"}


def _digest(value):
    return hashlib.sha256(json.dumps(value, ensure_ascii=False, sort_keys=True,
                                     separators=(",", ":")).encode("utf-8")).hexdigest()


def _inside(root, relative):
    if not isinstance(relative, str) or "\\" in relative or not relative:
        raise ValueError("材料路径必须是项目内 POSIX 相对路径")
    parts = relative.split("/")
    if any(part in ("", ".", "..") for part in parts) or Path(relative).is_absolute():
        raise ValueError("材料路径越界或包含空片段")
    current = root
    for part in parts:
        current = current / part
        if current.is_symlink():
            raise ValueError("确认材料路径不能经过软链接")
    return current


def _bytes(file, optional=False, limit=DOCUMENT_BYTES):
    try:
        info = file.lstat()
    except FileNotFoundError:
        if optional:
            return None
        raise ValueError("确认材料不存在：" + file.name)
    if not stat.S_ISREG(info.st_mode):
        raise ValueError("确认材料必须是普通文件：" + file.name)
    if info.st_size > limit:
        raise ValueError(f"结构化确认材料超过 {limit // 1024} KiB：{file.name}")
    with file.open("rb") as stream:
        opened = os.fstat(stream.fileno())
        if not _same_file_state(info, opened):
            raise ValueError("确认材料在打开时已变化：" + file.name)
        value = stream.read(limit + 1)
        if (not _same_file_state(opened, os.fstat(stream.fileno()), same_handle=True)
                or not _same_file_state(info, file.lstat())):
            raise ValueError("确认材料在读取期间已变化：" + file.name)
    if len(value) > limit:
        raise ValueError(f"结构化确认材料超过 {limit // 1024} KiB：{file.name}")
    return value


def _fingerprint(file, optional=False, info=None):
    if info is None:
        try:
            info = file.lstat()
        except FileNotFoundError:
            if optional:
                return None
            raise
    if not stat.S_ISREG(info.st_mode):
        raise ValueError("指纹材料必须是普通文件：" + str(file))
    digest = hashlib.sha256()
    with file.open("rb") as stream:
        opened = os.fstat(stream.fileno())
        if not _same_file_state(info, opened):
            raise ValueError("指纹材料在打开时已变化：" + str(file))
        for chunk in iter(lambda: stream.read(HASH_CHUNK), b""):
            _snapshot_remaining(str(file))
            digest.update(chunk)
        if (not _same_file_state(opened, os.fstat(stream.fileno()), same_handle=True)
                or not _same_file_state(info, file.lstat())):
            raise ValueError("指纹材料在读取期间已变化：" + str(file))
    return digest.hexdigest()


def _same_file_state(first, second, *, same_handle=False):
    fields = ("st_dev", "st_ino", "st_mode", "st_size", "st_mtime_ns")
    # Windows Python 3.12 的路径和句柄查询可能为同一文件返回不同的 ctime。
    if sys.platform != "win32" or same_handle:
        fields += ("st_ctime_ns",)
    return all(getattr(first, field) == getattr(second, field) for field in fields)


def _boolean(value, label):
    if value is True or value is False:
        return value
    if isinstance(value, str) and value.lower() in ("true", "false"):
        return value.lower() == "true"
    raise ValueError(label + " 必须为 true 或 false")


def _integer(value, label, maximum=MAX_SECONDS):
    if isinstance(value, bool) or not re.fullmatch(r"[0-9]+", str(value)):
        raise ValueError(label + " 必须为正整数")
    number = int(value)
    if number < 1 or number > maximum:
        raise ValueError(label + " 超出支持范围，保持人工确认")
    return number


def _policy(root):
    data = _bytes(_inside(root, ".trellis/config.yaml"), optional=True)
    text = data.decode("utf-8") if data is not None else ""
    # 简单 YAML parser 会覆盖重复 key；确认策略不能因此把用户关闭项悄悄改成开启。
    in_block = False
    seen = set()
    parents = []
    for line in text.splitlines():
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        if not line[0].isspace():
            in_block = bool(re.match(r"^confirmation_timeout\s*:", line))
            parents = []
        if not in_block:
            continue
        match = re.fullmatch(r"( *)([a-z_]+):(?:\s+(.*))?", line)
        if not match or "\t" in line:
            raise ValueError("confirmation_timeout 使用了不支持的 YAML 形式")
        indent, key, _ = match.groups()
        depth = len(indent)
        while parents and parents[-1][0] >= depth:
            parents.pop()
        identity = tuple(item[1] for item in parents) + (key,)
        if identity in seen:
            raise ValueError("confirmation_timeout 存在重复字段")
        seen.add(identity)
        parents.append((depth, key))
    raw = parse_simple_yaml(text).get("confirmation_timeout", {})
    if not isinstance(raw, dict) or set(raw) - set(DEFAULTS):
        raise ValueError("confirmation_timeout 结构或字段无效")
    result = {"enabled": _boolean(raw.get("enabled", True), "enabled")}
    for kind in ACTIONS:
        values = raw.get(kind, {})
        if not isinstance(values, dict) or set(values) - set(DEFAULTS[kind]):
            raise ValueError(kind + " 策略结构或字段无效")
        result[kind] = {}
        for key, default in DEFAULTS[kind].items():
            value = values.get(key, default)
            result[kind][key] = (_boolean(value, kind + "." + key) if key == "enabled"
                                 else _integer(value, kind + "." + key,
                                               10000000 if key.endswith("chars") else MAX_SECONDS))
    return result


def _git(root, *args, optional=False):
    command = ["git", "--no-optional-locks", "-C", str(root), *args]
    label = str(root) + "：git " + " ".join(args)
    try:
        process = subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                                 timeout=_snapshot_remaining(label, GIT_TIMEOUT))
    except subprocess.TimeoutExpired as error:
        raise ValueError("Git 只读快照命令超时：" + label) from error
    if process.returncode:
        if optional:
            return None
        raise ValueError("Git 只读快照命令失败：" + label)
    return process.stdout


def _snapshot_remaining(label, limit=None):
    deadline = _SNAPSHOT_DEADLINE.get()
    if deadline is None:
        return limit
    remaining = deadline - time.monotonic()
    if remaining <= 0:
        raise ValueError("Push 快照总耗时超出预算：" + label)
    return remaining if limit is None else min(limit, remaining)


def _git_hash(root, *args):
    command = ["git", "--no-optional-locks", "-C", str(root), *args]
    label = str(root) + "：git " + " ".join(args)
    deadline = time.monotonic() + _snapshot_remaining(label, GIT_TIMEOUT)
    process = subprocess.Popen(command, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
    results = queue.Queue(maxsize=1)

    def read():
        try:
            # 分块复现旧 JSON 十六进制摘要，避免缓存整份 Git 输出或改变既有指纹。
            digest = hashlib.sha256(b'"')
            with process.stdout as stream:
                for chunk in iter(lambda: stream.read(HASH_CHUNK), b""):
                    digest.update(chunk.hex().encode("ascii"))
            digest.update(b'"')
            results.put(digest.hexdigest())
        except OSError as error:
            results.put(error)

    # Windows 的匿名管道不能用 select；读线程独占 stdout，主线程负责超时回收 Git。
    try:
        threading.Thread(target=read, daemon=True).start()
        digest = results.get(timeout=max(0, deadline - time.monotonic()))
        if process.wait(timeout=max(0, deadline - time.monotonic())) or isinstance(digest, OSError):
            raise ValueError("Git 只读快照命令失败：" + label)
        return digest
    except (queue.Empty, subprocess.TimeoutExpired) as error:
        raise ValueError("Git 只读快照命令超时：" + label) from error
    finally:
        if process.poll() is None:
            process.kill()
            process.wait()


def _git_file(root, relative):
    file = root / relative
    # Git 允许提交软链本身；只读取其链接目标，不跟随软链读取项目外内容。
    parts = relative.split("/")
    if not relative or "\\" in relative or Path(relative).is_absolute() or any(p in ("", ".", "..") for p in parts):
        raise ValueError("Git 文件路径无效")
    _inside(root, "/".join(parts[:-1])) if len(parts) > 1 else None
    try:
        info = file.lstat()
    except FileNotFoundError:
        return None
    if stat.S_ISLNK(info.st_mode):
        return {"mode": "link", "target": os.readlink(str(file))}
    if stat.S_ISDIR(info.st_mode):
        actual = _git(file, "rev-parse", "--show-toplevel").decode("utf-8").strip()
        if Path(actual).resolve() != file.resolve():
            raise ValueError("计划目录不是精确 Git 子仓")
        return {"mode": "gitlink", "head": _git(file, "rev-parse", "HEAD").decode().strip(),
                "status": _git_hash(file, "status", "--porcelain=v1", "-z")}
    return {"mode": stat.S_IMODE(info.st_mode), "sha256": _fingerprint(file, info=info)}


def _repositories(project, material):
    evidence = material.get("evidence", {})
    if (not isinstance(evidence, dict) or evidence.get("check") not in ("passed", "accepted", "focused")
            or evidence.get("spec") not in ("no-op", "written")
            or type(evidence.get("unresolvedRisks")) is not int or evidence["unresolvedRisks"] != 0):
        raise ValueError("Push 完成链证据不完整或存在未处置风险，保持人工确认")
    repositories = material.get("repositories")
    if not isinstance(repositories, list) or not repositories or len(repositories) > 16:
        raise ValueError("Push 必须提供精确仓库计划")
    if material.get("mode") not in ("push", "commit-only"):
        raise ValueError("Push 必须明确 push 或 commit-only 模式")
    snapshots = []
    roots = set()
    for plan in repositories:
        root_text = plan.get("root")
        if not isinstance(root_text, str) or not root_text:
            raise ValueError("Push 缺少仓库根")
        root = (project / root_text).resolve()
        if str(root) in roots:
            raise ValueError("Push 仓库重复")
        roots.add(str(root))
        actual = _git(root, "rev-parse", "--show-toplevel").decode("utf-8").strip()
        if Path(actual).resolve() != root:
            raise ValueError("Push 仓库根不精确")
        if not isinstance(plan.get("message"), str) or not plan["message"].strip():
            raise ValueError("Push 缺少精确 commit message")
        files = plan.get("files")
        if not isinstance(files, list) or not files or len(files) > 10000 or len(set(files)) != len(files):
            raise ValueError("Push 文件清单无效")
        branch = _git(root, "symbolic-ref", "--short", "HEAD").decode("utf-8").strip()
        head = _git(root, "rev-parse", "--verify", "HEAD", optional=True)
        upstream = _git(root, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}", optional=True)
        integration = {}
        for name in ("MERGE_HEAD", "REBASE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "rebase-merge", "rebase-apply", "sequencer"):
            location = Path(_git(root, "rev-parse", "--git-path", name).decode("utf-8").strip())
            location = location if location.is_absolute() else root / location
            integration[name] = "directory" if location.is_dir() else _fingerprint(location, optional=True)
        if any(integration[name] is not None for name in integration if name != "MERGE_HEAD"):
            raise ValueError("Git 存在未完成集成状态，保持人工确认")
        if integration["MERGE_HEAD"]:
            merge = _git(root, "rev-parse", "--verify", "MERGE_HEAD").decode().strip()
            if (material["mode"] != "push" or plan.get("mergeHead") != merge
                    or _git_hash(root, "ls-files", "-u") != _digest("")):
                raise ValueError("Git merge 不符合已展示的就绪计划")
        if material["mode"] == "push" and upstream is None:
            if not all(isinstance(plan.get(key), str) and plan[key] for key in ("remote", "remoteBranch")):
                raise ValueError("无 upstream 的 Push 必须在计划中明确 remote 与 remoteBranch")
            if _git(root, "remote", "get-url", plan["remote"], optional=True) is None:
                raise ValueError("计划 remote 不存在")
        snapshots.append({
            "root": str(root), "branch": branch, "head": head.decode().strip() if head else None,
            "upstream": upstream.decode().strip() if upstream else None,
            "remoteHash": _git_hash(root, "remote", "-v"),
            "indexHash": _git_hash(root, "ls-files", "--stage", "-z"),
            "diffHash": _git_hash(root, "diff", "--binary"),
            "stagedHash": _git_hash(root, "diff", "--cached", "--binary"),
            "statusHash": _git_hash(root, "status", "--porcelain=v1", "-z", "--untracked-files=all"),
            "files": {name: _git_file(root, name) for name in files}, "integration": integration,
        })
        for key in ("branch", "head", "upstream"):
            if key not in plan or plan[key] != snapshots[-1][key]:
                raise ValueError("Push 计划与实际 " + key + " 不符")
    return snapshots


def inspect_confirmation(root, input_path, platform, session_id, seconds=None):
    """读取并计算一次完整确认快照。

    Args:
        root: 精确项目根。
        input_path: 项目内结构化确认材料路径。
        platform: 已支持的原生平台。
        session_id: 精确会话 UUID。
        seconds: 可选的本次正整数秒数。
    Returns:
        包含策略、材料摘要、正文、动作和时长的 JSON 对象。
    """
    root = Path(root).resolve(strict=True)
    input_file = _inside(root, input_path)
    input_bytes = _bytes(input_file, limit=INPUT_BYTES)
    material = json.loads(input_bytes.decode("utf-8"))
    kind = material.get("kind")
    if kind not in ACTIONS:
        raise ValueError("未知确认种类")
    active = resolve_active_task(root, {"session_id": session_id}, platform,
                                 allow_single_session_fallback=False, allow_environment_context=False)
    task = material.get("task") if kind == "brief" else active.task_path
    if kind == "brief":
        # Brief 先确认再 start，因此目标任务来自材料；会话绑定另行冻结以检测等待期间的切换。
        if active.source_type != "session" or active.stale:
            raise ValueError("Brief 确认的当前会话任务状态无效")
        if not isinstance(task, str) or not re.fullmatch(r"\.trellis/tasks/[^/]+", task):
            raise ValueError("Brief 确认必须指定活动任务目录")
    if (kind != "brief" or active.task_path is not None) and material.get("task") != active.task_path:
        raise ValueError("确认材料与当前会话任务不符")
    task_manual_only = False
    task_files = {}
    if task:
        metadata_bytes = _bytes(_inside(root, task + "/task.json"))
        task_metadata = json.loads(metadata_bytes.decode("utf-8"))
        if not isinstance(task_metadata, dict) or not isinstance(task_metadata.get("meta", {}), dict):
            raise ValueError("任务元数据无效")
        if kind == "brief":
            closeout = normalize_closeout(task_metadata)
            if not closeout["valid"] or closeout["status"] == "closed":
                raise ValueError("Brief 确认目标任务已关闭或关闭状态无效")
        task_manual_only = task_metadata.get("meta", {}).get("confirmationTimeoutDisabled") is True
        task_files["task.json"] = hashlib.sha256(metadata_bytes).hexdigest()
        for name in ("prd.md", "design.md", "implement.md", "brief.md"):
            if kind == "brief" and name == "brief.md":
                continue
            task_files[name] = _fingerprint(_inside(root, task + "/" + name), optional=True)
    if kind == "brief":
        brief_bytes = _bytes(_inside(root, task + "/brief.md"))
        task_files["brief.md"] = hashlib.sha256(brief_bytes).hexdigest()
        body = brief_bytes.decode("utf-8")
    else:
        body = material.get("body")
    if not isinstance(body, str) or not body.strip():
        raise ValueError("确认正文不能为空")
    body = body.replace("\r\n", "\n").replace("\r", "\n").strip()
    if len(body.encode("utf-8")) > DOCUMENT_BYTES:
        raise ValueError("确认正文超过 512 KiB 展示预算")
    facts = {"task": task, "taskFiles": task_files, "inputHash": hashlib.sha256(input_bytes).hexdigest()}
    if kind == "brief":
        facts["sessionTask"] = active.task_path
    count = 0
    if kind == "check":
        if not all(isinstance(material.get(key), str) and material[key] for key in ("reportVersion", "dispositionVersion")):
            raise ValueError("Check 缺少报告或处置版本")
        findings = material.get("findings")
        if not isinstance(findings, list):
            raise ValueError("Check 缺少待处置问题集合")
        pending = set()
        dispositions = {}
        for finding in findings:
            identifier, disposition = finding.get("id"), finding.get("status", "pending")
            if not isinstance(identifier, str) or not re.fullmatch(r"(?:CHK|FBK|DOC)-[0-9]{3,}", identifier):
                raise ValueError("Check 问题 ID 无效")
            if disposition not in ("pending", "accepted", "resolved"):
                raise ValueError("Check 问题处置无效")
            if identifier in dispositions and dispositions[identifier] != disposition:
                raise ValueError("Check 同 ID 处置状态冲突，保持人工确认")
            dispositions[identifier] = disposition
            if disposition == "pending" and not identifier.startswith("DOC-"):
                pending.add(identifier)
        facts["pending"] = sorted(pending)
        count = len(pending)
    elif kind == "push":
        token = _SNAPSHOT_DEADLINE.set(time.monotonic() + SNAPSHOT_TIMEOUT)
        try:
            facts["repositories"] = _repositories(root, material)
        finally:
            _SNAPSHOT_DEADLINE.reset(token)
    policy = _policy(root)
    config = policy[kind]
    if kind == "brief":
        extra = max(0, len(body) - config["threshold_chars"])
        duration = config["base_seconds"] + ((extra + config["increment_chars"] - 1) // config["increment_chars"]) * config["increment_seconds"]
    elif kind == "check":
        duration = config["base_seconds"] + max(0, count - 1) * config["increment_seconds"]
    else:
        duration = config["seconds"]
    duration = _integer(seconds if seconds is not None else duration, "本次秒数")
    enabled = not task_manual_only and policy["enabled"] and config["enabled"] and not material.get("manualOnly", False) and not (kind == "check" and count == 0)
    return {"version": VERSION, "kind": kind, "action": ACTIONS[kind], "enabled": enabled,
            "reason": "task_manual_only" if task_manual_only else ("no_pending_findings" if kind == "check" and not count else (None if enabled else "disabled")),
            "seconds": duration, "body": body, "pendingCount": count,
            "plan": {key: value for key, value in material.items() if key != "body"},
            "materialHash": _digest(facts), "policyHash": _digest(policy), "facts": facts}


def main():
    """解析只读命令参数并输出 JSON。

    Returns:
        成功返回 0；材料或配置无法核验时返回 1。
    """
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--project", required=True)
    parser.add_argument("--input", required=True)
    parser.add_argument("--platform", required=True, choices=("codex", "claude"))
    parser.add_argument("--session", required=True)
    parser.add_argument("--seconds")
    args = parser.parse_args()
    try:
        result = inspect_confirmation(args.project, args.input, args.platform, args.session, args.seconds)
        print(json.dumps(result, ensure_ascii=False))
        return 0
    except (ValueError, TypeError, AttributeError, OSError, subprocess.SubprocessError) as error:
        print(json.dumps({"enabled": False, "reason": "manual_required", "error": str(error)}, ensure_ascii=False))
        return 1


if __name__ == "__main__":
    sys.exit(main())
