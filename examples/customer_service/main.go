// customer_service 是整合所有组件的完整客服系统。
// 整合内容：LLM Client + Session 管理 + Agent Loop + RAG 知识库 + HTTP 服务器
//
// 运行：go run ./examples/customer_service/
// 测试：curl -X POST http://localhost:8080/chat -d '{"user_id":"u1","message":"退款要多久？"}'
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/pengpn/go-llm-agent/agent"
	"github.com/pengpn/go-llm-agent/client"
	"github.com/pengpn/go-llm-agent/config"
	"github.com/pengpn/go-llm-agent/rag"
	"github.com/pengpn/go-llm-agent/server"
	"github.com/pengpn/go-llm-agent/session"
)

const systemPrompt = `你是一个专业的订单客服助手。

你有以下工具可以使用：
- search_knowledge_base：在知识库中搜索相关内容
- transfer_to_human：将对话转接给人工客服并生成工单

处理用户问题时：
1. 先判断是否需要查阅知识库（订单、退款、物流、支付等业务问题需要查）
2. 对于闲聊、感谢等简单交互，直接回答无需查询
3. 检索到内容后，严格基于参考资料回答，不要编造信息
4. 遇到以下情况时，必须调用 transfer_to_human 转接人工：
   - 知识库中确实没有答案
   - 用户明确要求"转人工"或"联系客服"
   - 涉及退款金额较大的争议
   - 用户情绪激动或提到投诉、法律等词语`

func main() {
	// 初始化结构化日志（JSON 格式，方便接入日志平台）
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	cfg, err := config.Load("config.yaml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "配置加载失败: %v\n", err)
		os.Exit(1)
	}

	// config.Load() 内部已调用 godotenv.Load()，此处可直接读取 .env 里的值
	embedAPIKey := os.Getenv("DASHSCOPE_API_KEY")
	if embedAPIKey == "" {
		fmt.Fprintln(os.Stderr, "错误：请在 .env 中设置 DASHSCOPE_API_KEY")
		os.Exit(1)
	}

	ctx := context.Background()

	// ── 初始化 RAG 组件 ──────────────────────────────
	embedder := rag.NewQwenEmbedder(embedAPIKey)

	qdrantURL := os.Getenv("QDRANT_URL")
	if qdrantURL == "" {
		qdrantURL = "http://localhost:6333" // 本地开发默认值
	}
	store, err := rag.NewQdrantStore(ctx, qdrantURL, "order_faq", 1024)
	if err != nil {
		fmt.Fprintf(os.Stderr, "连接 Qdrant 失败（确认 Docker 已启动）: %v\n", err)
		os.Exit(1)
	}

	pipeline := rag.NewPipeline(rag.NewFixedSizeChunker(500, 50), embedder, store)
	retriever := rag.NewRetriever(embedder, store, 3, rag.WithMinScore(0.5))

	// ── 建立知识库索引 ───────────────────────────────
	faqDocs := buildFAQDocs()
	slog.Info("indexing knowledge base", "count", len(faqDocs))
	for _, doc := range faqDocs {
		text := doc.Title + "\n" + doc.Content
		if err := pipeline.IndexText(ctx, text, doc.Title); err != nil {
			fmt.Fprintf(os.Stderr, "索引 [%s] 失败: %v\n", doc.Title, err)
			os.Exit(1)
		}
	}
	slog.Info("knowledge base ready")

	// ── 初始化 LLM Client 和 Agent ───────────────────
	llmClient := client.NewFromConfig(&cfg.LLM)

	// ── 初始化工单存储（人工转接，Lesson 10）────────────
	ticketStore := server.NewTicketStore()

	registry := agent.NewRegistry()
	registry.Register(rag.NewSearchKBTool(retriever))
	registry.Register(server.NewTransferTool(ticketStore))

	ag := agent.New(llmClient, registry)

	// ── 初始化 Session Manager ───────────────────────
	// TTL 2 小时：超时未活跃的 Session 自动清理，释放内存
	sessions := session.NewManager(
		session.WithTTL(2*time.Hour),
		session.WithDefaultSystemPrompt(systemPrompt),
		session.WithTruncateStrategy(&session.ByTurns{MaxTurns: 20}),
	)

	// ── 启动 HTTP 服务器 ─────────────────────────────
	opts := []server.ServerOption{
		server.WithRateLimiter(10, time.Minute),
	}

	// JWT 认证：设置 JWT_SECRET 后自动启用（未设置则以开发模式运行，无需鉴权）
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		slog.Warn("JWT_SECRET 未设置，身份认证已禁用（开发模式）")
	} else {
		opts = append(opts, server.WithJWTSecret([]byte(jwtSecret)))
		slog.Info("JWT 认证已启用")
	}

	// API Key → UserID 映射：支持通过 POST /auth/token 颁发 JWT
	// 格式：SERVICE_API_KEY=sk-xxx  对应 user_id 为 "service"
	apiKey := os.Getenv("SERVICE_API_KEY")
	if apiKey != "" {
		opts = append(opts, server.WithAPIKeys(map[string]string{apiKey: "service"}))
	}

	// ── 自动工单提取（Lesson 13）：每次 /chat 回答后自动从对话中提取工单信息 ──
	ticketExtractor := server.NewTicketExtractor(llmClient)

	// ── 用户画像记忆（Lesson 14）：跨会话记住用户偏好，自动注入 System Prompt ──
	memoryStore := session.NewFileMemoryStore("./data/memories")
	memoryExtractor := server.NewMemoryExtractor(llmClient)
	slog.Info("用户画像记忆已启用", "dir", "./data/memories")

	srv := server.New(ag, sessions, pipeline, faqDocs,
		append(opts,
			server.WithTicketStore(ticketStore),
			server.WithTicketExtractor(ticketExtractor),
			server.WithMemoryStore(memoryStore),
			server.WithMemoryExtractor(memoryExtractor),
		)...)
	if err := srv.Run(":8080"); err != nil {
		slog.Error("server exited", "err", err)
		os.Exit(1)
	}
	slog.Info("server stopped gracefully")
}

