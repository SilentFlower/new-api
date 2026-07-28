# AI 消息持久化审计实施计划

## 1. 配置与安全边界

- 在 `common`/Option 初始化链增加 `MessageAuditEnabled=false` 和 `MessageAuditRetentionDays=7`，让数据库 Option 更新能同步到运行时状态。
- 在 `controller/option.go` 校验保留期限只能为 1-30；启用审计前调用 service 配置检查，缺少或不合法的 `MESSAGE_AUDIT_SECRET` 时返回管理 API 错误。
- 在 service 初始化时从 `MESSAGE_AUDIT_SECRET` 派生 AES-256-GCM 和 HMAC-SHA256 子密钥，生成短密钥指纹；不复用可能在重启后变化的默认 `CryptoSecret`。
- 增加密钥缺失、过短、密文篡改、随机 nonce、跨用户 HMAC 隔离和密钥指纹不匹配测试。

## 2. 数据模型与迁移

- 新建 `model/message_audit.go`，定义 `MessageAuditRequest`、`MessageAuditBlob`、`MessageAuditItem`、`MessageAuditState` 及列表/详情查询参数和响应投影。
- `MessageAuditRequest.RequestID` 唯一；`MessageAuditBlob` 对 `(user_id, schema_version, content_hmac)` 建唯一索引；`MessageAuditItem` 对 `(audit_request_id, sequence)` 建唯一索引。
- 密文与 nonce 使用 `[]byte`；查询列表显式 `Select` 元数据列，禁止读取密文。
- 将四个模型加入 `migrateDB()` 和 `migrateDBFast()`，固定使用 `model.DB`。
- 实现事务写入：锁定清理水位、过滤过期 capture、`OnConflict DoNothing` 写消息块、回查 ID、写有序引用和请求元数据。
- 实现 finalize 更新、分页筛选、详情读取、分批删除与孤立消息块回收。
- 增加 SQLite 行为测试，并用临时 MySQL/PostgreSQL 实例验证 AutoMigrate、去重、事务写入、查询和清理。

## 3. 协议规范化

- 新建 service 规范化模块，通过 `dto.Request` 类型分派支持：
  - `GeneralOpenAIRequest`：system/developer/user/assistant/tool、文本、多模态元数据、tool calls/results 和工具定义。
  - `OpenAIResponsesRequest`：instructions、可见 input item、function/tool call/output 和工具定义；不使用只覆盖文本/媒体的 `ParseInput()` 代替完整安全解析。
  - `ClaudeRequest`：system、messages、tool_use/tool_result 和工具定义。
  - `GeminiChatRequest`：systemInstruction、contents、functionCall/functionResponse、代码执行可见结果和工具定义。
- 明确排除 OpenAI reasoning/reasoning_content、Claude thinking/signature、Gemini thought/thoughtSignature、Responses encrypted/reasoning 内容。
- 媒体只生成 type/MIME/size/source kind/HMAC 摘要，不保存 URL、file ID、Base64 或二进制。
- 快照累计大小超过上限时生成 metadata-only capture 状态，不复制剩余正文。
- 使用表驱动测试验证四种协议、工具链、重复消息顺序、媒体过滤、隐藏思考过滤和超限行为。

## 4. 异步采集生命周期

- 新建消息审计 manager，使用单写 goroutine、有界 channel、总字节预算、原子计数器、批量 flush 和有限重试。
- 在 `controller.Relay` 完成请求校验和 `GenRelayInfo` 后调用非阻塞 capture；只使用原始验证后 DTO，不使用经过渠道映射/参数覆盖的重试请求。控制器仅组装最小上下文并调用 service，不引入协议分支、脱敏、加密、队列或数据库逻辑。
- 注册 defer 投递 finalize；安排 defer 顺序，使其在计费和并发 guard 收口后读取最终 `newAPIError`、HTTP 状态、结束原因和耗时。
- 将协议规范化、快照大小控制、队列生命周期、加密与写入编排集中在 service，将事务、去重、查询和清理集中在 model，避免新增仅转发参数的多余抽象层。
- capture 队列满或字节预算不足时直接丢弃；finalize 同样非阻塞。任何审计错误都不能改变 relay 响应、重试或计费。
- 在服务启动阶段启动 manager；HTTP graceful shutdown 后、数据库关闭前调用带超时的 drain/stop。
- 增加队列满不阻塞、capture/finalize 顺序、禁用时跳过、失败重试、字节预算释放和优雅关闭排空测试，定向运行 race 测试。

