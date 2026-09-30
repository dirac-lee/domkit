package acl

import (
	"errors"
	"fmt"
	"testing"

	"github.com/dirac-lee/domkit/domain"
)

func TestIsRetryableErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"domain rule error", domain.NewDomainRuleError([]domain.BrokenRule{{Code: "X"}}), false},
		{"wrapped domain rule error", fmt.Errorf("wrap: %w", domain.NewDomainRuleError(nil)), false},
		{"unknown error", errors.New("something else"), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsRetryableErr(c.err); got != c.want {
				t.Fatalf("IsRetryableErr(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}
