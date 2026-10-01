package mysql

import (
	"strings"
	"testing"

	orderdomain "github.com/dirac-lee/domkit/example/order/internal/order/domain"
)

func TestStatusFromValue(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  orderdomain.OrderStatus
	}{
		{name: "created", value: orderdomain.StatusCreated.Value(), want: orderdomain.StatusCreated},
		{name: "paid", value: orderdomain.StatusPaid.Value(), want: orderdomain.StatusPaid},
		{name: "cancelled", value: orderdomain.StatusCancelled.Value(), want: orderdomain.StatusCancelled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := statusFromValue(tt.value)
			if err != nil {
				t.Fatalf("statusFromValue() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("statusFromValue() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStatusFromValueRejectsUnknownStatus(t *testing.T) {
	_, err := statusFromValue("broken")
	if err == nil {
		t.Fatal("statusFromValue() should reject unknown status")
	}
	if !strings.Contains(err.Error(), `unknown order status "broken"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPOToOrderRejectsUnknownStatus(t *testing.T) {
	po := &OrderPO{
		ID:      "order-1",
		OrderNo: "NO-1",
		Amount:  100,
		Status:  "broken",
		Version: 2,
	}

	o, err := poToOrder(po)
	if err == nil {
		t.Fatal("poToOrder() should reject unknown status")
	}
	if o != nil {
		t.Fatalf("poToOrder() should not return order on invalid status: %+v", o)
	}
}
