# Brief — 视觉辅助 Build 薄层化

## Goal

- 收敛视觉辅助的 Relay 生命周期准备状态和专用错误日志，使领域实现集中、上游热点文件只保留行为不变的窄调用。

## Scope

- 将 Relay 错误日志门禁与写入实现迁到 `controller/relay_error_log.go`。
- 将渠道请求准备状态的标记、读取和重置统一收进 `relay/vision_assist.go`。
- 调整 `controller/relay.go`、`relay/compatible_handler.go`、`relay/claude_handler.go` 只调用稳定入口。
- 增加准备状态生命周期测试并运行视觉辅助、日志和 Relay 回归。

## Non-Goals

- 不新增视觉能力，不调整提示词、Provider、图片抽取、缓存、并发、重试或失败策略。
- 不修改 DTO、数据库、配置和渠道设置前端。
- 不修复既有视觉辅助或上游协议缺陷。

## Key Context

- `PrepareRequestForSelectedChannel` 必须继续在主请求计费前完成渠道元信息、模型映射和视觉辅助改写。
- 每次主渠道重试必须从未准备状态开始；辅助渠道嵌套请求必须快照并恢复主请求上下文。
- 视觉辅助预处理失败需要记录专用错误日志，但不能触发主渠道自动封禁。
- 所有迁移保持同 package，避免新增包装层、跨包依赖或导出业务实现。

## Acceptance

- `controller/relay.go` 不再承载 Relay 错误日志函数体，只保留既有调用。
- Claude/OpenAI handler 不直接读取视觉准备 context key，只通过稳定 helper 判断。
- 普通图片、Responses 工具输出图片、模型映射、计费、重试、失败策略和日志行为保持不变。
- 视觉辅助跨层测试、Controller/Relay/Service 回归、定向 race 和 `go vet` 通过。

## Next Step

- 激活任务后先运行治理前基线，再按错误日志迁移、准备状态收敛和完整回归的顺序实施。
