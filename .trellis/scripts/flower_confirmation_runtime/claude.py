"""Claude 原生事件、精确后台任务与 transcript 来源校验。"""

from __future__ import annotations

import re
import shlex
import sys
from pathlib import Path
from uuid import uuid4

from .session_log import DISPLAY_BYTES, SessionLog
from .store import ACTIVE, ConfirmationStore, confirmation_id, native_id, now_ms, text_content, validate_turn_origin, wake_message
from .windows_process import hook_frontend, open_identity


def _task_notice(text):
    matches = {value.strip() for value in re.findall(r"<task-id>\s*([^<>]+?)\s*</task-id>", text_content(text))}
    if len(matches) != 1:
        return None
    identifier = matches.pop()
    return identifier or None


def _wait_command(command, project, session_id, request_id):
    if not isinstance(command, str):
        return False
    try:
        tokens = shlex.split(command)
        entry = next(index for index, token in enumerate(tokens)
                     if Path(token).name == "flower_confirmation.py")
        executable = Path(tokens[0]).name
        if sys.platform == "win32":
            executable = executable.lower()
            if executable.endswith(".exe"):
                executable = executable[:-4]
        if not re.fullmatch(r"(?:python(?:[0-9]+(?:\.[0-9]+)*)?|py)", executable):
            return False
        prefix = tokens[1:entry]
        if sys.platform == "win32" and executable == "py" and prefix and re.fullmatch(r"-3(?:\.[0-9]+)?", prefix[0]):
            prefix = prefix[1:]
        if prefix not in ([], ["-X", "utf8"], ["-3"], ["-3", "-X", "utf8"]):
            return False
        if (project / tokens[entry]).resolve(strict=True) != project / ".trellis/scripts/flower_confirmation.py":
            return False
        if tokens[entry + 1] != "wait":
            return False
        flags = tokens[entry + 2:]
        if len(flags) % 2:
            return False
        values = {}
        for index in range(0, len(flags), 2):
            key, value = flags[index:index + 2]
            if key not in ("--request", "--platform", "--session", "--target") or key in values:
                return False
            values[key] = value
        return (values.get("--request") == request_id and values.get("--platform", "claude") == "claude"
                and values.get("--session", session_id) == session_id
                and (project / values.get("--target", ".")).resolve(strict=True) == project)
    except (ValueError, OSError, StopIteration, IndexError):
        return False


def record_event(payload, *, platform=None):
    """保存四类原生事件，内部通知在领取时再次核验来源。

    @param payload: 原始 Claude Hook JSON。
    @param platform: 测试可注入的平台能力条件。
    @return: 空输出或本请求的原生唤醒上下文。
    """
    event = payload.get("hook_event_name")
    if payload.get("agent_id") or event not in ("SessionStart", "UserPromptSubmit", "Stop", "SessionEnd"):
        return {}
    project = Path(payload["cwd"]).resolve(strict=True)
    if not (project / ".trellis/scripts/confirmation_policy.py").is_file():
        return {}
    session_id = confirmation_id(payload["session_id"])
    transcript = payload.get("transcript_path")
    if not isinstance(transcript, str) or not transcript:
        raise ValueError("Claude 缺少精确会话记录路径")
    store = ConfirmationStore(project, session_id, "claude")
    frontend = hook_frontend("claude.exe") if (platform or sys.platform) == "win32" else None
    reminders = []
    def apply(old):
        value = old or {"version": 2, "sessionId": session_id, "generation": 0, "inputSeq": 0, "request": None}
        native = value.get("native") or {"version": 1, "project": str(project), "sessionId": session_id,
                                         "tasks": {}, "runId": str(uuid4())}
        if native.get("project") != str(project) or native.get("sessionId") != session_id:
            raise ValueError("Claude 事件会话或项目不符")
        request = value.get("request")
        if frontend is not None:
            if native.get("frontend") and native["frontend"] != frontend:
                try:
                    previous = open_identity(native["frontend"])
                except (ProcessLookupError, ValueError):
                    previous = None
                except OSError as error:
                    if getattr(error, "winerror", None) != 87:
                        raise
                    previous = None
                if previous:
                    previous.close()
                    native["ambiguous"] = True
                    value.pop("turnOrigin", None)
                    value["generation"] += 1
                    if request and request["state"] in ACTIVE:
                        request.update(state="cancelled", error="frontend_ambiguous")
                    return value
            if event == "SessionStart":
                native.update(frontend=frontend, ambiguous=False)
            elif native.get("frontend") != frontend or native.get("ambiguous"):
                raise ValueError("Windows Claude Hook 未绑定唯一的原始前台")
        native.update(transcript=transcript, at=now_ms())
        if event == "SessionStart":
            value.pop("turnOrigin", None)
            native.update(runId=str(uuid4()), ended=False, tasks={}, turn=None)
            value["generation"] += 1
            if request and request["state"] in ACTIVE:
                request.update(state="cancelled", error="session_restarted")
        elif event == "UserPromptSubmit":
            prompt_id = native_id(payload.get("prompt_id"))
            if (value.get("turnOrigin") or {}).get("turnId") != prompt_id:
                value.pop("turnOrigin", None)
            task_id = _task_notice(payload.get("prompt"))
            owned = native.get("tasks", {}).get(task_id)
            previous_turn = (native.get("turn") or {}).get("id")
            native.update(turn={"id": prompt_id, "status": "inProgress"}, ended=False)
            if owned:
                native["notification"] = {"taskId": task_id, "requestId": owned, "promptId": prompt_id}
                reminders.append(wake_message(owned, "claude"))
            elif previous_turn != prompt_id:
                value.pop("turnOrigin", None)
                # Hook 也承载 peer 新回合；序号只使旧计划失效，不授予人工批准。
                value["inputSeq"] = value.get("inputSeq", 0) + 1
                value["generation"] += 1
                if request and request["state"] in ACTIVE:
                    request.update(state="cancelled", error="user_replied")
        elif event == "Stop":
            prompt_id = native_id(payload.get("prompt_id"))
            text = payload.get("last_assistant_message")
            if not isinstance(text, str) or len(text.encode("utf-8")) > DISPLAY_BYTES:
                raise ValueError("Claude 最终展示正文不可核验")
            native["turn"] = {"id": prompt_id, "status": "completed"}
            native["display"] = {"turnId": prompt_id, "text": text, "at": native["at"]}
            for task in payload.get("background_tasks", []):
                if (not request or task.get("type") != "shell"
                        or not _wait_command(task.get("command"), project, session_id, request["id"])):
                    continue
                task_id = task.get("id")
                native_id(task_id)
                native["tasks"][task_id] = request["id"]
                request["nativeTaskId"] = task_id
            for identifier in list(native["tasks"])[:-8]:
                del native["tasks"][identifier]
        else:
            value.pop("turnOrigin", None)
            native["ended"] = True
            value["generation"] += 1
            if request and request["state"] in ACTIVE:
                request.update(state="cancelled", error="session_ended")
        value["native"] = native
        return value
    store.update(apply)
    return {"hookSpecificOutput": {"hookEventName": "UserPromptSubmit", "additionalContext": reminders[-1]}} if reminders else {}


