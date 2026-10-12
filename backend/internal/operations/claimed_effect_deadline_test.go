package operations

import (
	"context"
	"testing"
	"time"
)

// Models timer delivery lag without sleeping or depending on scheduler load.
type delayedEffectDeadlineContext struct {
	context.Context
	deadline time.Time
}

func (c delayedEffectDeadlineContext) Deadline() (time.Time, bool) {
	return c.deadline, true
}

func TestCurrentSafeEffectScopeRequiresUnexpiredServerDeadline(t *testing.T) {
	for _, test := range []struct {
		name      string
		authority context.Context
		allowed   bool
	}{
		{"unbounded", context.Background(), false},
		{"expired_without_timer_delivery", delayedEffectDeadlineContext{context.Background(), time.Now().Add(-time.Hour)}, false},
		{"live", delayedEffectDeadlineContext{context.Background(), time.Now().Add(time.Hour)}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := &activeSafeEffect{authority: test.authority, scope: SafeEffectScope{Version: 42}}
			state.active.Store(true)
			ctx := context.WithValue(context.Background(), safeEffectContextKey{}, state)
			for _, candidate := range []context.Context{ctx, context.WithoutCancel(ctx)} {
				scope, ok := CurrentSafeEffectScope(candidate)
				if ok != test.allowed || (ok && scope.Version != 42) {
					t.Fatalf("authority=%v scope=%+v allowed=%t want=%t", test.authority, scope, ok, test.allowed)
				}
			}
		})
	}
}
