# 数据看板规范

> `web/default/src/features/dashboard/` 的筛选与图表实现契约。

## 适用范围

- 数据看板模型、分组、用户统计筛选。
- 用户消费排行、趋势图等 VChart 图表。
- 数据看板新增或修改用户可见文案。

## 自然周期筛选

新增“今天 / 昨天 / 本周 / 上周 / 本月 / 上月”这类自然周期时，必须遵守：

- 自然周期范围放在独立模块中计算，例如 `calendar-time-ranges.ts`，不要直接改写既有滚动快速范围定义。
- UI 上自然周期必须作为独立分组展示，例如“自然周期”，避免和“1天 / 7天 / 14天 / 29天”滚动范围混在同一组。
- 滚动范围与自然周期在筛选状态上互斥；选择其中一个时必须清掉另一个的选中态。
- 重新打开筛选弹窗时，日期反查必须先识别自然周期，再识别滚动范围，避免“今天”和“1天”这类边界被错误归类。
- 自定义起止日期时必须清掉滚动范围和自然周期的选中态。
- 所有新增显示文案必须通过 `t('English key')` 使用，并加入 `static-keys.ts` 与全部 locale JSON，再运行 `bun run i18n:sync`。

## 用户排行图表

用户排行的“前 N”只表示请求上限，不表示 UI 必须显示 N 条。图表必须以接口实际返回的 `topUsers.length` 作为显示数量：

- 当接口返回 13 个用户且用户选择“前20”时，标题或徽标显示 13，图表只展示 13 条。
- 当接口返回超过上限时，前端只处理前 N 条。
- 排行图和趋势图必须使用同一份截断后的用户集合，避免两个图的用户范围不一致。
- 不得为了凑齐“前 N”补空白用户、空标签或 0 值数据。

## VChart 类目标签

VChart 的类目轴可能默认采样标签。对于用户排行这类“每个类目都代表一个用户”的图表，必须显式关闭标签采样：

```typescript
axes: [
	{
		orient: 'left',
		type: 'band',
		label: {
			sampling: false,
		},
	},
]
```

当排行数量较大时，用图表容器高度和内部滚动承载更多条目；不要依赖 VChart 自动压缩标签来“适配”高度。

## 测试要求

- 自然周期范围必须覆盖本地时区的日、周、月边界，至少包括周日、跨年、闰年或月底场景。
- 用户排行必须覆盖“实际用户数少于上限”和“实际用户数多于上限”两个场景。
- 修改数据看板文案后必须验证 `bun run i18n:sync` 无 missing、extra、untranslated。

## Wrong vs Correct

### Wrong

```typescript
// 错误：把自然周期塞进既有滚动范围，后续上游同步冲突面更大。
const QUICK_RANGES = [
	{ label: '1 day', days: 1 },
	{ label: 'This Month', days: 30 },
];

// 错误：用选择上限当作实际显示数量。
const displayedUsers = rankLimit;
```

### Correct

```typescript
// 正确：自然周期独立计算，UI 分组也独立。
const calendarRange = getDashboardCalendarRange('thisMonth', now);

// 正确：显示数量来自接口实际返回并截断后的用户数组。
const displayedUsers = topUsers.length;
```
