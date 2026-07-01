---
name: trellis-release
description: "核对并汇总 Trellis 任务 release.md，生成版本或上线批次操作单。用于正式上线前整理 SQL、配置、批处理、外部系统、回滚、验证事项和文档漂移风险。"
---

# 上线操作单汇总

核对并汇总一组 Trellis 任务的上线事项，生成版本 / 上线批次操作单：`.trellis/releases/<release-file>.md`。

本 skill 只整理和核对上线事项，不执行上线、不提交代码、不推送代码。

## 适用场景

- 用户说“生成上线单”“汇总 release.md”“正式上线前整理操作单”。
- 用户说“版本上线总结”“汇总这些任务的上线事项”“trellis-release”。
- 用户需要把多个任务的 SQL、配置、批处理、外部系统上线、回滚和验证事项合成一份操作单。
- 用户担心任务文档、实现代码、提交记录或上下文压缩后发生文档漂移，需要上线前复核。

## 核心原则

- **先核对，再汇总**：不能只复制已有 `release.md`。必须对照任务文档、实现计划、检查记录和 git 证据判断上线事项是否完整。
- **文件证据优先**：即使刚经历上下文压缩、会话恢复或你“记得”之前做过什么，也必须重新读取本地文件和 git 证据。
- **不静默相信旧文档**：已有 `release.md` 只能作为输入之一。发现缺失、冲突、过期或证据不足时，在批次上线单里标记 `Needs human review`。
- **保留来源引用**：每条上线事项都要标注任务来源，例如 `[06-17-example-task]`。

## Step 1: 确定任务集合与 release 文件名

先读取本地任务目录，不要凭记忆回答：

```bash
python3 ./.trellis/scripts/task.py current --source || true
python3 ./.trellis/scripts/task.py list --mine || true
python3 ./.trellis/scripts/task.py list-archive || true
```

根据用户输入确定任务集合：

- 如果用户明确给出任务目录 / slug，优先使用这些任务。
- 如果用户给出版本名、日期范围、wave、批次名，从 `.trellis/tasks/` 和 `.trellis/tasks/archive/` 中匹配候选任务。
- 如果用户只说“当前版本”但没有足够上下文，先列出候选任务并问一个问题确认范围。

release 文件名生成规则：

- 用户显式给出文件名 / release 名称时，使用用户名称，并清理成安全文件名。
- 用户未给出时，使用 `YYYY-MM-DD-<release-slug>.md`。
- `<release-slug>` 优先来自用户给出的版本号、批次名、wave 名称；推导不到时使用 `release`。
- 示例：`2026-06-25-v0.3.1-beta.1.md`、`2026-06-25-h0-relay-batch.md`、`2026-06-25-release.md`。
- 文件名清理规则：转小写，空白替换为 `-`，移除路径分隔符和除字母、数字、点、下划线、短横线以外的字符，合并连续短横线，去掉首尾短横线。
- 输出路径固定为 `.trellis/releases/<release-file>.md`。
- 如果目标文件已存在，追加 `-2`、`-3` 等数字后缀，不能覆盖已有上线单。

## Step 2: 读取任务材料和 git 证据

对每个任务读取：

- `task.json`
- `prd.md`
- `design.md`（如果存在）
- `implement.md`（如果存在）
- `implement.jsonl`
- `check.jsonl`
- `release.md`（如果存在）

同时读取可用的本地证据：

- `task.json` 中记录的提交、分支、相关文件和任务状态。
- 任务文档中明确提到的代码路径、配置路径、脚本路径和外部系统。
- 近期 work commit 的文件列表和 diff，例如 `git log --oneline --name-only -n 30`、`git show --name-only <hash>`。
- 当前 `git status --porcelain` / `git diff --name-only` 只作为风险提示；未提交 dirty path 不能直接当作已完成上线内容。

核对时重点搜索这些上线信号：

- SQL、migration、DDL、DML、数据库脚本。
- 环境变量、配置中心、feature flag、权限、密钥、外部地址。
- 部署脚本、一次性命令、数据修复、定时任务触发、后台任务重跑。
- H0 接口中转平台、网关、消息平台、第三方管理后台等不在当前代码仓内但需要配合上线的系统。
- 回滚步骤、上线顺序、上线后验证要求。

