package acl

// CallLogger 外部调用日志钩子：在调用套路的关键节点触发，
// 便于业务侧输出日志、埋点或链路追踪，但不绑定任何具体日志框架。
//   - Q：对方接口请求类型
//   - S：对方接口响应类型
type CallLogger[Q, S any] interface {
	// OnRequest 请求转换完成、真正调用对方前触发。
	OnRequest(request Q)
	// OnResponse 收到对方响应、响应转换前触发。
	OnResponse(response S)
	// OnError 转换或调用环节出错时触发（参数为原始错误，未经 ACL 包装）。
	OnError(err error)
}

// NopCallLogger 不输出任何内容的默认日志钩子。
// 调用方未显式提供 logger 时使用，保证套路始终有一个非 nil 的钩子可调。
type NopCallLogger[Q, S any] struct{}

func (NopCallLogger[Q, S]) OnRequest(Q)   {}
func (NopCallLogger[Q, S]) OnResponse(S)  {}
func (NopCallLogger[Q, S]) OnError(error) {}