## 5. 保留与一键清空

- 在 `model.SystemTask` 增加 `message_audit_cleanup` 类型，在 service 注册同时支持定时保留和手动清空的 handler。
- handler 开始时锁定 `MessageAuditState` 并单调推进 `purge_before`；随后按请求批次删除引用/请求并回收孤立消息块，持续更新系统任务进度。
- 定时任务每天运行，截止时间为当前时间减保留天数；审计关闭时仍启用。
- 新增 `POST /api/system-task/message-audit-cleanup`，payload 固化点击时刻；复用当前任务与任务详情接口。
- 增加“队列旧 capture 在清空后不重现”“新请求不被误删”“共享消息块仍被新请求引用时不删除”和重复清空去重测试。

## 6. Root-only 管理 API 与操作审计

- 新建 `controller/message_audit.go`，提供列表、详情和状态接口，统一使用 `{success,message,data}`。
- 在 `router/api-router.go` 注册 `/api/message-audit` RootAuth 路由，以及消息审计清空系统任务路由。
- 详情接口只在单条读取时解密；列表 DTO 从类型上不包含密文、nonce 或正文预览。
- 详情读取无论成功或失败都写 `LogTypeManage` 管理审计，记录操作者、目标请求 ID 和结果，不记录正文。
- 在 `middleware/audit.go` 为清空接口增加稳定 action，依靠现有 RootAuth 写操作兜底审计。
- 增加非 root 拒绝、筛选参数、列表不泄密、详情解密、密钥不匹配和管理审计测试。

## 7. web/default 管理界面

- 新建 `features/message-audits/`，包含 API、类型、URL 筛选、React Query、列定义、移动卡片、详情 Sheet/Drawer、状态栏和清空任务交互。
- 新增 root-only `/message-audits` 文件路由，使用 Zod 校验分页、时间、用户、令牌、模型、请求 ID、路径和状态搜索参数。
- 在 root 导航日志区域增加 `Message Audit`，设置 `requiredRole: ROLE.SUPER_ADMIN`；后端权限仍作为最终边界。
- 列表复用 `DataTablePage`，不显示正文；详情打开时才请求正文，使用文本节点和 `pre` 安全展示长文本/JSON，支持逐项复制和折叠。
- 桌面端使用 Sheet，移动端使用全屏 Drawer；提供加载、空数据、密钥错误、正文超限和解密失败状态。
- 清空按钮使用危险样式和删除图标，AlertDialog 要求输入指定确认文本；任务运行时禁用重复操作并轮询展示 Progress、删除数量和错误。
- 在“系统设置 → 日志维护”增加审计开关、密钥配置状态和 1-30 天数字输入，复用现有 Option 更新链。
- 更新 `OperationsSettings` 默认值、导航 URL 配置和六语言 locale；增加关键纯逻辑/确认交互测试。

## 8. 规范、验证与交付

- 更新 `.trellis/spec/backend/logging-guidelines.md`：保留普通日志禁止正文的规则，仅为默认关闭、root-only、加密、限期清理的独立消息审计模块增加明确例外与安全合同。
- 后端格式化与定向验证：
  - `gofmt -w <本任务修改的 Go 文件>`
  - `go test ./model ./service ./controller ./router -count=1`
  - `go test -race ./service -run 'MessageAudit' -count=1`
  - `go vet ./model ./service ./controller ./router`
- 跨库验证：启动临时 MySQL/PostgreSQL，运行消息审计迁移与行为测试；SQLite 测试始终执行。
- 前端验证：
  - `cd web/default && bun test <消息审计测试文件>`
  - `cd web/default && bun run i18n:sync`
  - `cd web/default && bun run typecheck`
  - `cd web/default && bun run lint`
  - `cd web/default && bun run format:check`
  - `cd web/default && bun run build`
- 全仓回归：`go test ./... -count=1`、`git diff --check`。
- 启动本地开发服务，使用 root 账号验证关闭/启用、筛选、详情解密、移动布局、清空进度和清空后的新请求保留。
- 复核 `controller.Relay` diff，确认接入仍是薄层钩子，未把协议或存储业务逻辑带入主转发控制器。

