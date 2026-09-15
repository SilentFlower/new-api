# Action 内恢复与三轮纠正

收到 `status=retryable reason=artifact-recovery-required|artifact-recovery-failed` 时读取本文件，并在同轮执行纠正。它是原 action 的恢复通道，不是新的业务 action，也不要求用户回复“继续”。

## 登记与恢复

- 当前任务四文档优先用 `decide --task-file prd.md|design.md|implement.md|brief.md`，可重复。普通 `--file` 仍是仓库相对路径或 `<repository>::<path>`，可登记尚未创建的代码文件；裸文档名不自动解释为任务路径。
- 无效路径被拒绝时不会写入 decision/pending/manifest；有可信原 action 的确定 basename 错误会返回恢复诊断。根同名文件、未知仓库、越界、软链或 protected 冲突不能猜测修正。
- `next` 重放同一 outstanding action、issued_at、深度和原文档基线。正确 pending 覆盖的修改可以恢复执行，但只能由后续真实 `record` 消费 pending 和重绑 manifest。
- 原 Check 只有 implement/brief 的待申报 DOC 变化时，恢复后仍按 Check 的 DOC 资格与精确文件申报规则回写；不能把恢复查询视为接受内容。

## Agent 纠正步骤

1. 读取诊断的 `recovery_id`、`source`、原 action、原 decision ID、`baseline`、`candidates`、`changed` 和 `attempts`，结合原 `decisions.jsonl`、任务 artifacts、真实 diff 和执行证据核实归属。文件集合吻合不能代替需求和语义审查。
2. 对已证明的 basename 错登记，提交诊断中的精确一对一映射。旧 pending 的纠正只追加普通决策审计并改文件键，保留原 choice、requirements、risk 与修改前 baseline。初始 decide 被拒绝时，纠正成功后必须重新调用原 decide，登记成功才编辑。
3. 存在额外误改时，先保全新增记录，仅撤回能证明由本 action 造成的误改，再提交校验。runner 不覆盖文件；不得恢复用户、外部会话或来源不明的修改。
4. 无法安全归因、原语义不足、涉及 Open Questions、需求扩张或风险黑名单时提交 `blocked`。

```bash
python3 ./.trellis/scripts/auto_loop.py reconcile \
  --run-id <run> --task <task> \
  --recovery-id <诊断ID> --attempt-id <本次唯一ID> \
  --result ok|failed|blocked --summary "<纠正结论>" \
  [--evidence "<已读取的证据>" ...] \
  [--file-map '<旧唯一键>=<当前任务同名文档唯一键>' ...]
```

runner 会重读文件并验证映射、基线和 protected 边界，不能仅凭 `--result ok` 放行。`reconciled` 后按返回指令在同轮继续原 action 或重试原 decide，提交原 action 的真实结果；纠正不能代表 Check 通过、实现完成或提交成功。非 Check record 若返回恢复诊断，也必须先纠正，再重新提交原真实 record，不能直接 next 跳过。

## 预算与重复恢复

初次发现为 0 次；next/status/resume、压缩恢复和同尝试同载荷重放不计数。前三次以明确提交的实际纠正计数：前两次失败继续同轮纠正，第三次可成功，第三次失败才终态 blocked。更换诊断或错误路径不能重置同一 action 的预算。

同一个 attempt-id 仅用于重放完全相同的请求；载荷变化用新 ID。回执丢失后重放原请求，日志已写但 runtime 未写也不会重复追加决策。观察值变化会拒绝沿用旧回执，按 runner 指令重新诊断；不手写 runtime 或删审计来“修复”状态。

既有 Check `status=retryable reason=artifact-drift` 继续由原 Check 重录通道负责：不调用 next/reconcile 获取第二套预算，保留原 3 次 retryable 后第 4 次 blocked 的规则。next 已建立的恢复诊断先完成，再允许原 record；一个错误只走一个通道。fix/recheck、commit repair 和部分成功提交仍由原 owner 管理。

恢复期 run/item 仍 running，依赖项不会提前失败。未知漂移、无可信基线、受保护内容变化或预算耗尽仍按原阻塞和依赖传播规则处理；独立任务继续。历史终态 run 不自动复活，仍需用户显式 retry-blocked。
