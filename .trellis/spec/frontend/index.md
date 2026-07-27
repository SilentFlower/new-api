# 前端开发规范

> 本项目默认前端 `web/default/` 的可执行实现契约。

## 规范索引

| 规范 | 描述 | 状态 |
|------|------|------|
| [数据看板规范](./dashboard-guidelines.md) | 数据看板筛选、图表排行和 i18n 同步契约 | 已完成 |
| [Base UI 组件组合规范](./base-ui-composition.md) | 复合组件上下文、运行时错误排查和交互验证要求 | 已完成 |

## 开发前必读清单

### 涉及数据看板

- [数据看板规范](./dashboard-guidelines.md) — 自然周期筛选、用户排行图表和翻译同步要求

### 涉及 Base UI 复合组件

- [Base UI 组件组合规范](./base-ui-composition.md) — 菜单、选择器、Sheet、Drawer 的组合上下文与验证要求

## 核心规则速查

| # | 规则 | 详见 |
|---|------|------|
| 1 | 新增自然周期筛选必须独立于既有滚动快速范围 | [数据看板规范](./dashboard-guidelines.md) |
| 2 | 排名图表必须按实际返回用户数展示，不得补齐到所选上限 | [数据看板规范](./dashboard-guidelines.md) |
| 3 | VChart 类目排行需要关闭标签采样，避免“前 N”标签被自动省略 | [数据看板规范](./dashboard-guidelines.md) |
| 4 | Base UI 分组子组件必须位于对应 Group 上下文中 | [Base UI 组件组合规范](./base-ui-composition.md) |
| 5 | 受控选择器分页时必须保证当前值仍有对应选项 | [Base UI 组件组合规范](./base-ui-composition.md) |

**语言**: 所有文档使用**中文**编写。
