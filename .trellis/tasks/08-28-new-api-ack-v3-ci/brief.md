# Brief — new-api 接入 devops-infra V3 ACK 发布链路

## Goal

- 在 `new-api` 业务仓补齐内部 GitLab V3 发布配置，使生产分支可以复用 `digital-rd-infra/devops-infra` 模板完成 Dockerfile 镜像构建、阿里云 ACR 推送、ACK 发布提交与部署校验。

## Scope

- 新增根目录 `.gitlab-ci.yml`，只 include devops-infra 的 `/templates/projects/application.yml`。
- 新增 `.ci/project.yml`，配置 prod-only 环境映射、ACR、ACK、`xhgj-new-api-prod` namespace、`aliyun-ack-releaser:prod` release gateway 和 prod manual deploy policy。
- 新增 `.ci/components/api.yml`，声明单个 `api` 组件，类型为 `go-service`，镜像名 `new-api`，复用仓库根目录 `Dockerfile`，部署端口 3000。
- 新增 `.ci/env/prod.yml`，声明单副本、小规格资源、`APP_ENV_` 运行参数和 `APP_SECRET_` Secret 引用。
- 用 devops-infra MR `!430` 对应本地 planner 验证 `prod` 配置，并渲染 child pipeline 检查关键 job 和变量。

## Non-Goals

- 不创建或修改阿里云 RDS、Tair / Redis、ACK namespace、K8s Secret、Ingress、域名或证书。
- 不创建 GitLab 项目、不配置 GitLab remote、不推送 GitLab MR。
- 不接入测试线，不创建 `.ci/env/test.yml`，不走 K3s 测试发布流程。
- 不启多副本，不做 HPA、PDB、灰度、蓝绿或 canary。
- 不修改应用业务代码、Dockerfile 构建逻辑或 docker-compose 本地部署示例。
- 不合并或替代 devops-infra MR `!430` 的审核流程。

## Key Decisions

- 生产数据库和 Redis 使用阿里云托管 RDS / Tair 或 Redis，通过 K8s Secret 注入；不在 ACK 里用 docker-compose 拉起 postgres/redis。
- 初始只做 prod 环境，`replicas: 1`，生产部署保持 manual。
- `SESSION_COOKIE_SECURE` 按生产安全要求设为 `true`；`SESSION_COOKIE_TRUSTED_URL` 不把具体域名写死在仓库，改从 `new-api-secret:session-cookie-trusted-url` 注入。
- 健康检查 MVP 使用 `deploy.app_port: 3000` 派生的 TCP startup/readiness，不显式启用 liveness；`/api/status` 只作为发布后人工 HTTP smoke check。
- 正式 `.gitlab-ci.yml` 不提交临时 `DEVOPS_INFRA_REF`，但本地验证可以用 `--infra-ref feature-generic-dockerfile-service-v3`。

## Key Context

- 当前分支：`feat/ack-deployment`。
- 当前仓库 remote 只有 GitHub `origin`，尚未配置内部 GitLab remote。
- 当前仓库没有 `.gitlab-ci.yml` 和 `.ci/` 目录。
- `Dockerfile` 已完成前端和 Go 后端构建，并暴露 3000 端口。
- `main.go` 支持 `PORT`，未配置时默认监听 3000。
- `SQL_DSN`、`REDIS_CONN_STRING`、`SESSION_SECRET`、`CRYPTO_SECRET`、`SESSION_COOKIE_SECURE`、`SESSION_COOKIE_TRUSTED_URL` 是本次生产运行配置的关键环境变量。
- 任务创建前已有 3 个无关 `.trellis/spec/...` 脏文件，实现时必须保留且不纳入本任务变更。

## Risks / Deferred

- devops-infra MR `!430` 未合入 main 前，内部 GitLab 默认 `DEVOPS_INFRA_REF=main` 会缺少 `go-service` 支持；本任务先用本地 planner 验证。
- 真实流水线触发需要后续创建内部 GitLab 项目/remote 并推送业务分支。
- ACK namespace、`new-api-secret`、RDS、Tair / Redis、release gateway 权限、Ingress、域名和证书属于后续资源准备与发布验证。
- 首次 ACK 发布前必须确保 `new-api-secret` 中存在 `session-cookie-trusted-url` 且值为真实 HTTPS Origin；缺失时应用启动失败是预期安全失败。

## Acceptance

- `.gitlab-ci.yml` 只 include devops-infra 应用模板。
- `.ci/project.yml` 只定义 prod 环境，registry 为 ACR，Kubernetes provider 为 ACK，namespace 为 `xhgj-new-api-prod`，release gateway 为 `aliyun-ack-releaser:prod`，生产 deploy policy 为 manual。
- `.ci/components/api.yml` 定义单个 `api` / `go-service` 组件，使用根目录 `Dockerfile`，部署端口 3000。
- `.ci/env/prod.yml` 只定义 prod overlay；不存在 `.ci/env/test.yml`。
- prod overlay 包含 `replicas: 1`、约定资源、`APP_ENV_PORT`、`APP_ENV_SESSION_COOKIE_SECURE`、`APP_SECRET_SQL_DSN`、`APP_SECRET_REDIS_CONN_STRING`、`APP_SECRET_SESSION_SECRET`、`APP_SECRET_CRYPTO_SECRET`、`APP_SECRET_SESSION_COOKIE_TRUSTED_URL`。
- git diff 不包含真实 RDS DSN、Redis URL、Cookie Secret、Crypto Secret 或生产域名明文。
- planner `validate --ref-name prod` 通过。
- planner `render-child --ref-name prod` 输出包含 `package-api`、`deploy-submit-api`、`deploy-verify-api`，不包含额外 `build-api` job。
- 本任务不修改 `Dockerfile`、`docker-compose.yml`、业务 Go/React 代码，也不混入任务创建前已有的无关 spec 脏文件。

## Next Step

- 你确认本 Brief 后，运行 `task.py start` 进入实现阶段，并按 `implement.md` 新增 CI 配置文件与 planner 校验。
