package config

import "strings"

// GrayStrategy 灰度策略接口（SPI）：特性处于 Rollout 状态时，
// 由具体策略决定是否对当前上下文放量。
type GrayStrategy interface {
	Matches(featureKey string, ctx *FeatureContext) bool
}

// allowKeyPrefix 白名单配置的中段：{featureKey}.allow.{dimension}。
const allowKeyPrefix = ".allow."

// WhitelistStrategy 基于白名单的内置灰度策略，对齐 Java WhitelistGrayStrategy。
// 白名单按维度分组配置：{featureKey}.allow.{dimension}=值1,值2,...；
// 灰度上下文中任意维度取值命中其对应白名单即放量。
type WhitelistStrategy struct {
	src Source
}

// NewWhitelistStrategy 构造白名单策略。
func NewWhitelistStrategy(src Source) *WhitelistStrategy {
	return &WhitelistStrategy{src: src}
}

// Matches 实现 GrayStrategy；任意维度命中即返回 true。
func (s *WhitelistStrategy) Matches(featureKey string, ctx *FeatureContext) bool {
	// 无灰度上下文（nil）无法命中任何维度，提前返回 false，也避免解引用 nil 指针。
	if ctx == nil {
		return false
	}
	allowRoot := featureKey + allowKeyPrefix
	hit := false

	// 命中即停止遍历；每个维度独立判定。
	ctx.Each(func(dimension, value string) bool {
		if s.hits(allowRoot, dimension, value) {
			hit = true
			return false
		}
		return true
	})
	return hit
}

// hits 判断某维度的取值是否落在该维度白名单内；空值/未配置白名单提前返回 false。
func (s *WhitelistStrategy) hits(allowRoot, dimension, value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	allow := lookup(s.src, allowRoot+dimension, "")
	if strings.TrimSpace(allow) == "" {
		return false
	}
	return inWhitelist(value, allow)
}

// inWhitelist 把逗号分隔的白名单拆分为去空白、去空项的集合，判断 value 是否在内。
func inWhitelist(value, allow string) bool {
	for _, item := range strings.Split(allow, ",") {
		if strings.TrimSpace(item) == value {
			return true
		}
	}
	return false
}
