#!/usr/bin/env python3
"""按模板补全 sub2api 账号导出 JSON，并可直接推送到 sub2api admin 接口。"""

from __future__ import annotations

import argparse
import copy
import json
import os
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any
from urllib import error as urllib_error
from urllib import parse as urllib_parse
from urllib import request as urllib_request

SKILL_ROOT = Path(__file__).resolve().parent.parent
SKILL_PUSH_ENV_FILE = SKILL_ROOT / "env" / "push.env"

ENV_PUSH_ENV_FILE_OVERRIDE = "SUB2API_ACCOUNT_JSON_FIX_ENV_FILE"
ENV_PUSH_ADMIN_BASE_URL = "SUB2API_ACCOUNT_JSON_FIX_PUSH_ADMIN_BASE_URL"
ENV_PUSH_TOKEN = "SUB2API_ACCOUNT_JSON_FIX_PUSH_TOKEN"
ENV_PUSH_GROUP_IDS = "SUB2API_ACCOUNT_JSON_FIX_PUSH_GROUP_IDS"
ENV_PUSH_SCHEDULABLE = "SUB2API_ACCOUNT_JSON_FIX_PUSH_SCHEDULABLE"
ENV_PUSH_REFRESH_AFTER_CREATE = "SUB2API_ACCOUNT_JSON_FIX_PUSH_REFRESH_AFTER_CREATE"

EXAMPLE_PUSH_ADMIN_BASE_URL = "http://127.0.0.1:8080/api/v1/admin"
EXAMPLE_PUSH_TOKEN = "admin-xxxx"


@dataclass
class TemplateValues:
    """保存从模板账号提取出的补全字段。"""

    model_mapping: dict[str, Any]
    allow_overages: bool


@dataclass
class FileChangeSummary:
    """记录单个文件内的修改统计。"""

    name_fixed: int = 0
    model_mapping_fixed: int = 0
    allow_overages_fixed: int = 0

    def has_changes(self) -> bool:
        """判断当前文件是否发生了任何修改。"""

        return (
            self.name_fixed > 0
            or self.model_mapping_fixed > 0
            or self.allow_overages_fixed > 0
        )


@dataclass
class PushSummary:
    """记录直推 sub2api 的统计结果。"""

    success: int = 0
    failed: int = 0


def parse_args() -> argparse.Namespace:
    """解析命令行参数。"""

    parser = argparse.ArgumentParser(
        description="按模板补全 sub2api 账号导出 JSON 的 model_mapping、allow_overages 和 name，并可直接推送到 sub2api。"
    )
    parser.add_argument(
        "--template",
        required=True,
        help="模板 JSON 文件路径，默认取其第一个账号作为模板来源。",
    )
    parser.add_argument(
        "--template-account-index",
        type=int,
        default=0,
        help="模板文件中作为来源账号的索引，默认 0。",
    )
    parser.add_argument(
        "--glob",
        default="sub2api-account-*.json",
        help="未显式传入目标文件时使用的匹配模式，默认 sub2api-account-*.json。",
    )
    mode_group = parser.add_mutually_exclusive_group()
    mode_group.add_argument(
        "--write",
        action="store_true",
        help="真正写回文件。未指定时只预览。",
    )
    mode_group.add_argument(
        "--check",
        action="store_true",
        help="仅检查是否还有待修复项；存在待修复项时退出码为 1。",
    )
    parser.add_argument(
        "--push",
        action="store_true",
        help="把规范化后的账号直接推送到 sub2api admin 接口。",
    )
    parser.add_argument(
        "--push-admin-base-url",
        help="sub2api admin 基础地址，例如 http://127.0.0.1:8080/api/v1/admin。",
    )
    parser.add_argument(
        "--push-token",
        help="sub2api admin token。若以 Bearer 开头则走 Authorization，否则走 x-api-key。",
    )
    parser.add_argument(
        "--push-group-id",
        action="append",
        type=int,
        default=[],
        help="创建账号时绑定到这些 group_id，可重复传入。",
    )
    parser.add_argument(
        "--push-schedulable",
        choices=("true", "false"),
        help="创建账号后是否保持可调度。默认 false，也就是创建后立即关闭调度。",
    )
    parser.add_argument(
        "--push-refresh-after-create",
        choices=("true", "false"),
        help="创建账号后是否立即调用 refresh。默认 true。",
    )
    parser.add_argument(
        "targets",
        nargs="*",
        help="要处理的目标文件；留空时按 --glob 自动搜索。",
    )
    return parser.parse_args()


