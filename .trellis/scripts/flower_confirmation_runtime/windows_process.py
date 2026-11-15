"""通过 Windows 内核句柄核对本机前台身份、监听归属与退出。"""

import ctypes
import ipaddress
import os
import socket
import struct
import sys
from ctypes import wintypes
from functools import lru_cache
from hashlib import sha256
from pathlib import Path


def _api(library, name, result, *arguments):
    function = getattr(ctypes.WinDLL(library, use_last_error=True), name)
    function.restype = result
    function.argtypes = arguments
    return function


def _kernel(name, result, *arguments):
    return _api("kernel32", name, result, *arguments)


def _winerror():
    return ctypes.WinError(ctypes.get_last_error())


def _token_user(process):
    pointer = ctypes.POINTER(wintypes.HANDLE)
    token = wintypes.HANDLE()
    open_token = _api("advapi32", "OpenProcessToken", wintypes.BOOL, wintypes.HANDLE, wintypes.DWORD, pointer)
    info = _api("advapi32", "GetTokenInformation", wintypes.BOOL, wintypes.HANDLE, wintypes.DWORD,
                ctypes.c_void_p, wintypes.DWORD, ctypes.POINTER(wintypes.DWORD))
    if not open_token(process, 0x0008, ctypes.byref(token)):
        raise _winerror()
    try:
        size = wintypes.DWORD()
        info(token, 1, None, 0, ctypes.byref(size))
        if not 0 < size.value <= 65536:
            raise _winerror()
        data = ctypes.create_string_buffer(size.value)
        if not info(token, 1, data, size, ctypes.byref(size)):
            raise _winerror()
        sid = ctypes.c_void_p.from_buffer(data).value
        length = _api("advapi32", "GetLengthSid", wintypes.DWORD, ctypes.c_void_p)(sid)
        return ctypes.string_at(sid, length)
    finally:
        _kernel("CloseHandle", wintypes.BOOL, wintypes.HANDLE)(token)


class ProcessHandle:
    """持有一个只读查询及等待权限的原生进程句柄。"""

    def __init__(self, pid):
        """打开精确 PID 并核对同一 Windows 用户。

        @param pid: 待观察的 Windows 进程编号。
        @return: None。
        """
        if sys.platform != "win32" or type(pid) is not int or not 0 < pid <= 0xffffffff:
            raise ValueError("Windows 前台进程编号无效")
        self.handle = _kernel("OpenProcess", wintypes.HANDLE, wintypes.DWORD, wintypes.BOOL, wintypes.DWORD)(
            0x00101000, False, pid)
        if not self.handle:
            raise _winerror()
        try:
            if self.exited():
                raise ProcessLookupError("Windows 前台进程已退出")
            current = _kernel("GetCurrentProcess", wintypes.HANDLE)()
            if _token_user(self.handle) != _token_user(current):
                raise PermissionError("Windows 前台进程不属于当前用户")
            size = wintypes.DWORD(32768)
            path = ctypes.create_unicode_buffer(size.value)
            query = _kernel("QueryFullProcessImageNameW", wintypes.BOOL, wintypes.HANDLE, wintypes.DWORD,
                            wintypes.LPWSTR, ctypes.POINTER(wintypes.DWORD))
            if not query(self.handle, 0, path, ctypes.byref(size)):
                raise _winerror()
            times = [wintypes.FILETIME() for _ in range(4)]
            get_times = _kernel("GetProcessTimes", wintypes.BOOL, wintypes.HANDLE, *([ctypes.POINTER(wintypes.FILETIME)] * 4))
            if not get_times(self.handle, *(ctypes.byref(value) for value in times)):
                raise _winerror()
            born = (times[0].dwHighDateTime << 32) | times[0].dwLowDateTime
            self.identity = {"pid": pid, "exe": os.path.normcase(path.value), "created": str(born)}
        except BaseException:
            self.close()
            raise

    def exited(self):
        """查询原进程是否已退出。

        @return: 进程已退出时为 True。
        """
        result = _kernel("WaitForSingleObject", wintypes.DWORD, wintypes.HANDLE, wintypes.DWORD)(self.handle, 0)
        if result not in (0, 258):
            raise _winerror()
        return result == 0

    def close(self):
        """释放当前调用拥有的句柄。

        @return: None。
        """
        if self.handle:
            _kernel("CloseHandle", wintypes.BOOL, wintypes.HANDLE)(self.handle)
            self.handle = None


def open_identity(identity):
    """打开并复核已经冻结的前台进程身份。

    @param identity: 冻结的 PID、可执行文件和创建时间。
    @return: 由调用方关闭的进程句柄。
    """
    handle = ProcessHandle(identity.get("pid"))
    if handle.identity != identity:
        handle.close()
        raise ValueError("原 Windows 前台的进程身份已变化")
    return handle


def _parents():
    class ProcessEntry(ctypes.Structure):
        _fields_ = [("size", wintypes.DWORD), ("usage", wintypes.DWORD), ("pid", wintypes.DWORD),
                    ("heap", ctypes.c_size_t), ("module", wintypes.DWORD), ("threads", wintypes.DWORD),
                    ("parent", wintypes.DWORD), ("priority", wintypes.LONG), ("flags", wintypes.DWORD),
                    ("exe", wintypes.WCHAR * 260)]

    snapshot = _kernel("CreateToolhelp32Snapshot", wintypes.HANDLE, wintypes.DWORD, wintypes.DWORD)(2, 0)
    if snapshot == ctypes.c_void_p(-1).value:
        raise _winerror()
    try:
        row = ProcessEntry()
        row.size = ctypes.sizeof(row)
        first = _kernel("Process32FirstW", wintypes.BOOL, wintypes.HANDLE, ctypes.POINTER(ProcessEntry))
        next_row = _kernel("Process32NextW", wintypes.BOOL, wintypes.HANDLE, ctypes.POINTER(ProcessEntry))
        if not first(snapshot, ctypes.byref(row)):
            raise _winerror()
        entries = {}
        while True:
            entries[row.pid] = (row.parent, row.exe.lower())
            if not next_row(snapshot, ctypes.byref(row)):
                if ctypes.get_last_error() != 18:
                    raise _winerror()
                return entries
    finally:
        _kernel("CloseHandle", wintypes.BOOL, wintypes.HANDLE)(snapshot)


