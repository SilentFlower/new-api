# GitHub 用户 Key 公开泄露扫描契约

## 1. Scope / Trigger

- Trigger：修改用户 Key 公开泄露扫描、稳定搜索锚点、GitHub Code Search 客户端、`TokenLeak*` 模型、泄露扫描系统任务、站内/钉钉告警、Root 处置 API 或对应管理页面。
- 扫描对象固定为未软删除的 `model.Token.Key`；不得扩展到 `model.Channel.Key`、上游渠道凭据或其他实例密钥。
- 该能力是尽力检测，只覆盖 GitHub 当前可搜索索引中的公开仓库默认分支；不得宣称零漏报或等同于 GitHub Secret Scanning Partner。

## 2. Signatures

环境变量与设置：

```text
GITHUB_TOKEN_LEAK_SCAN_TOKEN          独立 fine-grained PAT
GITHUB_TOKEN_LEAK_SCAN_SECRET         至少 32 字节的 HMAC 密钥
DINGTALK_TOKEN_LEAK_WEBHOOK_TOKEN     可选的固定安全群机器人 token
DINGTALK_TOKEN_LEAK_WEBHOOK_SECRET    可选的机器人签名密钥

token_leak_scan.enabled               bool，默认 false
token_leak_scan.interval_hours        int，范围 1..168，默认 24
```

核心 Service 签名：

```go
func deriveTokenLeakIdentity(secret []byte, tokenKey string) (string, string, error)
func GetTokenLeakScanStatus() (*TokenLeakScanStatus, error)
func ListTokenLeakFindingViews(status string, page int, pageSize int) (*TokenLeakFindingPage, error)
func StartTokenLeakScanTask(tokenID int) (*model.SystemTask, bool, error)
func RunTokenLeakScan(ctx context.Context, tokenID int, progress func(processed, total int)) (TokenLeakScanRunSummary, error)
func DisableTokenLeakFindingToken(findingID int64) (int, int, error)
```

持久化字段与通知事件：

```go
type TokenLeakFinding struct {
	FindingKey  string
	TokenID     int
	Status      string
	ReopenCount int
}

type TokenLeakNotification struct {
	EventKey  string
	FindingID int64
	Channel   string
	Trigger   string
}

// 首次事件：first
// 第 N 次处置后重开：reopened:<N>
// 周期提醒：reminder:<event-seed>
```

Root-only API：

```text
GET  /api/token-leak-scan/status
GET  /api/token-leak-scan/findings
POST /api/token-leak-scan/run
POST /api/token-leak-scan/findings/:id/disable-token
```

系统任务类型：

```text
token_leak_scan
token_leak_scan_manual
```

两个任务类型共享活动键 `token_leak_scan`，任一时刻只能有一个泄露扫描执行者。

## 3. Contracts

