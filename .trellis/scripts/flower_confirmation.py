#!/usr/bin/env python3
"""项目内原生后台确认命令，使用 Python 标准库独立运行。"""

from __future__ import annotations

import json
import os
import signal
import subprocess
import sys
import time
from pathlib import Path

from flower_confirmation_runtime.store import ConfirmationStore, cancel_project, confirmation_id, wake_message


HELP = """项目内原生后台确认（Linux/WSL、Windows，Codex / Claude）

  python3 -X utf8 .trellis/scripts/flower_confirmation.py prepare --input <项目内 JSON> [--seconds <1..86400>]
  python3 -X utf8 .trellis/scripts/flower_confirmation.py wait --request <UUID>
  python3 -X utf8 .trellis/scripts/flower_confirmation.py claim --request <UUID>
  python3 -X utf8 .trellis/scripts/flower_confirmation.py result --request <UUID> --receipt <UUID> --state <started|completed|interrupted|needs-verification>
  python3 -X utf8 .trellis/scripts/flower_confirmation.py status
  python3 -X utf8 .trellis/scripts/flower_confirmation.py cancel [--request <UUID>]

公共参数：--platform codex|claude、--session <精确 UUID>、--target <项目根>。
prepare 返回完整 display；原生后台工具启动 wait，结束展示回合后计时。
到期只唤醒；claim 与 started 记账成功后进入返回的原流程。
输入、窗口退出和材料变化使旧请求失效。项目策略位于 confirmation_timeout。
hook 从 stdin 接收原生 JSON，由项目 Hook 使用。"""


def _output(value):
    print(json.dumps(value, ensure_ascii=False, allow_nan=False), flush=True)


def _arguments(argv):
    if not argv:
        return None, {}
    command, flags = argv[0], argv[1:]
    allowed = {"prepare": {"--input", "--seconds"}, "wait": {"--request"}, "claim": {"--request"},
               "result": {"--request", "--receipt", "--state"}, "cancel": {"--request"}, "status": set(), "hook": set(),
               "_cancel-project": {"--reason"}}
    if command not in allowed:
        raise ValueError("未知确认命令，使用 --help 查看用法")
    if len(flags) % 2:
        raise ValueError("确认参数缺少值")
    values = {}
    for index in range(0, len(flags), 2):
        key, value = flags[index:index + 2]
        if key not in allowed[command] | {"--target", "--platform", "--session"} or key in values:
            raise ValueError("确认参数未知或重复")
        values[key] = value
    return command, values


def main(argv=None):
    """执行一次项目确认命令并输出结构化结果。

    @param argv: 可选命令参数，缺省读取 sys.argv。
    @return: 成功 0、人工或失败 1、信号中断 130。
    """
    args = sys.argv[1:] if argv is None else argv
    if not args or any(value in ("--help", "-h") for value in args):
        print(HELP)
        return 0
    client = None
    store = None
    owned_request = None
    interrupted = []
    previous_handlers = {}
    try:
        command, values = _arguments(args)
        project = Path(values.get("--target", ".")).resolve(strict=True)
        if command == "_cancel-project":
            reason = values.get("--reason", "project_disabled")
            if reason not in ("project_disabled", "project_updated", "project_uninstalled"):
                raise ValueError("未知项目生命周期原因")
            _output({"cancelled": cancel_project(project, reason)})
            return 0
        if command == "hook":
            if sys.platform not in ("linux", "win32"):
                _output({})
                return 0
            raw = sys.stdin.buffer.read(1024 * 1024 + 1)
            if len(raw) > 1024 * 1024:
                raise ValueError("确认输入超过大小限制")
            payload = json.loads(raw.decode("utf-8"))
            platform = values.get("--platform") or ("codex" if os.environ.get("CODEX_THREAD_ID") else "claude")
            if platform == "codex":
                from flower_confirmation_runtime.codex import record_event
            elif platform == "claude":
                from flower_confirmation_runtime.claude import record_event
            else:
                raise ValueError("未知确认 Hook 平台")
            for attempt in range(3):
                try:
                    _output(record_event(payload))
                    return 0
                except ValueError as error:
                    if str(error) != "确认状态锁繁忙" or attempt == 2:
                        raise
                    time.sleep(0.025)
        environments = {"codex": os.environ.get("CODEX_THREAD_ID"), "claude": os.environ.get("CLAUDE_CODE_SESSION_ID")}
        detected = [key for key, value in environments.items() if value]
        platform = values.get("--platform") or (detected[0] if len(detected) == 1 else None)
        if platform not in environments:
            raise ValueError("缺少唯一的原生平台，保持人工确认")
        session_id = confirmation_id(values.get("--session") or environments[platform])
        if environments[platform] and environments[platform] != session_id:
            raise ValueError("显式会话与当前原生会话不符")
        if not (project / ".trellis").is_dir():
            raise ValueError("目标不是 Trellis 项目")
        store = ConfirmationStore(project, session_id, platform)
        request_id = values.get("--request")
        if request_id:
            confirmation_id(request_id)
        if command in ("wait", "claim", "result") and not request_id:
            raise ValueError("必须指定精确 --request")
        from flower_confirmation_runtime.runtime import cancel, claim, inspect_confirmation, prepare, record_result, status, wait
        if command == "status":
            _output(status(store))
            return 0
        if command == "cancel":
            _output(cancel(store, request_id=request_id))
            return 0
        if command == "result":
            confirmation_id(values.get("--receipt"))
            if values.get("--state") != "started":
                _output(record_result(store, None, request_id, values["--receipt"], values.get("--state")))
                return 0
        if command == "prepare":
            if not values.get("--input"):
                raise ValueError("prepare 必须提供项目内 --input 材料")
            frozen = inspect_confirmation(store, values["--input"], values.get("--seconds"))
            if not frozen["enabled"]:
                _output({"prepared": False, "mode": "manual", "reason": frozen.get("reason")})
                return 0
        for number in (signal.SIGINT, signal.SIGTERM):
            previous_handlers[number] = signal.getsignal(number)
            signal.signal(number, lambda signum, frame: interrupted.append(True))
        if platform == "codex":
            from flower_confirmation_runtime.codex import CodexClient
            client = CodexClient(project, session_id)
        else:
            from flower_confirmation_runtime.claude import ClaudeClient
            client = ClaudeClient(project, session_id)
        owned_request = request_id
        client.connect()
        if command == "prepare":
            result = prepare(store, client, values["--input"], values.get("--seconds"))
            owned_request = result.get("requestId")
            if interrupted:
                raise ValueError("确认准备已中断")
        elif command == "claim":
            result = claim(store, client, request_id)
        elif command == "result":
            result = record_result(store, client, request_id, values["--receipt"], values["--state"])
        else:
            result = wait(store, client, request_id, lambda: bool(interrupted), _output)
            _output({"event": "finished", **status(store), "workerResult": result["state"],
                     **({"notification": wake_message(request_id, platform)} if result["state"] == "queued" else {})})
            return 130 if interrupted else 1 if result["state"] in ("failed", "delivery_unknown") else 0
        _output(result)
        return 0
    except (ValueError, OSError, KeyError, TypeError, AttributeError, subprocess.SubprocessError) as error:
        _output({"prepared": False, "claimed": False, "mode": "manual", "reason": str(error)})
        return 130 if interrupted else 1
    finally:
        try:
            if interrupted and store and owned_request:
                from flower_confirmation_runtime.runtime import cancel
                cancel(store, client, owned_request)
        finally:
            if client:
                client.close()
            for number, handler in previous_handlers.items():
                signal.signal(number, handler)


if __name__ == "__main__":
    sys.exit(main())
