package executionbroker

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/executionauth"
	"automation-hub-backend/internal/operations"
	"github.com/google/uuid"
)

// Models a passed absolute deadline before delivery of its cancellation timer.
type executionDeadlineContext struct {
	context.Context
	expired atomic.Bool
}

func (c *executionDeadlineContext) Deadline() (time.Time, bool) {
	if c.expired.Load() {
		return time.Now().Add(-time.Hour), true
	}
	return time.Now().Add(time.Hour), true
}

type executionContextService struct {
	ExecutionAuthorizationService
	issueCalls, getCalls, consumeCalls int
	afterIssue, afterGet, afterConsume func()
	beforeIssue                        func(context.Context) error
}

func (s *executionContextService) Authorize(ctx context.Context, request executionauth.Request) (executionauth.Receipt, error) {
	s.issueCalls++
	if s.beforeIssue != nil {
		if err := s.beforeIssue(ctx); err != nil {
			return executionauth.Receipt{}, err
		}
	}
	receipt, err := s.ExecutionAuthorizationService.Authorize(ctx, request)
	if s.afterIssue != nil {
		s.afterIssue()
	}
	return receipt, err
}

func (s *executionContextService) Get(ctx context.Context, owner string, id uuid.UUID) (executionauth.Receipt, error) {
	s.getCalls++
	receipt, err := s.ExecutionAuthorizationService.Get(ctx, owner, id)
	if s.afterGet != nil {
		s.afterGet()
	}
	return receipt, err
}

func (s *executionContextService) AuthorizeAndConsume(ctx context.Context, request executionauth.Request, consumer, target string) (executionauth.Receipt, error) {
	s.consumeCalls++
	receipt, err := s.ExecutionAuthorizationService.AuthorizeAndConsume(ctx, request, consumer, target)
	if s.afterConsume != nil {
		s.afterConsume()
	}
	return receipt, err
}

