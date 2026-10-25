# 实施计划：父渠道限制继承

## 当前阶段与开工条件

- 当前任务为 in_progress，用户已确认最终完整 Brief，task.py start 已完成。
- 已通过 trellis-route(target=implement) 选择 inline，当前主会话负责实现与聚焦验证。
- 首轮 full Check-All 的 CHK-001 和 FBK-001 已按用户“修复全部”完成；定向测试、race/vet、三库及 Go 全仓通过，接续 Check-All 重检。
- 写代码前加载 trellis-before-dev；前端实施另加载 shadcn-ui、vercel-react-best-practices，任何文案/语言键修改前加载 i18n-translate。
- 任务创建记录的脏基线属于已有工作，保留归档任务变更与缓存文件，不清理、不提交到本功能。

## 有序实施

1. [x] 新增关系模型、DTO、安全摘要和事务/CAS；普通与快速迁移注册，补三库行为与删除竞争测试。按 design.md 新增职责表独立实现，不为复用重构旧渠道 CRUD。
2. [x] 新增关系服务、短缓存、归属解析和权威只读保护；无关系保持原行为，坏关系失败关闭，缓存版本不倒退。
3. [x] 接入选渠上下文、ChannelMeta 快照、两个并发 guard；A/B 合计租约，普通失败重试释放后重新取 owner，保留取消和请求级错误去重。
4. [x] 预算检查、金额预检和正向累计使用同一 owner；父计数沿用原 key，不迁历史，不改变账单物理渠道和财务算法。
5. [x] 新任务 PrivateData 冻结 owner；旧任务缺字段回落原 ChannelId。覆盖异步正差额、退款、Midjourney 历史操作、视觉辅助、Realtime/Responses WebSocket，修补实际遗漏的上下文接入点。
6. [x] 新关系管理 API、精简候选与审计；读接口使用有效限制但保留路径身份，B 限制写入口统一拒绝，A 原 API 保持合同。
7. [x] 独立父渠道面板嵌入用户限制弹窗；继承项只读、查看来源、进入 A 与解除关联。并发编辑入口同步只读，七语言、迟到响应和脏草稿行为一起完成。
8. [x] 用模拟上游做完整 Relay 管理/调用/结算回归，用真实父接口响应验证 ai-fund 的当前解析器和请求合同；不改 ai-fund 源码。
9. [x] 更新本计划实际文件范围、运行聚焦验证；默认首轮实现完成后进入 trellis-route(target=check) 与 trellis-check-all，按当前报告处理发现，不跳过必需的质量流程。

## 原文件接入与冲突控制

完整清单见 design.md 的“新增职责与旧文件薄接入”。重点冲突面是渠道 CRUD/删除、ChannelMeta、主 Relay 周边、任务结算和限制弹窗。每个旧文件只调用独立模块或传递 owner/只读状态，不重命名、移动、整体格式化或清理无关代码。若必须新增接入点，先在设计表说明其必要性，再实施。

## 聚焦验证矩阵

| 领域 | 必须保护的行为 |
| --- | --- |
| 关系与数据库 | 自身/循环/多级拒绝、CAS、并发绑定/删除、三库迁移重入、解除保留版本 |
| 并发 | 同用户 A2+B2 第五个 429、另一用户可用、跨令牌共享、释放/取消/失租、Redis/内存一致 |
| 重试/降级 | 普通 RetryTimes 不绕过、关联目标零上游、错误不禁用/不扣费、目标仍检查共享预算 |
| 金额与日志 | A3+B旧2 关联后仍 A3，B新1 后 A4；唯一正向累计，物理日志 B；免费/零/退款不新增 |
| 快照兼容 | 在途关系变更不搬桶，新任务有快照、旧任务缺字段、异步正差额沿用提交归属 |
| 管理写保护 | B 的并发/预算/时段/提额/用量/撤销拒绝，A 的 API 形状与 identity/revision 不变 |
| 前端 | 来源和同名 ID、继承只读、父配置显示、解除恢复、无权限、错误/冲突、迟到响应与脏草稿 |
| ai-fund | 当前请求与解析器处理父渠道真实响应，无需新增字段或修改源码，严格 ID/分页/白名单兼容 |

## 验证命令

