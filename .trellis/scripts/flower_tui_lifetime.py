"""通过 Codex 内置服务的实际监听者识别并观察前台进程。"""

from __future__ import annotations

import ipaddress
import json
import os
import select
import sys
from pathlib import Path
from urllib.parse import urlsplit


def parse_origin(origin: str) -> tuple:
    """只接受原生 TUI 返回的本机 HTTP origin。

    @param origin: 不包含路径、凭据和查询参数的服务来源。
    @return: 规范化 IP 与端口。
    """
    value = urlsplit(origin)
    if (value.scheme != "http" or value.hostname not in ("127.0.0.1", "::1")
            or value.username or value.password or value.path not in ("", "/")
            or value.query or value.fragment or not value.port):
        raise ValueError("TUI 服务必须提供准确的本机 HTTP origin")
    return str(ipaddress.ip_address(value.hostname)), value.port


def process_identity(pid: int, proc_root: Path = Path("/proc")) -> dict:
    """读取进程出生身份，拒绝已退出或不可核验的进程。

    @param pid: 待识别进程编号。
    @param proc_root: Linux 进程文件系统根目录。
    @return: 可跨调用比较的进程身份。
    """
    if isinstance(pid, bool) or not isinstance(pid, int) or pid <= 0:
        raise ValueError("前台进程编号无效")
    root = proc_root / str(pid)
    stat = (root / "stat").read_text(encoding="utf-8")
    fields = stat[stat.rfind(")") + 2:].split()
    if len(fields) < 20 or fields[0] in ("Z", "X", "x"):
        raise ValueError("前台进程已经退出或身份不完整")
    uid = root.stat().st_uid
    if uid != os.getuid():
        raise ValueError("前台进程不属于当前用户")
    return {"pid": pid, "startTicks": fields[19], "exe": os.readlink(root / "exe"),
            "uid": uid, "bootId": (proc_root / "sys/kernel/random/boot_id").read_text().strip()}


def listening_inodes(origin: str, proc_root: Path = Path("/proc")) -> set:
    """按真实 IP 和端口寻找监听 socket，避免同端口不同地址串用。

    @param origin: 已限制为本机的 TUI 来源。
    @param proc_root: Linux 进程文件系统根目录。
    @return: 匹配的监听 socket inode 集合。
    """
    host, port = parse_origin(origin)
    version = ipaddress.ip_address(host).version
    filename = "tcp" if version == 4 else "tcp6"
    result = set()
    for row in (proc_root / "net" / filename).read_text().splitlines()[1:]:
        fields = row.split()
        if len(fields) < 10 or fields[3] != "0A":
            continue
        address, encoded_port = fields[1].split(":")
        if int(encoded_port, 16) != port:
            continue
        # procfs 的地址按主机字序输出 32 位字，不能直接把完整十六进制当作网络字节序。
        raw = b"".join(int(address[i:i + 8], 16).to_bytes(4, sys.byteorder)
                       for i in range(0, len(address), 8))
        if str(ipaddress.ip_address(raw)) == host:
            result.add(fields[9])
    return result


def owned_inodes(pid: int, inodes: set, proc_root: Path = Path("/proc")) -> set:
    """核对指定进程实际持有的监听 socket。

    @param pid: 进程编号。
    @param inodes: 待匹配的 socket inode。
    @param proc_root: Linux 进程文件系统根目录。
    @return: 进程持有的匹配 inode。
    """
    found = set()
    for entry in (proc_root / str(pid) / "fd").iterdir():
        try:
            link = os.readlink(entry)
            if link.startswith("socket:[") and link.endswith("]") and link[8:-1] in inodes:
                found.add(link[8:-1])
        except FileNotFoundError:
            continue
    return found


def check_binding(binding: dict, proc_root: Path = Path("/proc")) -> None:
    """复核冻结的出生身份和原始监听资源，PID 复用不能通过。

    @param binding: bind_origin 返回的窗口绑定。
    @param proc_root: Linux 进程文件系统根目录。
    @return: 核验成功时返回 None，失效时抛出异常。
    """
    if type(binding.get("version")) is not int or binding["version"] != 1:
        raise ValueError("未知的窗口绑定版本")
    identity = process_identity(binding["pid"], proc_root)
    if any(identity[key] != binding.get(key) for key in identity):
        raise ValueError("原前台进程的出生身份已失效")
    expected = set(binding["socketInodes"])
    actual = listening_inodes(binding["origin"], proc_root)
    if not expected or not expected.issubset(actual) or owned_inodes(binding["pid"], expected, proc_root) != expected:
        raise ValueError("原 TUI 监听服务已经失效")


