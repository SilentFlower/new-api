# 代码质量标准

> 本项目的禁止模式、必需模式、测试要求和代码风格规范。

---

## 概述

- **后端**: Go 1.22+, Gin Web 框架, GORM v2 ORM
- **前端**: React 18, Vite, Semi Design UI, Bun 包管理器
- **代码检查**: 后端无 linter 配置；前端有 ESLint + Prettier
- **CI/CD**: GitHub Actions（仅构建，不运行测试或 lint）
- **测试**: 使用 `testify` 库，19 个测试文件，CI 中不自动运行

---

## 禁止的模式 [!] 关键

### 规则 1: JSON 包 — 必须使用 `common/json.go`

```go
// [X] 禁止：直接调用 encoding/json
import "encoding/json"
data, _ := json.Marshal(obj)
json.Unmarshal(data, &obj)
json.NewDecoder(reader).Decode(&obj)

// [OK] 正确：使用 common 封装
import "github.com/QuantumNous/new-api/common"
data, _ := common.Marshal(obj)
common.Unmarshal(data, &obj)
common.DecodeJson(reader, &obj)
common.UnmarshalJsonStr(jsonStr, &obj)
```

**例外**：类型引用如 `json.RawMessage`、`json.Number` 允许从 `encoding/json` 导入。

参考：`common/json.go` 提供 5 个封装函数。

### 规则 2: 数据库兼容性 — 三库同时支持

所有数据库代码必须同时兼容 SQLite、MySQL、PostgreSQL。详见 [database-guidelines.md](./database-guidelines.md)。

| 禁止 | 替代 |
|------|------|
| MySQL 专用函数不提供 PG 兼容 | 提供对应的 PG 分支 |
| PostgreSQL 专用操作符 | 使用通用 SQL |
| SQLite 中 `ALTER COLUMN` | `ALTER TABLE ... ADD COLUMN` |
| 数据库专用类型无降级 | 用 `TEXT` 代替 `JSONB` |

### 规则 3: 前端包管理器 — 使用 Bun

```bash
# [X] 禁止
npm install / yarn add / pnpm install

# [OK] 正确
bun install / bun add / bun run dev / bun run build
```

### 规则 4: 新渠道 StreamOptions 支持

添加新渠道适配器时：
- 确认供应商是否支持 `StreamOptions`
- 如支持，将渠道添加到 `streamSupportedChannels`

### 规则 5: 受保护的项目信息

项目名称和组织标识为受保护信息，**禁止**修改、删除或替换任何相关引用。

### 规则 6: 上游转发请求 DTO — 保留显式零值

```go
// [X] 禁止：非指针标量 + omitempty（零值会被静默丢弃）
type Request struct {
    TopP    float64 `json:"top_p,omitempty"`     // 0.0 会被丢弃！
    Stream  bool    `json:"stream,omitempty"`     // false 会被丢弃！
}

// [OK] 正确：指针类型 + omitempty
type Request struct {
    TopP    *float64 `json:"top_p,omitempty"`    // nil=省略, &0.0=发送
    Stream  *bool    `json:"stream,omitempty"`    // nil=省略, &false=发送
}
```

语义保证：
- 客户端 JSON 中字段缺失 → `nil` → marshal 时省略
- 客户端显式设置为零值 → 非 `nil` 指针 → 必须发送到上游

参考：`dto/openai_request.go` 和 `dto/openai_request_zero_value_test.go`。

### DTO 参数增加规范

```go
// 无引用的参数必须使用 json.RawMessage 类型，并添加 omitempty 标签
UnreferencedField json.RawMessage `json:"field_name,omitempty"`
```

---

## 必需的模式

### Gin 控制器签名

控制器使用独立导出函数，不使用结构体方法：

```go
// [OK] 正确
func GetPerformanceStats(c *gin.Context) {
    // 处理逻辑
    common.ApiSuccess(c, data)
}

// [X] 禁止：不要用方法
type Controller struct{}
func (ctrl *Controller) GetStats(c *gin.Context) { ... }
```

### 中间件工厂模式

中间件返回 `gin.HandlerFunc` 闭包：

```go
func SystemPerformanceCheck() gin.HandlerFunc {
    return func(c *gin.Context) {
        // 前置逻辑
        c.Next()
        // 后置逻辑
    }
}
```

### 管理 API 响应格式

```go
// 成功
common.ApiSuccess(c, data)
// 返回: {"success": true, "message": "", "data": {...}}

// 错误
common.ApiError(c, err)
common.ApiErrorI18n(c, i18n.MsgSomeError)
// 返回: {"success": false, "message": "错误信息"}
```

### 错误处理 — 分层返回

```go
// Model 层：返回标准 error
func GetUser(id int) (*User, error) {
    var user User
    err := DB.First(&user, id).Error
    return &user, err
}

// Service 层：转换为 *types.NewAPIError
func DoSomething() *types.NewAPIError {
    _, err := model.GetUser(id)
    if err != nil {
        return types.NewError(err, types.ErrorCodeQueryDataError)
    }
    return nil
}
```

---

## 测试规范

### 测试库

主要使用 `github.com/stretchr/testify`（`require` 和 `assert` 包）：

```go
import (
    "testing"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestSomething(t *testing.T) {
    result, err := DoSomething()
    require.NoError(t, err)
    assert.Equal(t, expected, result)
}
```

### 测试模式

#### 表驱动测试（推荐）

```go
// 参考: common/url_validator_test.go
func TestURLValidator(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected bool
    }{
        {"正常 URL", "https://example.com", true},
        {"空字符串", "", false},
        // ...
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := ValidateURL(tt.input)
            assert.Equal(t, tt.expected, result)
        })
    }
}
```

