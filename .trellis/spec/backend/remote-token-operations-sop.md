# 远程令牌运维 SOP

> 记录一次性远程令牌开通、用户迁移后修正、以及同步到外部 Cloudflare D1 `api_keys` 表的可执行流程。
> 本文只写流程和模板，禁止记录真实密码、真实 API Key、Cloudflare Token 或客户隐私数据。

---

## 场景：批量创建令牌并同步到外部 Key 库

### 1. Scope / Trigger

- Trigger: 需要在远程 `new-api` Docker 环境中为指定用户或指定名单批量创建 API key，并把同一批 key 写入外部系统（例如 `ai-fund` 的 Cloudflare D1 `apikey-db.api_keys`）。
- 适用范围:
  - 远程只读核对 Docker 容器、`SQL_DSN`、数据库表结构。
  - 批量写 `tokens` / `users.setting`。
  - 用户迁移后验证 token 当前归属用户已开启 IP 记录，必要时修复历史数据。
  - 同步 `name + api_key` 到 Cloudflare D1。
- 不适用范围:
  - 修改项目代码、表结构或正式迁移逻辑。
  - 记录或提交真实密钥文件。

### 2. Signatures

- `new-api` 运行库连接:

```bash
docker exec new-api sh -lc 'printenv | grep -Ei "SQL_DSN|DB|DATABASE|MYSQL|POSTGRES"'
```

- MySQL 只读/写入执行入口:

```bash
docker exec -i mysql sh -lc 'MYSQL_PWD=<password> mysql -uroot -D new-api --default-character-set=utf8mb4'
```

- 令牌表核心字段:

```sql
tokens(
  id,
  user_id,
  `key`,
  status,
  name,
  created_time,
  accessed_time,
  expired_time,
  remain_quota,
  unlimited_quota,
  model_limits_enabled,
  model_limits,
  allow_ips,
  used_quota,
  `group`,
  cross_group_retry,
  deleted_at
)
```

- 用户 IP 记录设置字段:

```sql
users.setting JSON path '$.record_ip_log'
```

- 外部 D1 key 表:

```sql
api_keys(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  api_key TEXT NOT NULL,
  created_at TEXT DEFAULT (datetime('now'))
)
```

- Cloudflare D1 执行入口:

```bash
npx wrangler d1 execute apikey-db --remote --json --command '<sql>'
npx wrangler d1 execute apikey-db --remote --json --file=<file.sql>
```

### 3. Contracts

- `tokens.key` 存储合同:
  - 数据库只存 48 位原始 key，不存 `sk-` 前缀。
  - 对外展示和分发时使用 `sk-` + 原始 key。
  - 应用校验会去掉入站 `sk-` 前缀后查 `tokens.key`。
- key 生成合同:
  - 必须匹配代码 `common.GenerateKey()`，即 `GenerateRandomCharsKey(48)`。
  - 字符集必须是 `0-9a-zA-Z`。
  - 禁止用 `HEX(RANDOM_BYTES(24))` 代替；它虽然可用，但只生成 `0-9A-F`，和应用格式不一致。
- 额度合同:
  - UI 中“额度 1000”通常表示显示单位金额，不是库内原始 quota。
  - 必须先从运行时 `/api/status` 或配置确认 `quota_per_unit` 和 `quota_display_type`。
  - `remain_quota = display_amount * quota_per_unit`；例如 `quota_per_unit=500000` 时，`1000` 写为 `500000000`。
- 永不过期合同:
  - `expired_time = -1`。
  - `unlimited_quota = 0` 表示有限额度；若使用无限额度，另按产品语义设置。
- 分组合同:
  - 用户分组在 `users.group`。
  - token 分组在 `tokens.group`。
  - 批量迁移时要同时核对用户分组和 token 分组，不能只看其中一个。
- IP 记录合同:
  - IP 记录是用户级设置，不是 token 级设置。
  - 只有 `users.setting.record_ip_log = true` 时，请求/错误日志才记录 IP。
  - 字段缺失、空 setting、无效 JSON 或 false 都等价于未开启。
  - 代码内置迁移接口 `MigrateTokensToAccounts` 创建独立账号时必须默认写入 `record_ip_log=true`。
  - 历史数据、手工迁移或异常恢复后，仍必须按 token 当前 `user_id` 重新检查并修复对应用户。
- Cloudflare D1 同步合同:
  - `api_keys.name` 使用 token 名，例如 `{姓名}-cop`。
  - `api_keys.api_key` 使用带 `sk-` 前缀的完整 key。
  - 远程 D1 表无唯一约束时，必须先查重，再用 `INSERT ... WHERE NOT EXISTS` 防止重复。

### 4. Validation & Error Matrix

| 条件 | 处理 |
|------|------|
| 远程无法 SSH / Docker 不可用 | 停止操作，不猜数据库位置 |
| `SQL_DSN` 指向未知库 | 先列库和表结构，不写入 |
| `users` / `tokens` 字段与预期不同 | 停止，重新读取实体/表定义 |
| token 名已存在且未删除 | 不重复创建；改为更新或询问业务意图 |
| key 碰撞 | 重新生成；插入前必须查 `tokens.key` |
| key 不匹配 `^[0-9A-Za-z]{48}$` | 不入库，重新生成 |
| quota 单位不明确 | 查 `/api/status` 的 `quota_per_unit` 后再换算 |
| setting 为空或无效 JSON | 用 `JSON_OBJECT('record_ip_log', true)` 初始化 |
| setting 有效但缺字段 | 用 `JSON_SET(setting, '$.record_ip_log', true)` 保留其他字段 |
| D1 `WITH ... UNION ALL` 触发 `too many terms in compound SELECT` | 改用 `WHERE name IN (...)` 或逐条语句 |
| D1 远程执行临时表报 `SQLITE_AUTH` | 不用临时表，改用普通 `INSERT ... WHERE NOT EXISTS` |
| Wrangler 未登录或账号不匹配 | 停止，先确认 `npx wrangler whoami` |

