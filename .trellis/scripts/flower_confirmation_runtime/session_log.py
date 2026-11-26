"""增量读取原生 JSONL，冻结会话记录的文件身份。"""

import json
import os
import stat
import time
from copy import deepcopy
from hashlib import sha256
from pathlib import Path


# 单条记录的解析预算与本机原生消息预算同量级；会话文件总量不限。
RECORD_BYTES = 16 * 1024 * 1024
ANCHOR_BYTES = 4096
DISPLAY_BYTES = 768 * 1024


class SessionLog:
    """在一次客户端生命周期内只消费新增的完整记录。"""

    def __init__(self):
        """初始化空会话观察缓存。"""
        self.observation = None
        self.offset = 0
        self.anchor = None
        self.result = None
        self._verify_resume = False

    @classmethod
    def checkpoint(cls, file):
        """冻结当前完整行边界，后续仅观察新追加的记录。

        @param file: 宿主提供的精确日志路径。
        @return: 可跨进程续读的文件身份与边界。
        """
        info = file.lstat()
        if not stat.S_ISREG(info.st_mode):
            raise ValueError("原生会话记录必须是普通文件")
        identity = [str(file), info.st_dev, info.st_ino]
        with file.open("rb") as stream:
            opened = os.fstat(stream.fileno())
            if (opened.st_dev, opened.st_ino) != (info.st_dev, info.st_ino):
                raise ValueError("原生会话记录在打开时已替换")
            start = max(0, info.st_size - RECORD_BYTES - 1)
            stream.seek(start)
            tail = stream.read(info.st_size - start)
            if len(tail) != info.st_size - start:
                raise ValueError("原生会话记录在读取时已截断")
            # 未写完的行必须留给下一次读取，避免准备过程中跳过新输入。
            boundary = tail.rfind(b"\n") + 1
            if not boundary and start:
                raise ValueError("原生会话未完成记录超过 16 MiB 解析预算")
            offset = start + boundary
            anchor = cls._anchor(stream, offset).hex()
            end = cls._stat(file, {"identity": identity, "size": info.st_size, "modified": info.st_mtime_ns})
            if ((end.st_dev, end.st_ino) != (info.st_dev, info.st_ino) or end.st_size < info.st_size
                    or (end.st_size == info.st_size and end.st_mtime_ns != info.st_mtime_ns)):
                raise ValueError("原生会话记录在准备观察边界时已变化")
        return {"identity": identity, "size": info.st_size, "modified": info.st_mtime_ns,
                "offset": offset, "anchor": anchor}

    @classmethod
    def locate(cls, file, identifier, matches):
        """反向分块定位当前回合的完整记录。

        @param file: 宿主报告的精确会话路径。
        @param identifier: 用于失败诊断的原生标识，准确匹配由 matches 完成。
        @param matches: 核对候选记录及其结构化身份。
        @return: 可跨进程续读的记录边界。
        """
        frozen = cls.checkpoint(file)
        offset = frozen["offset"]
        position = max(0, offset - 1)
        pending = b""
        with file.open("rb") as stream:
            opened = os.fstat(stream.fileno())
            if [opened.st_dev, opened.st_ino] != frozen["identity"][1:]:
                raise ValueError("原生会话记录在打开时已替换")
            # 反向分块定位当前回合；总日志长度不限制可用性，单条解析仍沿用既有预算。
            while position or pending:
                start = max(0, position - 65536)
                stream.seek(start)
                chunk = stream.read(position - start)
                if len(chunk) != position - start:
                    raise ValueError("原生会话记录在读取时已截断")
                lines = (chunk + pending).split(b"\n")
                pending = lines.pop(0) if start else b""
                if len(pending) > RECORD_BYTES:
                    raise ValueError("原生会话单条记录超过 16 MiB 解析预算")
                for line in reversed(lines):
                    if len(line) > RECORD_BYTES:
                        raise ValueError("原生会话单条记录超过 16 MiB 解析预算")
                    try:
                        entry = json.loads(line)
                    except (ValueError, UnicodeError):
                        entry = None
                    if isinstance(entry, dict) and matches(entry):
                        anchor = cls._anchor(stream, offset).hex()
                        current = cls.checkpoint(file)
                        cls.verify(frozen, current)
                        return {**frozen, "offset": offset, "anchor": anchor}
                    offset -= len(line) + 1
                position = start
        raise ValueError("当前回合缺少可核验的完整记录，保持人工确认：" + identifier)

    def resume(self, observation, initial):
        """从冻结位置继续观察，并在首读时核对文件身份和边界。

        @param observation: 已核对的记录边界。
        @param initial: 起点处的初始证据。
        @return: None。
        """
        self.observation = observation
        self.offset = observation["offset"]
        self.anchor = bytes.fromhex(observation["anchor"])
        self.result = initial
        self._verify_resume = True

    def read(self, file, initial, consume):
        """读取普通 JSONL 文件并累计当前会话证据。

        @param file: 宿主报告的精确会话文件路径。
        @param initial: 初始的证据字典。
        @param consume: 处理一条完整记录并更新证据的回调。
        @return: 累计证据及可用于跨阶段核验的文件身份和进度。
        """
        info = self._stat(file, self.observation)
        if not stat.S_ISREG(info.st_mode):
            raise ValueError("原生会话记录必须是普通文件")
        identity = [str(file), info.st_dev, info.st_ino]
        previous = self.observation
        if previous:
            if (identity != previous["identity"] or info.st_size < previous["size"]
                    or (info.st_size == previous["size"] and info.st_mtime_ns != previous["modified"])):
                raise ValueError("原生会话记录已替换、截断或重写")
            if info.st_size == previous["size"] and not self._verify_resume:
                return self.result, previous

        result = deepcopy(self.result) if previous else initial
        offset = self.offset
        with file.open("rb") as stream:
            opened = os.fstat(stream.fileno())
            if (opened.st_dev, opened.st_ino) != (info.st_dev, info.st_ino):
                raise ValueError("原生会话记录在打开时已替换")
            # 追加前核对已消费边界，识别同一文件被截断后重新写长的情况。
            if previous and self._anchor(stream, offset) != self.anchor:
                raise ValueError("原生会话记录的已核验内容已变化")
            stream.seek(offset)
            while stream.tell() < info.st_size:
                raw = stream.readline(min(RECORD_BYTES + 1, info.st_size - stream.tell()))
                if not raw:
                    raise ValueError("原生会话记录在读取时已截断")
                if len(raw) > RECORD_BYTES:
                    raise ValueError("原生会话单条记录超过 16 MiB 解析预算")
                if not raw.endswith(b"\n"):
                    break
                offset = stream.tell()
                if not raw.isspace():
                    consume(result, json.loads(raw.decode("utf-8")))
            anchor = self._anchor(stream, offset)
            end = self._stat(file, {"identity": identity, "size": info.st_size, "modified": info.st_mtime_ns})
            if ((end.st_dev, end.st_ino) != (info.st_dev, info.st_ino) or end.st_size < info.st_size
                    or (end.st_size == info.st_size and end.st_mtime_ns != info.st_mtime_ns)):
                raise ValueError("原生会话记录在读取时已变化")

        self.offset, self.anchor, self.result = offset, anchor, result
        self._verify_resume = False
        self.observation = {"identity": identity, "size": info.st_size, "modified": info.st_mtime_ns,
                            "offset": offset, "anchor": anchor.hex()}
        return result, self.observation

    @classmethod
    def verify(cls, frozen, current):
        """复核另一进程冻结的记录身份及已消费边界。

        @param frozen: 准备阶段的观察结果。
        @param current: 当前客户端已验证的观察结果。
        @return: None；来源漂移时抛出异常。
        """
        if (not current or current["identity"] != frozen["identity"] or current["size"] < frozen["size"]
                or current["offset"] < frozen["offset"]
                or (current["size"] == frozen["size"] and current["modified"] != frozen["modified"])):
            raise ValueError("冻结的原生会话记录已替换、截断或重写")
        if current["offset"] == frozen["offset"] and current["anchor"] == frozen["anchor"]:
            return
        with Path(current["identity"][0]).open("rb") as stream:
            opened = os.fstat(stream.fileno())
            if ([opened.st_dev, opened.st_ino] != frozen["identity"][1:] or opened.st_size < frozen["size"]
                    or cls._anchor(stream, frozen["offset"]).hex() != frozen["anchor"]):
                raise ValueError("冻结的原生会话记录边界已变化")

    @staticmethod
    def _stat(file, previous):
        """短暂重读追加期间先更新的 mtime，保留稳定重写的拒绝条件。

        @param file: 当前原生日志。
        @param previous: 需要核对的既有身份、大小和修改时间。
        @return: 预算内最终观察的文件元数据。
        """
        info = file.lstat()
        if previous:
            deadline = time.monotonic() + 0.05
            # 写入可先公布 mtime 再增长 size；只等待该瞬态，身份和大小异常交给调用方拒绝。
            while (stat.S_ISREG(info.st_mode) and [str(file), info.st_dev, info.st_ino] == previous["identity"]
                   and info.st_size == previous["size"] and info.st_mtime_ns != previous["modified"]
                   and time.monotonic() < deadline):
                time.sleep(0.005)
                info = file.lstat()
        return info

    @staticmethod
    def _anchor(stream, offset):
        length = min(ANCHOR_BYTES, offset)
        stream.seek(offset - length)
        return sha256(stream.read(length)).digest()
