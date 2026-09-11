# 渠道周期预算与超限降级技术设计

## 1. 边界与实现原则

本设计实现 `prd.md` 的 R1—R6，两个仓库作为一个共享接口契约的交付完成验收。配置和实际拦截均属于 new-api；ai-fund 只管理、转发和展示。

新增策略、时间解析、池子计数、个人时段特批和降级协调放入独立文件。普通渠道字段、既有日周 Redis key、现有个人覆盖 API 和实际财务结算能力继续复用。以下合同已落到代码，验证证据见 implement.md。

策略采用独立表，不直接塞入完整渠道编辑对象：它包含稳定规则身份、并发版本、独立管理权限和个人规则关联，不属于供应商转发参数。这样旧版渠道表单或 ai-fund 的固定四字段更新不会覆盖新策略。同步上游时，主要冲突面集中在注册和少量调用点。

## 2. 持久化与配置合同

### 2.1 渠道策略

新增 `model/channel_period_policy.go`，模型 `ChannelPeriodPolicy`：自动主键、唯一 `ChannelId`、整数 `Revision`、TEXT `Config`、创建／更新时间与修改人。缺少记录表示池子不限、无时间规则、降级关闭。

`Config` 通过 `common.Marshal/Unmarshal` 编解码为 `dto/channel_period_policy.go` 中的版本化配置。首版字段：

| 配置 | 含义 |
| --- | --- |
| `schema_version` | 固定为 1，未知版本拒绝处理 |
| `pool_daily_quota_limit`、`pool_weekly_quota_limit` | 池子默认日、周额度，整数 `0..common.MaxQuota` |
| `rules[]` | 自定义时间规则，保存稳定 ID、名称、启用状态、时间定义和四项可空限额 |
| `fallback.enabled` | 显式开关，默认 false |
| `fallback.channel_id`、`fallback.model` | 唯一目标渠道及其对外可选模型，不是密钥或厂商模型地址 |

每条规则四项限额为 `user_daily_quota_limit`、`pool_daily_quota_limit`、`user_period_quota_limit`、`pool_period_quota_limit`。`null` 继承较低优先级；`0` 明确表示该项不限；正数施加上限。周上限沿用个人默认／特批和池子默认，避免时间切换把过去一周消费错误解释为时段消费。

保存使用 `expected_revision` 乐观锁：不存在时版本为 0，首次插入处理唯一键冲突；更新以渠道和旧版本作为条件，受影响行数为 0 返回冲突。数据库结构使用普通整数、字符串、TEXT，不用 JSONB、数据库布尔默认值或特定方言 DDL。普通和快速迁移注册均加入新表。

### 2.2 时间规则与稳定计量身份

- `date_range`：服务端解释起止本地日期时间，持久化明确 Unix 时间戳；GET 同时返回本地显示文本、时区和解析后的时间戳。时间使用服务器配置的 `time.Local`，不引入另一套个人日周时区。
- `weekly`：一条规则描述每周的开始星期／时间与结束星期／时间，可跨日、跨周；按自然日历生成本次 occurrence。多个不连续时段用多条规则表示，不合并成含糊的“工作日”或“周末”布尔字段。
- 两类规则均使用 `[start,end)`；拒绝空区间、非法日期和无法唯一解释的本地时间。每周起止相同不隐式当作一整周。
- 规则 ID 由服务端分配并长期稳定；计量身份为渠道、规则 ID、本次 occurrence 起点。修改名称或上限不更换身份。已经开始的 occurrence 不允许通过修改起止或规则类型改变当前累计边界；未来排期可编辑。
- 禁用／删除规则不删除已经形成的用量；重新启用同一规则继续使用原 occurrence。新建规则只统计创建后的真实结算，返回 `tracking_since`，不回填创建前的消费。
- 每条规则保留停用状态以支持恢复；不能复用已经用过的规则 ID 代表另一个时段。后台不运行定时“清零”任务。

### 2.3 个人时段特批

