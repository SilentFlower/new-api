# 接通 ACK 百炼 decision-model 决策插件渠道

## 目标

让现有 ai、project 分组用户使用 ACK 的模型调用 Key，通过正式网关调用百炼 decision-model，取得结构化答案，并按实际输入 Token 正确记账。

## 已核实的事实

- ACK 入口为 https://fw.xhgj100.com，当前镜像 build-de755af。18 号渠道为普通 OpenAI 兼容渠道，19 号为百炼原生重排渠道，两者均列出 decision-model。
- 上游曾返回 Unsupported model，独立 /v1/systemone 在当前网关返回 404。使用 18 号渠道的现有凭据请求百炼工作空间 System One 接口，已得到 HTTP 200 和 83 个输入 Token。
- 官方市场 TypeSafe 1.2.0 的模型白名单仅包含 Jev。其输入上下文、图片字段和问题校验也属于 TypeSafe 契约。请求转发、百炼真实响应解析和用量提取已通过隔离验证。
- 百炼正式版支持 state、questions，以及 image/images。问题类型为 choice、noul、score；返回 answers 和 usage.input_tokens。官方最大输入长度为 268288 Token。
- ACK 现有 super_admin 维护接口已通过身份、插件列表与渠道读取核验。项目现有 Hub 管理授权继续维持原范围。

## 要求

1. 提供独立、可上传维护的百炼决策插件，模型为 decision-model，使用现有插件机制。模型 Key 的账号归属、分组、模型白名单及额度限制仍由网关执行。
2. 通过 /v1/systemone 接收百炼原生参数，保留状态数据顺序并返回结构化答案。支持正式版的三类问题和文本、图像输入，按官方约束处理参数与上游错误。
3. 使用独立的“百炼决策模型”任务插件渠道，复用 18 号渠道的工作空间及上游凭据，分组沿用 ai,project。其他模型和渠道属性保持现状。
4. 沿用当前管理员设置的输入单价与分组倍率，以插件专用输入 Token 表达式预扣、实际结算和记录日志。成功请求计费一次。上游失败按宿主退款机制处理。缺失或异常用量保留预扣并留下诊断，不能静默按免费结算。
5. 新渠道验收成功后，从 18、19 号渠道移除 decision-model。保留 deepseek-flash、qwen3.8-flash、gte-rerank-v2 的原有配置。
6. 发布前保存插件、渠道、价格的原始状态与私有恢复材料。发布后核对原 Key 可用性、分组可见性、扣费、日志和原渠道配置，失败时按精确范围恢复。

## 验收

- 隔离宿主使用与 ACK 相同的固定镜像加载插件；验证三类问题、图像字段、拒绝非法参数、上游失败、缺失/异常用量和重复计费保护。
- 正式网关 /v1/systemone 使用授权的模型 Key 返回 HTTP 200，包含 decision-model、对应答案和真实 usage.input_tokens。
- 新渠道在管理界面可见，类型为任务插件，绑定新插件。ai、project 两组模型可见；权限失败请求保留网关拒绝语义。
- 通过实际请求核对账号余额与 Token 消耗差额、任务和消费日志，确认单次结算、原输入单价与分组倍率。
- 18、19 号渠道仅移除 decision-model，原聊天、重排模型及其他设置逐项对账。临时测试账号、Key 和隔离资源清理完毕，恢复材料保留。

## 范围边界

本轮仅接入正式版 decision-model。预览模型及将决策结果包装为 Chat Completions 属于独立扩展。New API 核心流程和镜像保持当前版本，ai-fund 业务代码与长期管理授权保持现状。

## 证据

- /root/.local/share/aihub-audits/20261128-ack-decision-model/typesafe-unmodified-probe.json
- /root/.local/share/aihub-audits/20261128-ack-decision-model/bailian-native-probe.json
- /root/.local/share/aihub-audits/20261128-ack-decision-model/typesafe-compatibility-result.json
- https://help.aliyun.com/zh/model-studio/decision-model-api
- https://docs.bailian.console.aliyun.com/zh/model-studio/decision-model-stable
