package server

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// Logger 是结构化日志中间件，记录每个请求的方法、路径、状态码、耗时。
// 使用标准库 log/slog（Go 1.21+），输出 JSON 格式方便接入日志平台。
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next() // 执行后续 handler

		slog.Info("request",
			"method",   c.Request.Method,
			"path",     c.Request.URL.Path,
			"status",   c.Writer.Status(),
			"duration", time.Since(start).String(),
			"ip",       c.ClientIP(),
			"user_id",  c.GetString("user_id"), // 由 handler 写入，方便追踪
		)
	}
}
