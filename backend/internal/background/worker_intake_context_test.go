package background

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"automation-hub-backend/internal/accountfeed"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/operations"

	"github.com/google/uuid"
)

type workerIntakeContextRepository struct {
	*operations.MemoryRepository
	want        context.Context
	cancel      context.CancelFunc
	lookups     int
	found       bool
	creates     int
	updates     int
	legacyCalls int
	claimCalls  int
}

type workerIntakeCallerKey struct{}

func (r *workerIntakeContextRepository) FindByDedupeKeyContext(ctx context.Context, owner, workspace, key string) (*models.Operation, bool, error) {
	r.lookups++
	if ctx.Value(workerIntakeCallerKey{}) != r.want.Value(workerIntakeCallerKey{}) {
		return nil, false, errors.New("worker intake lookup lost caller context")
	}
	if _, ok := operations.CurrentSourceObservation(ctx); !ok {
		return nil, false, errors.New("worker intake lookup lost pre-read source observation")
	}
	op, found, err := r.MemoryRepository.FindByDedupeKeyContext(ctx, owner, workspace, key)
	r.found = found
	// Return a successful lookup after cancellation to exercise the intake fence.
	r.cancel()
	return op, found, err
}

func (r *workerIntakeContextRepository) CreateWithEventContext(ctx context.Context, op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	r.creates++
	return r.MemoryRepository.CreateWithEventContext(ctx, op, event)
}

func (r *workerIntakeContextRepository) UpdateWithEventContext(ctx context.Context, op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	r.updates++
	return r.MemoryRepository.UpdateWithEventContext(ctx, op, event)
}

func (r *workerIntakeContextRepository) FindByDedupeKey(owner, workspace, key string) (*models.Operation, bool, error) {
	r.legacyCalls++
	return r.MemoryRepository.FindByDedupeKey(owner, workspace, key)
}

func (r *workerIntakeContextRepository) CreateWithEvent(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	r.legacyCalls++
	return r.MemoryRepository.CreateWithEvent(op, event)
}

func (r *workerIntakeContextRepository) UpdateWithEvent(op *models.Operation, event *models.OperationEvent) (*models.Operation, error) {
	r.legacyCalls++
	return r.MemoryRepository.UpdateWithEvent(op, event)
}

func (r *workerIntakeContextRepository) Create(op *models.Operation) (*models.Operation, error) {
	r.legacyCalls++
	return r.MemoryRepository.Create(op)
}

func (r *workerIntakeContextRepository) Update(op *models.Operation) (*models.Operation, error) {
	r.legacyCalls++
	return r.MemoryRepository.Update(op)
}

func (r *workerIntakeContextRepository) AppendEvent(event *models.OperationEvent) error {
	r.legacyCalls++
	return r.MemoryRepository.AppendEvent(event)
}

func (r *workerIntakeContextRepository) ClaimNext(ctx context.Context, owner, workspace string, workerID uuid.UUID, lease time.Duration) (*operations.ClaimedOperation, error) {
	r.claimCalls++
	return r.MemoryRepository.ClaimNext(ctx, owner, workspace, workerID, lease)
}

type workerIntakeContextReader struct {
	feed     accountfeed.Feed
	items    []accountfeed.FeedItem
	reads    int
	ctx      context.Context
	observed bool
}

func (r *workerIntakeContextReader) Feed() accountfeed.Feed { return r.feed }

func (r *workerIntakeContextReader) Read(ctx context.Context) ([]accountfeed.FeedItem, error) {
	r.reads++
	r.ctx = ctx
	_, r.observed = operations.CurrentSourceObservation(ctx)
	return r.items, ctx.Err()
}

