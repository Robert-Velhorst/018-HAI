package router

import (
	"errors"
	"testing"

	"automation-hub-idp/internal/app/authentication"
	"automation-hub-idp/internal/app/users"

	"gorm.io/gorm"
)

func TestOwnedDatabaseClosesAfterServerOrRouteReturn(t *testing.T) {
	operationError := errors.New("operation failed")
	closeError := errors.New("close failed")
	for _, tc := range []struct {
		name                 string
		runError, closeError error
	}{
		{name: "normal return"},
		{name: "route or listener failure", runError: operationError},
		{name: "cleanup failure", closeError: closeError},
		{name: "both failures", runError: operationError, closeError: closeError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			ran := false
			err := runWithOwnedDatabase(func() error {
				calls++
				if !ran {
					t.Error("database closed before run returned")
				}
				return tc.closeError
			}, func() error {
				ran = true
				return tc.runError
			})
			if calls != 1 || !ran {
				t.Fatalf("run=%v close calls=%d", ran, calls)
			}
			if (tc.runError == nil && tc.closeError == nil && err != nil) ||
				(tc.runError != nil && !errors.Is(err, tc.runError)) ||
				(tc.closeError != nil && !errors.Is(err, tc.closeError)) {
				t.Fatalf("errors not preserved: %v", err)
			}
		})
	}
}

func TestOwnedDatabaseClosesOnPanic(t *testing.T) {
	marker := &struct{}{}
	closed := 0
	func() {
		defer func() {
			if recover() != marker {
				t.Error("original panic not preserved")
			}
		}()
		_ = runWithOwnedDatabase(func() error { closed++; return nil }, func() error { panic(marker) })
	}()
	if closed != 1 {
		t.Fatalf("close calls = %d", closed)
	}
}

func TestBorrowedServiceDatabaseRejectsMissingHandle(t *testing.T) {
	for _, database := range []*gorm.DB{nil, {}} {
		if _, err := authentication.NewDefaultAuthServiceWithDatabase(database); err == nil {
			t.Error("authentication accepted missing database")
		}
		if _, err := users.NewDefaultUserServiceWithDatabase(database); err == nil {
			t.Error("users accepted missing database")
		}
	}
}
