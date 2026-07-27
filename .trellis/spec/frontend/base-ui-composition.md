# Base UI 组件组合规范

> 适用于 `web/default/` 中基于 Base UI 封装的菜单、选择器、Sheet、Drawer 等交互组件。

## 1. Scope / Trigger

- Trigger：新增或修改 Base UI 复合组件的分组部件、受控值、Portal、Sheet/Drawer 内菜单或选择器。
- 风险：TypeScript 和生产构建可以通过，但缺少 React Context 会在用户打开组件时触发数字运行时错误。

## 2. Signatures

```tsx
<DropdownMenuGroup>{/* Label / CheckboxItem */}</DropdownMenuGroup>
<SelectGroup>{/* SelectItem */}</SelectGroup>
<DropdownMenuTrigger render={<Button />} />
```

## 3. Contracts

- `DropdownMenuLabel`、`DropdownMenuCheckboxItem` 必须位于 `DropdownMenuGroup` 或 `DropdownMenuRadioGroup` 中。
- `SelectItem` 必须位于 `SelectGroup` 中；其他复合组件同样遵守其父子上下文契约。
- 触发器替换底层元素时使用项目封装组件的 `render` 属性，保留事件、焦点和可访问性语义。
- 受控 Select 的当前 `value` 必须始终存在于渲染选项中；分页数据不含当前项时显式补入当前项。
- Sheet/Drawer 内切换数据时，是否重置子组件状态必须由 `key` 或外部状态明确表达，不能依赖偶然的容器重挂载。

## 4. Validation & Error Matrix

| 条件 | 行为 |
| --- | --- |
| 菜单分组上下文缺失 | 视为运行时缺陷；按官方错误码定位并补齐 Group |
| Network 为 200、错误边界显示“500” | 先检查浏览器控制台，不得误判为后端 500 |
| Select 当前值不在分页选项中 | 补入当前选项后再渲染受控 Select |
| Sheet/Drawer 内切换请求 | 容器保持打开，按需求重置或保留子状态 |

## 5. Good / Base / Bad Cases

- Good：复选菜单完整包在 `DropdownMenuGroup` 中，打开菜单不会触发缺失上下文错误。
- Good：详情选择另一请求时 Sheet 保持打开，带 `key={requestId}` 的内容区重新加载并重置局部过滤状态。
- Base：会话只有一页，当前请求自然存在于选项中，不显示分页按钮。
- Bad：把 Base UI 数字错误码当作接口 500，只排查后端日志。
- Bad：受控 Select 的 value 不在 items 中，导致显示、键盘或焦点行为异常。

## 6. Tests Required

- 执行 `bun run typecheck`、目标文件 oxlint、oxfmt 检查和 `bun run build`。
- 对筛选、选项拼接或分页占位等纯逻辑增加 Vitest 回归测试。
- 条件允许时在生产构建中实际打开菜单/选择器，验证 Portal、焦点、键盘和 Sheet/Drawer 切换行为。

## 7. Wrong vs Correct

```tsx
// 错误：分组子组件缺少 Group 上下文。
<DropdownMenuContent>
  <DropdownMenuCheckboxItem>user</DropdownMenuCheckboxItem>
</DropdownMenuContent>

// 正确：分组部件位于对应上下文中。
<DropdownMenuContent>
  <DropdownMenuGroup>
    <DropdownMenuCheckboxItem>user</DropdownMenuCheckboxItem>
  </DropdownMenuGroup>
</DropdownMenuContent>
```
