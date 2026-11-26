"""通过 Codex 内置服务的实际监听者识别并观察前台进程。"""

from __future__ import annotations

import ipaddress
import json
import os
import select
import socket
import stat
import struct
import sys
from pathlib import Path
from urllib.parse import urlsplit
from uuid import UUID


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


def owned_inodes(pid: int, inodes: set | None, proc_root: Path = Path("/proc")) -> set:
    """读取进程持有的 socket inode。

    @param pid: 进程编号。
    @param inodes: 待匹配的 inode 集合，为空值时读取全部 socket。
    @param proc_root: Linux 进程文件系统根目录。
    @return: 进程持有的匹配 inode。
    """
    found = set()
    for entry in (proc_root / str(pid) / "fd").iterdir():
        try:
            link = os.readlink(entry)
            if link.startswith("socket:[") and link.endswith("]") and (inodes is None or link[8:-1] in inodes):
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


def _tcp_listeners(proc_root):
    for filename in ("tcp", "tcp6"):
        for row in (proc_root / "net" / filename).read_text().splitlines()[1:]:
            fields = row.split()
            if len(fields) < 10 or fields[3] != "0A":
                continue
            address, port = fields[1].split(":")
            raw = b"".join(int(address[i:i + 8], 16).to_bytes(4, sys.byteorder)
                           for i in range(0, len(address), 8))
            yield ipaddress.ip_address(raw), int(port, 16), fields[9]


def _writer_locks(proc_root):
    # 只接受内核当前持有的独占锁，跳过排队者与可能残留的磁盘文件。
    with (proc_root / "locks").open(encoding="ascii") as stream:
        for line in stream:
            fields = line.split()
            if len(fields) == 8 and fields[1:4] == ["FLOCK", "ADVISORY", "WRITE"] and fields[6:] == ["0", "EOF"]:
                major, minor, inode = fields[5].split(":")
                yield (int(major, 16), int(minor, 16), int(inode)), int(fields[4])


def _lock_file(path):
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid():
        raise ValueError("Codex 会话写锁文件无法核验")
    return os.major(info.st_dev), os.minor(info.st_dev), info.st_ino


def _writer_owner(home, session_id, proc_root):
    directory = home / "thread-writer-locks"
    if directory.resolve() != directory or str(UUID(session_id)) != session_id:
        raise ValueError("Codex 会话写锁路径无法核验")
    path = directory / f"{session_id}.lock"
    try:
        key = _lock_file(path)
    except FileNotFoundError as error:
        raise ValueError("共享服务未加载当前会话，缺少可核验的内置 Codex 会话写锁") from error
    owners = {pid for lock, pid in _writer_locks(proc_root) if lock == key}
    if len(owners) != 1 or _lock_file(path) != key:
        raise ValueError("Codex 会话缺少稳定的独占写锁")
    return next(iter(owners)), list(key)


def _other_owned_session(home, session_id, pid, locks, proc_root):
    directory = home / "thread-writer-locks"
    if directory.resolve() != directory:
        return False
    sessions = {}
    for entry in (proc_root / str(pid) / "fd").iterdir():
        try:
            path = Path(os.readlink(entry))
            if path.parent != directory or path.suffix != ".lock":
                continue
            key = _lock_file(path)
            info = entry.stat()
            if (os.major(info.st_dev), os.minor(info.st_dev), info.st_ino) == key and locks.get(key) == pid:
                sessions[str(UUID(path.stem))] = (path, key)
        except (FileNotFoundError, ValueError):
            continue
    if not sessions or session_id in sessions:
        return False
    current = dict(_writer_locks(proc_root))
    return all(current.get(key) == pid and _lock_file(path) == key for path, key in sessions.values())


