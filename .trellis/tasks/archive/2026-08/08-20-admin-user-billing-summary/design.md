# 管理员用户账务批量摘要接口设计

## 1. 设计目标

- 用固定次数的批量数据库查询返回最多 500 个明确用户的钱包、订阅及远端账号事实。
- 通过统一 `sort_by` 和 `sort_key` 支持钱包余额与订阅周期剩余额度的跨批次全局排序。
- 保持现有订阅消费、懒重置、有限/无限额度和有效期语义。
- 保持 Router、Controller、Service、Model 为薄层，不在多层重复校验、订阅计算或排序规则。
- 远端状态/角色只读展示，不引入任何自动同步或提权副作用。
- 不修改现有接口、不新增迁移。

## 2. API 契约

### 2.1 路由

在现有 `/api/user` 管理员路由组中新增：

```http
POST /api/user/billing-summary
Authorization: 管理员会话
Content-Type: application/json
```

该路由复用 `middleware.AdminAuth()`，Router 不承载业务判断。

### 2.2 请求

```json
{
  "user_ids": [12, 34, 56],
  "sort_by": "subscription_remaining",
  "sort_order": "desc"
}
```

- `user_ids`：必填，正整数，去重后 1 至 500 个。
- `sort_by`：可选，允许 `user_id`、`wallet_quota`、`subscription_remaining`，默认 `user_id`。
- `sort_order`：可选，允许 `asc`、`desc`，默认 `desc`。
- 非法请求使用现有管理 API 参数错误；批量超限复用现有批量过多 i18n 错误。

### 2.3 响应

```json
{
  "success": true,
  "message": "",
  "data": {
    "sort_by": "subscription_remaining",
    "sort_order": "desc",
    "items": [
      {
        "user_id": 12,
        "status": "ok",
        "remote_status": 1,
        "remote_role": 1,
        "wallet": {
          "quota": 100000,
          "used_quota": 50000,
          "group": "default"
        },
        "subscription": {
          "active_count": 1,
          "unlimited": false,
          "finite_total": 200000,
          "finite_used": 50000,
          "finite_remaining": 150000,
          "items": [
            {
              "subscription_id": 9,
              "plan_id": 3,
              "plan_title": "Pro",
              "unlimited": false,
              "amount_total": 200000,
              "amount_used": 50000,
              "amount_remaining": 150000,
              "start_time": 1760000000,
              "end_time": 1762592000,
              "last_reset_time": 1760000000,
              "next_reset_time": 1760086400
            }
          ]
        },
        "sort_key": {
          "kind": "finite",
          "value": 150000
        }
      }
    ]
  }
}
```

单项状态：

| `status` | 含义 | 账务/远端字段 | `sort_key` |
| --- | --- | --- | --- |
| `ok` | 用户存在且摘要可靠 | 返回 `remote_status`、`remote_role`、钱包和订阅摘要 | `finite` 或 `infinite` |
| `not_found` | 用户不存在或已软删除 | 省略远端、钱包和订阅摘要 | `unknown` |
| `error` | 单用户订阅数据无法可靠计算 | 可保留已可靠读取的 `remote_status`、`remote_role` 和钱包，订阅返回机器错误码 | `unknown` |

`remote_status` 和 `remote_role` 直接使用 NewAPI 原生整数：不转换成门户枚举，不返回展示文案，不根据差异执行写操作。响应不包含用户名、邮箱、密码、Access Token、API Key、支付数据或完整用户模型。

## 3. 分层边界

### 3.1 Router

- 只在管理员用户路由组注册静态 `POST /billing-summary`。
- 不解析请求、不做额度或角色判断、不直接访问 Model。

### 3.2 Controller

- 使用 DTO 绑定 JSON，调用 Service 单一入口，并使用现有管理 API 响应助手。
- 只处理请求格式和 Service 错误映射，不查询数据库、不遍历订阅、不比较角色、不实现排序。

### 3.3 Service

- 归一化并校验用户 ID、`sort_by`、`sort_order`，保证直接调用时也遵守契约。
- 一次获取数据库时间，编排用户、有效订阅和套餐三类批量读取。
- 直接透传 Model 返回的 `User.Status` 和 `User.Role` 到 `remote_status`、`remote_role`。
- 按用户组装订阅摘要、局部错误和统一 `sort_key`，并应用唯一排序比较器。
- 不拼 SQL、不直接依赖 GORM、不复制订阅周期推进算法、不执行账号同步。

### 3.4 Model

