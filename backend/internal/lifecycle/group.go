// Package lifecycle owns API background work through cancellation and join.
package lifecycle

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type contextKey struct{}

type Group struct {
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	stopped bool
	nextID  uint64
	active  map[uint64]string
	wg      sync.WaitGroup
	done    chan struct{}
}

func New(parent context.Context) *Group {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	g := &Group{cancel: cancel, active: make(map[uint64]string), done: make(chan struct{})}
	g.ctx = g.WithContext(ctx)
	return g
}

func (g *Group) Context() context.Context { return g.ctx }

// WithContext attaches ownership without changing request cancellation semantics.
func (g *Group) WithContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, contextKey{}, g)
}

// Enter registers synchronous work. Stop serializes admission with WaitGroup.Wait.
func (g *Group) Enter(name string) (func(), bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stopped || g.ctx.Err() != nil {
		return nil, false
	}
	g.nextID++
	id := g.nextID
	g.active[id] = name
	g.wg.Add(1)
	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			delete(g.active, id)
			g.mu.Unlock()
			g.wg.Done()
		})
	}, true
}

// WithOwnership preserves caller values and cancellation, attaching only ownership.
// Existing caller ownership takes precedence over a service's compatibility owner.
func WithOwnership(ctx, owner context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Value(contextKey{}).(*Group); ok || owner == nil {
		return ctx
	}
	if g, ok := owner.Value(contextKey{}).(*Group); ok {
		return g.WithContext(ctx)
	}
	return ctx
}

// Enter keeps standalone callers compatible while registering API-owned work.
func Enter(ctx context.Context, name string) (func(), bool) {
	if ctx == nil || ctx.Err() != nil {
		return nil, false
	}
	if g, ok := ctx.Value(contextKey{}).(*Group); ok {
		return g.Enter(name)
	}
	return func() {}, true
}

// Go registers ownership before launching, so Stop cannot miss a queued goroutine.
func Go(ctx context.Context, name string, run func()) bool {
	if run == nil {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	finish, admitted := Enter(ctx, name)
	if !admitted {
		return false
	}
	go func() { defer finish(); run() }()
	return true
}

// Stop prevents new admission, cancels work and starts one join, without blocking.
func (g *Group) Stop() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stopped {
		return
	}
	g.stopped = true
	g.cancel()
	go func() { g.wg.Wait(); close(g.done) }()
}

func (g *Group) StopAndWait(ctx context.Context) error {
	g.Stop()
	if ctx == nil {
		return fmt.Errorf("runtime drain requires a context")
	}
	select {
	case <-g.done:
		return nil
	default:
	}
	select {
	case <-g.done:
		return nil
	case <-ctx.Done():
		g.mu.Lock()
		names := make([]string, 0, len(g.active))
		for _, name := range g.active {
			names = append(names, name)
		}
		g.mu.Unlock()
		sort.Strings(names)
		return fmt.Errorf("runtime drain incomplete (%s): %w", strings.Join(names, ", "), ctx.Err())
	}
}
