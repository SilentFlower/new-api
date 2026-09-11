# Maven Evidence

仅 Maven 变更加载本文件，按既有 evidence schema 只读核对 final evidence：

```bash
python3 ./.trellis/scripts/maven_verify.py check --latest --require-plan <final-plan.json>
```

- `reusable`：核对 lifecycle、模块、消费者、测试、附属制品和 skip 项后计入证据。
- `partial`：记录未覆盖的 module/consumer/test/artifact 或更高 lifecycle 要求。
- `stale`：记录源码、测试、POM、外部父 POM、Git 或工具链失效原因。
- `failed` / `blocked`：保留失败或证据损坏事实，不把未执行验证写成通过。

主会话和 dedicated subagent 都不得调用 `plan` / `run` 或任何 Maven goal，也不得运行会写 `target/`、本地仓库或缓存的构建。缺少可复用证据时报告由 implement 路径执行的精确重跑需求，不默认 `clean package/install`、`-amd` 或全 reactor。