def strip_wrapped_quotes(raw_value: str) -> str:
    """去掉包裹值的同类引号。"""

    text = str(raw_value or "").strip()
    if len(text) >= 2 and text[0] == text[-1] and text[0] in {"'", '"'}:
        return text[1:-1]
    return text


def load_env_file(path: Path) -> dict[str, str]:
    """解析简单的 .env 风格文件。"""

    if not path.is_file():
        return {}

    result: dict[str, str] = {}
    for raw_line in path.read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        if line.startswith("export "):
            line = line[7:].strip()
        key, sep, value = line.partition("=")
        if not sep:
            continue
        normalized_key = key.strip()
        if not normalized_key:
            continue
        result[normalized_key] = strip_wrapped_quotes(value)
    return result


def parse_group_ids(raw_value: str) -> list[int]:
    """把逗号分隔的 group_ids 转成整数列表。"""

    result: list[int] = []
    seen: set[int] = set()
    for chunk in str(raw_value or "").split(","):
        item = chunk.strip()
        if not item:
            continue
        try:
            group_id = int(item)
        except ValueError as exc:
            raise ValueError(f"group_id 不是合法整数: {item}") from exc
        if group_id <= 0:
            raise ValueError(f"group_id 必须大于 0: {item}")
        if group_id not in seen:
            seen.add(group_id)
            result.append(group_id)
    return result


def resolve_push_env_file() -> Path:
    """解析当前应该读取的 push.env 路径。"""

    override = str(os.getenv(ENV_PUSH_ENV_FILE_OVERRIDE, "")).strip()
    if not override:
        return SKILL_PUSH_ENV_FILE
    return Path(override).expanduser().resolve()


def parse_env_bool(raw_value: str) -> bool:
    """把环境变量中的布尔字符串转换为布尔值。"""

    normalized = str(raw_value or "").strip().lower()
    return normalized in {"1", "true", "yes", "on"}


def apply_env_defaults(args: argparse.Namespace) -> argparse.Namespace:
    """当命令行未显式传值时，使用技能目录配置和环境变量补全直推配置。"""

    file_env = load_env_file(resolve_push_env_file())
    if not args.push_admin_base_url:
        args.push_admin_base_url = (
            str(file_env.get(ENV_PUSH_ADMIN_BASE_URL, "")).strip()
            or str(os.getenv(ENV_PUSH_ADMIN_BASE_URL, "")).strip()
            or None
        )
    if not args.push_token:
        args.push_token = (
            str(file_env.get(ENV_PUSH_TOKEN, "")).strip()
            or str(os.getenv(ENV_PUSH_TOKEN, "")).strip()
            or None
        )
    if not args.push_group_id:
        raw_group_ids = (
            str(file_env.get(ENV_PUSH_GROUP_IDS, "")).strip()
            or str(os.getenv(ENV_PUSH_GROUP_IDS, "")).strip()
        )
        if raw_group_ids:
            args.push_group_id = parse_group_ids(raw_group_ids)
    if args.push_schedulable is None:
        raw_schedulable = (
            str(file_env.get(ENV_PUSH_SCHEDULABLE, "")).strip()
            or str(os.getenv(ENV_PUSH_SCHEDULABLE, "")).strip()
        )
        if raw_schedulable:
            args.push_schedulable = "true" if parse_env_bool(raw_schedulable) else "false"
        else:
            args.push_schedulable = "false"
    if args.push_refresh_after_create is None:
        raw_refresh_after_create = (
            str(file_env.get(ENV_PUSH_REFRESH_AFTER_CREATE, "")).strip()
            or str(os.getenv(ENV_PUSH_REFRESH_AFTER_CREATE, "")).strip()
        )
        if raw_refresh_after_create:
            args.push_refresh_after_create = "true" if parse_env_bool(raw_refresh_after_create) else "false"
        else:
            args.push_refresh_after_create = "true"
    return args