- 完整 Key 只允许存在于单条扫描的服务器内存中。GitHub 请求只能发送由域隔离 HMAC 稳定派生的连续 16 位锚点；数据库、日志、任务 payload/state/result、告警和管理 API 均不得保存完整 Key 或锚点。
- 指纹和锚点必须使用不同 HMAC 域标签。同一扫描密钥下，同一 Key 的锚点稳定；轮换扫描密钥可以改变指纹和锚点，但不能改变 finding 的位置幂等键。
- Code Search 查询不得包含 `repo:`、`user:` 或 `org:` 范围限定。`repository.private=true` 的结果必须丢弃；可见性缺失、`incomplete_results=true` 或结果截断必须把本次扫描标记为不完整。
- 每分钟最多发起 8 个 Code Search HTTP 请求，分页请求计入同一预算。搜索、候选下载、重试等待和功能关闭检查必须响应 `context.Context`。
- 只有下载公开候选后在本地完整 Key 精确命中，才能创建或更新 finding。单独命中 16 位锚点不构成泄露。
- finding 以 token、仓库和路径的稳定摘要做唯一键。同一路径重复扫描只更新最近确认信息；搜索未命中不得自动把已有 finding 标记为已处置。
- 令牌禁用或删除后，开放 finding 转为 `mitigated` 并停止提醒。已处置 finding 在令牌重新启用且再次精确命中时转回 `open`，同时持久化 `ReopenCount + 1`。
- 通知幂等键必须区分“业务对象”和“业务事件代次”。固定 trigger `reopened` 只能代表第一次重开，不能用于可重复发生的状态转换；每次重开必须使用 `reopened:<ReopenCount>`。
- finding 状态更新必须先持久化重开代次，再发送通知。进程在两步之间退出时，后续扫描必须从 finding 的 `ReopenCount` 恢复同一事件；成功或达到重试上限的同代次通知不得重复发送。
- 历史记录新增 `reopen_count` 后的 `NULL`/零值按 `0` 处理。旧通知 trigger `reopened` 仍需兼容读取；首次新代次从 `reopened:1` 开始，不能重发旧事件。
- 站内通知同时面向 root 与令牌所属用户。普通用户的处置入口固定为 `/keys`；root 入口为 `/security-alerts/token-leaks`。消息只包含 token ID/名称、用户 ID、公开位置、时间和处置链接。
- 钉钉 Webhook URL、token、签名密钥、请求正文和响应正文不得进入日志或通知审计。通知审计只保存渠道、trigger、状态、次数和稳定脱敏错误码。
- 一键禁用只允许 Root 调用，必须经过前端二次确认；后端在事务中禁用 token 并处置其全部开放 finding，再同步缓存并记录不含明文的 `token_leak.disable` 管理审计。

## 4. Validation & Error Matrix

| 条件 | 行为 |
| --- | --- |
| GitHub PAT 未配置 | 返回 `github_token_missing`，不得创建或执行扫描任务 |
| HMAC 密钥不足 32 字节 | 返回 `scan_secret_invalid`，不得计算指纹或锚点 |
| Token Key 长度不足以派生 16 位窗口 | 返回 `token_key_invalid`，本条扫描失败且不输出 Key |
| 功能在任务开始或请求间被关闭 | 返回 `token_leak_scan_disabled`，停止后续外部请求和新增扫描结果 |
| GitHub 返回 401/403 鉴权失败 | 记录 `auth_failed` 并结束本轮，不保存响应正文或请求 URL |
| GitHub 限流或可恢复 5xx | 有限重试并响应取消；最终只记录稳定错误码 |
| 私有仓库候选 | 丢弃，不下载、不创建 finding、不通知 |
| 仓库可见性缺失、搜索不完整或超过 1000 条 | 标记扫描不完整，不得报告为完整未命中 |
| 锚点命中但完整 Key 不命中 | 拒绝误报，不创建 finding |
| 同一开放 finding 再次精确命中 | 更新最近确认；复用当前事件键，不新增通知记录 |
| 已处置 finding 第一次重开 | `ReopenCount=1`，使用 `reopened:1` 告警 |
| 同一 finding 再次处置后第二次重开 | `ReopenCount=2`，使用 `reopened:2` 告警 |
| 重开状态已写入但通知中断 | 下次扫描按当前 `ReopenCount` 恢复该代次通知 |
| 历史 finding 的 `reopen_count` 为 `NULL`/0 | 按 0 处理，并兼容已有 `first`/`reopened` 通知 |
| 普通用户访问扫描 API | `RootAuth` 拒绝，不返回 finding 或凭据状态 |

## 5. Good / Base / Bad Cases

