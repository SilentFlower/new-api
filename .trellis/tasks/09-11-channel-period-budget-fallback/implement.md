# 渠道周期预算与降级实施计划

## 1. 执行前提与上下文

- 当前任务为 `.trellis/tasks/09-11-channel-period-budget-fallback`，已获用户确认，已通过 `task.py start` 切为 `in_progress`，implement 与 check 均使用已保存的 inline 路由。
- 工作仓库为 `/root/project/new-api` 和 `/root/project/ai-fund`。执行前检查两仓实际 Git 差异、各自当前任务及 AGENTS.md；保留研究记录中的初始 pycache 和任何用户后来添加的修改。
- 按 manifest → PRD → design → 本计划读取上下文。实现前使用 `trellis-before-dev` 加载对应层规范，切换 ai-fund 时按其本地规范加载，不把 new-api 的 React 约定用于 Vue。
- 应用 `.trellis/spec/guides/build-upstream-friendly-customization.md`：独立模块承载完整业务，热点只加必要调用和注册；不重命名或整理无关代码。
- NewAPI UI 工作加载 shadcn-ui、React 性能规范及 i18n-translate；添加任意 `t(...)` 文案前先加载 i18n skill。业务 JSON 一律 `common.*`；新增 Go 测试按 `require`／`assert` 编写。

## 2. 有序实施清单

### A. 固化管理合同与规则计算（R2、R5、R6）

- [x] 新增 `dto/channel_period_policy.go`，定义版本化配置、规则、预览、逐指标状态、规则特批和版本冲突输入；准确区分 0、null 和字段省略。
- [x] 新增 `model/channel_period_policy.go`、`model/channel_user_period_override.go`，实现唯一键、版本比较更新和普通 GORM 查询；在 `model/main.go` 的普通与快速注册路径均接入。
- [x] 新增 `service/channel_period_policy.go`、`channel_period_rules.go`，实现服务器时区、绝对／每周区间、半开边界、冲突检测、稳定规则身份、逐项优先级、到期回落与缓存失效。
- [x] 在 `service/channel_user_limit_override.go` 接入当前时间基线与个人明确项优先级，保留无新策略时旧行为；加入按规则整段特批，原三个字段的旧 API 不清除新记录。
- [x] 为非周末日期、跨日／跨周区间、重叠冲突、空值继承、永久／到期个人特批写确定性表驱动测试；比较配置前后生效值，不能只断言内部常量。
- [x] 验证策略修改中的乐观锁与写后读取、非法字段、不可用数据库／缓存、无历史用户、两种迁移入口。

### B. 扩展正向计数及所有既有限额入口（R1、R2、R6）

- [x] 新增 `service/channel_period_usage.go` 和 `channel_period_guard.go`，实现池子日周、规则个人／池子整段计数与结构化阻断原因。
- [x] `service/channel_user_quota_usage.go` 委托同渠道计数协调；保留旧日周 key。Redis 原子更新前预检 key 类型和整数边界，内存以一个持锁段完成相同更新。
- [x] 记录部署／创建后的 `tracking_since` 与统计完整性；在没有真实历史时不从可人工调整的个人列表猜测池子总量。
- [x] 为被日期规则遮盖的每周规则持续累计、跨周时段不清零、禁用后恢复、规则上限修改不换计数身份建立回归。
- [x] 扩展 Controller 和 Relay 两侧检查入口；旧个人日周错误码保留，新池子／整段错误明确 429，配置／Redis 故障明确 503。
- [x] 按研究文件逐一追踪普通文本、图像、音频、实时／WebSocket、任务、Midjourney、视觉辅助、工具费、违规费及异步正差额；只在正向一次性结算处累计，不新增重复记账。
- [x] 断言免费／零额／退款不累计、人工设置个人已用值不重置池子、相同用户多 Token 共用而不同渠道隔离、Redis 故障不切内存。

### C. 实现受控 HTTP 降级与目标计费（R3）