def is_placeholder_push_config(args: argparse.Namespace) -> bool:
    """判断当前直推配置是否仍然是示例占位值。"""

    admin_base_url = str(args.push_admin_base_url or "").strip()
    token = str(args.push_token or "").strip()
    if token == EXAMPLE_PUSH_TOKEN:
        return True
    return admin_base_url == EXAMPLE_PUSH_ADMIN_BASE_URL and token == EXAMPLE_PUSH_TOKEN


def load_json(path: Path) -> Any:
    """读取并解析 JSON 文件。"""

    with path.open("r", encoding="utf-8") as handle:
        return json.load(handle)


def write_json(path: Path, payload: Any) -> None:
    """以统一格式写回 JSON 文件。"""

    text = json.dumps(payload, ensure_ascii=False, indent=2)
    path.write_text(text + "\n", encoding="utf-8")


def build_target_paths(args: argparse.Namespace, template_path: Path) -> list[Path]:
    """构建要处理的目标文件列表，并自动跳过模板文件。"""

    if args.targets:
        raw_paths = [Path(item) for item in args.targets]
    else:
        raw_paths = sorted(Path.cwd().glob(args.glob))

    seen: set[Path] = set()
    result: list[Path] = []
    template_resolved = template_path.resolve()
    for raw_path in raw_paths:
        resolved = raw_path.resolve()
        if resolved == template_resolved:
            continue
        if resolved in seen:
            continue
        seen.add(resolved)
        result.append(raw_path)
    return result


def extract_template_values(template_data: dict[str, Any], account_index: int) -> TemplateValues:
    """从模板文件中提取补全时要使用的字段。"""

    accounts = template_data.get("accounts")
    if not isinstance(accounts, list) or not accounts:
        raise ValueError("模板文件缺少非空的 accounts 数组")
    if account_index < 0 or account_index >= len(accounts):
        raise ValueError(f"模板账号索引越界: {account_index}")

    template_account = accounts[account_index]
    if not isinstance(template_account, dict):
        raise ValueError("模板账号结构无效")

    credentials = template_account.get("credentials")
    if not isinstance(credentials, dict):
        raise ValueError("模板账号缺少 credentials 对象")

    model_mapping = credentials.get("model_mapping")
    if not isinstance(model_mapping, dict) or not model_mapping:
        raise ValueError("模板账号缺少有效的 credentials.model_mapping")

    extra = template_account.get("extra")
    if not isinstance(extra, dict):
        raise ValueError("模板账号缺少 extra 对象")

    allow_overages = extra.get("allow_overages")
    if not isinstance(allow_overages, bool):
        raise ValueError("模板账号缺少布尔类型的 extra.allow_overages")

    return TemplateValues(
        model_mapping=copy.deepcopy(model_mapping),
        allow_overages=allow_overages,
    )


def replace_or_insert_key(
    payload: dict[str, Any],
    key: str,
    value: Any,
    *,
    insert_after: tuple[str, ...] = (),
) -> tuple[dict[str, Any], bool]:
    """替换已有键或按指定相对位置插入新键。"""

    if key in payload:
        if payload[key] == value:
            return payload, False
        updated: dict[str, Any] = {}
        for existing_key, existing_value in payload.items():
            if existing_key == key:
                updated[existing_key] = copy.deepcopy(value)
            else:
                updated[existing_key] = existing_value
        return updated, True

    updated = {}
    inserted = False
    for existing_key, existing_value in payload.items():
        updated[existing_key] = existing_value
        if not inserted and existing_key in insert_after:
            updated[key] = copy.deepcopy(value)
            inserted = True
    if not inserted:
        updated[key] = copy.deepcopy(value)
    return updated, True


