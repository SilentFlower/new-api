# Full Profile

Full 是完整验收映射和全影响面审查。只有 `check_profile.effective_depth=full` 时读取本文件；共享验证规则见 `references/verification.md`。

---

## Step 1：对照规划三件套检查实现

untracked 上下文没有 task artifacts，本 Step 标记 `N/A`；不得把 summary、stage 或聊天记录当成 PRD。仍须完整执行 Step 2 与 Step 3，并对实际 diff、相关 spec、当前验证证据和多仓分发边界负责。

### 1.1 验收依据

- PRD Requirement / Acceptance Criteria：行为基线。
- Design API、数据模型、数据流、关键决策和 rollback：技术基线。
- Implement 中未被前两者覆盖的产物、review gate 和 rollback point：补充基线。

full 提取所有适用条目。同一行为在多份文档中重复出现时合并验收，保留全部来源位置；不同约束、边界场景或相互冲突的要求不得合并丢失。实际阅读对应代码后再判断。

### 1.2 必查类型

| 来源 | 可验证条目 |
| --- | --- |
| `prd.md` | AC、需求、业务规则、UI 文案、边界和异常场景 |
| `design.md` | API 路径/方法/字段、数据模型、数据流、关键 tradeoff、rollout/rollback |
| `implement.md` | 补充产物是否落地、review gate 是否满足、rollback point 是否可用；不重复核对已覆盖的实现步骤 |

`implement.md` 中的 validation command 在本步骤只做静态前提核对；真实运行归 Step 3。

### 1.3 追踪方法

| 条目类型 | 追踪路径 |
| --- | --- |
| API 行为 | Controller/Handler -> Service -> DAO/Storage |
| 前端交互 | 组件 -> 事件 -> 状态管理 -> API 调用 |
| 数据校验 | 前端规则 + 后端 validator/service |
| UI 文案 | 组件、i18n/locale 或其它有效文案来源 |
| 计算转换 | 实际 service/utility 算法及边界值 |
| 状态流转 | 状态定义 + 允许的转换条件 |
| Schema | DTO/类型/迁移中的字段、类型、约束和默认值 |
| Implement 步骤 | 对应代码、配置、迁移或资产是否存在且可用 |

文案要求逐字一致时，对照最终有效文案来源；不要强制要求文案必须直接写在组件字面量中。

同一条调用链只追踪一次，记录实现位置和关键边界证据，供 Step 2 与 Step 3 复用。后续只补尚未覆盖的约束、调用方、异常分支或失效证据；不得因复用而跳过独立的验收判断。

### 1.4 记录结果

发现偏差、缺失、部分实现或文案不一致时，写入统一问题集合并继续。不要在此步骤询问“先修还是继续检查”。

---

## Step 2：实现假设验证

根据实际变更选择适用 Dimension。每个适用 Dimension 都要确认源码或真实契约证据，不能凭记忆通过。先复用 Step 1 的追踪结果，再核对本维度新增的约束；验证命令需求统一交给 Step 3，不在各 Dimension 分别运行。

### Dimension A：API Contract

**Trigger**：新增或修改已有 API 调用、请求参数或响应解析。

- 读取 Controller/Handler 和 DTO/Schema，确认实际请求、响应结构。
- 契约仍有歧义时，补查项目内同 API 或同模式调用。
- 确认参数名、类型、默认值、分页字段和起始页码。
- 覆盖正常、空值、零值和错误响应。

### Dimension B：Component Context

**Trigger**：在 Modal、Drawer、Tab 或条件渲染容器内修改有状态组件。

- 确认容器关闭或切换时是否销毁子组件。
- 确认受控值、初始化值和外部状态绑定。
- 确认状态保持/重置行为符合规划。
- 状态约定仍不明确时，补查项目内相同容器的既有用法。

### Dimension C：Data History

**Trigger**：新增、修改或重新解释持久化字段。

- 确认历史记录的新字段值和 null/零值行为。
- 确认过滤、聚合和降级查询能处理历史数据。
- 追踪新字段的写入来源和可靠性。
- 无可用历史数据环境时按验证阶段判断：提交前原则上可完成但缺少证据时标记 `部分验证` 或 `阻塞`；本质依赖部署后真实状态时登记 `[上线后验证]`，不得伪报已执行。

### Dimension D：Data Flow Trace

**Trigger**：变更跨越 UI、API、Service、Storage 中的两个或更多边界。

- 模拟完整请求路径和返回路径。
- 确认各层参数名、类型、嵌套层级一致。
- 覆盖缺省、空值、零值、特殊字符和错误传播。
- 分层代码分别正确不等于整条链路正确，必须连起来核对。

发现假设错误时写入统一问题集合并继续其它可执行检查。只有该错误让后续检查前提失效时，才按“真正阻塞”规则暂停。

---

## Step 3：完整性、规范与项目验证

执行 `references/verification.md` 的共享清单，汇总 Step 1/2 和项目 spec 的验证需求，复用有效证据并只执行未覆盖的必要命令。数据流沿用前两步证据，不重新完整追踪。

所有发现候选按 `references/fallback-findings.md` 先判定 `CHK-*` / `FBK-*`，再分配严重度。严重度不得反向决定通道；不满足三项硬准入的泛化建议不报告，保护收益或验证环境不完整则保留 FBK 并标记报告缺口。

---

## Full 通过条件

Full 通过必须同时满足：

- 所有适用 PRD / design / implement 条目已映射到实现或明确 `N/A`；
- 所有触发的假设 Dimension 已完成源码或真实契约核对；
- 项目规范、复用、依赖、同层一致性和验证命令已覆盖实际变更范围；
- strict pass：无 `CHK-*`、无 `FBK-*`、无阻塞、无部分验证、无实质剩余风险；或
- 已接受风险通过：所有剩余 `CHK-*` / `FBK-*` 都有当前有效的用户风险接受，且无阻塞、无部分验证、无未接受的实质剩余风险。

strict pass 或已接受风险通过可以与已明确登记的 `[上线后验证]` 并存；该标签只表示发布阶段仍需执行的验收，不表示当前已经验证。
