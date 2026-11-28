# 技术设计

## 独立实现与接入

新增 scripts/task-plugins/bailian-decision.js，使用插件 API v1，独立 key 为 bailian-decision。插件通过原生上传接口加载到 ACK，保持当前网关镜像和 Go 路由、计费引擎。源码与测试留在 scripts/task-plugins，避免自动成为未来镜像中的内置插件。

新增 scripts/task-plugins/bailian-decision.test.mjs，集中保护请求、响应与计费契约。隔离宿主联调材料置于任务研究目录或本机私有审计目录，临时凭据只保存在受保护的恢复材料中。

原有上游代码文件的必要修改为空。稳定接入点为 POST /api/plugin/task、渠道管理 API、模型价格配置 API 和原生插件路由分发。定制逻辑集中在独立插件；未来上游合并复核插件 API v1、任务计费表达式、上传与渠道绑定契约。

## 参考实现与差异

参考 QuantumNous TypeSafe 1.2.0，源 SHA-256 为 60828f2fc3c32dab787d7748f4f398838dff84de6dd5da438276fc3f54689310。保留原始来源与作者归属。同步复核点为原生解码/呈现、同步完成返回结构、请求描述符和用量钩子。

百炼适配使用独立模型声明与校验，替换 Jev 模型白名单、私有图片请求头和上下文预算。沿用可验证的请求转发、同步完成和输入用量提取接口。模型价格固定为插件的 input_tokens 用量事实，单价描述为“输入 Token 单价”，单位为 token。

## 请求与响应

插件声明 POST /v1/systemone 及自身前缀路径，使用 requiredCapabilities 的 json-order@1 保留 state 对象顺序。上游地址来自新渠道，固定本次已验证的百炼工作空间 /compatible-mode。凭据只发送到该渠道可信的百炼主机。

支持正式版 decision-model 的 state、questions、image、images。choice、noul、score 按官方契约验证，保留其正常文本、对象、数组状态及图像输入。官方约束之外的限制必须有具体证据，禁止照搬 TypeSafe 的 box、assets、10 级 score 或 Jev 长上下文请求头。

成功响应校验模型、各问题答案类型、输入用量，原生返回 answers、usage、request_id 等百炼字段。parseSubmitResponse 返回即时 SUCCESS；retainResult=false 沿宿主契约处理结果保存。业务错误及状态码遵循百炼和宿主错误机制，凭据与验证材料不得进入日志。

## 计费

读回 decision-model 的完整价格快照及 expected_version，保留管理员显式覆盖。新配置仅增加 bailian-decision::decision-model 的插件用量表达式，采用 tier("base", u("input_tokens") * 单价 / 1000000)。单价由当前模型输入价格与 QuotaPerUnit 的真实配置换算，组倍率沿用宿主。

提交预留以正式版最大输入 268288 Token 为依据，完成时使用真实非负整数 usage.input_tokens。用量异常遵守宿主保留预扣和诊断语义；验证失败、上游失败与退款均在隔离宿主验证。测试覆盖真实 83 Token 响应与零值、缺失、负数、非整数和越界值。失败及重试不得重复结算。

## 发布及回退

使用已核验的原生超管维护接口，保持 Hub 长期具名授权范围。先备份 18、19 号渠道、插件清单、模型价格快照，核对基线后安装独立插件、保存精确插件价格、创建新任务插件渠道。新渠道仅服务 decision-model，分组 ai,project，复用 18 号工作空间和凭据。

先验证新渠道及两个入口，再通过原生渠道更新接口仅移除 18、19 号的 decision-model。逐项比较其余模型和属性。并发发现配置漂移时停止当前写入并重新核对，禁止用旧快照覆盖其它管理员调整。

失败先停止后续修改，按当时实际状态恢复匹配的模型列表、插件价格和新渠道状态。保留原渠道与上游凭据。插件回退仅操作本轮独立 key 与版本，并核对在途任务。正式操作与测试材料留存于私有审计目录。
