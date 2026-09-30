package config

import (
	"fmt"
	"reflect"
	"strings"
)

// configTag 字段上 `config:"..."` tag 解析后的形态。
type configTag struct {
	name     string // 键名（为空则用字段名转 kebab）
	skip     bool   // name=="-" 时忽略该字段
	required bool   // 缺失即报错
	hasDef   bool   // 是否声明 default
	def      string // default 的原始字符串值
}

// BindingError 配置绑定失败（缺必填、类型不符、反射异常等），对齐 Java ConfigurationBindingException。
type BindingError struct {
	msg   string
	cause error
}

func (e *BindingError) Error() string { return e.msg }

// Unwrap 暴露根因，支持 errors.Is/As。
func (e *BindingError) Unwrap() error { return e.cause }

// bindSettings 绑定可选项聚合，避免 Bind 出现过长参数。
type bindSettings struct {
	defaults reflect.Value // 整体默认值实例（零值表示未提供）
}

// BindOption 绑定选项。
type BindOption func(*bindSettings)

// WithDefault 传入整体默认值实例：配置缺失的字段取其同名字段兜底（对应 Java 宽松模式）。
func WithDefault[T any](def T) BindOption {
	return func(s *bindSettings) {
		s.defaults = reflect.ValueOf(def)
	}
}

// Bind 把 prefix 前缀下的键值绑定为 T（struct），返回其指针。
// 默认宽松：缺失字段保留零值或取 default；tag 标 required 的字段缺失才报错。
func Bind[T any](src Source, prefix string, opts ...BindOption) (*T, error) {
	settings := &bindSettings{}
	for _, opt := range opts {
		opt(settings)
	}

	var result T
	target := reflect.ValueOf(&result).Elem()
	// 目标必须是 struct，否则无法按字段绑定。
	if target.Kind() != reflect.Struct {
		return nil, &BindingError{msg: fmt.Sprintf("绑定目标必须是 struct，得到 %s", target.Kind())}
	}

	if err := bindStruct(src, prefix, target, settings.defaults); err != nil {
		return nil, err
	}
	return &result, nil
}

// bindStruct 递归绑定一个 struct 值；def 为对应的默认值（无效表示无默认）。
func bindStruct(src Source, prefix string, target reflect.Value, def reflect.Value) error {
	t := target.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !target.Field(i).CanSet() {
			continue // 非导出字段无法设置，直接跳过。
		}
		if err := bindField(src, prefix, field, target.Field(i), defField(def, i)); err != nil {
			return err
		}
	}
	return nil
}

// bindField 处理单个字段：嵌入/struct 递归，其余走标量绑定。
func bindField(src Source, prefix string, field reflect.StructField, dst, def reflect.Value) error {
	tag := parseConfigTag(field)
	if tag.skip {
		return nil
	}

	// struct 字段：嵌入不追加键名，非嵌入追加字段名，递归绑定其子字段。
	if dst.Kind() == reflect.Struct {
		nextPrefix := prefix
		if !field.Anonymous {
			nextPrefix = joinKey(prefix, keyName(tag, field))
		}
		return bindStruct(src, nextPrefix, dst, def)
	}
	return bindScalar(src, prefix, tag, field, dst, def)
}

// bindScalar 绑定标量字段：源命中→转换；否则按 required/default/整体默认值 的顺序兜底。
func bindScalar(src Source, prefix string, tag configTag, field reflect.StructField, dst, def reflect.Value) error {
	key := joinKey(prefix, keyName(tag, field))

	// 1. 源中命中：直接转换并赋值。
	if raw, ok := src.Lookup(key); ok {
		return assignConverted(dst, raw, key)
	}
	// 2. 必填字段缺失：立即报错（fail-fast）。
	if tag.required {
		return &BindingError{msg: fmt.Sprintf("缺少必需的配置项[%s]（绑定字段：%s）", key, field.Name)}
	}
	// 3. tag 内 default：按字段类型转换。
	if tag.hasDef {
		return assignConverted(dst, tag.def, key)
	}
	// 4. 整体默认值实例的同名字段：拷贝过来。
	if def.IsValid() && !def.IsZero() {
		dst.Set(def)
	}
	return nil
}

// assignConverted 把原始字符串转换为 dst 的类型并写入；失败统一包成 BindingError。
func assignConverted(dst reflect.Value, raw, key string) error {
	converted, err := convertForType(dst.Type(), raw)
	if err != nil {
		return &BindingError{
			msg:   fmt.Sprintf("配置项[%s]绑定失败：%v", key, err),
			cause: err,
		}
	}
	dst.Set(converted)
	return nil
}

// parseConfigTag 解析字段 tag；无 tag 时返回零值（调用方据此用字段名）。
func parseConfigTag(field reflect.StructField) configTag {
	raw, ok := field.Tag.Lookup("config")
	if !ok {
		return configTag{}
	}

	parts := strings.Split(raw, ",")
	tag := configTag{name: strings.TrimSpace(parts[0])}
	if tag.name == "-" {
		tag.skip = true
		return tag
	}
	for _, p := range parts[1:] {
		p = strings.TrimSpace(p)
		switch {
		case p == "required":
			tag.required = true
		case strings.HasPrefix(p, "default="):
			tag.hasDef = true
			tag.def = strings.TrimPrefix(p, "default=")
		}
	}
	return tag
}

// keyName 取字段对外键名：tag 指定优先，否则字段名转 kebab-case。
func keyName(tag configTag, field reflect.StructField) string {
	if tag.name != "" {
		return tag.name
	}
	return toKebab(field.Name)
}

// joinKey 拼接前缀与键名，空前缀不产生多余的点。
func joinKey(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

// toKebab 驼峰 → kebab-case，对齐 Java Binder 的短横线键风格（PoolSize→pool-size）。
func toKebab(name string) string {
	var sb strings.Builder
	for i, r := range name {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				sb.WriteByte('-')
			}
			sb.WriteRune(r + ('a' - 'A'))
			continue
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

// defField 安全取默认值实例的第 i 个字段；默认值无效时返回零 Value。
func defField(def reflect.Value, i int) reflect.Value {
	if !def.IsValid() || def.Kind() != reflect.Struct {
		return reflect.Value{}
	}
	return def.Field(i)
}
