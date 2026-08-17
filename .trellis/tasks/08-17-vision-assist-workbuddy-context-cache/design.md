# 技术设计

## 1. 变更边界

新增 `service/vision_assist_workbuddy.go` 独立承载 WorkBuddy 系统提醒扫描与过滤，新增 `service/vision_assist_workbuddy_test.go` 承载专用行为测试。原有 `service/vision_assist.go` 只增加过滤结果接入和 primary/legacy 缓存查询，避免把定制解析逻辑铺入上游核心流程。视觉辅助请求转换、上游适配、消息审计、数据库、渠道 DTO 和前端配置不需要变更。

本设计把 WorkBuddy 过滤限定在“为图片所属消息计算识图文本”的阶段：原始请求继续保留完整 WorkBuddy 上下文，只有派生辅助请求、辅助请求体大小估算和视觉辅助缓存键使用过滤后文本。

## 2. 数据流

```text
原始 user 文本
  -> 现有协议级文本提取
  -> WorkBuddy 已知注入块黑名单扫描
       -> 未删除完整黑名单块：effective = raw
       -> 已删除黑名单块：effective = 删除系统提醒后的其余原文
  -> 按 MessageIndex 绑定图片
  -> 使用 effective 规划批次和构造辅助请求
  -> 使用 effective 构造新缓存键
  -> 新键未命中时使用 raw 构造旧缓存键兼容查询
```

## 3. WorkBuddy 黑名单文本规范化

新增一个表达稳定业务概念的内部函数，例如：

```go
func filterWorkBuddyVisionAssistUserMessage(raw string) (effective string, changed bool)
```

解析规则：

1. 使用小型线性词法扫描器识别 `<...>` 标记，只解释 `system-reminder` 黑名单标签；图片本地路径、用户查询容器及其他内容全部作为普通原文保留。
2. `system-reminder` 标签名和相关属性名按 ASCII 大小写不敏感处理；相关属性比较时把 `_` 和 `-` 视为等价。开始标签允许常规空白、附加属性，以及单引号、双引号或无引号的简单属性值。
3. 将带等价 `data-role=user-context` 属性且边界完整的 `system-reminder` 块登记为删除区间。
4. 每个黑名单块必须独立完成开始/结束配对；未闭合、孤立闭合或错误嵌套的块不登记删除区间，不把剩余消息推断为块内容。
5. 如果没有登记任何删除区间，直接返回 `raw, false`。
6. 按原始顺序复制所有未删除原文。删除区间使用稳定换行连接，只裁剪由删除操作产生的首尾和相邻边界空白，不改变正文内部格式。
7. `user_query`、`image_local_path`、图片引用标记、本地路径和未知标签全部保留，因此也会进入辅助请求和 primary cache key。
8. 清理后没有正文时返回 `"", true`；否则返回清理后的正文和 `true`。

不使用 XML 或 HTML 反序列化：WorkBuddy 用户文本包含 Markdown、Windows 路径、身份文件和可能未转义的任意文本，不能假定整段消息是合法标记文档。扫描器只删除明确列入黑名单且边界完整的区间，时间复杂度为 O(n)，不递归解释未知标签，也不根据 User-Agent 猜测客户端身份。

本方案选择黑名单的原因是缓存身份可以直接由“原文减去系统提醒块”的稳定结果生成。WorkBuddy 在系统提醒内部增删身份文件或连接器内容时不会改变 primary key；本地路径和其他剩余原文仍会进入 key，避免在未过滤内容变化时错误合并缓存。

## 4. 识图单元数据

`visionAssistUnit` 同时保存：

```go
type visionAssistUnit struct {
    Images            []VisionAssistImage
    UserMessage       string // 规范化后，实际发送给辅助模型
    LegacyUserMessage string // 原始文本，仅用于旧缓存键兼容查询
}
```

现有协议解析和 `resolveVisionAssistUserMessages` 继续产生原始消息文本，避免改变其跨协议与历史消息绑定契约。`ApplyVisionAssist` 在构建批次前生成规范化消息映射；批次大小估算和 `buildVisionAssistRequest` 只读取 `UserMessage`。批次形成后按首张图片的 `MessageIndex` 关联对应的 `LegacyUserMessage`。

## 5. 缓存迁移

### 5.1 键定义

