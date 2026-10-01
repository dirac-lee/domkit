package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	orderapp "github.com/dirac-lee/domkit/example/order/internal/order"
	orderdomain "github.com/dirac-lee/domkit/example/order/internal/order/domain"
	"github.com/dirac-lee/domkit/readmodel"
)

type listReadModelStub struct {
	called bool
	page   int
	size   int
}

func (s *listReadModelStub) ReplicaID() string { return "test:summary" }

func (s *listReadModelStub) ReadVersion(context.Context, orderdomain.OrderID) (int64, error) {
	return 0, nil
}

func (s *listReadModelStub) Rebuild(context.Context, orderdomain.OrderID) error { return nil }

func (s *listReadModelStub) PurgeOrphan(context.Context, orderdomain.OrderID) error { return nil }

func (s *listReadModelStub) Sync(context.Context, orderdomain.OrderID) error { return nil }

func (s *listReadModelStub) GetByID(context.Context, orderdomain.OrderID) (orderdomain.OrderSummary, bool, error) {
	return orderdomain.OrderSummary{}, false, nil
}

func (s *listReadModelStub) Page(_ context.Context,
	req readmodel.PageRequest) (readmodel.PageResult[orderdomain.OrderSummary], error) {
	s.called = true
	s.page = req.PageNumber()
	s.size = req.PageSize()
	return readmodel.NewPageResult([]orderdomain.OrderSummary{}, 0, req), nil
}

func TestListRejectsInvalidQueryParameters(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "invalid page", path: "/orders?page=abc"},
		{name: "invalid page size", path: "/orders?pageSize=abc"},
		{name: "page below minimum", path: "/orders?page=0"},
		{name: "page size above maximum", path: "/orders?pageSize=201"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			readModel := &listReadModelStub{}
			handler := &orderHandler{app: &orderapp.Application{
				Options:   &orderapp.Options{DefaultPageSize: 20},
				ReadModel: readModel,
			}}
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			handler.list(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}
			if readModel.called {
				t.Fatal("read model should not be called for invalid query parameters")
			}
		})
	}
}

func TestListUsesDefaultsOnlyWhenQueryParametersAreMissing(t *testing.T) {
	readModel := &listReadModelStub{}
	handler := &orderHandler{app: &orderapp.Application{
		Options:   &orderapp.Options{DefaultPageSize: 20},
		ReadModel: readModel,
	}}
	req := httptest.NewRequest(http.MethodGet, "/orders", nil)
	rec := httptest.NewRecorder()

	handler.list(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !readModel.called {
		t.Fatal("read model should be called for valid default pagination")
	}
	if readModel.page != 1 || readModel.size != 20 {
		t.Fatalf("page = %d size = %d, want page=1 size=20", readModel.page, readModel.size)
	}
}
