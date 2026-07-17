# 视觉辅助 Build 薄层化实施计划

## 1. 固化治理前基线

- [x] 运行视觉辅助 Service、Relay 和 Controller 错误日志定向测试。
- [x] 运行相关包完整回归和定向 race。
- [x] 记录 `controller/relay.go`、Claude/OpenAI handler 的治理前差异。

验证：

```bash
go test ./controller ./relay ./service -run 'VisionAssist|RelayErrorLog' -count=1
go test -race ./controller ./relay ./service -run 'VisionAssist|RelayErrorLog' -count=1
```

## 2. 迁移 Relay 错误日志实现

- [x] 新建 `controller/relay_error_log.go`，原样迁移 `recordRelayErrorLog` 与 `shouldRecordRelayErrorLog`。
- [x] 保持预处理失败和 `processChannelError` 的调用位置不变。
- [x] 清理 `controller/relay.go` 不再需要的 import，不移动其他 Relay 生命周期代码。
- [x] 保持视觉辅助失败在全局错误日志关闭时仍可记录，且不触发主渠道自动封禁。

## 3. 收敛准备状态边界

- [x] 在 `relay/vision_assist.go` 增加准备状态标记、读取和重置 helper。
- [x] `PrepareRequestForSelectedChannel` 和辅助渠道切换复用统一 helper。
- [x] `controller/relay.go` 每次重试只保留一次重置调用。
- [x] Claude/OpenAI handler 只通过稳定 helper 判断是否跳过重复初始化和模型映射。
- [x] 增加准备状态生命周期测试，不改变 context 快照和恢复字段。

## 4. 完整回归与差异检查

- [x] 运行视觉辅助跨层测试、Controller 错误日志测试和相关 Relay 回归。
- [x] 运行相关包 `go vet`、`gofmt` 和 `git diff --check`。
- [x] 对比治理前后热点文件差异，确认只剩可解释窄调用。
- [x] 逐项复核普通图片、Responses 工具输出图片、失败策略、错误日志和重试隔离。

最终验证：

```bash
go test ./controller ./relay ./service -count=1
go test -race ./controller ./relay ./service -run 'VisionAssist|RelayErrorLog' -count=1
go vet ./controller ./relay ./service
git diff --check
```

## 5. Review Gates

- [x] Gate A：治理前基线通过。
- [x] Gate B：错误日志迁移后写入门禁、字段和脱敏行为不变。
- [x] Gate C：准备状态收敛后模型映射、视觉改写和重试行为不变。
- [x] Gate D：上游热点文件只保留窄调用，无功能修复或无关重排。
