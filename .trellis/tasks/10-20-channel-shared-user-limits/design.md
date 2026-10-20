# 技术设计：父渠道限制继承

## 目标与边界

实现 prd.md 的 R1–R11。现有物理渠道继续决定鉴权、选路、上游请求、账单和日志；新增“限制归属”只决定并发租约、预算策略、个人覆盖和用量桶。父渠道 A 的现有 ID 与管理 API 不变，ai-fund 固定管理 A。

## 现有实现依据

- 并发租约：`service/channel_user_concurrency.go:139`，Controller 和 Relay 各自通过独立 guard 接入。
- 有效并发与请求上下文：`service/channel_user_limit_override.go:69`、`middleware/distributor.go:455`。
- 预算检查：`service/channel_budget_guard.go:46`、`:241`；计数身份与正向写入：`service/channel_budget_usage.go:90`、`:156`。
- 最终 Relay 累计：`service/channel_user_quota_usage.go:48`；异步正向差额：`service/task_billing.go:267`。
- 请求快照：`relay/common/relay_info.go:79` 的 ChannelMeta；任务私有 JSON：`model/task.go` 的 TaskPrivateData。
- 管理入口：`controller/channel_user_limits.go`、`channel_user_limit_overrides.go`、`channel_period_policy.go`、`channel_budget.go`；权限注册：`router/channel-router.go`。

## 持久化与关系管理

新增独立的 `model/channel_limit_parent.go` 领域模型保存关系，不把结构化关系塞入 Group/Tag，也不为此更改 Channel 的既有字段语义。

拟新增 ChannelLimitParent：唯一 ChannelId、ParentChannelId、Revision、UpdatedAt、UpdatedBy；ParentChannelId=0 表示独立，缺少记录等价于独立且 revision=0。解除关联保留 revision 记录，避免旧版本请求或缓存重新覆盖新关系。普通和快速迁移均注册此模型，使用三库支持的普通整数列和 GORM，无数据库布尔默认值。

关系保存使用事务和 revision CAS。锁定涉及的渠道时使用既有 lockForUpdate(tx)，按 ID 稳定排序；校验父子均存在、不是自身、目标是顶层渠道、来源没有子渠道。三库分别验证并发创建关系和删除竞争，不能依赖 GORM v1 query_option。单删、批删、删除已禁用渠道均必须阻止删除仍被引用的父渠道。复制渠道默认不复制外置关系。

按状态清理先获取候选 ID，再由独立 `model/channel_status_cleanup.go` 在删除事务内按 ID 和原状态重新查询、锁定并筛选；只有仍匹配的渠道才进入父引用保护、渠道及能力删除，避免查询后重新启用仍被误删。该模块有意镜像 `BatchDeleteChannels` 的分块、渠道/能力同事务删除及错误回滚，不为复用改写原批删流程；上游同步需复核这两条清理路径，测试同时保护父引用和重新启用场景。

新增独立管理入口，路径位于 `/api/channel/:id` 下：

| 方法与后缀 | 权限 | 合同 |
| --- | --- | --- |
| GET `/limit-parent` | ChannelRead | 返回 channel_id、parent_channel_id、revision、父渠道安全摘要和 read_only |
| GET `/limit-parent/options` | ChannelRead | 使用 keyword/p/page_size 查询可用顶层父渠道，返回 id/name/status 等必要摘要，不返回 Key |
| PUT `/limit-parent` | ChannelOperate | `{expected_revision,parent_channel_id}`，0 表示解除；缺字段、非法类型、未知键拒绝 |

输入 DTO 采用指针区分缺省与显式 0。写成功失效当前关系缓存并记管理审计；冲突返回 409，非法关系 400。权限不新增角色，不降低既有敏感删除权限。

## 统一限制归属解析

新增 `service/channel_limit_parent.go` 负责关系短缓存、父渠道读取和一次尝试的限制快照。缓存借鉴现有版本化策略缓存：有界、本地短 TTL、版本发布不倒退、网络和数据库调用不持全局锁。管理员写保护使用权威关系，不能因过期缓存允许对 B 写入。

- 无父：owner=A 或 B 自身，保持现有行为。
- 有父：owner=A；读取 A 的基础并发、个人覆盖、预算、时段及预算提额。
- 父记录丢失、关系非法或存储失败：返回明确本地错误，受限请求失败关闭，不能退回 B 自己的不限配置。
- 配置生效仍遵循现有缓存传播边界，关系变更不声称跨实例瞬时强一致。关系启用前暂停相关新流量并等待当前请求结束，更新所有实例后再启用；界面提示切换只作用于后续尝试。

