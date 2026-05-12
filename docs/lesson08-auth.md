# Lesson 08：身份认证（API Key / JWT）

## 为什么要学

当前 API 没有任何鉴权，任何人都能调用，也无法区分不同租户。
生产环境必须有认证：知道是谁在调用、按 Key 计费、拦截未授权请求。

## 核心问题

**"为什么不直接用 API Key 替代 user_id？"**

| 方案 | 优点 | 缺点 |
|------|------|------|
| 请求体传 user_id | 简单 | 客户端可以伪造任意 user_id，A 用户冒充 B |
| API Key 每次验证 | 简单 | 每次请求都需要查表（数据库压力），Key 过长不适合频繁传输 |
| API Key → JWT | 两全其美 | 略有实现复杂度 |

**正确流程：**
```
客户端      服务端
  │── POST /auth/token (api_key: sk-xxx) ──▶ 查表验证 API Key
  │◀── JWT { user_id, exp, iat } ───────────  签名后下发
  │
  │── POST /chat (Authorization: Bearer JWT) ▶ 验签，从 Payload 读 user_id
  │◀── { answer } ─────────────────────────   无需再查表
```

---

## JWT 原理

JWT = **Header**.**Payload**.**Signature**，三段 Base64URL 编码，用 `.` 拼接。

```
eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9   ← Header（算法声明）
.eyJ1c2VyX2lkIjoiYWxpY2UiLCJleHAiOjE3…  ← Payload（数据：user_id、过期时间）
.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c  ← Signature（防篡改）
```

**Signature = HMAC-SHA256(Header + "." + Payload, secret)**

服务端拿 `secret` 重新计算签名，和收到的签名对比。
签名不同 → Token 被篡改 → 拒绝。
签名相同 → Payload 可信 → 直接读 user_id，无需查库。

**算法混淆攻击（Algorithm Confusion Attack）：**
攻击者把 Header 里的 `alg` 改成 `"none"`，某些库会跳过验签直接信任。
防御：明确指定期望的算法，拒绝其他所有算法。

```go
// parseToken 中的防御
if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
    return nil, fmt.Errorf("不支持的签名算法: %v", t.Header["alg"])
}
```

---

## 实现细节

### server/auth.go — JWT 签发与验证

```go
const tokenExpiry = 24 * time.Hour

type Claims struct {
    UserID string `json:"user_id"`
    jwt.RegisteredClaims  // 包含 exp、iat 等标准字段
}

func generateToken(userID string, secret []byte, expiry time.Duration) (string, error) {
    claims := Claims{
        UserID: userID,
        RegisteredClaims: jwt.RegisteredClaims{
            ExpiresAt: jwt.NewNumericDate(time.Now().Add(expiry)),
            IssuedAt:  jwt.NewNumericDate(time.Now()),
        },
    }
    token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    return token.SignedString(secret)
}

func AuthRequired(jwtSecret []byte) gin.HandlerFunc {
    return func(c *gin.Context) {
        if len(jwtSecret) == 0 {
            c.Next() // 未配置 JWT → no-op（开发模式）
            return
        }
        // 验签 → 解析 user_id → c.Set("user_id", claims.UserID)
    }
}
```

**关键设计：`AuthRequired(nil)` 是 no-op**

不传 JWT Secret 时中间件直接放行，所有现有测试和开发环境无需任何改动。
这是渐进增强（Progressive Enhancement）模式。

---

### handler.go — user_id 优先级

**旧代码（不安全）：**
```go
sess := s.sessions.GetOrCreate(req.UserID)  // 客户端可以填任意值
```

**新代码（JWT 优先）：**
```go
userID := c.GetString("user_id")  // JWT 中间件写入，不可伪造
if userID == "" {
    userID = req.UserID            // 未配置 JWT 时降级到请求体（向后兼容）
}
if userID == "" {
    c.JSON(http.StatusUnauthorized, gin.H{"error": "缺少用户身份"})
    return
}
```

