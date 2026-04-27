package main

import (
	"strings"
	"testing"
)

func TestCancelOrderReq_Validate_Valid(t *testing.T) {
	req := CancelOrderReq{OrderID: "ORDER-001", Reason: "不想要了"}
	if err := req.Validate(); err != nil {
		t.Errorf("合法输入应通过校验，got: %v", err)
	}
}

func TestCancelOrderReq_Validate_EmptyOrderID(t *testing.T) {
	req := CancelOrderReq{OrderID: "", Reason: "原因"}
	if err := req.Validate(); err == nil {
		t.Error("空 order_id 应失败")
	}
}

func TestCancelOrderReq_Validate_InvalidOrderIDFormat(t *testing.T) {
	// 格式必须是 ORDER-数字，其余均不合法
	cases := []string{
		"order-001",  // 小写
		"ORDER-abc",  // 含字母
		"ORD-001",    // 前缀错误
		"ORDER-",     // 无数字
		"ORDER-00 1", // 含空格
	}
	for _, id := range cases {
		req := CancelOrderReq{OrderID: id, Reason: "原因"}
		if err := req.Validate(); err == nil {
			t.Errorf("格式错误的 order_id %q 应校验失败", id)
		}
	}
}

func TestCancelOrderReq_Validate_ValidOrderIDFormats(t *testing.T) {
	cases := []string{"ORDER-1", "ORDER-001", "ORDER-999999"}
	for _, id := range cases {
		req := CancelOrderReq{OrderID: id, Reason: "原因"}
		if err := req.Validate(); err != nil {
			t.Errorf("合法 order_id %q 应通过，got: %v", id, err)
		}
	}
}

func TestCancelOrderReq_Validate_EmptyReason(t *testing.T) {
	req := CancelOrderReq{OrderID: "ORDER-001", Reason: ""}
	if err := req.Validate(); err == nil {
		t.Error("空原因应失败")
	}
}

func TestCancelOrderReq_Validate_ReasonExactly100Chars(t *testing.T) {
	// 恰好 100 个字符应通过（边界值）
	reason := strings.Repeat("字", 100)
	req := CancelOrderReq{OrderID: "ORDER-001", Reason: reason}
	if err := req.Validate(); err != nil {
		t.Errorf("恰好 100 字符应通过，got: %v", err)
	}
}

func TestCancelOrderReq_Validate_ReasonOver100Chars(t *testing.T) {
	// 101 个字符应失败
	reason := strings.Repeat("字", 101)
	req := CancelOrderReq{OrderID: "ORDER-001", Reason: reason}
	if err := req.Validate(); err == nil {
		t.Error("超过 100 字符的原因应失败")
	}
}

func TestCancelOrderReq_Validate_OrderValidationBeforeReason(t *testing.T) {
	// order_id 格式错误时，不应走到 reason 的校验（快速失败）
	req := CancelOrderReq{OrderID: "INVALID", Reason: ""}
	err := req.Validate()
	if err == nil {
		t.Fatal("应失败")
	}
	// 错误信息应提到 order_id 格式，而非 reason 为空
	if strings.Contains(err.Error(), "reason") {
		t.Errorf("应先报 order_id 格式错误，got: %v", err)
	}
}
