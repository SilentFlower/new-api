# Responses WebSocket 并发口径调研

调研日期：2026-11-16。用户要求联网验证，任务处于规划阶段。

## 官方一手资料

1. OpenAI Responses WebSocket 官方指南：
   https://developers.openai.com/api/docs/guides/websocket-mode
   - 连接用于多轮调用、工具往返与增量输入，持续连接属于协议的正常使用方式。
   - 服务端持有连接内的历史响应缓存，关闭连接会丢失这部分缓存；恢复可能需要完整输入上下文。
   - 当前指南还描述按 stream_id 分隔的并行请求；这进一步说明连接数量与执行请求数量是不同维度。
2. Codex 官方源码 client.rs（调研时 main）：
   https://github.com/openai/codex/blob/main/codex-rs/core/src/client.rs
   - ModelClientState 保存 cached_websocket_session。
   - new_session 从缓存取出 WebsocketSession。
   - ModelClientSession 的 Drop 把 WebsocketSession 放回缓存，供后续回合继续使用。
   - 上述源码确认官方客户端有意复用连接。
3. Codex 官方仓库问题报告 #58830（2026-11-02，调研时仍开放）：
   https://github.com/openai/codex/issues/58830
   - 提报者描述空闲连接遭中间网络设备静默丢弃，下一轮等待超时后才恢复。
   - 该报告是独立网络故障场景的用户报告。此次服务器排查尚未核实相同的网络故障特征。

## 与本项目的对应关系

- 本项目在连接建立时获取租约，直到整个代理连接退出才释放。
- 终止事件已经有统一分支：response.completed/done 成功结算；failed/incomplete/cancelled/canceled/response.error/error 执行既有失败处理。
- 每次终止后 currentTurn 归零，concurrencyGuard 继续存在并续租，直接导致空闲连接被统计为并发。
- 这是既有项目规范明确规定的按连接统计行为。将配置解释为正在执行的请求数量时，现有统计粒度与该目标不一致。

## 调研结论与建议

- 连接长期打开本身符合官方使用方式。仅凭连接持续存在，无法认定本地客户端存在连接泄漏。
- 以固定空闲时长主动断开连接作为主修复会破坏连接复用，并引入续接缓存恢复成本。用户已质疑该方案，规划中不将其作为已确认需求。
- 推荐方案（待用户确认）：每次 response.create 在预扣与上游发送前获取请求租约，回合终止或断开时幂等释放，空闲连接保持可复用；后续回合重新检查限流。
- WebSocket 连接资源保护与请求并发应分别定义；本任务优先修复请求并发口径，是否需要连接数量保护须独立确认。
- 当前项目仅支持每连接一个活动回合；官方新文档的多 stream 并发支持属于独立兼容性事项，本次调研不自动扩大实现范围。
- 线上客户端具体版本与本地进程仍待核实。以上源码证据用于确认官方设计允许连接复用。
