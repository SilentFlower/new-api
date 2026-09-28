#!/usr/bin/env python3
"""按已确认的清单开通工作流账号，并生成 Gateway 使用的 Secret 文件。"""

import argparse
import json
import os
import re
import sys
import tempfile
import unicodedata
import urllib.parse
import urllib.request
from pathlib import Path


WORKFLOW_ID = re.compile(r"^[a-z][a-z0-9-]*$")
SUPPORTED_CAPABILITIES = {"chat", "stream_chat", "vision", "embedding", "asr", "rerank"}


def validate_manifest(path):
    """校验清单中的稳定 ID、中文名称和原始额度。"""
    document = json.loads(path.read_text(encoding="utf-8"))
    entries = [entry for entry in document["workflows"] if entry.get("enabled", False)]
    ids = set()
    names = set()
    for entry in entries:
        workflow_id = entry["workflow_id"]
        username = entry["username"]
        quota = entry["user_quota"]
        group = entry["group"]
        if (not WORKFLOW_ID.fullmatch(workflow_id) or len(workflow_id) > 64
                or workflow_id in ids):
            raise ValueError(f"工作流 ID 无效或重复: {workflow_id}")
        if (not username or username != username.strip() or len(username) > 64
                or username in names
                or any(unicodedata.category(char) in {"Cc", "Cf", "Cs"} for char in username)
                or not any("\u4e00" <= char <= "\u9fff" for char in username)):
            raise ValueError(f"工作流中文用户名无效或重复: {username}")
        if not isinstance(quota, int) or isinstance(quota, bool) or quota <= 0:
            raise ValueError(f"工作流额度无效: {workflow_id}")
        if not isinstance(group, str) or not group or group == "auto":
            raise ValueError(f"工作流分组无效: {workflow_id}")
        capabilities = entry.get("capabilities")
        if (not isinstance(capabilities, list) or not capabilities
                or any(not isinstance(item, str) or item not in SUPPORTED_CAPABILITIES
                       for item in capabilities)
                or len(set(capabilities)) != len(capabilities)):
            raise ValueError(f"工作流模型能力未核验或 Gateway 尚不支持: {workflow_id}")
        if (entry.get("name_approved") is not True
                or entry.get("runtime_registered") is not True
                or entry.get("gateway_verified") is not True
                or entry.get("direct_vendor_paths") != []):
            raise ValueError(f"工作流名称、运行时或直连入口尚未核验: {workflow_id}")
        ids.add(workflow_id)
        names.add(username)
    return entries


