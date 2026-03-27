# 目录结构

> 本项目后端代码的组织方式和文件布局规范。

---

## 概述

本项目是一个 Go 语言构建的 AI API 网关/代理，采用**扁平分层架构**：

```
Router -> Controller -> Service -> Model
```

Go 模块路径：`github.com/QuantumNous/new-api`，Go 版本：1.22+

每个顶级目录即为一个独立的 Go 包，子包仅在需要进一步组织时创建。

---

## 目录布局

```
new-api/
├── main.go                # 应用入口；初始化资源、配置 Gin 引擎、嵌入 web/dist
├── go.mod / go.sum        # Go 模块定义与依赖锁定
├── VERSION                # 版本号文件
├── Dockerfile             # 多阶段构建（Bun 前端 + Go 后端 + Debian 运行时）
│
├── router/                # HTTP 路由定义（按域名拆分）
│   ├── main.go            #   SetRouter() 入口，调用各子路由
│   ├── api-router.go      #   /api/* 管理 API 路由
│   ├── relay-router.go    #   /v1/* AI API 转发路由
│   ├── dashboard.go       #   /dashboard/billing/* 仪表盘路由
│   ├── video-router.go    #   /v1/video/* 视频生成路由
│   └── web-router.go      #   静态文件服务（嵌入式 React SPA）
│
├── controller/            # 请求处理器（55+ 文件）
│   ├── user.go            #   用户管理
│   ├── token.go           #   API Token 管理
│   ├── channel.go         #   渠道管理
│   ├── relay.go           #   AI API 转发控制器（核心）
│   ├── log.go             #   日志查询
│   ├── model.go           #   模型管理
│   ├── pricing.go         #   定价管理
│   ├── subscription.go    #   订阅管理
│   ├── task.go            #   异步任务管理
│   └── ...
│
├── service/               # 业务逻辑层（47+ 文件）
│   ├── billing.go         #   计费结算
│   ├── billing_session.go #   计费会话管理
│   ├── quota.go           #   额度管理
│   ├── channel_select.go  #   渠道选择策略
│   ├── channel_affinity.go#   渠道亲和性
│   ├── tokenizer.go       #   Token 计数
│   ├── error.go           #   错误处理与包装
│   └── ...
│
├── model/                 # 数据模型与数据库访问（GORM）（34 文件）
│   ├── main.go            #   数据库初始化、迁移、特殊变量定义
│   ├── user.go            #   用户模型
│   ├── channel.go         #   渠道模型
│   ├── token.go           #   Token 模型
│   ├── log.go             #   日志模型（支持独立 LOG_DB）
│   ├── ability.go         #   能力（模型-渠道映射）
│   ├── utils.go           #   批量更新机制
│   ├── db_time.go         #   数据库时间查询（多数据库适配）
│   └── ...
│
├── relay/                 # AI API 转发/代理核心
│   ├── compatible_handler.go    # OpenAI 兼容处理器
│   ├── claude_handler.go        # Claude 原生 API 处理器
│   ├── gemini_handler.go        # Gemini 原生 API 处理器
│   ├── responses_handler.go     # OpenAI Responses API 处理器
│   ├── relay_adaptor.go         # 适配器工厂（渠道类型 -> 适配器映射）
│   ├── relay_task.go            # 异步任务转发（视频、音乐生成）
│   ├── websocket.go             # WebSocket 转发（实时对话）
│   ├── common/                  # 转发共享类型
│   │   ├── relay_info.go        #   RelayInfo 核心结构
│   │   └── override.go          #   参数覆盖逻辑
│   ├── constant/                # 转发模式常量
│   │   └── relay_mode.go
│   └── channel/                 # 供应商适配器（32+ 个）
│       ├── adapter.go           #   Adaptor 接口定义（13 个方法）
│       ├── api_request.go       #   共享 HTTP 请求助手
│       ├── openai/              #   OpenAI 适配器
│       ├── claude/              #   Claude/Anthropic 适配器
│       ├── gemini/              #   Gemini/Google 适配器
│       ├── aws/                 #   AWS Bedrock 适配器
│       ├── vertex/              #   Google Vertex 适配器
│       ├── ali/                 #   阿里/DashScope 适配器
│       ├── baidu/               #   百度/文心 适配器
│       ├── deepseek/            #   DeepSeek 适配器
│       ├── ...                  #   更多供应商...
│       └── task/                #   任务类适配器（视频/音乐生成）
│           ├── suno/            #     Suno（音乐）
│           ├── kling/           #     Kling（视频）
│           ├── sora/            #     Sora（视频）
│           └── ...
│
├── middleware/             # HTTP 中间件（21 文件）
│   ├── auth.go            #   认证（JWT Token、用户、管理员）
│   ├── distributor.go     #   渠道分发/选择
│   ├── rate-limit.go      #   全局 API 限流
│   ├── model-rate-limit.go#   按模型限流
│   ├── cors.go            #   CORS 处理
│   ├── logger.go          #   请求日志
│   ├── recover.go         #   Panic 恢复
│   ├── request-id.go      #   请求 ID 生成
│   ├── i18n.go            #   语言检测
│   └── ...
│
├── common/                # 共享工具库
│   ├── json.go            #   JSON 封装（[!] 必须使用，禁止直接 encoding/json）
│   ├── database.go        #   数据库类型标志（UsingSQLite, UsingPostgreSQL, UsingMySQL）
│   ├── gin.go             #   Gin 响应助手（ApiError, ApiSuccess, ApiErrorI18n）
│   ├── sys_log.go         #   系统日志（SysLog, SysError, FatalLog）
│   ├── str.go             #   字符串工具（MaskSensitiveInfo 等）
│   ├── redis.go           #   Redis 操作
│   ├── init.go            #   环境初始化
│   ├── constants.go       #   全局常量
│   ├── body_storage.go    #   请求体存储（内存/磁盘）
│   └── limiter/           #   Redis 限流器（含 Lua 脚本）
│
├── dto/                   # 数据传输对象（28 文件）
│   ├── openai_request.go  #   OpenAI 请求 DTO（[!] 可选字段用指针类型）
│   ├── openai_response.go #   OpenAI 响应 DTO
│   ├── claude.go          #   Claude DTO
│   ├── gemini.go          #   Gemini DTO
│   ├── error.go           #   错误响应 DTO
│   ├── task.go            #   任务 DTO
│   └── ...
│
├── constant/              # 常量定义（14 文件）
│   ├── api_type.go        #   API 类型
│   ├── channel_type.go    #   渠道类型
│   ├── context_key.go     #   Context Key
│   └── ...
│
├── types/                 # 核心类型定义（9 文件）
│   ├── error.go           #   NewAPIError 统一错误类型
│   ├── channel_error.go   #   渠道错误
│   └── ...
│
├── setting/               # 配置管理
│   ├── ratio_setting/     #   模型/分组比率配置
│   └── performance_setting/ # 性能配置
│
├── i18n/                  # 后端国际化
│   ├── i18n.go            #   初始化与翻译函数
│   ├── keys.go            #   消息 Key 常量
│   └── locales/           #   翻译文件（en.yaml, zh-CN.yaml, zh-TW.yaml）
│
├── oauth/                 # OAuth 提供商实现
│   ├── github.go          #   GitHub OAuth
│   ├── discord.go         #   Discord OAuth
│   ├── oidc.go            #   OIDC 通用
│   └── ...
│
├── logger/                # 日志设置
│   └── logger.go          #   LogInfo, LogWarn, LogError, LogDebug + 日志轮转
│
├── pkg/                   # 内部库包
│   ├── cachex/            #   混合缓存（内存 + Redis）
│   └── ionet/             #   IO.net 部署客户端
│
├── web/                   # React 前端（Vite + Semi Design UI）
│   ├── package.json       #   依赖与脚本（使用 Bun）
│   ├── src/
│   │   ├── pages/         #     页面组件（PascalCase 目录）
│   │   ├── components/    #     可复用组件（按领域分子目录）
│   │   ├── helpers/       #     工具函数（API 调用、认证、渲染等）
│   │   ├── constants/     #     前端常量
│   │   └── i18n/          #     国际化（i18next, 7 种语言）
│   └── ...
│
├── bin/                   # 迁移脚本与工具
├── .github/               # GitHub Actions 工作流 + Issue/PR 模板
└── .claude/               # Claude Code Agent 配置
```

