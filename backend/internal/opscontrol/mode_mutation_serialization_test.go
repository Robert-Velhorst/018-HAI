package opscontrol

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sync"
	"testing"
	"time"

	"automation-hub-backend/internal/autonomypolicy"
	"automation-hub-backend/internal/executionauth"
	"automation-hub-backend/internal/safety"
)

func TestModeMutationsWaitForExecutionCommitFence(t *testing.T) {
	for _, name := range []string{"seed", "setter", "compare-and-swap", "service restriction", "service persistence repair"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			service := NewService(dir, nil, nil, "owner", "local")
			controller := service.Control()
			if name != "seed" {
				if err := controller.seedModeIfAbsent(autonomypolicy.ModeAutonomousSafe); err != nil {
					t.Fatal(err)
				}
			}
			want := autonomypolicy.ModeReadOnly
			if name == "service persistence repair" {
				controller.mu.Lock()
				controller.modeErr = errors.New("untrusted persisted mode")
				controller.mu.Unlock()
				want = autonomypolicy.ModePaused
			}
			initial, _ := controller.ModePersistenceStatus()
			assertModeMutationWaitsForFence(t, controller, initial, func() error {
				switch name {
				case "seed":
					return controller.seedModeIfAbsent(want)
				case "setter":
					_, err := controller.SetMode(want)
					return err
				case "compare-and-swap":
					_, err := controller.SetModeIfCurrent(initial, want)
					return err
				default:
					_, err := service.SetMode(t.Context(), string(want), ControlAuthorization{})
					return err
				}
			})
			if mode, err := controller.ModePersistenceStatus(); err != nil || mode != want {
				t.Fatalf("mutation after fence: mode=%s error=%v", mode, err)
			}
			if got := NewController(dir).StoredMode(); got != want {
				t.Fatalf("persisted mode=%s, want %s", got, want)
			}
		})
	}
}

