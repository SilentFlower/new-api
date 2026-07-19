# VChart 用户排名标签采样研究

## 问题

用户选择“前 20”后，排名图可见用户名明显少于 20；用户确认当前筛选范围内实际活跃用户肯定超过可见数量。

## 本地代码链路

1. `web/default/src/features/dashboard/components/users/user-charts.tsx`
   - `topUserLimit` 会传给 `processUserChartData`。
   - 排名图和趋势图共用固定 `300px / 384px` 高度容器。
2. `web/default/src/features/dashboard/lib/charts.ts`
   - 用户名聚合后执行 `sorted.slice(0, limit)`，数据层会保留最多 N 个用户。
   - 排名图左侧 band 轴只声明 `{ orient: 'left', type: 'band' }`，没有显式关闭标签采样。
3. 项目安装的 `@visactor/vchart` 本地源码
   - `web/node_modules/@visactor/vchart/esm/component/axis/base-axis.js` 将轴组件的 `sampling` 默认值设为 true，除非 spec 显式配置 `sampling: false`。

## 结论

- 当前“前 N”数据截取逻辑本身不会把 20 固定截成 13；更直接的显示缺陷是 band 轴默认采样会省略部分用户名标签。
- 固定高度会进一步触发标签重叠处理，并使 20/50 名条目过密。
- 修复应同时：
  - 在排名图分类轴设置 `sampling: false`。
  - 按实际排名条目数计算画布高度，保证稳定的单行高度。
  - 外层卡片设置最大可视高度并纵向滚动，避免 50 名把整个页面拉长。
  - 暴露实际排名人数并在 UI 中显示，少于 N 时明确是当前范围只有这些活跃用户。

## 验证建议

- 使用包含 60 个唯一用户名的确定性数据调用 `processUserChartData(..., 50)`，断言排名数据为 50 条且实际人数为 50。
- 使用 13 个唯一用户名和 limit 20，断言实际人数为 13，不补零数据。
- 浏览器验证 20/50 名时每个用户名均可通过卡片内部滚动查看，移动端无横向溢出或文本重叠。
