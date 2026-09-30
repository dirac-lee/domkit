package config

import "testing"

// newToggleTestSource 构造三态 + 白名单配置源。
func newToggleTestSource() Source {
	return NewMapSourceFrom(map[string]string{
		"feature.on":                   "ON",
		"feature.off":                  "OFF",
		"feature.rollout":              "ROLLOUT",
		"feature.rollout.allow.userId": "u1,u2",
	})
}

// TestToggle_Enabled 三态与缺失键的基础判定。
func TestToggle_Enabled(t *testing.T) {
	toggle := NewToggle(newToggleTestSource())

	if !toggle.Enabled("feature.on") {
		t.Fatal("ON 应开启")
	}
	if toggle.Enabled("feature.off") {
		t.Fatal("OFF 应关闭")
	}
	if toggle.Enabled("feature.rollout") {
		t.Fatal("不带灰度上下文时 ROLLOUT 不应开启")
	}
	if toggle.Enabled("feature.missing") {
		t.Fatal("缺失键应关闭")
	}
}

// TestToggle_EnabledOr 非 ON 状态取兜底值。
func TestToggle_EnabledOr(t *testing.T) {
	toggle := NewToggle(newToggleTestSource())

	if !toggle.EnabledOr("feature.missing", true) {
		t.Fatal("缺失键应取默认 true")
	}
	if !toggle.EnabledOr("feature.off", true) {
		t.Fatal("非 ON 状态应返回兜底值 true")
	}
}

// TestToggle_StateOf 状态解析。
func TestToggle_StateOf(t *testing.T) {
	toggle := NewToggle(newToggleTestSource())

	if toggle.StateOf("feature.on") != StateOn {
		t.Fatal("应解析为 ON")
	}
	if toggle.StateOf("feature.rollout") != StateRollout {
		t.Fatal("应解析为 ROLLOUT")
	}
	if toggle.StateOf("feature.missing") != StateOff {
		t.Fatal("缺失应解析为 OFF")
	}
}

// TestToggle_Gray 灰度放量：命中白名单/未命中/空上下文，以及 OFF/ON 不受灰度影响。
func TestToggle_Gray(t *testing.T) {
	toggle := NewToggle(newToggleTestSource())

	allowed := NewFeatureContext().With("userId", "u1")
	if !toggle.EnabledFor("feature.rollout", allowed) {
		t.Fatal("白名单用户 u1 应放量")
	}
	denied := NewFeatureContext().With("userId", "u3")
	if toggle.EnabledFor("feature.rollout", denied) {
		t.Fatal("非白名单用户 u3 不应放量")
	}
	if toggle.EnabledFor("feature.rollout", nil) {
		t.Fatal("无灰度上下文不应放量")
	}
	if toggle.EnabledFor("feature.off", allowed) {
		t.Fatal("OFF 状态不应放量")
	}
	if !toggle.EnabledFor("feature.on", nil) {
		t.Fatal("ON 状态无需灰度上下文即开启")
	}
}
