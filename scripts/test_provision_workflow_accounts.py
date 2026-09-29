"""工作流开通命令的本地安全与恢复测试。"""

import io
import json
import os
import sys
import tempfile
import unittest
import urllib.error
from pathlib import Path
from unittest.mock import patch

import provision_workflow_accounts as provision


class FakeNewApi:
    """模拟令牌在 root 名下创建后迁移到工作流用户。"""

    def __init__(self):
        self.token = None
        self.user = None
        self.create_count = 0

    def account(self, entry):
        """读取账号与令牌的当前归属。"""
        if self.user is None:
            return {"user": None, "tokens": []}
        return {"user": self.user, "tokens": [{
            "id": self.token["id"], "status": 1, "unlimited_quota": True,
            "effective_state_matches": True,
        }]}

    def root_tokens(self, workflow_id):
        """只返回尚未迁移的同名令牌。"""
        return [self.token] if self.token and self.user is None else []

    def request(self, method, path, body=None):
        """模拟开通链路的三个写接口。"""
        if path == "/api/token/":
            self.create_count += 1
            self.token = {"id": 9, "name": body["name"], "group": body["group"],
                          "unlimited_quota": True, "status": 1}
            return None
        if path == "/api/token/batch/keys":
            return {"keys": {"9": "sk-private-value"}}
        if path == "/api/token/migrate":
            self.user = {"id": 12, "username": body["targets"][0]["username"],
                         "display_name": body["targets"][0]["username"], "role": 1,
                         "status": 1, "group": "default"}
            return {"results": [{"status": "success", "new_user_id": 12}]}
        raise AssertionError(path)