def write_private_json(path, value):
    """以 0600 权限原子写入凭证，避免中断后留下半截 JSON。"""
    path.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary = tempfile.mkstemp(prefix=".workflow-", dir=path.parent)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
            os.fchmod(stream.fileno(), 0o600)
            json.dump(value, stream, ensure_ascii=False, indent=2, sort_keys=True)
            stream.write("\n")
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
        directory = os.open(path.parent, os.O_RDONLY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def read_private_json(path):
    """读取已存在的私有凭证文件并拒绝过宽权限。"""
    if not path.exists():
        return None
    if path.stat().st_mode & 0o077:
        raise ValueError(f"凭证文件权限过宽: {path}")
    return json.loads(path.read_text(encoding="utf-8"))


class NewApi:
    """用 root PAT 调用 new-api 的工作流账号管理 API。"""

    def __init__(self, base_url, token):
        self.base_url = base_url.rstrip("/")
        self.token = token

    def request(self, method, path, body=None):
        """发送请求并只返回结构化 data，不在错误中包含请求体或令牌。"""
        payload = None if body is None else json.dumps(body).encode("utf-8")
        request = urllib.request.Request(
            self.base_url + path,
            data=payload,
            method=method,
            headers={"Authorization": f"Bearer {self.token}", "Content-Type": "application/json"},
        )
        try:
            with urllib.request.urlopen(request, timeout=20) as response:
                result = json.load(response)
        except Exception as error:
            raise RuntimeError(f"new-api 请求失败: {method} {path}") from error
        if result.get("success") is not True:
            raise RuntimeError(f"new-api 拒绝请求: {method} {path}: {result.get('message', '')}")
        return result.get("data")

    def account(self, entry):
        """按精确用户名和工作流 ID 读取非敏感归属状态。"""
        query = urllib.parse.urlencode({"username": entry["username"], "workflow_id": entry["workflow_id"]})
        return self.request("GET", "/api/token/workflow/status?" + query)

    def root_tokens(self, workflow_id):
        """查找 root 名下与工作流 ID 完全同名的令牌。"""
        query = urllib.parse.urlencode({"keyword": workflow_id, "p": 0, "page_size": 100})
        data = self.request("GET", "/api/token/search?" + query)
        if data.get("total", 0) > len(data.get("items", [])):
            raise RuntimeError(f"root 令牌搜索结果过多，需人工核对: {workflow_id}")
        return [token for token in data.get("items", []) if token["name"] == workflow_id]


def inspect_entry(api, entry, saved):
    """确认现有账号与保存的令牌 ID 是否一致。"""
    status = api.account(entry)
    user = status.get("user")
    tokens = status.get("tokens", [])
    if user is not None:
        if (saved is None or (saved.get("user_id") is not None and user["id"] != saved["user_id"])
                or user["username"] != entry["username"] or user["display_name"] != entry["username"]
                or user["role"] != 1 or user["status"] != 1 or user["group"] != entry["group"]):
            raise RuntimeError(f"已有账号归属无法安全恢复: {entry['workflow_id']}")
        matching = [token for token in tokens if token["id"] == saved["token_id"]]
        if (len(matching) != 1 or matching[0]["status"] != 1
                or not matching[0]["unlimited_quota"]
                or matching[0].get("effective_state_matches") is not True):
            raise RuntimeError(f"已有令牌状态不符合工作流约束: {entry['workflow_id']}")
        saved["user_id"] = user["id"]
        return "migrated"
    root_tokens = api.root_tokens(entry["workflow_id"])
    if saved is not None:
        if len(root_tokens) != 1 or root_tokens[0]["id"] != saved["token_id"]:
            raise RuntimeError(f"暂存令牌与 root 账号不一致: {entry['workflow_id']}")
        return "ready"
    if len(root_tokens) > 1:
        raise RuntimeError(f"root 名下存在同名令牌: {entry['workflow_id']}")
    if root_tokens and (root_tokens[0]["group"] != entry["group"]
                        or not root_tokens[0]["unlimited_quota"] or root_tokens[0]["status"] != 1):
        raise RuntimeError(f"root 令牌配置不符合工作流约束: {entry['workflow_id']}")
    return "recover-key" if root_tokens else "create"


def provision(api, entry, saved, state, bundle, bundle_path):
    """先持久化明文 Key，再迁移一个令牌的归属。"""
    workflow_id = entry["workflow_id"]
    if state == "migrated":
        return
    if saved is None:
        if state == "create":
            api.request("POST", "/api/token/", {
                "name": workflow_id, "group": entry["group"],
                "unlimited_quota": True, "expired_time": -1,
            })
        tokens = api.root_tokens(workflow_id)
        if len(tokens) != 1:
            raise RuntimeError(f"无法唯一定位新建令牌: {workflow_id}")
        token_id = tokens[0]["id"]
        keys = api.request("POST", "/api/token/batch/keys", {"ids": [token_id]})["keys"]
        key = keys.get(str(token_id))
        if not key:
            raise RuntimeError(f"无法取得令牌明文: {workflow_id}")
        saved = {"username": entry["username"], "token_id": token_id, "key": key}
        bundle["entries"][workflow_id] = saved
        write_private_json(bundle_path, bundle)
    response = api.request("POST", "/api/token/migrate", {
        "token_ids": [saved["token_id"]],
        "targets": [{"token_id": saved["token_id"], "username": entry["username"],
                     "user_quota": entry["user_quota"]}],
    })["results"]
    if len(response) != 1 or response[0]["status"] != "success":
        raise RuntimeError(f"令牌迁移失败: {workflow_id}: {response[0].get('error', '') if response else ''}")
    saved["user_id"] = response[0]["new_user_id"]
    write_private_json(bundle_path, bundle)
    if inspect_entry(api, entry, saved) != "migrated":
        raise RuntimeError(f"迁移后归属核验失败: {workflow_id}")


def main():
    """执行预检或开通，只有全部账号验证后才写 Gateway Secret。"""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", required=True, type=Path)
    parser.add_argument("--base-url", required=True)
    parser.add_argument("--bundle", required=True, type=Path)
    parser.add_argument("--secret", required=True, type=Path)
    parser.add_argument("--apply", action="store_true")
    args = parser.parse_args()
    entries = validate_manifest(args.manifest)
    if not entries:
        raise ValueError("清单没有已启用的工作流")
    token = os.environ.get("NEW_API_ROOT_PAT", "")
    if not token:
        raise ValueError("NEW_API_ROOT_PAT 未设置")
    api = NewApi(args.base_url, token)
    quota_unit = api.request("GET", "/api/status")["quota_per_unit"]
    max_quota = int(1_000_000_000 * quota_unit)
    for entry in entries:
        if entry["user_quota"] > max_quota:
            raise ValueError(f"工作流额度超过系统上限: {entry['workflow_id']}")
    bundle = read_private_json(args.bundle) or {"version": 1, "entries": {}}
    if bundle.get("version") != 1:
        raise ValueError("凭证包版本不支持")
    secret = read_private_json(args.secret) or {}
    if not isinstance(secret, dict):
        raise ValueError("Gateway Secret 格式无效")
    for entry in entries:
        workflow_id = entry["workflow_id"]
        previous = secret.get(workflow_id)
        saved = bundle["entries"].get(workflow_id)
        if previous is not None and (saved is None or previous != saved.get("key")):
            raise RuntimeError(f"Secret 已有不同凭证，需走轮换流程: {workflow_id}")
    states = {}
    for entry in entries:
        saved = bundle["entries"].get(entry["workflow_id"])
        if saved is not None and saved["username"] != entry["username"]:
            raise ValueError(f"清单名称与凭证包不一致: {entry['workflow_id']}")
        states[entry["workflow_id"]] = inspect_entry(api, entry, saved)
        print(f"{entry['workflow_id']}: {states[entry['workflow_id']]}")
    if not args.apply:
        return
    for entry in entries:
        workflow_id = entry["workflow_id"]
        provision(api, entry, bundle["entries"].get(workflow_id), states[workflow_id], bundle, args.bundle)
    write_private_json(args.bundle, bundle)
    for entry in entries:
        workflow_id = entry["workflow_id"]
        saved = bundle["entries"][workflow_id]
        if inspect_entry(api, entry, saved) != "migrated":
            raise RuntimeError(f"发布 Secret 前账号状态不正确: {workflow_id}")
        previous = secret.get(workflow_id)
        if previous is not None and previous != saved["key"]:
            raise RuntimeError(f"Secret 已有不同凭证，需走轮换流程: {workflow_id}")
        secret[workflow_id] = saved["key"]
    write_private_json(args.secret, secret)
    print(f"已核验 {len(entries)} 个工作流并更新本地 Secret 文件")


if __name__ == "__main__":
    try:
        main()
    except (ValueError, RuntimeError, KeyError, OSError) as error:
        print(f"开通失败: {error}", file=sys.stderr)
        sys.exit(1)
