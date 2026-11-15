# Repair And Recheck

仅在用户选择或 reporting 验证 claim/started 后读取；其余只读。

范围为用户选择的 `修复全部`/精确 ID，或有效后台回执的全部项：

本修复续行优先于新需求分类和 Active Task Scope Guard：按受检事项、报告证据和选定范围归属；跨模块、既有缺陷或标题/Brief 未列明不构成新需求。不得因此新建或纳管任务，也不得重走首次 Brief 确认；材料同步沿用 `trellis-task-brief` 增量规则。

1. 指针缺失时先按既有恢复规则找回受检事项，不以新建任务代替恢复；无法唯一确定归属时只补核该信息。主会话复用当前 task 已有的合法 implement route；untracked 则重新直接读取个人 pref。不存在合法 route 时进入 `trellis-route(target=implement)`，由其统一解析选择或内置缺省。
2. 修复过程中不对每个问题重复确认。
3. 新增业务歧义、破坏性风险、超出所选修复范围，或跨越已批准的候选、费用或生产操作边界时才暂停；一次说明变化及受影响问题，只确认尚未决定的增量，不重复审批已选修复。
4. 完成定向验证后复用当前 check route 重新执行 Check-All；后台回执在重检结束后记 completed，不以定向验证代替重检。
5. 原 ID 沿用，新根因递增。后续按当前 diff 选深度：原 finding、修复路径、引用/回归和定向证据均闭合时可 light；范围扩大、契约/基线变化、未知 dirty 或证据失效时 full。

完成后返回 `references/reporting-and-disposition.md`，加载 `references/findings-and-reporting.md` 的修复结果模板，再按既有 Gate 分流。