---

## 模块组织

### 新增功能的文件放置规则

| 代码类型 | 放置位置 | 示例 |
|----------|---------|------|
| HTTP 路由 | `router/` 中对应的路由文件 | API 路由 -> `api-router.go` |
| 请求处理 | `controller/` 按领域实体命名 | `controller/pricing.go` |
| 业务逻辑 | `service/` 按功能命名 | `service/billing.go` |
| 数据模型 | `model/` 按实体命名 | `model/subscription.go` |
| 请求/响应结构 | `dto/` 按供应商或功能命名 | `dto/openai_request.go` |
| 常量定义 | `constant/` 按类别命名 | `constant/channel_type.go` |
| 核心类型 | `types/` | `types/error.go` |
| 共享工具 | `common/` | `common/redis.go` |
| 中间件 | `middleware/` | `middleware/rate-limit.go` |

### 新增供应商适配器

每个供应商在 `relay/channel/` 下有独立的子包，典型文件结构：

```
relay/channel/{provider}/
├── adaptor.go       # 实现 Adaptor 接口
├── constants.go     # 模型列表和供应商常量
├── dto.go           # 供应商专属请求/响应类型（如需）
└── relay-*.go       # 请求/响应转换逻辑（如复杂）
```

任务类供应商（视频/音乐生成）放在 `relay/channel/task/{provider}/` 下。