以下为验证入口；实际执行的最终命令、退出状态及源码摘要统一保存在 `research/verification-evidence.json`。聚焦名称只统计实际匹配的测试。

```bash
go test ./model ./service ./controller ./middleware ./router ./relay -run 'TestChannelLimitParent|TestChannelInherited|TestChannelParent|TestChannelUserConcurrency|TestChannelLimitFallback|TestChannelPeriod|TestChannelBudget' -count=1
go test -race ./service ./controller -run 'TestChannelLimitParent|TestChannelInherited|TestChannelParent' -count=1
go test ./model ./service ./controller ./middleware ./router ./relay
```

三库用独立测试 DSN，沿用现有数据库合同测试的隔离方式，绝不连接生产 DSN；缺少 MySQL/PostgreSQL 测试依赖时保留该项未完成，SQLite 通过不能宣称三库已验。

```bash
cd web
bun test src/features/channels/components/dialogs/__tests__/channel-user-limits-dialog.test.tsx src/features/channels/components/dialogs/__tests__/channel-period-policy-panel.test.tsx
bun test src/features/channels/components/dialogs/__tests__/channel-limit-parent-panel.test.tsx
bun run typecheck
bun run lint
bun run i18n:sync
bun run build
```

前端还需按 i18n-translate 执行源码缺键与七语言值检查；i18n:sync 本身不能代替源码扫描。新增文件及改动区块格式检查遵循保护版权头脚本；不为格式检查改动全仓文件。若实际修改触及 relaykit 或其公开 API，必须额外执行 `cd relaykit && GOWORK=off go build ./...`。

ai-fund 验证以当前 worker 的 newapi_client/pool_limits/pool_period_limits 既有测试和本任务保存的真实响应合同为依据。测试入口先读取 ai-fund 的 package 脚本/测试文件再确定，不假定不存在的脚本。其运行日志/临时夹具存入本任务 research，源码保持不变。

## 审查与回滚点

- 数据层完成后审查关系不会绕过父删除保护、SQLite 不发 FOR UPDATE、原 Channel/策略数据未被迁移覆盖。
- Relay 接入后审查物理 ID 与 owner 完整分流，所有预扣前检查、结算和异步入口均闭合；不得以编译通过替代归属行为验证。
- 前端完成后验证实际只读写保护和 A 接口兼容，不只断言按钮禁用。
- 发布前必须全部实例升级后再绑定，关联变更先排空在途上游请求；回滚先解除/收口并处理新快照任务，再撤销薄接入与新增领域实现，旧配置不删。
- Check-All 通过后进入 trellis-update-spec；Git 交付由 trellis-push 展示精确范围并取得届时授权，本次规划不包含提交、推送或生产发布。

## 本轮实施结果

- 关系持久化/CAS、权威只读、物理渠道与限制归属分离、预算累计及任务快照已完成。
- 额外接通历史任务切渠、Midjourney 原渠道、视觉辅助上下文；实际 WebSocket 三回合验证中途解绑仍持有父租约，关闭释放。
- 旧缓存不能把解绑后的 B 写入导向 A；迟到的旧查询不能回退本节点已发布的新关联。
- 真实 Relay 验证 A2+B2 的第五个请求为 429、零上游/扣费，B 成功请求仍用 B 密钥且唯一消费日志为 B；共享金额降级不能绕过父预算。
- SQLite/MySQL 8/PostgreSQL 16 验证迁移重入、拓扑竞争、CAS、解除显式零值及绑定与父删除竞争；临时数据库已清理。
- 最终通过：Go 全仓、相关 race/vet、渠道前端 68 个测试、typecheck/lint/build、七语言 18 个新增键、relaykit 独立 build/vet、ai-fund 73 个现有测试及 12 个真实父接口读写合同。
- 保留既有软上限和缓存边界；关联变更前排空在途，保存后至少等待 5 秒再恢复流量。ai-fund 继续映射父渠道 A，源码未修改。
- 修复已完成：CHK-001 接通两个视频查询边界，父桶满时零上游、坏关系 503，释放后 Gemini 查询成功且无名额泄漏；FBK-001 在独立状态清理模块内重新筛选和锁定候选，三库验证重新启用渠道及其能力保留。新增定向回归、race/vet 和 Go 全仓均通过，21 项有效验证已更新，继续重检；尚未提交、推送或部署。
