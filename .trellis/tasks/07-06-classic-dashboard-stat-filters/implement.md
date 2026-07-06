# 旧版数据看板统计筛选升级实现计划

## 实现步骤

1. 后端参数归一化
   - 在 `controller/usedata.go` 增加通用多值查询参数解析 helper。
   - 将现有 `parseExportTokenNames` 替换为通用 helper，保留旧参数兼容。

2. 后端 Model 查询
   - `model/token.go` 的令牌选项 DTO 补充 `Group` 字段。
   - `model/log.go` 增加 tokenNames/groups 过滤 helper，并应用到统计和导出查询。
   - `model/usedata.go` 增加从 `logs` 表按多条件聚合 quota 数据的路径。
   - `model/usedata_rankings.go` 增加管理员用户排行/趋势的多条件聚合路径。

3. 后端 Controller 接入
   - `/api/data/`、`/api/data/users` 读取 `groups` / `token_names`。
   - `/api/data/self` 读取 `token_names`。
   - `/api/log/stat`、`/api/log/self/stat` 读取多值参数并传到 Model。
   - `/api/data/export` 读取 `groups` 并传入三个导出查询。

4. 旧版 UI 数据看板
   - `useDashboardData.js` 增加 groupOptions、groups、token_names 状态。
   - 加载 `/api/group/` 和 `/api/data/token-names`，并基于分组联动过滤令牌选项。
   - 搜索请求和导出请求使用 `URLSearchParams.append` 发送重复 key。
   - `SearchModal.jsx` 增加管理员分组多选，令牌改多选。
   - `ExportModal.jsx` 增加分组多选，并让令牌选项随分组联动。

5. 测试与验证
   - 增加 Go 单测覆盖多值参数归一化。
   - 增加 Model 查询单测覆盖 tokenNames/groups 的交集过滤。
   - 运行目标 Go 测试和旧版前端构建。

## 验证命令

```bash
go test ./controller ./model
cd web/classic && bun run build
```

## 重点回归点

- 未选择分组和令牌时，旧看板数据仍可加载。
- 分组和令牌同时选择时，查询结果为交集。
- 导出三个 Sheet 条件一致。
- 旧单值参数 `group`、`token_name` 仍被识别。