// buildFAQDocs 返回知识库文档列表。
// 生产环境通常从数据库或 CMS 加载，这里用硬编码模拟。
func buildFAQDocs() []server.FAQDoc {
	return []server.FAQDoc{
		{
			Title: "如何查询订单状态",
			Content: `订单状态查询方式：
1. 登录后进入"我的订单"页面，可查看所有历史订单。
2. 订单状态包括：待付款、已付款、备货中、已发货、已完成、已取消。
3. 发货后可在订单详情页点击"查看物流"实时追踪包裹位置。
4. 如状态超过24小时未更新，请联系客服处理。`,
		},
		{
			Title: "订单取消规则",
			Content: `订单取消说明：
1. 待付款状态：可随时取消，无需任何费用。
2. 已付款/备货中：可申请取消，需等待商家确认，通常1-2小时内处理。
3. 已发货：无法直接取消，需在收到货后申请退货退款。
4. 已完成：订单完成后不支持取消，可申请售后退款。
5. 取消成功后，退款将在3-5个工作日原路返回。`,
		},
		{
			Title: "退款退货流程",
			Content: `退款退货操作步骤：
1. 进入订单详情，点击"申请退款"或"申请退货"。
2. 选择退款原因，上传相关凭证图片（如有）。
3. 提交申请后，客服将在24小时内审核。
4. 审核通过后：退款订单直接退款，退货订单需寄回商品。
5. 退货地址：请以客服提供的地址为准，请勿擅自寄回。
6. 收到退货后，3-5个工作日完成退款。
退款说明：支付宝/微信退款到账1-3天，银行卡退款3-5个工作日。`,
		},
		{
			Title: "物流配送时效",
			Content: `配送时效参考：
1. 普通快递：全国大部分地区3-5个工作日。
2. 次日达：下午3点前下单，次日送达（覆盖城市见配送页面）。
3. 偏远地区（新疆、西藏、内蒙古等）：7-15个工作日。
4. 港澳台及海外：暂不支持直接配送，请选择转运服务。
5. 节假日期间可能有1-3天延迟，大促期间延迟视情况而定。
如超出预计时效仍未收货，可联系客服查询。`,
		},
		{
			Title: "支付方式与发票",
			Content: `支持的支付方式：
- 微信支付、支付宝、银行卡（储蓄卡/信用卡）
- 花呗分期（满100元可用，分3/6/12期）
- 京东白条（仅限白条用户）

发票申请：
1. 下单时可选择"需要发票"并填写发票信息。
2. 支持个人发票和企业增值税专用发票。
3. 发票将随商品一起寄出，电子发票发送至邮箱。
4. 订单完成后7天内可在订单详情申请补开发票。`,
		},
		{
			Title: "客服联系方式",
			Content: `客服联系渠道：
- 在线客服：APP/网页右下角"联系客服"按钮，7×24小时在线。
- 客服热线：400-888-8888，工作时间 9:00-21:00。
- 邮件：support@example.com，1个工作日内回复。

响应时效承诺：
- 在线客服：3分钟内响应。
- 电话：等待时间不超过5分钟。
- 邮件：1个工作日内回复。`,
		},
	}
}