- Good：finding 经两次“禁用 -> 重新启用 -> 再次精确命中”，分别产生 `reopened:1` 和 `reopened:2`；每个代次的重复扫描均不新增通知。
- Good：进程在写入 `ReopenCount=1` 后、创建用户通知前退出，下一轮扫描仍恢复 `reopened:1`，不会退回 `first` 或跳过告警。
- Good：GitHub 返回公开与私有候选混合结果，只下载公开候选，并把私有候选计入内部统计但不作为泄露结果。
- Base：GitHub 当前索引未命中锚点，记录本轮未命中与覆盖边界，不自动解除历史 finding。
- Base：历史 finding 没有重开代次，读取为 0；存在旧 trigger `reopened` 时继续复用旧事件，下一次真实重开进入新代次。
- Bad：把固定 `reopened` 作为所有重开事件的 trigger，导致第一次成功后永久抑制后续重开告警。
- Bad：用当前秒级时间戳作为重开事件唯一代次，同一秒内重复状态转换可能冲突，且进程中断后难以从持久化状态恢复。
- Bad：把完整 Key、16 位锚点或含 Webhook token 的 URL写入任务结果、错误日志或通知审计。

## 6. Tests Required

- 身份测试：断言 HMAC 域隔离、稳定 16 位窗口、密钥轮换、短 Key/短密钥错误，并确认输出不包含完整 Key。
- GitHub 客户端测试：断言每分钟 8 次请求预算、分页计数、1000 条截断、`incomplete_results`、私仓拒绝、可见性缺失、有界读取、取消和脱敏错误分类。
- 精确比对测试：裸 Key、`sk-` 前缀和客户端后缀均能命中；仅末位不同或仅锚点相同不得创建 finding。
- 状态与通知测试：同路径首次扫描幂等；第一次重开产生 `reopened:1`；重复扫描不重发；第二次重开产生 `reopened:2`；通知中断后按当前代次恢复。
- 历史兼容测试：`reopen_count` 为零值且存在旧 `first`/`reopened` 审计时不重复发送；首次新重开升级为 `reopened:1`。
- 数据库测试：SQLite 实际执行 AutoMigrate、查询和状态转换；MySQL/PostgreSQL 对字段类型、唯一索引和通用 GORM 查询做静态或隔离实例验证。
- API/权限测试：Root 可读取状态/列表和触发任务；非 Root 被拒绝；凭据只返回布尔状态；非法 token/finding ID 和状态过滤被拒绝。
- 前端测试：轮询只覆盖 pending/running、Token ID 和周期边界严格校验、Root 导航可见、普通用户入口隐藏、禁用确认和通知展示正确。
- 回归命令：
  - `go test ./... -count=1`
  - `go test -race ./model ./service -run 'TokenLeak' -count=1`
  - `go vet ./model ./service ./controller ./router`
  - `cd web/default && bun test src/features/token-leak-scan src/hooks/use-sidebar-view.test.ts`
  - `cd web/default && bun run typecheck && bun run build`
  - 对本任务前端文件执行定向 oxlint/oxfmt，并运行 `git diff --check`

## 7. Wrong vs Correct

#### Wrong：用固定 trigger 表示可重复发生的重开事件

```go
if finding.Status == TokenLeakFindingStatusMitigated && token.Status == TokenStatusEnabled {
	updates["status"] = TokenLeakFindingStatusOpen
	trigger = "reopened"
}
```

第一次重开成功后，后续重开会命中相同 `(finding, channel, trigger)` 幂等键并被永久跳过。

#### Correct：持久化业务事件代次并从状态恢复

```go
if finding.Status == TokenLeakFindingStatusMitigated && token.Status == TokenStatusEnabled {
	reopenCount := finding.ReopenCount + 1
	updates["status"] = TokenLeakFindingStatusOpen
	updates["reopen_count"] = reopenCount
	trigger = "reopened:" + strconv.Itoa(reopenCount)
}

if trigger == "" && finding.ReopenCount > 0 {
	trigger = "reopened:" + strconv.Itoa(finding.ReopenCount)
}
```

持久化代次同时解决多轮重开区分和通知中断恢复；同代次重复扫描仍由事件键幂等收敛。
