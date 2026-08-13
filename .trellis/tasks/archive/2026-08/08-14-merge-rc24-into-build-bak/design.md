# 合并 v1.0.0-rc.24 到 build-bak 技术设计

## 设计目标

使用一次真实、可审计的非快进合并吸收 `v1.0.0-rc.24`，以 rc.24 的目录和核心架构为基线，将 `build-bak` 的定制功能按业务契约重新接入。合并过程必须可中止、可恢复、可逐项证明功能没有静默消失。

## 总体策略

### 1. 合并边界

- 合并对象固定为 `5c3abffe8572aa8a49f15c3916707d2019d66af4`，不使用会继续移动的分支名。
- 合并前校验当前分支、起点提交、工作树和标签对象，并创建指向起点的本地备份分支。
- 使用 `git merge --no-ff --no-commit <rc24-commit>` 进入可验证的合并状态。
- 冲突解决和必要兼容修复先留在 merge index 中；验证完成后由 Trellis 提交门禁完成一个 merge commit。

### 2. 结构优先、能力回填

- 接受 rc.24 的 `web/default/ -> web/` 扁平化和 `web/classic/` 删除。
- 接受 rc.24 的认证会话、RelayKit、计费和 HTTP transport 新结构。
- 对 `build-bak` 独有能力，不恢复旧目录或旧调用层级，而是在新结构的稳定入口重新挂接。
- 上游核心文件只保留必要薄接入；可独立的定制逻辑继续放在独立文件，降低下次上游合并冲突。

### 3. 冲突台账

实际合并后建立冲突台账，至少记录：

| 字段 | 含义 |
|---|---|
| 路径与冲突类型 | Git 冲突路径、UU/UD/AU/AA 分类 |
| 双方意图 | rc.24 改动目的与 build-bak 定制目的 |
| 处理策略 | 采用上游结构、保留本地能力或语义组合 |
| 功能契约 | 受影响的 API、数据、计费、UI 或运行时行为 |
| 验证证据 | 对应测试、构建、静态检查或人工核对结果 |

台账用于防止“文件已解决但能力已丢失”，并作为最终合并报告的依据。

## 冲突处理分区

### A. 前端结构迁移

rc.24 的 `web/` 作为唯一前端基线。处理方式不是保留两个目录，而是：

1. 接受上游 `web/` 根目录、依赖、构建脚本和路由结构。
2. 对照 `build-bak` 的 `web/default/` 差异和历史迁移任务，逐项迁移 API Keys、Dashboard、公共日志、渠道配置和其他定制页面能力。
3. 删除 Classic 及其 Semi Design 依赖，不把 Classic 视觉实现迁移到新前端。
4. 重新生成路由和同步六种语言，确保只有 `web/` 参与构建。

### B. 认证与会话

以 rc.24 的 JWT/会话控制为主链路，同时保留兼容分支：

```text
ai-fund / 浏览器请求
  -> Authorization Bearer
  -> rc.24 JWT/会话校验
  -> 不匹配时进入 Personal Access Token ValidateAccessToken 回退
  -> 写入统一 Gin 用户上下文
```

- `New-Api-User` 不再作为 PAT 成功的前置条件；旧客户端继续携带时应被兼容处理。
- API Key 日志继续走 `TokenAuthReadOnly`，保持只读查询和安全字段边界。
- 认证冲突需联动检查 `model/user.go`、`middleware/auth.go`、路由和前端登录/session store，避免只修中间件而遗漏 token 创建、撤销或会话失效。

### C. RelayKit、provider 与协议

- 先完成 RelayKit 公共类型、请求转换、错误和响应接口的结构合并，再接各 provider。
- 新通道按 rc.24 接口落地；`build-bak` 的 Alpha Search、Compact、WebSocket、视觉辅助和 WebSearch 模拟在新接口上恢复。
- 所有从客户端解析并转发的可选标量继续使用指针与 `omitempty`，显式 `0`、`0.0`、`false` 不能丢失。

### D. 计费与模型映射

计费按一条完整链路合并：

```text
请求校验
  -> 模型映射与计费模型冻结
  -> rc.24 分层计费/用户组规则
  -> 预扣费
  -> RelayKit/provider 执行与重试
  -> 实际 usage 结算或退款
  -> 消费日志与异常审计
```

- `build-bak` 的映射后上游模型计费必须与 rc.24 的 tiered billing snapshot 同时成立。
- 表达式计费遵循 `pkg/billingexpr/expr.md` 的“一个表达式、一个事实”和 token 归一化规则。
- 所有 quota 转换继续使用 `common/quota_math.go` 的 checked helper，并把饱和信息写入日志审计。
- 重试必须清理请求级临时计费状态，但不能丢失已经冻结的结算契约或产生重复扣费。

