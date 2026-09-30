package domain

// MessageCode 规则违反消息码（不可变值对象）。
// 作为规则注册表的 key 与领域异常 code，身份仅由 Code 决定；
// Description 为消息模板，可包含 fmt.Sprintf 风格占位符，配合参数化规则渲染。
type MessageCode struct {
	// Code 局部错误码，全局唯一，例如 "ORDER_AMOUNT_INVALID"。
	Code string
	// Description 规则描述模板，例如 "订单金额必须大于 0，当前值：%d"。
	Description string
}

// NewMessageCode 创建消息码。
func NewMessageCode(code, description string) MessageCode {
	return MessageCode{Code: code, Description: description}
}
