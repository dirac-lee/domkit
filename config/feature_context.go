package config

// FeatureContext 特性开关的灰度上下文，承载用于灰度判定的维度键值对。
// 维度名由业务自定义（如 userId/shopId），框架不预设任何灰度维度，对齐 Java FeatureContext。
type FeatureContext struct {
	dimensions map[string]string
}

// NewFeatureContext 构造空的灰度上下文。
func NewFeatureContext() *FeatureContext {
	return &FeatureContext{dimensions: make(map[string]string)}
}

// With 写入一个维度，返回当前实例便于链式装配。
func (c *FeatureContext) With(name, value string) *FeatureContext {
	c.dimensions[name] = value
	return c
}

// Dimension 读取维度值；ok=false 表示该维度不存在。
func (c *FeatureContext) Dimension(name string) (string, bool) {
	v, ok := c.dimensions[name]
	return v, ok
}

// Each 遍历全部维度；fn 返回 false 可提前终止（用于命中即停）。
func (c *FeatureContext) Each(fn func(name, value string) bool) {
	for name, value := range c.dimensions {
		if !fn(name, value) {
			return
		}
	}
}
