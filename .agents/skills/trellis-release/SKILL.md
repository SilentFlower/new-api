---
name: trellis-release
description: "汇总 Trellis 任务 release.md，生成版本或上线批次操作单。用于正式上线前整理 SQL、配置、批处理、外部系统、回滚和验证事项。"
---

# Release Summary

汇总一组 Trellis 任务的 `release.md`，生成版本 / 上线批次操作单：`.trellis/releases/<release-name>.md`。

本 skill 只整理上线事项，不执行上线、不提交代码、不推送代码。

## 适用场景

- 用户说“生成上线单”“汇总 release.md”“正式上线前整理操作单”。
- 用户说“版本上线总结”“汇总这些任务的上线事项”“trellis-release”。
- 用户需要把多个任务的 SQL、配置、批处理、外部系统上线、回滚和验证事项合成一份操作单。

## Step 1: 确定 release 名称和任务集合

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

release 名称生成规则：

- 用户给出名称时使用用户名称，并清理成安全文件名。
- 用户未给出时使用日期，例如 `release-2026-06-17`。
- 输出路径固定为 `.trellis/releases/<release-name>.md`。

## Step 2: 读取任务上线事项

对每个任务读取：

- `task.json`
- `prd.md`
- `release.md`（如果存在）

缺失 `release.md` 时：

- 在汇总中列入“未记录上线事项的任务”。
- 默认不阻塞汇总。
- 不要自动为这些任务生成 `release.md`；单任务记录由 finish-work skill override 注入块负责。

## Step 3: 汇总分类

生成操作单草案，按以下固定小节归并：

```md
# 上线操作单：<release-name>

## 范围
- 任务：<task>
- 生成时间：<date>

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
- 每条内容保留任务来源引用，例如 `[06-17-example-task]`。

## Step 4: 写盘确认

写入 `.trellis/releases/<release-name>.md` 前，展示：

- 目标路径。
- 纳入的任务列表。
- 未记录上线事项的任务列表。
- 草案摘要。

等待用户明确确认后再写盘。用户要求调整范围、名称或内容时，先更新草案再重新确认。

## Step 5: 输出结果

写盘后报告：

- 已生成的 release 文件路径。
- 纳入任务数量。
- 未记录上线事项任务数量。
- 上线前仍需人工复核的事项。

## 反模式

- 自动执行 SQL、脚本、部署或外部系统操作。
- 把缺失 `release.md` 的任务静默忽略。
- 汇总时丢失任务来源引用。
- 把 H0 接口中转平台等外部依赖混入普通配置变更。
- 在用户确认前写入 `.trellis/releases/<release-name>.md`。
