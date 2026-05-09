# 令牌迁移到独立账号功能

## Goal

为超级管理员（RoleRootUser）提供「将自己账号下的多个令牌（API Key）批量迁移到一对一独立账号」的功能。
入口位于令牌管理页面的批量操作区。迁移后令牌的 `Key`（sk-xxx）与 `Group` 字段保持不变，
仅将 `tokens.user_id` 切换到为该令牌新建的用户上。外部调用方完全无感知。

## Requirements

### 范围与权限
- 仅 `RoleRootUser` 可见、可调用迁移功能。前端按钮在角色不足时隐藏；后端用 `RootAuth()` 中间件强制校验。
- 源账号 = **当前登录的超级管理员自己**。不开放跨账号迁移；后端按 `c.GetInt("id")` 拿到操作者，并以此为令牌的归属过滤源。
- 操作粒度 = **表格勾选**：用户在令牌管理页勾选若干行后点击「迁移到独立账号」按钮（与「复制所选/删除所选」UI 模式一致）；0 行勾选时按钮禁用。
- 不提供「全量一键迁移」与 dry-run 预演入口。
- 二次确认：点击迁移按钮先弹确认对话框，提示"将为每个令牌创建一个新用户、原令牌将归属新用户"，确认后再调用接口。

### 新账号生成规则
- **username**：取自 `token.Name`，按以下顺序处理：
  1. `strings.TrimSpace(token.Name)` 去首尾空白；
  2. 若结果为空，使用 `token_<token.Id>` 作为兜底；
  3. 否则按 rune 截断到 ≤ 20 个字符（不会切坏中文）；
  4. 与已有用户名/本批次已分配名冲突时追加 `_2 / _3 / ...`，超出 20 rune 时再压缩主体；最多重试 100 次仍冲突 → 该令牌迁移失败上报。
- **password**：调用 `common.GetRandomString(16)` 生成；通过 `common.Password2Hash` 入库；**不返回前端、不写日志**。超管事后用「用户管理 → 编辑用户」流程重置密码。
- **display_name**：与 username 相同。
- **email**：留空。
- **role**：`common.RoleCommonUser`（硬写）。
- **status**：`common.UserStatusEnabled`（硬写）。
- **group**：取自 `token.Group`；当 `token.Group == ""` 或 `"auto"` 时回退为源账号（当前超管）的 `Group`。
- **quota**：
  - 普通令牌（`UnlimitedQuota=false`）→ `newUser.Quota = token.RemainQuota`；
  - 无限令牌（`UnlimitedQuota=true`）→ `newUser.Quota = int(1000000000 * common.QuotaPerUnit)`（与 `controller/token.go` AddToken 处的 `maxQuotaValue` 同源，事实无限）。
- 不触发 `constant.GenerateDefaultToken` 路径（用 `User.InsertWithTx` 而非 `User.Insert`）。

### 令牌迁移操作
- 迁移本质：对每个令牌执行 `UPDATE tokens SET user_id = <newUserId> WHERE id = ? AND user_id = <srcId>`。
- 不修改令牌的任何其他字段（`Key`、`Group`、`RemainQuota`、`UsedQuota`、`Status`、`ModelLimits` 等全部保留）。
- 迁移成功后必须刷新 Redis 令牌缓存：`cacheSetToken(token)` 用更新后的 `UserId` 重写。

### 失败语义（逐令牌独立事务）
- 每个令牌的「创建新 user + 切换 token.user_id + 刷新 cache」打成一个独立 GORM 事务。
- 任一令牌失败：仅回滚自己（新 user 不入库、token 归属不变），其他令牌继续尝试。
- 响应包含每个令牌的最终状态：`{token_id, token_name, new_username?, new_user_id?, status: "success"|"failed", error?: string}`。
- 不在前端展示密码字段；展示 username 列表 + 失败原因。

### 跨数据库（CLAUDE.md Rule 2）
- 全部走 GORM API（Create / Updates / Find / Where），不写原生 SQL；自动兼容 SQLite / MySQL ≥ 5.7.8 / PostgreSQL ≥ 9.6。
- 唯一索引（username 唯一）冲突由事务回滚 + 应用层重命名重试解决。

### 安全
- 后端再次校验 `c.GetInt("role") == common.RoleRootUser`；防止有人绕过中间件。
- 前端入参不接受 `role`、`status`、`quota` 等可能被构造的字段；这些值后端写死。
- 审计：每条成功迁移写一条 `model.RecordLog(srcUserId, model.LogTypeManage, ...)`；日志内容只含 username、token id，不含密码。

