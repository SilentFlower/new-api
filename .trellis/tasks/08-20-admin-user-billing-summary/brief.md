# Brief — 管理员用户账务批量摘要接口

## Goal

- 为管理端提供批量读取多个用户钱包、订阅及 NewAPI 远端状态/角色的只读接口，消除逐用户 N+1 请求，并支持钱包余额和订阅周期剩余额度的稳定全局排序。

## Scope

- 新增管理员鉴权的 `POST /api/user/billing-summary`，按调用方明确提供的用户 ID 查询，去重后单批最多 500 个。
- 响应覆盖钱包 `quota`、累计 `used_quota`、用户分组、有效订阅明细与聚合，以及原生整数 `remote_status`、`remote_role`。
- 使用统一 `sort_by` 选择 `user_id`、`wallet_quota` 或 `subscription_remaining`，配合 `sort_order` 和每项 `sort_key` 支持跨批次合并后的全局稳定排序。
- 有效订阅按有限/无限区分，返回套餐名称、当前周期总额、已用、剩余、有效期和下次重置时间；多个有限订阅安全聚合，任一无限订阅明确标识无限。
- 摘要按实际消费路径的懒重置语义投影到当前周期，但只读接口不写回数据库。
- 逐用户区分 `ok`、`not_found`、`error`；单用户数据异常不阻断其他合法结果，全局数据库查询失败则整体失败。
- 增加参数、排序、远端状态/角色、钱包、订阅边界、局部异常、敏感字段、权限和跨数据库兼容性测试。

## Non-Goals

- 不修改钱包或订阅扣费、优先级、钱包兜底和订阅写操作。
- 不在批量读取中执行用户启停、提权、降权，或门户与 NewAPI 的自动状态/角色同步。
- 不修改现有用户列表、单用户钱包和订阅接口。
- 不实现 AI Fund 页面、筛选、分页、差异提示或显式同步操作。
- 不支持任意排序表达式，不新增数据库表、字段、索引或迁移。

## Key Decisions

- `remote_status` 原样返回 `User.Status`，`remote_role` 原样返回 `User.Role`；AI Fund 分开展示门户事实与 NewAPI 事实，列表刷新不触发同步副作用。
- 统一排序字段为 `sort_by`，方向为 `sort_order`；响应 `sort_key` 使用 `finite`、`infinite`、`unknown` 三类表达可合并的排序语义。
- 无有效订阅是已知零值；用户不存在或摘要不可靠才是 `unknown`，且未知值始终置底。
- 订阅降序时无限值在有限值之前，升序时在有限值之后；同类同值以用户 ID 倒序稳定并列。
- Router、Controller、Service、Model 遵守薄层边界；订阅周期投影只有一个权威实现，并由摘要与预消费路径复用。
- 正常路径使用用户、有效订阅、相关套餐三类批量读取，不产生逐用户 HTTP 或数据库查询。

## Key Context

- 用户原生字段定义在 `model.User`：`Status` 使用启用 `1`、禁用 `2`，`Role` 沿用 NewAPI 现有角色整数常量。
- 现有订阅模型和懒重置逻辑位于 `model/subscription.go`；实现必须保持预消费、后台重置和计费测试行为不变。
- 路由位于现有 `/api/user` 管理员组，管理 API 使用 `{success,message,data}` 响应格式。
- 数据读取必须使用 GORM 通用语义，同时兼容 SQLite、MySQL 5.7.8+ 和 PostgreSQL 9.6+。
- 下游依赖任务为 `/root/project/ai-fund/.trellis/tasks/08-20-admin-user-subscription-optimization`。

## Risks / Deferred

- 共享周期投影重构可能影响订阅预消费写路径，必须先用现有测试锁定行为，再接入只读摘要。
- `remote_role` 只提供远端事实；门户与 NewAPI 的显式角色同步及 Root 权限失败处理由 AI Fund 任务负责。
- `sort_key` 是下游跨批次排序依赖，必须直接测试无限、未知、并列、升降序和多订阅场景。

## Acceptance

- 管理员可一次查询最多 500 个用户，普通用户和未授权请求被拒绝。
- 每个命中用户返回钱包、分组、有效订阅、`remote_status` 和 `remote_role`，且不泄露密码、Access Token、API Key、支付载荷或邮箱。
- 三类数据通过固定次数批量查询完成，没有逐用户 Controller、HTTP 或数据库自调用。
- 钱包余额保持原始 `quota`；有限、无限、多个、无订阅及待懒重置订阅均返回正确摘要。
- 多批结果可按统一 `sort_key` 得到稳定全局顺序，未知值不被当成零。
- 批量读取不执行远端账号同步或账务写入；现有用户、订阅和计费行为保持兼容。
- SQLite、MySQL、PostgreSQL 兼容实现及关键回归测试全部通过。

## Next Step

- 启动任务并进入实现路由，先加载后端开发规范，再按实施计划完成 DTO、批量读取、周期投影、聚合排序和测试。