class ClaudeClient:
    """观察精确原生 Hook 状态与会话记录。"""

    delivery_on_exit = True

    def __init__(self, project, session_id, platform=None):
        """创建精确会话适配器。

        @param project: 项目根。
        @param session_id: 原生会话 UUID。
        @param platform: 可注入的平台能力条件。
        @return: None。
        """
        self.platform = platform or sys.platform
        if self.platform not in ("linux", "win32") or (self.platform == "win32" and sys.platform != "win32"):
            raise ValueError("Claude 后台确认需要可核验的 Linux/WSL 或原生 Windows 会话")
        self.store = ConfirmationStore(project, session_id, "claude")
        self.project = self.store.project
        self.session_id = self.store.session_id
        self.session_log = SessionLog()
        self.log_start = None
        self.prepare_turn = None
        self.prepare_origin = None
        self.frontend = None

    def connect(self):
        """核对原生 Hook 和活动 runId。

        @return: None。
        """
        native = (self.store.read() or {}).get("native") or {}
        if (native.get("version") != 1 or native.get("project") != str(self.project)
                or native.get("sessionId") != self.session_id or native.get("ended")):
            raise ValueError("Claude 原生确认 Hook 不可用或会话已退出，保持人工确认")
        confirmation_id(native.get("runId"))

    def prepare_snapshot(self):
        """核对当前回合并冻结本次新增记录的观察边界。

        @return: 本次准备的会话快照及冻结起点。
        """
        return self._snapshot(preparing=True)

    def snapshot(self):
        """增量核验 transcript 的真实人工和任务通知来源。

        @return: 输入序号、当前回合、通知和 runId。
        """
        return self._snapshot(preparing=False)

    def _snapshot(self, preparing):
        state = self.store.read() or {}
        native = state.get("native") or {}
        if not native or native.get("ended"):
            raise ValueError("Claude 会话状态不可用")
        if self.platform == "win32":
            identity = native.get("frontend")
            if not identity or native.get("ambiguous"):
                raise ValueError("Windows Claude 缺少唯一原始前台，保持人工确认")
            if self.frontend is None:
                self.frontend = open_identity(identity)
            if self.frontend.exited() or self.frontend.identity != identity:
                raise ValueError("frontend_exited")
        file = Path(native["transcript"])
        hook_notice = native.get("notification") or {}
        if preparing:
            turn = native.get("turn") or {}
            prompt_id = native_id(turn.get("id"))
            if turn.get("status") != "inProgress":
                raise ValueError("正式确认必须由当前活动回合准备")
            key = (native["runId"], prompt_id)
            saved = state.get("turnOrigin")
            origin = None if isinstance(saved, dict) and saved.get("version") == 1 else validate_turn_origin(saved, "claude", self.session_id)
            if origin and origin["inputSeq"] == state.get("inputSeq", 0) and origin["turnId"] != prompt_id:
                raise ValueError("Claude 当前回合来源已变化")
            if self.prepare_turn != key or self.log_start is None:
                if origin and origin["turnId"] == prompt_id and origin["inputSeq"] == state.get("inputSeq", 0):
                    if origin["runId"] != native["runId"] or origin.get("frontend") != native.get("frontend"):
                        raise ValueError("Claude 当前回合来源属于旧运行或窗口")
                    self._resume_log(origin["logStart"], origin["inputSeq"])
                    self.session_log.read(file, {}, self._consume_transcript)
                    SessionLog.verify(origin["logObservation"], self.session_log.observation)
                    self.prepare_origin = {key: value for key, value in origin.items() if key != "logObservation"}
                else:
                    start = SessionLog.checkpoint(file)
                    self._resume_log(start, state.get("inputSeq", 0))
                    self.prepare_origin = {"version": 2, "platform": "claude", "sessionId": self.session_id,
                                           "turnId": prompt_id, "inputSeq": self.prepare_baseline, "kind": "native",
                                           "runId": native["runId"], "frontend": native.get("frontend"), "logStart": start}
                self.prepare_turn = key
        else:
            request = state.get("request") or {}
            start = (request.get("contract") or {}).get("claudeLogStart")
            if not start:
                raise ValueError("Claude 请求缺少当前确认的记录起点，保持人工确认")
            if self.log_start != start:
                self._resume_log(start, request.get("inputSeq", state.get("inputSeq", 0)))
            self.prepare_turn = None
        record, observation = self.session_log.read(file, {}, self._consume_transcript)
        task_id = hook_notice.get("taskId")
        prompt_id = record["notifications"].get(task_id)
        request_id = native.get("tasks", {}).get(task_id)
        notification = ({"taskId": task_id, "requestId": request_id, "promptId": prompt_id}
                        if prompt_id and request_id else None)
        pending = (hook_notice.get("promptId") == (native.get("turn") or {}).get("id")
                   and hook_notice.get("requestId") == native.get("tasks", {}).get(hook_notice.get("taskId"))
                   and bool(hook_notice.get("requestId"))
                   and (notification or {}).get("promptId") != hook_notice.get("promptId"))
        sequence = max(state.get("inputSeq", 0), self.prepare_baseline + record["inputCount"])
        return {"inputSeq": sequence, "turn": native.get("turn"), "notification": notification,
                "notificationPending": pending, "runId": native.get("runId"), "logObservation": observation,
                "claudeLogStart": self.log_start, "prepareBaseline": self.prepare_baseline,
                "turnOrigin": self.prepare_origin if preparing else None}

    def _resume_log(self, start, sequence):
        self.session_log.resume(start, {"inputCount": 0, "notifications": {}})
        self.log_start = start
        self.prepare_baseline = sequence

    def _consume_transcript(self, record, entry):
        if not isinstance(entry, dict):
            return
        if entry.get("type") != "user" or entry.get("isSidechain") or entry.get("sessionId") != self.session_id:
            return
        prompt_id = entry.get("promptId")
        origin = entry.get("origin")
        kind = origin.get("kind") if isinstance(origin, dict) else None
        if not kind:
            kind = entry.get("turnOrigin")
        # 只提取必要的正向证据；新增上下文记录不会让整个自动流程失败。
        if kind == "human":
            record["inputCount"] = record.get("inputCount", 0) + 1
        elif kind in ("task-notification", "task_notification") and entry.get("queueTranscriptOnly") is not True:
            message = entry.get("message")
            task_id = _task_notice(message.get("content") if isinstance(message, dict) else None)
            if task_id and isinstance(prompt_id, str) and prompt_id:
                notices = record["notifications"]
                notices.pop(task_id, None)
                notices[task_id] = entry.get("promptId")
                if len(notices) > 8:
                    del notices[next(iter(notices))]

    def assistant_text(self, turn_id):
        """读取 Stop 绑定的原始展示。

        @param turn_id: 原回合标识。
        @return: 完整最终正文。
        """
        display = ((self.store.read() or {}).get("native") or {}).get("display") or {}
        if display.get("turnId") != turn_id:
            raise ValueError("Claude 最终展示回合不符")
        return display["text"]

    def add(self, request_id):
        """返回 Stop 已绑定的本次后台任务。

        @param request_id: 请求 UUID。
        @return: 原生任务标识；worker 随后退出以触发通知。
        """
        request = (self.store.read() or {}).get("request") or {}
        if request.get("id") != request_id or not request.get("nativeTaskId"):
            raise ValueError("Claude 未绑定本次原生后台任务")
        return {"id": request["nativeTaskId"]}

    def remove(self, queue_id):
        """报告 Claude 原生完成通知的撤回边界。

        @param queue_id: 精确任务 ID。
        @return: not_retractable 或 not_pending。
        """
        return "not_retractable" if queue_id else "not_pending"

    def close(self):
        """结束本客户端的本次观察。

        @return: None。
        """
        if self.frontend:
            self.frontend.close()
            self.frontend = None
