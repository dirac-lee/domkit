package config

import (
	"encoding"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"time"
)

// 转换阶段的哨兵错误。
var (
	// ErrKeyNotFound 指定配置键不存在。
	ErrKeyNotFound = errors.New("config key not found")
	// ErrUnsupportedType 目标类型无可用转换器（既非内建类型也未实现 TextUnmarshaler）。
	ErrUnsupportedType = errors.New("config unsupported target type")
)

// Convert 把原始字符串转换为目标类型 T；非法值返回转换错误。
// 内建类型走类型 switch（零反射）；其余类型尝试 encoding.TextUnmarshaler。
func Convert[T any](raw string) (T, error) {
	var zero T
	switch any(zero).(type) {
	case string:
		return any(raw).(T), nil
	case bool:
		v, err := strconv.ParseBool(raw)
		return any(v).(T), err
	case time.Duration:
		v, err := time.ParseDuration(raw)
		return any(v).(T), err
	// 有符号整数族：ParseInt 按位宽严格校验溢出，失败时 v=0，断言为 T 零值并连同 err 返回。
	case int:
		v, err := strconv.ParseInt(raw, 0, strconv.IntSize)
		return any(int(v)).(T), err
	case int8:
		v, err := strconv.ParseInt(raw, 0, 8)
		return any(int8(v)).(T), err
	case int16:
		v, err := strconv.ParseInt(raw, 0, 16)
		return any(int16(v)).(T), err
	case int32:
		v, err := strconv.ParseInt(raw, 0, 32)
		return any(int32(v)).(T), err
	case int64:
		v, err := strconv.ParseInt(raw, 0, 64)
		return any(int64(v)).(T), err
	// 无符号整数族。
	case uint:
		v, err := strconv.ParseUint(raw, 0, strconv.IntSize)
		return any(uint(v)).(T), err
	case uint8:
		v, err := strconv.ParseUint(raw, 0, 8)
		return any(uint8(v)).(T), err
	case uint16:
		v, err := strconv.ParseUint(raw, 0, 16)
		return any(uint16(v)).(T), err
	case uint32:
		v, err := strconv.ParseUint(raw, 0, 32)
		return any(uint32(v)).(T), err
	case uint64:
		v, err := strconv.ParseUint(raw, 0, 64)
		return any(uint64(v)).(T), err
	case uintptr:
		v, err := strconv.ParseUint(raw, 0, strconv.IntSize)
		return any(uintptr(v)).(T), err
	// 浮点族。
	case float32:
		v, err := strconv.ParseFloat(raw, 32)
		return any(float32(v)).(T), err
	case float64:
		v, err := strconv.ParseFloat(raw, 64)
		return any(float64(v)).(T), err
	default:
		// 自定义类型（如富枚举）：经反射走 TextUnmarshaler。
		return unmarshalText[T](raw)
	}
}

// unmarshalText 处理实现了 encoding.TextUnmarshaler 的自定义类型（含指针/值两种形态）。
// 仅在自定义类型分支使用反射，内建类型的常规读取不受影响。
func unmarshalText[T any](raw string) (T, error) {
	var zero T
	t := reflect.TypeOf((*T)(nil)).Elem()

	// 统一取「指针指向的元素类型」，便于构造可寻址实例调用 UnmarshalText。
	elem := t
	if t.Kind() == reflect.Ptr {
		elem = t.Elem()
	}
	holder := reflect.New(elem)

	u, ok := holder.Interface().(encoding.TextUnmarshaler)
	if !ok {
		return zero, fmt.Errorf("%w: %s", ErrUnsupportedType, t)
	}
	if err := u.UnmarshalText([]byte(raw)); err != nil {
		return zero, err
	}

	// T 为指针类型：holder(*elem) 即 T；T 为值类型：取 holder 指向的元素。
	if t.Kind() == reflect.Ptr {
		return holder.Interface().(T), nil
	}
	return holder.Elem().Interface().(T), nil
}
