# 数据库规范

> 本项目的数据库模式、ORM 用法和多数据库兼容性规范。

---

## 概述

- **ORM**: GORM v2 (`gorm.io/gorm` v1.25.2)
- **支持的数据库**: SQLite、MySQL (>= 5.7.8)、PostgreSQL (>= 9.6)
- **所有代码必须同时兼容三种数据库**
- **全局 DB 句柄**: `model.DB`（主库）、`model.LOG_DB`（日志库，可独立配置）
- **日志库**: 通过 `LOG_SQL_DSN` 环境变量配置独立数据库，未设置时默认使用主库

---

## 模型定义规范

### 结构体标签

```go
type User struct {
    Id            int            `json:"id"`
    Username      string         `json:"username" gorm:"unique;index" validate:"max=12"`
    Password      string         `json:"password" gorm:"not null;"`
    DisplayName   string         `json:"display_name" gorm:"index"`
    Role          int            `json:"role" gorm:"type:int;default:1"`
    Status        int            `json:"status" gorm:"type:int;default:1"`
    Token         string         `json:"token" gorm:"type:char(32);uniqueIndex"`
    Email         string         `json:"email" gorm:"index"`
    Group         string         `json:"group" gorm:"type:varchar(64);default:'default'"`
    Quota         int            `json:"quota" gorm:"type:int;default:0"`
    AccessToken   *string        `json:"access_token" gorm:"type:char(32);column:access_token;uniqueIndex"`
    DeletedAt     gorm.DeletedAt `gorm:"index"`
    // 非持久化字段
    OriginalPassword string `json:"original_password" gorm:"-:all"`
}
```

### 关键模式

| 模式 | 说明 | 示例 |
|------|------|------|
| **软删除** | 使用 `gorm.DeletedAt` 字段 | `User`, `Token`, `Redemption`, `Model` |
| **硬删除** | 使用 `DB.Unscoped().Delete()` | 管理员删除用户操作 |
| **可空字段** | 使用指针类型 `*string`, `*int` | `Channel.Setting *string` |
| **排除字段** | `gorm:"-:all"` 完全排除，`gorm:"-"` 排除读写 | `User.OriginalPassword`, `Channel.Keys` |
| **只读字段** | `gorm:"->"` 仅读取不写入 | `Log.ChannelName`（通过 JOIN 填充） |
| **自定义 JSON 类型** | 实现 `driver.Valuer` + `sql.Scanner` | `ChannelInfo` 结构体存储为 JSON |
| **复合主键** | `gorm:"primaryKey;autoIncrement:false"` | `Ability`（Group + Model + ChannelId） |
| **复合索引** | 单字段参与多个索引 | `Log` 的 `index:idx_created_at_id,priority:1` |

### 自定义 JSON 类型示例

```go
// model/channel.go
type ChannelInfo struct {
    CurrentBalance float64 `json:"current_balance"`
    BalanceTime    int64   `json:"balance_time"`
}

func (c ChannelInfo) Value() (driver.Value, error) {
    return common.Marshal(c)  // [!] 使用 common.Marshal
}

func (c *ChannelInfo) Scan(value interface{}) error {
    return common.Unmarshal(value.([]byte), c)  // [!] 使用 common.Unmarshal
}
```

### 表命名

项目使用 GORM 默认的复数化规则（`User` -> `users`，`Channel` -> `channels`）。不使用自定义 `TableName()` 方法。

---

## 查询模式

### 标准 GORM 方法（推荐）

```go
// 查询
DB.Where("user_id = ?", userId).Order("id desc").Limit(num).Offset(startIdx).Find(&tokens)

// 创建
DB.Create(&user)

// 选择性更新
DB.Model(token).Select("name", "status", "subnet", "models").Updates(token)

// 软删除
DB.Delete(token)

// 硬删除
DB.Unscoped().Delete(&User{}, "id = ?", id)

// 计数
DB.Model(&Token{}).Where("user_id = ?", userId).Count(&total)

// 去重查询
DB.Table("abilities").Distinct("model").Pluck("model", &models)

// 排除敏感列
DB.Omit("key").First(channel, "id = ?", id)

// 子查询
DB.Model(&Ability{}).Select("MAX(priority)").Where(...)

// JOIN 查询
DB.Table("abilities").Joins("left join channels on abilities.channel_id = channels.id")

// 聚合查询
DB.Table("logs").Select("sum(quota) quota").Where("user_id = ?", userId)
```

