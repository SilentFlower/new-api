# 规范化现有 Build 特有 Feature

## Goal

识别 `build-bak` 分支相对上游的现有定制功能，并按照“定制逻辑独立、上游文件最薄接入”的开发规范降低未来同步 `main` 时的冲突面，同时保持现有功能和对外协议不变。

## Background

- 当前开发分支为 `build-bak`，持续通过合并 `main` 获取上游更新。
- `.trellis/spec/guides/build-upstream-friendly-customization.md` 已将上游同步友好原则定义为 build 分支定制开发的最高优先级指导。
- 历史 build 定制覆盖 Responses Compact、Alpha Search、视觉辅助、Claude WebSearch、统计与 Excel 报表等多个领域，不能仅凭提交信息判断哪些实现仍需治理。
- 历史讨论已确认：定制业务优先放入新文件、包或组件；原有上游文件只保留必要的注册、条件分派或窄函数调用。
- 当前代码对比显示，排除 Trellis/Agent 文件后仍有约 196 个业务文件差异，不能安全地一次性重构。
- 首批审计确认 Responses Compact 与 Alpha Search 的协议实现已经独立，但共享 Relay 控制器和渠道分发中间件仍存在较大冲突面，详见 `research.md`。

## Confirmed Requirements

### R1. 以代码证据建立 Feature 清单

- 对照当前 `build-bak` 与上游基线，识别仍然存在的 build 特有功能及其入口、核心实现和测试。
- 排除纯 Trellis 记录、构建触发提交、已经被上游吸收的实现和仅存在于历史提交但当前代码已删除的功能。
- 每个功能记录现有上游文件改动、独立文件、稳定公共能力依赖和潜在冲突面。

### R2. 按上游同步友好规范评估

- 判断每个 build 特有功能是否已做到定制逻辑独立、上游文件最薄接入。
- 不为了消除少量重复而抽取、改名、移动或重写上游核心逻辑。
- 不修改与治理目标无关的格式、缩进、注释、命名或代码位置。
- 任何拟修改的原有上游文件都必须说明接入点、必要性和预计冲突面。

### R3. 保持行为与兼容性

- 治理过程不得删除现有 build 功能，不得改变既有 API、计费、日志、权限或前端交互语义，除非后续需求明确授权。
- 后端继续满足 SQLite、MySQL 和 PostgreSQL 兼容要求，并使用项目 JSON 封装。
- 前端用户文案继续满足六语言 i18n 要求。

### R4. 分批实施与验证

- 采用“全量盘点、分批治理”的方式，不进行一次性全分支重构。
- 首批优先评估 Responses Compact / Alpha Search 链路；只有代码证据确认存在明显冲突面或不符合最薄接入规范时才进入改造。
- 复杂、跨领域功能按可独立验收的边界拆分，避免一次性重写全部定制代码。
- 每批改动必须包含行为回归验证、原有文件 diff 审核和上游同步冲突面复核。
- 删除新模块并撤销薄接入点后，应能清晰回滚该批治理改动。

### R5. 首批结构治理边界

- 首批严格限定为行为不变的结构调整，已有功能必须保持正常。
- 首批只治理 Responses Compact / Alpha Search 的上游核心文件接入面。
- 优先恢复 `middleware.Distribute` 的上游友好结构，让 Responses WebSocket 使用独立渠道选择实现；允许为降低冲突保留受测试保护的局部重复。
- 将 Relay 请求快照、attempt 状态、普通计费收尾和 Alpha Search 专用计费准备移入独立文件；核心控制器只保留窄分派。
- 保留现有独立协议实现，不为了形式统一重写 `relay/responses_compact_passthrough.go` 或 `relay/alpha_search_handler.go`。
- 若审计发现现有行为与文档、测试或预期不一致，只记录为后续缺陷，不在首批治理中顺带修复。

## Acceptance Criteria

- [x] `research.md` 形成基于当前代码的 build 特有 Feature 清单，并标注首批治理或后续审计状态。
- [x] 用户确认采用全量盘点、分批治理，并将首批限定为严格行为不变的 Responses Compact / Alpha Search 结构调整。
- [x] `design.md` 列出新建文件、必须修改的上游文件、每个最薄接入点及其必要性。
- [x] `implement.md` 给出步骤、验证命令、冲突面、回滚方式和上游同步后的复核点。
- [ ] `middleware.Distribute` 恢复接近首批功能接入前的顺序式结构，不再因 Responses WebSocket 复用而大面积抽取和重排。
- [ ] Responses WebSocket 的模型权限、亲和性、首次选渠和当前渠道能力校验位于独立领域文件，并保持基础模型语义。
- [ ] `controller/relay.go` 中 Relay attempt 与 Alpha Search 计费实现迁入独立文件，核心主循环只保留必要调用和分派。
- [ ] `relay/responses_compact_passthrough.go`、`relay/alpha_search_handler.go` 等现有独立协议实现不因本任务重写。
- [ ] 实施范围内的现有功能行为保持不变，相关测试和差异检查通过。
- [ ] 结构调整后重复通过规划阶段基线、定向 race、全仓测试、vet 和 `git diff --check`。
- [ ] 未执行真实 OpenAI/sub2api 联调时，在交付说明中明确记录，但不阻塞本批完成。

## Out Of Scope

- 在本任务中合并新的 `main` 提交。
- 新增尚未提出的产品功能。
- 因风格偏好重写已符合规范的独立实现。
- 修改受项目策略保护的项目名称、组织标识、品牌或归属信息。

## Decision

- 用户确认采用“全量盘点、分批治理”。
- 首批治理 Responses Compact / Alpha Search 已确认的核心接入冲突面。
- 用户确认首批严格保持现有功能行为，不允许混入业务修复。
- 本地协议模拟、完整回归、定向 race、全仓测试和构建检查作为阻塞验收门槛；真实 OpenAI/sub2api 联调为非阻塞补充项。
