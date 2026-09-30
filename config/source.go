package config

// Source 配置原始键值源（L1 抽象）。
// 屏蔽底层配置后端（内存 Map、环境变量、Nacos、Apollo 等）的差异，
// 仅暴露原始字符串键值的读取能力；类型化与语义化由其上的 Get / Bind / Toggle 完成。
type Source interface {
	// Lookup 读取原始字符串；ok=false 表示键不存在，
	// 与「键存在但值为空串」严格区分，避免缺失误判。
	Lookup(key string) (value string, ok bool)

	// Keys 返回当前源中全部键，供 Binder 按前缀扫描。
	Keys() []string
}

// lookup 是 Source 的便捷取值包装：键不存在时返回传入的兜底值。
func lookup(src Source, key string, def string) string {
	v, ok := src.Lookup(key)
	if !ok {
		return def
	}
	return v
}

// contains 判断键是否存在（供 Binder / 白名单策略判定）。
func contains(src Source, key string) bool {
	_, ok := src.Lookup(key)
	return ok
}
