# 数据看板自然周期与用户统计修复设计

## 设计目标

- 保留新 UI 现有滚动时间快捷范围，新增与旧 UI 一致的自然日历范围。
- 修复用户统计筛选弹窗的动态 i18n 键遗漏。
- 保证用户排名前 5/10/20/50 的数据数量与可见条目一致，并在数据不足时展示实际活跃用户数。
- 将 build 分支定制逻辑收敛到独立模块，对现有上游组件保持最薄接入。

## 总体边界

- 本任务只修改 `web/default`，不修改 classic。
- 默认不修改 `/api/data/users` 及后端聚合逻辑；当前证据表明数据处理会保留 N 条，主要缺陷位于 VChart 标签采样和固定高度。
- 不修改 `TIME_RANGE_PRESETS`，不改变 `1/7/14/29 天`滚动时间语义。
- 不引入新的 UI 或日期依赖，复用现有 Button、Label、滚动容器、VChart 和本地时间工具。

## 自然周期模块

### 独立文件

新增 `web/default/src/features/dashboard/lib/calendar-time-ranges.ts`，集中承载：

- `DashboardCalendarRangeId`：`today`、`this_week`、`last_week`、`this_month`、`last_month`。
- 自然周期选项：英文 i18n key、范围标识、建议粒度。
- `getDashboardCalendarTimeRange(rangeId, fromDate)`：返回本地自然日历起止 `Date`。
- `detectDashboardCalendarTimeRange(filters, fromDate)`：判断已应用筛选是否精确匹配某个自然周期。

周范围沿用旧 UI 语义：周一为周起点；所有计算使用浏览器本地时区。

### 筛选弹窗接入

`models-filter-dialog.tsx` 保留现有 Quick Range 区块，新增独立的 Calendar Period 区块：

```text
Quick Range
  1 Day | 7 Days | 14 Days | 29 Days

Calendar Period
  Today | This Week | Last Week | This Month | Last Month
```

状态互斥规则：

- 点击滚动范围：清除自然周期选中态。
- 点击自然周期：清除滚动范围选中态，设置对应起止时间和建议粒度。
- 手动修改起止时间：清除两组快捷选中态。
- 重新打开弹窗：先检测自然周期；若命中则不再把近似 24 小时跨度误判成 `1 Day`，否则沿用现有滚动范围检测。
- 重置：恢复看板偏好中的滚动范围和粒度，自然周期选中态清空。

建议粒度：

- Today → hour
- This Week / Last Week → day
- This Month / Last Month → week

## 用户统计 i18n

补齐动态键：

- `User Analytics Filters`
- `Filter the user analytics view by time range, groups, and API keys.`
- `Calendar Period`
- `This Week`、`Last Week`、`This Month`、`Last Month`（`Today` 已存在时复用）
- 实际活跃用户数量文案，例如 `{{count}} active users`

所有动态配置键登记到 `web/default/src/i18n/static-keys.ts`。Locale 变更严格通过临时 `add-missing-keys.mjs` 和 `bun run i18n:sync` 完成，覆盖 en、zh、fr、ja、ru、vi、zh-TW。

## 用户排名渲染

### 数据契约

扩展 `ProcessedUserChartData`，增加 `rankUserCount`：

- 输入唯一活跃用户数不少于 limit 时，值等于 limit。
- 输入唯一活跃用户数少于 limit 时，值等于实际唯一用户数。
- 不生成零消费占位用户。

`processUserChartData` 继续让排名图和趋势图使用同一个 `topUsers` 集合，保持统计口径一致。

### VChart 配置

- 排名图左侧 band 轴显式设置 `sampling: false`，禁止自动省略用户名。
- 趋势图配置保持不变。
- 排名图实际画布高度按 `rankUserCount` 计算，使用稳定的每用户行高与标题/坐标轴预留空间。
- 外层排名区域设置固定最大可视高度和 `overflow-y-auto`；5/10 名通常无需滚动，20/50 名可在卡片内部滚动查看全部条目。
- 图表标题区显示实际活跃用户数，使少于 N 时的展示原因清晰可见。

建议尺寸约束：

- 最小画布高度保持当前移动端可用高度。
- 每名用户分配约 26-30px 行高。
- 卡片内部最大可视高度约 480px；最终值以桌面/移动端截图验证为准。

## 文件影响

### 新增文件

- `web/default/src/features/dashboard/lib/calendar-time-ranges.ts`
- `web/default/src/features/dashboard/lib/calendar-time-ranges.test.ts`
- 用户排名数据契约测试文件，优先新建 `web/default/src/features/dashboard/lib/charts.test.ts`。

### 必须修改的现有文件

- `web/default/src/features/dashboard/components/models/models-filter-dialog.tsx`
  - 最薄接入自然周期区块与互斥选中态。
- `web/default/src/features/dashboard/lib/charts.ts`
  - 关闭排名轴采样并返回实际排名人数。
- `web/default/src/features/dashboard/components/users/user-charts.tsx`
  - 排名图动态高度、内部滚动和实际人数展示。
- `web/default/src/features/dashboard/types.ts`
  - 增加 `rankUserCount` 类型字段。
- `web/default/src/i18n/static-keys.ts`
  - 登记动态翻译键。
- `web/default/src/i18n/locales/*.json`
  - 通过脚本写入翻译。

## 风险与回滚

- 自然日历范围可能与现有按跨度检测的 `1 Day` 同时命中；通过“先检测自然周期，再检测滚动范围”的互斥顺序避免双高亮。
- 50 名排名画布较高；通过卡片内部最大高度控制页面长度，并验证滚动和 VChart resize 行为。
- 长用户名可能占用左轴空间；保持现有轴布局，重点检查截断、tooltip 和移动端宽度，不在本任务重做用户名展示策略。
- 回滚时删除自然周期独立模块及测试，撤销筛选弹窗薄接入；排名修复可独立撤销 `rankUserCount`、轴采样和滚动容器改动。

## 上游同步复核点

- 上游若修改 `models-filter-dialog.tsx`，仅复核新增自然周期区块和两组快捷状态互斥。
- 上游若升级 VChart，复核 band 轴 `sampling: false` 是否仍生效。
- 上游若调整用户图表容器高度，复核排名图内部滚动与趋势图固定高度是否仍分离。
