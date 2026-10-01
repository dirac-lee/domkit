package mysql

import (
	"errors"
	"testing"
)

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
