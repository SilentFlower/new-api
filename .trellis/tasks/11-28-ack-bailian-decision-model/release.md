# ACK 百炼决策插件交付记录

## 已生效配置

- 正式网关：https://fw.xhgj100.com；New API 版本 build-de755af。
- 渠道 23「百炼决策模型」，类型 61（任务插件），分组 ai,project，状态启用。
- 插件 bailian-decision 1.0.0，模型 decision-model。源码 SHA-256：31e6a0ed1bda7f7597c16617fc139840d8191a3b52be55fd2cd6fcbde2b1fc3a。
- 上游为原 18 号渠道的百炼工作空间及凭据，地址以 /compatible-mode 结尾。
- 原输入单价 USD 0.0410958904 / 百万输入 Token。插件专用表达式为 `tier("base", u("input_tokens") * 0.0410958904 / 1000000)`，配置键为 `bailian-decision::decision-model`。原 ModelRatio=0.0205479452 及既有组倍率保留。
- 18 号模型列表由 deepseek-flash,qwen3.8-flash,decision-model 改为 deepseek-flash,qwen3.8-flash；19 号由 gte-rerank-v2,decision-model 改为 gte-rerank-v2。其余业务配置逐项对账通过。
- 插件独立源码保存在 scripts/task-plugins，通过原生管理 API 安装。本轮使用当前固定镜像。

## 客户端调用

使用现有 ai 或 project 分组模型 Key，调用百炼原生 System One 接口。

```bash
curl https://fw.xhgj100.com/v1/systemone \
  -H "Authorization: Bearer $NEW_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"model":"decision-model","state":"用户申请退款","questions":{"refund":{"type":"noul","instructions":"是否申请退款"}}}'
```

专属前缀入口 `/bailian-decision/v1/systemone` 同样可用。请求支持 choice、noul、score，以及 image/images；响应保留百炼 answers、usage 和 request_id。接入方选择支持该协议的客户端。聊天协议包装及预览模型属于独立扩展范围。

## 验收证据

审计根目录：`/root/.local/share/aihub-audits/20261128-ack-decision-model`。私有恢复材料按目录 0700、敏感文件 0600 保存。

| 验证 | 实际结果 | 证据 |
| --- | --- | --- |
| Node 集中回归 | 23 项通过 | node --test scripts/task-plugins/bailian-decision.test.mjs |
| 固定镜像宿主联调 | 11 个场景通过，原始状态顺序保留 | gateway-lab-result.json |
| 计费异常 | 缺失用量保留预扣并实际记录 WARN；上游 400 完整退款；零用量结算为 0 | gateway-lab-result.json、gateway-lab-container.log |
| 正式决策请求 | 6 次全部 200，均命中渠道 23；三类问题、两个分组、单图、双图、清理后复测 | live-verification-result.json |
| 正式计费 | 总输入 9341 Token，消费 192 内部额度单位；账号、Key、消费日志逐笔一致 | live-verification-result.json |
| 原项目 Key | 用户 69、70、71、73、74 对应的 5 个原 Key 均返回 200，各可见 12 个模型并包含 decision-model | existing-project-keys-final.json |
| 原渠道对账 | 18、19 号仅精确移除目标模型；自然变化的消费计数、探测时间等运行字段独立核对 | cutover-state.json、final-production-state.json |
| 临时资源清理 | 用户 77、78 已删除；用户、模型 Key、管理令牌、会话残留均 0；隔离容器已移除 | fixture-cleanup-result.json、final-fixture-database-cleanup.json、gateway-lab-state.json |

宿主联调使用 `ghcr.io/silentflower/new-api@sha256:211d60555c5d7f96d9d67004915b08e5871f3ac049f8dd09d17404f0cd5654cf`。ACK 保持生产固定镜像 `registry.cn-hangzhou.aliyuncs.com/xhgj-all/new-api@sha256:93786d6c68c644d39f92a4c8f9c5d3b22ef2c383b7e9ecd50d40ac51ffc3f3f2`，应用版本均为 build-de755af。

## 精确回退

回退属于实际发生故障后的运维操作。先停止后续写入，保存当前状态，与 `production-before.json`、`production-deployment-state.json` 和 `cutover-state.json` 比较，核对是否存在其他维护者的并发修改。

1. 通过原生管理 API 禁用 23 号渠道，先检查在途任务与已完成记账。
2. 在模型列表仍匹配本次写入值时，将 18 号恢复为 deepseek-flash,qwen3.8-flash,decision-model，将 19 号恢复为 gte-rerank-v2,decision-model；该步骤恢复原路由配置。
3. 重新读取价格版本，使用当前 expected_version，仅撤销本轮增加的 `bailian-decision::decision-model` 表达式。保留其他模型、管理员价格与分组倍率。
4. 核对渠道绑定和任务状态后，仅处理本轮独立插件 key 与 1.0.0 版本，以及新建 23 号渠道。凭据和已有业务账号继续使用原有维护流程。
5. 出现基线漂移时保持暂停，重新核对差异，按实际生效范围恢复。原版普通渠道对 decision-model 的旧协议限制仍需通过本记录中的独立插件接入解决。