- [x] 首先增加回归用例：现有准备可能访问视觉辅助上游、Responses 原 DTO 可能被重用、源价格可能残留到目标；输入和期望均围绕实际上游调用次数、请求内容和最终账单。
- [x] 新增 `service/channel_limit_fallback.go`、`controller/channel_limit_fallback.go` 与 `relay/common/channel_limit_fallback.go`，实现资格判断、请求级一次标记、目标权限／能力校验和上下文重新准备。
- [x] 为三个获准 HTTP DTO 提供独立克隆；源、目标、任何其他请求不能共享被模型映射改变的结构。
- [x] 在 `controller/relay.go`、`relay_attempt.go` 和 `relay/vision_assist.go` 的必要边界接入纯预检与最终复检，确保外部副作用之前判定路由；副作用发生后不得再自动切换。
- [x] 目标重新校验分组 Ability、Token 模型权限、固定渠道约束、端点能力、六类金额限制和并发租约；旧租约释放一次，目标失败不去第三个渠道。
- [x] 每次降级重新查目标价格并冻结；`OriginModelName` 保留原始请求，计费模型由 `relay/common/billing_model.go` 集中解析。工具价格快照、映射和预扣状态一并清理。
- [x] 调整请求亲和性记录，成功降级不得把来源渠道超限污染成目标成功或把正常恢复永久锁在备用渠道。
- [x] 在统一日志生成处增加原始／目标模型和限制原因，保持错误日志去重、正常消费只结算一次及 admin_info 隐私边界。
- [x] 覆盖普通上游错误后不可追加额度降级、响应已开始不可拼接、会话引用不可迁移、目标不兼容、价格缺失、目标也超限、RetryTimes=0 仍有一次获准降级。
- [x] WebSocket、任务、Compact 和其他未获准入口测试“执行新限额但不切换”，不得因共用检查函数被误放行。

### D. 完成 NewAPI 管理 API（R4、R5、R6）

- [x] 新增策略读写、预览、目标选项和规则特批 Controller，在 `router/channel-router.go` 注册精确的 `ChannelRead`／`ChannelOperate`。
- [x] 扩展统一状态、日周用户列表与覆盖列表，返回与 Relay 相同的有效值、逐项规则来源、池子与时段指标及统计起点。
- [x] 校验策略版本冲突、规则归属、用户存在、非管理员／无权限、金额范围、未知字段和敏感字段裁剪；管理操作写结构化审计。
- [x] 在 `research/` 保存一份由实际 Controller fixture 返回的脱敏 JSON 合同示例，供 ai-fund 客户端和组件使用；不能用手写样例代替实际响应验证。

### E. 实现 ai-fund BFF（R4）

- [x] 新增 `worker/src/pool_period_limits.js`；在 `newapi_client.js` 增加严格的出入站归一化，在 `index.js` 注册对应 admin 路由。
- [x] 每个写操作保留 D1 `expected_channel_id` 校验，再向 NewAPI 传 `expected_revision`；字段逐一构造，禁止展开完整渠道对象。
- [x] 所有金额通过既有每单位额度换算；时间由 NewAPI 预览解释，Worker 不复制规则优先级或周期算法。
- [x] 扩展 `pool_limits.js` 的本人和管理状态输出，沿用绑定身份、逐池失败隔离及写后缓存失效／权威回读。
- [x] 保存成功与回读失败区分，成功审计不因回读异常丢失；上游旧版本或未支持字段显示功能不可用，不生成假零值。
- [x] 补充 `pool_period_limits.test.js`、既有客户端／路由／映射测试，使用 D 段实际合同 fixture 验证贯穿两层白名单。

### F. 完成两个管理界面与本人状态（R4、R6）

