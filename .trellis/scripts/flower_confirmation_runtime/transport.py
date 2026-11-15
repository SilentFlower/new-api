"""仅连接 Codex 已有 Unix socket 的 WebSocket/JSON-RPC 标准库客户端。

帧处理依据 RFC 6455 第 4、5 节；该传输固定用于原生本机观察连接。
"""

from __future__ import annotations

import base64
import hashlib
import json
import os
import queue
import socket
import struct
import subprocess
import threading
import time
from pathlib import Path


MAX_PAYLOAD = 16 * 1024 * 1024


class RpcError(ValueError):
    """保留原生 JSON-RPC 错误码，供精确撤回结果判断。"""

    def __init__(self, method, error):
        """保存方法和原生失败原因。

        @param method: 已调用方法。
        @param error: 原生错误对象。
        @return: None。
        """
        super().__init__("Codex " + method + " 失败：" + str(error.get("message")))
        self.code = error.get("code")


class _WindowsProxy:
    """用自有 Codex 原生代理转发已有本机 socket 的字节流。"""

    def __init__(self, executable, path, env, timeout):
        endpoint = Path(path)
        if (not endpoint.is_absolute() or endpoint.drive.startswith("\\\\") or endpoint.is_symlink()
                or endpoint.parent.resolve() != endpoint.parent):
            raise ValueError("Windows Codex 本机控制端点路径无法核验，保持人工确认")
        # Windows 托管控制端点可以连接，却未必能由普通文件查询观察到。
        self.timeout = timeout
        self.incoming = queue.Queue(maxsize=16)
        self.stopped = threading.Event()
        self.process = subprocess.Popen([str(executable), "app-server", "proxy", "--sock", str(endpoint)],
                                        env=env, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                        stderr=subprocess.DEVNULL, bufsize=0, creationflags=subprocess.CREATE_NO_WINDOW)
        self.reader = threading.Thread(target=self._receive, daemon=True)
        self.reader.start()

    def _receive(self):
        while not self.stopped.is_set():
            try:
                chunk = self.process.stdout.read(65536)
            except (OSError, ValueError):
                chunk = b""
            while not self.stopped.is_set():
                try:
                    self.incoming.put(chunk, timeout=0.1)
                    break
                except queue.Full:
                    continue
            if not chunk:
                break

    def settimeout(self, timeout):
        self.timeout = timeout

    def recv(self, size):
        try:
            return self.incoming.get(timeout=self.timeout)
        except queue.Empty as error:
            raise socket.timeout("Windows Codex 本机代理读取超时") from error

    def sendall(self, data):
        result = queue.Queue(maxsize=1)

        def write():
            try:
                remaining = memoryview(data)
                while remaining:
                    written = self.process.stdin.write(remaining)
                    if not written:
                        raise ConnectionError("Windows Codex 本机代理断开")
                    remaining = remaining[written:]
                result.put(None)
            except (OSError, ValueError) as error:
                result.put(error)

        threading.Thread(target=write, daemon=True).start()
        try:
            error = result.get(timeout=self.timeout)
        except queue.Empty as error:
            self.close()
            raise TimeoutError("Windows Codex 本机代理写入超时，结果未知") from error
        if error:
            raise error

    def close(self):
        if self.stopped.is_set():
            return
        self.stopped.set()
        try:
            if self.process.poll() is None:
                self.process.kill()
            self.process.wait(timeout=2)
        finally:
            self.process.stdin.close()
            self.process.stdout.close()
            self.reader.join(timeout=1)