## 9. 服务端推断会话与详情增强（二次实现）

- 为 `MessageAuditRequest` 增加 `audit_session_id`、`parent_request_id`、`session_match`、会话序列指纹、会话消息项数和非持久化 `session_request_count`；字段使用跨库可迁移的普通字符串/整数类型和必要索引。
- service 在加密消息块后，排除独立工具/函数定义并按原始顺序生成每级会话前缀 HMAC；model 在现有写入事务与清理水位锁内查询同一用户、同一协议的唯一最长候选，生成或继承会话 ID。精确重复标记 `exact`，历史前缀延续标记 `prefix`，无匹配或歧义标记 `new`；metadata-only 请求不推断。
- 前缀失败且新会话序列缩短时，基于非公共角色消息块 ID/HMAC 查询候选请求并验证严格递增子序列；用集中常量定义最小锚点数、覆盖率、旧请求后段命中和唯一候选差距，满足时标记 `compressed`。避免 N×M 全表扫描，先由 `blob_id` 索引聚合候选，再只加载少量最佳候选的有序引用完成校验。
- 列表查询先应用现有筛选，再按会话 ID 分页，选择每个会话最新请求并返回 `session_request_count`；历史空会话 ID 按请求 ID 独立处理。列表 API 支持按 `audit_session_id` 查询会话内单次请求，结果按最新请求在前，所有列表路径均不得选择密文、nonce 或正文。
- Default 页面默认只展示每个推断会话的最新请求，显示会话请求数和推断会话 ID；新增可分页会话历史对话框，允许打开任一单次请求详情。
- 详情消息区增加从当前内容派生的角色与内容类型两组多选过滤，默认展示全部并提供恢复全部操作；将扁平消息块改为语义化纵向时间线，保持消息原始顺序，适配桌面 Sheet、移动 Drawer、长文本、JSON、工具调用和媒体元数据。
- model 增加只读存储统计：MySQL/PostgreSQL/SQLite 分支读取四张审计表的物理分配空间，失败时回退统计密文和 nonce 逻辑字节；service 状态与前端类型增加大小、估算标记及三类行数，状态栏用统一字节格式展示。
- i18n 新文案必须通过 `scripts/add-missing-keys.mjs` 同步六语言后运行 `bun run i18n:sync`，不得直接修改 locale JSON。
- 增加前缀指纹稳定性、工具定义排除、精确/前缀/压缩/新建/歧义归属、低覆盖压缩拒绝、筛选后会话分组、历史空会话、最新代表、会话内倒序、存储统计与回退、API 参数、详情类型过滤和时间线交互测试，并重新执行消息审计定向测试、三库兼容、typecheck、变更文件 lint/format、build 和 Check-All。

## 10. 审计模型名与消费日志对齐（二次实现）

- 入站 capture 继续使用原始模型名生成不可变快照，避免把模型映射和计费逻辑带入采集入口。
- 抽取 `ConsumeLogModelName()` 统一消费日志与消息审计的模型展示规则，以 `BillingModelName()` 为基础并保留 gizmo 通配归一化；文本消费日志和 `controller.Relay` 的审计 finalize 共用该函数。
- 现有 finalize 轻量事件增加最终模型名；依赖既有 defer 后进先出顺序，确保主计费收口先于消息审计 finalize 执行。
- model finalize 仅在最终模型名非空时更新 `message_audit_requests.model_name`，避免失败或未完成计费时清空采集阶段模型。
- 更新异步 capture/finalize 顺序测试，验证最终持久化模型名覆盖为消费日志同源模型，同时保持非阻塞队列与数据库结构不变。

## 风险与回滚点

- 密钥不一致会导致跨节点详情解密失败和去重失效；发布必须先统一环境变量，再启用设置。
- 主数据库会增加容量和写事务；队列 fail-open、批处理、媒体剥离、消息去重、快照上限和 7 天默认保留共同限制影响。
- 清理水位和写入事务顺序是防止旧队列数据重现的核心不变量，任何清理优化都必须保留该锁顺序。
- 回滚时先关闭审计并排空队列；新增表和 Option 可保留，不影响旧代码。需要释放空间时先通过已部署版本执行清空任务。