Gin 使用专用 ContextKey 保存限制归属，原 ContextKeyChannelId 保持物理渠道。ChannelMeta 新增内部 ChannelLimitOwnerID，InitChannelMeta 从已解析上下文冻结；普通重试重新冻结新候选，已结束租约在重试前释放。

视觉辅助切换/恢复上下文时必须同步保存归属键；主请求与辅助请求分别按其选定渠道解析，避免主请求的父归属污染辅助调用。Realtime/Responses WebSocket 的租约和相关结算使用连接开始的归属，不在一次连接中途搬迁租约。

## 并发、预算与累计

### 并发

Controller 与 Relay guard 向既有 AcquireChannelUserConcurrency 传 ownerID，复用 A 的原子 Lua/内存租约、索引、心跳、取消与幂等释放。只持有一份有效限制租约，不额外占 A/B 两个名额。A 的管理列表自然枚举父子合计租约。

第 5 个请求沿用 429 和 SkipRetry；存储/续租故障沿用 503。两种错误继续请求级去重，不进入自动禁用；取消传播仍绑定实际上游网络请求。

### 金额

检查使用 owner 的策略、覆盖与计数，匹配模型仍为本次 RoutingModel。A 的计数 key 和既有预算 ID 不变，因此已有累计和 ai-fund 引用保持有效。B 新请求的资金结算成功后，将新增正向用量写入快照 owner；不再次把 A 当成真实物理渠道记账。

`RecordRelayChannelUserQuotaUsage` 从 ChannelMeta 快照取 owner。未关联旧调用仍以自身 ID 兜底，但新的关联请求必须在建立快照时验证成功，不能在累计阶段重新解析当前父关系。原底层记录函数继续只做传入归属桶的计数，不新增一套金额计算。

任务的 TaskPrivateData 新增可选 ChannelLimitOwnerID 快照，提交时保存当前 owner；`RecalculateTaskQuota` 对正向差额使用该快照。历史任务缺字段时使用其原 ChannelId，不按后来关系改记父渠道。字段在私有 JSON，兼容旧数据、不对普通用户暴露。归属不改变资金、令牌、退款或物理 used_quota 的现有更新。

B 关联前预算和提额保留，但关联期间不参与拦截；仅 A 的限制计数持续汇总 B 的新增消费。解除后恢复 B 原配置及其保留的控制用量，不把 A 的共享累计搬回 B。账单历史不丢失；如需调整恢复后的控制用量，使用现有人工调整能力，不自动重建历史。

### 降级与重试

普通重试仍由 SkipRetry 和现有 guard 的 break 处理。额度预检/显式降级读取父配置，同时保留物理来源用于实际路由和日志。切换目标后重新解析其限制归属；同一共享额度已耗尽时不得访问关联目标上游。原同物理渠道换模型等动作仍需通过目标全部适用预算，不能省略检查或产生第三跳。

## 管理只读与 ai-fund 兼容

B 的限制读取返回 A 的有效数据，响应中的 channel_id 保持请求路径的物理渠道 ID；继承来源由新增关系接口提供，不把新字段硬塞进预算 v2 Config。

并发覆盖、预算保存、预算提额、用量调整、撤销等限制写入口统一拒绝关联中的 B，返回稳定的只读/状态冲突错误并指明应管理父渠道。通过权威写保护后始终写路径渠道自身，不能再次按缓存关系重定向，防止跨实例解绑后的旧缓存或并发关系变化导致反向写父。普通渠道更新中若试图改变 B 的并发，也必须拒绝；表单原值回传不得意外清空 B 备份配置，需在 Controller 规范化处识别真实修改。

A 的读写完全保持既有合同。ai-fund 依然从 A 读取同一形状的状态、策略、用量和个人覆盖；父状态包含 B 计数，不让 BFF 再次相加。验证父接口 identity、revision、分页与白名单，不修改 ai-fund 源码或自动改其映射。

## 前端交互

新增 `channel-limit-parent-panel.tsx` 和独立 API/类型模块，嵌入现有渠道用户限制弹窗上方，不改五页签顺序。独立渠道可选择父渠道并保存；关联渠道显示「继承自 A」和只读状态，可解除或进入父渠道管理。

