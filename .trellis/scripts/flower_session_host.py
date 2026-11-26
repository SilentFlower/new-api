"""按当前命令的真实祖先进程选择会话所属平台。"""

import os
import sys
from pathlib import Path


def select_platform(candidates):
    """从多个环境候选中选择最近的原生宿主。

    @param candidates: 含有效会话变量的平台名称集合。
    @return: 当前宿主的平台名称。
    """
    host = None
    if sys.platform == "win32":
        from flower_confirmation_runtime.windows_process import hook_frontend
        try:
            identity = hook_frontend(("codex.exe", "claude.exe"))
            host = Path(identity["exe"]).stem
        except (OSError, ValueError):
            pass
    elif sys.platform == "linux":
        pid, seen = os.getpid(), set()
        while pid > 0 and pid not in seen:
            seen.add(pid)
            try:
                root = Path("/proc") / str(pid)
                fields = (root / "stat").read_text().rsplit(") ", 1)[1].split()
                name = Path(os.readlink(root / "exe")).name
                # Claude 的 Linux ELF 安装包也使用 claude.exe，须在继续回溯外层工具前识别。
                if name in ("codex", "claude", "claude.exe"):
                    host = "claude" if name == "claude.exe" else name
                    break
                pid = int(fields[1])
            except (OSError, ValueError, IndexError):
                break
    if host in candidates:
        return host
    raise ValueError("多个平台会话变量共存，当前宿主身份不明确；请显式指定平台或 TRELLIS_CONTEXT_ID")
