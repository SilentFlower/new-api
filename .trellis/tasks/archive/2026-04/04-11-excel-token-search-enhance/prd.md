# Excel导出优化与管理员数据看板令牌搜索增强

## Goal

1. 优化数据报表 Excel 导出体验（列宽自适应 + 列名修正）
2. 增强管理员在数据看板中的令牌搜索能力（可选择所有用户的令牌 + 看板数据不限于当前账户）

## What I already know

### 需求1：Excel 导出优化

- **代码位置**: `controller/usedata.go` — `ExportQuotaDataExcel()` (L37-198)
- **库**: `github.com/xuri/excelize/v2` v2.9.0
- **当前列宽**: 未设置任何自定义列宽，使用 excelize 默认值（约 8.43 字符宽）
- **三个 Sheet**:
  - "汇总统计" — 列: API Key 名称, 请求次数, 请求 Token 数, 请求额度
  - "模型明细" — 列: 模型名称, 请求次数, 请求 Token 数, 请求额度
  - "请求日志" — 列: 时间, API Key, 模型, **提示 Tokens**, **完成 Tokens**, 额度消耗, 耗时(s), 是否流式, 渠道 ID, 请求 ID
- **用户期望**: "提示 Tokens" → "输入 Tokens"，"完成 Tokens" → "输出 Tokens"
- **列宽问题**: 因无自定义列宽，时间、请求ID等长内容列显示不全

### 需求2：管理员数据看板令牌搜索增强

- **搜索弹框**: `web/src/components/dashboard/modals/SearchModal.jsx` — 令牌名称是 `Form.Select` 下拉选择器 (L101-111)
- **令牌加载**: `web/src/hooks/dashboard/useDashboardData.js` (L156-170) — 调用 `GET /api/token/?p=1&size=100`
- **后端令牌接口**: `controller/token.go` — `GetAllTokens()` 使用 `model.GetAllUserTokens(userId, ...)` — **只返回当前用户的令牌**
- **看板数据接口**:
  - 管理员: `GET /api/data/` → `GetAllQuotaDates()` — 已支持全局查询
  - 普通用户: `GET /api/data/self/` → `GetUserQuotaDates()` — 限定当前 userId
- **核心问题**: 令牌下拉框只加载了当前用户的令牌，管理员无法选择其他用户的令牌

## Requirements

### R1: Excel 列宽自适应
- 为三个 Sheet 的所有列设置合理的固定列宽
- 使用 `excelize.SetColWidth()` 根据列内容类型设定宽度

### R2: 请求日志 Sheet 列名修改
- "提示 Tokens" → "输入 Tokens"
- "完成 Tokens" → "输出 Tokens"

### R3: 管理员数据看板令牌下拉增强
- 管理员打开搜索弹框时，令牌下拉应显示系统内**所有用户**的令牌
- 普通用户保持不变，只显示自己的令牌
- 需要后端新增接口或修改现有接口支持管理员获取所有令牌名称

### R4: 管理员看板数据全局查看
- 确认管理员查看看板数据时不限于当前账户（后端已支持，需确认前端是否正确调用 admin 接口）

## Acceptance Criteria

- [ ] 导出 Excel 后，所有列内容可完整显示，无截断
- [ ] 请求日志 Sheet 列名显示 "输入 Tokens" 和 "输出 Tokens"
- [ ] 管理员在数据看板搜索弹框中可看到所有用户的令牌列表
- [ ] 普通用户在数据看板搜索弹框中只看到自己的令牌
- [ ] 管理员查看看板数据时展示全局数据

## Definition of Done

- Go 代码通过编译
- 前端 bun run build 无报错
- Excel 导出、看板搜索功能正常工作

## Out of Scope

- Excel 导出性能优化
- 新增 Sheet 或列
- 非管理员用户的搜索增强
- 日志页面的令牌搜索修改

## Technical Notes

### 需求1 涉及文件
- `controller/usedata.go` — Excel 生成逻辑 (L37-198)
- excelize v2 API: `SetColWidth(sheet, startCol, endCol, width)`

### 需求2 涉及文件
- **前端**:
  - `web/src/components/dashboard/modals/SearchModal.jsx` — 搜索弹框 (L101-111)
  - `web/src/hooks/dashboard/useDashboardData.js` — 令牌加载 (L156-170)、数据获取 (L182-217)
- **后端**:
  - `controller/token.go` — `GetAllTokens()` 当前只返回当前用户令牌
  - `router/api-router.go` — 需要新增管理员令牌列表接口或扩展现有接口
  - `model/token.go` — 需要新增获取所有令牌名称的查询方法
