# 数据看板 Excel 分组列与导出性能优化设计

## 范围边界

本任务只修改管理员数据看板 Excel 导出链路：

- Controller：`controller/usedata.go` 的 Excel 生成与响应输出。
- Model：`model/log.go`、`model/token_statistics.go` 的导出查询和统计聚合。
- 测试：Controller 导出文件契约与 Model 分组聚合行为。

不修改前端导出弹窗、API 路由、权限、数据库结构、日志写入逻辑和其他统计接口。

## 当前数据流与瓶颈

```text
GET /api/data/export
  -> GetLogSummaryByKey       -> 分批扫描全部匹配日志
  -> GetLogDetailByKeyModel   -> 再次分批扫描全部匹配日志
  -> GetLogsForExport         -> 一次性加载前 500000 条日志
  -> excelize 普通模式逐单元格写入
  -> 写入 HTTP 响应
```

主要问题：

1. 同一筛选条件下的日志最多被遍历三次。
2. 请求日志使用 `[]*Log` 承载最多 500000 条完整记录。
3. Excel 普通写入模式会随单元格数量增加内存占用。
4. Sheet 1、Sheet 2 的聚合键没有分组，无法区分不同分组下的同名 API Key。

## 目标数据流

```text
GET /api/data/export
  -> 解析并校验时间、分组、API Key 筛选
  -> 创建三个 Sheet 的 StreamWriter
  -> Model 使用请求 Context 顺序读取匹配日志一次
       -> 累加“分组 + API Key + 用户”汇总
       -> 累加“分组 + API Key + 用户 + 模型”明细
       -> 仅将前 500000 条日志回调给 Sheet 3 StreamWriter
       -> 超过 500000 条后继续聚合，但不再写 Sheet 3
  -> 排序并流式写入 Sheet 1、Sheet 2
  -> Flush 三个 StreamWriter
  -> 写入 HTTP 响应
```

该设计保持现有输出上限：Sheet 3 仍只包含按时间升序排列的前 500000 条日志；Sheet 1、Sheet 2 仍覆盖完整匹配范围。

## Model 设计

### 导出数据结构

`LogSummaryByKey` 增加 `Group` 字段，聚合键调整为：

```text
Group + TokenName + Username
```

`LogDetailByKeyModel` 增加 `Group` 字段，聚合键调整为：

```text
Group + TokenName + Username + ModelName
```

历史日志的 `group` 为空时按空字符串独立聚合并原样导出，不擅自改写为 `default`。

### 单次遍历接口

新增一个面向导出业务的 Model 公共方法，职责为：

- 接收 `context.Context`、时间范围、用户名、API Key 列表和分组列表。
- 使用与现有导出一致的消费日志过滤条件。
- 按 `created_at asc` 顺序读取日志。
- 对全部匹配日志同时生成 Sheet 1、Sheet 2 聚合结果。
- 对前 `exportLogMaxRows` 条日志执行明细回调；回调错误立即终止查询。
- 返回排序后的汇总、模型明细和标准 Go `error`。

查询使用 `Rows()` 顺序读取，而不使用 `FindInBatches`：ClickHouse 日志表的 `id` 默认可能为 `0`，GORM 的 `FindInBatches` 依赖主键游标，不适合作为跨日志库的稳定顺序遍历机制。

查询只选择以下字段：

```text
created_at, username, token_name, model_name, quota, prompt_tokens,
completion_tokens, use_time, is_stream, channel_id, group, request_id, other
```

`group` 通过 GORM 字段选择或现有 `logGroupCol` 处理，禁止直接拼接不带引号的保留字。查询绑定请求 Context，客户端断开后由数据库驱动和 `rows.Err()` 传播取消错误。

### Token 统计口径

每条日志只解析一次 `other` JSON，并复用解析结果计算聚合 Token 与 Sheet 3 的缓存读写展示，继续保持：

```text
普通输入 + Anthropic 缓存读取 + Anthropic 缓存写入 + 输出
```

不改回数据库原生求和，避免历史 Anthropic 缓存日志统计回退。

## Controller 与 Excel 设计

### StreamWriter 使用约束

- 三个 Sheet 均使用 `NewStreamWriter`。
- 列宽必须在首次 `SetRow` 前通过 `StreamWriter.SetColWidth` 设置。
- 表头和数据行都使用 `SetRow`，同一 Sheet 不混用普通单元格写入 API。
- 加粗样式在创建 StreamWriter 前生成，通过 `excelize.Cell` 或行样式应用。
- 每个 Sheet 的行号严格递增，完成后逐一调用 `Flush`。

### Sheet 契约

Sheet 1“汇总统计”：

```text
分组 | API Key 名称 | 请求次数 | 请求 Token 数 | 请求额度
```

Sheet 2“模型明细”按“分组 + API Key + 用户”组织区块，标题明确展示分组与 API Key；区块内列为：

```text
模型名称 | 请求次数 | 请求 Token 数 | 请求额度
```

Sheet 3“请求日志”：

```text
时间 | 分组 | API Key | 模型 | 输入 Tokens | 输出 Tokens |
额度消耗 | 耗时(s) | 是否流式 | 渠道 ID | 请求 ID
```

现有文件名、Sheet 名、缓存 Token 展示、中文表头和响应头保持不变。

## 错误处理

- 时间参数错误继续使用现有管理 API 错误响应。
- Model 查询、行扫描、明细回调、StreamWriter 写入或 Flush 失败时，在 Excel 响应头写出前使用 `common.ApiError` 返回错误。
- 最终向客户端写文件失败时记录系统日志并结束响应；客户端取消产生的写入错误不继续处理。
- 所有打开的 SQL Rows、Excel 文件和临时资源通过 `defer` 关闭。

## 性能与资源边界

- 数据库遍历次数：从最多三次降为一次。
- 日志明细内存：从最多 500000 个 `*Log` 降为单行对象和数据库驱动缓冲。
- Excel 单元格内存：由 StreamWriter 在超过内部阈值后落到临时文件。
- 聚合内存仍与唯一“分组 + API Key + 用户 + 模型”组合数相关，但不再与日志总行数线性增长。
- 继续扫描 500000 行之后的数据只为保证 Sheet 1、Sheet 2 的完整统计，不再产生 Sheet 3 单元格。

## 兼容与回滚

- SQLite、MySQL、PostgreSQL 使用相同 GORM 查询路径。
- ClickHouse 使用相同 Rows 遍历路径，并保留现有保留字引用规则。
- 不新增迁移，回滚时可恢复 Controller 的普通 Excel 写入和原三个 Model 查询；分组字段为纯读取字段，不涉及数据回滚。
