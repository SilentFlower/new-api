# 实施与验证计划

## 1. 进入实现

- [x] 展示最新完整 Brief，经用户确认后启动任务并按 trellis-route 选择执行方式。
- [x] 读取 backend 质量、目录、日志与计费模型规范，以及 JSONL 指向的实际定义；核对 `build-bak` 基线并建立独立业务分支。

## 2. 补齐日志元数据

- [x] 独立 helper 和测试承载新逻辑；四个 handler、Chat 经 Responses 桥接和尝试初始化仅保留最薄接入，避免重构上游文件。
- [x] 实现高级渠道的明确强度同步，按最终协议取值，保持旧 Anthropic helper 的隔离合同。
- [x] 在转换/覆盖后的请求准备阶段接入；透传复用已解析参数，同步/流式一致，每次尝试清理旧值。
- [x] 复用 GenerateTextOtherInfo 和现有 UI 字段；核对未修改请求正文、身份、路由或计费。

## 3. 验证

- [x] 表驱动覆盖 Chat/Responses/Claude/Gemini 字符串、缺失、空值、非字符串及不支持协议。
- [x] 高级渠道实际转换、本机 HTTP、真实参数覆盖 low→high/删除、请求透传和跨渠道重试回归。
- [x] 定向 `go test ./relay ./relay/channel/advancedcustom ./relay/channel/openai ./relay/channel/deepseek ./service -count=1`；先核对实际测试名称再选择聚焦 race 测试。
- [x] 按改动影响运行 `go test ./relay/... -count=1`、`go vet ./relay/... ./service`、`git diff --check`；若改变 relaykit API，另执行 `cd relaykit && GOWORK=off go build ./...`。
- [x] trellis-check-all 通过；trellis-update-spec 补齐日志规范与索引，更新后的精确提交计划已获用户确认，实现与规范已推送至 `origin/build-bak`。

## 本轮实现事实

- 已由用户确认 Brief 并启动任务，implement/check 使用本地偏好 inline / check-all-inline；业务分支为 `feature/advanced-channel-reasoning-effort`，实现基线为 `7b042a8e`。提交前 HEAD 为 `67f2e821f`，仅增加既有已关闭任务的自动归档，`verify_gc_commit.py` 审计通过，无业务基线变化。
- 新逻辑独立放在 `relay/advanced_channel_reasoning_effort.go`。六个既有文件共增加 14 行：四个协议入口、Chat 经 Responses 桥接及渠道尝试状态清理。没有修改前端、DTO、schema、转发参数或计费算术。
- 使用本机 HTTP 模拟上游验证 DeepSeek/Qwen、同步/流式、协议双向转换、覆盖 high、删除、透传和消费元数据；模拟响应在账务写入前终止，UI 与日志持久化合同由现有调用链静态核对，未调用真实模型。
- 验证通过：`go test ./relay/... ./service ./controller -count=1`；补充测试和别名处理后重跑 `go test ./relay -count=1`。`go vet ./relay/... ./service ./controller` 通过，最终变更后 `go vet ./relay ./controller` 通过。
- 竞态通过：`go test -race ./relay ./controller -run 'TestAdvancedChannelReasoningEffort|TestResetMainRelayAttemptReasoningEffort|TestSyncAnthropicReasoningEffort' -count=1`；最终 helper 与扩展用例后再跑 `go test -race ./relay -run 'TestAdvancedChannelReasoningEffort|TestSyncAnthropicReasoningEffort' -count=1` 通过。
- `git diff --check`、新增文件空白核对及四个既有 Python 缓存 SHA-256 保留核对通过；relaykit 未修改，无独立模块 API 构建要求。
- Check-All：interactive，requested=auto，effective=full（消费元数据与重试状态传播变化）；三件套、实现假设、完整性与规范均通过，CHK/FBK 为 0；机械同步本文件的实施状态（DOC-001）。其后规范更新结果为 written，日志规范新增高级渠道六节合同、索引同步；规范 diff、签名、引用及写入边界核对通过，业务代码未再修改。实现与规范已提交并推送，生产发布尚未执行。
- [上线后验证] 发布获准后由本任务执行方核对新的高级渠道 DeepSeek low 消费记录和实际详情页展示；无明确强度的请求仍不显示。当前结论仅为本地实现与验证完成。
- 用户补充交付要求后，提交标题改为 `fix(relay): 补齐高级渠道思考强度消费日志 [build]`，推送目标改为 `origin/build-bak`。更新后的计划已获确认，业务提交 `cb68d9cab41b3894ab4c22e5b53342c32b6a1b5d` 已成功推送；四个保留缓存内容完全不变。任务记录通过 `task_progress.py write --complete` 同步，并按原确认范围独立提交。
- 用户已明确要求等待 GitHub Actions 构建成功，将固定版本镜像同步至 ACR，再部署 ACK new-api。构建运行 `38049320240` 当前进行中；镜像同步、发布及生产日志验收尚未完成，不能将开发任务完成态解释为生产已生效。
