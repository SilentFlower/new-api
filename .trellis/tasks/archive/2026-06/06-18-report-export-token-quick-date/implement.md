# 报表导出支持令牌筛选和快捷时间 - 实现计划

## 实现步骤

1. 阅读开发规范：
   - `.trellis/spec/backend/quality-guidelines.md`
   - `.trellis/spec/backend/directory-structure.md`
   - `.trellis/spec/backend/database-guidelines.md`
   - `.trellis/spec/backend/error-handling.md`
2. 后端模型层：
   - 将 `model/log.go` 中导出查询相关方法的 `tokenName string` 参数调整为 `tokenNames []string`。
   - 对非空切片使用 `token_name IN ?` 过滤。
   - 保持用户名过滤和时间过滤逻辑不变。
3. 后端控制器：
   - 在 `controller/usedata.go` 中为导出接口解析 `token_names` 多值参数。
   - 保留旧 `token_name` 参数并归一化为同一切片。
   - 将归一化后的令牌名称传给三个导出查询。
4. 经典前端导出弹窗：
   - 在 `ExportModal.jsx` 新增令牌名称多选下拉。
   - 导出弹窗打开时加载令牌选项，但不从搜索条件带入已选令牌。
   - `onConfirm` 将选中的令牌值传回 `exportExcel`。
5. 经典前端数据 hook：
   - 调整 `showExportModal` 或弹窗打开流程，确保令牌选项可用。
   - 调整 `exportExcel` 参数，发送 `token_names` 多值查询参数。
6. 经典前端搜索弹窗：
   - 添加 5 个快速查询标签。
   - 新增或复用时间工具函数，计算本地自然日、周、月范围。
   - 周范围固定按周一到周日计算。
7. i18n：
   - 若新增中文文案未被现有 classic i18n 捕获，需要补充 classic 相关语言文件或遵循现有自动兜底模式。
8. 验证：
   - 运行后端相关测试。
   - 运行经典前端构建或至少相关 lint/类型检查脚本；若环境缺失，记录原因。
   - 手工检查导出参数和快捷时间计算边界。

## 重点文件

- `controller/usedata.go`
- `model/log.go`
- `web/classic/src/hooks/dashboard/useDashboardData.js`
- `web/classic/src/components/dashboard/modals/ExportModal.jsx`
- `web/classic/src/components/dashboard/modals/SearchModal.jsx`
- `web/classic/src/helpers/dashboard.jsx`

## 验证命令

```bash
go test ./model ./controller
```

```bash
cd web/classic && npm run build
```

如果 classic 前端使用的脚本不同，以 `web/classic/package.json` 为准。

## 回滚点

- 后端模型层方法签名变更会影响所有调用方，提交前需用 `rg "GetLogSummaryByKey|GetLogDetailByKeyModel|GetLogsForExport"` 确认调用点。
- 前端导出参数序列化需确认 axios 是否按数组生成后端可解析的查询参数；必要时改为显式 URLSearchParams。