现有 `ChannelUserLimitOverride` 继续保存个人并发、日、周特批。新增 `model/channel_user_period_override.go`，以 `(channel_id,user_id,rule_id)` 唯一保存指定规则整段上限和到期时间，避免老版整条替换接口清除新增特批。

指定用户的整段特批只覆盖该规则的个人整段上限，不覆盖池子上限或其他规则。新规则特批使用独立 PUT／DELETE；现有三项特批仍用原接口。写入检查用户、规则归属、额度正数和最大值、到期时间，并与不含个人覆盖的有效基础上限比较，保持明确提额而非无意封禁。

按当前时间先解析默认→每周→指定日期，再将有效个人明确覆盖项作为最终值。已保存的个人覆盖保持最高优先级，不能因为时间规则切换重新取旧渠道默认与覆盖的 `max` 而吞掉新规则。没有新策略的渠道保留旧解析行为；有策略时的新增语义更新对应规范与测试。

## 3. 规则解析、状态与累计

### 3.1 独立解析器

新增 `service/channel_period_policy.go` 和 `service/channel_period_rules.go`，提供校验、版本写入、读取缓存、时间求交、逐字段生效来源与下次切换计算。时间通过可注入的时钟参数传入，测试使用明确日期，不用 sleep。

同级、同一指标、有效时间相交的规则拒绝保存；不同指标可重叠。日期规则不填写的指标继续继承每周规则。同一时刻可能有多个不同指标来源，状态需逐项返回 `source`、`rule_id`、规则名和 occurrence，不能只返回一个笼统的“当前规则”。

每日、每周与选定的整段额度同时约束。所有仍在其计量区间内的有效规则均累计正向用量，包括暂时被更高优先级规则遮盖的规则；只按最终解析结果执行限额。这样覆盖结束后不会重新拿到空白额度。

配置缓存使用 5 秒 TTL，Redis 开启时跨实例共用，写后通过比较版本的 Lua 脚本发布，不允许迟到的旧版本覆盖新版本；Redis 关闭时使用有容量限制的本地缓存。每次请求按当前时刻解析缓存中的配置，不能缓存一个跨越切换时间仍沿用的“有效上限”。数据库或配置解析失败必须与“查明没有配置”区分；无法确认策略时不按默认不限放行。

### 3.2 用量存储

新增 `service/channel_period_usage.go`，复用既有 Redis／内存两种运行模式。

| 计数 | 维度及存储 |
| --- | --- |
| 个人日／周 | 继续使用 `channel_user_daily_quota:{channel_id}:YYYY-MM-DD` 与 `channel_user_weekly_quota:{channel_id}:YYYY-MM-DD` |
| 池子日／周 | 新增同渠道 hash tag 的日、周计数，跨用户累计实际正向消费 |
| 整段 | 渠道＋规则 ID＋occurrence 起点；Hash 保存池子总量及各用户累计 |

各周期元信息保留 `tracking_since`；键 TTL 至少覆盖结束后 24 小时。未开启限制也记录池子正向消费；存在时间规则时即使整段上限暂为不限也追踪该规则的区间消费，以便随后启用上限。内存模式复用同样语义，但明确标为单进程状态，重启后起点更新。

在 `RecordChannelUserQuotaUsage` 协调入口扩展池子和时段记录，使现有普通文本、音频、图片、任务、Midjourney、工具费、违规费的正向记账都继承新计数。免费结果、零额结算、退款和负差额不增加也不减少这些正向计数。异步任务提交与后来正差额分别按各自实际结算时刻记账；跨时段的在途请求也是结算时刻归属，维持现有日周口径。

Redis 同渠道的一次正向记账用一个协调脚本更新个人日周、池子日周、所有命中的区间，预先验证 key 类型、整数范围和增量，避免写了一部分后才因类型／溢出失败。继续使用整数增量与 `HINCRBY`，不能用 Lua 浮点金额累计；不得盲目重试超时脚本导致重复入账。内存实现一次持锁完成同样操作。

