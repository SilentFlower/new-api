# 实施计划

## 顺序

- [x] 排查线上计数、释放路径与官方 WebSocket 复用设计，取得请求级统计口径决定。
- [x] 收敛 PRD、设计与本计划，派生完整 Brief。
- [x] 完成首次 Brief 确认并激活任务，进入 trellis-route(target=implement)。
- [x] 加载 trellis-before-dev 与任务上下文，核对相关 DTO、guard、父关联与账务方法定义。
- [x] 在独立 controller/responses_websocket_concurrency_test.go 建立请求级行为回归。
- [x] 修改 controller/responses_websocket.go 的后续请求获取、终态释放及异常清理边界；需要时以最小改动同步 recovery 注释与相关测试。
- [x] 更新原多轮测试中的连接级预期，保留普通/Compact、原始帧、usage、亲和性与恢复协议覆盖。
- [x] 执行定向验证、Pre-Check，再进入 trellis-route(target=check) 及 Check-All。
- [x] 进入 trellis-update-spec 同步请求级并发规范，然后按 trellis-push 精确展示本任务的 Git 计划。

## 验证计划

- go test ./controller -run 'ResponsesWebSocket|ProxyResponsesWebSocket|ChannelUserConcurrency' -count=1
- go test ./service -run 'ChannelUserConcurrency|ChannelLimitParent|ChannelUserLimitOverride' -count=1
- go test -race ./controller -run 'ResponsesWebSocket|ProxyResponsesWebSocket' -count=1
- go test ./controller ./service
- git diff --check

按构建环境和执行证据报告真实结果。仅当新增失败或未决关注点需要时追加验证。若实际改动触及 relaykit，则独立运行 cd relaykit && GOWORK=off go build ./...。

## 验收覆盖

新测试直接验证活动名额、终态后空闲复用、达到上限后的上游前拒绝、父桶/个人覆盖刷新、失败和断开的清理；既有回归保护普通/Compact 多轮消费与重连协议。使用 require/assert、可控上游和同步屏障，避免随机压力循环、固定休眠和只断言内部调用次数。

## 文件职责与回滚点

独立新增测试承载请求级业务契约。生产接入集中在已有定制 WebSocket 入口；复用现有租约及账务接口，避免重构公共 Relay。规范改动集中在两份既有领域文档。恢复这组精确文件即可回滚，工作区既有变更及 Flower 同步单独保留。

## 评审关注点

每次 prepare 之前均持有当前请求 guard；空闲阶段没有续租；所有终态及异常出口幂等释放；更换渠道前释放旧名额；context 恢复后 LostSignal 仍有效；不将并发拒绝导入渠道禁用或重试；账务每轮一次。

## 已执行验证

- 红灯复现：新增普通/Compact 用例在原实现中均观察到终态后仍占 1 个名额，预期 0。
- go test ./controller -run 'ResponsesWebSocket|ProxyResponsesWebSocket|ChannelUserConcurrency' -count=1：通过（4.148s）。
- go test ./service -run 'ChannelUserConcurrency|ChannelLimitParent|ChannelUserLimitOverride' -count=1：通过（0.224s）。
- go test ./controller ./service：通过（controller 8.819s、service 15.067s）。
- go test -race ./controller -run 'ResponsesWebSocket|ProxyResponsesWebSocket' -count=1：通过（8.484s）。
- go vet ./controller ./service：通过。
- git diff --check：通过。