func assertModeMutationWaitsForFence(t *testing.T, controller *Controller, initial autonomypolicy.Mode, mutate func() error) {
	t.Helper()
	releaseCommit := safety.AcquireExecutionCommitFence()
	var once sync.Once
	release := func() { once.Do(releaseCommit) }
	started, finished := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	t.Cleanup(func() {
		release()
		select {
		case <-finished:
		case <-time.After(2 * time.Second):
			t.Error("mode mutation did not finish after releasing the fence")
		}
	})
	go func() {
		defer close(finished)
		close(started)
		done <- mutate()
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("mode mutation did not start")
	}
	select {
	case err := <-done:
		t.Fatalf("mode mutation crossed the execution fence: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	// A fenced writer must not hold Controller.mu while waiting for the fence.
	if !controller.mu.TryLock() {
		t.Fatal("fenced writer blocked the controller reader")
	}
	mode := controller.mode
	if controller.modeErr != nil {
		mode = autonomypolicy.ModePaused
	}
	controller.mu.Unlock()
	if mode != initial {
		t.Fatalf("mode changed under execution fence: %s -> %s", initial, mode)
	}
	release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("mutation after fence release: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("mutation remained blocked after fence release")
	}
}

func TestRestrictiveModeCASPreservesNewerServicePause(t *testing.T) {
	dir := t.TempDir()
	service := NewService(dir, nil, nil, "owner", "local")
	controller := service.Control()
	stale, err := controller.ModePersistenceStatus()
	if err != nil || stale != autonomypolicy.ModeAutonomousSafe {
		t.Fatalf("initial snapshot: %s %v", stale, err)
	}
	if _, err := service.SetMode(t.Context(), string(autonomypolicy.ModePaused), ControlAuthorization{}); err != nil {
		t.Fatalf("newer restrictive service request: %v", err)
	}
	mode, err := controller.SetModeIfCurrent(stale, autonomypolicy.ModeDraftOnly)
	if !errors.Is(err, ErrAutonomyModeStateChanged) || mode != autonomypolicy.ModePaused {
		t.Fatalf("stale restrictive request: mode=%s error=%v", mode, err)
	}
	if got := NewController(dir).StoredMode(); got != autonomypolicy.ModePaused {
		t.Fatalf("stale restriction replaced persisted pause: %s", got)
	}
}

func TestModePersistenceRepairCASPreservesNewerMode(t *testing.T) {
	dir := t.TempDir()
	controller := NewController(dir)
	controller.mu.Lock()
	controller.modeErr = errors.New("untrusted persisted mode")
	controller.mu.Unlock()
	stale, err := controller.ModePersistenceStatus()
	if err == nil || stale != autonomypolicy.ModePaused {
		t.Fatalf("repair snapshot: %s %v", stale, err)
	}
	if _, err := controller.SetMode(autonomypolicy.ModeReadOnly); err != nil {
		t.Fatal(err)
	}
	mode, err := controller.SetModeIfCurrent(stale, stale)
	if !errors.Is(err, ErrAutonomyModeStateChanged) || mode != autonomypolicy.ModeReadOnly || NewController(dir).StoredMode() != mode {
		t.Fatalf("stale repair overwrote newer mode: %s %v", mode, err)
	}
}

func TestControllerModeWriteFailureBlocksCachedAutonomy(t *testing.T) {
	for _, conditional := range []bool{false, true} {
		t.Run(fmt.Sprintf("conditional_%t", conditional), func(t *testing.T) {
			dir := t.TempDir()
			controller := NewController(dir)
			// Only this newly owned test directory is changed. Simulate a path
			// which cannot accept the mode write, without touching installed state.
			if err := os.Mkdir(controller.modePath, 0o700); err != nil {
				t.Fatal(err)
			}
			var mode autonomypolicy.Mode
			var err error
			if conditional {
				mode, err = controller.SetModeIfCurrent(autonomypolicy.ModeAutonomousSafe, autonomypolicy.ModeReadOnly)
			} else {
				mode, err = controller.SetMode(autonomypolicy.ModeReadOnly)
			}
			if err == nil || mode != autonomypolicy.ModePaused || controller.Mode() != autonomypolicy.ModePaused || controller.StoredMode() != autonomypolicy.ModePaused {
				t.Fatalf("failed mode write retained permission: %s / %v", mode, err)
			}
			if _, err := controller.ModePersistenceStatus(); err == nil {
				t.Fatal("failed mode write was reported healthy")
			}
			controller.mu.Lock()
			cached := controller.mode
			controller.mu.Unlock()
			if cached != autonomypolicy.ModeAutonomousSafe {
				t.Fatal("failed write fabricated a successfully stored restriction")
			}
		})
	}
}

func TestModeServiceAuthorizerCanApplyConcurrentRestriction(t *testing.T) {
	service := newTestService(t)
	if _, err := service.SetMode(t.Context(), string(autonomypolicy.ModeReadOnly), ControlAuthorization{}); err != nil {
		t.Fatal(err)
	}
	auth := controlAuthorizationFor(t, service, "operator", escalateAutonomyAction, autonomyModeResourceType,
		autonomyModeResourceID(string(autonomypolicy.ModeReadOnly), string(autonomypolicy.ModeAutonomousSafe)), string(autonomypolicy.ModeAutonomousSafe))
	service.WithExecutionAuthorizer(controlAuthorizerFunc(func(ctx context.Context, request executionauth.Request, _, _ string) (executionauth.Receipt, error) {
		if _, err := service.SetMode(ctx, string(autonomypolicy.ModePaused), ControlAuthorization{}); err != nil {
			return executionauth.Receipt{}, fmt.Errorf("concurrent service restriction: %w", err)
		}
		return exactControlReceipt(request, service.now()), nil
	}))
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := service.SetMode(ctx, string(autonomypolicy.ModeAutonomousSafe), auth)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrAutonomyModeStateChanged) {
			t.Fatalf("stale authorized escalation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("service held a mutation lock across its authorizer")
	}
	if mode := service.Control().StoredMode(); mode != autonomypolicy.ModePaused {
		t.Fatalf("concurrent service pause overwritten: %s", mode)
	}
}

// This source contract ties the stale-snapshot regressions to all service paths;
// the restrictive path has no existing hook between its read and mutation.
func TestModeServiceMutationPathsUseCompareAndSwap(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "service.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var setters int
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || fn.Name.Name != "SetMode" {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			method, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || method.Sel.Name != "SetMode" && method.Sel.Name != "SetModeIfCurrent" {
				return true
			}
			if method.Sel.Name == "SetMode" {
				t.Fatal("service mode path uses an unconditional setter")
			}
			if len(call.Args) != 2 {
				t.Fatal("mode CAS must carry the original snapshot and target")
			}
			expected, expectedOK := call.Args[0].(*ast.Ident)
			target, targetOK := call.Args[1].(*ast.Ident)
			if !expectedOK || !targetOK || expected.Name != "current" || target.Name != "target" {
				t.Fatal("service mode CAS does not preserve its decision snapshot")
			}
			setters++
			return true
		})
	}
	if setters != 3 {
		t.Fatalf("CAS paths=%d, want repair, escalation and restriction", setters)
	}
}
