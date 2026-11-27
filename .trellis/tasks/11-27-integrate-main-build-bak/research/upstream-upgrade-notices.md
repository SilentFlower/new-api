# 从 rc.24 定制版升级到 rc.51 后主线的兼容清单

2026-11-27 从官方 GitHub Releases API 核对了 rc.25 至 rc.51 的全部 27 份发布说明，并单独检查了 rc.51 之后至目标的 34 个提交。目标是 `81c1fd20efade39a5d5692c8018dfef4cfb30e1d`。下表合并重复公告。各项列出触发条件，部署侧实际启用的模型、架构、登录方式、节点和定价以发布前盘点为准。

## 可能导致启动失败、错误计费或访问中断

| 影响条件 | 官方变更与本地影响 | 演练及通过条件 | 官方说明 |
|---|---|---|---|
| 32 位平台或旧 MySQL/PostgreSQL 主库 | rc.26 要求 64 位运行环境。目标在主库迁移前检查 `users.quota`、`used_quota`、`aff_quota`、`aff_history` 的列宽。旧 32 位钱包列可阻断启动。SQLite 走自己的整数处理路径。 | 核查部署架构和四列类型。在数据副本使用目标镜像启动并再次重启，核对大额钱包、单请求饱和边界和行数。若需改列，记录具体表、迁移耗时、备份与回退。 | [rc.26](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.26) |
| 已有 MySQL/PostgreSQL 约束 | rc.28、rc.29 均曾导致部分旧库重启失败。后续修了 PostgreSQL 的预设分组名及令牌 Key 历史唯一约束。 | 直接演练旧库到目标的一次升级。保留重复/软删除边界用例，核对唯一性、原 Key 可用性、第二次启动的 DDL。 | [rc.29](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.29)、[rc.30](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.30) |
| 已启用视频模型 | rc.27 起旧任务适配器改为插件。官方要求从 rc.26 或更早升级的实例重新配置所有视频模型价格。rc.35 改变插件关闭后的回退行为。rc.47 允许普通渠道按优先级和权重与插件竞争；同名模型的两种路径分别需要适用的价格。 | 导出旧价格、重定向、已启用插件与实际模型清单。为每个在用视频模型建立新旧价格对照，标出已完成或需配置的项。用同一输入验证插件与普通渠道、成功与失败、切渠重试时的预扣及实扣。切流前清零实际在用模型的待处理价格。 | [rc.27](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.27)、[rc.35](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.35)、[rc.47](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.47)、[rc.48](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.48) |
| 历史任务、媒体和回调调用方 | 轮询失败分类与退费变化，连续失败阈值默认 20。视频超时退款默认从 24 小时延长到 72 小时，显式配置继续生效。部分厂商原生插件拒绝带 `callback_url` 的请求。Vertex 视频的区域选择也有变化。 | 验证升级前创建的任务、新任务、轮询、取消、退款只执行一次。盘点可灵、Vidu、MiniMax、火山方舟回调调用方以及每个 Vertex 模型的可用区，校对模型特定的区域规则与历史媒体下载。 | [rc.31](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.31)、[rc.47](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.47)、[rc.48](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.48)、[rc.51](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.51) |
| Telegram 登录、Passkey、多因素验证 | Telegram 迁到统一 OAuth。需登记 `/oauth/telegram` 回调并配置 Client ID、Secret 后启用。敏感操作引入绑定会话的一次性验证。移除通行密钥所属域名会影响其使用。 | 对启用的登录方式验旧用户绑定、登录、敏感操作二次验证与域名。确认管理员登录和恢复路径后再切换。 | [rc.34](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.34)、[rc.37](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.37)、[rc.41](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.41) |
| ai-fund、脚本或运维使用旧面板 PAT | rc.41 为旧面板访问令牌引入 30 天过渡期，首启写入 `LegacyAccessTokenRetireAt`，后续重启沿用该值。每用户最多 20 个具名 PAT，新建默认有效期 30 天。模型调用 Key 属于另一套机制。 | 盘点面板 PAT 使用方、所需作用域和轮换责任人。用真实类型的测试凭据覆盖账务摘要与工作流管理，分别验证过渡期内、到期和换新后的结果。 | [rc.41](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.41) |
| 多节点共用 Redis | rc.48 启用用户、令牌、会话缓存签名。旧缓存会回源数据库。rc.49 修复 Redis Cluster 键槽兼容，并要求多节点一起升级；混跑会影响用户和令牌变更的传播。 | 用隔离 Redis 和两节点演练，预先核对各节点密钥配置、冷缓存回源压力，验证禁用令牌、切换归属和预算余额的一致性。生产切换采用节点同版，蓝绿验证分别使用隔离数据环境。 | [rc.48](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.48)、[rc.49](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.49) |
| 既有敏感词策略及其回滚 | rc.45 提醒检查旧开关。rc.50 起迁移把词表转为请求过滤插件并删除旧选项。目标代码根据两个旧开关决定迁入实例是否生效。命中改为 HTTP 400 / `request_rejected`。直接回退旧程序后需要重建旧配置。 | 在副本记录旧词表与开关，验证迁移后的实例状态、实际拦截及客户端错误分类。用旧程序和已保存配置演练恢复。 | [rc.45](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.45)、[rc.50](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.50)、[rc.51](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.51) |

