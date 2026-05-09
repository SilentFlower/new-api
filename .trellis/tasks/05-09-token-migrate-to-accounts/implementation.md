# 实现摘要：令牌迁移到独立账号

按 PRD 切片落地，全部代码已通过 `go vet ./...` / `go build ./...` 与新增单元测试。

---

## 切片 1：后端 model 层（`/root/project/new-api/model/token_migrate.go`）

新增导出函数：

- `GetTokensForMigration(ids []int, userId int) ([]Token, error)`：拉取指定用户名下、指定 ID 列表的令牌全字段（与 `GetTokenKeysByIds` 不同，本函数不限制 Select 列）。
- `RefreshTokenCache(token Token) error`：薄包装 `cacheSetToken`，用于事务提交后异步刷新 Redis。
- `MigrateTokenOwnership(tx *gorm.DB, tokenId, srcUserId, newUserId int) error`：
  - 在事务内执行 `tx.Model(&Token{}).Where("id = ? AND user_id = ?", ...).Update("user_id", newUserId)`；
  - 校验 `RowsAffected == 1`，否则返回错误（防止误改他人令牌、防止幂等漏更）；
  - 边界参数（0 值、tx 为空、src == new）都做了显式校验。
- `BuildMigratedUsername(tx *gorm.DB, tokenName string, tokenId int, assigned map[string]bool) (string, error)`：
  - `TrimSpace` → 空名兜底 `token_<id>` → `runeTruncate` 到 ≤ 20 rune（**按 rune 而非 byte，中文不会切坏**）；
  - DB 查重（`Unscoped()` 含软删除用户）+ assigned 集合查重；
  - 冲突时追加 `_2 / _3 / ...`，保证 `composeCandidate` 在加后缀时进一步压缩主体确保 ≤ 20 rune；
  - 最多 100 次仍冲突 → 返回错误，由超管修改令牌名后重试。

**关键点 review**：
- `BuildMigratedUsername` 不会主动写回 `assigned`，保持职责单一；调用方在「事务提交成功」后自行更新 assigned，避免事务回滚导致的名字泄漏。

---

## 切片 2：后端 controller 层（`/root/project/new-api/controller/token_migrate.go`）

新增 handler `MigrateTokensToAccounts(c *gin.Context)`：

1. **双重权限校验**：handler 内再次 `c.GetInt("role") == common.RoleRootUser`，作为 `RootAuth` 中间件的纵深防御。
2. **请求体强约束**：`migrateTokensRequest` 只读 `token_ids`，role/quota/group 等敏感字段全部由后端推导。
3. **批次大小限制**：`MigrateTokensBatchMaxSize = 100`（与 `GetTokenKeysBatch` 一致）。
4. **逐令牌独立事务**：
   - `model.DB.Transaction(...)` 包住「BuildMigratedUsername → 创建新 user → 强制更新 quota → MigrateTokenOwnership」；
   - 任一步骤失败即整体回滚；
   - 单令牌失败仅记录到结果数组并继续下一个。
5. **Quota 强制覆盖**：`InsertWithTx` 内部会把 `user.Quota` 覆写为 `common.QuotaForNewUser`，所以紧接着用 `tx.Model(&User{}).Where("id = ?", ...).Update("quota", newQuota)` 把目标值写回去。
6. **缓存刷新**：事务提交后异步 `gopool.Go` 调 `model.RefreshTokenCache`（基于 `cacheSetToken`），传入「已经更新过 UserId 的 token 副本」（避免 goroutine 闭包竞态，先取地址解引用拷贝）。
7. **审计日志**：`model.RecordLog(srcUserId, model.LogTypeManage, ...)` 仅含 token id、token name、新 username、新 user id；**不含密码**。
8. **响应**：`{success: true, data: {results: [{token_id, token_name, new_username?, new_user_id?, status, error?}]}}`，**永远不含密码字段**。

**关键点 review**：
- 密码：`common.GetRandomString(16)` 在事务外生成（顺便避免 bcrypt 在 tx 内长时间持有连接），但仅入库 bcrypt 哈希，绝不返回前端、绝不写日志。
- Group 回退：`if tk.Group == "" || tk.Group == "auto" { newGroup = srcUser.Group }`。
- Quota 回退：`if tk.UnlimitedQuota { newQuota = int(1000000000 * common.QuotaPerUnit) }`，与 `controller/token.go` 的 `maxQuotaValue` 同源。

---

## 切片 3：后端路由 + i18n

### 路由（`/root/project/new-api/router/api-router.go`）

在 `tokenRoute` 组内新增一行（保持与其它令牌接口的组织一致性）：

```go
tokenRoute.POST("/migrate", middleware.RootAuth(), middleware.CriticalRateLimit(), controller.MigrateTokensToAccounts)
```

`tokenRoute` 自带 `UserAuth()`，叠加 `RootAuth()` 后效果是「先验普通用户登录、再验 Root 角色」，按声明顺序执行，无副作用。

### i18n（`/root/project/new-api/i18n/keys.go` + `locales/{zh-CN,zh-TW,en}.yaml`）

新增 5 个 key 常量：
- `MsgTokenMigrateForbidden` / `MsgTokenMigrateInvalidIds` / `MsgTokenMigrateBatchTooMany` / `MsgTokenMigrateNotFound` / `MsgTokenMigrateUsernameConflict`

每个 key 在 zh-CN / zh-TW / en 三个 yaml 文件中都补全了对应翻译。

---

## 切片 4：前端

### 新增组件（`/root/project/new-api/web/src/components/table/tokens/modals/MigrateToAccountsModal.jsx`）