func TestWorkerIntakeLookupCancellationStopsWrites(t *testing.T) {
	decorators := []struct {
		name string
		wrap func(*workerIntakeContextRepository) operations.Repository
	}{
		{"outcome_transition_failure", func(r *workerIntakeContextRepository) operations.Repository {
			return &outcomeTransitionFailureRepository{Repository: r, ClaimRepository: r}
		}},
		{"heartbeat_loss", func(r *workerIntakeContextRepository) operations.Repository {
			return &heartbeatLossRepository{Repository: r, ClaimRepository: r}
		}},
		{"fail_claim", func(r *workerIntakeContextRepository) operations.Repository {
			return &failClaimRepository{Repository: r, ClaimRepository: r}
		}},
		{"failed_deferral", func(r *workerIntakeContextRepository) operations.Repository {
			return &failedDeferralRepository{Repository: r, ClaimRepository: r}
		}},
		{"fail_status_update_once", func(r *workerIntakeContextRepository) operations.Repository {
			return &failStatusUpdateOnceRepository{Repository: r, ClaimRepository: r}
		}},
		{"cancel_on_status", func(r *workerIntakeContextRepository) operations.Repository {
			return &cancelOnStatusRepository{Repository: r, ClaimRepository: r, cancel: r.cancel}
		}},
	}
	for _, decorator := range decorators {
		t.Run(decorator.name, func(t *testing.T) {
			for _, refresh := range []bool{false, true} {
				name := "create"
				if refresh {
					name = "refresh"
				}
				t.Run(name, func(t *testing.T) {
					base := operations.NewMemoryRepository()
					ctx, cancel := context.WithCancel(context.WithValue(context.Background(), workerIntakeCallerKey{}, uuid.New()))
					defer cancel()
					probe := &workerIntakeContextRepository{MemoryRepository: base, want: ctx, cancel: cancel}
					feed := accountfeed.Feed{
						ID: uuid.New(), Name: "intake", Provider: "local", AccountLabel: "primary",
						SourceType: accountfeed.SourceLocalJSONFile, Path: "feed.json",
						OwnerUserID: "user-1", WorkspaceID: "local", Enabled: true,
					}
					item := accountfeed.FeedItem{ExternalID: "first", Title: "Review intake", Body: "Local evidence", RawJSON: `{"revision":2}`}
					input, err := feed.ToOperationInput(item)
					if err != nil {
						t.Fatal(err)
					}
					if refresh {
						seed := input
						seed.EvidenceJSON = `{"revision":1}`
						if _, err := operations.NewService(base).Ingest(seed); err != nil {
							t.Fatal(err)
						}
					}
					filter := operations.Filter{OwnerUserID: feed.OwnerUserID, WorkspaceID: feed.WorkspaceID}
					before, err := base.List(filter)
					if err != nil {
						t.Fatal(err)
					}
					audits := make(map[uuid.UUID][]models.OperationEvent)
					for _, op := range before {
						events, err := base.ListEvents(op.ID, 0)
						if err != nil {
							t.Fatal(err)
						}
						audits[op.ID] = events
					}
					reader := &workerIntakeContextReader{feed: feed, items: []accountfeed.FeedItem{
						item, {ExternalID: "second", Title: "Must not be ingested"},
					}}
					later := &workerIntakeContextReader{feed: feed, items: reader.items}
					worker := New(operations.NewService(decorator.wrap(probe)), nil, []accountfeed.Reader{reader, later}, Options{
						OwnerUserID: feed.OwnerUserID, WorkspaceID: feed.WorkspaceID,
					})
					report, err := worker.RunOnce(ctx)
					if !errors.Is(err, context.Canceled) || errors.Is(err, ErrReportedFailures) || ctx.Err() != context.Canceled {
						t.Fatalf("lookup cancellation not propagated: report=%+v err=%v", report, err)
					}
					if probe.lookups != 1 || probe.found != refresh || probe.creates != 0 || probe.updates != 0 || probe.legacyCalls != 0 || probe.claimCalls != 0 {
						t.Fatalf("canceled lookup reached a write, fallback, or processing: probe=%+v", probe)
					}
					if reader.reads != 1 || reader.ctx == nil || !reader.observed || reader.ctx.Value(workerIntakeCallerKey{}) != ctx.Value(workerIntakeCallerKey{}) || reader.ctx.Err() != context.Canceled || later.reads != 0 || report.FeedsRead != 1 || report.ItemsIngested != 1 || report.OperationsCreated != 0 || report.Classified != 0 || report.AutoExecuted != 0 || len(report.Errors) != 0 {
						t.Fatalf("worker continued after cancellation: report=%+v reads=%d later=%d", report, reader.reads, later.reads)
					}
					after, err := base.List(filter)
					if err != nil || !reflect.DeepEqual(after, before) {
						t.Fatalf("canceled intake changed operations: before=%+v after=%+v err=%v", before, after, err)
					}
					for id, beforeEvents := range audits {
						afterEvents, err := base.ListEvents(id, 0)
						if err != nil || !reflect.DeepEqual(afterEvents, beforeEvents) {
							t.Fatalf("canceled intake changed audit for %s: before=%+v after=%+v err=%v", id, beforeEvents, afterEvents, err)
						}
					}
				})
			}
		})
	}
}