上述协调只保证限额计数的原子更新，不把它误称为财务数据库和 Redis 的分布式事务。结算成功但计数写入失败时保留业务响应、写请求关联告警；新增 `model/channel_quota_tracking.go` 持久化最近一次缺口 Unix 时间（不记录金额），受影响周期返回 `coverage=incomplete`。数据库同样故障时在本实例保留待补写时间；数据库与计数存储同时不可用且进程丢失时，未落盘缺口无法恢复。所有新增计数固定标为 `since_tracking_start`，不宣称财务对账完整。已有财务一次性结算保护继续是重复回调的入口保护。

### 3.3 统一请求检查

新增 `service/channel_period_guard.go` 返回结构化阻断原因：`scope=user|pool`、`period=daily|weekly|custom`、当前值、上限、规则与重置时间。所有正数金额检查仍是 `used >= limit`，没有在途预占。

`controller/channel_user_weekly_quota.go` 与 `relay/channel_user_weekly_quota.go` 的领域入口扩展为统一检查。旧个人日周错误码继续有效；新池子／时段错误定义为独立稳定错误码，503 与额度耗尽 429 分开。没有获准降级的入口保持 `skipRetry`，本地拒绝不自动禁用渠道。

用户并发仍执行原租约逻辑，首版并发耗尽不触发金额降级。免费模型沿用既有价格判定和检查豁免；新增金额限制不变成全请求封禁。所有已经覆盖日周的入口、独立视觉辅助及 WebSocket 每轮检查均需落实新池子／时段约束。

## 4. HTTP 超限降级

### 4.1 调度顺序

新增 `controller/channel_limit_fallback.go` 和 `service/channel_limit_fallback.go`，以现有选渠结果为起点：

1. 保存原始 DTO、原始模型、入口路径和分组。每次候选使用独立深拷贝，尤其补齐当前 Responses 克隆的缺口。
2. 在副作用前完成候选的纯模型映射、价格／免费判定和限额预检。`PrepareRequestForSelectedChannel` 当前包含视觉辅助上游调用，因此在其纯准备和外部调用边界接入独立 guard，不能直接在整个准备完成后才决定降级。
3. 只有六类用户／池子金额耗尽且开关开启、入口为获准的三类 HTTP 接口、尚无上游副作用时允许降级。一次性目标标记独立于原有 `RetryTimes`，即使重试次数为 0 也可做一次配置内的降级。
4. 初选预检发生在获取并发租约之前；若前面已有普通重试，其旧租约已在尝试结束时释放。使用配置的明确目标，重建请求体、渠道上下文、准备状态、计费与工具价格快照。校验当前有效分组的 Ability、用户和 Token 模型权限、目标启用状态及端点支持；固定渠道 Token 不得借此逃逸。
5. 对目标重新检查所有限额，完成目标准备后获取其并发租约，再在预扣阶段校验余额并调用上游。计费前复检是处理并发变化的必要步骤；若预处理已发生上游调用，再次超限只能拒绝，不能重新执行整个请求。
6. 目标不可用或已降级则结束；源渠道额度耗尽不被送入普通上游错误重试／自动禁用机制。首版目标失败不再改换到第三个渠道。正常未降级请求的普通重试行为保持既有规则，但已经发生上游调用的请求不能之后再走额度降级。

通过请求级状态记录是否已有任何上游副作用、是否已用一次降级及最终选定来源。不能仅凭响应头已提交或 `RetryIndex` 推断是否已经访问上游。保活协议和现有错误输出约定继续保持。

### 4.2 能力与状态限制

目标支持判断以现有适配器和 Advanced Custom 路由能力为基础；客户端接口不改变，已有协议转换可复用，不新增转换器。工具、图片和原始透传字段必须可保留，无法保留时明确失败，禁止静默删掉请求能力。当前保守边界：实际适配器转换结果必须逐字段包含客户端 JSON（只允许顶层 model 改名与省略 false 的 stream），最终发送前再次检查。需要改变字段结构的跨协议转换、会删除字段的目标设置和 ParamOverride 不自动降级。厂商不同但原协议透传一致的目标可以使用。

