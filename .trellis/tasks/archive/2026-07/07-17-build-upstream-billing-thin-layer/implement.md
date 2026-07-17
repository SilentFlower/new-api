# 上游模型计费 Build 薄层化实施计划

## 1. 固化治理前基线

- [x] 运行映射计费、价格、文本结算、任务日志和重试定向测试。
- [x] 运行相关包完整回归和定向 race。
- [x] 记录 `relay/common/relay_info.go` 与 `relay/responses_handler.go` 治理前差异。

验证：

```bash
go test ./relay/common ./relay/helper ./relay ./service ./controller -run 'BillingModel|MappedUpstreamModel|TaskBilling|TextQuotaSummary|RelayRetry' -count=1
go test -race ./relay/common ./relay/helper ./service -run 'BillingModel|MappedUpstreamModel|TaskBilling|TextQuotaSummary' -count=1
```

## 2. 迁移计费模型快照领域

- [x] 新建 `relay/common/billing_model.go`，原样迁移选择、解析、冻结、清理和读取方法。
- [x] 增加 `FrozenBillingModelName()`，只返回当前冻结值，不回退到动态解析。
- [x] 保留 `RelayInfo` 冻结字段的导出名称，仓内调用方禁止直接读写。
- [x] `relay_info.go` 清理 build 专用方法和 `ratio_setting` import，不移动其他结构或方法。

## 3. 收敛调用方与直接测试

- [x] `relay/responses_handler.go` 通过稳定方法保存和恢复冻结快照。
- [x] 搜索所有价格、结算、任务和日志路径，确认继续统一读取 `BillingModelName()`。
- [x] 新增直接表驱动测试覆盖开关、映射、Compact 后缀、冻结、上游模型变化和清理重算。
- [x] 保持旧任务 `BillingModelName` 缺失时回退 `OriginModelName` 的兼容逻辑。

## 4. 完整回归与安全检查

- [x] 运行相关包完整回归、定向 race 和 `go vet`。
- [x] 执行 `gofmt`、`git diff --check`、重复判断和直接字段访问扫描。
- [x] 复核表达式快照、预扣/结算、任务、退款、违规费和 quota saturation 链路未变化。
- [x] 对比治理前后上游热点文件，确认只保留兼容字段和稳定方法调用。

最终验证：

```bash
go test ./relay/common ./relay/helper ./relay ./service ./controller -count=1
go test -race ./relay/common ./relay/helper ./service -run 'BillingModel|MappedUpstreamModel|TaskBilling|TextQuotaSummary' -count=1
go vet ./relay/common ./relay/helper ./relay ./service ./controller
git diff --check
```

## 5. Review Gates

- [x] Gate A：治理前计费与重试基线通过。
- [x] Gate B：冻结字段保持兼容，所有仓内调用方只使用稳定接口。
- [x] Gate C：冻结、清理、Compact 临时恢复和历史任务回退行为不变。
- [x] Gate D：未改价格算术、表达式、quota conversion 或数据库契约。
