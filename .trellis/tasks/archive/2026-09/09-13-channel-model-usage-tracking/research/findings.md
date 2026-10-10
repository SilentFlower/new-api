# 规划依据

## 当前基线

- new-api：`build-bak`，`ad8e96d4d21f05179298325f5ffb015b9432ba1a`，业务文件无未提交修改，旧任务目录 clean，upstream 无 ahead。
- ai-fund：`master`，`5e88b88a4fe564c0c0d63d12aa4ef6ac3c03bde9`；旧任务 `task.json` 及两处 pycache 为保留变更，不属于本次实现。
- 旧 completed-task preflight 结果为普通已同步，无需重复 Push。新需求已创建独立子任务。

## 已核对的实现事实

1. `recordChannelBudgetUsage` 追加的 `channelBudgetBaseRows()` 只包含不限定模型的 daily/weekly；当前一个 Lua 预检全部 key 的类型与 int64 溢出后原子递增。
2. `identityKey()` 对 daily/weekly 使用窗口及排序去重模型集合，不包含行 ID、scope 或 schedule_id；occurrence 才包含时段 ID。不能按行 ID另建调整规则而破坏既有共享语义。
3. `channelBudgetResolution.counting` 让停用行继续累计，日周行即使绑定时段也按整个自然周期累计；仅 occurrence 受当前时段活跃状态约束。
4. 模型计数写用户字段及独立 `__pool`；`setChannelBudgetUsage` 分别设置用户字段/池子字段，所以旧模型组合计数可能已经包含人工修正，不能作为可分拆的原始模型事实。
5. `bindChannelPeriodJSON` 通过请求 JSON 与 DTO 重新序列化结果的递归键数、类型精确比较识别未知字段。新增可选字段必须处理 omitempty、缺省、显式 false 和 null，不能直接增加必序列化字段破坏旧请求。
6. `NormalizeChannelBudgetConfig` 与 `validateChannelBudgetStoredPolicy` 对存储形状做稳定归一化和 DeepEqual；新增服务端元数据必须保持旧配置可读、重复归一化不变。
7. `SaveChannelPeriodPolicy` 使用旧 revision CAS，成功后发布 5 秒策略缓存；后续缓存失败返回 committed=true。开关应跟随该现有事务与缓存规则，不另存一份权威配置。
8. new-api 前端在 `period-types.ts` 使用 Zod 白名单；表单在 `channel-period-policy-panel.tsx`，已有 Switch 组件。ai-fund 使用 `PoolPeriodPolicyEditor.vue`，外层 `PoolLimitAdminModal.vue` 管理号池/渠道身份与草稿。
9. Worker 的 `normalizePeriodPolicyConfig` 显式构造配置和预算行；新增开关及服务端统计来源元数据需要完整保留。`pool_period_limits.js` 继续承担两次渠道映射核对、换算和审计。

## 方案取舍

- 不把“当前预算计数”直接当成“模型原始消费”：旧计数受人工调整影响，A+B 也不能反推 A 和 B。
- 新增独立单模型日周事实计数；旧预算继续读旧计数，新创建且采用持续统计的预算读取单模型事实聚合。计数来源由服务端持久化，不能随开关当前值来回切换。
- 人工调整保存选定身份的目标值及对应原始计数快照；之后用原始增量推进目标值，避免改写其他预算所共用的事实。
- 已配置预算需要的事实累计不受额外预累计开关关闭影响；日周自然过期统一，关闭期间的覆盖状态保守标明不完整。
- 不在本轮进行生产压测或历史回填。性能对比只使用隔离的本地 Redis 与明确的确定性数据规模。