def _unix_socket_peers():
    # 按 Linux unix_diag UAPI 读取已建立连接的双方 inode，避免按端口或 argv 推测服务归属。
    if not hasattr(socket, "AF_NETLINK"):
        raise ValueError("当前 Linux 缺少 Codex 原生连接观测能力")
    header = struct.Struct("=IHHII")
    sequence = 1
    sock_diag_by_family, netlink_sock_diag = 20, 4
    request = struct.pack("=BBHIIIII", socket.AF_UNIX, 0, 0, 1 << 1, 0, 4, 0xffffffff, 0xffffffff)
    peers = {}
    with socket.socket(socket.AF_NETLINK, socket.SOCK_RAW, netlink_sock_diag) as client:
        client.settimeout(5)
        client.bind((0, 0))
        client.send(header.pack(header.size + len(request), sock_diag_by_family, 0x301, sequence, 0) + request)
        while True:
            packet, _, flags, sender = client.recvmsg(65536)
            if not packet or flags & socket.MSG_TRUNC or sender[0] != 0:
                raise ValueError("Linux Codex 原生连接快照不完整")
            offset = 0
            while offset + header.size <= len(packet):
                length, kind, options, reply, _ = header.unpack_from(packet, offset)
                end = offset + length
                if reply != sequence or options & 0x10 or length < header.size or end > len(packet):
                    raise ValueError("Linux Codex 原生连接快照不完整")
                if kind == 3:
                    if length > header.size and (length < header.size + 4 or struct.unpack_from("=i", packet, offset + header.size)[0]):
                        raise ValueError("Linux Codex 原生连接快照读取失败")
                    return peers
                if kind != sock_diag_by_family or length < header.size + 16:
                    raise ValueError("Linux Codex 原生连接快照读取失败")
                inode = struct.unpack_from("=I", packet, offset + header.size + 4)[0]
                position = offset + header.size + 16
                while position + 4 <= end:
                    size, attribute = struct.unpack_from("=HH", packet, position)
                    if size < 4 or position + size > end:
                        raise ValueError("Linux Codex 原生连接信息无效")
                    if attribute == 2 and size == 8:
                        peers[str(inode)] = str(struct.unpack_from("=I", packet, position + 4)[0])
                    position += (size + 3) & ~3
                offset += (length + 3) & ~3


def _control_peers(control, tcp_listeners, proc_root):
    service_pid, local_inode = control
    sockets = owned_inodes(service_pid, None, proc_root)
    if not sockets or any(inode in sockets for _, _, inode in tcp_listeners):
        raise ValueError("无法核验共享 Codex 服务仅使用本机 Unix socket")
    peers = _unix_socket_peers()
    daemon_socket = peers.get(local_inode)
    if daemon_socket not in sockets or peers.get(daemon_socket) != local_inode:
        raise ValueError("无法核验当前共享 Codex 服务的 Unix 连接")
    return {peer for inode, peer in peers.items() if inode in sockets}


def _terminal_input(process, *, raw=False):
    try:
        descriptor = process / "fd/0"
        info = descriptor.stat()
        if not stat.S_ISCHR(info.st_mode):
            return False
        if not os.readlink(descriptor).startswith("/dev/pts/"):
            # 物理或串口终端不在 devpts 中，必须与进程的内核控制终端一致。
            status = (process / "stat").read_text(encoding="utf-8")
            terminal = int(status.rsplit(") ", 1)[1].split()[4])
            if terminal == 0 or info.st_rdev != terminal:
                return False
        if not raw:
            return True
        # 普通 App Server 也可从终端启动。仅查看设备状态，确认 TUI 已接管终端输入。
        import termios

        fd = os.open(descriptor, os.O_RDONLY | os.O_NONBLOCK | os.O_NOCTTY)
        try:
            if os.fstat(fd).st_rdev != info.st_rdev:
                return False
            return not (termios.tcgetattr(fd)[3] & (termios.ICANON | termios.ECHO))
        except termios.error as error:
            raise ValueError("内置 Codex 终端输入状态无法核验") from error
        finally:
            os.close(fd)
    except FileNotFoundError:
        return False


