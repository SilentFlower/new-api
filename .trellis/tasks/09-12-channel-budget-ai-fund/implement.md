# Implement — ai-fund 迁移到预算 v2

1. [ ] 替换 fixture 为 v2 实际响应；归一化白名单改 v2，先让现有测试按新 fixture 跑通。
2. [ ] worker BFF：按行用量/覆盖路由与客户端函数；删除旧路由与旧列写入；`PUT /pools/:poolId/limits` 只保留并发。
3. [ ] 本人降级提示按行携带 `models` 与目标。
4. [ ] 前端预算表 + 时段列表 + 按行用量/特批 + 本人面板标签。
5. [ ] 规范更新。

## 验证

```bash
cd /root/project/ai-fund/worker && node --test src/*.test.js && node --check src/index.js
cd /root/project/ai-fund/frontend && npm run build
```