### 5. Good/Base/Bad Cases

- Good: 先读 `SQL_DSN`、表结构、目标用户、重复 token 名、`quota_per_unit`，生成 48 位字母数字 key，事务插入 `tokens`，再复核数量/格式/额度/过期时间，最后同步 D1 并查重。
- Base: 只需要修复历史 IP 记录时，先按 token 当前 owner 聚合 `user_id`，只更新这些用户的 `users.setting.record_ip_log`。
- Bad: 直接用 `HEX(RANDOM_BYTES(24))` 创建 key；直接把“1000”写进 `remain_quota`；迁移后仍只检查原用户的 `record_ip_log`；D1 里不查重直接插入。

### 6. Tests Required

- 运维前只读断言:
  - `SHOW TABLES` 包含 `users`、`tokens`。
  - `DESCRIBE users/tokens` 字段和本文合同一致。
  - 目标 token 名在目标用户下不存在，或明确为更新路径。
  - `/api/status` 返回的 `quota_per_unit` 已记录到操作说明中。
- 写入后数据库断言:
  - 新建 token 数量等于名单数量。
  - `CHAR_LENGTH(tokens.key)=48`。
  - `tokens.key REGEXP '^[0-9A-Za-z]{48}$'` 全部通过。
  - `tokens.group`、`remain_quota`、`expired_time`、`status` 与需求一致。
  - token 当前 owner 的 `users.setting.record_ip_log` 全部为 true。
  - 迁移接口单测必须断言新建用户 `GetSetting().RecordIpLog == true`。
- D1 同步断言:
  - `matched_count` 等于名单数量。
  - `name` 没有重复记录。
  - `api_key` 与 `new-api` 最终展示 key 完全一致。

### 7. Wrong vs Correct

#### Wrong

```sql
-- 只会得到 0-9A-F，格式不像应用生成的 key。
SELECT HEX(RANDOM_BYTES(24));
```

#### Correct

```python
import secrets

chars = '0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ'
key = ''.join(secrets.choice(chars) for _ in range(48))
```

#### Wrong

```sql
-- 迁移后仍检查旧 owner，实际新用户不会记录 IP。
SELECT JSON_EXTRACT(setting, '$.record_ip_log')
FROM users
WHERE username = 'old-owner';
```

#### Correct

```sql
SELECT DISTINCT u.id, u.username, JSON_UNQUOTE(JSON_EXTRACT(u.setting, '$.record_ip_log')) AS record_ip_log
FROM tokens t
JOIN users u ON u.id = t.user_id
WHERE t.deleted_at IS NULL
  AND t.name IN (<token_names>);
```

## 推荐操作模板

### 1. 发现运行库和目标用户

```sql
SELECT id, username, display_name, email, `group`, status, quota, used_quota, deleted_at
FROM users
WHERE username = '<owner>' OR display_name = '<owner>' OR email = '<owner>';
```

### 2. 确认运行时额度单位

```bash
docker exec new-api sh -lc 'wget -qO- http://127.0.0.1:3000/api/status 2>/dev/null || curl -s http://127.0.0.1:3000/api/status'
```

### 3. 插入 token 后复核格式

```sql
SELECT
  COUNT(*) AS checked_count,
  SUM(CASE WHEN CHAR_LENGTH(`key`) = 48 THEN 1 ELSE 0 END) AS length_48_count,
  SUM(CASE WHEN `key` REGEXP '^[0-9A-Za-z]{48}$' THEN 1 ELSE 0 END) AS code_rule_match_count
FROM tokens
WHERE user_id = <user_id>
  AND deleted_at IS NULL
  AND name IN (<token_names>);
```

### 4. 迁移后验证并修复 token 当前 owner 的 IP 记录

正常代码路径应已在创建独立账号时默认开启 IP 记录；以下 SQL 只用于历史数据补救、手工迁移后兜底或线上复核发现缺失时修复。

```sql
UPDATE users u
JOIN (
  SELECT DISTINCT t.user_id
  FROM tokens t
  WHERE t.deleted_at IS NULL
    AND t.name IN (<token_names>)
) target ON target.user_id = u.id
SET u.setting = CASE
  WHEN u.setting IS NULL OR TRIM(u.setting) = '' OR NOT JSON_VALID(u.setting)
    THEN JSON_OBJECT('record_ip_log', true)
  ELSE JSON_SET(u.setting, '$.record_ip_log', true)
END
WHERE u.deleted_at IS NULL;
```

### 5. 同步 Cloudflare D1

```sql
INSERT INTO api_keys (name, api_key)
SELECT '<name>', '<sk-prefixed-key>'
WHERE NOT EXISTS (SELECT 1 FROM api_keys WHERE name = '<name>');
```

执行前后都必须查询:

```sql
SELECT id, name, api_key, created_at
FROM api_keys
WHERE name IN (<names>)
ORDER BY name, id;
```