def open_binding(binding: dict) -> int:
    """为已冻结的原进程打开内核退出句柄。

    @param binding: 待核验的窗口身份。
    @return: 由调用者关闭的 pidfd。
    """
    if sys.platform != "linux" or not callable(getattr(os, "pidfd_open", None)):
        raise ValueError("当前运行时缺少可靠的 Linux 前台退出观测能力")
    check_binding(binding)
    fd = os.pidfd_open(binding["pid"])
    try:
        # 打开句柄前后重验，避免进程恰在绑定过程中退出或 PID 已被重新分配。
        check_binding(binding)
        poll = select.poll()
        poll.register(fd, select.POLLIN)
        if poll.poll(0):
            raise ValueError("原前台进程已退出")
        return fd
    except BaseException:
        os.close(fd)
        raise


def bind_origin(origin: str) -> dict:
    """从内置服务的准确监听者建立窗口绑定。

    @param origin: 原生会话返回的 TUI 服务来源。
    @return: 唯一前台 Codex 进程的冻结身份。
    """
    if sys.platform != "linux":
        raise ValueError("当前平台保持人工确认")
    inodes = listening_inodes(origin)
    if not inodes:
        raise ValueError("TUI 服务没有可核验的本机监听者")
    owners = []
    for root in Path("/proc").iterdir():
        if not root.name.isdigit():
            continue
        try:
            if root.stat().st_uid == os.getuid() and owned_inodes(int(root.name), inodes):
                owners.append(process_identity(int(root.name)))
        except (FileNotFoundError, ProcessLookupError):
            continue
    if len(owners) != 1 or Path(owners[0]["exe"]).name != "codex":
        raise ValueError("TUI 服务没有唯一可核验的前台 Codex 进程")
    binding = {"version": 1, "origin": origin, **owners[0], "socketInodes": sorted(inodes)}
    fd = open_binding(binding)
    os.close(fd)
    return binding


def check_frontend_coverage(origins: list) -> None:
    """拒绝尚未对应到原生会话的额外前台，防止同 thread 双窗口借用原窗口。

    @param origins: 当前共享服务全部已加载会话的有效 TUI 来源。
    @return: 无归属歧义时返回 None。
    """
    if sys.platform != "linux" or not isinstance(origins, list) or not origins or len(origins) > 1000:
        raise ValueError("无法核验全部本机前台归属")
    known = set()
    for origin in origins:
        known.update(listening_inodes(origin))
    listeners = set()
    for filename in ("tcp", "tcp6"):
        for row in (Path("/proc/net") / filename).read_text().splitlines()[1:]:
            fields = row.split()
            if len(fields) < 10 or fields[3] != "0A":
                continue
            address = fields[1].split(":")[0]
            raw = b"".join(int(address[i:i + 8], 16).to_bytes(4, sys.byteorder)
                           for i in range(0, len(address), 8))
            if ipaddress.ip_address(raw).is_loopback:
                listeners.add(fields[9])
    for root in Path("/proc").iterdir():
        if not root.name.isdigit():
            continue
        try:
            if root.stat().st_uid != os.getuid():
                continue
            if Path(os.readlink(root / "exe")).name != "codex":
                continue
            identity = process_identity(int(root.name))
            held = owned_inodes(identity["pid"], listeners)
            if held and not held.intersection(known):
                raise ValueError("存在未对应到原生会话的前台窗口，可能同会话双开，保持人工确认")
        except (FileNotFoundError, ProcessLookupError):
            continue


def emit(value: dict) -> None:
    """向本请求的父进程输出一行 UTF-8 JSON。

    @param value: 事件或执行结果。
    @return: None。
    """
    print(json.dumps(value, ensure_ascii=False), flush=True)


def watch_binding(binding: dict) -> None:
    """等待前台退出，并在确认 worker 消失后结束自己的观察。

    @param binding: 已核验的窗口身份。
    @return: 原窗口或父进程失效后返回。
    """
    fd = open_binding(binding)
    try:
        poll = select.poll()
        poll.register(fd, select.POLLIN)
        poll.register(sys.stdin.fileno(), select.POLLIN | select.POLLHUP)
        emit({"event": "watching"})
        while True:
            events = poll.poll(250)
            for current, unused in events:
                if current == fd:
                    emit({"event": "exited", "reason": "frontend_exited"})
                    return
                if current == sys.stdin.fileno():
                    return
            check_binding(binding)
    finally:
        os.close(fd)


def main() -> int:
    """处理一次绑定、检查或临时观察命令。

    @return: 成功为 0，能力不足或绑定失效为 1。
    """
    try:
        value = json.loads(sys.stdin.buffer.readline(65537))
        if not isinstance(value, dict):
            raise ValueError("窗口观测输入必须是 JSON 对象")
        command = sys.argv[1] if len(sys.argv) == 2 else ""
        if command == "bind":
            emit({"binding": bind_origin(value["origin"])})
        elif command == "coverage":
            check_frontend_coverage(value["origins"])
            emit({"valid": True})
        elif command == "check":
            fd = open_binding(value)
            os.close(fd)
            emit({"valid": True})
        elif command == "watch":
            watch_binding(value)
        else:
            raise ValueError("窗口观测命令无效")
        return 0
    except (OSError, ValueError, KeyError, TypeError) as error:
        emit({"event": "invalid", "valid": False, "reason": str(error)[:500]})
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