class CodexTransport:
    """单连接同步 RPC，写操作超时保持结果未知。"""

    def __init__(self, path, timeout=5, notification=None, *, proxy=None, env=None):
        """创建尚未连接的本机传输。

        @param path: 已有 Unix socket 路径。
        @param timeout: 每次 RPC 的秒数预算。
        @param notification: 可选原生通知回调。
        @param proxy: Windows 现有 Codex 可执行文件；缺省直连 Unix socket。
        @param env: 传给 Windows 代理的精确宿主环境。
        @return: None。
        """
        self.path = str(path)
        self.timeout = timeout
        self.notification = notification or (lambda packet: None)
        self.socket = None
        self.buffer = b""
        self.sequence = 0
        self.proxy = proxy
        self.env = env

    def connect(self):
        """完成 RFC 6455 升级并初始化原生协议。

        @return: None；连接失败时关闭自有 socket。
        """
        try:
            if self.proxy is not None:
                self.socket = _WindowsProxy(self.proxy, self.path, self.env, self.timeout)
            else:
                self.socket = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
                self.socket.settimeout(self.timeout)
                self.socket.connect(self.path)
            key = base64.b64encode(os.urandom(16)).decode("ascii")
            request = ("GET / HTTP/1.1\r\nHost: localhost\r\nUpgrade: websocket\r\n"
                       "Connection: Upgrade\r\nSec-WebSocket-Key: " + key
                       + "\r\nSec-WebSocket-Version: 13\r\n\r\n")
            self.socket.sendall(request.encode("ascii"))
            deadline = time.monotonic() + self.timeout
            while b"\r\n\r\n" not in self.buffer:
                if len(self.buffer) >= 16384:
                    raise ValueError("Codex WebSocket 握手超出预算")
                self.buffer += self._receive(deadline)
            header, self.buffer = self.buffer.split(b"\r\n\r\n", 1)
            lines = header.decode("ascii").split("\r\n")
            fields = {}
            for line in lines[1:]:
                name, value = line.split(":", 1)
                name = name.lower()
                if name in fields:
                    raise ValueError("Codex WebSocket 握手字段重复")
                fields[name] = value.strip()
            expected = base64.b64encode(hashlib.sha1((key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode("ascii")).digest()).decode("ascii")
            if (lines[0].split()[:2] != ["HTTP/1.1", "101"]
                    or fields.get("upgrade", "").lower() != "websocket"
                    or "upgrade" not in [value.strip().lower() for value in fields.get("connection", "").split(",")]
                    or fields.get("sec-websocket-accept") != expected
                    or "sec-websocket-extensions" in fields or "sec-websocket-protocol" in fields):
                raise ValueError("Codex WebSocket 握手无法核验")
            self.rpc("initialize", {"clientInfo": {"name": "flower_confirmation", "version": "1"},
                                    "capabilities": {"experimentalApi": True}})
            self._send_json({"method": "initialized"})
        except BaseException:
            self.close()
            raise

    def _receive(self, deadline):
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise TimeoutError("Codex 原生回执超时，结果未知")
        self.socket.settimeout(remaining)
        try:
            raw = self.socket.recv(65536)
        except socket.timeout:
            raise TimeoutError("Codex 原生回执超时，结果未知")
        if not raw:
            raise ConnectionError("Codex 连接断开")
        return raw

    def _read(self, size, deadline):
        while len(self.buffer) < size:
            self.buffer += self._receive(deadline)
        value, self.buffer = self.buffer[:size], self.buffer[size:]
        return value

    def _send(self, opcode, raw):
        if self.socket is None:
            raise ConnectionError("Codex 尚未连接")
        if len(raw) > MAX_PAYLOAD:
            raise ValueError("Codex 消息超出预算")
        size = len(raw)
        prefix = bytes((0x80 | opcode, 0x80 | (size if size < 126 else 126 if size <= 65535 else 127)))
        extra = b"" if size < 126 else struct.pack("!H" if size <= 65535 else "!Q", size)
        mask = os.urandom(4)
        masked = bytes(value ^ mask[index % 4] for index, value in enumerate(raw))
        self.socket.settimeout(self.timeout)
        self.socket.sendall(prefix + extra + mask + masked)

    def _send_json(self, packet):
        self._send(1, json.dumps(packet, ensure_ascii=False, separators=(",", ":"), allow_nan=False).encode("utf-8"))

    def _message(self, deadline):
        chunks = []
        total = 0
        fragmented = False
        while True:
            first, second = self._read(2, deadline)
            final, opcode = bool(first & 0x80), first & 0x0f
            if first & 0x70 or second & 0x80:
                raise ValueError("Codex WebSocket 帧标志无效")
            size = second & 0x7f
            encoded_size = size
            if size >= 126:
                size = struct.unpack("!H" if size == 126 else "!Q", self._read(2 if size == 126 else 8, deadline))[0]
                if size < (126 if encoded_size == 126 else 65536):
                    raise ValueError("Codex WebSocket 长度编码无效")
            if size > MAX_PAYLOAD or (opcode < 8 and total + size > MAX_PAYLOAD):
                raise ValueError("Codex 消息超出预算")
            if opcode >= 8 and (not final or size > 125 or opcode not in (8, 9, 10)):
                raise ValueError("Codex WebSocket 控制帧无效")
            raw = self._read(size, deadline)
            if opcode == 8:
                raise ConnectionError("Codex 原生连接已关闭")
            if opcode == 9:
                self._send(10, raw)
                continue
            if opcode == 10:
                continue
            if opcode not in (0, 1) or (opcode == 0) != fragmented:
                raise ValueError("Codex WebSocket 分片顺序无效")
            chunks.append(raw)
            total += size
            if final:
                packet = json.loads(b"".join(chunks).decode("utf-8"))
                if not isinstance(packet, dict):
                    raise ValueError("Codex 原生消息不是 JSON 对象")
                return packet
            fragmented = True

    def rpc(self, method, params):
        """调用准确方法并读取唯一回执，期间处理原生通知。

        @param method: 原生 JSON-RPC 方法名。
        @param params: 已核对的参数对象。
        @return: 原生 result；回执未知保持异常。
        """
        self.sequence += 1
        current = self.sequence
        deadline = time.monotonic() + self.timeout
        self._send_json({"id": current, "method": method, "params": params})
        while True:
            packet = self._message(deadline)
            if "method" in packet:
                if "id" in packet:
                    # 观察连接只读宿主状态，审批请求明确交回宿主。
                    self._send_json({"id": packet["id"], "error": {"code": -32601, "message": "观察连接不提供此方法"}})
                else:
                    self.notification(packet)
            elif packet.get("id") == current:
                if "error" in packet:
                    raise RpcError(method, packet["error"])
                return packet.get("result")

    def close(self):
        """关闭本连接，释放自己的文件描述符。

        @return: None。
        """
        if self.socket is not None:
            self.socket.close()
            self.socket = None