- 按用户 ID 批量选择 `id`、`quota`、`used_quota`、`group`、`status`、`role`，不加载敏感列。
- 按用户 ID 和同一数据库时间批量选择 `status = active AND end_time > now` 的订阅。
- 按去重套餐 ID 批量选择套餐标题及重置规则所需字段。
- 查询全部使用 GORM `IN`、最小 `Select` 和稳定 `Order`，不使用数据库专用 SQL。
- 将现有懒重置计算提炼为共享周期投影：预消费路径按需保存，摘要路径只读投影结果。
- 不负责 HTTP 参数、管理 API 响应、门户差异判断和跨用户排序。

## 4. 数据流

```text
AdminAuth
  -> Controller 绑定 JSON
  -> Service 归一化 user_ids / sort_by / sort_order
  -> Model 批量读取用户钱包 + remote_status + remote_role
  -> Model 批量读取有效订阅
  -> Model 批量读取相关套餐
  -> 共享周期投影得到当前周期视图
  -> Service 聚合摘要与 sort_key
  -> Service 统一稳定排序
  -> Controller 返回管理 API 包装
```

正常路径最多三次批量读取，查询次数不随用户数量线性增加。

## 5. 远端状态与角色边界

- `remote_status` 对应 `model.User.Status`：当前 `1` 为启用、`2` 为禁用。
- `remote_role` 对应 `model.User.Role`：沿用 `common.RoleGuestUser`、`RoleCommonUser`、`RoleAdminUser`、`RoleRootUser` 的原生整数值。
- AI Fund 可把门户本地状态/角色与这两个字段并列展示并提示不一致。
- 读取接口不接受门户状态/角色作为输入，不调用 NewAPI 用户管理动作，也不根据差异自动修复。
- 角色提升、撤销和账号启停继续由现有显式管理接口及其权限校验负责，不能借批量摘要绕过 Root/Admin 边界。

## 6. 订阅投影与聚合

- 只读取 `status = active` 且 `end_time > now` 的订阅。
- `amount_total = 0` 表示无限；`amount_used` 表示当前周期已用量。
- `next_reset_time <= now` 时，使用与预消费相同的周期推进规则把只读投影视图的 `amount_used` 置零，并推进 `last_reset_time`、`next_reset_time`；摘要不执行保存。
- 套餐缺失、重置规则非法或投影失败时，该用户为 `error`，不得猜测订阅余额。
- 单个有限订阅剩余为 `max(amount_total - effective_amount_used, 0)`。
- 多个有限订阅分别安全累加总额、已用和剩余；每次累加前检查 `int64` 溢出。
- 任一有效无限订阅存在时聚合标记无限；有限订阅字段仍保留用于混合订阅展示。
- 无有效订阅返回已知零值和空 `items`。

## 7. 统一排序

| `sort_by` | 结果 | `sort_key` |
| --- | --- | --- |
| `user_id` | 用户存在 | `{kind: finite, value: user_id}` |
| `wallet_quota` | 用户存在 | `{kind: finite, value: wallet.quota}` |
| `subscription_remaining` | 存在无限订阅 | `{kind: infinite}` |
| `subscription_remaining` | 仅有限订阅或无订阅 | `{kind: finite, value: finite_remaining}` |
| 任意 | `not_found` 或 `error` | `{kind: unknown}` |

排序规则：

1. `unknown` 始终置底。
2. 订阅降序时 `infinite` 在 `finite` 前，升序时在 `finite` 后。
3. `finite` 按值升降序。
4. 同类同值、两个无限或两个未知时按 `user_id desc`。

## 8. 错误、兼容性与回滚

- 请求非法时不查询数据库；批量查询失败时整个接口返回数据库错误并记录脱敏、带请求 ID 的日志。
- 单用户套餐缺失、投影失败、负数异常或加法溢出返回 `status = error` 和稳定机器错误码，其他用户继续返回。
- 只新增端点和 DTO，不改变现有用户、钱包、订阅或角色管理接口。
- 使用 GORM 通用查询和内存聚合，兼容 SQLite、MySQL 5.7.8+、PostgreSQL 9.6+。
- 回滚时删除新增路由与文件；共享周期投影若引发回归，恢复原预消费实现后重新设计复用。无数据迁移需要回滚。

## 9. Build 分支文件映射

新增独立文件承载本需求主体，降低后续同步上游时的冲突面：

- `dto/user_billing_summary.go`：请求与响应 DTO。
- `model/user_billing_summary.go`：三类最小批量读取。
- `model/subscription_cycle_projection.go`：共享订阅周期只读投影。
- `service/user_billing_summary.go`：校验、聚合与统一排序。
- `controller/user_billing_summary.go`：薄 Controller。
- 对应测试均放在同名新增测试文件中。

仅对现有核心文件做最小接入：

- `model/subscription.go`：原懒重置写路径改为调用共享纯投影，保存语义不变。
- `router/api-router.go`：管理员用户路由组新增一行静态路由注册。
