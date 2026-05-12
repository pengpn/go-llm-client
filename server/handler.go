package server

import (
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pengpn/go-llm-agent/models"
	"github.com/pengpn/go-llm-agent/session"
)

// AuthTokenRequest 是 POST /auth/token 的请求体。
type AuthTokenRequest struct {
	APIKey string `json:"api_key" binding:"required"`
}

// AuthTokenResponse 是 POST /auth/token 的响应体。
type AuthTokenResponse struct {
	Token     string `json:"token"`
	ExpiresIn int    `json:"expires_in"` // 有效期（秒）
	UserID    string `json:"user_id"`
}

// handleAuthToken 用 API Key 换 JWT。
// 调用方后续请求携带 JWT：Authorization: Bearer {token}
func (s *Server) handleAuthToken(c *gin.Context) {
	var req AuthTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误: " + err.Error()})
		return
	}

	userID, ok := s.apiKeys[req.APIKey]
	if !ok {
		// 不返回"key 不存在"的具体原因，防止枚举 key
		c.JSON(http.StatusUnauthorized, gin.H{"error": "无效的 API Key"})
		return
	}

	if len(s.jwtSecret) == 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务端未配置 JWT 密钥"})
		return
	}

	token, err := generateToken(userID, s.jwtSecret, tokenExpiry)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成 token 失败"})
		return
	}

	c.JSON(http.StatusOK, AuthTokenResponse{
		Token:     token,
		ExpiresIn: int(tokenExpiry.Seconds()),
		UserID:    userID,
	})
}

// handleRefreshToken 用当前有效 JWT 换一个新 JWT（延长有效期）。
// 请求头：Authorization: Bearer {current_token}
// 必须在 token 未过期时调用；过期后须重新用 API Key 换取。
func (s *Server) handleRefreshToken(c *gin.Context) {
	// AuthRequired 中间件已验证 token 并写入 user_id
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	token, err := generateToken(userID, s.jwtSecret, tokenExpiry)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成 token 失败"})
		return
	}

	c.JSON(http.StatusOK, AuthTokenResponse{
		Token:     token,
		ExpiresIn: int(tokenExpiry.Seconds()),
		UserID:    userID,
	})
}

// ChatRequest 是 POST /chat 的请求体。
// UserID 可选：JWT 配置时从 token 读取；未配置 JWT 时从此字段读取（向后兼容）。
type ChatRequest struct {
	UserID  string `json:"user_id"`
	Message string `json:"message" binding:"required"`
}

// ChatResponse 是 POST /chat 的响应体。
type ChatResponse struct {
	UserID string `json:"user_id"`
	Answer string `json:"answer"`
}

// HistoryMessage 是对话历史中的单条消息（给前端用的精简格式）。
type HistoryMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// handleChat 处理对话请求：解析参数 → 调用 processChat。
func (s *Server) handleChat(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误: " + err.Error()})
		return
	}
	s.processChat(c, req)
}

// processChat 是 /chat 和 /chat/stream（降级时）的共享业务逻辑。
// req 已由调用方解析完毕，body 不再需要读取。
func (s *Server) processChat(c *gin.Context, req ChatRequest) {
	// JWT 优先：中间件已将 user_id 写入 Context；未配置 JWT 时从请求体取（向后兼容）。
	// 这样可防止用户通过请求体伪造他人的 user_id。
	userID := c.GetString("user_id")
	if userID == "" {
		userID = req.UserID
	}
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "缺少用户身份，请携带有效 JWT 或在请求体中提供 user_id"})
		return
	}
	c.Set("user_id", userID) // 更新 context，供 Logger 中间件读取

	// 频率限制：防止同一用户短时间内发送大量请求（如果配置了限流器）
	if s.limiter != nil && !s.limiter.Allow(userID) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "请求过于频繁，请稍后再试"})
		return
	}

	sess := s.sessions.GetOrCreate(userID)
	sess.AddUserMessage(req.Message)

	msgs := sess.Messages()
	initialLen := len(msgs)

	// 将 userID 注入 context，供 transfer_to_human 等工具读取（无需改变工具函数签名）
	ctx := ContextWithUserID(c.Request.Context(), userID)
	answer, history, err := s.ag.Run(ctx, msgs)
	if err != nil {
		// 回滚：把刚加入的用户消息从 Session 中移除，保证历史一致
		sess.Clear()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Agent 执行失败: " + err.Error()})
		return
	}

	applyHistoryToSession(sess, history, initialLen)

	c.JSON(http.StatusOK, ChatResponse{
		UserID: userID,
		Answer: answer,
	})
}

