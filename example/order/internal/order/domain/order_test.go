package domain

import (
	"testing"
	"time"
)

func TestCreateOrderMarksAuditTime(t *testing.T) {
	o, err := CreateOrder("order-1", "NO-1", 100)
	if err != nil {
		t.Fatalf("CreateOrder failed: %v", err)
	}
	if o.CreatedAt.IsZero() || o.UpdatedAt.IsZero() {
		t.Fatal("CreateOrder should initialize audit timestamps")
	}
	if !o.CreatedAt.Equal(o.UpdatedAt) {
		t.Fatal("CreateOrder should use the same timestamp for CreatedAt and UpdatedAt")
	}
}

func TestOrderBehaviorsMarkModified(t *testing.T) {
	o, err := CreateOrder("order-1", "NO-1", 100)
	if err != nil {
		t.Fatalf("CreateOrder failed: %v", err)
	}
	createdAt := o.CreatedAt
	o.UpdatedAt = time.Now().Add(-time.Second)

	if err := o.Pay(); err != nil {
		t.Fatalf("Pay failed: %v", err)
	}
	if !o.CreatedAt.Equal(createdAt) {
		t.Fatal("Pay must not change CreatedAt")
	}
	if !o.UpdatedAt.After(createdAt) {
		t.Fatal("Pay should refresh UpdatedAt")
	}
}
