# Implement — Build 薄层后续治理

## Execution Order

1. 启动并完成 `07-18-build-relay-info-methods-thin-layer`。
2. 启动并完成 `07-18-build-responses-compact-audit-thin-layer`。
3. 启动并完成 `07-18-build-alpha-search-validation-thin-layer`。
4. 启动并完成 `07-18-build-distributor-compact-detection-thin-layer`。
5. 启动并完成 `07-18-build-responses-handler-compact-thin-layer`。
6. 启动并完成 `07-18-build-public-token-log-frontend-thin-layer`。
7. 最后执行父任务集成复核：`git diff --stat`、后端相关测试、Default/Classic 前端构建或类型检查。

## Validation Plan

- 后端分阶段：
  - `go test ./relay/common ./service ./relay/helper ./middleware ./relay -count=1`
  - `go test ./dto ./middleware ./relay ./relay/channel/openai ./controller ./service -count=1`
  - `git diff --check`
- 前端分阶段：
  - `cd web/default && bun run typecheck && bun run build`
  - `cd web/classic && bun run build`
- 全量可选：
  - `go test ./... -count=1`

## Review Gates

- 每个子任务完成后检查原上游文件 diff 是否只剩窄调用。
- 如果某个治理点需要扩大到重写主流程，停止并回到该子任务设计文档更新范围。
- 所有检查通过后，进入 `trellis-push` 前再汇总精确文件范围。

## Rollback Points

- 每个子任务提交前保持可独立撤销。
- 不使用 `git reset --hard` 或覆盖用户改动。
