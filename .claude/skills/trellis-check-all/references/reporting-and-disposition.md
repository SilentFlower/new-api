# Reporting And Disposition

本文件拥有报告与分流；按实际分支读取：

- full 报告、存在 CHK/FBK/DOC、风险接受或修复重检，以及其它未通过/阻塞/部分验证结果：读 `references/findings-and-reporting.md`，保留完整模型、分类分区、证据和增量展示；未变化的有效已接受问题不重复展开。
- validated auto-loop：读 `references/auto-loop-return.md`，匹配 action 的 record 成功后立即 next；不渲染交互报告。
- 普通 light strict pass：只执行下方简短格式，不读取上述细则。

默认对话报告；明确导出或已有交付约定才保存附件。Maven/runner 沿用机器证据；不以链接替代正文。

## Light 通过报告

基于最终 diff 自然说明改动、影响与结论，复用已读证据，不额外启动 Diff Brief。包含工作/范围、实际验证、必要风险和下一步即可，不强制展开画像、空区块与维度表。例如：

```markdown
## Trellis Check-All 结果

[通过] 轻量检查 · <改动、影响与结论>

- **范围**：<当前任务或事项、受检变更范围>
- **验证**：<实际执行或有效复用的验证与结果>
- **风险**：<仅存在时显示，保留上线后验证的动作、责任边界和预期结果>

### 下一步

<按下方 Gate 继续或明确停止>
```

内部保留 `check_profile` 和三维状态；未触发维度为 `N/A`，不得伪报已验证。strict pass 要求无 CHK/FBK、无阻塞、无阻断型部分验证和未接受实质风险。存在 findings 或接受记录时按条件读取完整规则，不能使用简短格式隐去问题。

## 验证阶段

- `部分验证`：当前结论必需、提交前可完成但证据不足；阻断 strict pass、Update-Spec 和 direct Git。
- `[上线后验证]`：仅部署后、生产或外部系统可安全验收；在“未覆盖与风险”写动作、责任边界和预期结果。它不属于维度状态，不阻断 strict pass、Update-Spec 或 direct Git；strict pass 可以与其并存。
- 本地 fixture、测试环境、静态契约或无副作用命令可完成的检查不得延期为 `[上线后验证]`。Check-All 不得执行生产或外部系统操作，也不得伪报通过。

---

untracked helper 不存检查证据或风险接受。通过且按下方 Gate 默认推进时执行 `advance --stage spec`；用户明确只检查或检查后停止时保持 `stage=check`。未处置 `CHK-*` / `FBK-*`、阻断型部分验证、阻塞或新编辑先 `advance --stage implement`；仅 `[上线后验证]` 不回退。

## Interactive Post-Check Stop Gate

非 validated auto-loop 最终回复前按以下 Gate 准备与展示；接受状态不重贴报告：

1. 先识别当前事项最新有效的明确停止要求：`只检查`、`只读检查`、`检查后停止`、`暂不收尾`、`仅接受、不继续` 等。停止收尾，不运行 Update-Spec 或生成 Git 计划；同一事项后续明确继续可解除该限制。普通“检查一下”不等于只检查后停止，不从无关历史或摘要猜测限制。
2. blocked、阻断型部分验证或未决实质风险（`[上线后验证]` 除外）报告并保持人工，不进入 Spec/Git。普通 CHK/FBK 进入“后台处置”，不因检查或 Git 请求自行修复/接受风险。
3. 没有有效停止要求且 strict pass，或全部 findings 已有效接受时，默认同轮继续；通过条件沿用已加载 findings reference 的风险接受规则和本文件验证阶段，完整登记的 `[上线后验证]` 不阻断。报告或简短确认后同轮进入 Update-Spec，`no-op|written` 再到 Push，`needs-review` 停止。无需 direct Git intent 或额外回复“继续”，通过不授权 Git 写入。
4. 只从当前完成链证据识别 direct Git intent，保留 push / 用户主动 `commit-only` 选择；不从无关历史、摘要、dirty 状态或 auto-loop action 推断。风险接受沿用 findings reference 的证据规则。

### 后台处置

最终回复前读取 `.trellis/scripts/confirmation.md`：普通待处置项必须 项目 `prepare` 完整报告/全部 findings/版本，成功后后台 `wait`，原样完整展示 `display`。实际失败说明原因；只检查、停止、本机制自身开发验证等排除项保持人工。提醒仅 `claimed=true`、`source=policy_timeout`、`action=repair_all_and_recheck` 且 started 成功才修复重检并记终态。准备不需领取。

### 交互式下一步引导

interactive 标准报告末尾必须输出 `### 下一步`，并按首个命中分支给出主动作：

1. 有未处置 findings：提示 `修复全部`、精确 ID、接受当前报告全部风险、`接受风险 <ID> 并继续` 或 `仅保留报告`；不逐项重复确认。
2. 有 blocked、阻断型部分验证或未标记 `[上线后验证]` 的实质风险：指出所需决策、授权或验证，完成后重跑 Check-All；不自行执行生产、外部或破坏性操作。
3. 有有效的明确停止要求且检查通过：说明已按要求停止，之后明确继续才恢复收尾。
4. 无有效停止要求且 strict pass / 已接受风险通过：说明本轮正在进入 `trellis-update-spec`，不要求用户再次回复“继续”；Git 计划确认仍由 Push 执行。

停止边界只控制是否自动推进，不能让报告在没有下一步提示的情况下结束。

Check-All 不新增 direct Git 摘要或 Git 计划；这些仍由 Update-Spec 与 Push 所有。`[上线后验证]` 交给 Push 风险摘要和既有 `trellis-release` / `release.md`。