def normalize_account(
    account: dict[str, Any],
    template_values: TemplateValues,
) -> FileChangeSummary:
    """补全单个账号对象并返回修改统计。"""

    summary = FileChangeSummary()

    credentials = account.get("credentials")
    if not isinstance(credentials, dict):
        raise ValueError("账号缺少 credentials 对象")

    updated_credentials, changed = replace_or_insert_key(
        credentials,
        "model_mapping",
        template_values.model_mapping,
        insert_after=("expires_at", "email"),
    )
    if changed:
        account["credentials"] = updated_credentials
        credentials = updated_credentials
        summary.model_mapping_fixed += 1

    email = credentials.get("email")
    if isinstance(email, str):
        normalized_email = email.strip()
        if normalized_email and account.get("name") != normalized_email:
            account["name"] = normalized_email
            summary.name_fixed += 1

    extra = account.get("extra")
    if extra is None:
        extra = {}
    if not isinstance(extra, dict):
        raise ValueError("账号 extra 不是对象")

    updated_extra, changed = replace_or_insert_key(
        extra,
        "allow_overages",
        template_values.allow_overages,
    )
    if changed or account.get("extra") is None:
        account["extra"] = updated_extra
        summary.allow_overages_fixed += 1

    return summary


def normalize_file(path: Path, template_values: TemplateValues) -> tuple[dict[str, Any], FileChangeSummary]:
    """补全单个 JSON 文件中的所有账号。"""

    payload = load_json(path)
    if not isinstance(payload, dict):
        raise ValueError("JSON 根节点必须是对象")

    accounts = payload.get("accounts")
    if not isinstance(accounts, list):
        raise ValueError("JSON 缺少 accounts 数组")

    total = FileChangeSummary()
    for account in accounts:
        if not isinstance(account, dict):
            raise ValueError("accounts 中存在非对象元素")
        item_summary = normalize_account(account, template_values)
        total.name_fixed += item_summary.name_fixed
        total.model_mapping_fixed += item_summary.model_mapping_fixed
        total.allow_overages_fixed += item_summary.allow_overages_fixed

    return payload, total


def print_summary(path: Path, summary: FileChangeSummary, *, action: str) -> None:
    """打印单个文件的处理摘要。"""

    print(
        f"[{action}] {path}: "
        f"name={summary.name_fixed}, "
        f"model_mapping={summary.model_mapping_fixed}, "
        f"allow_overages={summary.allow_overages_fixed}"
    )


class Sub2APIClient:
    """简易 sub2api admin API 客户端。"""

    def __init__(self, admin_base_url: str, token: str) -> None:
        self.admin_base_url = self.normalize_admin_base_url(admin_base_url)
        self.token = str(token or "").strip()

    @staticmethod
    def normalize_admin_base_url(admin_base_url: str) -> str:
        """兼容传入 admin 根路径或具体 accounts/proxies 路径。"""

        url = str(admin_base_url or "").rstrip("/")
        for suffix in ("/accounts", "/proxies"):
            if url.endswith(suffix):
                return url[: -len(suffix)]
        return url

    def request(
        self,
        method: str,
        path: str,
        *,
        payload: Any | None = None,
        query: dict[str, Any] | None = None,
    ) -> Any:
        """发起一次 JSON 请求并返回解包后的 data。"""

        url = self.admin_base_url + path
        if query:
            normalized_query = {
                key: value
                for key, value in query.items()
                if value is not None and value != ""
            }
            if normalized_query:
                url += "?" + urllib_parse.urlencode(normalized_query)

        headers = {
            "Accept": "application/json, text/plain, */*",
        }
        if payload is not None:
            body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
            headers["Content-Type"] = "application/json"
        else:
            body = None

        if self.token.lower().startswith("bearer "):
            headers["Authorization"] = self.token
        else:
            headers["x-api-key"] = self.token

        request = urllib_request.Request(url=url, data=body, headers=headers, method=method.upper())
        try:
            with urllib_request.urlopen(request, timeout=30) as response:
                raw = response.read().decode("utf-8")
                return self._unwrap_response(raw)
        except urllib_error.HTTPError as exc:
            error_body = exc.read().decode("utf-8", errors="replace")
            raise RuntimeError(self._format_error(exc.code, error_body)) from exc
        except urllib_error.URLError as exc:
            raise RuntimeError(f"请求失败: {exc.reason}") from exc

    @staticmethod
    def _unwrap_response(raw_text: str) -> Any:
        """解包 sub2api 标准响应格式。"""

        if not raw_text.strip():
            return None
        payload = json.loads(raw_text)
        if isinstance(payload, dict) and "code" in payload:
            if payload.get("code") == 0:
                return payload.get("data")
            raise RuntimeError(str(payload.get("message", "接口返回错误")))
        return payload

    @staticmethod
    def _format_error(status_code: int, raw_text: str) -> str:
        """把 HTTP 错误响应整理成可读文本。"""

        try:
            payload = json.loads(raw_text)
        except json.JSONDecodeError:
            payload = None

        if isinstance(payload, dict):
            message = payload.get("message")
            reason = payload.get("reason")
            if message and reason:
                return f"HTTP {status_code}: {message} ({reason})"
            if message:
                return f"HTTP {status_code}: {message}"
        compact = raw_text.strip().replace("\n", " ")
        if compact:
            return f"HTTP {status_code}: {compact[:300]}"
        return f"HTTP {status_code}"


