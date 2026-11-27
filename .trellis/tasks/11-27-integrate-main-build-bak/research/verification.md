# build-main 候选版本验证

定制基线 `accb26db3`，合入提交 `81c1fd20e`，共同基线 `v1.0.0-rc.24`。评估阶段使用 Git 的三方合并记录了 153 个冲突路径。合并提交 `7426c5da3` 已推送到 `origin/build-main`，保留两侧历史。验证在本机隔离数据库、回环网络和受控上游中执行。

## 构建镜像

GitHub Actions 已完成 Linux amd64 和 arm64 两项原生构建，并发布多架构清单。固定标签 `ghcr.io/silentflower/new-api:build-7426c5d` 和分支标签 `ghcr.io/silentflower/new-api:build-main-latest` 发布时的摘要均为 `sha256:2c36eada495ebc2e9076f9be5fc135c980aaec53017c9521d4547da44ab84e9c`。已从 GHCR 分别读取两个标签并核对平台和摘要。[构建记录](https://github.com/SilentFlower/new-api/actions/runs/45979839926)

## 最终检查结果

| 检查 | 结果 |
|---|---|
| 根 Go 模块 | `go build ./...`、`go test ./...` 成功。52 个有测试的包通过，2,947 个顶层测试通过。默认环境跳过 10 个顶层测试，其中 9 个随后在隔离数据库中通过，覆盖真实行锁、审计、预设分组/Key 约束和 令牌列从 CHAR(48) 到 VARCHAR(128) 的迁移。真实 vLLM 服务探测留给启用该服务的部署环境。 |
| 独立 Go 模块 | `relaykit` 与 `tokenkit` 各自使用 `GOWORK=off` 构建和测试，分别有 225 和 7 个顶层测试通过。 |
| 并发与退款 | 为 WebSocket 即时取消及换渠、并发租约、资金退款和任务 CAS 执行定向 `go test -race`，控制器、服务和模型三个包通过。 |
| 前端 | Bun 1.4.4。类型检查覆盖 1,596 个文件，生产构建成功。Vitest 262 个测试文件全部通过，共 3,021 个用例。 |
| 浏览器 | 运行打包后的 Linux amd64 候选程序，使用新建的隔离 SQLite 库。真实 Chrome 完成初始化后的管理员登录，打开看板、渠道、用量日志、消息审计和公开日志页。前四个页面访问对应后端接口成功。 |
| 架构 | Go 1.26.10 按项目 Dockerfile 使用无 CGO 与 PGO 参数，分别产出 Linux amd64 与 arm64 静态可执行文件。amd64 实际启动并提供页面，arm64 验证至交叉构建。 |
| 工作目录 | `MERGE_HEAD` 等于固定目标，`git ls-files -u` 为 0，暂存差异通过 `git diff --cached --check`。合并前的 46 个已有路径均与集成代码暂存范围分开。 |

早期并行验证中，前端重建曾暂时移除供 Go 嵌入的 `web/dist/index.html`，还有一次 HTTP 测试请求等待响应头超过 3 秒。最终先生成前端产物，再以隔离临时目录运行根模块全套测试并取得成功退出。上述用例保留原来的超时和业务断言。

## 上线环境需独立核验

部署方逐项确认真实视频价格、Telegram 回调与凭据、ai-fund 等旧 PAT 调用方、代理和 WS 渠道开关、Redis 集群所有节点的配置。使用 ClickHouse 日志库或 arm64 生产节点的站点，应在对应环境执行日志查询/导出和启动/真实调用。数据库演练使用合成的历史数据，生产备份恢复和切换后新增账务的对账是现场步骤。

## 业务行为与实测

| 范围 | 输入与观察结果 |
|---|---|
| 路由和协议 | Claude 上游计数、Alpha Search、每轮 WS、普通 Responses 与 Compact 分别走候选路由。真实 WS 通道覆盖普通 → Compact → 普通、显式 0/false、未知字段、渠道显式开关、换模型和轮间释放。插件改写正文保留封套。Claude→Gemini 的工具续轮和 Read 上下文有独立模块用例。 |
| 失败与重复执行 | 两个受控上游实际接受 WebSocket 请求。首帧过载换渠只结算一次，已输出部分内容后失败只退款。即时取消在重试开和关两种配置下均退还钱包及 Key 预扣，Pong 和应用关闭码 4001 能送达。成功轮次、HTTP SSE 有单独的终态与实扣断言。 |
| 阶梯预扣 | 采用已确认的新规则。在相同输入及测试费率下，付费组、1,000 输入 token、缺省输出上限的预扣从 62,940 改为 1,500。按 100 个显式输出上限时，预扣从 2,250 改为 1,500。固定价 WS 的失败轮次保留旧版退款。 |
| 预算和异步任务 | 日周预算、父子渠道、个人提额、超限降级、并发租约与任务退款通过定向回归。异步补扣和 Midjourney 只记录一次账务变化。两种测试插件切渠分别核对旧表达式清除、替换后的价格、余额不足时拦截提交。历史 Google/Vertex 视频继续读取旧任务并使用所属渠道轮询、下载。 |
| 管理及可见性 | 带范围的面板 PAT 实际请求管理员账务摘要，校验角色和 `billing:read`。错误作用域及普通用户被拒绝。工作流签发/状态端口分别核验超管及权限。旧 PAT 截止点持久化和过期拒绝有独立断言。重复 Key 名、历史名称筛选和用户日志隔离有数据级回归。 |
| 日志和导出 | SQLite、MySQL、PostgreSQL 下管理员/个人日志、CSV 和模型可见性通过。三表 Excel 用例核对汇总和缓存 token。额外插入 500,001 条匹配记录，明细回调 500,000 次，汇总 500,001 条、500,001 额度，模型汇总共 2,500,005 token。 |
| 三库升级与回滚 | 在旧版创建钱包、Key、旧视频任务和敏感词规则，分别做一致性备份。本地 SQLite、MySQL 8.0、PostgreSQL 15 均经过候选首次启动、第二次启动、恢复备份后由旧程序校验。三轮核对钱包 3,456,789、累计消费 1,234、Key 余额 5,678、旧任务结果地址与规则。MySQL 与 PostgreSQL 的四个 32 位钱包列会使启动被拒绝，改为 64 位后通过。 |
| Redis 同版多进程 | 两个独立候选进程共享真实 Redis。A 预热并读取签名授权，B 读取该签名缓存并收紧 Key 分组，A 随即只看到新分组。另测数据库回源、凭据轮换、进程间 WebSocket 注销及缓存失效。 |
| 页面与语言 | 渠道配置、用量详情、审计和报表界面进入前端回归。7 个语言包均覆盖双亲并集的 8,555 个键，每个另有 1 个订阅周期边界提示。游乐场旧全局数据清理及不同账号存储隔离有已运行的前端用例。 |

## 保留的旧行为如何核对

原版控制器、模型、服务共 79 个顶层定向用例通过。候选版其中 75 个仍在原包同名运行，业务错误状态判断已移入 relay。高级渠道按模型选路改由统一选渠和路由恢复用例覆盖。两个首响应计时用例改由真实多轮连接消费日志逐轮核对 `frt`，控制帧另验。原版的 3 个 Claude→Gemini 特定回归也在合并后的独立模块运行。

冲突 Go 测试文件中，以下 48 个定制侧独有测试按最终代码逐个对应。原名相同的行给出测试文件，替换的行列出候选测试。预扣预期有单独的业务确认。

| 定制版测试 | 候选版验证 |
|---|---|
| `TestRecordRelayErrorLogIncludesVisionAssistFailureFields` | 同名。controller/relay_error_log_compat_test.go |
| `TestRecordRelayErrorLogRespectsGlobalSwitchForNormalErrors` | 同名。controller/relay_error_log_compat_test.go |
| `TestResponsesWebSocketBusinessErrorPersistsCompactAudit` | 同名。controller/relay_error_log_compat_test.go |
| `TestApplyConvertedResponsesWebSocketRequestPreservesUnknownFields` | TestResponsesWSCreateEventKeepsClientBytes、TestResponsesWSIngressPreservesEnvelope、TestResponsesCompactWebSocketRawFrameAndTerminalUsage |
| `TestHandleResponsesWebSocketUpstreamEventRefundsFailureTerminals` | TestResponsesStreamOutcomesPreserveAccounting、TestResponsesInterruptedStreamHealth |
| `TestHandleResponsesWebSocketUpstreamEventSettlesSuccessTerminal` | TestResponsesWebSocketReusesConnectionAndSettlesEachRequest |
| `TestHandleResponsesWebSocketUpstreamEventTracksCompactOutputAndFailure` | TestProxyResponsesWebSocketSupportsOrdinaryCompactAndOrdinaryTurns、TestResponsesCompactWebSocketRawFrameAndTerminalUsage |
| `TestParseResponsesWebSocketTurnDetectsCompactV2` | TestProxyResponsesWebSocketSupportsOrdinaryCompactAndOrdinaryTurns |
| `TestPrepareResponsesWebSocketTurnAttemptRejectsAdvancedCustomConverter` | TestSelectResponsesWSChannelAcceptsNativeResponsesChannelTypes |
| `TestPrepareResponsesWebSocketTurnAttemptRejectsMaxOutputTokensOverflow` | TestNormalizeResponsesWSMaxOutputTokens、TestResponsesWebSocketLocalErrorsKeepStreamIdentity |
| `TestProxyResponsesWebSocketDoesNotRetryAfterBusinessOutput` | TestResponsesWebSocketRetryBeforeDelivery（已收到增量） |
| `TestProxyResponsesWebSocketForwardsCancelPongAndCloseCode` | 同名。controller/responses_websocket_test.go |
| `TestProxyResponsesWebSocketRetriesFirstBusinessErrorBeforeDownstreamWrite` | TestResponsesWebSocketRetryBeforeDelivery（首帧失败） |
| `TestProxyResponsesWebSocketSupportsOrdinaryCompactAndOrdinaryTurns` | 同名。controller/responses_websocket_compat_test.go |
| `TestResponsesWebSocketBusinessErrorUsesSemanticRetryStatus` | 同名。relay/responses_websocket_compat_test.go |
| `TestResponsesWebSocketChannelSupportsTurnUsesAdvancedCustomModelRoute` | TestSelectResponsesWSChannelAcceptsNativeResponsesChannelTypes、TestRestoreConnectionContextRejectsChangedAdvancedCustomRoute |
| `TestGetTokenUsageUsesAuthenticatedContextForSuffixedAuthorization` | 同名。controller/token_test.go |
| `TestAdminAuthAllowsPATWithLegacyNewAPIUserHeader` | 同名。middleware/auth_test.go |
| `TestMigratedRootTokenUsesCurrentOwnerDespiteStaleCache` | 同名。middleware/auth_test.go |
| `TestTokenAuthReadOnlyAllowsAIKeyLogQuery` | 同名。middleware/auth_test.go |
| `TestAlphaSearchHelperDoesNotCommitNon2xxResponse` | 同名。relay/alpha_search_handler_test.go |
| `TestAlphaSearchHelperDoesNotLogTransportURLQuery` | 同名。relay/alpha_search_handler_test.go |
| `TestAlphaSearchHelperSettlesFrozenToolBillingOnSuccess` | 同名。relay/alpha_search_handler_test.go |
| `TestBuildAlphaSearchUpstreamURL` | 同名。relay/alpha_search_handler_test.go |
| `TestNewAlphaSearchUpstreamRequestReplacesClientAuthorization` | 同名。relay/alpha_search_handler_test.go |
| `TestPrepareAlphaSearchRequestBodyPreservesRawFields` | 同名。relay/alpha_search_handler_test.go |
| `TestPrepareRequestForSelectedChannelMapsAlphaSearchModel` | 同名。relay/alpha_search_handler_test.go |
| `TestSafeAlphaSearchResponseFiltersUnsafeHeaders` | 同名。relay/alpha_search_handler_test.go |
| `TestModelPriceHelperFreezesMappedBillingModel` | 同名。relay/helper/price_test.go |
| `TestModelPriceHelperTieredPreConsumeMaxTokensFallback` | 已确认预扣改用主线输入倍率。TestModelPriceHelperTieredInputPreConsumeMultiplier 覆盖缺省上限、显式上限、免费分组 |
| `TestModelPriceHelperTieredUsesMappedUpstreamModelWhenChannelSettingEnabled` | 同名。relay/helper/price_test.go |
| `TestModelPriceHelperUsesMappedUpstreamModelWhenChannelSettingEnabled` | 同名。relay/helper/price_test.go |
| `TestDialResponsesWebSocketBuildsSafeUpstreamHandshake` | 同名。relay/responses_websocket_compat_test.go |
| `TestDialResponsesWebSocketUsesAdvancedCustomNativeRoute` | 同名。relay/responses_websocket_compat_test.go |
| `TestMessageParseContentSupportsTypedMediaContent` | 同名。relaykit/dto/openai_request_zero_value_test.go |
| `TestOpenAIChatRequestToClaudeMessagesConvertsTextFileToTextBlock` | 同名。relaykit/relayconvert/internal/oai_chat/to_claude_messages_req_test.go |
| `TestSetRelayRouterRegistersAlphaSearchRoute` | 同名。router/relay_router_compat_test.go |
| `TestSetRelayRouterRegistersClaudeCountTokensRoute` | 同名。router/relay_router_compat_test.go |
| `TestSetRelayRouterRegistersResponsesWebSocketRoute` | 同名。router/relay_router_compat_test.go |
| `TestChargeViolationFeeRecordsChannelUserDailyQuota` | 同名。service/task_billing_test.go |
| `TestLogTaskConsumptionUsesBillingModelAndTraceFields` | 同名。service/task_billing_test.go |
| `TestRecalculateTaskQuotaTracksFuturePositiveDeltaForHistoricalTask` | 同名。service/task_billing_test.go |
| `TestRecalculateTaskQuotaTracksPositiveDeltaOnSettlementDay` | 同名。service/task_billing_test.go |
| `TestRefundTaskQuota_FundingFailureKeepsPendingMarker` | TestRefundTaskQuota_FundingFailureKeepsAccountingAndPendingMarker |
| `TestCalculateTextQuotaSummaryUsesBillingModelName` | 同名。service/text_quota_test.go |
| `TestCalculateTextQuotaSummaryUsesFrozenBillingModelAfterUpstreamModelChanged` | 同名。service/text_quota_test.go |
| `TestCalculateTextToolCallSurchargeBillsLocalChatWebSearchOnce` | 同名。service/text_quota_test.go |
| `TestCalculateTextToolCallSurchargeMergesSameNameAndPrice` | 同名。service/text_quota_test.go |
