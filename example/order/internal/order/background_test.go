package order

import (
	"testing"
	"time"
)

func TestPostCommitContextHasDeadline(t *testing.T) {
	ctx, cancel := postCommitContext()
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("post-commit context should have deadline")
	}
	if time.Until(deadline) <= 0 {
		t.Fatal("post-commit context deadline should be in the future")
	}
}