def hook_frontend(executable):
    """沿真实 Hook 进程祖先链识别同用户前台。

    @param executable: 预期原生宿主可执行文件名。
    @return: 原生宿主的冻结身份。
    """
    parents = _parents()
    pid = os.getpid()
    younger = None
    for _ in range(16):
        process = ProcessHandle(pid)
        try:
            identity = process.identity
            born = int(identity["created"])
            if younger is not None and born > younger:
                raise ValueError("Windows Hook 祖先进程编号已复用")
            if Path(identity["exe"]).name == executable:
                return identity
            younger = born
        finally:
            process.close()
        pid = parents.get(pid, (0, ""))[0]
        if not pid:
            break
    raise ValueError("Windows Hook 缺少可核验的原生前台进程")


def _listeners():
    get_table = _api("iphlpapi", "GetExtendedTcpTable", wintypes.DWORD, ctypes.c_void_p,
                     ctypes.POINTER(wintypes.DWORD), wintypes.BOOL, wintypes.ULONG, wintypes.DWORD, wintypes.ULONG)
    for family, layout, fields in ((socket.AF_INET, struct.Struct("<I4sI4sII"), (1, 2, 5)),
                                   (socket.AF_INET6, struct.Struct("<16sII16sIIII"), (0, 2, 7))):
        size = wintypes.DWORD()
        code = get_table(None, ctypes.byref(size), False, family, 3, 0)
        for _ in range(3):
            if code != 122 or not 4 <= size.value <= 16 * 1024 * 1024:
                raise OSError(code, "无法查询 Windows TCP 监听归属")
            data = ctypes.create_string_buffer(size.value)
            code = get_table(data, ctypes.byref(size), False, family, 3, 0)
            if code == 0:
                break
        if code:
            raise OSError(code, "Windows TCP 监听表查询失败")
        count = struct.unpack_from("<I", data)[0]
        if count > (len(data) - 4) // layout.size:
            raise ValueError("Windows TCP 监听表长度无效")
        for index in range(count):
            row = layout.unpack_from(data, 4 + index * layout.size)
            address = ipaddress.ip_address(row[fields[0]])
            if address.is_loopback:
                yield str(address), socket.ntohs(row[fields[1]] & 0xffff), row[fields[2]]


def _image_stamp(path):
    info = path.stat()
    return info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns


@lru_cache(maxsize=8)
def _image_hash(path, stamp):
    digest = sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    if _image_stamp(path) != stamp:
        raise ValueError("Windows 前台程序在核验期间已替换")
    return digest.digest()


def _same_image(path, executable):
    if path.samefile(executable):
        return True
    # Windows 原生 TUI 和托管 daemon 可使用同一发行二进制的两个副本。
    first, second = _image_stamp(path), _image_stamp(executable)
    return (first[2] == second[2]
            and _image_hash(path, first) == _image_hash(executable, second))


def listener_identity(host, port, executable):
    """通过精确本机监听地址取得前台进程身份。

    @param host: 已验证的 loopback IP。
    @param port: 原生 TUI 报告的监听端口。
    @param executable: 预期同一 Codex 可执行文件。
    @return: 同用户的唯一监听者身份。
    """
    owners = {pid for address, number, pid in _listeners() if (address, number) == (host, port)}
    if len(owners) != 1:
        raise ValueError("Windows TUI 监听者缺失或归属不唯一")
    process = ProcessHandle(owners.pop())
    try:
        if not _same_image(Path(process.identity["exe"]), Path(executable)):
            raise ValueError("Windows TUI 监听者不是当前 Codex 客户端")
        return process.identity
    finally:
        process.close()


def check_listener_coverage(known, executable, frontend_images=()):
    """拒绝同用户且未映射到当前共享服务的 Codex 监听窗口。

    @param known: 已核验的前台 PID 集合。
    @param executable: 预期同一 Codex 可执行文件。
    @param frontend_images: 已从原生来源核验的前台映像；识别尚未监听的新窗口。
    @return: None；发现未归属窗口时抛出异常。
    """
    entries = _parents()
    listening = {pid for _, _, pid in _listeners()}
    for pid, (_, name) in entries.items():
        if pid in known or name != Path(executable).name.lower() or (pid not in listening and not frontend_images):
            continue
        try:
            process = ProcessHandle(pid)
        except OSError as error:
            if isinstance(error, ProcessLookupError) or getattr(error, "winerror", None) == 87:
                continue
            raise
        try:
            image = Path(process.identity["exe"])
            # 第二个窗口可能已经启动，尚未建立 codex_tui 监听。托管服务和代理使用独立映像路径。
            if (any(image.samefile(frontend) for frontend in frontend_images)
                    or (pid in listening and _same_image(image, Path(executable)))):
                raise ValueError("存在未归属的 Windows Codex 前台，保持人工确认")
        except FileNotFoundError:
            continue
        finally:
            process.close()
