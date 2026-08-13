# 合并 v1.0.0-rc.24 到 build-bak 实施计划

## 前置条件

- 任务 Brief 已展示并由用户在后续消息明确批准。
- 通过 `task.py start` 将任务切换为 `in_progress`，再进入 `trellis-route(target=implement)`。
- 当前分支仍为 `build-bak`，起点仍为规划基线或已重新记录的新基线。
- 除本任务规划文件外不存在无法归属的未提交修改。

## 实施顺序

### 1. 合并前快照与范围校验

- 获取最新远端 tag 引用并验证 `v1.0.0-rc.24` 仍解析到 `5c3abffe8572aa8a49f15c3916707d2019d66af4`。
- 记录当前 `build-bak` HEAD、merge-base、双方独有提交数和 diff 统计。
- 创建明确命名的本地备份分支，确认它精确指向合并前 HEAD。
- 记录 rc.24 后续提交集合，供最终范围审计排除。

退出条件：来源、目标和恢复点均可通过 Git 命令复核。

### 2. 启动真实合并并建立台账

- 执行非快进、暂不提交的精确提交合并。
- 导出所有未合并路径及 UU/UD/AU/AA 分类。
- 按前端、认证、RelayKit/provider、计费、HTTP、数据库/配置、其他定制能力建立冲突台账。
- 先处理纯结构、依赖和生成文件冲突，但暂不覆盖业务冲突。

退出条件：每个冲突路径都有归属分区和预定处理策略。

### 3. 前端目录结构落地

- 接受 rc.24 的 `web/` 根结构和 Classic 删除。
- 清除 `web/default/`、workspace catalog 或旧构建脚本的失效入口。
- 暂不声明功能迁移完成，只建立后续回填的唯一新前端基线。

退出条件：前端只有一个构建入口，目录不存在双重来源。

### 4. 认证、会话与 ai-fund 兼容

- 合并 user/session model、认证中间件、路由和前端 session store。
- 保留 JWT 主链路和 Personal Access Token 回退。
- 保留 `TokenAuthReadOnly` 的 API Key 日志能力，并验证 `New-Api-User` 的兼容处理。
- 补齐或修复 JWT、PAT、API Key 只读接口的契约测试。

退出条件：三条认证路径分别有自动化或可重复验证证据。

### 5. RelayKit、provider 与协议迁移

- 先解决 RelayKit 公共接口、类型、错误和响应转换冲突。
- 接入 rc.24 新增/调整通道。
- 按规范恢复 Alpha Search、Responses Compact V1/V2/WebSocket、视觉辅助和 Claude WebSearch 模拟。
- 逐项检查可选标量、显式零值、stream options、错误映射和敏感信息边界。

退出条件：相关 provider 编译通过，定制协议测试通过，台账记录新入口。

### 6. 计费、模型映射和 HTTP 重试

- 合并 tiered billing、用户组规则、预扣与结算链路。
- 将映射后上游模型计费接入统一 billing snapshot 和日志生成。
- 合并 HTTP/2、transport、`GetBody`/body replay、provider retry 与非流式 JSON keepalive。
- 核对重试前后计费状态、quota saturation、退款和日志只发生一次且数据一致。

退出条件：计费链路测试、请求体重放测试和 keepalive 回归通过。

### 7. 其他 build-bak 后端定制能力

- 恢复消息审计与 AI 审核。
- 恢复 GitHub 密钥泄漏扫描。
- 恢复通道级用户并发限制及 HTTP/WebSocket/任务取消传播。
- 恢复公共 API Key 日志、Excel 数据接口、Token 迁移、User-Agent 日志。
- 对照各专项 Trellis spec 更新台账，确认没有只保留 UI 或只保留 API 的半链路状态。

退出条件：PRD R4 的每项能力均有代码入口和验证项。

### 8. Default 定制功能迁移到新 web

- 将 API Keys 批量迁移入口迁到 `web/`。
- 将 Dashboard 分组/token 筛选和 Excel 导出迁到 `web/`。
- 将公共 `/log`、公共 API Key client 和安全列定义迁到 `web/`。
- 恢复渠道 Vision Assist、WebSearch、上游模型计费等配置字段的读写闭环。
- 复用 rc.24 Base UI/shadcn 组件，不引入 Classic/Semi Design。

退出条件：关键页面完成编译和交互核对，Classic 无残留运行依赖。

### 9. 依赖、迁移、路由和 i18n 收口

- 统一 Go module、Bun lockfile、前端 package 配置和构建脚本。
- 核对数据库 migration 和 model 字段在 SQLite、MySQL、PostgreSQL 的兼容写法。
- 重新生成或校验前端 route tree。
- 运行 i18n 同步并补齐 en、zh、fr、ru、ja、vi。
- 对所有变更 Go 文件执行 gofmt，检查受保护信息未被更改。

退出条件：生成文件与源码一致，依赖和翻译没有漂移。

### 10. 全量验证与功能审计

- 清理所有冲突标记、未合并 index 和无意删除。
- 执行后端定向测试、全量测试、vet 和可行的 race 测试。
- 执行前端 typecheck、lint、format check、i18n sync 和生产构建。
- 逐项完成定制功能保留矩阵，静态检查 `ai-fund` 调用契约，并在凭据可用时做只读联调。
- 审计 merge parents、tag 祖先关系和 rc.24 后续提交未被带入。
- 生成最终合并报告，进入 `trellis-check-all`，修复后重新验证。

退出条件：所有验收标准完成，或每个未完成项都有明确阻塞与风险说明。

## 主要验证命令

具体命令以合并后的目录和脚本为准，预期至少包括：

```bash
git ls-files -u
git diff --check
rg -n '^(<{7}|={7}|>{7})( |$)' .
go test ./middleware ./model ./controller ./service ./relay/... ./relaykit/...
go test ./...
go vet ./...
```

前端目录完成迁移后：

```bash
cd web
bun install --frozen-lockfile
bun run i18n:sync
bun run typecheck
bun run lint
bun run format:check
bun run build
```

最终 Git 范围审计：

```bash
git merge-base --is-ancestor 5c3abffe8572aa8a49f15c3916707d2019d66af4 build-bak
git log --oneline --left-right build-bak...5c3abffe8572aa8a49f15c3916707d2019d66af4
```

## 高风险文件与区域

- `middleware/auth.go`、`model/user.go`、认证相关 router/controller/session 文件。
- `relay/`、`relaykit/`、`service/quota.go`、`service/tiered_settle.go`、`service/log_info_generate.go`。
- `common/http_client.go` 及 transport/retry/body replay 相关文件。
- model migration、options、group、token、log 相关文件。
- `web/`、`web/default/`、`web/classic/` 的目录级删除/新增冲突。
- 前端 API Keys、Dashboard、Usage Logs、Channel、route tree 和六种 locale 文件。

## 回滚点

- 启动合并前：备份分支指向原始 `build-bak` HEAD。
- merge commit 前：任何不可控问题通过 `git merge --abort` 恢复，不使用硬重置。
- 每个冲突分区完成后：用台账和定向测试作为逻辑检查点，不创建破坏 merge 原子性的临时普通提交。
- merge commit 后：如必须撤销，单独确认后执行 merge revert。

## 提交策略

- 本次冲突解决和兼容修复形成一个真实 merge commit，保留 rc.24 与 build-bak 两个父提交。
- 不在合并提交中夹带 rc.24 之后的提交或无关重构。
- 提交与推送前展示精确文件范围、验证结果和剩余风险，由用户确认后进入 `trellis-push`。
