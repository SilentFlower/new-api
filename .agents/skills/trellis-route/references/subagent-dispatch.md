# Subagent 路由分发

仅在最终模式为 subagent 时读取。当前平台启动的唯一事实源是同目录 `platform-dispatch.json`；当前运行平台上下文确定稳定 ID，读取对应条目，不从目录存在或 Codex banner 推导执行模式。

## 启动资格

- implement 使用 `implement.target/launch`；Check-All 使用 `checkAll.target/launch/format/verification`。把 catalog 的 launch 翻译成当前 host 的真实工具调用。
- 目标文件存在只证明已投影；还须确认当前 host 暴露声明的 launcher、目标实际可发现且 `eligible=true`。不成立时说明原因，展示 `inlineOnlyReason`（若有）并让用户改选 inline；禁止静默降级、通用 agent、替代角色或上游 workspace-write `trellis-check` fallback。
- Check-All 目标必须明确 audit-only：允许返回 DOC 候选，禁止自修源代码、测试、配置或任务材料。`verification` 不放宽只读边界；不合格时停止，由用户重选。
- agent 直接执行，不递归 dispatch implement/check agent。

## 上下文与 Prompt

任务 prompt 第一行固定为 `Active task: <task path from task.py current>`。实现使用当前任务契约；Check-All 先读 `check.jsonl` 及其文件，再读 prd.md、design.md（若存在）、implement.md（若存在）。

untracked prompt 第一行固定为 `Untracked work: <work-id>`，包含事项摘要、当前 stage、实际 diff、相关 spec、已有验证和本轮职责。helper 只提供流程游标，不提供 scope、baseline、fingerprint 或 owner evidence。不得写 `Active task:`，不得要求 task PRD/JSONL 或伪造 task artifacts。

Check-All prompt 指明：执行本地 `trellis-check-all` 的 audit-only collect-all 全流程，使用专用角色规则并完成三件套实现、实现假设、完整性与规范三个维度。返回完整报告、check_profile、全部 CHK/FBK/DOC 候选、验证证据与剩余风险，不输出 commit/push 计划，不替用户决定修复范围。

风险分类和只读细则由已投影的专用 agent（作者模板 `check-all-agent-body.md`）及 Check-All owner 所有；发送 prompt 不复制完整审计手册。必须确认其契约保留：

- 根因先于严重度，主路径 `CHK-*`、兜底 `FBK-*` 和低风险事实漂移 `DOC-*` 分开；CHK/FBK 均分 P0/P1/P2。
- 已声明的兜底契约只影响证据和严重度，不改变 `FBK-*` 归属；具体位置、可达场景和问题证据足以返回 FBK，不要求异常实际发生。保护收益/验证方式属于报告完整度，泛化建议不报告。
- 提交前必需但证据不足为阻断型部分验证；只可上线后、生产或外部环境验收的内容保留 `[上线后验证]` 动作、边界和预期结果，不执行且不误标阻断。
- 审查只读，DOC 也只返回候选，主会话按 Check-All 规则处理；风险接受由用户决定，agent 不接受、不清除风险。

## implement 跳过编译

仅 implement subagent 读取 `.trellis/config.yaml` 的 `subagent_skip_compile`。缺失或不是 true 视为 false；check 任何模式不得附加跳过编译。为 true 时 prompt 附加：

```text
本项目 subagent_skip_compile=true。跳过编译相关命令，完成其它必要验证并说明未编译；不要把未执行的编译报告为通过。
```

接收 implement 结果后返回 workflow Phase 2.1 completion contract；Check-All 结果交主会话按其 owner 分流。route 不独立拥有完成后停止策略。