二步弹窗组件：
- **Step 1（confirm）**：`Banner` 警示文案 + 选中令牌简表（令牌名 + masked key）+ 「确认迁移」按钮；
- **Step 2（result）**：成功 / 失败统计 Banner + 结果表格（令牌名 / 新用户名 / 状态），失败行红色 + tooltip 显示 error；
- 加载期间禁止 mask close / Esc close，防止误关；
- `onSuccess` 在 step=result 时触发关闭，触发父组件刷新列表。

### 修改的现有文件

- `web/src/components/table/tokens/TokensActions.jsx`：
  - 新增「迁移到独立账号」按钮，**仅 `isRoot()` 为 true 时渲染**（`isRoot()` 来自 `helpers/utils.jsx`，读 localStorage `user.role >= 100`）；
  - 用 `useMemo` 锁住 `isRootUser`，避免每次渲染都读 localStorage；
  - 0 选中时点击会弹错误提示，不打开弹窗；
  - 透传 `refresh` 用于成功后刷新列表。
- `web/src/components/table/tokens/index.jsx`：
  - 给 `<TokensActions />` 多传一个 `refresh={refresh}` prop。
- `web/src/helpers/token.js`：
  - 新增 `migrateTokensToAccounts(tokenIds)`：`POST /api/token/migrate`，正确解包 `data.results`。

### i18n（`web/src/i18n/locales/{zh-CN,en}.json`）

在 zh-CN（fallback）+ en 两个语言文件里加了以下 key（key = 中文源字符串）：
- `迁移到独立账号` / `迁移成功` / `迁移失败` / `迁移完成：成功 {{success}} 个，失败 {{failed}} 个，共 {{total}} 个。`
- `令牌名` / `新用户名` / `确认要迁移以下 {{count}} 个令牌吗？` / `确认迁移`
- `将为每个所选令牌创建一个新用户、并把令牌的归属切换到新用户上。原令牌的密钥与分组保持不变，外部调用方无感。后续登录新用户需在「用户管理 → 编辑用户」中重置密码。`

其它复用键（`状态` / `确认` / `取消` / `关闭` / `密钥` 等）已存在，未重复添加。其它语言（fr/ja/ru/vi）会通过 `fallbackLng: 'zh-CN'` 自动回退到中文。

---

## 切片 5：单元测试（`/root/project/new-api/model/token_migrate_test.go`）

12 个 `BuildMigratedUsername` 测试覆盖：
1. 普通名 / 2. 首尾空白 trim / 3. 空名兜底 `token_<id>` / 4. 全空白名兜底
5. 长 ASCII 按 rune 截断 / 6. 中文按 rune 截断（不会切坏字符）
7. assigned 内冲突加 `_2` / 8. DB 内冲突加 `_2` / 9. 多次冲突 `_3`
10. 主体接近 20 rune 时加后缀压缩主体保证 ≤ 20 / 11. assigned + DB 混合冲突
12. 不自动 mutate `assigned`

5 个 `MigrateTokenOwnership` 测试覆盖：
1. 正常切换 / 2. srcUserId 不匹配 / 3. token 不存在 / 4. src == new / 5. 0 值参数

测试方式：
- 共享 `model/task_cas_test.go` 已有的 `TestMain`（SQLite in-memory，已迁移 User/Token 表）；
- 每个测试自己 `t.Cleanup` 清空对应表，防止相互污染；
- 占位用户必须分配唯一 `aff_code`（`users.aff_code` 是 uniqueIndex）。

```bash
$ go test ./model/ -v
... 17 tests, all PASS, 0.05s
```

---

## 验证状态

- `go vet ./model/... ./controller/... ./i18n/... ./router/...` ✅ 无 error
- `go build ./...`（占位 web/dist）✅ 通过
- `go test ./model/ ./controller/ ./common/ ./service/ ./dto/` ✅ 全部通过

未执行：
- 三库（SQLite / MySQL / PostgreSQL）真机集成测试（PRD 接受标准里 `[ ] SQLite / MySQL / PostgreSQL 三库手工跑一次`，留 check 阶段处理）；
- `sk-xxx` 真实调用 `/v1/chat/completions` 全链路（PRD 接受标准里 `[ ] 用同一 sk-xxx 调任意 OpenAI/Claude 兼容接口`，留 check 阶段处理）；
- 前端 `bun run build` 与 lint（仅写代码，留 check 阶段验证）。

---

## 受 review 的关键决策

1. **`InsertWithTx` 后 `Update("quota", ...)`** — `InsertWithTx` 内部会覆写 `user.Quota = QuotaForNewUser`，所以必须紧跟一次 quota update。这是 PRD 没明说的一个细节，按现有代码约束就近处理而不是修改 `InsertWithTx` 行为。
2. **`refreshMigratedTokenCache` 通过 `model.RefreshTokenCache` 间接调用 `cacheSetToken`** — 这是因为 `cacheSetToken` 原本是 model 包内部函数。新增一个最小导出包装而不暴露所有缓存细节，是为了保持 model 包的封装性。
3. **审计日志写入时机** — 在事务提交成功后异步刷缓存的同时写 `LogTypeManage` 审计日志；如果日志写失败（比如 LOG_DB 离线），不影响业务结果（仅打 SysLog 记录）。
4. **`BuildMigratedUsername` 不主动写回 `assigned`** — 改为由调用方在「事务提交成功后」显式写回，避免事务回滚却把名字永久占用。
5. **路由放置位置** — 选择放在 `tokenRoute` group 里而不是顶层 `apiRouter`，保持与其它令牌相关接口的组织一致性。`UserAuth + RootAuth` 双中间件叠加顺序执行，效果等价于直接 RootAuth，因为 RootAuth 内部就是 `authHelper(RootRole)`，是 UserAuth 的超集。