### 原子计数器更新（防止竞态条件）

使用 `gorm.Expr()` 执行数据库级别的原子操作：

```go
// [!] 更新计数器必须使用 gorm.Expr，禁止先读后写
DB.Model(&User{}).Where("id = ?", id).Update("quota", gorm.Expr("quota + ?", quota))
DB.Model(&User{}).Where("id = ?", id).Updates(map[string]interface{}{
    "used_quota":    gorm.Expr("used_quota + ?", quota),
    "request_count": gorm.Expr("request_count + ?", 1),
})
```

### 冲突处理（Upsert）

```go
tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&chunk)
```

### 行级锁定

```go
tx.Set("gorm:query_option", "FOR UPDATE").Where("trade_no = ?", ref).First(topUp)
```

---

## 多数据库兼容性 [!] 关键

### 全局数据库类型标志

定义在 `common/database.go`，在数据库初始化时设置：

```go
var UsingSQLite = false
var UsingPostgreSQL = false
var UsingMySQL = false
```

### 保留字列名引用

由于 `group` 和 `key` 是 SQL 保留字，`model/main.go` 定义了跨数据库兼容的变量：

```go
var commonGroupCol string   // MySQL/SQLite: "`group`"    PostgreSQL: `"group"`
var commonKeyCol string     // MySQL/SQLite: "`key`"      PostgreSQL: `"key"`
var commonTrueVal string    // MySQL/SQLite: "1"          PostgreSQL: "true"
var commonFalseVal string   // MySQL/SQLite: "0"          PostgreSQL: "false"
```

**使用示例**：
```go
DB.Where(commonGroupCol+" = ?", group).Find(&abilities)
DB.Where("status = "+commonTrueVal).Find(&channels)
```

### 字符串拼接

```go
// MySQL
query = "CONCAT(',', models, ',') LIKE ?"
// SQLite / PostgreSQL
query = "(',' || models || ',') LIKE ?"
```

### 时间戳函数

```go
// PostgreSQL: SELECT EXTRACT(EPOCH FROM NOW())::bigint
// SQLite:     SELECT strftime('%s','now')
// MySQL:      SELECT UNIX_TIMESTAMP()
```

### 表截断

```go
if common.UsingSQLite {
    DB.Exec("DELETE FROM abilities")       // SQLite 不支持 TRUNCATE
} else {
    DB.Exec("TRUNCATE TABLE abilities")
}
```

### 列引用（在原生 SQL 中）

```go
refCol := "`trade_no`"
if common.UsingPostgreSQL {
    refCol = `"trade_no"`
}
```

---

## 事务模式

### 方式一：`DB.Transaction()` 回调（推荐）

自动处理提交/回滚，适用于新代码：

```go
err = DB.Transaction(func(tx *gorm.DB) error {
    err := tx.Set("gorm:query_option", "FOR UPDATE").
        Where("trade_no = ?", refId).First(topUp).Error
    if err != nil {
        return errors.New("充值记录不存在")
    }
    // ... 业务操作 ...
    return nil  // 自动提交
})
// err != nil 时自动回滚
```

### 方式二：手动 `DB.Begin()` / `Commit()` / `Rollback()`

用于需要更细粒度控制的场景（如分页查询的计数+获取原子性、批量分块操作）：

```go
tx := DB.Begin()
defer func() {
    if r := recover(); r != nil {
        tx.Rollback()
    }
}()

// ... 操作 ...

if err := tx.Commit().Error; err != nil {
    tx.Rollback()
    return err
}
```

### SQLite 事务注意事项

