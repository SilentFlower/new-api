# Thinking Guides

> **Purpose**: Expand your thinking to catch things you might not have considered.

---

## Why Thinking Guides?

**Most bugs and tech debt come from "didn't think of that"**, not from lack of skill:

- Didn't think about what happens at layer boundaries → cross-layer bugs
- Didn't think about code patterns repeating → duplicated code everywhere
- Didn't think about edge cases → runtime errors
- Didn't think about future maintainers → unreadable code

These guides help you **ask the right questions before coding**.

---

## Available Guides

| Guide | Purpose | When to Use |
|-------|---------|-------------|
| **[Build 分支上游同步友好定制指南](./build-upstream-friendly-customization.md)** | **最高优先级：定制逻辑独立、旧代码最薄接入、降低上游同步冲突** | **build 分支新增或接入任何上游没有的能力时必须阅读** |
| [Code Reuse Thinking Guide](./code-reuse-thinking-guide.md) | Identify patterns and reduce duplication | When you notice repeated patterns |
| [Cross-Layer Thinking Guide](./cross-layer-thinking-guide.md) | Think through data flow across layers | Features spanning multiple layers |

---

## Quick Reference: Thinking Triggers

### Build 分支定制（最高优先级）

- [ ] 正在增加上游仓库没有的能力
- [ ] 需要修改上游核心文件才能接入本地实现
- [ ] 后续还要持续同步上游更新
- [ ] 当前方案为了复用准备重构、移动或抽取旧代码

→ 必须先读 [Build 分支上游同步友好定制指南](./build-upstream-friendly-customization.md)

### When to Think About Cross-Layer Issues

- [ ] Feature touches 3+ layers (API, Service, Component, Database)
- [ ] Data format changes between layers
- [ ] Multiple consumers need the same data
- [ ] You're not sure where to put some logic

→ Read [Cross-Layer Thinking Guide](./cross-layer-thinking-guide.md)

### When to Think About Code Reuse

- [ ] You're writing similar code to something that exists
- [ ] You see the same pattern repeated 3+ times
- [ ] You're adding a new field to multiple places
- [ ] **You're modifying any constant or config**
- [ ] **You're creating a new utility/helper function** ← Search first!

→ Read [Code Reuse Thinking Guide](./code-reuse-thinking-guide.md)

---

## Pre-Modification Rule (CRITICAL)

> **Before changing ANY value, ALWAYS search first!**

```bash
# Search for the value you're about to change
grep -r "value_to_change" .
```

This single habit prevents most "forgot to update X" bugs.

---

## How to Use This Directory

1. **Before coding**: Skim the relevant thinking guide
2. **During coding**: If something feels repetitive or complex, check the guides
3. **After bugs**: Add new insights to the relevant guide (learn from mistakes)

---

## Contributing

Found a new "didn't think of that" moment? Add it to the relevant guide.

---

**Core Principle**: 30 minutes of thinking saves 3 hours of debugging.