**handleHistory 授权检查：**
```go
if len(s.jwtSecret) > 0 {
    callerID := c.GetString("user_id")
    if callerID != userID {
        c.JSON(http.StatusForbidden, gin.H{"error": "无权查看其他用户的对话历史"})
        return
    }
}
```

启用 JWT 后，用户 A 无法查看用户 B 的对话记录。未启用时任意访问（兼容管理后台场景）。

---

## API 接口

### POST /auth/token — API Key 换 JWT

**请求：**
```json
{ "api_key": "sk-your-api-key" }
```

**响应：**
```json
{
  "token": "eyJhbGci...",
  "expires_in": 86400,
  "user_id": "alice"
}
```

**错误：**
- `400` — 缺少 api_key 字段
- `401` — API Key 不合法（不返回具体原因，防止枚举）
- `500` — 服务端未配置 JWT 密钥

### POST /auth/refresh — 刷新 JWT（延长有效期）

需携带：`Authorization: Bearer <current_token>`
响应格式同 `/auth/token`。

**注意：** Token 过期后不能刷新，必须重新用 API Key 换取。
过期前刷新是最佳实践（如：剩余 5 分钟时刷新）。

---

## 配置方式

```go
srv := server.New(ag, sessions, pipeline, faqDocs,
    server.WithJWTSecret([]byte(os.Getenv("JWT_SECRET"))),
    server.WithAPIKeys(map[string]string{
        os.Getenv("SERVICE_API_KEY"): "service",
    }),
    server.WithRateLimiter(10, time.Minute),
)
```

生产环境 `.env`：
```
JWT_SECRET=your-32-bytes-or-longer-random-secret
SERVICE_API_KEY=sk-your-service-api-key
```

---

## 安全要点

| 要点 | 说明 |
|------|------|
| Secret 长度 | 生产环境至少 32 字节随机值，切勿使用硬编码字符串 |
| HTTPS | JWT 明文传输，必须用 HTTPS 防止窃听 |
| 算法固定 | 明确要求 HMAC，拒绝 none/RSA 防算法混淆攻击 |
| 过期时间 | access token 1-24h，不应过长 |
| 枚举防御 | API Key 错误不返回"key 不存在"，统一返回"无效" |
| user_id 来源 | JWT 配置后，user_id 从 Token 读取，不信任请求体 |

---

## 课后作业（已完成）

### 作业1：实现 POST /auth/token（API Key → JWT 颁发）

**实现位置：** `server/auth.go` + `server/handler.go`（`handleAuthToken`）

**测试覆盖：**
- ✅ 有效 API Key → 200，返回 token、user_id、expires_in
- ✅ 无效 API Key → 401（不泄露原因）
- ✅ 缺少 api_key 字段 → 400

### 作业2：实现 AuthRequired 中间件，user_id 从 JWT 读取

**实现位置：** `server/auth.go`（`AuthRequired`）、`server/handler.go`（`processChat` / `handleChatStream` 更新）

**关键设计：**
- `AuthRequired(nil)` 是 no-op，开发模式不受影响
- JWT 优先：`c.GetString("user_id")` > `req.UserID`（防止伪造）
- `handleHistory` 授权：JWT 启用时只能查看自己的历史（403 保护）

**测试覆盖：**
- ✅ 未配置 JWT：请求体 user_id 正常工作
- ✅ 有效 JWT：user_id 从 Token 读取，响应中 user_id 来自 JWT 而非请求体
- ✅ 无效 Token → 401
- ✅ 缺少 Authorization 头 → 401
- ✅ 缺少 user_id（无 JWT 无请求体）→ 401
- ✅ 访问他人历史 → 403

### 作业3：实现 POST /auth/refresh（Token 刷新接口）

**实现位置：** `server/handler.go`（`handleRefreshToken`）、`server/server.go`（路由注册）

**测试覆盖：**
- ✅ 携带有效 Token → 200，新 Token、user_id 正确
- ✅ 无 Token → 401（AuthRequired 中间件拦截）