def validate_push_args(args: argparse.Namespace) -> None:
    """校验直推模式必需参数。"""

    if not args.push:
        return
    push_env_file = resolve_push_env_file()
    if not args.push_admin_base_url:
        raise ValueError(
            "启用 --push 时必须提供 --push-admin-base-url，或在技能目录配置 "
            f"{push_env_file}，或设置环境变量 {ENV_PUSH_ADMIN_BASE_URL}"
        )
    if not args.push_token:
        raise ValueError(
            "启用 --push 时必须提供 --push-token，或在技能目录配置 "
            f"{push_env_file}，或设置环境变量 {ENV_PUSH_TOKEN}"
        )
    if is_placeholder_push_config(args):
        raise ValueError(
            "检测到直推配置仍是示例占位值，请先让用户提供真实的 sub2api admin 地址和 token，"
            f"再更新 {push_env_file} 或命令行参数后重试"
        )
    for group_id in args.push_group_id:
        if group_id <= 0:
            raise ValueError(f"group_id 必须大于 0: {group_id}")


def build_proxy_key(protocol: str, host: str, port: int, username: str, password: str) -> str:
    """按 sub2api 的规则生成 proxy_key。"""

    return "|".join(
        [
            str(protocol or "").strip(),
            str(host or "").strip(),
            str(port),
            str(username or "").strip(),
            str(password or "").strip(),
        ]
    )


def build_proxy_key_from_item(item: dict[str, Any]) -> str:
    """从代理对象中提取或构造 proxy_key。"""

    proxy_key = str(item.get("proxy_key", "") or "").strip()
    if proxy_key:
        return proxy_key
    return build_proxy_key(
        str(item.get("protocol", "") or ""),
        str(item.get("host", "") or ""),
        int(item.get("port", 0) or 0),
        str(item.get("username", "") or ""),
        str(item.get("password", "") or ""),
    )


def list_remote_proxies(client: Sub2APIClient) -> dict[str, dict[str, Any]]:
    """读取远端全部代理，并按 proxy_key 建立索引。"""

    page = 1
    page_size = 1000
    result: dict[str, dict[str, Any]] = {}
    while True:
        payload = client.request(
            "GET",
            "/proxies",
            query={"page": page, "page_size": page_size},
        )
        if not isinstance(payload, dict):
            raise RuntimeError("代理列表响应格式不正确")
        items = payload.get("items")
        if not isinstance(items, list):
            raise RuntimeError("代理列表缺少 items")
        for item in items:
            if not isinstance(item, dict):
                continue
            key = build_proxy_key(
                str(item.get("protocol", "") or ""),
                str(item.get("host", "") or ""),
                int(item.get("port", 0) or 0),
                str(item.get("username", "") or ""),
                str(item.get("password", "") or ""),
            )
            result[key] = item
        total = int(payload.get("total", 0) or 0)
        if len(items) == 0 or page * page_size >= total:
            break
        page += 1
    return result


