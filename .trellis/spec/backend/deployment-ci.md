# 部署与 CI 契约

> 记录 `new-api` 生产部署、GitLab V3 CI、ACK 发布和运行时 Secret/env wiring 的可执行契约。

## Scenario: GitLab V3 接入 ACK 生产发布

### 1. Scope / Trigger

- Trigger: 业务仓新增或修改 `.gitlab-ci.yml`、`.ci/project.yml`、`.ci/components/*.yml`、`.ci/env/*.yml`。
- Trigger: 生产环境运行时变量、数据库连接、Redis 连接、Cookie 安全配置或 K8s Secret 注入方式发生变化。
- Trigger: 构建方式从仓库原生命令切换为 devops-infra V3 Dockerfile 服务组件。
- 不适用：本地 `docker-compose.yml` 示例、GitHub Actions、纯业务 Go / React 逻辑。

### 2. Signatures

业务仓正式 GitLab CI 入口必须保持最小 include：

```yaml
include:
  - project: 'digital-rd-infra/devops-infra'
    file: '/templates/projects/application.yml'
```

`new-api` 的 V3 项目配置入口：

```yaml
project:
  schema_version: v3alpha1
  name: new-api
```

Dockerfile 服务组件入口：

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

必跑 planner 命令：

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
  --output <临时文件> \
  --infra-project digital-rd-infra/devops-infra \
  --infra-ref <包含 go-service 支持的 devops-infra ref>
```

### 3. Contracts

#### GitLab V3 配置

- `.gitlab-ci.yml` 只 include devops-infra 应用模板，不写业务自定义 build/deploy job。
- 正式配置不提交临时 `DEVOPS_INFRA_REF`。只有本地验证或临时调试可以使用 feature ref。
- prod 发布必须是 manual；不得因为只接生产线就改为 auto。
- ACK provider 必须声明 `kubernetes.<env>.release_gateway.function_name` 和 `alias`。

#### Dockerfile 服务

- `new-api` 使用根目录 `Dockerfile` 作为唯一构建入口；不要在 `.ci` 里重复 Go build 或 Bun build。
- `deploy.app_port` 必须显式声明为 3000，使 V3 能派生 TCP startup/readiness 探针。
- `go-service` 组件会复用 generic-image package 模板；渲染后的 `DEPLOY_COMPONENT_TYPE` 保持 `go-service`，不要把它改成 `generic-image` 来迎合断言。

#### 生产运行时环境

非敏感运行时变量通过 `deploy.env` 的 `APP_ENV_` 前缀注入，Pod 内实际变量名由 devops-infra 剥离前缀：

```yaml
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
```

敏感或强环境绑定配置通过 `deploy.secret_env` 的 `APP_SECRET_` 前缀引用 K8s Secret，仓库只提交 `secret:key` 引用：

```yaml
secret_env:
  APP_SECRET_SQL_DSN: "new-api-secret:sql-dsn"
  APP_SECRET_REDIS_CONN_STRING: "new-api-secret:redis-conn-string"
  APP_SECRET_SESSION_SECRET: "new-api-secret:session-secret"
  APP_SECRET_CRYPTO_SECRET: "new-api-secret:crypto-secret"
  APP_SECRET_SESSION_COOKIE_TRUSTED_URL: "new-api-secret:session-cookie-trusted-url"