## Acceptance Criteria

- [ ] 普通用户访问 `/api/token/migrate` 接口返回 403；超管访问通过。
- [ ] 前端令牌管理页：普通用户/管理员看不到「迁移到独立账号」按钮；超管登录可见可用。
- [ ] 选定 N 个令牌点击迁移后，每个令牌都有一个对应的新用户被创建（username/email/password 字段符合上述规则）。
- [ ] 新用户的 `Group` 与令牌期望一致（含 `""`/`"auto"` 回退分支）。
- [ ] 新用户的 `Quota` 与令牌期望一致（普通 = RemainQuota；无限 = maxQuotaValue）。
- [ ] 令牌的 `Key` / `Group` / `RemainQuota` / `UsedQuota` / `Status` 在迁移前后字节级一致；只 `UserId` 变化。
- [ ] 用同一 `sk-xxx` 调任意 OpenAI/Claude 兼容接口（如 `/v1/chat/completions`）：迁移前能通，迁移后仍能通，且响应正常。
- [ ] 用户名冲突时自动加 `_2 / _3` 后缀，且仍 ≤ 20 rune。
- [ ] 单令牌失败不影响其他令牌；响应中能区分成功/失败。
- [ ] 迁移结果不含密码字段；后端日志不含密码。
- [ ] SQLite / MySQL / PostgreSQL 三库手工跑一次本功能，结果一致。

## Technical Approach

### 后端

**新增文件**：`controller/token_migrate.go`
- `MigrateTokensToAccounts(c *gin.Context)`：
  1. 解析请求体 `{token_ids: []int}`；校验 `len > 0` 且 `≤ 100`（与现有 `GetTokenKeysBatch` 上限一致）。
  2. 拉取源用户：`srcUserId = c.GetInt("id")`，`srcUser, _ = model.GetUserById(srcUserId, false)`。
  3. 用 `model.GetTokenKeysByIds(ids, srcUserId)` 拉令牌（自带 user_id 过滤）。不在源下的令牌从结果消失，前端给清晰报错。
  4. 维护本批次已分配 username 集合 `assigned := map[string]bool{}`。
  5. 对每个令牌，单独事务里：
     - 计算 username（`buildMigratedUsername(token, assigned, db)` —— 含截断、冲突重试 100 次）；
     - 计算 group（含 ""/"auto" 回退）；
     - 计算 quota（含 unlimited 走 max）；
     - 用 `User.InsertWithTx(tx, 0)` 创建新用户；
     - `tx.Model(&model.Token{}).Where("id = ? AND user_id = ?", token.Id, srcUserId).Update("user_id", newUserId)`，校验 `RowsAffected==1`；
     - `tx.Commit()`；
     - commit 后异步 `cacheSetToken` 刷新令牌缓存（注意 token.UserId 已变）；记录 `model.RecordLog(...)`。
  6. 返回 `{success: true, data: {results: [...]}}`。

**新增 model 工具**（`model/token_migrate.go`）：
- `MigrateTokenOwnership(tx *gorm.DB, tokenId, srcUserId, newUserId int) error`：封装第 5 步的 token 归属切换，返回错误（含 RowsAffected 不为 1 的情况）。

**路由**（`router/api-router.go`）：
- 在 `tokenRoute := apiRouter.Group("/token")` 之后新增一行：`tokenRoute.POST("/migrate", middleware.RootAuth(), middleware.CriticalRateLimit(), controller.MigrateTokensToAccounts)`。
- `tokenRoute` 当前是 `UserAuth()`，但 RootAuth 内部就是 authHelper(RootRole)，会覆盖更严格；中间件链按顺序执行，效果是先做 UserAuth、再做 RootAuth；为避免重复鉴权，单独放在外层用 `apiRouter.POST("/api/token/migrate", middleware.RootAuth(), ...)`。

### 前端

**新增组件**：`web/src/components/table/tokens/modals/MigrateToAccountsModal.jsx`
- 接收 `selectedKeys`、`onClose`、`onSuccess`；
- 第一屏：确认提示 + 选中令牌的简表（name + masked key）+ 「确认迁移」按钮；
- 第二屏（成功后）：结果表格，三列 `令牌名 / 新用户名 / 状态`，带「关闭」按钮；
- 整个流程不涉及密码字段。

