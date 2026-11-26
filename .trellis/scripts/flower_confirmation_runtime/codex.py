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

from flower_tui_lifetime import bind_embedded, bind_origin, check_binding, check_frontend_coverage, open_binding
from .session_log import DISPLAY_BYTES, RECORD_BYTES, SessionLog
from .store import ACTIVE, ConfirmationStore, confirmation_id, native_id, now_ms, text_content, validate_turn_origin, wake_id, wake_message
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
                        "inputSeq": 0, "request": None}
        if (value.get("turnOrigin") or {}).get("turnId") != payload.get("turn_id"):
            value.pop("turnOrigin", None)
        value["native"] = {"version": 2, "project": str(project), "sessionId": session_id,
                           "at": now_ms(), "turnId": payload.get("turn_id")}
        if frontend is not None:
            value["native"]["hookProcess"] = frontend
        request = value.get("request")
        internal = (payload["hook_event_name"] == "UserPromptSubmit" and request
                    and request["state"] in ("dispatching", "queued")
                    and wake_id({"type": "userMessage", "clientId": request["id"], "content": payload.get("prompt")}) == request["id"])
        # Hook 缺少 client_id，仅暂缓精确提醒取消；claim 仍核对原生记录来源。
        if not internal:
            value.pop("turnOrigin", None)
            value["generation"] += 1
            value["inputSeq"] = value.get("inputSeq", 0) + 1
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
        self.prepare_origin = None
        self.prepare_baseline = 0
        self.input_events = 0
        self.humans = []
        self.seen = OrderedDict()
        self.window_fd = None
        self.window = None
        self.checked_at = 0

    def _notification(self, packet):
        params = packet.get("params") or {}
        item = params.get("item") or {}
        if (params.get("threadId") == self.session_id and packet.get("method") in ("item/started", "item/completed")
                and item.get("type") == "userMessage" and isinstance(item.get("id"), str)
                and item["id"] and not wake_id(item) and item["id"] not in self.seen):
            identifier = native_id(item.get("id"))
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

        @return: 输入序号、当前回合及内部唤醒快照。
        """
        return self._snapshot(preparing=False)

    def _thread(self):
        thread = self.rpc("thread/read", {"threadId": self.session_id, "includeTurns": False})["thread"]
        if thread.get("id") != self.session_id or Path(thread["cwd"]).resolve(strict=True) != self.project:
            raise ValueError("Codex 目标会话或项目目录不匹配")
        if thread.get("archived") or (thread.get("status") or {}).get("type") == "systemError":
            raise ValueError("Codex 目标线程处于错误状态")
        return thread

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
        thread = self._thread()
        state = self.store.read() or {}
        request = state.get("request") or {}
        file = self._rollout_path(thread.get("path"))
        native = state.get("native") or {}
        if preparing:
            if (native.get("version") != 2 or native.get("project") != str(self.project)
                    or native.get("sessionId") != self.session_id):
                raise ValueError("当前会话尚未收到确认输入 Hook，保持人工确认")
            saved = state.get("turnOrigin")
            origin = None if isinstance(saved, dict) and saved.get("version") == 1 else validate_turn_origin(saved, "codex", self.session_id)
            if self.prepare_turn is None:
                if origin and origin["inputSeq"] == state.get("inputSeq", 0):
                    if origin.get("hookTurnId") != native.get("turnId"):
                        raise ValueError("Codex 当前回合来源已变化")
                    self.verify_window(origin["windowBinding"])
                    self._resume_rollout(file, origin["logStart"], origin["turnId"], origin["inputSeq"])
                    self._read_rollout(file)
                    SessionLog.verify(origin["logObservation"], self.session_log.observation)
                    self.prepare_origin = {key: value for key, value in origin.items() if key != "logObservation"}
                else:
                    boundary = SessionLog.checkpoint(file)
                    found = {}

                    def current_turn(entry):
                        event = entry.get("payload")
                        if (entry.get("type") == "event_msg" and isinstance(event, dict)
                                and event.get("type") in ("task_started", "turn_started")
                                and event.get("thread_id") in (None, self.session_id)):
                            found.update(event)
                            return True
                        return False

                    start = SessionLog.locate(file, "Codex 当前活动回合", current_turn)
                    if start["offset"] > boundary["offset"]:
                        raise ValueError("准备期间 Codex 当前回合已变化")
                    turn_id = native_id(found.get("turn_id"))
                    attribution = found.get("turn_attribution") or {}
                    if native.get("turnId") not in (None, turn_id) and attribution.get("turn_trigger") != "daemon_recovery":
                        raise ValueError("Codex 当前回合与宿主输入事件不符")
                    self._resume_rollout(file, start, turn_id, state.get("inputSeq", 0))
                    current = self._read_rollout(file)["turn"]
                    if current != {"id": turn_id, "status": "inProgress"}:
                        raise ValueError("Codex 当前回合未处于活动状态")
                    self._resume_rollout(file, boundary, turn_id, state.get("inputSeq", 0))
                    self.prepare_origin = {"version": 2, "platform": "codex", "sessionId": self.session_id,
                                           "turnId": turn_id, "inputSeq": self.prepare_baseline,
                                           "kind": "native", "hookTurnId": native.get("turnId"), "logStart": boundary}
                self.prepare_turn = self.prepare_origin["turnId"]
            record = self._read_rollout(file)
            if (record.get("inputCount") or record.get("rolledBack")
                    or record.get("turn") != {"id": self.prepare_turn, "status": "inProgress"}):
                raise ValueError("Codex 当前回合或人工输入已变化")
        elif request:
            start = (request.get("contract") or {}).get("codexLogStart")
            if not start:
                raise ValueError("Codex 请求缺少当前确认的记录起点，保持人工确认")
            if self.log_start != start:
                self._resume_rollout(file, start, request["originTurnId"], request.get("inputSeq", state.get("inputSeq", 0)))
            self.prepare_turn = None
            record = self._read_rollout(file)
        else:
            record = self._read_rollout(file)
        humans, self.humans = self.humans, []
        self.input_events += len(humans)
        wake = record["wake"]
        if wake and wake["turnId"] != (record["turn"] or {}).get("id"):
            wake = None
        sequence = max(state.get("inputSeq", 0), self.prepare_baseline + max(record.get("inputCount", 0), self.input_events))
        return {"inputSeq": sequence, "turn": record["turn"], "hookTurnId": native.get("turnId"),
                "status": thread.get("status"), "wake": wake, "logObservation": self.session_log.observation,
                "codexLogStart": self.log_start, "prepareBaseline": self.prepare_baseline,
                "turnOrigin": self.prepare_origin if preparing else None}

    def read_rollout(self, filename):
        """按文件身份增量读取原生 JSONL，保留未完成行。

        @param filename: thread/read 返回的准确路径。
        @return: 有界消息来源和最终正文缓存。
        """
        return self._read_rollout(self._rollout_path(filename))

    def _rollout_path(self, filename):
        if not isinstance(filename, str) or not filename:
            raise ValueError("Codex 未提供精确会话记录路径")
        file = Path(filename)
        try:
            file.resolve(strict=True).relative_to(self.home / "sessions")
        except ValueError:
            raise ValueError("Codex 会话记录路径越界")
        return file

    def _resume_rollout(self, file, start, turn_id, sequence):
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
        self.session_log.resume(start, {"verified": True, "inputCount": 0,
                                        "anchorTurn": turn_id, "turn": {"id": turn_id, "status": "inProgress"},
                                        "wake": None, "displays": OrderedDict()})
        self.log_start = start
        self.prepare_baseline = sequence
        self.input_events = 0
        self.humans = []

    def _read_rollout(self, file):
        initial = {"verified": False, "inputCount": 0, "turn": None, "wake": None, "displays": OrderedDict()}
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
        if not isinstance(entry, dict):
            return
        if entry.get("type") == "session_meta":
            if not record["verified"] and (entry.get("payload") or {}).get("id") != self.session_id:
                raise ValueError("Codex 会话记录标识不符")
            record["verified"] = True
        if entry.get("type") != "event_msg":
            return
        event = entry.get("payload") or {}
        if not isinstance(event, dict):
            return
        if event.get("thread_id") and event["thread_id"] != self.session_id:
            return
        kind = event.get("type")
        if kind in ("task_started", "turn_started"):
            record["turn"] = {"id": event.get("turn_id"), "status": "inProgress"}
        if kind in ("task_complete", "turn_complete") and event.get("turn_id") == (record["turn"] or {}).get("id"):
            record["turn"]["status"] = "completed"
        if kind == "turn_aborted" and (not event.get("turn_id") or event.get("turn_id") == (record["turn"] or {}).get("id")):
            record["turn"] = {"id": (record["turn"] or {}).get("id"), "status": "interrupted"}
        if kind == "thread_rolled_back":
            record.update(turn=None, wake=None)
            record["displays"].clear()
            record["rolledBack"] = True
        if kind != "item_completed":
            return
        item = event.get("item") or {}
        if not isinstance(item, dict):
            return
        if item.get("type") == "UserMessage":
            internal = self._wake_id(item)
            if internal:
                record["wake"] = {"requestId": internal, "turnId": event.get("turn_id")}
            else:
                record["inputCount"] = record.get("inputCount", 0) + 1
        if item.get("type") == "AgentMessage" and item.get("phase") == "final_answer":
            text = text_content(item.get("content"))
            turn_id = event.get("turn_id")
            previous = record["displays"].get(turn_id, "")
            if previous is not None:
                text = previous + text
                # 超大历史正文不应阻塞后续回合；该回合自身仍无法通过完整展示核验。
                record["displays"][turn_id] = text if len(text.encode("utf-8")) <= DISPLAY_BYTES else None
            while len(record["displays"]) > 8:
                record["displays"].popitem(last=False)

    def assistant_text(self, turn_id):
        """读取原展示回合的最终正文。

        @param turn_id: 原展示回合。
        @return: 完整正文。
        """
        self.snapshot()
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

    def _coverage(self, binding):
        result = self.rpc("thread/loaded/list", {"limit": 1000})
        threads = result.get("data")
        embedded = "writerLock" in binding
        if (result.get("nextCursor") or not isinstance(threads, list) or len(threads) > 1000
                or (self.session_id in threads) == embedded):
            raise ValueError("无法核验全部已加载原生会话")
        origins = {binding["origin"]} if embedded else set()
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
            check_frontend_coverage(list(origins), home=self.home, session_id=self.session_id,
                                    control=self.transport.control_connection())

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
                or native.get("sessionId") != self.session_id or not turn_id):
            raise ValueError("当前会话尚未收到确认输入 Hook，重开原生客户端后使用；本次保持人工确认")
        if self.platform == "linux" and (self._thread().get("status") or {}).get("type") == "notLoaded":
            binding = bind_embedded(self.home, self.session_id, self.transport.control_connection())
            self._coverage(binding)
            return binding
        origin = self._origin()
        binding = self._windows_binding(origin) if self.platform == "win32" else bind_origin(origin)
        if self._origin() != origin:
            raise ValueError("准备期间前台窗口绑定已改变")
        self._coverage(binding)
        return binding

    def verify_window(self, binding):
        """复核冻结窗口和全部前台归属。

        @param binding: 原请求的窗口身份。
        @return: None；任何漂移均拒绝。
        """
        if self.platform == "linux" and "writerLock" in binding:
            if ((self._thread().get("status") or {}).get("type") != "notLoaded"
                    or bind_embedded(self.home, self.session_id, self.transport.control_connection()) != binding):
                raise ValueError("原内置 Codex 窗口或会话写锁已改变")
            self._coverage(binding)
            return
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
        self._coverage(binding)

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
