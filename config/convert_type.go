package config

import (
	"encoding"
	"reflect"
	"strconv"
	"time"
)

// convertForType 按目标类型 t 转换原始字符串，返回可直接 Set 的值。
// 用于反射绑定路径，因此按 reflect.Kind 分派；借助 Convert 支持命名类型（如 type Port int）。
func convertForType(t reflect.Type, raw string) (reflect.Value, error) {
	switch t.Kind() {
	case reflect.String:
		return reflect.ValueOf(raw).Convert(t), nil
	case reflect.Bool:
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return reflect.Value{}, err
		}
		return reflect.ValueOf(v).Convert(t), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		// 精确匹配 time.Duration：按 Duration 语法解析（5s/100ms）。
		if t == reflect.TypeOf(time.Duration(0)) {
			v, err := time.ParseDuration(raw)
			if err != nil {
				return reflect.Value{}, err
			}
			return reflect.ValueOf(v).Convert(t), nil
		}
		v, err := strconv.ParseInt(raw, 0, t.Bits())
		if err != nil {
			return reflect.Value{}, err
		}
		return reflect.ValueOf(v).Convert(t), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		v, err := strconv.ParseUint(raw, 0, t.Bits())
		if err != nil {
			return reflect.Value{}, err
		}
		return reflect.ValueOf(v).Convert(t), nil
	case reflect.Float32, reflect.Float64:
		v, err := strconv.ParseFloat(raw, t.Bits())
		if err != nil {
			return reflect.Value{}, err
		}
		return reflect.ValueOf(v).Convert(t), nil
	default:
		return unmarshalTextForType(t, raw)
	}
}

// unmarshalTextForType 处理实现 encoding.TextUnmarshaler 的类型（指针/值两种形态）。
func unmarshalTextForType(t reflect.Type, raw string) (reflect.Value, error) {
	elem := t
	if t.Kind() == reflect.Ptr {
		elem = t.Elem()
	}
	holder := reflect.New(elem)

	u, ok := holder.Interface().(encoding.TextUnmarshaler)
	if !ok {
		return reflect.Value{}, ErrUnsupportedType
	}
	if err := u.UnmarshalText([]byte(raw)); err != nil {
		return reflect.Value{}, err
	}
	if t.Kind() == reflect.Ptr {
		return holder, nil
	}
	return holder.Elem(), nil
}
