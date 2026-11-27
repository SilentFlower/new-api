# 升级目标与兼容性评审

## 目标

在当前 `build-main` 合入 **`81c1fd20e`，即官方 `v1.0.0-rc.51` 后第 34 个提交**，保留 `build-bak@accb26db3` 已交付的功能。

## 范围

处理 153 个冲突，复核自动合并的代码，覆盖协议转发、插件、长连接、预算与计费、工作流账号、审计、报表和七语言前端。本轮交付本地候选版本及验证结果，生产切流另行安排。

## 合并方式

使用新版路由和插件入口，接回本地 Claude 上游计数、Compact 透传、WebSocket 逐轮并发与结算。普通路径和异常路径使用同一批输入对照新旧账单。

## 跨版本决定与风险

已逐版核对 `rc.25–rc.51` 的 27 份官方发布说明。现场启用项、旧库列类型和实际价格仍待核实。[完整迁移与回滚清单](sandbox:/mnt/data/new-api-upstream-upgrade-notices.md)

- **启动与定价。** 旧 MySQL/PostgreSQL 的四个钱包额度列可能阻断启动。从本项目的 `rc.24` 基线跨到插件版，已启用的视频模型需逐个重配价格，并核对实际扣费。[rc.26](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.26) · [rc.27](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.27)
- **登录与调用方。** 启用 Telegram 登录的站点需重配 OAuth 回调和凭据。旧面板 PAT 仅有 30 天过渡期，ai-fund 和运维脚本需在到期前验证新凭据与作用域。[rc.34](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.34) · [rc.41](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.41)
- **多节点升级。** Redis 缓存签名和同步规则已变化。原先的单节点生产灰度建议已修正为隔离环境演练，生产共享 Redis 的节点同版切换。[rc.48](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.48) · [rc.49](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.49)
- **过滤与数据保留。** 敏感词迁成请求过滤插件，命中改为 HTTP 400，回退旧程序要恢复旧规则。另需提前告知游乐场旧聊天的一次性清理，核对已结束系统任务默认 30 天的保留期。[rc.44](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.44) · [rc.51](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.51)

## 验收与下一步

候选版本须通过构建、历史数据迁移与回退、凭据轮换、预算并发、断流退款和跨用户隔离验证。开工后先保护当前目录已有改动，再发起待提交合并。