class ProvisionWorkflowAccountsTest(unittest.TestCase):
    """验证凭证持久化顺序及重复执行行为。"""

    def test_admin_request_retries_429_with_retry_after(self):
        """管理接口限流时遵守 Retry-After，并重发同一个请求。"""
        limited = urllib.error.HTTPError("http://localhost/api/status", 429, "limited",
                                         {"Retry-After": "41"}, None)
        response = io.BytesIO(b'{"success":true,"data":{"quota_per_unit":1}}')
        api = provision.NewApi("http://localhost", "test-pat")

        with patch.object(provision.urllib.request, "urlopen", side_effect=[limited, response]) as urlopen, \
                patch.object(provision.time, "sleep") as sleep:
            self.assertEqual(api.request("GET", "/api/status"), {"quota_per_unit": 1})

        self.assertEqual(urlopen.call_count, 2)
        sleep.assert_called_once_with(41)

    def test_admin_request_stops_after_bounded_429_retries(self):
        """持续限流时停止请求，不让开户脚本无限等待。"""
        api = provision.NewApi("http://localhost", "test-pat")

        def limited(*_args, **_kwargs):
            raise urllib.error.HTTPError("http://localhost/api/status", 429, "limited",
                                         {"Retry-After": "1"}, None)

        with patch.object(provision.urllib.request, "urlopen", side_effect=limited) as urlopen, \
                patch.object(provision.time, "sleep") as sleep:
            with self.assertRaisesRegex(RuntimeError, "new-api 请求失败: GET /api/status"):
                api.request("GET", "/api/status")

        self.assertEqual(urlopen.call_count, 5)
        self.assertEqual(sleep.call_count, 4)

    def test_provision_persists_key_and_resumes_without_duplicate(self):
        """同一清单重跑时保留原账号、原令牌和私有凭证。"""
        entry = {"workflow_id": "material-check", "username": "物料校验", "group": "default", "user_quota": 10}
        api = FakeNewApi()
        with tempfile.TemporaryDirectory() as directory:
            bundle_path = Path(directory) / "bundle.json"
            bundle = {"version": 1, "entries": {}}
            provision.provision(api, entry, None, "create", bundle, bundle_path)
            self.assertEqual(api.create_count, 1)
            self.assertEqual(bundle_path.stat().st_mode & 0o777, 0o600)
            saved = json.loads(bundle_path.read_text(encoding="utf-8"))["entries"][entry["workflow_id"]]
            self.assertEqual(saved["key"], "sk-private-value")
            self.assertEqual(provision.inspect_entry(api, entry, saved), "migrated")
            provision.provision(api, entry, saved, "migrated", bundle, bundle_path)
            self.assertEqual(api.create_count, 1)

    def test_existing_account_without_bundle_is_rejected(self):
        """缺少原始 Key 的已迁移账号不能被静默重新开通。"""
        entry = {"workflow_id": "material-check", "username": "物料校验", "group": "default", "user_quota": 10}
        api = FakeNewApi()
        api.token = {"id": 9}
        api.user = {"id": 12, "username": "物料校验", "display_name": "物料校验",
                    "role": 1, "status": 1, "group": "default"}
        with self.assertRaises(RuntimeError):
            provision.inspect_entry(api, entry, None)

    def test_stale_effective_token_owner_blocks_secret_publication(self):
        """数据库已迁移但鉴权路径仍读到旧缓存时不得发布 Key。"""
        entry = {"workflow_id": "material-check", "username": "物料校验", "group": "default", "user_quota": 10}
        api = FakeNewApi()
        api.token = {"id": 9}
        api.user = {"id": 12, "username": "物料校验", "display_name": "物料校验",
                    "role": 1, "status": 1, "group": "default"}
        original_account = api.account

        def stale_account(current_entry):
            status = original_account(current_entry)
            status["tokens"][0]["effective_state_matches"] = False
            return status

        api.account = stale_account
        saved = {"username": "物料校验", "token_id": 9, "key": "sk-private-value", "user_id": 12}
        with self.assertRaisesRegex(RuntimeError, "已有令牌状态不符合工作流约束"):
            provision.inspect_entry(api, entry, saved)

    def test_secret_conflict_stops_before_account_creation(self):
        """已有不同 Key 时，apply 不能先创建 root 令牌。"""
        api = FakeNewApi()
        original_request = api.request

        def request(method, path, body=None):
            if path == "/api/status":
                return {"quota_per_unit": 1}
            return original_request(method, path, body)

        api.request = request
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            manifest = base / "manifest.json"
            manifest.write_text(json.dumps({"workflows": [{"workflow_id": "material-check",
                "username": "物料校验", "user_quota": 10, "group": "default", "enabled": True,
                "capabilities": ["chat"], "name_approved": True, "runtime_registered": True,
                "gateway_verified": True, "direct_vendor_paths": []}]}),
                encoding="utf-8")
            secret = base / "secret.json"
            provision.write_private_json(secret, {"material-check": "sk-existing-key"})
            argv = ["provision", "--manifest", str(manifest), "--base-url", "http://localhost",
                    "--bundle", str(base / "bundle.json"), "--secret", str(secret), "--apply"]
            with patch.object(provision, "NewApi", return_value=api), \
                    patch.dict(os.environ, {"NEW_API_ROOT_PAT": "test-pat"}), \
                    patch.object(sys, "argv", argv):
                with self.assertRaisesRegex(RuntimeError, "Secret 已有不同凭证"):
                    provision.main()
            self.assertEqual(api.create_count, 0)
            self.assertFalse((base / "bundle.json").exists())

    def test_manifest_rejects_unverified_workflows(self):
        """未核验调用链不能开户；已核验的 Rerank 能力可列入清单。"""
        entry = {"workflow_id": "material-check", "username": "物料校验", "user_quota": 10,
                 "group": "default", "enabled": True, "capabilities": ["chat"],
                 "name_approved": True, "runtime_registered": True, "gateway_verified": True,
                 "direct_vendor_paths": []}
        with tempfile.TemporaryDirectory() as directory:
            manifest = Path(directory) / "manifest.json"
            for changes in ({"username": "english-only"}, {"username": "物料\u200b校验"},
                            {"username": "物料\ud800校验"}, {"user_quota": 0},
                            {"gateway_verified": False}, {"runtime_registered": False},
                            {"name_approved": False}, {"direct_vendor_paths": ["embedding"]},
                            {"capabilities": ["unknown"]}):
                manifest.write_text(json.dumps({"workflows": [{**entry, **changes}]}), encoding="utf-8")
                with self.assertRaises(ValueError):
                    provision.validate_manifest(manifest)
            manifest.write_text(json.dumps({"workflows": [entry]}), encoding="utf-8")
            self.assertEqual(provision.validate_manifest(manifest), [entry])
            rerank_entry = {**entry, "capabilities": ["chat", "rerank"]}
            manifest.write_text(json.dumps({"workflows": [rerank_entry]}), encoding="utf-8")
            self.assertEqual(provision.validate_manifest(manifest), [rerank_entry])


if __name__ == "__main__":
    unittest.main()
