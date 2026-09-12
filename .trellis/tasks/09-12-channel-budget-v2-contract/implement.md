# Implement — 预算 v2 契约

## 步骤

1. [x] DTO v2 + 归一化/校验/预览 + 表驱动测试（含 128/64 上限、相交、occurrence 引用、降级校验矩阵）。
2. [x] 新表 + 迁移列表 + 启动迁移 + 三库迁移测试。
3. [x] 内核改为直接消费 v2；旧列/旧覆盖字段 `json:"-"`；删除渠道更新校验。
4. [x] 按行用量/覆盖接口 + 路由 + 权限测试 + 契约测试；删除旧接口与旧控制器。
4b. [x] R7：删除旧 relay 日/周独立检查、降级预检直接调用、revision=0 早返回、RelayInfo 字段与 vision_assist 上下文键复制；指标与降级信息新增字段，`period` 改 `occurrence`；删除 `resolveChannelPeriodSources`。
5. [x] 降级全链路新增按模型/同渠道用例（AC2、AC4），并覆盖同渠道及跨渠道目标个人日/周限额（AC11）。
6. [x] 前端 v2：types/api → 面板重写 → 按行用量/覆盖 → 旧抽屉字段删除 → i18n；按父 PRD R4.1 补齐时段列、已用/剩余进度、行编辑抽屉和表下方默认动作，关闭与 Esc 均保留草稿。
7. [x] 用 `controller/channel_period_policy_test.go` 实际响应生成 fixture，放入 `research/channel-period-contract.v2.json` 供 ai-fund 子任务。（`CHANNEL_PERIOD_CONTRACT_OUTPUT=<path> go test ./controller -run 'TestChannelPeriodPolicyManagementContract$'`）
8. [x] SQLite 文件库备份/迁移/恢复及实际旧版读取、HTTP 200/429 演练已记录（`research/rollback-drill.md`）；SQLite/MySQL/PostgreSQL 的迁移及重复执行契约均已实测通过。

## 验证

```bash
go build ./... && go vet ./...
go test ./...
CHANNEL_PERIOD_TEST_MYSQL_DSN=... CHANNEL_PERIOD_TEST_POSTGRES_DSN=... go test ./service ./model -run 'ChannelBudgetMigrationDatabaseContract|ChannelPeriodPolicyDatabaseContract' -count=1 -v
cd relaykit && GOWORK=off go build ./...
cd web && bun run typecheck && bun run lint && bun test src/features/channels
```

## 风险文件 / 回滚点

- `service/channel_budget_migrate.go`：先在 sqlite 快照演练；上线前备份三表。
- `router/channel-router.go`：删除接口前确认 ai-fund 子任务已就绪同版本发布。

## 2026-09-12 检查修复记录

- CHK-001：补充真实 Relay → 测试上游 → 结算/消费日志测试，覆盖按模型的池子/个人日周限额、同渠道模型降级按目标价格扣费、目标个人限额阻断且不二跳；通过。
- CHK-002：按行用量调整与提额创建/替换/撤销均断言 revision 不变；提额审计补充 before/after，并核对实际管理日志；通过。
- CHK-003：旧个人日/周计数的 used_quota 超过 32 位上界时返回参数错误 400；通过。
- FBK-001：无策略记录时把旧渠道日/周列派生为 revision=0 的预算视图，统一判定继续拦截；内存/Redis 的日/周用例以及保存 v2 后不复活旧列均通过。
- 整包回归补齐 4 个旧测试缺失的渠道记录，`go test ./controller ./service -count=1` 通过。
- `go build ./...`、`go vet ./...`、`go test ./...`、`relaykit` 的 `GOWORK=off go build ./...` 全部通过。
- 临时 MySQL 8.0、PostgreSQL 16 与 SQLite：迁移契约和原策略数据库契约全部通过，外部数据库用例没有跳过。
- 前端 typecheck、lint（0 error，现有 warning）、渠道测试（37 pass / 0 fail）和 format:check 通过；在临时副本运行实际 i18n 同步脚本后七个语言文件逐字节不变，缺失/多余/未翻译项均为 0；变更组件的静态翻译键扫描通过。
- 本轮重检画像：interactive / requested=auto / effective=full；原因是迁移兼容和管理审计行为变化。新增待处理项：CHK-004（撤销旧提额后重启重新导入）、CHK-005（R4.1 界面与规划差异）、CHK-006（修改模型后统计起点仍沿用旧行创建时间）、CHK-007（旧版本回滚验证缺失）、FBK-002（未知持久化 schema 被当作 v1 转换）、FBK-003（审计前值读取失败静默记 0）。未通过整体门禁，任务继续保持 in_progress。

## 2026-09-12 第二轮修复与验证

- CHK-004：撤销改为新覆盖表的零额度非生效记录，单条查询与分页过滤；旧表重启迁移不会复活撤销，重新提额也不会被旧值覆盖。SQLite、MySQL 8.0、PostgreSQL 16 均通过；撤销池子行返回 400，避免写入无效占位。
- CHK-005：按父 PRD 补齐时段列、池子/最高用量用户进度、抽屉编辑及表下方默认动作；日/周也能引用时段；关闭和 Esc 保留草稿；用户进度读取实际提额，加载失败可重试，新行不请求用量。用户分页按用量降序、同值按 ID 升序，内存与 Redis 已验证。
- CHK-006：模型/窗口/整段时段身份变更更新 created_at，改名和调额不改变统计起点；daily/weekly/occurrence × 内存/Redis 六组用例通过。
- CHK-007：从 `f0e225fc1` 归档的实际旧代码编译读取恢复后的 v1；预算 100 下，用量 99 返回 HTTP 200、用量 100 返回 429。临时驱动已移出产品代码，证据见回滚记录。
- FBK-002：未知、零、缺失版本、null 和空正文的已存储策略读取失败且不迁移覆盖；无策略记录仍保留旧列兼容路径。
- FBK-003：注入审计前值 Redis 读取故障，返回 503，原用量 40 不变且没有成功管理日志。
- 前端 39 项渠道测试通过，typecheck、lint、生产 build、format:check 通过；七个语言文件 i18n:sync 逐字节无变化，missing/extras/untranslated 均为 0，渠道组件静态翻译键扫描通过。
- Go 全量测试通过；最后的局部修复补跑 controller/service 整包测试及根模块 build/vet；relaykit 独立构建通过。三库数据库契约无跳过。
- 最终重检画像：interactive / requested=auto / effective=full / confidence=high，因持久化撤销语义、迁移兼容与抽屉状态行为变化；三个维度均通过，剩余 CHK=0、FBK=0。DOC-001 同步本文件与 brief 的实现/验证状态，修正摘要中的过期参数和迁移前提；未改变验收标准。下一步规范同步，任务继续保持 in_progress。

## 2026-09-12 规范回查补修

- FBK-004：旧 user-limit-overrides 的日/周字段拒绝分支原先通过 ApiErrorI18n 返回 HTTP 200，与父 PRD R6.3 的 400 不一致。保留本地化错误正文，改为 HTTP 400；测试分别断言日字段、周字段的真实状态码和 success=false，并继续核对失败请求不产生成功审计。
- 修改前两个断言均稳定复现 expected=400 / actual=200；修复后 `go test ./controller ./router -count=1`、`go build ./...`、`go vet ./controller ./router` 通过。
- 补充 Full 重检复用未变更链路证据，重新核对旧接口拒绝、并发覆盖正常路径、前端 API 错误传播与审计日志；三个维度通过，剩余 CHK=0、FBK=0。该项修正上一轮状态码验证遗漏。