def bind_embedded(home: Path, session_id: str, control: tuple, proc_root: Path = Path("/proc")) -> dict:
    """从精确会话的排他写锁核对内置 Codex 的前台身份。

    @param home: 当前 Codex 用户目录。
    @param session_id: 精确会话 UUID。
    @param control: 已连接的共享控制服务 PID 与本端 socket inode。
    @param proc_root: Linux procfs 根目录。
    @return: 冻结的监听、进程与写锁身份。
    """
    if sys.platform != "linux":
        raise ValueError("当前平台保持人工确认")
    service_pid, _ = control
    pid, lock = _writer_owner(home, session_id, proc_root)
    if pid == service_pid:
        raise ValueError("共享 Codex 服务尚未给出当前会话的前台身份")
    service = process_identity(service_pid, proc_root)
    identity = process_identity(pid, proc_root)
    listeners = list(_tcp_listeners(proc_root))
    sockets = owned_inodes(pid, None, proc_root)
    origins = {(address, port) for address, port, inode in listeners if address.is_loopback and inode in sockets}
    if (Path(identity["exe"]).name != "codex" or len(origins) != 1
            or not _terminal_input(proc_root / str(pid), raw=True)):
        raise ValueError("内置 Codex 会话缺少唯一可核验的本机前台监听者")
    if (os.readlink(proc_root / str(pid) / "ns/net") != os.readlink(proc_root / str(service_pid) / "ns/net")
            or sockets & _control_peers(control, listeners, proc_root)):
        raise ValueError("无法核验内置 Codex 前台独立于共享服务")
    address, port = next(iter(origins))
    host = f"[{address}]" if address.version == 6 else str(address)
    origin = f"http://{host}:{port}"
    binding = bind_origin(origin)
    if (any(binding[key] != value for key, value in identity.items())
            or _writer_owner(home, session_id, proc_root) != (pid, lock)
            or process_identity(service_pid, proc_root) != service
            or owned_inodes(pid, _control_peers(control, listeners, proc_root), proc_root)):
        raise ValueError("内置 Codex 前台或会话写锁已变化")
    return {**binding, "writerLock": lock, "queueService": service}


def check_frontend_coverage(origins: list, *, home: Path | None = None, session_id: str | None = None,
                            control: tuple | None = None) -> None:
    """拒绝尚未对应到原生会话的额外前台，防止同 thread 双窗口借用原窗口。

    @param origins: 当前共享服务全部已加载会话的有效 TUI 来源。
    @param home: 当前 Codex 用户目录，提供时允许核验额外内置窗口。
    @param session_id: 本次确认的精确会话 UUID。
    @param control: 当前共享服务 PID 与本端 socket inode。
    @return: 无归属歧义时返回 None。
    """
    if sys.platform != "linux" or not isinstance(origins, list) or not origins or len(origins) > 1000:
        raise ValueError("无法核验全部本机前台归属")
    known = set()
    for origin in origins:
        known.update(listening_inodes(origin))
    proc_root = Path("/proc")
    tcp_listeners = list(_tcp_listeners(proc_root))
    listeners = {inode for address, _, inode in tcp_listeners if address.is_loopback}
    service_pid = control[0] if control else None
    peers = None
    for root in proc_root.iterdir():
        if not root.name.isdigit():
            continue
        try:
            if root.stat().st_uid != os.getuid():
                continue
            if Path(os.readlink(root / "exe")).name != "codex":
                continue
            identity = process_identity(int(root.name))
        except (FileNotFoundError, ProcessLookupError):
            # 内核线程及退出中的进程可能没有 exe，无法成为运行中的前台。
            continue
        try:
            pid = identity["pid"]
            sockets = owned_inodes(pid, None)
            if sockets & known or pid == service_pid:
                continue
            if not sockets & listeners:
                if service_pid is None or not _terminal_input(root):
                    continue
                if peers is None:
                    peers = _control_peers(control, tcp_listeners, proc_root)
                if not sockets & peers:
                    continue
            elif home is not None and session_id is not None and service_pid is not None:
                if peers is None:
                    peers = _control_peers(control, tcp_listeners, proc_root)
                if (not sockets & peers
                        and os.readlink(root / "ns/net") == os.readlink(proc_root / str(service_pid) / "ns/net")
                        and _other_owned_session(home, session_id, pid, dict(_writer_locks(proc_root)), proc_root)
                        and not owned_inodes(pid, _control_peers(control, tcp_listeners, proc_root))
                        and process_identity(pid) == identity):
                    continue
            raise ValueError("存在未对应到原生会话的前台窗口，可能同会话双开，保持人工确认")
        except (FileNotFoundError, ProcessLookupError):
            if root.exists():
                raise ValueError("无法复核 Codex 进程的窗口及连接归属") from None


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
