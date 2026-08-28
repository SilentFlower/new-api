# new-api 接入 devops-infra V3 ACK 发布链路

## Goal

在 `new-api` 业务仓补齐内部 GitLab V3 发布配置，使生产分支可以复用 `digital-rd-infra/devops-infra` 的统一模板完成 Dockerfile 镜像构建、阿里云 ACR 推送、ACK 发布提交流转与部署校验。

本任务只覆盖业务仓第 4 项接入工作：新增 CI 配置和生产环境 overlay。生产数据库和 Redis 使用阿里云托管 RDS / Tair 或 Redis，以 K8s Secret 注入；测试线、本地 compose 依赖和多副本扩容不纳入。

## Confirmed Facts

- 当前工作分支是 `feat/ack-deployment`。
- 当前仓库 remote 只有 `origin=https://github.com/SilentFlower/new-api.git`，尚未配置内部 GitLab remote；真实 GitLab pipeline 需要后续把仓库推到内部 GitLab 项目后触发。
- 当前仓库没有 `.gitlab-ci.yml` 和 `.ci/` 目录。
- `Dockerfile` 已能从仓库根目录构建前端和 Go 后端，并在运行镜像中 `EXPOSE 3000`、`ENTRYPOINT ["/new-api"]`。
- `main.go` 优先读取 `PORT`，未配置时默认监听 3000。
- `docker-compose.yml` 中的 postgres / redis 仅用于本地或单机部署示例，生产 ACK 不使用 compose 拉起数据库或 Redis。
- 运行时生产依赖通过环境变量注入：`SQL_DSN`、`REDIS_CONN_STRING`、`SESSION_SECRET`、`CRYPTO_SECRET`、`SESSION_COOKIE_SECURE`、`SESSION_COOKIE_TRUSTED_URL` 等。
- `SESSION_COOKIE_SECURE=true` 时，`SESSION_COOKIE_TRUSTED_URL` 必须配置精确 HTTPS Origin；否则应用启动会失败。
- `deploy.app_port` 在 devops-infra V3 中决定 K8s 默认 TCP startup/readiness 探针；不显式开启 `deploy.health.enabled=true` 时不会加 liveness。
- `devops-infra` 对 `generic-image` / `go-service` 的支持仍在 MR `!430` 待审核；业务仓最终使用 `DEVOPS_INFRA_REF=main` 依赖该 MR 先合入。
- 本任务创建前已有 3 个与本任务无关的 `.trellis/spec/...` 脏文件，需要保留且不纳入本任务实现 diff。

## Requirements

### R1. GitLab CI 入口

新增根目录 `.gitlab-ci.yml`，只 include 内部 devops-infra 应用模板：

- `project: digital-rd-infra/devops-infra`
- `file: /templates/projects/application.yml`

不得在业务仓新增自定义 build/deploy job，也不得把临时 `DEVOPS_INFRA_REF=feature-generic-dockerfile-service-v3` 提交进正式配置。临时 infra ref 只允许用于本地 planner 验证或临时人工调试。

### R2. V3 项目配置

新增 `.ci/project.yml`：

- `project.schema_version: v3alpha1`
- `project.name: new-api`
- 只配置 `prod` 环境映射，允许 `main`、`master`、`prod` 分支触发生产环境解析。
- `registry.prod.provider: acr`
- `kubernetes.prod.provider: ack`
- `kubernetes.prod.namespace: xhgj-new-api-prod`
- `kubernetes.prod.release_gateway.function_name: aliyun-ack-releaser`
- `kubernetes.prod.release_gateway.alias: prod`
- `deploy_policy.prod: manual`，生产发布必须人工点部署。

### R3. Dockerfile 服务组件

新增 `.ci/components/api.yml`：

- 组件名使用 `api`。
- 组件类型使用 `go-service`，复用 devops-infra 的 `generic-image` 打包模板。
- 镜像名使用 `new-api`。
- 构建上下文为仓库根目录 `.`。
- Dockerfile 路径为 `Dockerfile`。
- 部署端口显式声明 `deploy.app_port: 3000`。
- 不新增 Go build 命令、Bun build 命令或额外 runner 脚本；构建流程完全由现有 Dockerfile 承担。

### R4. 生产环境 overlay

新增 `.ci/env/prod.yml`，仅覆盖 `api` 组件：

- `replicas: 1`，符合当前 ACK 资源短缺和暂不启多副本的方向。
- 资源先用保守小规格：`cpu_request=100m`、`cpu_limit=1000m`、`mem_request=512Mi`、`mem_limit=1Gi`。
- 运行时非敏感环境变量通过 `deploy.env` 的 `APP_ENV_` 前缀注入：
  - `PORT=3000`
  - `TZ=Asia/Shanghai`
  - `ERROR_LOG_ENABLED=true`
  - `BATCH_UPDATE_ENABLED=true`
  - `STREAMING_TIMEOUT=1800`
  - `SQL_MAX_OPEN_CONNS=50`
  - `SQL_MAX_IDLE_CONNS=10`
  - `SYNC_FREQUENCY=60`
  - `SESSION_COOKIE_SECURE=true`
