# Lesson 08：身份认证（API Key / JWT）

## 为什么要学

当前 API 没有任何鉴权，任何人都能调用，也无法区分不同租户。
生产环境必须有认证：知道是谁在调用、按 Key 计费、拦截未授权请求。

## 学习目标

- 理解 API Key 认证 vs JWT 认证的适用场景
- 用 Gin 中间件实现 API Key 鉴权
- 理解 JWT 的结构（Header.Payload.Signature）和验签原理
- 掌握认证信息在 `gin.Context` 中的传递

## 计划内容

### 原理

**API Key 认证**（适合服务间调用）：
```
请求头：Authorization: Bearer sk-xxxxx
中间件：从数据库/配置查 Key → 合法则放行，否则返回 401
```

**JWT 认证**（适合用户登录场景）：
```
登录 → 服务端签发 Token（含 userID、角色、过期时间）
请求头：Authorization: Bearer eyJxxx.eyJxxx.xxx
中间件：验证签名 → 解析 Payload → 写入 Context
```

### 要实现的东西
```
POST /auth/token    — 颁发 JWT（用 API Key 换 JWT）
中间件 AuthRequired — 验证 JWT，解析 userID 写入 Context
```

### 和现有代码的联动
- `user_id` 从请求体改为从 JWT Payload 读取，防止用户伪造他人 ID
- 权限控制（Lesson 04 的 RoleGate）和 JWT 中的 role 字段联动

### 课后作业（预计）
- 作业1：实现 API Key → JWT 颁发接口
- 作业2：实现 JWT 验证中间件，user_id 从 Token 读取
- 作业3：Token 过期处理 + 刷新 Token 接口
