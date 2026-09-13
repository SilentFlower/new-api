# 模型用量持续累计：实际开销记录

## 环境与方法

2026-09-13，本机 WSL2 / Linux amd64，AMD Ryzen 7 5800H，Go 基准默认 16 线程。使用独立、无业务数据的 Docker Redis 8.10.1（`model-usage-test-redis`），仅绑定 127.0.0.1 动态端口；没有访问生产 Redis。Redis `hash-max-listpack-entries=512`。

基准在 `service/channel_model_usage_benchmark_test.go`，通过现有 SQLite 夹具和真实 `RecordChannelUserModelQuotaUsage` / `readChannelBudgetCounter` 路径执行。Redis 脚本已预热，内存与 Redis 结果分开；固定逻辑时间为 2031-09-16，仅用于稳定日周窗口。

```bash
CHANNEL_MODEL_USAGE_BENCH_REDIS_ADDR=127.0.0.1:44623 go test ./service -run '^$' -bench 'BenchmarkChannelModelUsage(Record|Read)$' -benchtime=200ms -count=1
CHANNEL_MODEL_USAGE_BENCH_REDIS_ADDR=127.0.0.1:44623 go test ./service -run '^$' -bench BenchmarkChannelModelUsageStorage -benchtime=1x -count=1
```

该变量只允许指向专用测试实例：基准会清空其 DB。测试实例在验证结束后销毁。

## 结算与读取

| 路径 | 配置 | 耗时 ns/op | Go B/op | allocs/op |
| --- | --- | ---: | ---: | ---: |
| 内存结算 | 开关关闭 | 26,943 | 5,749 | 88 |
| 内存结算 | 开关开启 | 37,335 | 7,690 | 123 |
| Redis 结算 | 开关关闭 | 1,665,643 | 8,395 | 117 |
| Redis 结算 | 开关开启 | 1,601,208 | 10,744 | 159 |
| 内存预算读取 | 1 模型 | 864 | 528 | 10 |
| 内存预算读取 | 8 模型 | 4,602 | 3,504 | 38 |
| 内存预算读取 | 64 模型 | 36,898 | 25,376 | 261 |
| Redis 预算读取 | 1 模型 | 654,090 | 1,528 | 49 |
| Redis 预算读取 | 8 模型 | 855,719 | 6,576 | 175 |
| Redis 预算读取 | 64 模型 | 1,127,928 | 45,264 | 1,182 |

内存结算本次增加约 10.4 μs、1.9 KB 临时分配。Redis 两组差异受本机并行构建和 Docker 网络波动影响，不能据此声称开启更快，也不是生产请求端到端延迟承诺。新增来源的多模型读取一次 Lua 批量返回，仅随所选模型数增加计算与数据量，没有逐模型网络往返。

## Redis 持久统计存储与写调用

固定 100 名用户 × 20 个模型，每个组合一次正向结算 500 quota，共 2,000 次；两组初始没有模型预算。累计后逐 key 执行 `MEMORY USAGE` 求和，只包含日周计数，不含策略缓存、数据库、Redis 进程基础开销或 Go 临时分配。

| 指标 | 关闭 | 开启 | 差值 |
| --- | ---: | ---: | ---: |
| 日周统计 key | 4 | 44 | +40（20 模型 × 日周） |
| MEMORY USAGE 字节 | 2,762 | 29,582 | +26,820（约 26.2 KiB） |
| EVALSHA 次数 | 2,000 | 2,000 | 0 |
| HINCRBY 次数（Lua 内部） | 12,000 | 20,000 | +8,000 |
| Redis 脚本平均用时 μs | 69.13 | 109.63 | +40.50 |

每次额外更新两个模型 Hash，每个 Hash 各增加用户字段与 `__pool`，所以增加四次脚本内 HINCRBY，仍为一次结算累计脚本。关闭组没有模型事实 key。

空间随活跃的用户/模型/周期组合增长；超过 Redis Hash 紧凑编码阈值时会跳变，不能把这组小规模数字按人数简单线性承诺。日周事实和人工调整均使用窗口结束加一天的过期时间；内存调整桶与原计数桶共用过期清理。以上属于受控样本观测，没有进行生产压测。