// handleChatStream 流式对话接口，通过 SSE 逐 token 推送 LLM 回答。
//
// SSE 事件格式：
//
//	event: token\ndata: {文字片段}\n\n  — 每个 token
//	event: done\ndata: \n\n            — 流结束
//
// 工作流程：
//  1. 通过类型断言检查 ag 是否支持流式（StreamingAgentRunner）
//  2. 不支持则降级为普通 /chat（复用 processChat，body 已解析不需要再读）
//  3. 支持则启动后台 goroutine 运行 RunStream，token 写入 tokenCh
//  4. c.Stream 读 tokenCh，每个 token 推送一个 SSE 事件
//  5. tokenCh 关闭（RunStream 返回）时，推送 done 事件，结束流
//
// 为什么需要后台 goroutine？
// c.Stream 是阻塞的（持续推送直到 func 返回 false）；
// RunStream 也是阻塞的（等待 LLM 响应）；二者需要并发执行。
func (s *Server) handleChatStream(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误: " + err.Error()})
		return
	}

	// 类型断言：检查 Agent 是否支持流式输出，不支持则降级
	streamAg, ok := s.ag.(StreamingAgentRunner)
	if !ok {
		// body 已解析完毕，直接传 req 给 processChat，不再读 body
		s.processChat(c, req)
		return
	}

	// JWT 优先：与 processChat 保持一致，防止伪造 user_id
	userID := c.GetString("user_id")
	if userID == "" {
		userID = req.UserID
	}
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "缺少用户身份，请携带有效 JWT 或在请求体中提供 user_id"})
		return
	}
	c.Set("user_id", userID)

	if s.limiter != nil && !s.limiter.Allow(userID) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "请求过于频繁，请稍后再试"})
		return
	}

	sess := s.sessions.GetOrCreate(userID)
	sess.AddUserMessage(req.Message)

	msgs := sess.Messages()
	initialLen := len(msgs)

	// tokenCh 在 RunStream goroutine 和 c.Stream 之间传递 token
	tokenCh := make(chan string, 32)

	var (
		finalAnswer string
		history     []models.Message
		runErr      error
	)

	// 同 processChat：将 userID 注入 context，供工具函数读取
	streamCtx := ContextWithUserID(c.Request.Context(), userID)

	done := make(chan struct{})
	go func() {
		defer close(done)
		// RunStream 内部会 close(tokenCh)，通知 c.Stream 流结束
		finalAnswer, history, runErr = streamAg.RunStream(streamCtx, msgs, tokenCh)
	}()

	// 禁止代理缓冲，确保 token 实时推送到客户端
	c.Header("X-Accel-Buffering", "no")

	c.Stream(func(w io.Writer) bool {
		select {
		case token, open := <-tokenCh:
			if !open {
				c.SSEvent("done", "")
				return false // 结束流
			}
			c.SSEvent("token", token)
			return true // 继续读下一个 token

		case <-c.Request.Context().Done():
			// 客户端断开连接，停止推送（RunStream 也会因 ctx 取消而终止）
			return false
		}
	})

	// c.Stream 结束后等待 RunStream 完全退出，再做 session 持久化
	<-done
	if runErr == nil {
		applyHistoryToSession(sess, history, initialLen)
	}
	_ = finalAnswer
}

// handleReload 热更新知识库：重新索引所有 FAQ，不中断正在进行的对话。
// Qdrant upsert 是幂等的，重复索引不会产生重复数据。
func (s *Server) handleReload(c *gin.Context) {
	ctx := c.Request.Context()
	for _, doc := range s.faqDocs {
		text := doc.Title + "\n" + doc.Content
		if err := s.pipeline.IndexText(ctx, text, doc.Title); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "索引失败: " + err.Error(),
				"doc":   doc.Title,
			})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"indexed": len(s.faqDocs),
	})
}

// handleHealth 健康检查：返回服务状态、运行时长、当前活跃 Session 数。
func (s *Server) handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":   "ok",
		"uptime":   time.Since(s.startAt).Round(time.Second).String(),
		"sessions": s.sessions.Count(),
	})
}

// handleHistory 返回指定用户的完整对话历史（不含 system prompt，不截断）。
// 用于客服管理后台查看用户对话记录。
// 安全：启用 JWT 时，用户只能查看自己的历史（callerID == userID）。
func (s *Server) handleHistory(c *gin.Context) {
	userID := c.Param("user_id")

	// JWT 启用时做身份校验：防止 A 用户偷看 B 用户的对话记录
	if len(s.jwtSecret) > 0 {
		callerID := c.GetString("user_id")
		if callerID != userID {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权查看其他用户的对话历史"})
			return
		}
	}

	sess := s.sessions.Get(userID)
	if sess == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在或会话已过期"})
		return
	}

	history := sess.History()
	msgs := make([]HistoryMessage, len(history))
	for i, m := range history {
		msgs[i] = HistoryMessage{Role: string(m.Role), Content: m.Content}
	}

	c.JSON(http.StatusOK, gin.H{
		"user_id":  userID,
		"messages": msgs,
		"count":    len(msgs),
	})
}

// handleListTickets 返回工单列表，供人工客服查看待处理工单。
// 可选查询参数：?status=pending（默认）/ all
func (s *Server) handleListTickets(c *gin.Context) {
	statusParam := c.DefaultQuery("status", "pending")

	var filter TicketStatus
	switch statusParam {
	case "all":
		filter = ""
	default:
		filter = TicketStatusPending
	}

	tickets := s.tickets.List(filter)
	c.JSON(http.StatusOK, gin.H{
		"tickets": tickets,
		"count":   len(tickets),
	})
}

// applyHistoryToSession 将 Agent Run 新增的消息写入 Session。
// assistant+tool 消息必须原子写入，保证 OpenAI 协议要求的消息顺序。
func applyHistoryToSession(sess *session.Session, history []models.Message, initialLen int) {
	newMsgs := history[initialLen:]
	i := 0
	for i < len(newMsgs) {
		msg := newMsgs[i]
		if msg.Role == models.RoleAssistant && len(msg.ToolCalls) > 0 {
			j := i + 1
			for j < len(newMsgs) && newMsgs[j].Role == models.RoleTool {
				j++
			}
			sess.AddAgentTurn(msg, newMsgs[i+1:j])
			i = j
		} else if msg.Role == models.RoleAssistant {
			sess.AddAssistantMessage(msg.Content)
			i++
		} else {
			i++
		}
	}
}
