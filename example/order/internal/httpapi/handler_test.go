package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type decodeJSONTestRequest struct {
	Name string `json:"name"`
}

func TestDecodeJSON(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{name: "valid JSON", body: `{"name":"order"}`},
		{name: "empty body", body: "", wantErr: true},
		{name: "malformed JSON", body: `{"name":`, wantErr: true},
		{name: "unknown field", body: `{"name":"order","extra":1}`, wantErr: true},
		{name: "trailing object", body: `{"name":"order"}{}`, wantErr: true},
		{name: "trailing token", body: `{"name":"order"}xxx`, wantErr: true},
		{name: "oversized body", body: `{"name":"` + strings.Repeat("x", maxRequestBodyBytes) + `"}`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			var dst decodeJSONTestRequest

			err := decodeJSON(rec, req, &dst)
			if (err != nil) != tt.wantErr {
				t.Fatalf("decodeJSON() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && dst.Name != "order" {
				t.Fatalf("decoded name = %q, want order", dst.Name)
			}
		})
	}
}
