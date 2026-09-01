# 前端 i18n 与上游合并规范

> `web/src/i18n/` 的翻译键、语言包写入和上游合并契约。

## 1. 适用范围与触发条件

以下任一情况必须执行本规范：

- 新增或修改 `t(...)`、动态文案键、`static-keys.ts` 或用户可见文案。
- 修改 `web/src/i18n/`、`web/src/i18n/locales/*.json`、语言检测或运行时资源映射。
- 合并、变基或挑选上游提交时，任一父分支修改了前端功能代码或 i18n 路径。
- i18n 目录发生移动或重命名，例如旧路径 `web/default/src/i18n/` 迁移到 `web/src/i18n/`。

`bun run i18n:sync` 成功不是完整验收。同步器只比较当前七个语言包之间的键集合；如果某个键在合并时从七个文件中同时消失，同步报告仍可能全部为 `0`，但运行时 `t('Missing key')` 会回退显示英文键。

## 2. 文件与命令签名

### 语言包

```text
web/src/i18n/locales/{en,zh,zh-TW,fr,ja,ru,vi}.json
```

每个文件必须保持以下结构：

```typescript
type LocaleFile = {
  translation: Record<string, string>
}
```

运行时映射由 `web/src/i18n/config.ts` 定义：

| 运行时语言码 | 文件 |
|---|---|
| `en` | `en.json` |
| `zhCN` | `zh.json` |
| `zhTW` | `zh-TW.json` |
| `fr` | `fr.json` |
| `ja` | `ja.json` |
| `ru` | `ru.json` |
| `vi` | `vi.json` |

### 标准命令

```bash
cd web
bun run i18n:sync
bun run typecheck
bun run build
```

源码级缺键扫描必须使用项目 `i18n-translate` 工作流定义的 `find-missing-keys.mjs`，扫描完成后删除临时脚本。语言包写入必须使用该工作流定义的 `add-missing-keys.mjs`，禁止直接编辑 locale JSON。

## 3. 可执行契约

### 新增文案

- 字面量文案使用 `t('English key')`。
- 运行时动态传入 `t(key)` 的键必须登记到 `web/src/i18n/static-keys.ts`，或者提供等价、可扫描的静态来源。
- 每个新增键必须同时写入 `en`、`zh`、`zh-TW`、`fr`、`ja`、`ru`、`vi`。
- `{{variable}}` 占位符的名称和数量必须在七种语言中一致。
- 品牌名、URL、模型名和协议标识可以保持英文；菜单、按钮、状态、提示、校验错误和说明文字必须翻译。

### 上游合并

语言包冲突不得使用整目录或整文件的 `ours` / `theirs` 解决。对每个 locale，默认合并结果必须满足：

```text
mergedKeys(locale) ⊇ localParentKeys(locale) ∪ upstreamParentKeys(locale)
```

只有同时满足以下条件时才允许删除父分支已有键：

1. 已确认对应功能或文案被有意删除。
2. 源码字面量扫描和 `static-keys.ts` 均不再引用该键。
3. 七个 locale 同步删除，且审查记录列出删除键及理由。

目录发生重命名时，必须先建立逻辑路径映射，再比较父分支键集合。例如：

```text
local:    web/default/src/i18n/locales/zh.json
upstream: web/src/i18n/locales/zh.json
result:   web/src/i18n/locales/zh.json
```

Git 的 rename 识别不能替代键集合检查。

### 同键值冲突

- 同一键在两个父分支值不同时，必须根据当前调用点语义选择，不能按文件侧别批量覆盖。
- 非英文 locale 若从父分支的本地化值退化成与英文值完全相同，默认视为回归并阻止合并。
- 只有品牌名、技术标识或经现有 allowlist 认可的字面量可以保持英文。
- 上游已更新且语义正确的翻译应保留；仅补回上游结果缺失的 build 分支键，避免覆盖上游修订。

## 4. 校验与错误矩阵

| 条件 | 结果 |
|---|---|
| `i18n:sync` 报告任一 locale 有 missing / extras / untranslated | 阻止提交，修复后重跑 |
| 源码静态扫描发现 `t(...)` 键不在 `en.json` | 阻止提交；同步报告即使全绿也不能放行 |
| 七个 locale 键集合或键数量不一致 | 阻止提交 |
| 合并结果缺少任一父分支键，且无明确删除记录 | 阻止合并 |
| 非英文值从已翻译内容退化为英文，且不在技术字面量 allowlist | 阻止合并并人工复核调用点 |
| 只更新部分 locale | 阻止提交，使用脚本一次性补齐七种语言 |
| 占位符名称或数量不一致 | 阻止提交 |
| 临时 i18n 脚本残留在工作区 | 删除后再提交 |
| 类型检查或生产构建失败 | 阻止提交 |

## 5. Good / Base / Bad

- **Good**：上游移动 i18n 目录时，按新旧逻辑路径读取两个父分支的七语言键，保留上游现有值，只补回 build 分支独有键，再执行源码扫描、同步、类型检查和构建。
- **Good**：发现中文键仍存在但值从“消息审计”退化成 `Message Audits`，将其作为值回归处理，而不是因为键数量一致就放行。
- **Base**：普通功能改动新增一个静态文案，通过脚本一次性写入七个 locale，源码扫描和同步报告均为零问题。
- **Bad**：冲突时执行 `git checkout --theirs web/src/i18n/locales/*.json`，导致 build 分支功能代码保留、翻译键整体丢失。
- **Bad**：只看 `_sync-report.json` 全绿就认定 i18n 完整。
- **Bad**：只修复用户看到的一个中文菜单，未扫描同一合并中其他缺失键和其他语言。

## 6. 必须执行的测试

### 普通 i18n 改动

1. 源码级 `t(...)` 缺键扫描结果必须为 `0`。
2. `bun run i18n:sync` 后七个 locale 的 `missingCount`、`extrasCount`、`untranslatedCount` 必须全部为 `0`。
3. 校验七个 locale 的键集合完全一致。
4. `bun run typecheck` 通过。
5. `bun run build` 通过。

### 上游合并额外测试

1. 对每个 locale 计算两个父分支键集合并集，断言合并结果无未说明缺失键。
2. 对两个父分支共同拥有的键检查值退化：非英文值由本地化内容变成英文时必须列出并复核。
3. 目录重命名时按逻辑映射读取父分支文件，不能只比较相同物理路径。
4. 至少抽查简体中文、繁体中文和一个非中文 locale 的 build 分支菜单或核心功能文案。
5. 确认 `web/src/i18n/config.ts` 的资源映射实际加载已修改的 locale 文件。

## 7. Wrong vs Correct

### Wrong

```bash
# 错误：整包选择上游侧，Git 冲突消失但 build 独有翻译一起消失。
git checkout --theirs web/src/i18n/locales/*.json

# 错误：当前语言包内部一致，不代表源码使用的键存在。
cd web && bun run i18n:sync
```

### Correct

```javascript
// 正确：合并时先建立每个 locale 的父分支键并集。
const expectedKeys = new Set([
  ...Object.keys(localParent.translation),
  ...Object.keys(upstreamParent.translation),
])

const unexplainedMissing = [...expectedKeys].filter(
  (key) => !Object.hasOwn(merged.translation, key)
)

if (unexplainedMissing.length > 0) {
  throw new Error(`i18n merge lost keys: ${unexplainedMissing.join(', ')}`)
}
```

完成键并集校验后，仍必须执行源码缺键扫描、`i18n:sync`、类型检查和生产构建；这些门禁互补，任何单项都不能替代其他项。