```

- `SQL_DSN` 必须指向托管数据库；生产不得缺省回退 SQLite。
- `REDIS_CONN_STRING` 必须指向托管 Redis / Tair；生产 ACK 不使用 compose redis。
- `SESSION_COOKIE_SECURE=true` 时，`SESSION_COOKIE_TRUSTED_URL` 必须存在且是精确 HTTPS Origin。
- `SESSION_SECRET` 和 `CRYPTO_SECRET` 必须稳定；后续启多副本时所有 Pod 必须一致。

#### 健康检查

- 首发 ACK 配置优先使用 `deploy.app_port: 3000` 派生的 TCP startup/readiness。
- 不要默认开启 liveness；只有确认启动、迁移、外部依赖抖动不会误杀后再显式配置。
- `/api/status` 可作为发布后 smoke check；`/api/status/test` 需要管理员鉴权且访问 DB，不适合作为 K8s 探针。

### 4. Validation & Error Matrix

| 条件 | 必须结果 |
| --- | --- |
| `.gitlab-ci.yml` 缺失或 include 非 devops-infra 应用模板 | GitLab V3 接入不成立，必须修正后再提交 |
| `.ci/project.yml` 缺少 `schema_version: v3alpha1` | planner validate 必须失败或 Check-All 记录发布配置问题 |
| ACK provider 缺 `release_gateway.function_name` 或 `alias` | planner validate 必须 fail-fast |
| `api` 组件缺 `deploy.app_port` | planner validate 必须 fail-fast，禁止无探针发布 Web 服务 |
| prod overlay 新增 `.ci/env/test.yml` 或 test env_map | 违背 prod-only 范围，必须删除 |
| git diff 出现真实 DSN、Redis URL、Cookie Secret、Crypto Secret、生产域名明文 | 阻断提交，改为 K8s Secret 引用 |
| `SESSION_COOKIE_SECURE=true` 但 Secret 缺 `session-cookie-trusted-url` | 应用启动失败是预期安全失败；上线前必须补 Secret |
| render-child 缺 `package-api`、`deploy-submit-api` 或 `deploy-verify-api` | 发布链路不完整，必须修正 `.ci` |
| render-child 出现额外 `build-api` | Dockerfile 服务职责重复，必须修正组件类型或模板集成 |

### 5. Good/Base/Bad Cases

- Good: `.gitlab-ci.yml` 只 include devops-infra；`.ci/components/api.yml` 声明 `go-service` 和 `app_port: 3000`；`.ci/env/prod.yml` 只放 prod 单副本、资源、`APP_ENV_` 和 `APP_SECRET_` 引用。
- Base: MR 待合入期间，本地 render-child 使用包含 `go-service` 支持的 devops-infra feature ref 验证；正式业务仓配置仍不提交 `DEVOPS_INFRA_REF`。
- Bad: 在业务仓 `.gitlab-ci.yml` 里复制 devops-infra job、写真实 `SQL_DSN` / `REDIS_CONN_STRING`、创建 `.ci/env/test.yml`，或把 `SESSION_COOKIE_TRUSTED_URL` 的真实域名写进仓库。

### 6. Tests Required

每次修改 `.gitlab-ci.yml` 或 `.ci/**` 后至少执行：

- `git diff --check -- .gitlab-ci.yml .ci`：断言 YAML 文件无空白错误。
- `ci_planner.py validate --ref-name prod`：断言配置可被 V3 planner 解析。
- `ci_planner.py render-child --ref-name prod`：断言 child pipeline 可渲染。
- 渲染结果断言：
  - 包含 `package-api`、`deploy-submit-api`、`deploy-verify-api`。
  - 不包含 `build-api`。
  - 包含 `DEPLOY_APP_PORT: "3000"`、`REPLICAS: "1"`、资源变量、`APP_ENV_PORT`、`APP_ENV_SESSION_COOKIE_SECURE`。
  - 包含 `APP_SECRET_SQL_DSN`、`APP_SECRET_REDIS_CONN_STRING`、`APP_SECRET_SESSION_SECRET`、`APP_SECRET_CRYPTO_SECRET`、`APP_SECRET_SESSION_COOKIE_TRUSTED_URL`。
- 敏感值扫描：对 `.gitlab-ci.yml`、`.ci/**` 和任务提交范围搜索真实 DSN、Redis URL、密码、AK、私钥和默认 `random_string`。

真实 GitLab pipeline、ACK Deployment Available、RDS / Redis 连通性、Ingress / 域名 / 证书属于上线后验证，不得在本地 Check-All 中伪报已完成。

### 7. Wrong vs Correct

#### Wrong

```yaml
variables:
  DEVOPS_INFRA_REF: feature-generic-dockerfile-service-v3

build-api:
  script:
    - bun run build
    - go build ./...

deploy:
  variables:
    SQL_DSN: "root:password@tcp(rds.example.com:3306)/new-api"
    REDIS_CONN_STRING: "redis://:password@redis.example.com:6379"
```

问题：业务仓复制发布逻辑、提交临时 infra ref、暴露敏感值，并绕开 V3 的 Secret/env 注入契约。

#### Correct

```yaml
include:
  - project: 'digital-rd-infra/devops-infra'
    file: '/templates/projects/application.yml'
```

```yaml
components:
  api:
    deploy:
      env:
        APP_ENV_SESSION_COOKIE_SECURE: "true"
      secret_env:
        APP_SECRET_SQL_DSN: "new-api-secret:sql-dsn"
        APP_SECRET_REDIS_CONN_STRING: "new-api-secret:redis-conn-string"
        APP_SECRET_SESSION_COOKIE_TRUSTED_URL: "new-api-secret:session-cookie-trusted-url"
```

原因：构建和发布编排归 devops-infra，业务仓只声明组件与运行时契约；敏感值只存在于 ACK K8s Secret。
