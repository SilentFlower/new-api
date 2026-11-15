"""有界确认状态与跨进程目录锁，沿用原 version 2 持久化契约。"""

from __future__ import annotations

import errno
import hashlib
import json
import os
import re
import shutil
import stat
import sys
import time
from pathlib import Path
from uuid import uuid4


ACTIVE = frozenset(("prepared", "waiting", "dispatching", "queued"))
STATES = ACTIVE | {"cancelled", "failed", "delivery_unknown", "consumed"}
MAX_BYTES = 1024 * 1024
STATE_BYTES = 2 * 1024 * 1024
DISPLAY_BUDGET = 300
CLAIM_BUDGET = 300
UUID = re.compile(r"[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}", re.I)


def confirmation_id(value):
    """校验会话、请求和回执标识。

    @param value: 待校验 UUID。
    @return: 原始标识；无效时抛出异常。
    """
    if not isinstance(value, str) or not UUID.fullmatch(value):
        raise ValueError("必须提供精确的 UUID 会话或请求标识")
    return value


def wake_message(request_id, platform="codex"):
    """生成只唤醒原工作流的完整内部消息。

    @param request_id: 精确请求 UUID。
    @param platform: codex 或 claude。
    @return: 需要结合原生来源核验的提醒正文。
    """
    confirmation_id(request_id)
    if platform not in ("codex", "claude"):
        raise ValueError("未知确认平台")
    return ("[flower-confirmation:v2:" + request_id + "]\n后台确认已到期。"
            "先按 .trellis/scripts/confirmation.md 定位本项目的 Python 入口，再执行 claim --platform "
            + platform + " --request " + request_id + "。只有返回 claimed=true 才按返回的 action 和冻结 context 进入原流程；"
            "必须继续执行原有范围检查与失败恢复。claimed=false 或失败时，停止旧动作，"
            "按协议读取当前 status 并恢复当前流程。已有有效等待予以保留；需要新等待时自动重新准备、完整展示并计时。"
            "不得仅因旧提醒失效要求人工再确认，也不得重试领取旧请求或执行旧动作。")


def wake_id(item):
    """核对内部正文与 Codex 原生 clientId。

    @param item: 原生 userMessage。
    @return: 匹配的请求 UUID，普通消息返回 None。
    """
    content = item.get("content")
    if item.get("type") != "userMessage" or not isinstance(content, list) or len(content) != 1:
        return None
    part = content[0]
    text = part.get("text") if isinstance(part, dict) and part.get("type") == "text" else None
    match = re.match(r"^\[flower-confirmation:v2:([0-9a-f-]{36})\]", text, re.I) if isinstance(text, str) else None
    if match and UUID.fullmatch(match[1]) and item.get("clientId") == match[1] and text == wake_message(match[1]):
        return match[1]
    return None


def now_ms():
    """返回兼容原状态的毫秒墙钟时间。

    @return: 当前 Unix 毫秒数。
    """
    return int(time.time() * 1000)


def message_hash(message):
    """计算动作正文摘要。

    @param message: 待冻结的动作名称。
    @return: SHA-256 十六进制摘要。
    """
    return hashlib.sha256(message.encode("utf-8")).hexdigest()


def _directory(path, create=False):
    if path.parent != path:
        _directory(path.parent, create)
    try:
        info = path.lstat()
    except FileNotFoundError:
        if not create:
            return
        try:
            path.mkdir(mode=0o700)
        except FileExistsError:
            _directory(path)
        return
    if not stat.S_ISDIR(info.st_mode) or stat.S_ISLNK(info.st_mode):
        raise ValueError("确认状态目录必须是普通目录")


def _pairs(items):
    result = {}
    for key, value in items:
        if key in result:
            raise ValueError("确认 JSON 存在重复字段")
        result[key] = value
    return result