- Primary key：现有 `buildVisionAssistCacheKey` 参数不变，传入 `unit.UserMessage`。
- Legacy key：只有原始文本与规范化文本不同时构造，传入 `unit.LegacyUserMessage`。
- `vision_assist:v1` 命名空间和 `visionAssistCacheValue{Text}` 保持不变。

### 5.2 查询顺序

```text
请求内 primary key
  -> HybridCache primary key
  -> HybridCache legacy key（仅键不同且 primary 正常未命中）
  -> 上游辅助调用
```

旧键命中后：

- 立即作为普通缓存命中返回；
- 使用当前规范化 TTL 回填 primary key；
- 回填失败只记录告警，不得转为上游调用；
- 请求内缓存与批次去重统一登记 primary key。

新识图成功后只写 primary key。这样部署后已有精确旧键仍可复用，同时不会为每份动态 WorkBuddy 上下文持续写入双份缓存。当前 `HybridCache` 不暴露剩余 TTL，因此回填使用渠道当前 TTL；这是有意的兼容选择，缓存身份仍由图片、问题、规则、渠道和模型完整约束。

### 5.3 错误处理

- Primary cache 读取错误沿用现有告警并进入上游流程，不再对同一异常缓存后端读取 legacy key。
- Legacy cache 读取错误记录告警并进入上游流程。
- Legacy 命中后的 primary 回填错误不影响已命中的结果。
- 空文本缓存继续视为未命中。

## 6. 去重、审计与计费

- `requestCache` 和 `missingByCacheKey` 继续以 primary key 为唯一身份，保证同一实际识图语义在请求内合并。
- 新键或旧键命中都不进入 caller，因此不会创建视觉辅助独立审计、预扣费或消费记录。
- 内部重试行为不变；只有两个缓存键都未命中时才进入现有 caller 和重试链路。
- 现有缓存命中统计按实际图片数量计算，不新增可能暴露用户结构的日志字段。

## 7. 测试设计

### 文本规范化

- 完整线上结构：只过滤系统提醒，保留用户查询标签和本地路径原文。
- 多个用户查询容器、图片路径标签和本地路径按原顺序完整保留。
- 消息只有系统提醒黑名单内容：得到空问题。
- 表格覆盖系统提醒标签大小写、空白、属性顺序、单双引号、附加属性和相关属性的 `_` / `-` 别名。
- 系统提醒未闭合、孤立闭合或错误嵌套：对应块保持原文，不发生越界删除。
- 未删除系统提醒时，全部原始文本保持不变。
- 删除系统提醒后，Markdown、比较符号、用户查询标签、图片路径标签和未知标签逐字保留。

### 缓存兼容

- 预置 legacy key，执行请求后 caller 为 0，primary key 可读取。
- 两份系统提醒内容或写法不同，但相同图片和保留正文时只调用一次 caller。
- 相同图片但本地路径或其他过滤后保留正文不同则调用两次。
- 旧键命中仍计入缓存命中，且不产生真实辅助尝试。

### 回归

- 保留历史图片问题绑定、纯文本追问不重识别、联合分批、请求内重复图片和缓存边界测试。

## 8. 风险与回滚

- 风险：WorkBuddy 未来使用新的注入标签。黑名单不会删除未知块，可能重新把噪声发送给辅助模型并形成不同缓存键；后续只基于真实请求增加边界明确的黑名单项。
- 风险：本地路径保留在辅助请求和 cache key 中，会向辅助模型暴露客户端路径，并使同一图片在路径变化时无法复用缓存；这是本任务明确接受的行为。
- 风险：普通用户正文包含完整的黑名单标签。处理方式是只删除语义明确的 `data-role=user-context` 系统提醒并保留其他标签；这是采用黑名单后接受的兼容性取舍。
- 风险：畸形标签导致越界误删。处理方式是每个黑名单块独立严格配对，边界不完整时保留原文，不做开标签到消息末尾的推断。
- 风险：旧缓存内容由未过滤上下文生成，回填后可能延长其生命周期。该缓存已经是成功的非空识图结果，且键仍完整绑定图片和问题；优先复用符合“不重复识别”的要求。
- 风险：多实例混跑新旧版本时，旧实例不知道 primary key。当前部署为单 new-api 实例；代码避免永久 dual-write 带来的高基数膨胀。多实例发布应在同一发布窗口完成版本收敛。
- 回滚只需恢复文本规范化和旧键兼容逻辑；没有数据库、配置或持久数据迁移，已有 primary 缓存可按 TTL 自然过期。
