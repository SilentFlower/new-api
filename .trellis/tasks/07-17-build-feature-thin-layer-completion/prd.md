# 完成剩余 Build Feature 薄层治理

## Goal

完成 `build-bak` 分支中尚未达到“定制逻辑独立、上游文件最薄接入”标准的全部现有业务能力治理，在严格保持 API、协议、计费、日志、权限、数据库兼容性和前端交互行为不变的前提下，降低后续同步 `origin/main` 的冲突面。

## Background

- 已完成 Responses Compact、Alpha Search 与 Responses WebSocket 首批薄层治理，并归档任务 `07-17-build-feature-normalization`。
- 当前 session 显示的 `07-09-stream-sse-keepalive-config` 是失效指针，对应活动任务目录不存在，不需要重复归档。
- 当前差异审计确认，剩余主要冲突热点集中在 Dashboard/Excel、公共 API Key 日志、Claude WebSearch、视觉辅助、映射后上游模型计费、Anthropic 缓存 Token 与 Reasoning Effort，以及两套渠道设置主表单。
- 非流式 JSON 保活、Claude `count_tokens`、Token 迁移到独立账号，以及已治理的 Compact/Alpha/WS 已基本符合薄层规范，不纳入重复改造。

## Confirmed Requirements

### R1. 全量完成，不遗漏剩余领域

本父任务覆盖以下六个可独立验收的子领域：

1. Claude 主链路：WebSearch 模拟、渠道 WebSearch 密钥处理和 Anthropic Reasoning Effort 同步。
2. 公共日志与统计：公共 API Key 日志、筛选、脱敏、趋势以及 Anthropic 缓存 Token 统计。
3. Dashboard：分组/API Key 筛选、统计查询与 Excel 导出。
4. 视觉辅助：Relay 生命周期、错误日志和各格式 handler 的最薄接入。
5. 映射后上游模型计费：计费模型冻结、价格、预扣、结算、任务计费和日志调用边界。
6. 渠道设置前端：Default 与 Classic 中 WebSearch、视觉辅助、Compact 和上游模型计费配置的独立组件挂载。

### R2. 严格保持现有行为

- 不改变任何现有 API 路径、请求参数、响应结构、错误码、HTTP 状态码或权限边界。
- 不改变 Relay 请求字节、渠道选择、重试、auto-ban、计费、退款、日志脱敏或审计语义。
- 不改变现有数据库结构、迁移、配置 JSON 键、默认值或旧数据兼容行为。
- 不改变 Default/Classic 的用户可见文案、i18n key、字段 round-trip、校验或交互结果。
- 发现既有缺陷时记录为后续任务，不在结构治理中顺带修复。

### R3. 上游文件只保留最薄接入

- 定制业务逻辑优先迁入同 package 的独立领域文件、现有独立模块或独立前端组件。
- 原有上游文件只允许保留路由注册、条件分派、窄函数调用或标准结果交回。
- 不为了 DRY 重构上游核心流程，不创建通用插件框架，不修改无关格式、命名、注释或代码位置。
- 每个子任务必须记录原有文件、目标独立文件、必要接入点、回滚方式和后续同步复核点。

### R4. 父子任务与顺序

- 父任务只维护全量范围、子任务映射、跨子任务约束和最终集成验收，不直接作为实现目标。
- 子任务按存在文件重叠的顺序串行推进：Claude 主链路 → 视觉辅助 → 上游模型计费 → 公共日志与统计 → Dashboard → 渠道设置前端。
- `relay/claude_handler.go` 先由 Claude 子任务收窄，再由视觉辅助子任务处理剩余视觉接入。
- `model/log.go` 先由公共日志与统计子任务收窄，再由 Dashboard 子任务处理导出与看板查询。
- 同一轮不得让两个子任务同时修改同一上游热点文件。

### R5. 验证强度

- 每个子任务先建立或确认行为契约测试，再迁移实现。
- 每个子任务至少执行相关包完整回归、关键路径定向 race、`go vet`、格式检查和 `git diff --check`。
- 数据库查询相关子任务必须覆盖 SQLite、MySQL 与 PostgreSQL 兼容边界；无法本地启动三库时，至少使用已有方言测试和 SQL 构造断言，并明确残余风险。
- 前端子任务使用 Bun，执行类型检查、相关测试、i18n 检查和生产构建。
- 全部子任务完成后执行父任务级全仓回归、跨层数据流复核和上游冲突面复核。

## Task Map

| 顺序 | 子任务 | 主要热点 | 完成口径 |
| --- | --- | --- | --- |
| 1 | Claude 主链路薄层化 | `relay/claude_handler.go`、`controller/channel.go` | WebSearch、密钥处理、effort 同步迁入独立领域文件，旧文件只留窄调用 |
| 2 | 视觉辅助薄层化 | `controller/relay.go`、Claude/Compatible/Responses handlers | 视觉准备、状态和专用错误日志收敛，普通 Relay 行为不变 |
| 3 | 上游模型计费薄层化 | `relay/common/relay_info.go`、`relay/helper/price.go`、`service/*billing*` | 计费模型快照边界明确，调用点收敛且结算语义不变 |
| 4 | 公共日志与缓存统计薄层化 | `controller/log.go`、`model/log.go`、`model/token_statistics.go` | 公共日志查询、脱敏、趋势和缓存 Token 统计独立 |
| 5 | Dashboard/Excel 薄层化 | `controller/usedata.go`、`model/usedata.go`、Dashboard 前端 | Excel 与筛选逻辑独立，旧 Controller/Model 只保留调用 |
| 6 | 渠道设置前端薄层化 | Default/Classic 渠道主表单 | build 配置拆成独立组件，主表单只挂载并保持完整 round-trip |

## Acceptance Criteria

- [ ] 六个子任务均已创建、规划、实施、检查、提交并归档。
- [ ] 每个剩余 build Feature 的核心逻辑位于独立领域文件或组件中。
- [ ] 每个被修改的上游文件只保留可用一句话解释的最薄接入点。
- [ ] `origin/main...build-bak` 的热点文件冲突面相较治理前显著下降，且没有通过无关重排制造虚假下降。
- [ ] API、Relay、计费、日志、数据库、前端交互和 i18n 行为保持兼容。
- [ ] 相关包测试、定向 race、vet、前端检查及最终全仓回归达到任务设计约定的质量门。
- [ ] 真实外部 Provider、生产数据库或浏览器联调未执行时，在对应子任务和父任务最终结论中明确记录。

## Out Of Scope

- 新增产品功能、修复已知但与结构迁移无关的业务缺陷。
- 合并新的 `main` 提交或处理未来尚未发生的上游冲突。
- 重做已经基本符合薄层规范的 Compact/Alpha/WS、非流式保活、Claude `count_tokens` 和 Token 迁移。
- 修改受保护的项目名称、组织标识、品牌、版权或归属信息。