- [x] new-api 在 channels feature 新增规则、池子预算、降级和整段特批子组件，接入现有用户限制工作区；API／types／schema 同步，保留无历史用户搜索。
- [x] ai-fund 新增 `PoolPeriodPolicyEditor.vue`、`PoolPeriodOverrideEditor.vue` 及必要的状态子组件，由现有 `PoolLimitAdminModal.vue` 两种壳复用，不复制页面。
- [x] 两端展示个人与池子日／周／整段指标、命中规则、个人特批、下次切换、重置时间与统计起点；编辑时使用服务器时区预览，允许清空数值后重新输入。
- [x] 保留版本／映射冲突、迟到响应隔离、尾随刷新、关闭后停止请求、键盘操作、只读权限和无效输入提示。
- [x] NewAPI 七语言同步并扫描源码缺键；ai-fund 文案和组件继续遵循其中文约定。
- [x] NewAPI 使用当前 Bun＋happy-dom 真实交互测试；ai-fund 补齐可运行的 Vue 交互测试入口，保护跨池切换、保存预览和冲突反馈，不用源码字符串快照代替交互。若需要新增测试开发依赖，限定在 ai-fund frontend 并保留锁文件增量。

## 3. 验证矩阵

| 需求／验收 | 主要验证层 |
| --- | --- |
| R1，AC1、AC8 | Redis／内存精确计数、边界等值拒绝、两个在途请求合法超额、渠道／用户隔离 |
| R2，AC2、AC9、AC10 | 时间解析、任意假期、跨周区间、覆盖仍累计、统计身份不被修改重置 |
| R5，AC3 | 永久／临时个人优先级、单指标覆盖、不绕过池子、到期回落 |
| R3，AC4、AC5、AC6、AC11 | 三接口实际模拟上游、目标权限、深拷贝、租约释放、价格冻结、一次结算与日志 |
| R4、R6，AC7、AC12 | Controller 合同→Worker 归一化→两个 UI，版本／映射冲突、旧响应隔离和统计起点 |

### 定向验证

在 new-api 根目录，按 A—D 的实现阶段运行相应包，不在每次小改后重复全量构建：

```bash
go test ./model ./service ./controller ./relay/... ./middleware ./router -run 'Channel.*(Quota|Limit|Period)|PeriodPolicy|LimitFallback|BillingModel|MappedUpstreamModel' -count=1
go test -race ./model ./service ./controller ./relay/common -run 'Channel.*(Quota|Limit|Period)|PeriodPolicy|LimitFallback|BillingModel' -count=1
```

在 new-api/web：

```bash
bun test src/features/channels
bun run typecheck
bun run lint
bun run build
```

在 ai-fund/worker：

```bash
node --test src/newapi_client.test.js src/pool_limits.test.js src/pool_period_limits.test.js src/index.test.js src/settings.test.js
node --check src/index.js
```

在 ai-fund/frontend：

```bash
node --test tests/*.test.mjs src/utils/*.test.mjs
npm run test:components
npm run build
```

新增用例名称应匹配上述筛选；确需调整文件名时同步命令与实际证据，不能把未执行的命令记为通过。

### 最后一轮完整检查

- 通过 `trellis-route(target=check)` 进入统一 Check-All；范围包括两仓整体差异和 AC1—AC12。该任务涉及权限、计费、多入口和跨仓合同，应完整核对实现假设及覆盖。
- new-api：`go test ./...`、`go vet ./...`；在 `relaykit/` 单独执行 `GOWORK=off go build ./...`、`GOWORK=off go vet ./...`，并运行受影响独立模块测试。
- new-api/web：复用变更后有效的定向测试、typecheck、lint、build 证据；按 i18n skill 运行 `bun run i18n:sync`、源码缺键扫描及格式检查。若同步产生新变更，补做对应验证。
- ai-fund/worker：`node --test src/*.test.js`；前端沿用变更后有效的全套相关测试和 build。
- 两仓 `git diff --check`。数据库测试显式建表和初始化；三库模型结构／迁移分别核验，有环境时运行三库集成 fixture，缺少某方言实测必须在证据中明确，不宣称已验证。
- 联调使用本地数据库和可记录请求的模拟上游，运行“额度耗尽→目标请求→唯一消费记录→门户状态”以及目标拒绝场景。无需生产凭据或供应商费用。

## 4. 同步友好性与回滚检查点

