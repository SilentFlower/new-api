# new-api 接入 devops-infra V3 ACK 发布链路设计

## Architecture Boundary

本任务只在 `new-api` 业务仓新增 V3 CI 声明文件：

- `.gitlab-ci.yml`：入口文件，include devops-infra 应用模板。
- `.ci/project.yml`：项目、环境、registry、ACK release gateway 和部署策略。
- `.ci/components/api.yml`：单服务组件声明，复用仓库 `Dockerfile`。
- `.ci/env/prod.yml`：生产环境副本、资源、环境变量和 Secret 引用。

构建、镜像推送、ACK 发布提交、部署校验由 devops-infra 模板负责；业务仓不复制模板逻辑。

## Pipeline Flow

1. 内部 GitLab 读取业务仓 `.gitlab-ci.yml`。
2. `.gitlab-ci.yml` include `digital-rd-infra/devops-infra` 的 `/templates/projects/application.yml`。
3. devops-infra planner 读取 `.ci/project.yml`、`.ci/components/*.yml`、`.ci/env/*.yml`。
4. prod 分支解析到 `prod` 环境。
5. `api` 组件按 `go-service` 类型渲染为 generic-image package job，Kaniko 直接使用业务仓根目录 `Dockerfile` 构建镜像。
6. 镜像推送到阿里云 ACR。
7. deploy submit job 通过 ACK release gateway 提交部署。
8. deploy verify job 等待 Deployment Available。

## Configuration Design

### `.gitlab-ci.yml`

正式配置保持最小 include，不提交临时 infra ref：

```yaml
include:
  - project: 'digital-rd-infra/devops-infra'
    file: '/templates/projects/application.yml'
```

### `.ci/project.yml`

生产环境使用 ACR + ACK，初始只走手动 prod 发布：

```yaml
project:
  schema_version: v3alpha1
  name: new-api
  env_map:
    prod:
      - main
      - master
      - prod
  registry:
    prod:
      provider: acr
  kubernetes:
    prod:
      provider: ack
      namespace: xhgj-new-api-prod
      release_gateway:
        function_name: aliyun-ack-releaser
        alias: prod
  deploy_policy:
    prod: manual
```

### `.ci/components/api.yml`

`new-api` 已有 Dockerfile 完成全量构建，因此组件只声明 Dockerfile 服务元数据：

```yaml
component:
  name: api
  type: go-service
  image_name: new-api
  build:
    context: .
    dockerfile: Dockerfile
  deploy:
    app_port: 3000
```

### `.ci/env/prod.yml`

prod overlay 只放运行环境差异和 Secret 引用：

```yaml
components:
  api:
    deploy:
      replicas: 1
      resources:
        cpu_request: "100m"
        cpu_limit: "1000m"
        mem_request: "512Mi"
        mem_limit: "1Gi"
      env:
        APP_ENV_PORT: "3000"
        APP_ENV_TZ: "Asia/Shanghai"
        APP_ENV_ERROR_LOG_ENABLED: "true"
        APP_ENV_BATCH_UPDATE_ENABLED: "true"
        APP_ENV_STREAMING_TIMEOUT: "1800"
        APP_ENV_SQL_MAX_OPEN_CONNS: "50"
        APP_ENV_SQL_MAX_IDLE_CONNS: "10"
        APP_ENV_SYNC_FREQUENCY: "60"
        APP_ENV_SESSION_COOKIE_SECURE: "true"
      secret_env:
        APP_SECRET_SQL_DSN: "new-api-secret:sql-dsn"
        APP_SECRET_REDIS_CONN_STRING: "new-api-secret:redis-conn-string"
        APP_SECRET_SESSION_SECRET: "new-api-secret:session-secret"
        APP_SECRET_CRYPTO_SECRET: "new-api-secret:crypto-secret"
        APP_SECRET_SESSION_COOKIE_TRUSTED_URL: "new-api-secret:session-cookie-trusted-url"
```

## Runtime Contracts

- `APP_ENV_` 前缀由 devops-infra 在部署阶段剥离，Pod 内实际环境变量不带前缀。
- `APP_SECRET_` 前缀由 devops-infra 在部署阶段剥离，并从 K8s Secret `secret:key` 注入。
- `SQL_DSN` 必须指向阿里云 RDS，生产不允许缺省回退 SQLite。
- `REDIS_CONN_STRING` 必须指向阿里云 Tair / Redis，不使用 compose redis。
- `SESSION_SECRET` 和 `CRYPTO_SECRET` 必须长期稳定；后续多副本时所有 Pod 必须一致。
- `SESSION_COOKIE_SECURE=true` 与 `SESSION_COOKIE_TRUSTED_URL` 一起启用，防止生产 refresh/logout OriginGuard 被关闭。

## Health Check Strategy

MVP 使用 `deploy.app_port: 3000` 派生的 TCP startup/readiness 探针，不启 liveness：

- 好处：可以避免首发阶段因启动慢、DB 初始化慢或瞬时上游抖动导致 liveness 误杀。
- 代价：TCP 探针只能确认端口监听，不能确认业务 HTTP 或 DB 可用；首次发布后需要人工或发布脚本额外访问 `/api/status` 做 smoke check。

后续如果已有稳定生产观测，再考虑把 HTTP readiness 切到 `/api/status`。`/api/status/test` 需要管理员鉴权且访问 DB，不适合作为探针。

## Operational Dependencies

进入真实 GitLab/ACK 发布前，需要外部资源已就绪：

- devops-infra MR `!430` 已合入 main。
- 内部 GitLab 项目和 remote 已创建。
- ACK namespace `xhgj-new-api-prod` 存在。
- K8s Secret `new-api-secret` 存在，并包含 PRD R5 列出的键。
- RDS 和 Tair / Redis 网络、安全组、白名单、账号权限已允许 ACK Pod 访问。
- ACK release gateway `aliyun-ack-releaser:prod` 有发布该 namespace 的权限。

## Rollback

本任务对运行时系统无直接副作用。回滚方式是删除或回退新增的 `.gitlab-ci.yml` 与 `.ci/` 文件。

如果 CI 配置已进入内部 GitLab，回滚应通过业务 MR 反向提交完成；不要直接修改 devops-infra main 或生产 ACK 资源。