预算、时段、用量和个人覆盖保持可查看，继承写操作禁用；渠道编辑抽屉里的并发项同样只读。所有文案七语言同步。父候选支持名称与 ID 区分，同名可辨，当前父即使不在当前分页也保留显示。脏草稿处理沿用既有确认逻辑，渠道切换/弹窗关闭/关系变化时隔离迟到请求并刷新关联查询。

## 新增职责与旧文件薄接入

| 新增文件或文件组 | 完整职责 |
| --- | --- |
| `model/channel_limit_parent.go` 及测试 | 关系持久化、CAS、拓扑与删除约束 |
| `model/channel_status_cleanup.go` 及测试 | 按状态清理的事务内复核、锁定与父引用保护 |
| `dto/channel_limit_parent.go` | 新关系管理输入/安全视图，均明确为本任务新增 |
| `service/channel_limit_parent.go` 及测试 | 限制归属、短缓存、请求快照与只读保护 |
| `controller/channel_limit_parent.go` 及测试 | 关系读取、候选、保存及管理错误映射 |
| 前端 `limit-parent-api.ts`、`limit-parent-types.ts`、独立面板与 `__tests__` | 关联设置、继承状态与交互验证 |
| 新增父关联集成测试 | 真实管理/Relay/模拟上游/结算及 ai-fund 合同 |

| 必须修改的旧文件 | 最薄接入与必要性 |
| --- | --- |
| `model/main.go` | 普通/快速迁移注册新模型 |
| `model/channel.go` | 删除入口调用关系删除保护，不重构渠道 CRUD |
| `router/channel-router.go` | 注册三条关系路由及既有权限 |
| `constant/context_key.go`、`relay/common/relay_info.go` | 增加内部归属键与快照，不改变物理渠道字段 |
| `middleware/distributor.go` | 在既有选渠边界调用独立关系服务，设置有效归属及并发 |
| Controller/Relay 两个并发 guard | 获取租约改用快照 owner，其余生命周期复用 |
| `service/channel_budget_guard.go`、`channel_user_quota_usage.go` | 统一检查/记录使用 owner |
| `controller/channel_limit_fallback.go` | 预检读取有效父策略，目标继续重新检查 |
| 限制管理 Controller 与 `controller/channel.go` | 统一只读保护，读取使用有效归属 |
| `model/task.go`、`service/task_billing.go` | 保存/使用任务私有归属快照 |
| `relay/vision_assist.go` | 辅助调用归属上下文同步与恢复 |
| `relay/relay_task.go`、`relay/midjourney_origin_channel.go`、`relay/mjproxy_handler.go` | 原任务/历史操作切换物理渠道时重新解析归属，并传播失败关闭错误 |
| `controller/video_proxy_gemini.go` | Gemini/Vertex 结果 URL 实时查询在获取并发租约前解析父归属；存储失败保持 503，超限保持 429 |
| 用户限制 Dialog、渠道并发字段入口 | 挂载独立面板并传递只读状态 |
| 七语言 JSON | 仅增加必要键，遵循 i18n skill |

复用原并发租约、预算内核、资金结算、JSON 包装和权限系统；不抽取或重写上游 Relay 主循环，不修改 relaykit 的独立模块边界。实施中实际新增接入文件须补充本表及理由。

## 发布、回滚与同步复核

- 本任务交付代码与本地验证，不执行生产变更。
- 新关系表初始无绑定，不回填历史。所有 new-api 实例升级后，才创建关系，避免旧实例仍独立计数。
- 绑定/解除前排空相关在途上游请求，之后按既有缓存传播时限验证；后台异步任务仍使用提交快照。
- 回滚前解除关系、停止新流量并收口在途请求；含新私有快照的未结任务须在新代码处理完成后再退回旧代码，避免旧程序忽略快照而记错桶。新增表保留不影响旧程序，不删除原策略和计数。
- 上游同步复核：ChannelMeta 初始化/重试重置、视觉上下文清单、新资金结算入口、任务 PrivateData 序列化、渠道批删以及限制管理新端点；新增路径必须同步 owner 与只读保护。

## 验证与风险

重点验证共享原子并发、正向累计唯一性、物理渠道不串、历史任务、只读 API、CAS/删除竞争和 ai-fund 合同。多实例依赖 Redis；金额仍是软上限；运行中关系切换不自动搬迁旧租约。若三库专用测试环境缺失，必须明确标为部分验证，不虚报完整三库结果。
