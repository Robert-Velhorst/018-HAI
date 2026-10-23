package infra

import (
	"context"
	"errors"
	"sync"
	"testing"

	"gorm.io/gorm"
)

type cancelAfterDatabaseContextCheck struct {
	context.Context
	cancel context.CancelFunc
	once   sync.Once
}

func (c *cancelAfterDatabaseContextCheck) Err() error {
	err := c.Context.Err()
	c.once.Do(c.cancel)
	return err
}

func TestSharedPoolAdmissionRechecksCancellationBeforeCacheOrStartup(t *testing.T) {
	for _, cached := range []bool{false, true} {
		name := "cold startup"
		if cached {
			name = "cached pool"
		}
		t.Run(name, func(t *testing.T) {
			restoreSharedPoolHooks(t)
			if cached {
				db, _ := sharedPoolFixture(t)
				defaultDBMu.Lock()
				defaultDB = db
				defaultDBMu.Unlock()
			}
			opens := 0
			openConfiguredDB = func(context.Context) (*gorm.DB, error) {
				opens++
				return nil, nil
			}
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			// Simulate cancellation immediately after the initial clear snapshot.
			ctx := &cancelAfterDatabaseContextCheck{Context: parent, cancel: cancel}
			db, err := GetDefaultDBContext(ctx)
			if db != nil || !errors.Is(err, context.Canceled) || opens != 0 {
				t.Fatalf("cancelled admission returned pool=%v err=%v opens=%d", db != nil, err, opens)
			}
			if defaultDBStarting != nil || defaultDBStartCancel != nil {
				t.Fatal("cancelled admission created startup state")
			}
		})
	}
}
