package domain

import "testing"

type sliceVO struct {
	parts []any
}

func (v sliceVO) EqualityComponents() []any { return v.parts }

func TestEqualWithSliceComponents(t *testing.T) {
	a := sliceVO{parts: []any{[]int{1, 2, 3}, "x"}}
	b := sliceVO{parts: []any{[]int{1, 2, 3}, "x"}}
	c := sliceVO{parts: []any{[]int{1, 2, 9}, "x"}}

	if !Equal(a, b) {
		t.Fatal("structurally identical value objects should be equal")
	}
	if Equal(a, c) {
		t.Fatal("value objects with different slice content must not be equal")
	}
}

func TestEqualNilAndLength(t *testing.T) {
	if !Equal(nil, nil) {
		t.Fatal("nil value objects should be equal")
	}
	if Equal(nil, sliceVO{}) {
		t.Fatal("nil and non-nil must not be equal")
	}
	short := sliceVO{parts: []any{"x"}}
	long := sliceVO{parts: []any{"x", "y"}}
	if Equal(short, long) {
		t.Fatal("different component count must not be equal")
	}
}
