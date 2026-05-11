// Package server 提供基于 Gin 的 HTTP 服务器，整合 Agent + RAG + Session 管理。
package server

import (
	"context"
	"log/slog"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pengpn/go-llm-agent/agent"
	"github.com/pengpn/go-llm-agent/rag"
	"github.com/pengpn/go-llm-agent/session"
)

// FAQDoc 是知识库文档的基本单元，Title 作为来源标识，Content 是正文。
type FAQDoc struct {
	Title   string
	Content string
}

// Server 封装所有共享资源。
// 共享：LLM Client（通过 agent）、RAG Pipeline/Retriever（通过 agent 工具）
// 隔离：Session（每个 user_id 独立）
type Server struct {
	engine   *gin.Engine
	sessions *session.Manager
	ag       *agent.Agent
	pipeline *rag.Pipeline // 用于热更新知识库
	faqDocs  []FAQDoc      // 知识库原始数据，Reload 时重新索引
	startAt  time.Time
}

// New 创建 Server 并注册所有路由。
func New(ag *agent.Agent, sessions *session.Manager, pipeline *rag.Pipeline, faqDocs []FAQDoc) *Server {
	// ReleaseMode 关闭 Gin 的调试日志，改用我们自己的 slog 中间件
	gin.SetMode(gin.ReleaseMode)

	s := &Server{
		engine:   gin.New(),
		sessions: sessions,
		ag:       ag,
		pipeline: pipeline,
		faqDocs:  faqDocs,
		startAt:  time.Now(),
	}
	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	// gin.Recovery() 捕获 panic，防止单个请求的 panic 崩掉整个服务
	s.engine.Use(Logger(), gin.Recovery())

	s.engine.POST("/chat", s.handleChat)
	s.engine.POST("/reload", s.handleReload)
	s.engine.GET("/health", s.handleHealth)
}

// Run 启动 HTTP 服务器，并在收到 SIGINT/SIGTERM 时优雅关闭。
//
// 优雅关闭流程：
// 1. 停止接受新请求
// 2. 等待进行中的请求处理完（最多 30 秒）
// 3. 停止 Session TTL 清理协程
func (s *Server) Run(addr string) error {
	srv := &http.Server{
		Addr:         addr,
		Handler:      s.engine,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second, // LLM 响应可能需要较长时间
		IdleTimeout:  60 * time.Second,
	}

	// 后台启动 HTTP 服务
	go func() {
		slog.Info("server started", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
		}
	}()

	// 等待系统信号（Ctrl+C 或 kill）
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	slog.Info("shutting down, waiting for in-flight requests...")

	// 给进行中的请求最多 30 秒完成
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s.sessions.Stop() // 停止后台 TTL 清理协程
	return srv.Shutdown(shutdownCtx)
}