#### 内存 SQLite 数据库测试

```go
// 参考: model/task_cas_test.go
func TestMain(m *testing.M) {
    db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
    DB = db
    common.UsingSQLite = true
    DB.AutoMigrate(&Task{})
    os.Exit(m.Run())
}
```

#### Gin 测试上下文

```go
// 参考: controller/token_test.go
w := httptest.NewRecorder()
c, _ := gin.CreateTestContext(w)
c.Request = httptest.NewRequest("GET", "/api/token/", nil)
c.Set("id", userId)
c.Set("role", model.RoleCommonUser)
```

#### 测试辅助函数

```go
func setupTestDB(t *testing.T) {
    t.Helper()  // 确保堆栈跟踪干净
    // 初始化逻辑
    t.Cleanup(func() {
        // 清理逻辑
    })
}
```

### 现有测试覆盖范围

| 领域 | 测试文件数 | 关注点 |
|------|-----------|--------|
| Controller | 2 | Token CRUD、渠道更新 |
| Common | 1 | URL 验证（安全） |
| DTO | 2 | 指针零值保留、Gemini 配置 |
| Model | 1 | Task CAS 并发 |
| Relay | 5 | Claude/AWS/Gemini 响应解析、流扫描器 |
| Service | 5 | 计费、渠道亲和性、错误处理、额度计算 |
| Setting | 1 | 状态码范围解析 |

---

## 构建流程

### Docker 多阶段构建

```dockerfile
# 阶段 1: 前端构建（oven/bun:1）
bun install && bun run build

# 阶段 2: 后端构建（golang:1.26.1-alpine）
CGO_ENABLED=0 GOEXPERIMENT=greenteagc go build -ldflags "-s -w" -o new-api

# 阶段 3: 运行时（debian:bookworm-slim）
```

### 构建标志

- `CGO_ENABLED=0`：纯静态编译
- `GOEXPERIMENT=greenteagc`：启用 Green Tea GC 实验特性
- `-ldflags "-s -w"`：去除调试信息和符号表
- 版本号注入：通过 `-ldflags` 注入 `common.Version`

### CI/CD（GitHub Actions）

| 工作流 | 触发条件 | 内容 |
|--------|---------|------|
| Release | Tag 推送 | 构建多平台二进制（Linux/macOS/Windows） |
| Docker | Push | 多架构 Docker 镜像 + cosign 签名 |
| Electron | 手动 | Electron 桌面应用构建 |

**注意**：CI 中**不运行** `go test`、`go vet` 或 `golangci-lint`。前端构建设置了 `DISABLE_ESLINT_PLUGIN='true'`。

---

## 前端代码质量

### Prettier

```bash
bun run lint        # 检查格式
bun run lint:fix    # 自动修复格式
```

配置：`singleQuote: true`, `jsxSingleQuote: true`，使用 `@so1ve/prettier-config` 预设。

### ESLint

```bash
bun run eslint      # 检查
bun run eslint:fix  # 自动修复
```

规则：
- **AGPL 许可证头**：所有 `.js/.jsx` 文件必须包含 AGPL 许可证头块
- `no-multiple-empty-lines`：最多 1 个空行
- `react-hooks` 规则

### i18n 工具

```bash
bun run i18n:extract  # 提取国际化字符串
bun run i18n:sync     # 同步翻译
bun run i18n:lint     # 检查翻译
```

---

## 核心依赖

| 类别 | 包 | 版本 |
|------|-----|------|
| Web 框架 | `gin-gonic/gin` | v1.9.1 |
| ORM | `gorm.io/gorm` | v1.25.2 |
| Redis | `go-redis/redis/v8` | v8.11.5 |
| JWT | `golang-jwt/jwt/v5` | v5.3.0 |
| WebAuthn | `go-webauthn/webauthn` | v0.14.0 |
| 测试 | `stretchr/testify` | v1.11.1 |
| AWS | `aws-sdk-go-v2` | v1.41.2 |
| 支付 | `stripe/stripe-go/v81` | v81.4.0 |
| WebSocket | `gorilla/websocket` | v1.5.0 |
| 验证 | `go-playground/validator/v10` | v10.20.0 |
| 工具 | `samber/lo` | v1.52.0 |
| JSON 查询 | `tidwall/gjson` | v1.18.0 |

---

## 代码审查清单

### 提交前必检项

- [ ] 所有 JSON 操作使用 `common.Marshal/Unmarshal`（不直接调用 `encoding/json`）
- [ ] 数据库代码兼容 SQLite + MySQL + PostgreSQL
- [ ] 保留字列名使用 `commonGroupCol`/`commonKeyCol`
- [ ] 上游转发 DTO 可选字段使用指针类型 + `omitempty`
- [ ] 错误消息返回客户端前已脱敏
- [ ] 新渠道适配器检查了 StreamOptions 支持
- [ ] 前端使用 Bun 作为包管理器
- [ ] 不修改受保护的项目标识信息

### 安全检查项

- [ ] 无硬编码密钥或敏感信息
- [ ] 日志不包含 API 密钥、密码或 AI 对话内容
- [ ] 用户输入经过验证（使用 `validator/v10`）
- [ ] 错误消息不泄露内部实现细节
- [ ] IP 记录尊重用户隐私设置

### 数据库检查项

- [ ] 计数器更新使用 `gorm.Expr()`（原子操作）
- [ ] 事务中包含正确的回滚逻辑
- [ ] SQLite 路径处理了不支持的操作（TRUNCATE、ALTER COLUMN、嵌套事务）
- [ ] 日志模型操作使用 `LOG_DB`
