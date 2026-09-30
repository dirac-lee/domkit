package config

// Context 配置上下文：聚合 L1 配置源与 L3 特性开关，对齐 Java IConfigurationContext。
// 使用方只需注入此一个对象，即可读取配置或判定开关；需要窄接口做测试替身时由使用方按需定义。
type Context struct {
	src    Source
	toggle *Toggle
}

// ContextOption 装配选项。
type ContextOption func(*Context)

// WithToggle 注入自定义特性开关；默认基于同一配置源构造。
func WithToggle(toggle *Toggle) ContextOption {
	return func(c *Context) { c.toggle = toggle }
}

// NewContext 基于配置源构造上下文，开关缺省从该源派生。
func NewContext(src Source, opts ...ContextOption) *Context {
	c := &Context{src: src, toggle: NewToggle(src)}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Source 返回底层配置源。
func (c *Context) Source() Source { return c.src }

// Toggle 返回底层特性开关。
func (c *Context) Toggle() *Toggle { return c.toggle }
