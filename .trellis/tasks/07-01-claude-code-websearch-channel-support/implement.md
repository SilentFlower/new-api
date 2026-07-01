# Claude Code WebSearch 渠道支持实施计划

## 实施顺序

1. 后端配置 DTO
   - 在 `dto/channel_settings.go` 增加 `ChannelWebSearchSettings`，并挂到 `ChannelSettings.WebSearch`。
   - 使用中文 Javadoc 注释说明结构体和导出方法用途。
   - 增加 provider 常量：`tavily`、`anysearch`。
   - 增加配置规范化方法，统一默认值、最大结果数范围、provider 校验和 key 状态判断。

2. 管理 API 脱敏与 key 保留
   - 在 `controller/channel.go` 增加渠道响应脱敏辅助函数，只处理 API 响应副本，不修改数据库模型。
   - `GetAllChannels`、`GetChannel` 返回前移除 `setting.web_search.api_key`，填充 `api_key_configured`。
   - `AddChannel` 校验启用 WebSearch 时 provider 可用，并按供应商校验 API Key：Tavily 必填，AnySearch 可选。
   - `UpdateChannel` 在读取 `originChannel` 后合并 WebSearch key：空 key + 未显式清空时沿用旧 key，`clear_api_key=true` 时清空。
   - 保持 `CopyChannel` 读取完整原渠道并浅拷贝，不对 `Setting` 做脱敏或重写。

3. Provider 包
   - 新增 `relay/websearch` 包。
   - 定义 `Provider`、`SearchRequest`、`SearchResponse`、`SearchResult`。
   - 实现 Tavily provider，测试用 `httptest.Server` 覆盖成功、HTTP 错误、非法 JSON。
   - 实现 AnySearch provider，按 JSON-RPC `tools/call` 调用 `search`，测试覆盖带/不带 Authorization、结果规范化和错误响应。
   - 所有 JSON 编解码走 `common.*`，响应体读取使用大小上限。

4. Claude 纯 WebSearch 识别与查询提取
   - 在 `relay/websearch` 或 `relay` 侧增加纯 WebSearch 判断函数。
   - 支持工具 type/name：`web_search`、`web_search_20250305`、`google_search`。
   - 从最后一条 `role=user` 消息提取文本；支持字符串 content 和 Claude 多段 content 中的 text 块。
   - 单测覆盖：无 tools、多个 tools、普通工具、单个搜索工具、文本提取失败。

5. Claude relay 短路
   - 在 `relay/claude_handler.go` 上游请求体构造前插入短路逻辑。
   - 非纯 WebSearch 保持现有路径不变。
   - 纯 WebSearch 且未启用 / 配置错误 / 查询为空时返回不可重试的 Claude relay 错误。
   - 成功时调用 provider，构造 Claude 响应，写回客户端。
   - 设置 `claude_web_search_requests=1`，构造最小 `dto.Usage` 并调用 `service.PostTextConsumeQuota`。
   - 确认短路路径不调用 `adaptor.DoRequest`，不生成上游请求体。

6. 前端渠道表单
   - 扩展 `web/default/src/features/channels/types.ts` 的 `ChannelSettings`。
   - 扩展 `channel-form.ts` schema、默认值解析、`buildSettingJSON`。
   - 在 `channel-mutate-drawer.tsx` 增加 WebSearch 设置区，沿用当前表单控件风格。
   - 编辑模式下 API Key 输入为空表示保留；清空用明确复选项或按钮。
   - 不把后端脱敏状态写回 `api_key`。

7. i18n
   - 为新增 UI 文案补齐 `web/default/src/i18n/locales/{en,zh,fr,ja,ru,vi}.json`。
   - 运行 `cd web/default && bun run i18n:sync`，检查没有缺失 key。

8. 回归测试
   - 后端新增/更新单测后运行相关包测试。
   - 前端运行构建或至少类型检查命令。
   - 检查 git diff，确认没有无关格式化和 protected project 信息改动。

## 关键风险与处理

- 请求体稳定：短路必须发生在上游 body 构造前，并且只对纯 WebSearch 生效。混合工具请求不能被模拟。
- 密钥泄露：脱敏只在响应副本上执行；日志、错误消息、前端状态都不能包含明文 API Key。
- 渠道复制：复制路径不应调用脱敏辅助函数；实现后用测试或手工断言确认新渠道仍有真实 key。
- JSON 包装：新增代码不能直接用 `encoding/json` 做 marshal/unmarshal，必须使用 `common.*`。
- 跨数据库：本任务不改表结构；如新增查询应使用 GORM，避免数据库方言差异。

## 验证命令

实现完成后优先运行：

```bash
go test ./dto ./relay/websearch ./relay ./controller ./service
```

前端相关改动完成后运行：

```bash
cd web/default && bun run i18n:sync
cd web/default && bun run build
```

如果改动触及共享 relay 行为，最后运行：

```bash
go test ./...
```

## 回滚点

- Provider 包是独立新增目录，失败时可单独回滚。
- Relay 短路入口集中在 `relay/claude_handler.go`，出现问题时可关闭渠道配置或回滚该插入点。
- 前端表单只写入 `setting.web_search`，老渠道默认没有该字段，不需要数据库回滚。

## 开始实现前检查

- `prd.md`、`design.md`、`implement.md` 已同步最终范围。
- `implement.jsonl` 和 `check.jsonl` 已替换 seed 示例，包含真实项目规范条目。
- 已生成并展示 `brief.md`。
- 用户确认 planning artifacts 和 brief 后，才运行 `task.py start`。
