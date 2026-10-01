package order

import "testing"

func TestOptionsValidate(t *testing.T) {
	tests := []struct {
		name    string
		options Options
		wantErr bool
	}{
		{
			name:    "valid options",
			options: Options{DefaultPageSize: 20, SummaryCacheTTLSec: 30, PayIdempotencyTTLSec: 300},
		},
		{
			name:    "default page size too small",
			options: Options{DefaultPageSize: 0, SummaryCacheTTLSec: 30, PayIdempotencyTTLSec: 300},
			wantErr: true,
		},
		{
			name:    "default page size too large",
			options: Options{DefaultPageSize: 201, SummaryCacheTTLSec: 30, PayIdempotencyTTLSec: 300},
			wantErr: true,
		},
		{
			name:    "summary cache ttl not positive",
			options: Options{DefaultPageSize: 20, SummaryCacheTTLSec: 0, PayIdempotencyTTLSec: 300},
			wantErr: true,
		},
		{
			name:    "pay idempotency ttl not positive",
			options: Options{DefaultPageSize: 20, SummaryCacheTTLSec: 30, PayIdempotencyTTLSec: -1},
			wantErr: true,
		},
		{
			name: "relay interval negative",
			options: Options{
				DefaultPageSize: 20, SummaryCacheTTLSec: 30, PayIdempotencyTTLSec: 300,
				RelayIntervalSec: -1,
			},
			wantErr: true,
		},
		{
			name: "relay batch size negative",
			options: Options{
				DefaultPageSize: 20, SummaryCacheTTLSec: 30, PayIdempotencyTTLSec: 300,
				RelayBatchSize: -1,
			},
			wantErr: true,
		},
		{
			name: "relay grace negative",
			options: Options{
				DefaultPageSize: 20, SummaryCacheTTLSec: 30, PayIdempotencyTTLSec: 300,
				RelayGraceSec: -1,
			},
			wantErr: true,
		},
		{
			name: "relay lease negative",
			options: Options{
				DefaultPageSize: 20, SummaryCacheTTLSec: 30, PayIdempotencyTTLSec: 300,
				RelayLeaseSec: -1,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.options.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
