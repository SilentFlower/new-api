# 实施计划

## 1. 实现 WorkBuddy 注入块黑名单过滤

- [x] 新建 `service/vision_assist_workbuddy.go`，独立实现只识别 WorkBuddy 系统提醒的线性词法扫描逻辑。
- [x] 系统提醒标签和相关属性按 ASCII 大小写不敏感处理，并兼容常规空白、属性顺序与引号、附加属性及属性 `_` / `-` 别名。
- [x] 只删除边界完整的 `data-role=user-context` 系统提醒块，保留边界异常及未知块原文。
- [x] 完整保留用户查询标签、图片本地路径标签、路径值、图片引用标记和其他剩余原文。
- [x] 在系统提醒删除边界处生成稳定分隔，避免文本粘连，同时不压缩或重写其余原文。
- [x] 保证规范化仅用于视觉辅助派生数据，不改写主请求。

## 2. 接入识图单元和请求规划

- [x] 扩展 `visionAssistUnit`，同时保留规范化用户问题和原始旧用户文本。
- [x] 在 `ApplyVisionAssist` 中先解析原始消息映射，再生成规范化消息映射。
- [x] 让批次划分、8 MiB 请求体估算和辅助请求构造使用过滤后文本。
- [x] 保持 MessageIndex 绑定、历史图片问题和多图分批语义不变。

## 3. 实现缓存双键兼容

- [x] Primary key 使用过滤后文本，legacy key 使用原始文本，命名空间和值结构不变。
- [x] 按 primary -> legacy -> caller 顺序查询。
- [x] Legacy 命中后复用结果并回填 primary；回填失败只告警。
- [x] 请求内缓存、重复批次合并、失败图片统计和成功写入统一使用 primary key。
- [x] 新识图结果只写 primary key，空结果不写缓存。

## 4. 增加行为测试

- [x] 新建 `service/vision_assist_workbuddy_test.go`，增加线上 WorkBuddy 结构、多个查询、路径块、空问题和畸形结构表格测试。
- [x] 增加系统提醒大小写、空白、属性顺序、单双引号、附加属性和属性 `_` / `-` 别名的兼容矩阵。
- [x] 增加系统提醒未闭合、孤立闭合、错误嵌套、用户查询标签、图片路径标签和未知正文标签的保留测试。
- [x] 断言实际辅助请求不包含系统提醒、身份文件或连接器状态，但保留本地路径原文。
- [x] 增加 legacy key 预置、命中、primary 回填以及 caller 不执行测试。
- [x] 增加仅系统提醒内容或写法变化仍命中新缓存、本地路径或其他保留正文变化必须重新识别测试。
- [x] 保留并运行历史追问、多图分批、请求内去重和缓存边界回归。

## 5. 验证

- [x] `go test ./service -run 'VisionAssist' -count=1`
- [x] `go test -race ./service -run 'VisionAssist' -count=1`
- [x] `go test ./relay -run 'VisionAssist|RequestPreparationState' -count=1`
- [x] `cd relaykit && GOWORK=off go build ./...`
- [x] `go test ./... -count=1`
- [x] `go vet ./...`
- [x] `git diff --check`

## 6. 实现后核验

- [x] 确认辅助请求正文使用过滤后文本，主请求仍保留完整 WorkBuddy 上下文。
- [x] 确认新旧缓存命中均不进入 caller、审计或计费链路。
- [x] 确认缓存日志没有新增原文、路径或图片内容。
- [x] 对照 `.trellis/spec/backend/relay-vision-assist.md` 更新必要的可执行契约。

## 7. 回滚点

- [ ] 若规范化产生兼容回归，回退 WorkBuddy 黑名单过滤并恢复 `effective = raw`。
- [ ] 若缓存迁移产生异常，保留 primary key 结构并暂时关闭 legacy 查询；所有新增缓存可按现有 TTL 自然过期。