- A 完成后复核：新增表与独立规则模块可单独撤销，普通渠道更新不带新策略对象，旧 API 仍有回归。
- B 完成后复核：源财务结算调用点只增加协调入口；未重写金额公式，旧个人 key 可被原接口读写，池子不从人工个人值推算。
- C 完成后复核：每个 Relay 热点修改都能解释为 guard、目标分派、状态字段或克隆修复；没有大规模通用重构。上游同步后重点复核 `controller/relay.go`、`relay_attempt.go`、`relay/vision_assist.go` 的顺序以及 `billing_model.go` 的唯一模型选择职责。
- E/F 完成后复核：ai-fund 旧四字段写入、两个管理入口共用工作区及认证绑定身份没有被替换。
- 回滚新能力前先关闭策略与降级；不删除新表、Redis 历史计数或财务日志。旧版本不会执行新增限制，部署回滚必须显式说明这一影响。
- 规范更新进入 `trellis-update-spec`：分别更新两仓的限额、个人覆盖、降级、计费模型和 BFF 契约。版本发布、线上启用、提交／推送另按届时明确授权执行。

## 5. 当前证据状态

当前两仓业务实现、本地 Check-All 与规范同步完成。实现与验证详情如下。


### 2026-09-11 实际验证证据

- `go test ./...`、`go vet ./...` 通过（日志 `/tmp/channel-period-go-full.log`、`/tmp/channel-period-go-vet.log`）。
- `go test -race ./model ./service ./controller ./relay/common -run 'Channel.*(Quota|Limit|Period)|PeriodPolicy|LimitFallback|BillingModel' -count=1` 通过（`/tmp/channel-period-go-race.log`）。
- SQLite、MySQL 8.0、PostgreSQL 16：`TestChannelPeriodPolicyDatabaseContract` 在本地隔离容器验证两次迁移、首次保存竞争、旧版本冲突、覆盖 upsert/删除、缺口时间单调性，三方言均通过。临时容器已清理。未实测 MySQL 5.7/PostgreSQL 9.6 最低版本。
- 三协议 × 流/非流共 6 项真实模拟上游验证唯一请求、目标计费模型、钱包扣款、消费日志和池子状态；完整 Relay 验证 RetryTimes=0、拒绝目标、恢复、Compact 不降级与并发租约释放。补充 WebSocket 回合准备、Sora 任务、Midjourney 在启用降级时池子耗尽仍拒绝的 3 项回归，断言上游请求、扣款和消费日志均为零。
- 内存和 Redis fixture 验证任意工作日假期、5 日每日 5/整段 15、软超额 140、到期、遮盖仍累计、跨周、禁用恢复、整数溢出原子性、缓存损坏与跨实例迟到发布。
- Worker 全量 582 项通过；实际 Controller JSON 保存在 `ai-fund/worker/src/fixtures/channel-period-contract.json`，贯穿客户端及 BFF 两层白名单、映射/版本冲突和已保存但回读失败审计。
- React channels 42 项通过，typecheck/lint/build 通过；Vue 真实挂载 3 项、既有 Node 前端 31 项、build 通过。lint 和构建现有 warning 保留，零 error。
- 新增 63 个翻译键 × 7 语言，源码缺键为 0、所有新增占位符一致，临时脚本已删除。已有未翻译扫描命中品牌/技术名和历史词条，不属于本次新增。
- `relaykit` 在 `GOWORK=off` 下独立 build/vet 通过；`./types` 无测试文件。两仓 diff 空白检查通过。
- 最终检查修复：配置缓存发布比较 revision；阶梯预检不执行零输入表达式；所有正向累计入口只在资金成功后调用；测试等待异步退款与 WebSocket 性能指标任务收口。
- 最后新增测试后，Controller 全包 `go test -race ./controller -count=1` 和 `go vet ./controller` 通过（`/tmp/channel-period-controller-final-race.log`）。未改动产品代码的其他包复用前述全量有效证据。
- 以上不代表真实供应商或生产部署验收；没有使用生产配置、数据或凭据。