---

## 命名规范

### Go 后端文件命名

| 规范 | 说明 | 示例 |
|------|------|------|
| **snake_case**（推荐） | 新文件应使用下划线分隔 | `channel_cache.go`, `token_counter.go`, `relay_info.go` |
| **kebab-case**（历史遗留） | 部分旧文件使用短横线分隔 | `rate-limit.go`, `go-channel.go` |
| **单词** | 简单实体名直接使用 | `channel.go`, `user.go`, `token.go` |

**规则**：新文件统一使用 **snake_case**。

### React 前端文件命名

| 类型 | 规范 | 示例 |
|------|------|------|
| 组件文件 | PascalCase | `LoginForm.jsx`, `DashboardHeader.jsx` |
| 工具文件 | camelCase | `api.js`, `token.js`, `quota.js` |
| 常量文件 | camelCase + `.constants` | `channel.constants.js` |
| 页面目录 | PascalCase | `Home/`, `Channel/`, `Setting/` |

---

## 路由组织模式

路由采用**按领域拆分**模式，在 `router/main.go` 的 `SetRouter()` 中依次调用：

| 路由函数 | 文件 | 前缀 | 用途 |
|----------|------|------|------|
| `SetApiRouter()` | `api-router.go` | `/api/*` | 管理 API（用户、Token、渠道、设置等） |
| `SetDashboardRouter()` | `dashboard.go` | `/dashboard/billing/*` | 仪表盘计费查询 |
| `SetRelayRouter()` | `relay-router.go` | `/v1/*` | AI API 转发（OpenAI/Claude/Gemini 兼容） |
| `SetVideoRouter()` | `video-router.go` | `/v1/video/*` | 视频生成 API |
| `SetWebRouter()` | `web-router.go` | `/` | React SPA 静态文件 |

### 中间件链模式

路由通过 `.Use()` 链式调用中间件，典型顺序：

```go
relayRouter.Use(
    middleware.RouteTag("relay"),          // 路由标签（用于指标）
    middleware.TokenAuth(),                // Token 认证
    middleware.SystemPerformanceCheck(),   // 系统负载检查
    middleware.ModelRequestRateLimit(),    // 模型级限流
    middleware.Distribute(),              // 渠道选择分发
)
```

---

## 示例：添加新的 API 端点

以添加 "定价管理" 功能为例，涉及文件：

1. **路由**: `router/api-router.go` - 添加 `/api/pricing/*` 路由
2. **控制器**: `controller/pricing.go` - 处理 HTTP 请求
3. **服务**: `service/pricing.go` - 业务逻辑（如有）
4. **模型**: `model/pricing.go` - GORM 模型与数据库操作
5. **DTO**: `dto/pricing.go` - 请求/响应结构体
