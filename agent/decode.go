package agent

import (
	"encoding/json"
	"fmt"
)

// Validator 是可选的参数校验接口。
// 工具参数结构体实现此接口后，DecodeAndValidate 会自动调用 Validate()。
// 不实现则跳过校验，向后兼容。
type Validator interface {
	Validate() error
}

// DecodeInput 是泛型 JSON 解码函数，消除每个工具重复写 json.Unmarshal 的样板代码。
func DecodeInput[T any](input string) (T, error) {
	var v T
	if err := json.Unmarshal([]byte(input), &v); err != nil {
		return v, fmt.Errorf("参数解析失败: %w", err)
	}
	return v, nil
}

// DecodeAndValidate 解码后自动触发 Validate()（如果结构体实现了 Validator 接口）。
// 检查指针接收者：pointer receiver 和 value receiver 实现的 Validate() 均可触发。
func DecodeAndValidate[T any](input string) (T, error) {
	v, err := DecodeInput[T](input)
	if err != nil {
		return v, err
	}
	if validator, ok := any(&v).(Validator); ok {
		if err := validator.Validate(); err != nil {
			return v, fmt.Errorf("参数校验失败: %w", err)
		}
	}
	return v, nil
}
