package lifecycle

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func waitSignal(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("owned work did not reach its synchronization point")
	}
}

func drainGroup(t *testing.T, g *Group) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := g.StopAndWait(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestGroupJoinsDetachedChildrenAndRejectsAdmissionAfterStop(t *testing.T) {
	g := New(context.Background())
	release, entered := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer func() { once.Do(func() { close(release) }); drainGroup(t, g) }()
	if !Go(g.Context(), "parent", func() {
		if !Go(context.WithoutCancel(g.Context()), "detached-child", func() {
			close(entered)
			<-release
		}) {
			t.Error("child admission failed before stop")
		}
	}) {
		t.Fatal("parent admission failed")
	}
	waitSignal(t, entered)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	err := g.StopAndWait(ctx)
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "detached-child") {
		t.Fatalf("drain claimed completion while child was active: %v", err)
	}
	if Go(context.WithoutCancel(g.Context()), "late", func() { t.Error("late work ran") }) {
		t.Fatal("stopped group admitted work through a detached context")
	}
	if _, admitted := Enter(g.WithContext(context.Background()), "late-request"); admitted {
		t.Fatal("stopped group admitted a synchronous request")
	}
	once.Do(func() { close(release) })
	drainGroup(t, g)
	g.Stop()
	drainGroup(t, g)
}

func TestSynchronousOwnershipPreservesCallerContextAndFinishIsIdempotent(t *testing.T) {
	g := New(nil)
	defer drainGroup(t, g)
	request, cancel := context.WithCancel(context.Background())
	defer cancel()
	attached := g.WithContext(request)
	finish, admitted := Enter(attached, "http-request")
	if !admitted {
		t.Fatal("request not admitted")
	}
	defer finish()
	g.Stop()
	if attached.Err() != nil || g.Context().Err() != context.Canceled {
		t.Fatal("ownership changed caller cancellation semantics")
	}
	finish()
	finish()
	drainGroup(t, g)
}

func TestGroupAdmissionAndStopSerializeConcurrentCallers(t *testing.T) {
	g := New(context.Background())
	var callers sync.WaitGroup
	var count atomic.Int32
	start := make(chan struct{})
	for i := 0; i < 16; i++ {
		callers.Add(1)
		go func() {
			defer callers.Done()
			<-start
			Go(g.Context(), "concurrent", func() { count.Add(1) })
		}()
	}
	close(start)
	g.Stop()
	callers.Wait()
	drainGroup(t, g)
	before := count.Load()
	if Go(g.WithContext(context.Background()), "late", func() { count.Add(1) }) || count.Load() != before {
		t.Fatal("work admitted after join")
	}
}

func TestStandaloneCompatibilityAndCanceledAdmission(t *testing.T) {
	done := make(chan struct{})
	if !Go(nil, "standalone", func() { close(done) }) {
		t.Fatal("standalone work refused")
	}
	waitSignal(t, done)
	if Go(context.Background(), "nil-function", nil) {
		t.Fatal("nil function admitted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, ok := Enter(ctx, "canceled"); ok {
		t.Fatal("canceled synchronous work admitted")
	}
	if Go(ctx, "canceled", func() { t.Error("canceled work ran") }) {
		t.Fatal("canceled work admitted")
	}
	g := New(ctx)
	if _, ok := g.Enter("canceled-parent"); ok {
		t.Fatal("group admitted work after parent cancellation")
	}
	if g.StopAndWait(nil) == nil {
		t.Fatal("nil drain context accepted")
	}
	drainGroup(t, g)
}

func TestCompatibilityOwnershipPreservesCancellationAndExistingGroup(t *testing.T) {
	owner, callerOwner := New(context.Background()), New(context.Background())
	defer drainGroup(t, owner)
	defer drainGroup(t, callerOwner)
	request, cancel := context.WithCancel(context.Background())
	defer cancel()
	attached := WithOwnership(request, owner.Context())
	finish, ok := Enter(attached, "compatibility-call")
	if !ok {
		t.Fatal("compatibility ownership not attached")
	}
	finish()
	cancel()
	if attached.Err() != context.Canceled {
		t.Fatal("caller cancellation lost")
	}
	owner.Stop()
	existing := WithOwnership(callerOwner.Context(), owner.Context())
	finish, ok = Enter(existing, "existing-caller")
	if !ok {
		t.Fatal("service owner overrode the existing caller group")
	}
	finish()
	if _, ok := Enter(WithOwnership(context.Background(), owner.Context()), "late"); ok {
		t.Fatal("legacy call admitted after stop")
	}
	if ctx := WithOwnership(nil, nil); ctx == nil || ctx.Err() != nil {
		t.Fatal("standalone fallback invalid")
	}
}
