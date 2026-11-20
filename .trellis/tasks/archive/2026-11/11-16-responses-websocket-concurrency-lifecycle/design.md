# 技术设计

## 边界与复用

复用 service 的原子租约存储、controller 的 acquire/finish guard、service.ApplyChannelLimitContext、既有终态 observer 与 BillingSession。服务层 key、TTL、续租周期、管理 API、数据库和前端保持原结构。

Responses WebSocket 原本是项目独立定制入口，本任务在该入口的首轮连接、后续回合和终态边界调整最少接入点；新增测试放在独立文件。原有 HTTP Relay 与 Realtime 生命周期保持。

## 状态与数据流

1. 首轮：沿用已选渠上下文，在准备（包含预扣）和 dial/send 前获取请求租约；连接成功后转交代理循环。
2. 空闲态：currentTurn 与当前 guard 均为空；连接、reader 与续接缓存继续存在，LostSignal 分支使用空通道。
3. 新 response.create：解析及鉴权，核对当前渠道资格。可复用时通过 model.CacheGetChannel 读取当前渠道的限制配置，再调用 service.ApplyChannelLimitContext 刷新父桶及个人覆盖，然后获取请求租约，再调用既有 prepare 与上游写入。仅刷新限制快照，避免重选渠道 Key 导致上下文与已有上游握手认证不一致。
4. 生成态：当前回合持有一个 guard；成功发送后将 Gin context 恢复到连接的稳定 baseContext，租约丢失仍由 LostSignal 分支终止连接。
5. 终止态：普通和 Compact observer 完成既有结算或退款后，调用 finish 释放当前 guard，并清空回合与 guard 的引用。终态转发、计费和释放失败的响应语义沿用现有逻辑。
6. 任意异常退出：代理 defer 释放当前 guard；幂等释放保护避免重复扣减。续租丢失优先保留本地 503、错误日志及中止行为。
7. 资格变化或实际重试：先释放旧请求租约，再为新尝试选渠并获取；不能同时占用共享父桶两个名额。增量跨渠恢复沿用 previous_response_not_found 与完整重放规则。

## 关键约束

- 所有 prepare 分支都可能建立预扣，后续回合也必须先获取租约。
- 当前回合冻结限制归属；关联或覆盖变化仅影响下一回合。
- 后续本地拒绝继续沿用 writeResponsesWebSocketError 的现有封套及关闭行为。
- 同连接一次只允许一个活动回合。官方新文档的 stream_id 多路复用属于独立协议能力。
- 终止帧写入与 Redis 释放之间存在有限的调度间隔，测试使用同步屏障或有界状态等待观察完成，不以固定休眠推断状态。

## 预计文件与最薄接入

- 新增 controller/responses_websocket_concurrency_test.go：请求级名额、空闲复用、真实上游前拒绝和异常释放回归。
- 修改 controller/responses_websocket.go：后续回合获取与限制刷新、终态释放、异常路径 guard 所有权与稳定 context。
- 按需修改 controller/responses_websocket_recovery.go：让重连注释和请求级 guard 语义一致，保持恢复协议。
- 修改 controller/responses_websocket_test.go：替换旧连接级断言，保留普通/Compact 多轮账务与帧回归；按需同步 recovery/Compact 既有回归。
- 更新 .trellis/spec/backend/channel-user-concurrency.md 和 relay-alpha-search-compact.md 中 Responses WebSocket 生命周期描述及示例。

## 兼容与回滚

无迁移及新配置。管理端沿用同一计数 API，其 Responses WebSocket 数值变为活动请求数。发布后既有连接由旧进程按旧语义处理至退出；新进程使用新语义。回滚精确恢复本次 controller 与对应测试/规范改动，Redis 租约格式兼容。

## 风险

重点验证预扣前获取、失败重试、重复释放、父关联轮间变化、终态与续租丢失竞态，以及 Gin context 在连接和请求生命周期之间的恢复。真实网络故障与客户端版本诊断可独立继续，不作为本次修复已核实的根因。