func TestExecutionEntryPointsRejectExpiredOrAbsentAuthority(t *testing.T) {
	service, op, claim := runningOperationScopeFixture(t)
	var retained context.Context
	if err := service.WithClaimedSafeEffect(context.Background(), claim, op, func(ctx context.Context) error {
		retained = context.WithoutCancel(ctx)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	expired := &executionDeadlineContext{Context: context.Background()}
	expired.expired.Store(true)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, test := range []struct {
		name string
		ctx  context.Context
		want error
	}{
		{"nil", nil, operations.ErrInvalidSourceObservation},
		{"expired_without_timer", expired, context.DeadlineExceeded},
		{"canceled", canceled, context.Canceled},
		{"ended_detached_effect", retained, operations.ErrInvalidSafeEffectAuthority},
	} {
		t.Run(test.name, func(t *testing.T) {
			harness := newBridgeHarness(t, "robert")
			spy := &executionContextService{ExecutionAuthorizationService: harness.service}
			bridge, err := NewDurableAuthorizationBridge(spy, "robert", "local")
			if err != nil {
				t.Fatal(err)
			}
			workspace := filepath.Join(t.TempDir(), "not-created")
			input := authorizedInput(t, workspace, "ordinary.txt", "ordinary marker")
			verifier := newTestAuthorizationVerifier()
			worker := newProductionLocalSafeWorker(workspace, bridge, verifier)
			broker := &Broker{safeWorker: worker, issuer: bridge}
			for name, call := range map[string]func() error{
				"issue":   func() error { _, err := bridge.Issue(test.ctx, workspace, input); return err },
				"consume": func() error { _, err := bridge.VerifyAndConsume(test.ctx, AuthorizationVerification{}); return err },
				"worker_run": func() error {
					out, err := worker.Run(test.ctx, input)
					if out.Progress != (SafeWorkerProgress{}) {
						t.Error("refusal claimed execution progress")
					}
					return err
				},
				"worker_execute": func() error { _, err := worker.Execute(test.ctx, nil); return err },
				"broker":         func() error { _, err := broker.ExecuteLocalSafeWorker(test.ctx, input); return err },
				"legacy_scope":   func() error { return validateActiveOperationScope(test.ctx, nil) },
			} {
				t.Run(name, func(t *testing.T) {
					if err := call(); !errors.Is(err, test.want) {
						t.Fatalf("got %v, want %v", err, test.want)
					}
				})
			}
			if spy.issueCalls != 0 || spy.getCalls != 0 || spy.consumeCalls != 0 || verifier.callCount.Load() != 0 {
				t.Fatalf("invalid context reached authorization: %+v; verifier=%d", spy, verifier.callCount.Load())
			}
			assertPathAbsent(t, workspace)
		})
	}
}

func TestBridgeRechecksAuthorityAfterPersistence(t *testing.T) {
	for _, stage := range []string{"issue", "get", "consume"} {
		t.Run(stage, func(t *testing.T) {
			harness := newBridgeHarness(t, "robert")
			spy := &executionContextService{ExecutionAuthorizationService: harness.service}
			bridge, err := NewDurableAuthorizationBridge(spy, "robert", "local")
			if err != nil {
				t.Fatal(err)
			}
			workspace := filepath.Join(t.TempDir(), "not-created")
			input := SafeWorkerInput{ArtifactName: "ordinary.txt", Marker: "ordinary marker"}
			ctx := &executionDeadlineContext{Context: context.Background()}
			expire := func() { ctx.expired.Store(true) }
			if stage == "issue" {
				spy.afterIssue = expire
			}
			prepared, err := bridge.Issue(ctx, workspace, input)
			if stage == "issue" {
				if !errors.Is(err, context.DeadlineExceeded) || spy.issueCalls != 1 || prepared.Authorization.ReceiptID != "" {
					t.Fatalf("expired issuance returned usable authority: %+v / %v / calls=%d", prepared, err, spy.issueCalls)
				}
				assertPathAbsent(t, workspace)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if stage == "get" {
				spy.afterGet = expire
			} else {
				spy.afterConsume = expire
			}
			worker := NewAuthorizedLocalSafeWorker(workspace, bridge)
			out, err := worker.Run(ctx, prepared)
			wantConsume := 0
			if stage == "consume" {
				wantConsume = 1
			}
			if !errors.Is(err, context.DeadlineExceeded) || spy.getCalls != 1 || spy.consumeCalls != wantConsume ||
				out.Progress.Authorization != "unknown" || out.Progress.EffectStarted {
				t.Fatalf("late deadline ignored or outcome misreported: %+v / %v / get=%d consume=%d", out, err, spy.getCalls, spy.consumeCalls)
			}
			// A committed consume is retained as evidence, never inferred unused.
			if stage == "consume" {
				id := uuid.MustParse(prepared.Authorization.ReceiptID)
				consumption, err := harness.repository.GetConsumption(context.Background(), "robert", id)
				if err != nil || consumption.ConsumedAt.IsZero() {
					t.Fatalf("consumption evidence lost: %+v / %v", consumption, err)
				}
			}
			assertPathAbsent(t, workspace)
		})
	}
}

func TestWorkerRejectsLateDeadlineBeforeFilesystemEffect(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "not-created")
	ctx := &executionDeadlineContext{Context: context.Background()}
	verifier := newTestAuthorizationVerifier()
	verifier.beforeConsume = func(AuthorizationVerification) error { ctx.expired.Store(true); return nil }
	input := authorizedInput(t, workspace, "ordinary.txt", "ordinary marker")
	out, err := NewAuthorizedLocalSafeWorker(workspace, verifier).Run(ctx, input)
	if !errors.Is(err, context.DeadlineExceeded) || verifier.callCount.Load() != 1 || out.Progress.Authorization != "consumed" || out.Progress.EffectStarted {
		t.Fatalf("late deadline allowed effect: %+v / %v / verifier=%d", out, err, verifier.callCount.Load())
	}
	assertPathAbsent(t, workspace)
}

type executionDeadlineWriter struct {
	ctx           *executionDeadlineContext
	writes, syncs int
}

func (w *executionDeadlineWriter) Write(p []byte) (int, error) {
	w.writes++
	w.ctx.expired.Store(true)
	return len(p), nil
}
func (w *executionDeadlineWriter) Sync() error { w.syncs++; return nil }

func TestArtifactWriteRetainsPartialEvidenceWhenDeadlinePasses(t *testing.T) {
	ctx := &executionDeadlineContext{Context: context.Background()}
	writer := &executionDeadlineWriter{ctx: ctx}
	var out SafeWorkerOutput
	err := writeSafeArtifact(ctx, writer, "marker", &out)
	if !errors.Is(err, context.DeadlineExceeded) || writer.writes != 1 || writer.syncs != 0 ||
		out.Progress.BytesWritten != len("marker") || !out.Progress.WriteComplete || out.Progress.SyncComplete {
		t.Fatalf("late write deadline lost evidence or continued: %+v / %v / %+v", out, err, writer)
	}
}

func TestBridgeBindsDetachedPrivateContextToOriginalCancellation(t *testing.T) {
	for _, kind := range []string{"source_observation", "safe_effect"} {
		t.Run(kind, func(t *testing.T) {
			service, op, claim := runningOperationScopeFixture(t)
			harness := newBridgeHarness(t, "robert")
			original, cancel := context.WithCancel(context.Background())
			defer cancel()
			spy := &executionContextService{ExecutionAuthorizationService: harness.service}
			spy.beforeIssue = func(ctx context.Context) error {
				cancel()
				if ctx.Done() == nil {
					return errors.New("persistence inherited detached cancellation")
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Second):
					return errors.New("original cancellation did not reach persistence")
				}
			}
			bridge, err := NewDurableAuthorizationBridge(spy, "robert", "local")
			if err != nil {
				t.Fatal(err)
			}
			workspace := filepath.Join(t.TempDir(), "not-created")
			var detached context.Context
			read := func(ctx context.Context) error {
				detached = context.WithoutCancel(ctx)
				input := SafeWorkerInput{ArtifactName: "ordinary.txt", Marker: "ordinary marker"}
				if kind == "safe_effect" {
					input = scopedTestInput(op)
				}
				_, err := bridge.Issue(detached, workspace, input)
				return err
			}
			if kind == "safe_effect" {
				err = service.WithClaimedSafeEffect(original, claim, op, read)
			} else {
				err = service.WithSourceObservation(original, operations.SourceObservationStart{
					OwnerUserID: op.OwnerUserID, WorkspaceID: op.WorkspaceID, OriginID: uuid.New(), ConfigDigest: strings.Repeat("a", 64),
				}, read)
			}
			if !errors.Is(err, context.Canceled) || spy.issueCalls != 1 || spy.getCalls != 0 || spy.consumeCalls != 0 || detached == nil || detached.Err() != nil {
				t.Fatalf("detached private authority was not canceled before persistence: %v / %+v", err, spy)
			}
			assertPathAbsent(t, workspace)
		})
	}
}

func TestExecutionBindingPreservesNestedObservationAndEffectScopes(t *testing.T) {
	service, op, claim := runningOperationScopeFixture(t)
	harness := newBridgeHarness(t, "robert")
	workspace := filepath.Join(t.TempDir(), "workspace")
	broker, err := NewAuthorizedBroker(workspace, "robert", "local", harness.service)
	if err != nil {
		t.Fatal(err)
	}
	start := operations.SourceObservationStart{
		OwnerUserID: op.OwnerUserID, WorkspaceID: op.WorkspaceID, OriginID: uuid.New(), ConfigDigest: strings.Repeat("a", 64),
	}
	var result ExecutionResult
	err = service.WithSourceObservation(context.Background(), start, func(observationCtx context.Context) error {
		observation, ok := operations.CurrentSourceObservation(observationCtx)
		if !ok {
			return errors.New("source observation missing before nested effect")
		}
		return service.WithClaimedSafeEffect(observationCtx, claim, op, func(effectCtx context.Context) error {
			bound, finish, err := operations.BindExecutionContext(context.WithoutCancel(effectCtx))
			if err != nil {
				return err
			}
			defer finish()
			if got, ok := operations.CurrentSourceObservation(bound); !ok || got != observation {
				return errors.New("binding stripped original source observation")
			}
			if got, ok := operations.CurrentSafeEffectScope(bound); !ok || got.OperationID != op.ID || got.ClaimGeneration != claim.Generation {
				return errors.New("binding stripped exact running operation scope")
			}
			result, err = broker.ExecuteLocalSafeWorker(bound, scopedTestInput(op))
			return err
		})
	})
	if err != nil || !result.OK || !result.Verification.Passed || !result.Output.Progress.FileClosed {
		t.Fatalf("nested healthy authorization/effect/verification chain failed: %+v / %v", result, err)
	}
}