SQLite 不支持嵌套事务，在某些场景下需要非事务路径：

```go
// model/checkin.go
if common.UsingSQLite {
    return userCheckinWithoutTransaction(userId)  // 非事务路径
}
return DB.Transaction(func(tx *gorm.DB) error {
    // 事务路径
})
```

---

## 迁移模式

### AutoMigrate（主要方式）

所有模型结构体在 `model/main.go` 的 `migrateDB()` 中统一注册：

```go
func migrateDB() error {
    return DB.AutoMigrate(
        &Channel{}, &Token{}, &User{}, &Option{}, &Ability{},
        &Log{}, &Redemption{}, &TopUp{}, &Pricing{}, &Task{},
        // ... 更多模型 ...
    )
}
```

### 手动迁移（特殊场景）

当 AutoMigrate 无法处理时（如列类型变更），需要编写手动迁移：

- **检查当前列类型**：查询 `information_schema`（MySQL/PostgreSQL）或 `PRAGMA table_info`（SQLite）
- **条件执行**：仅在类型不匹配时才执行 ALTER
- **跨数据库分支**：PostgreSQL 用 `ALTER COLUMN ... TYPE`，MySQL 用 `MODIFY COLUMN`，SQLite 跳过或用 ADD COLUMN

```go
// 示例：model/main.go
if common.UsingPostgreSQL {
    DB.Exec("ALTER TABLE subscription_plans ALTER COLUMN price_amount TYPE decimal(10,6)")
} else if common.UsingMySQL {
    DB.Exec("ALTER TABLE subscription_plans MODIFY COLUMN price_amount decimal(10,6)")
}
// SQLite: 跳过，类型亲和性自动处理
```

### 迁移仅在主节点执行

```go
if !common.IsMasterNode {
    return nil  // 从节点/副本跳过迁移
}
```

---

## 批量更新机制

`model/utils.go` 实现了**写合并批量更新器**，用于高频计数器更新：

- 变更累积在内存 `map[int]int` 中，按类型分离，各有独立互斥锁
- 后台 goroutine 按 `common.BatchUpdateInterval` 周期刷新到数据库
- 支持的更新类型：用户额度、Token 额度、已用额度、渠道已用额度、请求计数
- 当 `common.BatchUpdateEnabled` 开启时，`IncreaseUserQuota` 等函数走内存累积路径

---

## 禁止的模式

| 禁止 | 原因 | 替代方案 |
|------|------|---------|
| 直接使用 `encoding/json` 的 Marshal/Unmarshal | 统一 JSON 处理 | `common.Marshal()`, `common.Unmarshal()` |
| MySQL 专用函数（如 `GROUP_CONCAT`）不提供 PG 兼容 | 跨数据库兼容 | 同时提供 `STRING_AGG` 分支 |
| PostgreSQL 专用操作符（`@>`, `?`, `JSONB`） | 跨数据库兼容 | 使用 `TEXT` 类型存 JSON |
| SQLite 中的 `ALTER COLUMN` | SQLite 不支持 | 使用 `ALTER TABLE ... ADD COLUMN` |
| 先读后写更新计数器 | 竞态条件 | `gorm.Expr("quota + ?", delta)` |
| 数据库专用列类型无降级方案 | 跨数据库兼容 | 使用 `TEXT` 代替 `JSONB` |

---

## 常见错误

1. **忘记处理 SQLite 兼容性**：SQLite 不支持 `TRUNCATE`、嵌套事务、`ALTER COLUMN`
2. **保留字未引用**：在原生 SQL 中使用 `group`、`key` 列名时忘记使用 `commonGroupCol`/`commonKeyCol`
3. **布尔值硬编码**：在 WHERE 条件中使用 `1`/`true` 而非 `commonTrueVal`
4. **日志表操作用错句柄**：日志相关操作应使用 `LOG_DB`，而非 `DB`
5. **非原子计数器更新**：先 `SELECT` 再 `UPDATE` 计数器字段，导致并发丢失更新