def read_json(file, limit=MAX_BYTES):
    """读取有界普通 JSON 文件，缺失保持空状态。

    @param file: Path 文件路径。
    @param limit: 最大字节数。
    @return: JSON 对象或 None。
    """
    _directory(file.parent)
    try:
        info = file.lstat()
        if not stat.S_ISREG(info.st_mode) or info.st_size > limit:
            raise ValueError("确认状态必须是有界普通文件")
        with file.open("rb") as stream:
            raw = stream.read(limit + 1)
        if len(raw) > limit:
            raise ValueError("确认状态超出大小限制")
        return json.loads(raw.decode("utf-8"), object_pairs_hook=_pairs)
    except FileNotFoundError:
        return None


def _retry_windows_permission(operation):
    # 竞争读取会短暂占用 Windows 文件；仅重试文件操作，避免重放已经执行的状态转换。
    deadline = time.monotonic() + 1
    while True:
        try:
            return operation()
        except OSError as error:
            if (sys.platform != "win32" or error.errno not in (errno.EACCES, errno.EPERM)
                    or time.monotonic() >= deadline):
                raise
            time.sleep(0.01)


def _write_json(file, value, limit=MAX_BYTES):
    raw = json.dumps(value, ensure_ascii=False, separators=(",", ":"), allow_nan=False).encode("utf-8")
    if len(raw) > limit:
        raise ValueError("确认状态超出大小限制")
    read_json(file, limit)
    temporary = file.with_name(file.name + "." + str(uuid4()) + ".tmp")
    try:
        fd = os.open(str(temporary), os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
        with os.fdopen(fd, "wb") as stream:
            stream.write(raw)
            stream.flush()
            os.fsync(stream.fileno())
        _retry_windows_permission(lambda: os.replace(str(temporary), str(file)))
    finally:
        try:
            temporary.unlink()
        except FileNotFoundError:
            pass


def _pid_alive(pid):
    if sys.platform == "win32":
        # Windows 用查询句柄判定退出，避免 os.kill 的非 POSIX 信号语义。
        from ctypes import POINTER, WinDLL, byref, get_last_error
        from ctypes.wintypes import BOOL, DWORD, HANDLE
        kernel = WinDLL("kernel32", use_last_error=True)
        kernel.OpenProcess.argtypes = (DWORD, BOOL, DWORD)
        kernel.OpenProcess.restype = HANDLE
        kernel.GetExitCodeProcess.argtypes = (HANDLE, POINTER(DWORD))
        kernel.GetExitCodeProcess.restype = BOOL
        kernel.CloseHandle.argtypes = (HANDLE,)
        kernel.CloseHandle.restype = BOOL
        handle = kernel.OpenProcess(0x1000, False, pid)
        if not handle:
            return get_last_error() != 87
        try:
            code = DWORD()
            return not kernel.GetExitCodeProcess(handle, byref(code)) or code.value == 259
        finally:
            kernel.CloseHandle(handle)
    try:
        os.kill(pid, 0)
    except ProcessLookupError:
        return False
    except PermissionError:
        return True
    return True


class ConfirmationStore:
    """精确项目、平台、会话的原子状态存储。"""

    def __init__(self, project, session_id, platform):
        """定位 version 2 状态。

        @param project: 项目根目录。
        @param session_id: 精确会话 UUID。
        @param platform: codex 或 claude。
        @return: None。
        """
        if platform not in ("codex", "claude"):
            raise ValueError("未知确认平台")
        self.project = Path(project).resolve(strict=True)
        self.session_id = confirmation_id(session_id)
        self.platform = platform
        self.directory = self.project / ".trellis/.runtime" / ("confirmation-" + platform)
        self.file = self.directory / (session_id + ".json")

    def read(self):
        """读取并核对状态身份和代次。

        @return: 已校验状态或 None；查询保持零写入。
        """
        value = read_json(self.file, STATE_BYTES)
        if value is None:
            return None
        if (not isinstance(value, dict) or type(value.get("version")) is not int or value["version"] != 2
                or value.get("sessionId") != self.session_id or type(value.get("generation")) is not int
                or not 0 <= value["generation"] <= 9007199254740991):
            raise ValueError("确认状态版本、会话或结构无效")
        request = value.get("request")
        if request:
            if (not isinstance(request, dict) or request.get("state") not in STATES
                    or type(request.get("generation")) is not int or request["generation"] > value["generation"]):
                raise ValueError("确认请求结构无效")
            confirmation_id(request.get("id"))
        elif not isinstance(value.get("native"), dict):
            raise ValueError("确认状态缺少原生来源")
        return value

    def update(self, mutate, create=True):
        """在目录锁内执行同步状态转换。

        @param mutate: 接收旧状态并返回完整新状态的函数。
        @param create: 是否允许创建缺失状态。
        @return: 转换后的状态或 None。
        """
        if not create and self.read() is None:
            return None
        _directory(self.directory, create)
        lock = self.file.with_name(self.file.name + ".lock")
        token = str(uuid4())
        deadline = time.monotonic() + 1
        while True:
            try:
                lock.mkdir(mode=0o700)
            except OSError as error:
                if sys.platform == "win32" and error.errno in (errno.EACCES, errno.EPERM):
                    if time.monotonic() >= deadline:
                        raise
                elif error.errno == errno.EEXIST:
                    self._reap(lock, token)
                    if time.monotonic() >= deadline:
                        raise ValueError("确认状态锁繁忙")
                else:
                    raise
                time.sleep(0.01)
                continue
            try:
                _write_json(lock / "owner.json", {"pid": os.getpid(), "token": token})
            except BaseException:
                lock.rmdir()
                raise
            break
        try:
            value = mutate(self.read())
            if value is not None:
                _write_json(self.file, value, STATE_BYTES)
            return value
        finally:
            owner = read_json(lock / "owner.json")
            if owner and owner.get("token") == token:
                _retry_windows_permission(lambda: shutil.rmtree(str(lock)))

    def _reap(self, lock, token):
        try:
            info = lock.lstat()
            if not stat.S_ISDIR(info.st_mode) or stat.S_ISLNK(info.st_mode):
                raise ValueError("确认状态锁必须是普通目录")
            owner = read_json(lock / "owner.json")
            pid = owner.get("pid") if isinstance(owner, dict) else None
            dead = type(pid) is int and pid > 0 and not _pid_alive(pid)
            if not dead and (owner or time.time() - info.st_mtime <= 30):
                return
            reaper = lock.with_name(lock.name + ".reap")
            try:
                reaper.mkdir(mode=0o700)
            except FileExistsError:
                return
            try:
                current = read_json(lock / "owner.json")
                fresh = lock.lstat()
                if current == owner and (fresh.st_ino, fresh.st_mtime_ns) == (info.st_ino, info.st_mtime_ns):
                    stale = lock.with_name(lock.name + "." + token + ".stale")
                    os.rename(str(lock), str(stale))
                    shutil.rmtree(str(stale))
            finally:
                reaper.rmdir()
        except (FileNotFoundError, PermissionError, FileExistsError):
            # 新锁可能已接替旧锁；下一次重新取锁，沿用同一预算。
            return

    def change(self, request_id, mutate):
        """只更新仍属于当前请求的状态。

        @param request_id: 精确请求 UUID。
        @param mutate: 接收 request 和完整状态的同步函数。
        @return: 当前状态或 None。
        """
        def apply(value):
            if value and (value.get("request") or {}).get("id") == request_id:
                mutate(value["request"], value)
            return value
        return self.update(apply, create=False)


def cancel_project(project, reason="project_disabled"):
    """项目生命周期开始前作废已有许可，保留 started 执行事实。

    @param project: 精确项目根。
    @param reason: 项目关闭或更新原因。
    @return: 被取消的请求数。
    """
    count = 0
    for platform in ("codex", "claude"):
        directory = Path(project) / ".trellis/.runtime" / ("confirmation-" + platform)
        _directory(directory)
        if not directory.exists():
            continue
        for file in directory.iterdir():
            if file.is_symlink():
                raise ValueError("确认状态不能使用软链接")
            if not file.is_file() or file.suffix != ".json":
                continue
            store = ConfirmationStore(project, file.stem, platform)
            def apply(value):
                nonlocal count
                if value and value.get("request"):
                    value["generation"] += 1
                    if value["request"]["state"] in ACTIVE:
                        value["request"].update(state="cancelled", error=reason)
                        count += 1
                return value
            store.update(apply, create=False)
    return count
