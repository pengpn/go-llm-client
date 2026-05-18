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
	"github.com/pengpn/go-llm-agent/models"
	"github.com/pengpn/go-llm-agent/session"
)

// AgentRunner 是 Server 依赖的 Agent 接口，便于测试注入 mock。
// *agent.Agent 实现此接口，无需改动调用方代码。
type AgentRunner interface {
	Run(ctx context.Context, msgs []models.Message, opts ...agent.RunOption) (string, []models.Message, error)
}

// StreamingAgentRunner 是可选的流式扩展接口。
// handleChatStream 通过类型断言检查 ag 是否支持流式，不支持时降级为普通 /chat。
type StreamingAgentRunner interface {
	RunStream(ctx context.Context, msgs []models.Message, tokenCh chan<- string, opts ...agent.RunOption) (string, []models.Message, error)
}

// KBIndexer 是知识库索引接口，便于测试 /reload 接口。
// *rag.Pipeline 实现此接口。
type KBIndexer interface {
	IndexText(ctx context.Context, text, source string) error
}

// FAQDoc 是知识库文档的基本单元，Title 作为来源标识，Content 是正文。
type FAQDoc struct {
	Title   string
	Content string
}

// Server 封装所有共享资源。
// 共享：LLM Client（通过 agent）、RAG Pipeline/Retriever（通过 agent 工具）
// 隔离：Session（每个 user_id 独立）
type Server struct {
	engine    *gin.Engine
	sessions  *session.Manager
	ag        AgentRunner
	pipeline  KBIndexer    // 用于热更新知识库
	faqDocs   []FAQDoc     // 知识库原始数据，Reload 时重新索引
	startAt   time.Time
	limiter   *RateLimiter      // nil 表示不限流
	jwtSecret       []byte            // nil 表示不启用 JWT 认证
	apiKeys         map[string]string // API Key → UserID 映射
	tickets          *TicketStore              // nil 表示未启用人工转接工单
	ticketExtractor  *TicketExtractor          // nil 表示不自动提取工单
	memoryStore      session.MemoryStore       // nil 表示不启用用户画像
	memoryExtractor  *MemoryExtractor          // nil 表示不自动提取画像
}

// ServerOption 用于配置 Server 实例（函数式选项模式）。
type ServerOption func(*Server)

// WithJWTSecret 配置 JWT 签名密钥，启用身份认证中间件。
// 不调用此选项则 JWT 认证不生效（开发/测试环境）。
// 生产环境建议使用 32 字节以上随机值，从环境变量读取。
func WithJWTSecret(secret []byte) ServerOption {
	return func(s *Server) {
		s.jwtSecret = secret
	}
}

// WithAPIKeys 配置 API Key → UserID 映射，用于 POST /auth/token 颁发 JWT。
// key 是 API Key 字符串，value 是对应的 user_id。
func WithAPIKeys(keys map[string]string) ServerOption {
	return func(s *Server) {
		s.apiKeys = keys
	}
}

// WithTicketStore 启用人工转接工单功能，注册 GET /tickets 路由。
// store 应与注册到 Agent 的 NewTransferTool(store) 共享同一个实例。
func WithTicketStore(store *TicketStore) ServerOption {
	return func(s *Server) {
		s.tickets = store
	}
}

// WithTicketExtractor 启用自动工单提取功能。
// 每次 /chat 回答完成后自动从对话中提取工单信息，附在响应中返回。
// 提取失败不影响主流程（仅日志记录），ticket 字段为 null。
func WithTicketExtractor(te *TicketExtractor) ServerOption {
	return func(s *Server) {
		s.ticketExtractor = te
	}
}

// WithMemoryExtractor 启用 LLM 自动提取用户画像功能。
// 需同时配置 WithMemoryStore，否则提取结果无处保存。
// 每次 /chat 回答后自动从对话中提取值得记忆的用户信息。
func WithMemoryExtractor(me *MemoryExtractor) ServerOption {
	return func(s *Server) {
		s.memoryExtractor = me
	}
}

// WithMemoryStore 启用用户画像记忆功能。
// 每次 /chat 请求时自动加载用户画像并注入 System Prompt。
// 同时注册 GET/PUT /memory/:user_id 路由。
func WithMemoryStore(store session.MemoryStore) ServerOption {
	return func(s *Server) {
		s.memoryStore = store
	}
}

// WithRateLimiter 为 /chat 接口启用基于 user_id 的请求频率限制。
// limit：时间窗口内允许的最大请求数，window：时间窗口大小。
//
// 为什么按 user_id 而非 IP 限流？
// 客服系统面向已知用户，按 user_id 更精准：同一 NAT 下多用户互不影响。
func WithRateLimiter(limit int, window time.Duration) ServerOption {
	return func(s *Server) {
		s.limiter = NewRateLimiter(limit, window)
	}
}

// New 创建 Server 并注册所有路由。
func New(ag AgentRunner, sessions *session.Manager, pipeline KBIndexer, faqDocs []FAQDoc, opts ...ServerOption) *Server {
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
	for _, opt := range opts {
		opt(s)
	}
	s.setupRoutes()
	return s
}

// ServeHTTP 实现 http.Handler 接口，方便 httptest 直接使用 Server 作为 handler。
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.engine.ServeHTTP(w, r)
}

func (s *Server) setupRoutes() {
	// gin.Recovery() 捕获 panic，防止单个请求的 panic 崩掉整个服务
	s.engine.Use(Logger(), gin.Recovery())

	// 公开路由（无需认证）
	s.engine.POST("/auth/token", s.handleAuthToken)
	s.engine.GET("/health", s.handleHealth)

	// 受保护路由：JWT 未配置时中间件是 no-op（开发/测试不受影响）
	protected := s.engine.Group("/")
	protected.Use(AuthRequired(s.jwtSecret))
	protected.POST("/auth/refresh", s.handleRefreshToken)
	protected.POST("/chat", s.handleChat)
	protected.POST("/chat/stream", s.handleChatStream)
	protected.POST("/reload", s.handleReload)
	protected.GET("/history/:user_id", s.handleHistory)
	if s.tickets != nil {
		protected.GET("/tickets", s.handleListTickets)
	}
	if s.memoryStore != nil {
		protected.GET("/memory/:user_id", s.handleGetMemory)
		protected.PUT("/memory/:user_id", s.handleUpdateMemory)
	}
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
