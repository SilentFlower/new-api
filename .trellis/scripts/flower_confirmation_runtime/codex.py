"""Codex 精确会话、原生队列与冻结前台接入。"""

from __future__ import annotations

import json
import os
import re
import select
import sys
import time
from collections import OrderedDict
from pathlib import Path
from uuid import uuid4

from flower_tui_lifetime import bind_origin, check_binding, check_frontend_coverage, open_binding
from .session_log import DISPLAY_BYTES, RECORD_BYTES, SessionLog
from .store import ACTIVE, ConfirmationStore, confirmation_id, now_ms, wake_id, wake_message
from .transport import CodexTransport, RpcError
from .windows_process import check_listener_coverage, hook_frontend, listener_identity, open_identity


def record_event(payload):
    """原生人工输入进入模型前失效旧请求。

    @param payload: Codex Hook 原始 JSON。
    @return: 空 Hook 输出。
    """
    if payload.get("agent_id") or payload.get("hook_event_name") not in ("UserPromptSubmit", "Interrupt"):
        return {}
    project = Path(payload["cwd"]).resolve(strict=True)
    if not (project / ".trellis/scripts/confirmation_policy.py").is_file():
        return {}
    session_id = confirmation_id(payload["session_id"])
    store = ConfirmationStore(project, session_id, "codex")
    frontend = hook_frontend("codex.exe") if sys.platform == "win32" else None
    def apply(old):
        value = old or {"version": 2, "sessionId": session_id, "generation": 0,
                        "lastHumanMessageId": None, "request": None}
        value["native"] = {"version": 2, "project": str(project), "sessionId": session_id,
                           "at": now_ms(), "turnId": payload.get("turn_id")}
        if frontend is not None:
            value["native"]["hookProcess"] = frontend
        request = value.get("request")
        internal = (payload["hook_event_name"] == "UserPromptSubmit" and request
                    and request["state"] in ("dispatching", "queued") and payload.get("prompt") == wake_message(request["id"]))
        # Hook 缺少 client_id，仅暂缓精确提醒取消；claim 仍核对原生记录来源。
        if not internal:
            value["generation"] += 1
            value["lastHumanMessageId"] = str(uuid4())
            if request and request["state"] in ACTIVE:
                request.update(state="cancelled", error="turn_interrupted" if payload["hook_event_name"] == "Interrupt" else "user_replied")
        return value
    store.update(apply)
    return {}