### E. HTTP transport 与请求体重放

- 接受 rc.24 的 HTTP/2 和 transport 管理方式。
- 将 `build-bak` 重试、视觉辅助、WebSearch 等读取请求体的逻辑统一建立在 `GetBody` 或等价可重放机制上。
- 每次重试使用新的 body reader；响应、连接和 keepalive writer 均按既有生命周期关闭。
- 非流式 JSON keepalive 仅在允许的响应类型启用，不影响状态码、错误体、重试判定和最终 JSON。

### F. 数据库、配置与生成文件

- model 和 migration 冲突优先采用 rc.24 schema，再回填定制字段、索引、默认值和兼容逻辑。
- 所有数据库改动使用 GORM 或已有跨方言 helper，兼容 SQLite、MySQL、PostgreSQL。
- 配置项、环境变量、路由树、前端 lockfile、Go 依赖和 i18n 产物必须与最终代码一致，不手工保留失效生成文件。

## 定制功能保留映射

| 能力 | 主要边界 | 合并重点 |
|---|---|---|
| 消息审计与 AI 审核 | controller/service/model/relay | 保留清空、重审、Tool 降级和上游错误边界 |
| GitHub 密钥泄漏扫描 | controller/service/model | 保留任务互斥、HMAC 锚点、通知幂等和脱敏 |
| 通道级用户并发限制 | middleware/relay/task/ws | 保留 Redis/内存租约、取消传播和错误隔离 |
| Responses Compact/WS | router/relay/provider | 适配 RelayKit 新接口并保持透传、计费隔离 |
| Alpha Search | router/relay | 保持 standalone 路径与原始响应契约 |
| 视觉辅助 | relay/provider | 保持辅助请求、端点模式和重试 body 稳定性 |
| Claude WebSearch 模拟 | relay/provider | 保持纯 WebSearch 识别、provider 与密钥脱敏 |
| 映射后模型计费 | relay/helper/service/log | 与 tiered snapshot、重试和日志统一 |
| 公共 API Key 日志 | router/middleware/controller/web | 保留只读鉴权、脱敏列和 `/log` 页面 |
| Excel 导出与筛选 | controller/model/web | 保留重复 query key、时间/分组/token 条件和下载 |
| Token 迁移 | controller/model/web | 保留 Root 权限、逐项结果和刷新选择状态 |
| User-Agent 日志 | middleware/service/model/web | 保留采集、存储、查询和展示 |
| 非流式 JSON keepalive | relay/service | 保持允许列表、writer 生命周期和响应语义 |

## 验证设计

### Git 与范围

- 校验 merge parent、标签祖先关系和 rc.24 之后提交未被引入。
- `git ls-files -u` 必须为空；扫描标准冲突标记。
- 对删除和重命名执行统计复核，重点检查 `web/classic/`、`web/default/` 与新 `web/`。

### 后端

- 对变更 Go 文件执行 gofmt 和 `git diff --check`。
- 运行认证、model、controller、service、relay、relaykit 的定向测试，再运行 `go test ./...` 与 `go vet ./...`。
- 对并发租约、keepalive 或共享 transport 的受影响包执行可行的 race 测试。
- 缺失关键回归覆盖时，新增针对可观察契约的测试，不添加只锁定内部实现的测试。

### 前端

- 在新 `web/` 目录使用 Bun 安装锁定依赖。
- 运行 i18n 同步、类型检查、lint、格式检查和生产构建。
- 核对 API Keys 迁移、Dashboard 筛选/导出、公共 `/log`、渠道定制字段和响应式布局。

### ai-fund 兼容

- 在 `new-api` 侧用测试或本地请求证明 JWT 与 PAT 回退均可用。
- 证明 API Key 可调用只读日志接口，旧 `New-Api-User` header 不会导致拒绝。
- 如环境具备现有凭据，仅执行只读联调；不得打印或写入密钥。

## 回滚与恢复

- 合并提交前可用 `git merge --abort` 回到起点；本地备份分支作为二次恢复点。
- 不使用 `git reset --hard`、强推或批量覆盖工作树。
- 若某一分区验证失败，先回到该分区的冲突台账和三方差异修正，不通过关闭功能绕过验收。
- merge commit 完成后如需回退，使用显式 revert merge，并由用户单独确认。

## 关键取舍

- 正式移除 Classic，换取与 rc.24 一致的单前端结构和更低的后续同步成本。
- 保留一个原子 merge commit，换取完整 Git 历史和可审计的双方父提交；不把冲突解决伪装为若干普通 feature commit。
- 不机械追求减少 diff；优先保证上游结构、定制功能契约和计费/认证安全同时成立。
