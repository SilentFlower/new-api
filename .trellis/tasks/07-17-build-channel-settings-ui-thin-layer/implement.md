# 渠道设置前端 Build 薄层化实施计划

## 实施步骤

1. Default 侧新增 build 映射模块，将 build 字段 schema/default/parse/build/refine 从主 `channel-form.ts` 迁出。
2. Default 侧新增 build UI 组件，将 WebSearch、视觉辅助、Responses Compact、上游模型计费和上游模型检测从主 drawer 迁出。
3. Classic 侧新增 build 映射 helper，将初始化、提交合并、WebSearch 校验和临时字段清理从 `EditChannelModal.jsx` 迁出。
4. Classic 侧新增 build UI 组件，将上游模型检测和 build 额外字段从 `EditChannelModal.jsx` 迁出。
5. 增加/扩展单测，覆盖 round-trip、未知字段保留、WebSearch API Key 保留/清空/替换、上游模型检测 settings 合并。
6. 执行相关 Bun 测试、Default typecheck/lint、Classic 构建或语法校验、`git diff --check`。

## 验证重点

- 不改字段名和 JSON 键。
- `web_search.api_key` 仍只在输入新 Key 时提交。
- `web_search.clear_api_key` 仍只在清空且没有新 Key 时提交为 true。
- `vision_assist` 和 `web_search` 内未知字段保留。
- `settings` 内上游模型检测未知字段保留，关闭检测时仍按旧逻辑清空 last detected models。
- 主表单文件只保留薄接入点。
