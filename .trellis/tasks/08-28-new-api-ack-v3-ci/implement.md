# new-api 接入 devops-infra V3 ACK 发布链路实施计划

## Implementation Checklist

1. 保留当前无关脏文件：
   - `.trellis/spec/frontend/index.md`
   - `.trellis/spec/guides/build-upstream-friendly-customization.md`
   - `.trellis/spec/frontend/i18n-merge-guidelines.md`
2. 新增根目录 `.gitlab-ci.yml`，只 include devops-infra 应用模板。
3. 新增 `.ci/project.yml`，声明 prod-only、ACR、ACK、namespace、release gateway、manual deploy policy。
4. 新增 `.ci/components/api.yml`，声明 `api` / `go-service` / `new-api` / 根目录 Dockerfile / `app_port: 3000`。
5. 新增 `.ci/env/prod.yml`，声明单副本、资源、`APP_ENV_` 运行参数和 `APP_SECRET_` Secret 引用。
6. 不新增 `.ci/env/test.yml`，不修改 Dockerfile、docker-compose 或业务代码。
7. 用 devops-infra MR `!430` 对应 planner 做本地校验：

```bash
python3 /root/project/devops-infra-harness/devops-infra/scripts/ci_planner.py validate \
  --project-file .ci/project.yml \
  --components-dir .ci/components \
  --env-dir .ci/env \
  --ref-name prod
```

8. 渲染 child pipeline 到临时输出文件并检查关键 job / 变量：

```bash
python3 /root/project/devops-infra-harness/devops-infra/scripts/ci_planner.py render-child \
  --project-file .ci/project.yml \
  --components-dir .ci/components \
  --env-dir .ci/env \
  --ref-name prod \
  --output .ci-out/child-pipeline.yml \
  --infra-project digital-rd-infra/devops-infra \
  --infra-ref feature-generic-dockerfile-service-v3
```

9. 检查渲染结果：
   - 必须包含 `package-api`、`deploy-submit-api`、`deploy-verify-api`。
   - 必须包含 `DEPLOY_APP_PORT: "3000"`、`REPLICAS: "1"`、资源变量、`APP_ENV_` 变量和 `APP_SECRET_` 变量。
   - 不应包含额外 `build-api` job。
10. 删除或忽略本地临时 `.ci-out/child-pipeline.yml`，避免把生成物纳入提交。
11. 执行 `git diff --stat` 和 `git status --short`，确认本任务 diff 只包含 Trellis 任务文档和新增 CI 配置文件。

## Validation Commands

本任务只新增 CI 配置，不修改 Go / React 业务代码。必跑校验：

```bash
python3 /root/project/devops-infra-harness/devops-infra/scripts/ci_planner.py validate \
  --project-file .ci/project.yml \
  --components-dir .ci/components \
  --env-dir .ci/env \
  --ref-name prod
```

```bash
python3 /root/project/devops-infra-harness/devops-infra/scripts/ci_planner.py render-child \
  --project-file .ci/project.yml \
  --components-dir .ci/components \
  --env-dir .ci/env \
  --ref-name prod \
  --output .ci-out/child-pipeline.yml \
  --infra-project digital-rd-infra/devops-infra \
  --infra-ref feature-generic-dockerfile-service-v3
```

可选校验：

```bash
git diff --check -- .gitlab-ci.yml .ci
```

## Risky Files / Rollback Points

- `.gitlab-ci.yml` 是业务仓 CI 入口，错误 include 会导致 GitLab pipeline 无法展开。
- `.ci/project.yml` 的 `env_map`、`registry`、`kubernetes` 字段决定发布环境和 ACK 目标，必须用 planner 校验。
- `.ci/env/prod.yml` 的 `SESSION_COOKIE_SECURE=true` 依赖 `APP_SECRET_SESSION_COOKIE_TRUSTED_URL` 对应 K8s Secret key；缺失时应用会启动失败，这是安全失败。
- 任何真实 DSN、Redis URL、Cookie Secret、Crypto Secret 都不得出现在 git diff 中。

回滚方式：删除新增 `.gitlab-ci.yml` 和 `.ci/` 文件，或通过反向 MR 回退对应提交。

## Preconditions Before Real Pipeline

- devops-infra MR `!430` 合入 main，或临时流水线明确使用该 feature ref。
- 内部 GitLab remote/project 已创建并推送当前业务分支。
- ACK namespace、`new-api-secret`、RDS、Tair / Redis、release gateway 权限已由运维侧准备。
