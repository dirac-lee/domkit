package mysql

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/dirac-lee/domkit/infra/outbox"
	"gorm.io/gorm"
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

func TestOutboxStoreMarkSentRequiresProcessingAndClearsToken(t *testing.T) {
	db, mock, cleanup := newMockGormDB(t)
	defer cleanup()

	store := NewOutboxStore(db)
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `outbox_messages` SET `claim_token`=?,`sent_at`=?,`status`=? WHERE id = ? AND claim_token = ? AND status = ?")).
		WithArgs("", sqlmock.AnyArg(), outbox.StatusSent, "msg-1", "relay-1", outbox.StatusProcessing).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := store.MarkSent(context.Background(), "msg-1", "relay-1"); err != nil {
		t.Fatalf("MarkSent() unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestOutboxStoreReleaseRequiresProcessing(t *testing.T) {
	db, mock, cleanup := newMockGormDB(t)
	defer cleanup()

	store := NewOutboxStore(db)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT `retry_count` FROM `outbox_messages` WHERE id = ? AND claim_token = ? AND status = ? LIMIT ?")).
		WithArgs("msg-1", "relay-1", outbox.StatusProcessing, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	_, err := store.Release(context.Background(), "msg-1", "relay-1", "publish failed")
	if !errors.Is(err, outbox.ErrClaimLost) {
		t.Fatalf("Release() expected ErrClaimLost, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestOutboxStoreMoveToDeadLetterRequiresProcessing(t *testing.T) {
	db, mock, cleanup := newMockGormDB(t)
	defer cleanup()

	store := NewOutboxStore(db)
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `outbox_messages` SET `claim_token`=?,`last_error`=?,`status`=? WHERE id = ? AND claim_token = ? AND status = ?")).
		WithArgs("", "bad payload", outbox.StatusDeadLetter, "msg-1", "relay-1", outbox.StatusProcessing).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	err := store.MoveToDeadLetter(context.Background(), "msg-1", "relay-1", "bad payload")
	if !errors.Is(err, outbox.ErrClaimLost) {
		t.Fatalf("MoveToDeadLetter() expected ErrClaimLost, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
