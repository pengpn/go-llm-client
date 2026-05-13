package eval

// DefaultTestCases 20 个覆盖客服系统各类场景的测试用例
var DefaultTestCases = []EvalCase{
	// ── 订单查询 (5 条) ──────────────────────────────────────────
	{
		ID:             "order-001",
		Category:       "订单查询",
		Question:       "我的订单 ORDER-001 现在是什么状态？",
		ExpectedAnswer: "您的订单 ORDER-001 状态为已发货，预计 3 天内送达。",
	},
	{
		ID:             "order-002",
		Category:       "订单查询",
		Question:       "订单 ORDER-002 的收货地址是哪里？",
		ExpectedAnswer: "订单 ORDER-002 的收货地址是上海市浦东新区某某路 123 号。",
	},
	{
		ID:             "order-003",
		Category:       "订单查询",
		Question:       "我有哪些订单？",
		ExpectedAnswer: "您目前有 2 笔订单：ORDER-001（已发货）和 ORDER-002（处理中）。",
	},
	{
		ID:             "order-004",
		Category:       "订单查询",
		Question:       "ORDER-002 的金额是多少？",
		ExpectedAnswer: "订单 ORDER-002 的金额为 299 元。",
	},
	{
		ID:             "order-005",
		Category:       "订单查询",
		Question:       "我的订单什么时候会到？",
		ExpectedAnswer: "需要您提供订单号才能查询预计到货时间。ORDER-001 预计 3 天内送达。",
	},

	// ── 退款 / 取消订单 (4 条) ───────────────────────────────────
	{
		ID:             "refund-001",
		Category:       "退款政策",
		Question:       "我想取消订单 ORDER-001，可以吗？",
		ExpectedAnswer: "ORDER-001 已发货，发货后无法直接取消。您可以在收货后申请退货退款。",
	},
	{
		ID:             "refund-002",
		Category:       "退款政策",
		Question:       "退款一般需要多久到账？",
		ExpectedAnswer: "退款通常在 3-5 个工作日内原路退回到您的支付账户。",
	},
	{
		ID:             "refund-003",
		Category:       "退款政策",
		Question:       "我收到的商品有破损，怎么办？",
		ExpectedAnswer: "收到破损商品非常抱歉！请拍照留证，联系客服申请退换货，运费由我们承担。",
	},
	{
		ID:             "refund-004",
		Category:       "退款政策",
		Question:       "七天无理由退货适用吗？",
		ExpectedAnswer: "支持七天无理由退货（自收货之日起），商品需保持原包装、未使用状态。",
	},

	// ── 物流查询 (3 条) ──────────────────────────────────────────
	{
		ID:             "logistics-001",
		Category:       "物流查询",
		Question:       "ORDER-001 的快递单号是多少？",
		ExpectedAnswer: "ORDER-001 的快递单号为 SF1234567890，承运商为顺丰快递。",
	},
	{
		ID:             "logistics-002",
		Category:       "物流查询",
		Question:       "包裹现在到哪里了？",
		ExpectedAnswer: "需要提供订单号或快递单号才能查询物流轨迹。",
	},
	{
		ID:             "logistics-003",
		Category:       "物流查询",
		Question:       "快递一直显示在途，超过预计送达时间了怎么办？",
		ExpectedAnswer: "超出预计送达时间，建议联系快递公司查询。若 48 小时仍未解决，我们可以为您申请赔偿。",
	},

	// ── 商品 / 使用咨询 (4 条) ───────────────────────────────────
	{
		ID:             "product-001",
		Category:       "商品咨询",
		Question:       "这款耳机支持降噪吗？",
		ExpectedAnswer: "根据知识库，您购买的耳机支持主动降噪（ANC），降噪深度约 30dB。",
	},
	{
		ID:             "product-002",
		Category:       "商品咨询",
		Question:       "充电宝能带上飞机吗？",
		ExpectedAnswer: "容量 ≤ 20000mAh（约 74Wh）的充电宝可以随身携带登机，不可托运。",
	},
	{
		ID:             "product-003",
		Category:       "商品咨询",
		Question:       "商品保修期多久？",
		ExpectedAnswer: "电子类商品保修期为 1 年，非人为损坏免费维修；耗材类商品不在保修范围内。",
	},
	{
		ID:             "product-004",
		Category:       "商品咨询",
		Question:       "有没有使用说明书？",
		ExpectedAnswer: "商品包装内附有纸质说明书，也可在商品详情页下载电子版 PDF。",
	},

	// ── 账户 / 积分 (2 条) ───────────────────────────────────────
	{
		ID:             "account-001",
		Category:       "账户服务",
		Question:       "我的积分可以用来做什么？",
		ExpectedAnswer: "积分可在下单时抵扣现金（100 积分 = 1 元），也可兑换优惠券或商品。",
	},
	{
		ID:             "account-002",
		Category:       "账户服务",
		Question:       "忘记密码了怎么找回？",
		ExpectedAnswer: "点击登录页面的「忘记密码」，通过手机验证码或邮箱验证即可重置密码。",
	},

	// ── 边界 / 超出知识范围 (2 条) ───────────────────────────────
	{
		ID:             "edge-001",
		Category:       "边界测试",
		Question:       "你们什么时候上市？",
		ExpectedAnswer: "非常抱歉，我是客服助手，暂时没有公司上市相关信息。如有其他问题请告诉我。",
	},
	{
		ID:             "edge-002",
		Category:       "边界测试",
		Question:       "我要投诉你们的服务！",
		ExpectedAnswer: "非常抱歉给您带来不好的体验！我会为您记录投诉，也可以将您转接给人工客服处理。",
	},
}
