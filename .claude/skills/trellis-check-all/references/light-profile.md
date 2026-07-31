# Light Profile

Light 是局部且可穷举的检查，不是“少看一点”的检查。只在 `check_profile.effective_depth=light` 时读取本文件。

---

## 适用边界

只有同时满足以下条件才继续 light：

- 变更集中在单一局部行为或无行为风险的文案、注释、局部样式；
- 影响面、直接引用点和回归路径能完整列出；
- 没有 hard-full 信号；
- 有定向验证，或变更天然不需要运行行为验证；
- 不是 full 修复/重检链的一部分。

执行中发现任一边界不成立，记录升级原因，切换到 `references/full-profile.md`。

---

## 维度 1：三件套实现

只提取受影响的 PRD / design / implement 条目。每条必须记录来源位置，并阅读对应实现后判断：

- 需求、AC、业务规则是否被本次局部变更影响；
- design 中相关 API、字段、数据流、rollback 是否仍成立；
- implement 中与本次 diff 直接相关的步骤是否落地；
- UI 文案要求逐字一致时，对照最终有效文案来源。

未被本次变更触达且无直接依赖的规划条目标记 `N/A`，不得伪装为 full 已验证。

---

## 维度 2：实现假设

只检查被本次 diff 触发的假设维度。每个适用维度都要用源码或真实契约证明，不能凭记忆通过。

| Dimension | Trigger | Light 检查 |
| --- | --- | --- |
| API Contract | 新增或修改 API 调用、请求参数或响应解析 | 读取实际 Handler/DTO/Schema 或同模式调用，确认字段名、类型、默认值和错误响应 |
| Component Context | Modal、Drawer、Tab 或条件渲染容器内修改有状态组件 | 确认销毁/保留、受控值、初始化值和重置行为 |
| Data History | 新增、修改或重新解释持久化字段 | 确认 null/零值/历史记录降级；无环境证据标记 `部分验证` |
| Data Flow Trace | 变更跨越 UI/API/Service/Storage 中两个或更多边界 | 连起来核对参数名、类型、嵌套层级和错误传播 |
| Verification Tests | A-D 任一适用 | 找到并运行定向测试；缺少测试记录为 `CHK-*` |

未触发的 Dimension 标记 `N/A`。

---

## 维度 3：完整性、规范与项目验证

读取 `.agents/skills/trellis-check/SKILL.md`（Claude-only 项目读取对应 `.claude` 副本），只复用与本次局部变更相关的：

- 适用 spec 的读取方法；
- 定向 lint、typecheck、测试或格式检查命令；
- 复用、依赖、同层一致性、debug logging、warning suppression 和类型安全绕过检查。

在 Check-All 内执行时，`trellis-check` 中任何直接修复、补测试、反复修到通过的指令一律失效。验证失败记录为 `CHK-*` 并继续其它独立验证。

---

## Light 通过条件

Light 通过必须同时满足：

- 所有受影响规划条目已核对；
- 所有触发的假设维度状态为 `通过` 或合理的 `N/A`；
- 定向验证已运行并通过，或明确说明无需运行的原因；
- 无 `CHK-*`、无阻塞、无部分验证、无实质剩余风险。

存在未覆盖但不影响局部结论的内容时，必须在“未覆盖与风险”中说明。