## 协议、数据保留和运行时行为

- **模型别名与计数端点。** OpenRouter 对通用旧 `-thinking` 后缀的处理改变。盘点公开模型别名，必要时用映射保留名称，分别核对选渠、模型限制和计费变体。rc.32 暂停了主线 Claude 计数路由。集成版须继续提供已部署的渠道透传计数能力。[rc.32](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.32)、[rc.33](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.33)
- **旧定价、分词、断流。** 旧定价转换到表达式应先预览再保存。已保存 MiniMax-H3 表达式若要计入媒体，需显式包含相应用量项。rc.38 调整 Responses 中断时的估算结算，和本地“客户端先断开且仅有本地估算才结算为零”有直接交集。分别重放真实 usage、缺失 usage、上游明确失败、客户端断开；另测 TokenKit 长输入与上游报告的搜索次数，防止重复扣费。[rc.31](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.31)、[rc.37](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.37)、[rc.38](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.38)、[rc.42](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.42)
- **用户可见的数据。** rc.44 将普通用户日志及看板的查询窗口默认设为 365 天，并在游乐场改为按账号存储时清空一次旧聊天记录。升级前通知受影响用户。核对用户端限制、管理员报表与公开 Key 的隔离。rc.50 引入已结束系统任务的定期清理，默认保留 30 天；发布前明确运维留存需求。[rc.44](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.44)、[rc.50](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.50)
- **SQLite 备份。** 使用纯文件路径的 `SQLITE_PATH` 会开启 WAL。使用 SQLite 一致性备份，或停写后完整保存数据库及相关 WAL、SHM 文件，再实际打开副本验证最近写入。[rc.28](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.28)、[rc.49](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.49)
- **环境与灰度。** 上游响应头等待默认 1,800 秒，请与本地流式空闲超时分别配置。默认界面与服务语言按实际站点要求核对。Go 1.26.10 调整了部分证书链的信任，应从候选环境验证真实类型的上游 TLS 链。rc.51 修复旧 x86-64 CPU 上 rc.50 的特定崩溃，覆盖相关节点时要用实际聊天请求验收。rc.50 起自动余额更新只在主节点执行。[rc.28](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.28)、[rc.43](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.43)、[rc.46](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.46)、[rc.51](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.51)
- **图片、计费与零用量。** 百炼万相、豆包 Seedream 等模型逐步转入图片任务插件。按最终发给上游的图片数量、任务实际时长和分辨率核对预扣。另测缓存读写、上游明确零用量、切渠后补预扣。[rc.39](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.39)、[rc.41](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.41)、[rc.45](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.45)、[rc.46](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.46)、[rc.50](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.50)
- **较早或重复发布说明。** rc.25 的字段透传与缓存结算纳入旧新请求对照。rc.28–38 多次重申视频定价。rc.36 的站点币种输入、rc.40 的上游 201/202、工具结果图片均在相关场景验收。[rc.25](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.25)、[rc.36](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.36)、[rc.40](https://github.com/QuantumNous/new-api/releases/tag/v1.0.0-rc.40)

## 本次集成确定的账务和渠道行为

- **阶梯文本预扣采用主线规则。** 默认按预计输入费用的 1 倍预留。对 1,000 个输入 token、缺省输出上限的指定测试费率，预留从 62,940 变为 1,500 额度。余额门槛随之降低。最终按真实用量结算。[主线计费改动](https://github.com/QuantumNous/new-api/commit/9978ee1e2)
- **普通 Responses WebSocket 保留失败退款。** 上游确认受理后返回失败、取消、断连或缺失有效用量，退回本轮预扣。正常完成仍按配置价格结算。HTTP SSE 有独立的终态与计费规则。
- **已有 WebSocket 渠道须核对开关。** 对照升级前实际承接 Responses WebSocket 的渠道，显式启用 `responses_websocket_enabled`。使用 Compact 的渠道还需保留 `responses_compact_passthrough_enabled`。对同一模型实测普通、Compact、普通三轮及 HTTP 入口。

## rc.51 之后的 34 个提交

单独核对了 `c9c6da278..81c1fd20e`。重点覆盖插件作用域的启用与匹配、任务进度达到 100% 但状态仍非终态、私有渠道媒体下载、已删除用户的 Key 返回 401、缓存用量在 Gemini 回复中的转换，以及启用易支付 RSA 时回调商户身份校验。进入验收时以固定目标提交的实现复核发布说明与后续修复的交集。[官方提交对比](https://github.com/QuantumNous/new-api/compare/v1.0.0-rc.51...81c1fd20efade39a5d5692c8018dfef4cfb30e1d)

## 现场证据

发布前登记实际平台与数据库类型、在用视频模型及价格、面板 PAT 调用方、Telegram/Passkey 配置、Redis 拓扑和节点数、过滤开关及系统任务保留期。记录脱敏结果及配置版本。验证真实生产配置、写库、切换流量和部署均由发布环节另行执行。
