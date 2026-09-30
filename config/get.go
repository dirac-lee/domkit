package config

import "fmt"

// Get 强类型读取：键缺失返回 ErrKeyNotFound，值非法返回转换错误。
func Get[T any](src Source, key string) (T, error) {
	raw, ok := src.Lookup(key)
	if !ok {
		var zero T
		return zero, fmt.Errorf("%w: %s", ErrKeyNotFound, key)
	}
	return Convert[T](raw)
}

// GetOr 带兜底读取：键缺失或转换失败时均返回 def，适合可选配置。
func GetOr[T any](src Source, key string, def T) T {
	v, err := Get[T](src, key)
	if err != nil {
		return def
	}
	return v
}