含 `previous_response_id`、不透明上游会话／文件引用，或无法证明能在目标重建会话状态的请求不自动切换。HTTP `/v1/responses/compact` 原始透传及 WebSocket 不等同于获准的普通 HTTP Responses，本轮仅继承限额。

### 4.3 价格与审计

`OriginModelName` 始终保留客户端原始模型。新增本次路由模型字段和独立降级元信息，目标 DTO 使用配置的目标模型；实际映射后上游模型仍由原映射能力计算。

在 `relay/common/billing_model.go` 增加最小的降级模型选择分支：目标请求未映射时按目标模型；发生目标渠道映射时遵循该渠道既有计费开关。使用 `ResolveBillingModelName`→`FreezeBillingModelName` 统一冻结，不依赖修改 `OriginModelName` 冒充目标。结算继续读取既有冻结入口，不复制计费公式。

日志记录来源渠道、原始模型、目标渠道、路由模型、最终上游模型、触发的作用域／周期／规则和请求 ID；管理细节放 `other.admin_info`。成功降级写正常消费与降级元信息，避免把原渠道的可恢复超限重复记为最终请求失败。失败仍按现有统一错误日志去重，包含最终原因。

不改计费表达式、饱和换算或退款公式；实现中若需触及表达式调用，先读取 `pkg/billingexpr/expr.md`。目标价格无法解析则失败，不回用源模型价格。

## 5. 管理 API 与 ai-fund 联动

### 5.1 new-api 新合同

| 方法与路径 | 权限及行为 |
| --- | --- |
| GET `/api/channel/:id/period-policy` | `ChannelRead`，返回版本、配置、时区及服务器当前时间 |
| PUT `/api/channel/:id/period-policy` | `ChannelOperate`，白名单策略＋`expected_revision`，原子保存并审计 |
| POST `/api/channel/:id/period-policy/preview` | `ChannelRead`，只读解析日期、冲突及生效来源，不保存／不访问上游 |
| GET `/api/channel/:id/period-policy/targets` | `ChannelRead`，返回可配置的渠道／模型最小摘要，不含密钥；请求时仍重新鉴权 |
| PUT／DELETE `/api/channel/:id/period-rules/:rule_id/user-overrides/:user_id` | `ChannelOperate`，指定规则个人整段提额／撤销，支持到期 |

扩展既有 `/user-limit-status/:user_id`，旧并发／日／周字段保持结构，只更新正确生效值；新增 `period_limits` 对象，包含 `schema_version`、池子日周指标、各规则个人／池子区间指标、逐项生效来源、`next_change_at`、`tracking_since`、`coverage` 和安全裁剪的降级状态。扩展旧日周列表与个人覆盖列表的生效显示，使其与统一状态一致。

明确配置不存在时返回版本 0 的默认对象，能力不支持或上游版本旧时不得伪造新功能可用。管理写后重读权威状态，保存成功但刷新失败应有可区分的结果，避免用户重复提交。

### 5.2 ai-fund

新增 `worker/src/pool_period_limits.js` 承载新策略、预览、目标选项和整段特批 BFF；`newapi_client.js` 只增加明确端点客户端和严格归一化，不透传任意路径。

在 `/api/admin/pools/:poolId/` 下提供与上述策略对应的 `period-policy`、`period-policy/preview`、`period-policy/targets`、`period-rules/:ruleId/users/:userId/override` 路径。写请求同时包含 `expected_channel_id` 与策略相关的 `expected_revision`。先重读 D1 防止错渠，再由 NewAPI 处理配置版本冲突。

所有金额显式使用展示值字段并按 `quota_per_unit` 换算，时间定义按服务端时区合同转发。Worker 同时扩展 NewAPI 入站归一化和本人／管理状态的出站白名单；GET `/api/pools/limits` 继续从认证绑定取本人身份、逐池隔离错误，不扫描全部用户来找本人。

D1 只写现有映射与本地管理审计，不新增策略、个人覆盖或用量副本。旧渠道三项个人默认写入仍保持四字段出站。上游不支持新版策略接口时显示功能不可用，不以 0 假装不限。

## 6. 界面组织

