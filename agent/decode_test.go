package agent

import (
	"fmt"
	"testing"
)

// ---- DecodeInput ----

func TestDecodeInput_Success(t *testing.T) {
	type Req struct {
		OrderID string `json:"order_id"`
		Count   int    `json:"count"`
	}
	got, err := DecodeInput[Req](`{"order_id":"ORDER-001","count":3}`)
	if err != nil {
		t.Fatal(err)
	}
	if got.OrderID != "ORDER-001" || got.Count != 3 {
		t.Errorf("got %+v", got)
	}
}

func TestDecodeInput_InvalidJSON(t *testing.T) {
	_, err := DecodeInput[struct{ X int }]("not json")
	if err == nil {
		t.Fatal("期望解析错误，got nil")
	}
}

func TestDecodeInput_EmptyObject(t *testing.T) {
	type Req struct{ X int }
	got, err := DecodeInput[Req]("{}")
	if err != nil {
		t.Fatal(err)
	}
	if got.X != 0 {
		t.Errorf("期望零值，got %+v", got)
	}
}

// ---- DecodeAndValidate（带 Validator）----

type validatedReq struct {
	OrderID string `json:"order_id"`
}

func (r *validatedReq) Validate() error {
	if r.OrderID == "" {
		return fmt.Errorf("order_id 不能为空")
	}
	return nil
}

func TestDecodeAndValidate_PassesValidation(t *testing.T) {
	got, err := DecodeAndValidate[validatedReq](`{"order_id":"ORDER-001"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got.OrderID != "ORDER-001" {
		t.Errorf("got %+v", got)
	}
}

func TestDecodeAndValidate_FailsValidation(t *testing.T) {
	_, err := DecodeAndValidate[validatedReq](`{"order_id":""}`)
	if err == nil {
		t.Fatal("期望校验错误，got nil")
	}
}

func TestDecodeAndValidate_NoValidatorInterface(t *testing.T) {
	// 不实现 Validator 的结构体：解码成功即可，不调用 Validate
	type Plain struct{ X int }
	_, err := DecodeAndValidate[Plain](`{"x":1}`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDecodeAndValidate_InvalidJSONBeforeValidate(t *testing.T) {
	// JSON 格式错误时不进入校验
	_, err := DecodeAndValidate[validatedReq]("bad json")
	if err == nil {
		t.Fatal("期望解析错误，got nil")
	}
}