## Step 3: 核对 release.md 漂移

对每个任务形成核对结论：

- `已覆盖`：已有 `release.md` 与任务材料、实现影响面和提交证据一致。
- `缺失 release.md`：任务没有 `release.md`，但批次上线单仍要记录核对结果。
- `疑似漂移`：已有 `release.md` 与任务材料 / 提交证据不一致，或明显遗漏上线信号。
- `Needs human review`：证据不足、上下文不完整、dirty path 影响判断，或无法确认外部系统是否已处理。

处理规则：

- 缺失 `release.md` 时，在汇总中列入“未记录上线事项的任务”，并写明从其他证据核对出的事项或风险。
- 不要自动为这些任务生成单任务 `release.md`；单任务记录由 finish-work skill override 注入块负责。
- 已有 `release.md` 发生漂移时，不要静默改写原任务文件；在批次上线单的“风险标记 / 需人工复核”中记录差异。
- 如果无法高置信判断某项是否需要上线操作，保留为 `Needs human review`，不要写成“无”。

## Step 4: 汇总分类

生成操作单草案，按以下固定小节归并：

```md
# 上线操作单：<release-name>

## 范围
- 文件：.trellis/releases/<release-file>.md
- 生成时间：<date>
- 任务：
  - <task>

## 核对摘要
| 任务 | release.md | 核对证据 | 结论 |
| --- | --- | --- | --- |
| <task> | 存在 / 缺失 | prd / implement / check / git | 已覆盖 / 缺失 release.md / 疑似漂移 / Needs human review |

## 风险标记 / 需人工复核
- 无

## SQL 变更
- 无

## 配置变更
- 无

## 批处理 / 部署脚本 / 数据修复
- 无

## 外部系统 / 依赖平台上线
- 无

## 上线顺序
- 无特殊顺序

## 回滚说明
- 回滚代码即可

## 上线后验证
- 按任务验收标准验证

## 未记录上线事项的任务
- 无
```

分类规则：

- SQL、migration、DDL、DML、数据库脚本放入“SQL 变更”。
- 环境变量、配置中心、feature flag、权限、密钥、外部地址放入“配置变更”。
- 部署脚本、一次性命令、数据修复、定时任务触发、后台任务重跑放入“批处理 / 部署脚本 / 数据修复”。
- H0 接口中转平台、网关、消息平台、第三方管理后台等不在当前代码仓内但需要配合上线的系统，放入“外部系统 / 依赖平台上线”。
- 回滚和验证不能只写模板默认值；如果任务材料或 git 证据显示存在特殊回滚 / 验证要求，必须覆盖默认项。
- 每条内容保留任务来源引用，例如 `[06-17-example-task]`。

## Step 5: 写盘确认

写入 `.trellis/releases/<release-file>.md` 前，展示：

- 目标路径。
- 纳入的任务列表。
- 文件名生成依据和冲突处理结果。
- 每个任务的核对结论。
- 未记录上线事项的任务列表。
- 漂移 / 风险 / 需人工复核列表。
- 草案摘要。

等待用户明确确认后再写盘。用户要求调整范围、名称或内容时，先更新草案再重新确认。

## Step 6: 输出结果

写盘后报告：

- 已生成的 release 文件路径。
- 纳入任务数量。
- 未记录上线事项任务数量。
- `疑似漂移` / `Needs human review` 数量。
- 上线前仍需人工复核的事项。

## 反模式

- 自动执行 SQL、脚本、部署或外部系统操作。
- 只汇总已有 `release.md`，不核对任务材料和 git 证据。
- 上下文压缩或会话恢复后依赖记忆判断上线事项。
- 把缺失 `release.md` 的任务静默忽略。
- 发现旧 `release.md` 与实现证据不一致时仍写“无风险”。
- 汇总时丢失任务来源引用。
- 把 H0 接口中转平台等外部依赖混入普通配置变更。
- 在用户确认前写入 `.trellis/releases/<release-file>.md`。
- 覆盖已有 `.trellis/releases/` 文件。
