"""增量读取原生 JSONL，冻结会话记录的文件身份。"""

import json
import os
import stat
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
    def locate(cls, file, identifier, matches):
        """在有界文件尾部定位当前回合的完整记录。

        @param file: 宿主报告的精确会话路径。
        @param identifier: 原生回合 ID。
        @param matches: 核对候选记录及其结构化身份。
        @return: 可跨进程续读的记录边界。
        """
        info = file.lstat()
        if not stat.S_ISREG(info.st_mode):
            raise ValueError("原生会话记录必须是普通文件")
        identity = [str(file), info.st_dev, info.st_ino]
        start = max(0, info.st_size - 2 * RECORD_BYTES)
        needle = identifier.encode("ascii")
        with file.open("rb") as stream:
            opened = os.fstat(stream.fileno())
            if (opened.st_dev, opened.st_ino) != (info.st_dev, info.st_ino):
                raise ValueError("原生会话记录在打开时已替换")
            stream.seek(start)
            raw = stream.read(info.st_size - start)
            end = file.lstat()
            if (len(raw) != info.st_size - start or (end.st_dev, end.st_ino) != (info.st_dev, info.st_ino)
                    or end.st_size < info.st_size or (end.st_size == info.st_size and end.st_mtime_ns != info.st_mtime_ns)):
                raise ValueError("原生会话记录在读取时已变化")
            lines = raw.split(b"\n")
            offset = info.st_size - len(lines.pop())
            # 只解析可能属于当前回合的完整行。窗口左侧可能落在某条旧记录中间。
            for index in range(len(lines) - 1, -1, -1):
                line = lines[index]
                if (index or not start) and len(line) < RECORD_BYTES and needle in line:
                    try:
                        entry = json.loads(line)
                    except (ValueError, UnicodeError):
                        entry = None
                    if isinstance(entry, dict) and matches(entry):
                        anchor = raw[max(0, offset - start - ANCHOR_BYTES):offset - start]
                        if len(anchor) != min(ANCHOR_BYTES, offset):
                            anchor_hash = cls._anchor(stream, offset).hex()
                        else:
                            anchor_hash = sha256(anchor).hexdigest()
                        return {"identity": identity, "size": info.st_size, "modified": info.st_mtime_ns,
                                "offset": offset, "anchor": anchor_hash}
                offset -= len(line) + 1
        raise ValueError("当前回合缺少可核验的完整记录，保持人工确认")

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
        info = file.lstat()
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
            end = file.lstat()
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
    def _anchor(stream, offset):
        length = min(ANCHOR_BYTES, offset)
        stream.seek(offset - length)
        return sha256(stream.read(length)).digest()