- new-api：在既有渠道用户限制工作区加入池子预算、时间规则和降级配置子组件；个人特批保留用户搜索入口，新增按规则的整段上限。API、类型、表单 schema、查询失效在 channels feature 内维护。
- ai-fund：`PoolLimitAdminModal.vue` 继续作为 `/pools` 与 `/admin` 共用壳，新增 `PoolPeriodPolicyEditor.vue`、`PoolPeriodOverrideEditor.vue` 等子组件；`PoolLimitPanel.vue` 区分个人与池子指标并展示限制原因、统计起点及恢复时间。
- 规则编辑以命名规则、起止时间、每周区间和逐项额度字段组成；不以“周末模式”限制日期范围。覆盖编辑展示当前基础值与特批后值。
- 时间输入交由服务端预览解释，浏览器时区不能偷偷改变规则；显示服务器时区。下次切换与额度重置分开表示。
- 写后刷新必须保留请求归属和尾随刷新；切池、切页签、关闭组件时废弃迟到结果。旧版本策略载荷显示不可用状态，不暴露管理凭据。
- NewAPI 七语言文案通过 i18n skill 同步，React 组件按 Base UI 组合与键盘可访问要求实现；ai-fund 保持 Vue 组件及中文文案约定。

## 7. 文件所有权、薄接入及回滚

| 新增文件／模块 | 完整职责 |
| --- | --- |
| `model/channel_period_policy.go`、`model/channel_user_period_override.go` | 新表、版本更新、规则特批查询 |
| `dto/channel_period_policy.go` | 策略／管理合同与状态 DTO |
| `service/channel_period_{policy,rules,usage,guard}.go` | 解析、缓存、累计、检查 |
| `service/channel_limit_fallback.go`、`controller/channel_limit_fallback.go` | 降级资格、目标检查与一次切换协调 |
| `controller/channel_period_policy.go`、`controller/channel_user_period_override.go` | 独立管理 API |
| `relay/common/channel_limit_fallback.go` | 请求级降级状态与模型读取辅助 |
| channels feature 的新规则编辑和状态子组件 | NewAPI 用户可见交互 |
| ai-fund `worker/src/pool_period_limits.js` 与 Vue 新子组件 | BFF 与门户交互 |

| 原有文件 | 必要的最薄接入点 |
| --- | --- |
| `model/main.go` | 两种迁移路径注册新表 |
| `router/channel-router.go` | 注册明确权限的新 API |
| `service/channel_user_limit_override.go` | 读写特批时接入时间规则基线／优先级，不内嵌解析算法 |
| `controller/channel_user_limits.go`、`channel_user_weekly_limits.go`、`channel_user_limit_overrides.go` | 旧列表／统一状态接入正确有效值和新指标 |
| `service/channel_user_quota_usage.go`、`text_quota.go`、`quota.go`、`tool_billing.go` | 委托统一限额计数协调；文本、音频、工具、任务、Midjourney 的调用点移至资金结算成功后，金额公式不变 |
| `controller/channel_user_weekly_quota.go`、`relay/channel_user_weekly_quota.go` | 接入结构化检查和稳定错误映射 |
| `middleware/distributor.go` | 刷新有效限制上下文；降级请求不把来源亲和性误写为成功目标 |
| `controller/relay.go`、`relay_attempt.go`、`relay/vision_assist.go` | 副作用前 guard、深拷贝和一次目标切换；必要性见第 4 节 |
| `relay/common/relay_info.go`、`billing_model.go` | 状态字段和集中计费模型选择，不重写各适配器 |
| `relaykit/types` 中实际错误码定义文件 | 增加稳定错误码，保持独立模块无根模块依赖 |
| `service/log_info_generate.go` 与管理审计 action 模板文件 | 委托新增降级日志元信息，保留一次性日志保护 |
| NewAPI `web/src/features/channels` API／类型／工作区、七语言文件 | 注册新请求和子组件，不重做全站界面 |
| ai-fund `worker/src/newapi_client.js`、`pool_limits.js`、`index.js` | 客户端白名单、公开状态、路由注册；规则判定仍由 NewAPI 承担 |
| ai-fund `frontend/src/api/index.js`、`PoolLimitAdminModal.vue`、`PoolLimitPanel.vue` | 前端接口与复用壳接入 |

