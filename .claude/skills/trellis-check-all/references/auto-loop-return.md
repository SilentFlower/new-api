# Auto-Loop Return

仅在 depth-routing 已验证当前 runner action 时读取；不使用交互式报告和下一步。

## Auto-Loop Return Gate

validated auto-loop 复用相同的画像、profile、`DOC-*` 通道和问题模型，但不展示普通模式的修复选择：

- 有 `DOC-*` 且可自动修复：主会话先应用并验证；只有当前任务 `implement.md` / `brief.md` 的实际变化追加精确 `--doc-remediation-file`，源码注释等其它 DOC 改动不得传该参数；随后重算 diff、范围和画像，再决定最终 `ok|failed|blocked`。
- 有剩余 `CHK-*` 或 `FBK-*`：向 runner `record --result failed --effective-check-depth <light|full> --check-depth-reason <summary>`，摘要包含最高严重度、两类问题 ID、根因、受影响文件和已自动修复的 `DOC-*`。validated auto-loop 不创建也不复用 interactive 风险接受。
- 产品决策、越权、提交前生产副作用授权或破坏性决策：`record --result blocked`。
- 无 `CHK-*` / `FBK-*` 和阻断型部分验证：`record --result ok --effective-check-depth <light|full> --check-depth-reason <summary>`；摘要包含自动修复和全部 `[上线后验证]`，后者不阻断且不得代执行。
- record 成功后立即 `next`；若返回 `status=retryable reason=artifact-drift`，不得 `next`，先按 runner 指令在同一 outstanding action 内自纠并重录。validated auto-loop 不渲染交互式下一步段、不提示用户回复“继续”、不等待普通修复范围选择。
- `artifact-recovery-required` 交由 `trellis-auto-loop`；不叠加 Check 重录预算，不改 fix/recheck、commit-only、队列。

subagent 只返回结构化 `CHK-*`、`FBK-*`、`DOC-*` 候选、报告和 `check_profile`；主会话收到后必须完成允许的 `DOC-*` 处理，再完成匹配 action 的 `record + next`。

---
