# Brief — 接通 ACK 百炼决策模型

## Goal

- 让现有 ai、project 用户通过 ACK 网关正常调用 decision-model，并按实际输入 Token 结算。

## Scope

- 独立百炼决策插件、集中回归测试、生产插件安装与专用渠道、价格配置、原渠道模型清理及正式验收。

## Non-Goals

- 预览模型和聊天接口包装留作独立扩展。
- New API 镜像与核心代码、ai-fund 业务代码及长期管理授权保持当前状态。

## Technical Overview

- 通过现有插件 API 安装独立 bailian-decision 插件，创建“百炼决策模型”任务插件渠道，复用 18 号的百炼工作空间及凭据。

## Context & Decisions

- 百炼直连已返回 200 和 83 个输入 Token，原版 TypeSafe 插件会拒绝该模型。
- **【调用方式】客户端使用 /v1/systemone，提交 state、questions，支持正式版的文本和图像决策。**
- **【计费】沿用现有输入单价和分组倍率，按真实 usage.input_tokens 单次结算。**
- **【切换顺序】新渠道验收通过后，再从 18、19 号移除 decision-model，保留原聊天及重排配置。**

## Risks

- 正式操作涉及插件、渠道和模型价格配置。先做相同镜像的隔离验证，并保存原配置供精确回退。

## Acceptance

- 正式网关使用模型 Key 返回决策答案；ai、project 分组可用；实际 Token、扣费和日志一致。
- 三类问题、图像、权限拒绝、上游错误和计费异常路径通过验证，原渠道配置对账通过。

## Next Step

- 启动任务，完成独立插件及固定镜像联调。
