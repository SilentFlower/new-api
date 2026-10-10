# Implement — 渠道可选模型用量持续累计

## 1. 开始条件

- [x] 创建独立 planning 子任务，旧 completed 任务的普通 Git 同步已核对。
- [x] 确定默认关闭、按渠道、个人/池子日周、旧计数与调整隔离、历史不回填及性能验证范围。
- [x] 展示最终 Brief 并取得评审确认；随后 `task.py start`，进入 trellis-route(target=implement)。
- [x] 实现前按 trellis-before-dev 完整读取 new-api 后端/前端及 ai-fund 对应规范；新增 UI 文案前读取 i18n-translate，React 面板按 shadcn-ui 与既有组件约定实现。

## 2. 后端合同与持续计数

- [x] 扩展 DTO 可选开关和服务端统计来源/覆盖元数据；验证缺省、显式 false、null、未知字段、旧客户端保存保留新状态与存储稳定归一化。
- [x] 新建 `service/channel_model_usage.go` 与对应确定性测试；在原结算计数入口追加最多两个模型事实更新，合并 Redis Lua、整数原子预检和内存锁。
- [x] 在预算规范化与计划中持久选择来源，复用已有同身份来源，保护旧计数及人工值。
- [x] 新建 `service/channel_model_usage_adjustment.go` 与对应测试；实现多模型聚合、原子调整快照、列表/统一状态一致性及清理。
- [x] 覆盖首次开启、关闭/重新开启、周期切换、模型选择变化、空模型名、覆盖缺口、并发调整、溢出、Redis 故障与内存兼容。

## 3. 前端及 ai-fund

- [x] new-api 的 `period-types.ts`、`channel-period-policy-panel.tsx` 增加开关及保留服务端来源；补齐七语言、权限、草稿与预览保存组件测试。
- [x] ai-fund 的 `worker/src/newapi_period_policy.js`、`pool_period_limits.js`、相关客户端合同保留字段；继续验证映射、审计、committed 和迟到响应。
- [x] `frontend/src/components/PoolPeriodPolicyEditor.vue` 增加同义开关；覆盖不同渠道隔离、保存/重读、关闭草稿、映射变化与统计覆盖说明。
- [x] 从实际 Go 响应生成新增字段的合同 fixture，并让 Worker 和 Vue 回归消费该 fixture。

## 4. 验证

- [x] 定向 Go 测试覆盖 ChannelModelUsage、ChannelBudget、ChannelPeriod、ChannelUser 与 ChannelLimitFallback；必要时对并发计数/调整运行 race。
- [x] 根模块 `go build ./...`、相关包 `go vet`，业务功能稳定后运行根模块全量测试；若实际改到 relaykit，则必须 `cd relaykit && GOWORK=off go build ./...`。
- [x] SQLite/MySQL/PostgreSQL 验证 JSON 配置读写及旧 v2 数据兼容；本任务不借此改数据库 schema。
- [x] new-api `web/`：`bun run typecheck`、`bun run lint`、渠道相关组件测试、`bun run build`、按 i18n skill 完成同步检查。
- [x] ai-fund `worker/`：`node --test src/*.test.js` 与 `node --check src/index.js`；`frontend/`：`npm run test:components`、`node --test tests/*.test.mjs`、`npm run build`。
- [x] 增加真实 Go benchmark，并使用隔离 Redis 在固定活跃用户/模型规模上测量内存和写调用变化，保存命令、环境、数据规模及结果到 research/performance.md。禁止把耗时阈值做成易波动的单元测试或用循环次数代替业务断言。
- [x] Check-All 经 trellis-route 选择 inline，完成跨仓 full 检查与修复重检。
- [x] 按后续工作流更新预算权威规范和 ai-fund 对应规范，处理提交交付。

## 5. 变更边界与交付

新增核心逻辑及测试集中在模型用量领域文件。DTO、原预算模块、两端类型/白名单/表单是必要接入面；各供应商适配器、资金结算、通用 Relay 主循环和无关 UI 不进入范围。

保留原有任务记录、父任务、pycache 及 ai-fund 既有未推送提交；不做顺手整理、自动归档或生产开关修改。交付报告说明实际测试与性能观测，发布前必须落实新旧统计来源的回退方案。

## 6. 已执行验证记录（2026-09-13）

- 两仓功能实现完成，用户已确认的 Brief 对应功能均已落地；生产开关和部署未操作。
- Go：根模块全量测试、定向预算/模型累计/降级回归、race、build、相关包 vet 均通过。SQLite、隔离 MySQL 8.0 与 PostgreSQL 16 的 TEXT 策略往返及 CAS 测试通过。
- new-api web：7 个实际面板交互测试通过；typecheck、lint（无 error）、生产 build、变更文件格式检查通过；源码缺键 0，七语言 sync 的 missing/extras/untranslated 均为 0。
- ai-fund：Worker 全量 594 测试通过，index.js 语法检查通过；Vue 18 个挂载交互测试和 Node 测试集通过，生产 build 通过。新增 `channel-model-usage-contract.json` 由真实 Go Controller 生成并被 Worker、Vue 测试消费。
- 性能原始输出及解读见 `research/benchmark.txt`、`research/redis-storage.txt`、`research/performance.md`。100 用户 × 20 模型的样本额外使用 26,820 字节，累计脚本仍为每次结算一次。
- 实现中修复了可选字段排序导致预览消失的问题；检查中修复 FBK-001（上游模型累计元数据损坏被误分类为客户端 400），故障测试先失败、修复后 Worker 全量通过。剩余 CHK/FBK 为 0。
- 本次没有修改 relaykit、资金结算入口或数据库结构。两仓既有未提交任务记录与 pycache 保留。
