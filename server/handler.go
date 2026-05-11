package server

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pengpn/go-llm-agent/models"
	"github.com/pengpn/go-llm-agent/session"
)

// ChatRequest 是 POST /chat 的请求体。
type ChatRequest struct {
	UserID  string `json:"user_id" binding:"required"`
	Message string `json:"message" binding:"required"`
}

// ChatResponse 是 POST /chat 的响应体。
type ChatResponse struct {
	UserID string `json:"user_id"`
	Answer string `json:"answer"`
}

// handleChat 处理对话请求：获取或创建 Session → 调用 Agent → 持久化历史 → 返回答案。
func (s *Server) handleChat(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误: " + err.Error()})
		return
	}

	// 写入 context 供 Logger 中间件读取
	c.Set("user_id", req.UserID)

	sess := s.sessions.GetOrCreate(req.UserID)
	sess.AddUserMessage(req.Message)

	msgs := sess.Messages()
	initialLen := len(msgs)

	answer, history, err := s.ag.Run(c.Request.Context(), msgs)
	if err != nil {
		// 回滚：把刚加入的用户消息从 Session 中移除，保证历史一致
		sess.Clear()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Agent 执行失败: " + err.Error()})
		return
	}

	applyHistoryToSession(sess, history, initialLen)

	c.JSON(http.StatusOK, ChatResponse{
		UserID: req.UserID,
		Answer: answer,
	})
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
