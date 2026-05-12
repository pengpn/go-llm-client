package server

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// tokenExpiry 是 JWT 的默认有效期。
// 为什么是 24 小时？生产系统通常 access token 1-2 小时，refresh token 7-30 天。
// 这里为了演示方便，设为 24 小时。
const tokenExpiry = 24 * time.Hour

// Claims 是 JWT 的 Payload 内容。
// 嵌入 jwt.RegisteredClaims 获得 exp/iat 等标准字段。
type Claims struct {
	UserID string `json:"user_id"`
	jwt.RegisteredClaims
}

// generateToken 生成一个 HMAC-SHA256 签名的 JWT。
// secret 是签名密钥，必须保密且足够复杂（生产环境至少 32 字节随机值）。
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

// parseToken 验证签名并解析 JWT，返回 Claims。
// 失败原因：签名不匹配、token 已过期、格式错误。
func parseToken(tokenStr string, secret []byte) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		// 防止算法混淆攻击：明确要求 HMAC，拒绝 RSA/none 等算法
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("不支持的签名算法: %v", t.Header["alg"])
		}
		return secret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("无效的 token")
	}
	return claims, nil
}

// AuthRequired 返回一个 Gin 中间件，验证 JWT 并将 user_id 写入 Context。
//
// jwtSecret 为空时跳过验证（用于测试和禁用认证的场景）。
// 验证通过后：c.GetString("user_id") 可取到用户 ID。
// 验证失败：直接返回 401，终止后续 handler 执行。
func AuthRequired(jwtSecret []byte) gin.HandlerFunc {
	return func(c *gin.Context) {
		if len(jwtSecret) == 0 {
			// 未配置 JWT，跳过认证（开发模式）
			c.Next()
			return
		}

		authHeader := c.GetHeader("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized,
				gin.H{"error": "缺少 Authorization: Bearer <token> 请求头"})
			return
		}

		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		claims, err := parseToken(tokenStr, jwtSecret)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized,
				gin.H{"error": "无效或过期的 token: " + err.Error()})
			return
		}

		// 将 user_id 写入 Context，后续 handler 通过 c.GetString("user_id") 读取
		c.Set("user_id", claims.UserID)
		c.Next()
	}
}
