package broadcast

import "encoding/json"

// Serializer 信封序列化端口：把信封对象序列化为字符串后交给 Messenger 发送。
// 引用方可替换为 JSON 以外的对接协议实现。
type Serializer interface {
	Serialize(v any) (string, error)
}

// JSONSerializer 零依赖内置的 JSON 序列化器。
type JSONSerializer struct{}

// NewJSONSerializer 创建内置 JSON 序列化器。
func NewJSONSerializer() JSONSerializer { return JSONSerializer{} }

// Serialize 以紧凑 JSON 序列化信封；序列化失败由调用方包装为不可重试错误。
func (JSONSerializer) Serialize(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
