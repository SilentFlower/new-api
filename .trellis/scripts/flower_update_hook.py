#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""复用 SessionStart 升级入口，检测 CLI 缺失并提供对话安装引导。

只读检测，不执行 npm；CLI 已安装时沿用 self-check 更新检查。
"""

from __future__ import annotations

import argparse
import json
import os
import re
import shutil
import subprocess
import sys
from pathlib import Path


ACTIONABLE_STATUSES = {"update_available", "project_out_of_sync"}
SELF_CHECK_TIMEOUT_SECONDS = 30
BOOTSTRAP_STATUSES = {"cli_missing", "cli_unavailable", "project_version_unavailable", "prerequisites_missing"}
MAX_LOCK_BYTES = 2 * 1024 * 1024


def _executable_status(name: str) -> str:
    """检查 PATH，区分缺失与已有但不可执行的入口。"""
    if shutil.which(name):
        return "available"
    names = [name]
    if os.name == "nt":
        names.extend(name + suffix for suffix in os.environ.get("PATHEXT", ".COM;.EXE;.BAT;.CMD").split(";"))
    for directory in os.get_exec_path():
        if any(os.path.lexists(os.path.join(directory, candidate)) for candidate in names):
            return "unavailable"
    return "missing"


def _locked_flower_version(project_dir: Path) -> str:
    """只读取受支持的内置锁条目，拒绝把未知版本拼入安装命令。"""
    lock_path = project_dir / ".flower" / "plugin-lock.json"
    if lock_path.parent.is_symlink() or lock_path.is_symlink() or not lock_path.is_file():
        raise ValueError("项目缺少普通文件形式的 .flower/plugin-lock.json")
    if lock_path.stat().st_size > MAX_LOCK_BYTES:
        raise ValueError("项目锁超过读取上限")
    lock = json.loads(lock_path.read_text(encoding="utf-8"))
    if not isinstance(lock, dict) or type(lock.get("schemaVersion")) is not int or lock["schemaVersion"] != 1:
        raise ValueError("项目锁 schema 不受支持")
    plugins, roots = lock.get("plugins"), lock.get("roots")
    if not isinstance(plugins, list) or not isinstance(roots, list) or any(not isinstance(root, str) for root in roots):
        raise ValueError("项目锁 plugins/roots 结构无效")
    if any(not isinstance(plugin, dict) for plugin in plugins):
        raise ValueError("项目锁 Plugin 条目无效")
    matches = [plugin for plugin in plugins if plugin.get("id") == "flower/skill-garden"]
    if len(matches) != 1:
        raise ValueError("项目锁缺少唯一的 flower/skill-garden 条目")
    plugin = matches[0]
    source = plugin.get("source")
    if (
        not isinstance(source, dict)
        or source.get("id") != "flower"
        or source.get("type") != "builtin"
        or not isinstance(source.get("reference"), str)
        or source.get("reference") not in {
            "package:skill-garden:old", "package:skill-garden:0.5", "package:skill-garden:0.6",
        }
    ):
        raise ValueError("项目锁的 Skill-Garden 来源不是受支持的内置来源")
    version = plugin.get("version")
    match = re.fullmatch(r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?", version) if isinstance(version, str) and len(version) <= 256 else None
    if not match or any(int(part) > 9007199254740991 for part in match.groups()[:3]):
        raise ValueError("项目 Flower 版本不是完整有效的 SemVer")
    if match[4] and any(part.isdigit() and len(part) > 1 and part.startswith("0") for part in match[4].split(".")):
        raise ValueError("项目 Flower 预发布版本包含非法数字标识")
    return version


def _check_cli(project_dir: Path) -> dict | None:
    """只读检查本机入口，返回安装或环境诊断；可用时返回空值。"""
    if not project_dir.is_dir():
        return {"status": "cli_unavailable", "reason": "目标项目目录不存在"}
    cli_status = _executable_status("flower-trellis")
    if cli_status == "available":
        return None
    if cli_status == "unavailable":
        return {"status": "cli_unavailable", "reason": "PATH 中已有 Flower 入口但不可执行，请检查权限、链接与解释器"}
    try:
        version = _locked_flower_version(project_dir)
    except (OSError, ValueError) as exc:
        return {"status": "project_version_unavailable", "reason": f"Flower CLI 缺失，项目版本无法确认：{exc}"}
    missing = [name for name in ("node", "npm") if _executable_status(name) != "available"]
    if missing:
        return {"status": "prerequisites_missing", "version": version, "reason": f"Flower CLI 缺失，请先准备可执行的 {', '.join(missing)}"}
    return {"status": "cli_missing", "version": version, "command": f"npm install -g flower-trellis@{version}"}


def _format_bootstrap(data: dict) -> str:
    """生成唯一的对话安装协议，检测脚本自身不执行安装。"""
    lines = [
        "<flower-cli-bootstrap>",
        "priority: blocking_confirmation_required",
        f"status: {data['status']}",
    ]
    if data.get("version"):
        lines.append(f"project_flower: {data['version']}")
    if data.get("reason"):
        lines.append("reason: " + json.dumps(data["reason"], ensure_ascii=False))
    if data.get("command"):
        lines.extend([
            f"recommended_command: {data['command']}",
            "ai_instruction: 告知成员本机缺少 Flower CLI，展示锁定版本和 recommended_command；说明全局 npm 安装会同步本机 Trellis 命令。先征得当前成员确认，确认前禁止执行 recommended_command；当前对话已明确授权这次安装时不重复确认。",
            "verification: 安装返回成功后，在目标项目执行 flower-trellis -v 核对 CLI 首行版本，再执行 flower-trellis self-check --json --target <目标项目>；参数按所在 shell 引用。确认 CLI 可用后继续原请求，不把项目锁视为内容完整性证明，不隐式执行 init/update/self-update。",
        ])
    else:
        lines.append("ai_instruction: 说明诊断原因与缺失前提；版本未知时请维护者补齐有效项目锁，不依据旧 manifest 或猜测 latest 安装；入口异常先修复 PATH、权限或解释器。")
    lines.extend([
        "failure_policy: 成员拒绝后本次对话不重复追问；安装或验证失败如实报告，不自动提权、不循环安装。依赖 CLI 的操作说明尚未满足的前提；本次已处理的相同引导不重复执行。",
        "</flower-cli-bootstrap>",
    ])
    return "\n".join(lines)


def _debug(message: str) -> None:
    """在显式调试时输出简短错误。"""
    if os.environ.get("FLOWER_UPDATE_HOOK_DEBUG"):
        print(f"flower_update_hook: {message}", file=sys.stderr)


def _project_dir(hook_input: dict) -> Path:
    """从平台环境变量或 hook stdin 解析项目目录。"""
    for name in (
        "CLAUDE_PROJECT_DIR",
        "CODEX_PROJECT_DIR",
        "CURSOR_PROJECT_DIR",
        "GEMINI_PROJECT_DIR",
        "QODER_PROJECT_DIR",
        "CODEBUDDY_PROJECT_DIR",
        "TRAE_PROJECT_DIR",
    ):
        value = os.environ.get(name)
        if value:
            return Path(value).resolve()
    return Path(str(hook_input.get("cwd") or ".")).resolve()


def _run_self_check(project_dir: Path) -> dict | None:
    """执行 flower-trellis self-check 并解析 JSON。"""
    bootstrap = _check_cli(project_dir)
    if bootstrap:
        return bootstrap
    try:
        result = subprocess.run(
            [
                "flower-trellis",
                "self-check",
                "--json",
                "--target",
                str(project_dir),
            ],
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
            timeout=SELF_CHECK_TIMEOUT_SECONDS,
            cwd=str(project_dir),
        )
    except subprocess.TimeoutExpired:
        return {"status": "cli_unavailable", "reason": "Flower CLI 检查超时，不能据此判定未安装"}
    except OSError as exc:
        return {"status": "cli_unavailable", "reason": f"Flower 入口存在但执行失败，请检查 PATH、权限与解释器：{exc}"}
    if result.returncode != 0:
        _debug(result.stderr.strip())
        return {"status": "cli_unavailable", "reason": f"Flower CLI 检查退出码 {result.returncode}，请先诊断现有安装"}
    try:
        data = json.loads(result.stdout)
    except json.JSONDecodeError as exc:
        _debug(f"JSON 解析失败:{exc}")
        return {"status": "cli_unavailable", "reason": "Flower CLI 返回的检查结果不是有效 JSON"}
    return data if isinstance(data, dict) and isinstance(data.get("status"), str) else {"status": "cli_unavailable", "reason": "Flower CLI 检查结果结构无效"}


def _json_bool(value: object) -> str:
    """把布尔值格式化成 JSON 风格小写文本。"""
    return json.dumps(bool(value), ensure_ascii=False)


def _has_release_notes(data: dict) -> bool:
    """判断 self-check 是否提供了可展示的 release notes。"""
    notes = data.get("releaseNotes")
    if not isinstance(notes, dict):
        return False
    versions = notes.get("versions")
    return isinstance(versions, list) and bool(versions)


def _release_notes_lines(data: dict) -> list[str]:
    """把 self-check 的 releaseNotes 摘要格式化为短字段。"""
    notes = data.get("releaseNotes")
    if not isinstance(notes, dict):
        return []
    versions = notes.get("versions") or []
    if not isinstance(versions, list):
        versions = []
    safe_versions = []
    for entry in versions:
        if not isinstance(entry, dict):
            continue
        version = entry.get("version")
        body = entry.get("body")
        if isinstance(version, str) and isinstance(body, str) and body.strip():
            safe_versions.append(
                {
                    "version": version,
                    "body": body,
                    "truncated": bool(entry.get("truncated")),
                }
            )

    lines: list[str] = []
    note_range = notes.get("range") or {}
    if isinstance(note_range, dict) and (note_range.get("from") or note_range.get("to")):
        range_text = f"{note_range.get('from')} -> {note_range.get('to')}"
        if note_range.get("channel"):
            range_text += f" ({note_range.get('channel')})"
        lines.append(f"release_notes_range: {range_text}")
    if safe_versions:
        lines.append(
            "release_notes: "
            + json.dumps(safe_versions, ensure_ascii=False, separators=(",", ":"))
        )
    elif notes.get("unavailable"):
        lines.append("release_notes_unavailable: true")
    if notes.get("truncated"):
        lines.append(f"release_notes_truncated: {_json_bool(notes.get('truncated'))}")
    if notes.get("moreVersions"):
        lines.append(f"release_notes_more_versions: {_json_bool(notes.get('moreVersions'))}")
    return lines


def _ai_instruction(data: dict, command: str | None) -> str | None:
    """生成面向 AI 的单条动作指令,避免重复字段互相打架。"""
    ai = data.get("ai") or {}
    instruction = ai.get("instruction")
    if ai.get("mode") == "ask" and command:
        if _has_release_notes(data):
            return "先展示 release_notes 摘要和 recommended_command,再询问用户确认;确认前禁止执行 recommended_command;用户拒绝本次升级时执行 snooze_command,明确跳过时执行 skip_command。"
        return "先展示 recommended_command,再询问用户确认;确认前禁止执行 recommended_command;用户拒绝本次升级时执行 snooze_command,明确跳过时执行 skip_command。"
    return instruction if isinstance(instruction, str) and instruction else None


def _format_context(data: dict) -> str:
    """把 self-check JSON 转成给 AI 读取的短上下文块。"""
    current = data.get("current") or {}
    project = data.get("project") or {}
    remote = data.get("remote") or {}
    ai = data.get("ai") or {}
    prompt = data.get("prompt") or {}
    safety = data.get("safety") or {}
    out_of_sync_reasons = project.get("outOfSyncReasons") or []
    current_trellis = current.get("bundledTrellisVersion")
    project_trellis = project.get("trellisVersion")
    lines = [
        "<flower-update>",
        "priority: blocking_confirmation_required",
        "instruction_scope: first_assistant_reply",
        f"status: {data.get('status')}",
        f"current_flower: {current.get('flowerVersion')}",
        f"project_flower: {project.get('flowerVersion')}",
    ]
    if current_trellis and project_trellis and current_trellis != project_trellis:
        lines.append(f"bundled_trellis: {current_trellis}")
        lines.append(f"project_trellis: {project_trellis}")
    if out_of_sync_reasons:
        lines.append(f"project_out_of_sync_reasons: {', '.join(out_of_sync_reasons)}")
    if data.get("status") == "update_available" and remote.get("tags"):
        lines.append(f"remote: {json.dumps(remote.get('tags'), ensure_ascii=False)}")
    if remote.get("errorCode"):
        lines.append(f"remote_error_code: {remote.get('errorCode')}")
    lines.extend(_release_notes_lines(data))
    command = (data.get("commands") or {}).get("recommended") or ai.get("command")
    if command:
        lines.append(f"recommended_command: {command}")
    prompt_commands = prompt.get("commands") if isinstance(prompt, dict) else {}
    if isinstance(prompt_commands, dict):
        snooze_command = prompt_commands.get("snooze")
        skip_command = prompt_commands.get("skip")
        if isinstance(snooze_command, str) and snooze_command:
            lines.append(f"snooze_command: {snooze_command}")
        if isinstance(skip_command, str) and skip_command:
            lines.append(f"skip_command: {skip_command}")
    if safety.get("reasons"):
        lines.append(f"safety_reasons: {', '.join(safety.get('reasons') or [])}")
    instruction = _ai_instruction(data, command)
    if instruction:
        lines.append(f"ai_instruction: {instruction}")
    lines.append("</flower-update>")
    return "\n".join(lines)


def _system_message(data: dict) -> str:
    """生成 Codex / Claude Code 更容易注意到的短系统提示。"""
    ai = data.get("ai") or {}
    command = (data.get("commands") or {}).get("recommended") or ai.get("command")
    if ai.get("mode") == "ask" and command:
        if _has_release_notes(data):
            return "flower-trellis 发现可执行更新;必须先展示更新摘要并询问用户是否执行 recommended_command,确认前禁止运行;用户拒绝时执行 snooze_command。"
        return "flower-trellis 发现可执行更新;必须先询问用户是否执行 recommended_command,确认前禁止运行;用户拒绝时执行 snooze_command。"
    if command:
        return "flower-trellis 发现可执行更新;已注入 recommended_command。"
    return "flower-trellis update context injected"


def _emit_context(context: str, system_message: str) -> None:
    """输出 Codex / Claude Code 都接受的 SessionStart hook JSON。"""
    result = {
        "suppressOutput": True,
        "systemMessage": system_message,
        "hookSpecificOutput": {
            "hookEventName": "SessionStart",
            "additionalContext": context,
        },
    }
    print(json.dumps(result, ensure_ascii=False), flush=True)


def main(argv: list[str] | None = None) -> None:
    """处理宿主启动或手动只读检测入口并输出上下文。

    Args:
        argv: 显式参数列表，省略时读取进程参数。
    Returns:
        无返回值；通过标准输出返回宿主 JSON。
    """
    parser = argparse.ArgumentParser(description="Flower CLI 安装与升级只读检测")
    parser.add_argument("--bootstrap-only", action="store_true")
    parser.add_argument("--target", type=Path)
    args = parser.parse_args(argv)
    if not args.bootstrap_only and (os.environ.get("TRELLIS_HOOKS") == "0" or os.environ.get("TRELLIS_DISABLE_HOOKS") == "1" or os.environ.get("CODEX_NON_INTERACTIVE") == "1"):
        return
    try:
        hook_input = {} if args.bootstrap_only else json.loads(sys.stdin.read() or "{}")
        if not isinstance(hook_input, dict):
            hook_input = {}
    except json.JSONDecodeError:
        hook_input = {}

    project_dir = args.target.resolve() if args.target else _project_dir(hook_input)
    data = _check_cli(project_dir) if args.bootstrap_only else _run_self_check(project_dir)
    if data and data.get("status") in BOOTSTRAP_STATUSES:
        _emit_context(_format_bootstrap(data), "Flower CLI 需要安装确认或环境诊断")
        return
    if not data or data.get("status") not in ACTIONABLE_STATUSES:
        return
    _emit_context(_format_context(data), _system_message(data))


if __name__ == "__main__":
    main()