优先复用 JSON、GORM、Redis、已有价格冻结、BillingSession、租约、用户搜索和审计能力，不为 DRY 重构上游内部架构。

回滚先关闭新降级与规则、恢复旧默认配置，再回退薄接入及独立模块。新表和计数键保留，绝不通过删除历史消费恢复行为。回退旧二进制将停止新增限制执行，应明确展示其运营影响。重新上线后不补记中间缺失消费，统计完整性不得伪造。

## 8. 实施与验证风险

- 软限额允许在途超额，计数与财务提交存在既有跨存储边界，不能宣称严格预算。
- 新计数从部署／规则创建开始，不完整周期必须有可见标记；内存模式只覆盖当前实例。
- 已有规范中“个人覆盖只提额且读取失败回落默认”“超限永不换渠”需要按有无新策略及降级开关更新，不能直接推翻旧渠道默认行为。
- 副作用前路由、Responses 深拷贝、目标价格冻结和高优先级规则遮盖期间累计是先写回归场景再接入的重点。
- 目标模型的全部语义能力无法仅靠模型名称证明，使用现有明确能力及实际请求校验；不可迁移请求明确失败，不以删字段伪装兼容。
- 验证采用本地数据库、Redis 测试服务及模拟上游，不使用真实业务凭据或请求生产模型。

## 2026-09-11 本人面板反馈修正

- `PoolLimitPanel.vue` 使用同一套卡片模板渲染个人与号池两组指标。个人日周继续读取既有 `daily_quota` / `weekly_quota`，只从周期状态补充来源；号池指标读取服务端 `scope=pool` 的状态，不从个人金额推导池子总量。
- 生效整段指标按 `scope` 放入所属分组；非生效规则仅在管理入口查看。默认状态缺失时保留个人指标并显示号池状态不可用，不生成零值占位。
- 本人视图停止叠加面向管理的 `PoolPeriodMetrics` 全指标明细；管理工作区继续使用原明细组件。本人页不单列规则与统计说明，规则来源和统计边界通过卡片 title 备查，管理入口保留完整明细。
- 线上排查仅通过授权 SSH、Docker、数据库及 Redis 只读命令；不重启、不执行回填、不改变真实账单。

- 后续线上恢复授权采用固定重建截止点的历史增量；Lua 先校验全部目标键和数值，再一次性累计 8 键并写幂等回执；保留恢复过程中正常结算，单独备份宿主机回执。Redis 数据目录迁移和 AOF 修复涉及服务重启，另待用户明确回复。

- 用户已明确确认“现在执行持久化修复”：允许备份当前 Redis 数据、短暂停止 NewAPI/Redis、挂载 `/root/new-api/redis-data:/data`、开启 AOF 每秒同步，并在恢复服务前验证全部个人／号池计数保持。

- 最新界面反馈：本人面板去掉“规则与统计说明”及额外规则说明文字，只保留额度卡片。详细规则在管理入口；卡片标签和金额的 title 可保留必要来源与统计边界。按用户此前明确要求继续部署最新前端。

## 2026-09-11 最终降级提示实现边界

- ai-fund 的 `getSelfPoolLimit` 复用既有本人统一状态；仅渠道启用且 `blocked && fallback_enabled` 时读取既有周期策略，从同一 revision 获取模型。
- 触发原因只取本人状态中 `enforced && limit > 0 && used >= limit` 的指标；公开 `period_limits.fallback` 仅含模型与 scope/period，不返回目标渠道、密钥或他人状态。
- 策略按渠道与 revision 缓存 3 秒，个人原因不共享；读取失败、配置变化或版本不一致时仅对应号池暂不可用。冷请求最多 7 次 NewAPI HTTP 调用，正常用户不额外读取策略。
- NewAPI 不增加展示字段，最终版本仅发布 ai-fund Worker 与 Pages；不改额度计算、请求预检或财务结算。
