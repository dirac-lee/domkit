package mysql

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/dirac-lee/domkit/application"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func newMockGormDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, func()) {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error: %v", err)
	}
	gdb, err := gorm.Open(gormmysql.New(gormmysql.Config{
		Conn:                      db,
		SkipInitializeWithVersion: true,
	}), &gorm.Config{
		DisableAutomaticPing: true,
	})
	if err != nil {
		db.Close()
		t.Fatalf("gorm.Open() error: %v", err)
	}
	cleanup := func() {
		_ = db.Close()
	}
	return gdb, mock, cleanup
}

func TestGormTxManagerRunsAfterCommitHooksAfterSuccessfulCommit(t *testing.T) {
	db, mock, cleanup := newMockGormDB(t)
	defer cleanup()
	mock.ExpectBegin()
	mock.ExpectCommit()

	tm := NewGormTxManager(db)
	var events []string

	err := tm.DoInTx(context.Background(), application.PropagationRequired, func(ctx context.Context, tx any) error {
		if tx == nil {
			t.Fatal("transaction handle should be passed to callback")
		}
		if txFromContext(ctx) == nil {
			t.Fatal("transaction should be available from context")
		}
		OnAfterCommit(ctx, func() error {
			events = append(events, "hook")
			return nil
		})
		events = append(events, "fn")
		return nil
	})
	if err != nil {
		t.Fatalf("DoInTx() unexpected error: %v", err)
	}
	if !reflect.DeepEqual(events, []string{"fn", "hook"}) {
		t.Fatalf("events = %v, want [fn hook]", events)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestGormTxManagerSwallowsAfterCommitHookError(t *testing.T) {
	db, mock, cleanup := newMockGormDB(t)
	defer cleanup()
	mock.ExpectBegin()
	mock.ExpectCommit()

	tm := NewGormTxManager(db)
	err := tm.DoInTx(context.Background(), application.PropagationRequired, func(ctx context.Context, tx any) error {
		OnAfterCommit(ctx, func() error {
			return errors.New("publish failed")
		})
		return nil
	})
	if err != nil {
		t.Fatalf("DoInTx() should ignore after-commit hook error, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestGormTxManagerSkipsAfterCommitHooksOnRollback(t *testing.T) {
	db, mock, cleanup := newMockGormDB(t)
	defer cleanup()
	mock.ExpectBegin()
	mock.ExpectRollback()

	tm := NewGormTxManager(db)
	wantErr := errors.New("business failed")
	hookCalled := false

	err := tm.DoInTx(context.Background(), application.PropagationRequired, func(ctx context.Context, tx any) error {
		OnAfterCommit(ctx, func() error {
			hookCalled = true
			return nil
		})
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("DoInTx() error = %v, want %v", err, wantErr)
	}
	if hookCalled {
		t.Fatal("after-commit hook should not run when transaction rolls back")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestTxHooksRunAfterCommitSwallowsHookErrors(t *testing.T) {
	hooks := newTxHooks()
	called := false
	hooks.add(func() error {
		called = true
		return errors.New("publish failed")
	})

	hooks.runAfterCommit()

	if !called {
		t.Fatal("after-commit hook should be executed")
	}
}