- 敏感或环境强绑定配置通过 `deploy.secret_env` 的 `APP_SECRET_` 前缀从 K8s Secret 注入：
  - `SQL_DSN`
  - `REDIS_CONN_STRING`
  - `SESSION_SECRET`
  - `CRYPTO_SECRET`
  - `SESSION_COOKIE_TRUSTED_URL`

### R5. Secret 和外部资源边界

业务仓只提交 Secret 引用，不提交真实密钥、DSN、Redis URL 或生产域名。

部署前需要在 ACK 命名空间 `xhgj-new-api-prod` 准备 K8s Secret `new-api-secret`，至少包含：

- `sql-dsn`：阿里云 RDS 的 MySQL 或 PostgreSQL 主库连接串；生产不得回退 SQLite。
- `redis-conn-string`：阿里云 Tair / Redis 连接 URL。
- `session-secret`：生产鉴权签名密钥。
- `crypto-secret`：Redis 缓存键 HMAC 密钥。
- `session-cookie-trusted-url`：生产 refresh/logout 允许的精确 HTTPS Origin，多个 Origin 用英文逗号分隔。

### R6. 健康检查策略

本任务 MVP 使用 V3 默认 TCP startup/readiness 探针：只依赖 `deploy.app_port: 3000`，不显式开启 liveness。

`/api/status` 可作为部署后的人工 HTTP smoke check；`/api/status/test` 需要管理员鉴权且会触发 DB ping，不适合作为 K8s 探针。

### R7. 验证要求

在 devops-infra MR `!430` 合入前，本地使用 `/root/project/devops-infra-harness/devops-infra/scripts/ci_planner.py` 和临时 `--infra-ref feature-generic-dockerfile-service-v3` 做 planner 验证。

在 MR `!430` 合入后，业务仓正式 `.gitlab-ci.yml` 应通过默认 `DEVOPS_INFRA_REF=main` 解析。

## Acceptance Criteria

- [ ] 根目录存在 `.gitlab-ci.yml`，且只 include `digital-rd-infra/devops-infra` 的 `/templates/projects/application.yml`。
- [ ] `.ci/project.yml` 只定义 `prod` 环境，registry 为 ACR，Kubernetes provider 为 ACK，namespace 为 `xhgj-new-api-prod`，release gateway 为 `aliyun-ack-releaser:prod`，生产 deploy policy 为 manual。
- [ ] `.ci/components/api.yml` 定义单个 `api` 组件，类型为 `go-service`，复用仓库根目录 `Dockerfile`，部署端口为 3000。
- [ ] `.ci/env/prod.yml` 只定义 prod overlay，不存在 `.ci/env/test.yml`。
- [ ] prod overlay 中副本数为 1，并包含约定资源、非敏感 `APP_ENV_` 变量和 `APP_SECRET_` Secret 引用。
- [ ] 仓库提交内容不包含真实 RDS DSN、Redis URL、Cookie Secret、Crypto Secret 或生产域名明文。
- [ ] 本地 planner `validate` 对 `--ref-name prod` 通过。
- [ ] 本地 planner `render-child` 输出包含 `package-api`、`deploy-submit-api`、`deploy-verify-api`，且不包含额外 `build-api` job。
- [ ] 本地 planner 渲染结果包含 `REPLICAS: "1"`、`DEPLOY_APP_PORT: "3000"`、生产资源限制、`APP_ENV_PORT`、`APP_ENV_SESSION_COOKIE_SECURE`、`APP_SECRET_SQL_DSN`、`APP_SECRET_REDIS_CONN_STRING`、`APP_SECRET_SESSION_SECRET`、`APP_SECRET_CRYPTO_SECRET`、`APP_SECRET_SESSION_COOKIE_TRUSTED_URL`。
- [ ] 本任务实现 diff 不改动 `Dockerfile`、`docker-compose.yml`、业务 Go/React 代码，也不纳入任务创建前已有的 `.trellis/spec/...` 脏文件。

## Non-Goals

- 不创建或修改阿里云 RDS、Tair / Redis、ACK namespace、K8s Secret、Ingress、域名或证书。
- 不创建 GitLab 项目、不配置 GitLab remote、不推送 GitLab MR。
- 不接入测试线，不创建 `.ci/env/test.yml`，不走 K3s 测试发布流程。
- 不启多副本，不做 HPA、PDB、灰度、蓝绿或 canary。
- 不修改应用业务代码、Dockerfile 构建逻辑或 docker-compose 本地部署示例。
- 不合并或替代 devops-infra MR `!430` 的审核流程。

## Risks / Deferred Items

- devops-infra MR `!430` 未合入前，内部 GitLab 使用默认 `DEVOPS_INFRA_REF=main` 会缺少 `go-service` 支持；本任务只能用本地 planner 或临时 infra ref 验证。
- 当前仓库没有内部 GitLab remote；真实流水线触发和业务 MR 需要后续完成 GitLab 项目/remote 准备。
- `new-api-secret` 的真实键值、RDS 白名单、Redis 网络连通性、ACK release gateway 权限、Ingress/域名/证书均属于后续上线资源准备与发布验证。
- 首次 ACK 发布前必须确保 `session-cookie-trusted-url` 填入真实 HTTPS Origin；否则 `SESSION_COOKIE_SECURE=true` 会使应用启动失败，这是预期的安全失败。
