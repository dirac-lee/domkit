package domain

import "reflect"

// ValueObject 值对象接口：不可变，基于属性相等判断
type ValueObject interface {
	EqualityComponents() []any
}

// Equal 判断两个值对象是否相等。
// 相等性分量可能包含切片/map 等不可直接用 == 比较的类型，
// 因此统一使用 reflect.DeepEqual，避免运行时 panic。
func Equal(a, b ValueObject) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	ca := a.EqualityComponents()
	cb := b.EqualityComponents()
	if len(ca) != len(cb) {
		return false
	}
	for i := range ca {
		if !reflect.DeepEqual(ca[i], cb[i]) {
			return false
		}
	}
	return true
}