def ensure_remote_proxy(
    client: Sub2APIClient,
    remote_proxies: dict[str, dict[str, Any]],
    item: dict[str, Any],
) -> int:
    """确保远端存在目标代理，并返回 proxy_id。"""

    key = build_proxy_key_from_item(item)
    existing = remote_proxies.get(key)
    desired_status = str(item.get("status", "") or "").strip().lower()
    if existing is None:
        created = client.request(
            "POST",
            "/proxies",
            payload={
                "name": str(item.get("name", "") or "imported-proxy"),
                "protocol": str(item.get("protocol", "") or "").strip(),
                "host": str(item.get("host", "") or "").strip(),
                "port": int(item.get("port", 0) or 0),
                "username": str(item.get("username", "") or "").strip(),
                "password": str(item.get("password", "") or "").strip(),
            },
        )
        if not isinstance(created, dict):
            raise RuntimeError("创建代理返回格式不正确")
        existing = created
        remote_proxies[key] = existing

    proxy_id = int(existing.get("id", 0) or 0)
    if proxy_id <= 0:
        raise RuntimeError("远端代理缺少有效 id")

    current_status = str(existing.get("status", "") or "").strip().lower()
    if desired_status in {"active", "inactive"} and current_status != desired_status:
        updated = client.request(
            "PUT",
            f"/proxies/{proxy_id}",
            payload={"status": desired_status},
        )
        if isinstance(updated, dict):
            remote_proxies[key] = updated
    return proxy_id


def build_create_account_payload(
    account: dict[str, Any],
    proxy_id: int | None,
    group_ids: list[int],
) -> dict[str, Any]:
    """把导出 JSON 中的账号对象转换为创建接口所需 payload。"""

    payload = {
        "name": account.get("name"),
        "notes": account.get("notes"),
        "platform": account.get("platform"),
        "type": account.get("type"),
        "credentials": account.get("credentials"),
        "extra": account.get("extra") or {},
        "proxy_id": proxy_id,
        "concurrency": int(account.get("concurrency", 0) or 0),
        "priority": int(account.get("priority", 0) or 0),
        "rate_multiplier": account.get("rate_multiplier"),
        "expires_at": account.get("expires_at"),
        "auto_pause_on_expired": account.get("auto_pause_on_expired", True),
    }
    if group_ids:
        payload["group_ids"] = group_ids
    return payload


def set_account_schedulable(client: Sub2APIClient, account_id: int, schedulable: bool) -> None:
    """创建账号后显式设置调度开关。"""

    client.request(
        "POST",
        f"/accounts/{account_id}/schedulable",
        payload={"schedulable": schedulable},
    )


def should_refresh_after_create(account: dict[str, Any], refresh_after_create: bool) -> bool:
    """判断当前账号创建后是否应触发 refresh。"""

    if not refresh_after_create:
        return False
    if str(account.get("type", "") or "").strip() != "oauth":
        return False
    credentials = account.get("credentials")
    if not isinstance(credentials, dict):
        return False
    refresh_token = str(credentials.get("refresh_token", "") or "").strip()
    return refresh_token != ""


def refresh_account_after_create(client: Sub2APIClient, account_id: int) -> None:
    """创建账号后触发一次远端 refresh。"""

    client.request("POST", f"/accounts/{account_id}/refresh", payload={})


