package config

import "strings"

// State 特性开关的生效状态，对齐 Java ToggleState。
type State uint8

const (
	// StateOff 关闭：未命中任何放量范围时返回 false。
	StateOff State = iota
	// StateRollout 灰度：仅命中灰度策略（指定人/账号/条件）时返回 true。
	StateRollout
	// StateOn 全量开启：对任何调用均返回 true。
	StateOn
)

// String 返回状态名，便于输出与断言。
func (s State) String() string {
	switch s {
	case StateOn:
		return "ON"
	case StateRollout:
		return "ROLLOUT"
	default:
		return "OFF"
	}
}

// parseState 解析配置原始值为 State；空白或无法识别一律按 Off 处理。
func parseState(raw string) State {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "ON":
		return StateOn
	case "ROLLOUT":
		return StateRollout
	default:
		return StateOff
	}
}
