# 数据看板自然周期与用户统计修复实施计划

## 前置检查

1. 读取 `prd.md`、`design.md`、`brief.md`、`implement.jsonl`。
2. 运行 `trellis-before-dev`，加载 build 分支定制、前端质量、i18n 和 React 规范。
3. 确认工作区状态，避免混入无关改动。
4. 通过 `trellis-route(target=implement)` 决定实现执行方式。

## 实现顺序

### 1. 自然周期纯逻辑

- 新增 `calendar-time-ranges.ts`，定义五个自然周期、i18n key、建议粒度和本地时间计算。
- 新增确定性测试，覆盖：
  - 今天起止时间。
  - 本周/上周以周一开始，包括参考时间为周日的场景。
  - 本月/上月跨年或跨月边界。
  - 精确识别已应用自然周期，自定义或滚动 24 小时范围不误判。

### 2. 筛选弹窗薄接入

- 保留现有 `TIME_RANGE_PRESETS` 和 Quick Range 区块不变。
- 新增 Calendar Period 区块，使用五个自然周期按钮。
- 实现滚动范围、自然周期和自定义范围之间的互斥选中态。
- 自然周期应用建议粒度；重置继续恢复现有看板偏好。
- 检查桌面和移动端按钮换行、长翻译和弹窗滚动，不增加嵌套卡片。

### 3. 用户统计 i18n

- 在 `static-keys.ts` 登记全部动态键。
- 通过临时 `scripts/add-missing-keys.mjs` 写入 en、zh、fr、ja、ru、vi、zh-TW。
- 运行 `bun run i18n:sync`，读取同步报告并删除临时脚本。
- 验证中文用户统计筛选弹窗标题和描述不再回退英文。

### 4. 用户排名数据与渲染

- 在 `ProcessedUserChartData` 增加 `rankUserCount`。
- 调整 `processUserChartData`：
  - 返回实际排名人数。
  - 排名 band 轴设置 `sampling: false`。
  - 排名与趋势继续共享同一前 N 用户集合。
- 调整 `user-charts.tsx`：
  - 根据 `rankUserCount` 计算排名画布高度。
  - 排名卡片使用最大可视高度和内部纵向滚动。
  - 趋势图保持原固定高度。
  - 标题区展示实际活跃用户数。
- 新增确定性排名测试：
  - 60 个唯一用户、limit 50 → 50 条排名数据。
  - 13 个唯一用户、limit 20 → 13 条排名数据，不补零。
  - 排名和趋势使用相同用户集合。

### 5. 运行时验证

- 使用合成 50 用户数据确认 VChart spec 包含完整排名条目，分类轴不采样。
- 启动 default 开发服务器，检查：
  - 模型统计、用户统计、流量分析筛选弹窗均出现独立自然周期区块。
  - 中文用户统计弹窗文案正确。
  - 20/50 名排名可在卡片内部滚动查看全部用户。
  - 5/10 名不产生多余滚动空间。
  - 桌面与移动端无按钮、文本、图表重叠或横向溢出。

## 验证命令

```bash
cd web/default
node --test src/features/dashboard/lib/calendar-time-ranges.test.ts src/features/dashboard/lib/charts.test.ts
bun run i18n:sync
bun run typecheck
bunx oxlint -c .oxlintrc.json src/features/dashboard src/i18n/static-keys.ts
bun run format:check
bun run build
```

若全量 format 检查存在历史问题，额外对本任务涉及文件执行目标格式检查并记录全量失败原因；不得遗留本任务引入的格式错误。

## 验收场景

- 点击现有 `1 Day` 后只有滚动范围高亮，时间仍为过去 24 小时。
- 点击 `Today` 后只有自然周期高亮，时间为当天完整范围，粒度为小时。
- 点击 `This Week / Last Week` 后时间为对应周一至周日，粒度为天。
- 点击 `This Month / Last Month` 后时间为完整自然月，粒度为周。
- 选择自然周期后手动修改日期，两组快捷按钮均取消高亮。
- 中文用户统计筛选弹窗标题和描述正确翻译。
- 选择前 20 且存在至少 20 个活跃用户时，滚动区域可查看全部 20 名。
- 选择前 50 且存在至少 50 个活跃用户时，滚动区域可查看全部 50 名。
- 只有 13 个活跃用户且选择前 20 时，显示 13 名和实际人数提示，不生成 7 个零消费用户。

## 风险文件与回滚点

- `models-filter-dialog.tsx`：保持现有滚动范围逻辑不被重写。
- `charts.ts` / `types.ts`：`rankUserCount` 类型与空数据路径必须同时更新。
- `user-charts.tsx`：排名滚动只作用于 rank 图，不影响 trend 图。
- locale 文件：只能通过 i18n 脚本修改；如需回滚，也通过脚本移除本任务新增键。
- 不修改后端文件；若运行时证据显示 `/api/data/users` 本身少返回用户，应回到规划阶段扩展任务，而不是在前端伪造用户。

## 完成条件

- PRD 全部验收项满足。
- 目标测试、类型检查、lint、格式检查、构建通过。
- 桌面和移动端视觉检查通过。
- 最终统一 check 无阻塞问题后才进入提交阶段。
