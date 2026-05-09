---
name: sub2api-account-json-fix
description: 按模板批量补全 sub2api 账号导出 JSON，修复 `accounts[].name` 与 `credentials.email` 不一致、缺失 `credentials.model_mapping`、缺失 `extra.allow_overages` 等问题，并把这些账号直接推送到 sub2api admin 创建接口。用于处理 `sub2api-account-*.json` 这类导出文件，尤其适合“先规范化 JSON，再批量推送到 sub2api 指定分组”的场景。
---

# Sub2API 账号 JSON 补全

## 概览

先确认模板 JSON，再用脚本批量修复目标文件。默认修复三件事：

- 把每个账号的 `credentials.model_mapping` 同步成模板账号的值
- 确保每个账号都包含 `extra.allow_overages`
- 把 `name` 改成该账号 `credentials.email`

脚本只改这些字段，不会删除其他 `credentials`、`extra`、代理、并发、优先级或 token 字段。

## 工作流

1. 读取模板文件，默认使用模板中的第一个账号。
2. 确认模板账号里已经有正确的 `credentials.model_mapping` 和 `extra.allow_overages`。
3. 先用预览模式检查哪些文件会被修改。
4. 确认预览结果后，再用 `--write` 真正写回文件。
5. 如果要直推到 sub2api，先检查技能目录里的 `env/push.env`；没有再看系统环境变量；还没有才向用户询问直推地址、token 和目标分组 IDs。
6. 如果 `env/push.env` 里还是示例值
   `SUB2API_ACCOUNT_JSON_FIX_PUSH_ADMIN_BASE_URL=http://127.0.0.1:8080/api/v1/admin`
   和 `SUB2API_ACCOUNT_JSON_FIX_PUSH_TOKEN=admin-xxxx`，把它视为“未配置”，不要直接直推，先向用户索要真实值。
7. 创建账号后，默认立刻关闭调度；如果账号带有 `refresh_token`，默认立刻调用一次 refresh。

## 快速命令

先预览：

```bash
python sub2api-account-json-fix/scripts/fix_exported_account_json.py \
  --template sub2api-account-20260316021320.json
```

真正写回：

```bash
python sub2api-account-json-fix/scripts/fix_exported_account_json.py \
  --template sub2api-account-20260316021320.json \
  --write
```

只检查是否还有待修复项，并用退出码表示结果：

```bash
python sub2api-account-json-fix/scripts/fix_exported_account_json.py \
  --template sub2api-account-20260316021320.json \
  --check
```

把规范化后的账号直接推送到 `sub2api`：

```bash
python sub2api-account-json-fix/scripts/fix_exported_account_json.py \
  --template sub2api-account-20260316021320.json \
  --push \
  --push-admin-base-url http://127.0.0.1:8080/api/v1/admin \
  --push-token 'admin-xxxx' \
  --push-group-id 3 \
  --push-group-id 7
```

也可以预先设置环境变量，调用时不再重复传参：

```bash
export SUB2API_ACCOUNT_JSON_FIX_PUSH_ADMIN_BASE_URL='http://127.0.0.1:8080/api/v1/admin'
export SUB2API_ACCOUNT_JSON_FIX_PUSH_TOKEN='admin-xxxx'
export SUB2API_ACCOUNT_JSON_FIX_PUSH_SCHEDULABLE='false'
export SUB2API_ACCOUNT_JSON_FIX_PUSH_REFRESH_AFTER_CREATE='true'
python sub2api-account-json-fix/scripts/fix_exported_account_json.py \
  --template sub2api-account-20260316021320.json \
  --push
```

更推荐把配置放在技能目录里：

```bash
cat > sub2api-account-json-fix/env/push.env <<'EOF'
SUB2API_ACCOUNT_JSON_FIX_PUSH_ADMIN_BASE_URL=http://127.0.0.1:8080/api/v1/admin
SUB2API_ACCOUNT_JSON_FIX_PUSH_TOKEN=admin-xxxx
SUB2API_ACCOUNT_JSON_FIX_PUSH_GROUP_IDS=3,7
SUB2API_ACCOUNT_JSON_FIX_PUSH_SCHEDULABLE=false
SUB2API_ACCOUNT_JSON_FIX_PUSH_REFRESH_AFTER_CREATE=true
EOF
```

指定部分文件：

```bash
python sub2api-account-json-fix/scripts/fix_exported_account_json.py \
  --template sub2api-account-20260316021320.json \
  sub2api-account-20260316015827.json \
  sub2api-account-20260316020213.json \
  --write
```

## 规则

- 把模板账号 `credentials.model_mapping` 原样复制到目标账号。
- 只补齐 `extra.allow_overages`，不要覆盖目标账号 `extra` 里的其他键。
- 只有当 `credentials.email` 是非空字符串时，才把 `name` 改成邮箱。
- 默认处理当前工作目录里匹配 `sub2api-account-*.json` 的文件，并自动跳过模板自身。
- 脚本支持一个文件里有多个 `accounts`，会逐个账号处理。

## 直推 sub2api

- 直推固定走 `POST /api/v1/admin/proxies` 和 `POST /api/v1/admin/accounts`。
- 脚本会先确保 JSON 里的代理存在，再按账号逐个创建到 sub2api。
- 如果配置了 `group_ids`，会在创建账号时直接带上 `group_ids`。
- 创建账号后默认会再调用一次 `POST /api/v1/admin/accounts/:id/schedulable`，把调度显式设为关闭。
- 如果账号带有 `refresh_token`，创建账号后默认会再调用一次 `POST /api/v1/admin/accounts/:id/refresh`。
- 直推参数优先级是：命令行参数 > 技能目录 `env/push.env` > 系统环境变量。
- 技能目录配置文件路径固定是 `sub2api-account-json-fix/env/push.env`。
- 支持的键有 `SUB2API_ACCOUNT_JSON_FIX_PUSH_ADMIN_BASE_URL`、`SUB2API_ACCOUNT_JSON_FIX_PUSH_TOKEN`、`SUB2API_ACCOUNT_JSON_FIX_PUSH_GROUP_IDS`、`SUB2API_ACCOUNT_JSON_FIX_PUSH_SCHEDULABLE`、`SUB2API_ACCOUNT_JSON_FIX_PUSH_REFRESH_AFTER_CREATE`。
- 如果 `push.env` 仍然保留示例值 `admin-xxxx`，脚本会拒绝直推，并要求先替换为真实配置。
- 如果要直推但命令行、技能目录配置和系统环境变量都没给地址或 token，调用这个技能的代理应先询问用户，再决定是否执行 `--push`。
- `group_ids` 为空时，是否触发 sub2api 默认分组绑定，取决于 sub2api 当前创建接口的行为。

## 资源

- `scripts/fix_exported_account_json.py`：批量补全 JSON 的主脚本。
- `env/push.env`：可选的技能目录本地配置文件；不存在时不会报错。