class CodexClient:
    """只观察原生精确会话，沿用当前共享服务和前台。"""

    delivery_on_exit = False

    def __init__(self, project, session_id, env=None, platform=None, timeout=5):
        """创建精确项目客户端。

        @param project: 项目根。
        @param session_id: 原生会话 UUID。
        @param env: 可注入的隔离宿主环境。
        @param platform: 平台覆盖，仅供能力分支测试。
        @param timeout: 原生 RPC 超时秒数。
        @return: None。
        """
        self.platform = platform or sys.platform
        if self.platform not in ("linux", "win32") or (self.platform == "win32" and sys.platform != "win32"):
            raise ValueError("Codex 后台确认需要 Linux/WSL 或原生 Windows 的共享服务")
        self.project = Path(project).resolve(strict=True)
        self.session_id = confirmation_id(session_id)
        self.store = ConfirmationStore(self.project, self.session_id, "codex")
        environment = os.environ if env is None else env
        self.home = Path(environment.get("CODEX_HOME") or (Path.home() / ".codex")).resolve(strict=True)
        self.executable = None
        if self.platform == "win32":
            native = (self.store.read() or {}).get("native") or {}
            if native.get("project") != str(self.project) or native.get("sessionId") != self.session_id or not native.get("hookProcess"):
                raise ValueError("缺少 Windows Codex 原生输入 Hook，保持人工确认")
            # 使用真实 Hook 祖先进程的映像，避免在项目目录或 PATH 中挑选代理程序。
            process = open_identity(native["hookProcess"])
            try:
                self.executable = Path(process.identity["exe"]).resolve(strict=True)
                if self.executable.name.lower() != "codex.exe":
                    raise ValueError("Windows Codex 原生 Hook 映像不匹配")
            finally:
                process.close()
        self.transport = CodexTransport(self.home / "app-server-control/app-server-control.sock", timeout, self._notification,
                                        proxy=self.executable, env=environment)
        self.rollout = None
        self.session_log = SessionLog()
        self.log_start = None
        self.prepare_turn = None
        self.humans = []
        self.seen = OrderedDict()
        self.window_fd = None
        self.window = None
        self.checked_at = 0

    def _notification(self, packet):
        params = packet.get("params") or {}
        item = params.get("item") or {}
        if (params.get("threadId") == self.session_id and packet.get("method") in ("item/started", "item/completed")
                and item.get("type") == "userMessage" and not wake_id(item) and item.get("id") not in self.seen):
            identifier = confirmation_id(item.get("id"))
            self.seen[identifier] = True
            self.humans.append(identifier)
            while len(self.seen) > 4096:
                self.seen.popitem(last=False)

    def connect(self):
        """连接已有原生服务。

        @return: None。
        """
        self.transport.connect()

    def rpc(self, method, params):
        """调用精确原生方法。

        @param method: 原生方法名。
        @param params: 准确参数。
        @return: 原生结果对象。
        """
        return self.transport.rpc(method, params)

    def prepare_snapshot(self):
        """从当前原生 Hook 对应的回合建立本次观察起点。

        @return: 当前回合的来源证据和冻结起点。
        """
        return self._snapshot(preparing=True)

    def snapshot(self):
        """被动读取精确线程及消息来源，观察前台退出。

        @return: 人工基线、当前回合及内部唤醒快照。
        """
        return self._snapshot(preparing=False)

    def _snapshot(self, preparing):
        if self.window_fd is not None:
            if self._frontend_exited():
                raise ValueError("frontend_exited")
            if time.monotonic() - self.checked_at >= 0.25:
                if self.platform == "win32":
                    self._check_windows_binding(self.window)
                else:
                    check_binding(self.window)
                self.checked_at = time.monotonic()
        thread = self.rpc("thread/read", {"threadId": self.session_id, "includeTurns": False})["thread"]
        if thread.get("id") != self.session_id or Path(thread["cwd"]).resolve(strict=True) != self.project:
            raise ValueError("Codex 目标会话或项目目录不匹配")
        if thread.get("archived") or (thread.get("status") or {}).get("type") == "systemError":
            raise ValueError("Codex 目标线程处于错误状态")
        state = self.store.read() or {}
        request = state.get("request") or {}
        file = self._rollout_path(thread.get("path"))
        if preparing:
            native = state.get("native") or {}
            if (native.get("version") != 2 or native.get("project") != str(self.project)
                    or native.get("sessionId") != self.session_id or not native.get("turnId")):
                raise ValueError("当前会话尚未收到确认输入 Hook，保持人工确认")
            turn_id = confirmation_id(native["turnId"])
            if self.prepare_turn != turn_id:
                def matches(entry):
                    event = entry.get("payload")
                    return (entry.get("type") == "event_msg" and isinstance(event, dict)
                            and event.get("type") in ("task_started", "turn_started")
                            and event.get("turn_id") == turn_id and event.get("thread_id") in (None, self.session_id))

                start = SessionLog.locate(file, turn_id, matches)
                prior = ({key: request.get(key) for key in ("id", "baselineMessageId")}
                         if request.get("state") == "consumed" else {})
                self._resume_rollout(file, start, turn_id, prior=prior)
                source = self._read_rollout(file)
                if source.get("turn") != {"id": turn_id, "status": "inProgress"} or not source.get("inputAnchor"):
                    raise ValueError("Codex 缺少可核验的当前回合输入")
                identifier, internal = source["inputAnchor"]

                def matches_input(entry):
                    event = entry.get("payload")
                    if (entry.get("type") != "event_msg" or not isinstance(event, dict)
                            or event.get("type") != "item_completed" or event.get("turn_id") != turn_id
                            or event.get("thread_id") not in (None, self.session_id)):
                        return False
                    item = event.get("item")
                    return (isinstance(item, dict) and item.get("type") == "UserMessage" and item.get("id") == identifier
                            and self._wake_id(item) == internal)

                # 同回合在 prepare 之前的插话是基线；从其完整记录之后观察新的输入。
                self._resume_rollout(file, SessionLog.locate(file, identifier, matches_input), turn_id,
                                     baseline=source["humanId"])
                self.prepare_turn = turn_id
            record = self._read_rollout(file)
            if (record.get("base") is None or record.get("newHuman") or record.get("rolledBack")
                    or record.get("turn") != {"id": turn_id, "status": "inProgress"}):
                raise ValueError("Codex 当前回合或人工输入已变化，或缺少可核验的当前回合输入")
        elif request:
            start = (request.get("contract") or {}).get("codexLogStart")
            if not start:
                raise ValueError("Codex 请求缺少当前确认的记录起点，保持人工确认")
            if self.log_start != start:
                self._resume_rollout(file, start, request["originTurnId"], baseline=confirmation_id(request["baselineMessageId"]))
            self.prepare_turn = None
            record = self._read_rollout(file)
        else:
            record = self._read_rollout(file)
        if not record["humanId"]:
            raise ValueError("Codex 会话没有可核验的人工消息基线")
        self.seen[record["humanId"]] = True
        humans, self.humans = self.humans, []
        wake = record["wake"]
        if wake and wake["turnId"] != (record["turn"] or {}).get("id"):
            wake = None
        if record.get("newHuman"):
            humans = [*humans, record["newHuman"]]
        return {"humanId": record["humanId"], "humanEvents": humans, "turn": record["turn"],
                "status": thread.get("status"), "wake": wake, "logObservation": self.session_log.observation,
                "codexLogStart": self.log_start, "prepareBaseline": record.get("base")}

    def read_rollout(self, filename):
        """按文件身份增量读取原生 JSONL，保留未完成行。

        @param filename: thread/read 返回的准确路径。
        @return: 有界消息来源和最终正文缓存。
        """
        return self._read_rollout(self._rollout_path(filename))

    def _rollout_path(self, filename):
        if not isinstance(filename, str) or not Path(filename).name.endswith("-" + self.session_id + ".jsonl"):
            raise ValueError("Codex 未提供精确会话记录路径")
        file = Path(filename)
        try:
            file.resolve(strict=True).relative_to(self.home / "sessions")
        except ValueError:
            raise ValueError("Codex 会话记录路径越界")
        return file

    def _resume_rollout(self, file, start, turn_id, baseline=None, prior=None):
        # 首条身份记录独立于当前回合；只读这一行，保留线程身份约束。
        if start["identity"][0] != str(file):
            raise ValueError("Codex 会话记录路径已变化")
        with file.open("rb") as stream:
            info = os.fstat(stream.fileno())
            if [info.st_dev, info.st_ino] != start["identity"][1:]:
                raise ValueError("Codex 会话记录在打开时已替换")
            line = stream.readline(RECORD_BYTES + 1)
        if not line.endswith(b"\n") or len(line) > RECORD_BYTES:
            raise ValueError("Codex 缺少原生会话身份记录")
        header = json.loads(line)
        if header.get("type") != "session_meta" or (header.get("payload") or {}).get("id") != self.session_id:
            raise ValueError("Codex 会话记录标识不符")
        self.session_log.resume(start, {"verified": True, "humanId": baseline, "base": baseline,
                                        "anchorTurn": turn_id, "prior": prior or {}, "findingInput": baseline is None,
                                        "turn": {"id": turn_id, "status": "inProgress"}, "wake": None,
                                        "displays": OrderedDict()})
        self.log_start = start

    def _read_rollout(self, file):
        initial = {"verified": False, "humanId": None, "turn": None, "wake": None, "displays": OrderedDict()}
        record, _ = self.session_log.read(file, initial, self._consume_rollout)
        if not record["verified"]:
            raise ValueError("Codex 缺少原生会话身份记录")
        if record.get("rolledBack"):
            raise ValueError("Codex 会话在本次观察期间已回滚，保持人工确认")
        self.rollout = record
        return record

    @staticmethod
    def _wake_id(item):
        return wake_id({"type": "userMessage", "content": item.get("content"), "clientId": item.get("client_id")})

    def _consume_rollout(self, record, entry):
        if entry.get("type") == "session_meta":
            if (entry.get("payload") or {}).get("id") != self.session_id:
                raise ValueError("Codex 会话记录标识不符")
            record["verified"] = True
        if entry.get("type") != "event_msg":
            return
        event = entry.get("payload") or {}
        if event.get("thread_id") and event["thread_id"] != self.session_id:
            raise ValueError("Codex 记录中混入其他会话事件")
        kind = event.get("type")
        if kind in ("task_started", "turn_started"):
            record["turn"] = {"id": event.get("turn_id"), "status": "inProgress"}
        if kind in ("task_complete", "turn_complete") and event.get("turn_id") == (record["turn"] or {}).get("id"):
            record["turn"]["status"] = "completed"
        if kind == "turn_aborted" and (not event.get("turn_id") or event.get("turn_id") == (record["turn"] or {}).get("id")):
            record["turn"] = {"id": (record["turn"] or {}).get("id"), "status": "interrupted"}
        if kind == "thread_rolled_back":
            record.update(humanId=None, turn=None, wake=None)
            record["displays"].clear()
            if record.get("anchorTurn"):
                record["rolledBack"] = True
        if kind != "item_completed":
            return
        item = event.get("item") or {}
        if item.get("type") == "UserMessage":
            internal = self._wake_id(item)
            if internal:
                record["wake"] = {"requestId": internal, "turnId": event.get("turn_id")}
                if record.get("anchorTurn") and record.get("base") is None:
                    prior = record["prior"]
                    if event.get("turn_id") != record["anchorTurn"] or prior.get("id") != internal:
                        raise ValueError("Codex 当前回合的队列唤醒来源不符")
                    record["base"] = record["humanId"] = confirmation_id(prior.get("baselineMessageId"))
                    record["inputAnchor"] = (confirmation_id(item.get("id")), internal)
            else:
                identifier = confirmation_id(item.get("id"))
                if record.get("anchorTurn"):
                    if record.get("findingInput"):
                        if event.get("turn_id") != record["anchorTurn"]:
                            raise ValueError("Codex 当前回合缺少可核验的人工输入")
                        record["base"] = identifier
                        record["inputAnchor"] = (identifier, None)
                    elif record["base"] != identifier:
                        record.setdefault("newHuman", identifier)
                record["humanId"] = identifier
        if not record.get("findingInput") and item.get("type") == "AgentMessage" and item.get("phase") == "final_answer":
            parts = item.get("content")
            if not isinstance(parts, list) or any(part.get("type") != "Text" or not isinstance(part.get("text"), str) for part in parts):
                raise ValueError("Codex 最终正文结构不可核验")
            turn_id = event.get("turn_id")
            previous = record["displays"].get(turn_id, "")
            if previous is not None:
                text = previous + "".join(part["text"] for part in parts)
                # 超大历史正文不应阻塞后续回合；该回合自身仍无法通过完整展示核验。
                record["displays"][turn_id] = text if len(text.encode("utf-8")) <= DISPLAY_BYTES else None
            while len(record["displays"]) > 8:
                record["displays"].popitem(last=False)

    def assistant_text(self, turn_id):
        """读取原展示回合的最终正文。

        @param turn_id: 原展示回合。
        @return: 完整正文。
        """
        snapshot = self.snapshot()
        self.humans[:0] = snapshot.get("humanEvents", [])
        display = self.rollout["displays"].get(turn_id)
        if display is None:
            raise ValueError("Codex 原始最终展示缺失或超过核验预算")
        return display

    def _origin(self):
        result = self.rpc("mcpServerStatus/list", {"threadId": self.session_id, "serverName": "codex_tui",
                                                  "detail": "toolsAndAuthOnly", "limit": 5})
        servers = [value for value in result.get("data", []) if value.get("name") == "codex_tui"]
        if (result.get("nextCursor") or len(servers) != 1 or servers[0].get("runtimeStatus") != "connected"
                or not re.fullmatch(r"http://(?:127\.0\.0\.1|\[::1\]):[1-9][0-9]{0,4}", servers[0].get("httpOrigin", ""))):
            raise ValueError("当前会话缺少唯一可核验的原生 TUI 服务，保持人工确认")
        return servers[0]["httpOrigin"]

    def _coverage(self):
        result = self.rpc("thread/loaded/list", {"limit": 1000})
        threads = result.get("data")
        if result.get("nextCursor") or not isinstance(threads, list) or not 0 < len(threads) <= 1000:
            raise ValueError("无法核验全部已加载原生会话")
        origins = set()
        for thread_id in threads:
            current = self.rpc("mcpServerStatus/list", {"threadId": thread_id, "serverName": "codex_tui", "detail": "toolsAndAuthOnly", "limit": 5})
            if current.get("nextCursor"):
                raise ValueError("原生 TUI 来源超出核验预算")
            origins.update(value["httpOrigin"] for value in current.get("data", [])
                           if value.get("name") == "codex_tui" and value.get("runtimeStatus") == "connected" and value.get("httpOrigin"))
        if self.platform == "win32":
            identities = [self._windows_binding(origin)["identity"] for origin in origins]
            owners = {item["pid"] for item in identities}
            if not owners or len(owners) != len(origins):
                raise ValueError("Windows Codex 前台归属不唯一")
            check_listener_coverage(owners, self.executable, {Path(item["exe"]) for item in identities})
        else:
            check_frontend_coverage(list(origins))

    def _windows_binding(self, origin):
        from flower_tui_lifetime import parse_origin

        host, port = parse_origin(origin)
        return {"version": 1, "platform": "win32", "origin": origin,
                "identity": listener_identity(host, port, self.executable)}

    def _check_windows_binding(self, binding):
        if binding != self._windows_binding(binding["origin"]):
            raise ValueError("原 Windows Codex 前台或监听归属已变化")

    def _frontend_exited(self):
        if self.platform == "win32":
            return self.window_fd.exited()
        poll = select.poll()
        poll.register(self.window_fd, select.POLLIN)
        return bool(poll.poll(0))

    def capture_window(self, turn_id):
        """从当前输入 Hook 和实际监听者冻结前台身份。

        @param turn_id: 当前活动回合。
        @return: 原窗口出生身份。
        """
        native = (ConfirmationStore(self.project, self.session_id, "codex").read() or {}).get("native") or {}
        if (native.get("version") != 2 or native.get("project") != str(self.project)
                or native.get("sessionId") != self.session_id or not turn_id or native.get("turnId") != turn_id):
            raise ValueError("当前会话尚未收到确认输入 Hook，重开原生客户端后使用；本次保持人工确认")
        origin = self._origin()
        binding = self._windows_binding(origin) if self.platform == "win32" else bind_origin(origin)
        if self._origin() != origin:
            raise ValueError("准备期间前台窗口绑定已改变")
        self._coverage()
        return binding

    def verify_window(self, binding):
        """复核冻结窗口和全部前台归属。

        @param binding: 原请求的窗口身份。
        @return: None；任何漂移均拒绝。
        """
        if self._origin() != binding.get("origin"):
            raise ValueError("原前台窗口绑定已改变")
        if self.platform == "win32":
            self._check_windows_binding(binding)
            handle = open_identity(binding["identity"])
            handle.close()
        else:
            fd = open_binding(binding)
            os.close(fd)
        if self._origin() != binding["origin"]:
            raise ValueError("核验期间前台窗口绑定已改变")
        self._coverage()

    def observe_window(self, binding):
        """为 worker 持有自己的内核退出句柄。

        @param binding: 冻结窗口。
        @return: None。
        """
        self.verify_window(binding)
        self.window_fd = open_identity(binding["identity"]) if self.platform == "win32" else open_binding(binding)
        self.window = binding

    def add(self, request_id):
        """向同一原生线程投递一次唤醒。

        @param request_id: 请求 UUID。
        @return: 原生 queuedSubmission 回执。
        """
        result = self.rpc("thread/queue/add", {"threadId": self.session_id, "clientUserMessageId": request_id,
                                              "input": [{"type": "text", "text": wake_message(request_id)}]})
        queued = result.get("queuedSubmission") or {}
        if not queued.get("id"):
            raise ValueError("Codex 队列未返回有效回执，结果未知")
        return queued

    def remove(self, queue_id):
        """撤回本请求精确队列项。

        @param queue_id: 原生队列项标识。
        @return: deleted 或 not_pending。
        """
        try:
            self.rpc("thread/queue/delete", {"threadId": self.session_id, "queuedSubmissionId": queue_id})
            return "deleted"
        except RpcError as error:
            if error.code == -32600 and "queued submission not found" in str(error).lower():
                return "not_pending"
            raise

    def close(self):
        """释放自己的连接与进程退出句柄。

        @return: None。
        """
        if self.window_fd is not None:
            if self.platform == "win32":
                self.window_fd.close()
            else:
                os.close(self.window_fd)
            self.window_fd = None
        self.transport.close()
