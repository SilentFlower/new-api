# 实施计划

## 实施顺序

1. 新增渠道配置字段
   - 在 `dto.ChannelSettings` 增加 `UseUpstreamModelForBilling`。
   - 保持 `omitempty`，历史配置默认为关闭。

2. 增加统一模型解析 helper
   - 在合适的后端包中增加 `ShouldUseUpstreamModelForBilling` 与 `BillingModelName`。
   - 所有计费、日志主模型路径通过 helper 获取实际计费用模型。
   - 保留 `OriginModelName` 的原始请求语义，不用覆盖字段来表达计费用模型。

3. 改造普通 relay 价格与日志
   - `relay/helper/price.go` 中所有价格、倍率、表达式读取改为计费模型。
   - `modelPriceHelperTiered` 快照和错误信息改为计费模型。
   - `service/text_quota.go` 中 `summary.ModelName` 改为计费模型。

4. 改造音频/实时路径
   - `service/quota.go` 中倍率读取、`QuotaInfo.ModelName`、日志主模型改为计费模型。
   - 保证 `GenerateWssOtherInfo` / `GenerateAudioOtherInfo` 的 other 中能看到计费用模型。

5. 改造异步任务路径
   - `ModelPriceHelperPerCall` 使用计费模型读取价格和倍率。
   - `LogTaskConsumption` 日志主模型使用计费模型。
   - 特殊 `TaskPricePatches` 判断按计费模型执行。
   - 保持请求上游模型仍由 `ModelMappedHelper` 决定。

6. 补充日志追溯字段
   - 发生映射时在 other 中写入 `origin_model_name`、`upstream_model_name`、`billing_model_name`。
   - 文本、音频/实时、任务日志都要一致。

7. 更新 `web/classic` 渠道编辑 UI
   - state、读取、保存、临时字段清理、UI 开关同步增加。

8. 更新 `web/default` 渠道编辑 UI
   - 类型定义、表单 schema/default、setting JSON 构建、drawer UI 开关同步增加。

9. 补测试
   - 后端至少覆盖普通 relay 的开关关闭/开启计费模型选择。
   - 覆盖任务类 `ModelPriceHelperPerCall` 或日志模型选择，防止遗漏异步路径。
   - 如改动前端表单纯配置拼装逻辑，可补 TypeScript 静态检查或现有构建检查。

## 验证命令

后端：

```bash
go test ./relay/helper ./service
```

如涉及更多包或测试依赖扩大：

```bash
go test ./...
```

默认前端：

```bash
cd web/default && bun run build
```

经典前端如有可用脚本，优先运行对应 build/lint；否则至少做目标文件静态检查。

## 风险文件

- `relay/helper/price.go`
- `service/text_quota.go`
- `service/quota.go`
- `service/log_info_generate.go`
- `service/task_billing.go`
- `relay/relay_task.go`
- `dto/channel_settings.go`
- `web/classic/src/components/table/channels/modals/EditChannelModal.jsx`
- `web/default/src/features/channels/types.ts`
- `web/default/src/features/channels/lib/channel-form.ts`
- `web/default/src/features/channels/components/drawers/channel-mutate-drawer.tsx`

## 回归检查重点

- 开关关闭时，模型映射请求仍按原始模型计费和记录日志主模型。
- 开关开启时，只有实际发生映射才按上游模型计费。
- 预扣费模型、实际结算模型、日志主模型、日志 other 中 `billing_model_name` 一致。
- 分层表达式计费快照中的模型名与表达式来源一致。
- 任务类请求不会因为恢复 `OriginModelName` 而绕过开关。
- 两套前端保存后不会丢失已有 `setting` JSON 中的其他字段。
