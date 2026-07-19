# Brief — 数据看板自然周期与用户统计修复

## Goal

- 保留新 UI 现有 `1/7/14/29 天`滚动范围，新增旧 UI 的自然周期查询，并修复用户统计筛选英文文案与前 N 排名标签被省略的问题。

## Scope

- 在数据看板筛选弹窗新增独立“自然周期”区块：今天、本周、上周、本月、上月。
- 自然周期使用浏览器本地时间，周一为周起点，并自动联动粒度：今天→小时，本周/上周→天，本月/上月→周。
- 保持滚动范围、自然周期、自定义日期三种状态互斥；不修改 `TIME_RANGE_PRESETS`。
- 补齐用户统计筛选标题、描述、自然周期及实际活跃用户数的全部支持语言翻译和动态键登记。
- 用户排名 band 轴关闭 VChart 标签采样，按实际排名人数分配稳定画布行高，并在卡片内部纵向滚动查看前 20/50。
- 当前活跃用户少于 N 时显示实际人数，不补零消费用户；排名和趋势继续使用同一前 N 用户集合。

## Non-Goals

- 不修改 classic UI。
- 不修改 `/api/data/users` 查询参数、后端聚合口径或数据库。
- 不替换或改变现有滚动快捷范围和模型统计默认偏好。
- 不重做数据看板整体布局、图表视觉体系或长用户名策略。

## Key Context

- 自然周期核心逻辑新增到 `web/default/src/features/dashboard/lib/calendar-time-ranges.ts`，筛选弹窗只保留薄接入，降低 build 分支上游同步冲突。
- 旧 UI 自然周期定义位于 `web/classic/src/helpers/dashboard.jsx`；新 UI 入口位于 `models-filter-dialog.tsx`。
- 用户统计英文回退源于 `dashboard/index.tsx` 动态传入的 i18n 键未进入 locale 和 `static-keys.ts`。
- 用户排名数据会保留 `slice(0, limit)` 的 N 条结果；主要显示缺陷是 VChart band 轴默认 `sampling: true` 和当前固定图表高度。
- `ProcessedUserChartData` 将新增 `rankUserCount`，用于实际人数提示和排名画布高度计算。
- Locale 文件只能通过临时 `add-missing-keys.mjs` 写入，并在结束前运行 `bun run i18n:sync`、删除临时脚本。
- 风险重点是自然周期与 `1 Day` 双高亮、50 名画布滚动行为、排名滚动误影响趋势图。

## Acceptance

- 现有 `1/7/14/29 天`行为不变，新增五个自然周期且时间边界、周起点和建议粒度正确。
- 应用、重开、手动修改和重置筛选时，两组快捷范围状态正确且不会双高亮。
- 中文用户统计筛选弹窗不再显示英文，全部支持语言有对应翻译。
- 接口存在至少 N 个唯一活跃用户时，前 5/10/20/50 的排名数据和可见条目均为 N。
- 前 20/50 可在卡片内部滚动查看全部用户名和条形，不把整页无限拉长。
- 活跃用户少于 N 时显示实际人数，不生成零消费占位；趋势图与排名图用户集合一致。
- 目标测试、i18n 同步、类型检查、lint、格式检查、构建及桌面/移动端视觉检查通过。

## Next Step

- 用户确认 planning artifacts 和本 brief 后，运行 `task.py start`，再通过 `trellis-route(target=implement)` 进入实现。
