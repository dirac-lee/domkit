package mysql

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/dirac-lee/domkit/infra/outbox"
)

func TestOutboxStoreFlashClaimKeepsEligibilityGuard(t *testing.T) {
	db, mock, cleanup := newMockGormDB(t)
	defer cleanup()

	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	store := NewOutboxStore(db)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `outbox_messages` SET `claim_token`=?,`claimed_at`=?,`status`=? WHERE id IN (?,?) AND ((status = ? AND created_at < ?) OR (status = ? AND claimed_at < ?))")).
		WithArgs(
			"relay-1",
			now,
			outbox.StatusProcessing,
			"msg-1",
			"msg-2",
			outbox.StatusPending,
			now.Add(-time.Second),
			outbox.StatusProcessing,
			now.Add(-30*time.Second),
		).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	err := store.flashClaim(context.Background(),
		[]string{"msg-1", "msg-2"}, "relay-1", now, time.Second, 30*time.Second)
	if err != nil {
		t.Fatalf("flashClaim() unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