def push_files_by_create(
    client: Sub2APIClient,
    prepared_files: list[tuple[Path, dict[str, Any], FileChangeSummary]],
    args: argparse.Namespace,
) -> PushSummary:
    """通过 `/accounts` 逐个创建账号到 sub2api。"""

    summary = PushSummary()
    remote_proxies = list_remote_proxies(client)
    target_schedulable = str(args.push_schedulable or "false").lower() == "true"
    refresh_after_create = str(args.push_refresh_after_create or "true").lower() == "true"

    for path, payload, _ in prepared_files:
        proxies = payload.get("proxies")
        accounts = payload.get("accounts")
        if not isinstance(proxies, list) or not isinstance(accounts, list):
            print(f"[错误] {path}: 不是合法的 sub2api bundle JSON", file=sys.stderr)
            summary.failed += 1
            continue

        proxy_id_by_key: dict[str, int] = {}
        for item in proxies:
            if not isinstance(item, dict):
                continue
            try:
                proxy_id_by_key[build_proxy_key_from_item(item)] = ensure_remote_proxy(
                    client, remote_proxies, item
                )
            except Exception as exc:  # noqa: BLE001
                summary.failed += 1
                print(
                    f"[错误] {path}: 代理 {item.get('name') or item.get('host') or '-'} 推送失败: {exc}",
                    file=sys.stderr,
                )

        for account in accounts:
            if not isinstance(account, dict):
                summary.failed += 1
                print(f"[错误] {path}: accounts 中存在非法元素", file=sys.stderr)
                continue

            try:
                proxy_key = str(account.get("proxy_key", "") or "").strip()
                proxy_id = proxy_id_by_key.get(proxy_key) if proxy_key else None
                if proxy_key and proxy_id is None:
                    raise RuntimeError(f"找不到对应代理: {proxy_key}")
                created = client.request(
                    "POST",
                    "/accounts",
                    payload=build_create_account_payload(account, proxy_id, args.push_group_id),
                )
                if not isinstance(created, dict):
                    raise RuntimeError("创建账号返回格式不正确")
                account_id = int(created.get("id", 0) or 0)
                if account_id <= 0:
                    raise RuntimeError("创建账号成功但返回中缺少有效 id")
                set_account_schedulable(client, account_id, target_schedulable)
                refresh_status = "skipped"
                if should_refresh_after_create(account, refresh_after_create):
                    refresh_account_after_create(client, account_id)
                    refresh_status = "triggered"
                summary.success += 1
                group_text = ",".join(str(item) for item in args.push_group_id) if args.push_group_id else "-"
                print(
                    f"[直推/create] {path}: "
                    f"account_id={account_id}, "
                    f"name={created.get('name', account.get('name', ''))}, "
                    f"group_ids={group_text}, "
                    f"schedulable={str(target_schedulable).lower()}, "
                    f"refresh={refresh_status}"
                )
            except Exception as exc:  # noqa: BLE001
                summary.failed += 1
                print(
                    f"[错误] {path}: 账号 {account.get('name') or '-'} 推送失败: {exc}",
                    file=sys.stderr,
                )

    return summary


def push_prepared_files(
    args: argparse.Namespace,
    prepared_files: list[tuple[Path, dict[str, Any], FileChangeSummary]],
) -> PushSummary:
    """把已规范化的数据推送到 sub2api。"""

    validate_push_args(args)
    client = Sub2APIClient(args.push_admin_base_url, args.push_token)
    return push_files_by_create(client, prepared_files, args)


def main() -> int:
    """执行入口。"""

    args = apply_env_defaults(parse_args())
    template_path = Path(args.template)
    if not template_path.is_file():
        print(f"模板文件不存在: {template_path}", file=sys.stderr)
        return 2

    try:
        template_data = load_json(template_path)
        if not isinstance(template_data, dict):
            raise ValueError("模板 JSON 根节点必须是对象")
        template_values = extract_template_values(template_data, args.template_account_index)
    except Exception as exc:  # noqa: BLE001
        print(f"读取模板失败: {exc}", file=sys.stderr)
        return 2

    target_paths = build_target_paths(args, template_path)
    if not target_paths:
        print("没有找到需要处理的目标文件", file=sys.stderr)
        return 2

    changed_files = 0
    had_error = False
    action = "写入" if args.write else "预览"
    prepared_files: list[tuple[Path, dict[str, Any], FileChangeSummary]] = []

    for path in target_paths:
        try:
            payload, summary = normalize_file(path, template_values)
        except Exception as exc:  # noqa: BLE001
            had_error = True
            print(f"[错误] {path}: {exc}", file=sys.stderr)
            continue

        if not summary.has_changes():
            print_summary(path, summary, action="跳过")
        else:
            changed_files += 1
            if args.write:
                write_json(path, payload)
            print_summary(path, summary, action=action)
        prepared_files.append((path, payload, summary))

    print(f"总目标文件: {len(target_paths)}")
    print(f"发生修改的文件: {changed_files}")

    if had_error:
        return 2
    if args.push:
        try:
            push_summary = push_prepared_files(args, prepared_files)
        except Exception as exc:  # noqa: BLE001
            print(f"直推失败: {exc}", file=sys.stderr)
            return 2
        print(f"直推成功: {push_summary.success}")
        print(f"直推失败: {push_summary.failed}")
        if push_summary.failed > 0:
            return 2
    if args.check and changed_files > 0:
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
