#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""将明确的平台活动交给 Flower 本地采集器；不输出或传递会话内容。"""

from __future__ import annotations

import json
import os
import shutil
import subprocess
import sys
import time
from datetime import datetime, timezone
from pathlib import Path


def config_directory(env: dict, platform: str, home: Path) -> Path:
    """按 Node flowerConfigDirectory 相同规则定位用户目录。

    @param env: 环境变量。
    @param platform: win32 或其他平台。
    @param home: 用户主目录。
    @return: 用户配置路径。
    """
    if env.get("XDG_CONFIG_HOME"):
        return Path(env["XDG_CONFIG_HOME"]) / "flower-trellis"
    if platform == "win32" and env.get("APPDATA"):
        return Path(env["APPDATA"]) / "flower-trellis"
    return home / ".config" / "flower-trellis"


def _read_json(file: Path) -> dict:
    """只读普通小文件；损坏提示由 Node 最终判断。"""
    if any(parent.is_symlink() for parent in [file, *file.parents]):
        raise ValueError("symlink")
    if not file.is_file() or file.stat().st_size > 16384:
        raise ValueError("invalid file")
    value = json.loads(file.read_text(encoding="utf-8"))
    if not isinstance(value, dict):
        raise ValueError("invalid json")
    return value


def _can_skip(platform: str) -> bool:
    """同日提示命中后仍为到期 pending 留出唤醒机会。"""
    try:
        directory = config_directory(os.environ, sys.platform, Path.home())
        state = _read_json(directory / "telemetry.json")
        if state.get("enabled") is False:
            return True
        meta = _read_json(directory / "telemetry-v2" / "meta.json")
        key = datetime.now(timezone.utc).date().isoformat() + ":" + platform
        hint = meta.get("hints", {}).get(key)
        if not isinstance(hint, dict) or not hint.get("event_id"):
            return False
        if meta.get("pending") == 0:
            return hint.get("delivered") is True
        retry = meta.get("nextRetryAt")
        return bool(retry and datetime.fromisoformat(retry.replace("Z", "+00:00")).timestamp() > time.time())
    except (OSError, ValueError, TypeError, AttributeError):
        return False


def main() -> None:
    """适配 SessionStart/UserPromptSubmit，所有失败静默退出。

    @return: 无输出。
    """
    try:
        args = sys.argv[1:]
        if len(args) != 2 or args[0] != "--platform" or args[1] not in ("claude", "codex"):
            return
        platform = args[1]
        if os.environ.get("FLOWER_NO_TELEMETRY") or os.environ.get("TRELLIS_HOOKS") == "0" or os.environ.get("TRELLIS_DISABLE_HOOKS") == "1":
            return
        if platform == "codex" and os.environ.get("CODEX_NON_INTERACTIVE") == "1":
            return
        hook_input = json.loads(sys.stdin.read(1048577) or "{}")
        if not isinstance(hook_input, dict) or hook_input.get("hook_event_name") not in ("SessionStart", "UserPromptSubmit"):
            return
        if _can_skip(platform):
            return
        target = os.environ.get("CLAUDE_PROJECT_DIR" if platform == "claude" else "CODEX_PROJECT_DIR") or hook_input.get("cwd")
        if not isinstance(target, str) or not Path(target).is_dir():
            return
        command = shutil.which("flower-trellis")
        if not command:
            return
        # 只转交固定事件类型、平台和本地定位参数；Node 独占身份创建及队列写入。
        subprocess.run([command, "telemetry", "record-activity", platform, "--target", target],
                       cwd=target, stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
                       stderr=subprocess.DEVNULL, timeout=3, check=False)
    except (OSError, ValueError, TypeError, subprocess.SubprocessError):
        return


if __name__ == "__main__":
    main()
