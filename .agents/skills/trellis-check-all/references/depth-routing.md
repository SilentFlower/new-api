# Depth Routing

本文件只负责范围、上下文和 light/full 选择。不要在这里展开具体检查 profile。

---

## Step 0：确认范围与适用性

### 0.1 确认变更范围

默认工作区检查：

```bash
git status --short
git diff --name-only HEAD
git ls-files --others --exclude-standard
git log --oneline -10
```

`git diff --name-only HEAD` 用于覆盖 staged + unstaged 的已跟踪文件，未跟踪文件由 `git ls-files` 补充。不能只用 `git diff --name-only` 判断“无变更”。

如果用户要求检查已经提交的 PR/分支改动，先确认目标基线，再使用 merge-base 对应的 diff 范围；`git log -10` 不能替代 PR 变更范围。

如果确认范围内确实无变更，提示用户并终止。

### 0.2 读取工作上下文与规范

存在当前 task 时读取：

- `prd.md`；没有时三件套实现维度标记 `N/A`。
- `design.md`（若存在）。
- `implement.md`（若存在）。
- `check.jsonl` 中列出的 spec/research 文件（若存在）。
- 变更包对应的 `.trellis/spec/` 具体规范。

不得只依赖 session 摘要推断规划内容，必须读取实际文件。

没有当前 task 时运行 `python3 ./.trellis/scripts/untracked_flow.py status --verbose`：

- `hit`：读取 work id、summary 和 stage；三件套实现维度标记 `N/A`，其余维度仍对实际 diff、相关 spec 和本轮可验证证据负责。
- `miss`：仅当用户明确要求检查一个无状态的已知 diff 时继续，并把工作上下文缺失列为风险；否则停止并回到 Request Triage。
- `error`：按损坏状态阻塞报告，禁止用聊天摘要恢复或覆盖游标。

untracked 检查必须处于 `stage=check`，且只读取个人 check 偏好，不创建 task-scoped route decision。

### 0.3 验证运行上下文

默认 `context=interactive`。只有调用方声称来自 auto-loop 时，才通过 runner 的 `status` / `next` 验证以下事实：

- run 为 `running`；
- 当前 task 与本次检查任务一致；
- outstanding action 为 `run_check_all` 或 `run_recheck`。

不得用聊天摘要、自然语言声明或直接读取 raw runtime JSON 代替 runner 验证。验证失败时不得使用 auto-loop 授权；报告失败原因，并按 interactive 边界处理。

### 0.4 解析请求深度

`requested_depth` 只允许 `auto`、`light`、`full`，优先级固定为：

1. 当前用户请求里最新的显式深度意图；
2. validated auto-loop action 的 `requested_check_depth`；
3. 默认 `auto`。

显式意图按语义识别：`简单检查`、`轻量检查`、`light check` 表示 light；`全面检查`、`全量检查`、`最终检查`、`提交前检查`、`full check` 表示 full。同一请求出现多次切换时，以最后一次明确表达为准。单独说 `check` / `check-all` 只是调用统一入口，不自动等同 full。

历史 auto-loop state 缺少深度字段时，runner 会返回 `full`。不得根据文件数、diff 行数或“看起来简单”单独判定 light。

### 0.5 选择有效深度

按以下顺序生成检查画像：

```yaml
check_profile:
  context: interactive | auto-loop
  requested_depth: auto | light | full
  effective_depth: light | full
  confidence: high | fallback-full | escalated
  reasons: [string]
```

决策顺序：

1. `requested=full` -> `effective=full`。
2. 命中任一 hard-full -> `effective=full`；若请求为 light，使用 `confidence=escalated` 并记录原因。
3. `requested=light` 且无 hard-full -> `effective=light`。
4. `requested=auto` 且高置信满足全部 light eligibility -> `effective=light`。
5. 其它情况 -> `effective=full`、`confidence=fallback-full`；不询问用户。

**hard-full 信号**：

- 复杂任务存在 design/implement，且本次变更需要完整验收映射；
- 跨层、跨包、跨仓、submodule 或影响面尚未完全展开；
- 公共 API、CLI、schema、持久化状态、缓存契约、迁移或历史数据兼容；
- 权限、鉴权、安全、资金、并发、时序、状态机或回滚；
- workflow、skill、command、hook 注入或生成快照；
- 安装、升级、发布、push/commit 工作流控制面；
- 正在重检既有 full `CHK-*` 修复结果；
- light 执行中发现未知 dirty path、真实影响面扩大或关键验证缺口。

**light eligibility 必须全部满足**：

- 变更可完整归属，且集中在单一局部行为；
- 无 hard-full 信号；
- 受影响规划条目、直接引用点和回归路径可穷举；
- 存在可运行的定向验证，或仅为无行为风险的文案、注释、局部样式；
- 不在既有 full 修复/重检链中。

light 执行中命中 hard-full 时，立即单向升级 full 并补齐所有适用维度；同一修复/重检循环内 full 不得降级。