**修改**：`web/src/components/table/tokens/TokensActions.jsx`
- 新增按钮「迁移到独立账号」，仅在 `userRole === RoleRootUser` 时渲染（角色从 `useUser` 或全局上下文取）；
- 点击逻辑：`selectedKeys.length === 0` 提示报错；否则打开 `MigrateToAccountsModal`。

**API 客户端**（`web/src/helpers/api.js` 或 `services/`）：
- 新增 `migrateTokensToAccounts(tokenIds)` 调用 `POST /api/token/migrate`。

### i18n
- 后端新增 `i18n.MsgTokenMigrateXxx` 文案（zh/en），覆盖：成功摘要、超出 100、源用户加载失败、用户名冲突、令牌不存在等。
- 前端 i18n keys 新增到 `web/src/i18n/locales/{zh,en,...}.json`，键名沿用中文源串。

## Decision (ADR-lite)

**Context**：超管账号下集中存了大量令牌，需要把每个令牌"分户"到独立账号，便于后续按账号粒度做管理 / 计费 / 审计。要求外部调用方完全无感（key/group 不变）。

**Decision**：实现一个 RootAuth 保护的批量迁移接口，前端在令牌管理页提供勾选+迁移按钮。逐令牌独立事务，部分成功允许；不返回密码，后续由超管在用户管理里重置；不做 dry-run。新账号 username 沿用令牌名（截断 + 冲突加序号），group/quota 继承自令牌（含特殊值回退）。

**Consequences**：
- 优点：改动局限在令牌管理 + 一个新增 API，对其他模块零侵入；逐令牌事务降低 DB 风险；不暴露密码符合最小权限原则。
- 缺点：超管事后必须为每个新账号手动重置密码才能登录（操作量随令牌数线性增长）；批量很大的迁移 UX 一般。
- 风险：令牌名含极端字符或与现有用户名重叠时可能要走 100 次重试；冲突极端情况会失败，需要超管修改令牌名再重试。

## Out of Scope

- 反向操作（多个独立账号合并回一个总账号）。
- 跨账号迁移（root 帮其他账号做迁移）。
- 「自动重置密码邮件 / 一次性重置链接」机制。
- 用户管理界面新增"重置密码"快捷入口（沿用现有的「编辑用户」即可）。
- 迁移历史/审计页面（落到 `LogTypeManage` 即可，专门 UI 后续再说）。

## Technical Notes

### 关键文件清单
- 后端
  - `controller/token_migrate.go`（新增）
  - `model/token_migrate.go`（新增）
  - `controller/token.go`（无需改，仅复用 `GetTokenKeysByIds` 等）
  - `model/user.go`（复用 `User.InsertWithTx`、`CheckUserExistOrDeleted`）
  - `model/token_cache.go`（复用 `cacheSetToken`）
  - `middleware/auth.go`（复用 `RootAuth()`）
  - `router/api-router.go`（新增一行路由）
  - `i18n/`（新增几条文案 key）
- 前端
  - `web/src/components/table/tokens/modals/MigrateToAccountsModal.jsx`（新增）
  - `web/src/components/table/tokens/TokensActions.jsx`（增按钮）
  - `web/src/components/table/tokens/TokensTable.jsx`（透传 props）
  - `web/src/helpers/api.js`（新增请求函数）
  - `web/src/i18n/locales/{zh,en}.json`（增 key）

### 鉴权与缓存
- TokenAuth 中间件在拿到 token 后会读 `model.GetUserCache(token.UserId)` —— 切换 user_id 后旧用户缓存命中没问题（命中的是旧 user_id），新用户的 cache 走第一次访问时自动 build。
- 令牌 cache 必须刷：`cacheSetToken` 入库 token 完整对象（含 UserId），如果不刷会让旧 cache 一直返回旧 UserId 直到 TTL 过期。

### 验证 sk 调用全链路
- 手动用 `curl -H "Authorization: Bearer sk-xxx" .../v1/chat/completions` 在迁移前/后各跑一次。
- 检查 `/api/usage/token`（read-only token usage）返回值在迁移后正确。

### 不动 GenerateDefaultToken
- `User.Insert` 会读 `constant.GenerateDefaultToken`（可能给新用户自动建一个默认令牌）；用 `User.InsertWithTx` 完全跳过这个分支。

### 安全 Checklist
- 后端 RootAuth；handler 内再验一次 role；
- 入参只读 `token_ids`，role/quota/group 全部由后端推导；
- 响应不含 password；
- 日志不含 password；
- HTTPS 部署强制（运维约束，代码不强求）；
- 限频走 `CriticalRateLimit()`，避免被刷接口创建大量用户。
