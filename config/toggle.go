package config

// Toggle 特性开关门面（L3）：在原始键值之上提供三态判定与灰度放量。
type Toggle struct {
	src  Source
	gray GrayStrategy
}

// ToggleOption 装配选项，避免 NewToggle 参数列表膨胀。
type ToggleOption func(*Toggle)

// WithGrayStrategy 注入自定义灰度策略；默认使用白名单策略。
func WithGrayStrategy(strategy GrayStrategy) ToggleOption {
	return func(t *Toggle) { t.gray = strategy }
}

// NewToggle 基于配置源构造开关，灰度策略缺省为白名单。
func NewToggle(src Source, opts ...ToggleOption) *Toggle {
	t := &Toggle{src: src, gray: NewWhitelistStrategy(src)}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// Enabled 判断特性是否开启（不带灰度上下文）：仅 On 返回 true。
func (t *Toggle) Enabled(featureKey string) bool {
	return t.StateOf(featureKey) == StateOn
}

// EnabledOr 带兜底：On→true；其余状态（Off/Rollout）→ def。
func (t *Toggle) EnabledOr(featureKey string, def bool) bool {
	if t.StateOf(featureKey) == StateOn {
		return true
	}
	return def
}

// EnabledFor 携带灰度上下文判定：On→true；Off→false；Rollout 交由灰度策略。
func (t *Toggle) EnabledFor(featureKey string, ctx *FeatureContext) bool {
	switch t.StateOf(featureKey) {
	case StateOn:
		return true
	case StateRollout:
		return t.gray.Matches(featureKey, ctx)
	default:
		return false
	}
}

// StateOf 返回特性当前状态；键缺失按 Off 处理。
func (t *Toggle) StateOf(featureKey string) State {
	raw, ok := t.src.Lookup(featureKey)
	if !ok {
		return StateOff
	}
	return parseState(raw)
}
