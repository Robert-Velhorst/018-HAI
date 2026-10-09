// Package background runs the autonomous back-office loop (§10.16). One RunOnce
// pass ingests account-feed items into the Operation Ledger, then for each new
// Operation runs the deterministic pipeline: privacy scan -> risk/autonomy
// decision -> route (auto-execute the safe local worker + verify, request
// approval, block, draft, or observe). Every step writes audit events and moves
// the Operation through its state machine. Nothing is faked: only the local
// safe worker actually executes, and completion requires passing verification.
package background

import (
	"context"
	"errors"
	"fmt"
	"time"

	"automation-hub-backend/internal/accountfeed"
	"automation-hub-backend/internal/autonomypolicy"
	"automation-hub-backend/internal/executionbroker"
	"automation-hub-backend/internal/idempotency"
	"automation-hub-backend/internal/lifecycle"
	"automation-hub-backend/internal/modelintelligence"
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/operations"
	"automation-hub-backend/internal/privacyfilter"

	"github.com/google/uuid"
)

// ErrBusy is returned when a RunOnce is already in progress.
var ErrBusy = errors.New("background: run already in progress")

// ErrReportedFailures indicates that a RunOnce pass completed with failures
// retained in Report.Errors. Callers can use this to avoid recording partial
// feed or ledger failures as a successful scheduled pass.
var ErrReportedFailures = errors.New("background: run recorded failures")

var errExecutionDeferredByPolicy = errors.New("background: safe execution deferred because the current policy no longer permits it")

// Options configures a background worker.
type Options struct {
	OwnerUserID   string
	WorkspaceID   string
	Mode          autonomypolicy.Mode
	EmergencyStop bool
	MaxOps        int
}

// Worker orchestrates one background pass over feeds and the Operation Ledger.
type Worker struct {
	svc          *operations.Service
	broker       *executionbroker.Broker
	readers      []accountfeed.Reader
	feedRegistry *accountfeed.Registry
	modelInt     *modelintelligence.Service // optional; drives the fast-triage lane
	control      Control                    // optional; live mode + emergency stop
	blockRules   BlockRules                 // optional; operator "block similar" rules
	opts         Options
	now          func() time.Time
	leaseKey     leaseKey
	claimOwner   uuid.UUID
	claimLease   time.Duration
}

const operationClaimLease = 2 * time.Minute

// New builds a worker. If opts.MaxOps <= 0 a default of 50 is used.
func New(svc *operations.Service, broker *executionbroker.Broker, readers []accountfeed.Reader, opts Options) *Worker {
	if opts.WorkspaceID == "" {
		opts.WorkspaceID = "local"
	}
	if opts.MaxOps <= 0 {
		opts.MaxOps = 50
	} else if opts.MaxOps > 200 {
		opts.MaxOps = 200
	}
	if opts.Mode == "" {
		opts.Mode = autonomypolicy.ModeAutonomousSafe
	}
	return &Worker{
		svc:        svc,
		broker:     broker,
		readers:    readers,
		opts:       opts,
		now:        time.Now,
		leaseKey:   leaseKey{service: svc, ownerUserID: opts.OwnerUserID, workspaceID: opts.WorkspaceID},
		claimOwner: uuid.New(),
		claimLease: operationClaimLease,
	}
}

// WithModelIntelligence attaches a model-intelligence service so the fast-triage
// lane runs a real (bounded, local) model call per operation and records
// telemetry. Returns the worker for chaining.
func (w *Worker) WithModelIntelligence(mi *modelintelligence.Service) *Worker {
	w.modelInt = mi
	return w
}

// Attach before serving requests. Registry failure never falls back to cached
// readers; both the scheduled and interactive pass use canonical configurations.
func (w *Worker) WithFeedRegistry(registry *accountfeed.Registry) *Worker {
	w.feedRegistry = registry
	return w
}

// Control supplies the live autonomy mode + emergency-stop state so pause/
// resume/mode changes take effect on the next pass without rebuilding the
// worker.
type Control interface {
	Mode() autonomypolicy.Mode
	EmergencyStop() bool
}

// WithControl attaches a live control source. Returns the worker for chaining.
func (w *Worker) WithControl(c Control) *Worker {
	w.control = c
	return w
}

// BlockRules decides whether an operation matches an operator "block similar"
// rule and must be blocked without autonomous processing.
type BlockRules interface {
	ShouldBlock(operationType, title string) (bool, string)
}

// WithBlockRules attaches a block-rule source consulted for every operation.
func (w *Worker) WithBlockRules(b BlockRules) *Worker {
	w.blockRules = b
	return w
}

func (w *Worker) effectiveMode() autonomypolicy.Mode {
	if w.control != nil {
		return w.control.Mode()
	}
	return w.opts.Mode
}

func (w *Worker) effectiveEmergencyStop() bool {
	if w.control != nil {
		return w.control.EmergencyStop()
	}
	return w.opts.EmergencyStop
}

// Report summarizes a RunOnce pass.
type Report struct {
	FeedsRead                       int      `json:"feedsRead"`
	ItemsIngested                   int      `json:"itemsIngested"`
	OperationsCreated               int      `json:"operationsCreated"`
	RecoveredOperations             int      `json:"recoveredOperations"`
	UnleasedExecutingOperations     int      `json:"unleasedExecutingOperations"`
	ExpiredOperationClaimsRemaining int      `json:"expiredOperationClaimsRemaining"`
	Classified                      int      `json:"classified"`
	Triaged                         int      `json:"triaged"`
	AutoExecuted                    int      `json:"autoExecuted"`
	Verified                        int      `json:"verified"`
	Failed                          int      `json:"failed"`
	AwaitingApproval                int      `json:"awaitingApproval"`
	Blocked                         int      `json:"blocked"`
	Drafted                         int      `json:"drafted"`
	Observed                        int      `json:"observed"`
	Interrupted                     int      `json:"interrupted"`
	DeferredByPolicy                int      `json:"deferredByPolicy"`
	Errors                          []string `json:"errors,omitempty"`
}

// RunOnce performs a single background pass. It acquires a process-local pass
// lease and then uses durable per-operation claims across backend replicas.
func (w *Worker) RunOnce(ctx context.Context) (Report, error) {
	ctx, finish, err := operations.BindExecutionContext(ctx)
	if err != nil {
		return Report{}, err
	}
	defer finish()
	leaseRef, leaseOwner, ok := acquireRunLease(w.leaseKey)
	if !ok {
		return Report{}, ErrBusy
	}
	defer releaseRunLease(w.leaseKey, leaseRef, leaseOwner)

	var rep Report
	recovery, err := w.svc.RecoverExpiredClaims(ctx, w.opts.OwnerUserID, w.opts.WorkspaceID, 200)
	if err != nil {
		rep.Errors = append(rep.Errors, fmt.Sprintf("recover expired operation claims: %v", err))
	} else {
		rep.RecoveredOperations = recovery.Recovered
		rep.UnleasedExecutingOperations = recovery.UnleasedRunning + recovery.UnleasedVerifying
		rep.ExpiredOperationClaimsRemaining = recovery.ExpiredClaimsRemain
	}
	if err := ctx.Err(); err != nil {
		return rep, err
	}
	if err := w.ingest(ctx, &rep); err != nil {
		return rep, err
	}
	if err := ctx.Err(); err != nil {
		return rep, err
	}
	if w.effectiveEmergencyStop() {
		// Emergency stop still ingests for the record but processes nothing.
		return rep, reportFailures(rep)
	}
	if err := w.process(ctx, &rep); err != nil {
		return rep, err
	}
	if err := ctx.Err(); err != nil {
		return rep, err
	}
	return rep, reportFailures(rep)
}

// ingest reads every feed and creates/refreshes Operations for its items.
func (w *Worker) ingest(ctx context.Context, rep *Report) error {
	if w.feedRegistry != nil {
		reports, err := w.feedRegistry.SyncDueContext(ctx, accountfeed.FeedScope{OwnerUserID: w.opts.OwnerUserID, WorkspaceID: w.opts.WorkspaceID})
		for _, result := range reports {
			if result.ReadCompleted {
				rep.FeedsRead++
			}
			rep.ItemsIngested += result.OperationsCreated + result.OperationsRefresh
			rep.OperationsCreated += result.OperationsCreated
			rep.Errors = append(rep.Errors, result.Errors...)
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			rep.Errors = append(rep.Errors, "configured feed storage could not be verified; inspect local operator diagnostics")
			return reportFailures(*rep)
		}
		return nil
	}
	for _, r := range w.readers {
		if err := ctx.Err(); err != nil {
			return err
		}
		feed := r.Feed()
		if !feed.Enabled {
			continue
		}
		start := feed.SourceObservationStart()
		// Explicit static-reader composition remains a trusted non-registry
		// class. Once an origin is registry-managed this cannot change its config.
		start.RegistryManaged = false
		err := w.svc.WithSourceObservation(ctx, start, func(observed context.Context) error {
			return w.ingestObservedFeed(observed, r, feed, rep)
		})
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			rep.Errors = append(rep.Errors, fmt.Sprintf("feed %s: %v", feed.Name, err))
		}
	}
	return nil
}

func (w *Worker) ingestObservedFeed(ctx context.Context, r accountfeed.Reader, feed accountfeed.Feed, rep *Report) error {
	rep.FeedsRead++
	items, err := r.Read(ctx)
	if err != nil {
		return err
	}
	for _, it := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		rep.ItemsIngested++
		in, err := feed.ToOperationInput(it)
		if err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("feed %s item %s: %v", feed.Name, it.ExternalID, err))
			continue
		}
		res, err := w.svc.IngestContext(ctx, in)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			rep.Errors = append(rep.Errors, fmt.Sprintf("ingest %s: %v", it.ExternalID, err))
			continue
		}
		if res.Created {
			rep.OperationsCreated++
		}
	}
	return nil
}

// process claims one operation at a time. A batch of claims would let later
// items expire while waiting behind a slow model or execution call.
func (w *Worker) process(ctx context.Context, rep *Report) error {
	for processed := 0; processed < w.opts.MaxOps; processed++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		claimed, err := w.svc.ClaimNext(ctx, w.opts.OwnerUserID, w.opts.WorkspaceID, w.claimOwner, w.claimLease)
		if err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("claim next operation: %v", err))
			return fmt.Errorf("claim next operation: %w", err)
		}
		if claimed == nil {
			return ctx.Err()
		}
		if err := w.validateClaimedOperation(claimed.Operation); err != nil {
			if releaseErr := w.svc.ReleaseClaim(ctx, claimed.Claim); releaseErr != nil {
				err = errors.Join(err, fmt.Errorf("release invalid operation claim: %w", releaseErr))
			}
			rep.Errors = append(rep.Errors, err.Error())
			return errors.Join(ErrReportedFailures, fmt.Errorf("validate claimed operation: %w", err))
		}
		if err := w.processClaimed(ctx, *claimed, rep); err != nil {
			if err == errExecutionDeferredByPolicy {
				continue
			}
			rep.Errors = append(rep.Errors, fmt.Sprintf("operation %s: %v", claimed.Operation.ID, err))
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return errors.Join(ErrReportedFailures, fmt.Errorf("operation %s: %w", claimed.Operation.ID, err))
		}
	}
	return ctx.Err()
}

func (w *Worker) validateClaimedOperation(op models.Operation) error {
	status := operations.OperationStatus(op.Status)
	if status == operations.StatusDrafting && operations.CurrentDecision(op.CurrentDecision) != operations.DecisionCreateDraft {
		return fmt.Errorf("operation %s: drafting status has non-draft decision %q", op.ID, op.CurrentDecision)
	}
	if status == operations.StatusApproved {
		if !operations.IsSourceDerived(op) {
			return nil
		}
		_, err := w.svc.SourceApprovalForExecution(op)
		return err
	}
	if status == operations.StatusReady || (status == operations.StatusClassified && operations.CurrentDecision(op.CurrentDecision) == operations.DecisionRunSafeLocalWorker) {
		return safeExecutionPolicyError(op)
	}
	return nil
}

func (w *Worker) processClaimed(ctx context.Context, claimed operations.ClaimedOperation, rep *Report) error {
	claimCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	heartbeatDone := make(chan error, 1)
	heartbeatStopped := make(chan struct{})
	if !lifecycle.Go(claimCtx, "background-claim-heartbeat", func() {
		defer close(heartbeatStopped)
		ticker := time.NewTicker(w.claimLease / 3)
		defer ticker.Stop()
		for {
			select {
			case <-claimCtx.Done():
				heartbeatDone <- nil
				return
			case <-ticker.C:
				if err := w.svc.RenewClaim(claimCtx, claimed.Claim, w.claimLease); err != nil {
					heartbeatDone <- fmt.Errorf("renew claim generation %d: %w", claimed.Claim.Generation, err)
					cancel()
					return
				}
			}
		}
	}) {
		return context.Canceled
	}
	defer func() { cancel(); <-heartbeatStopped }()
	processErr := w.processOne(claimCtx, claimed.Operation, claimed.Claim, rep)
	cancel()
	heartbeatErr := <-heartbeatDone
	if processErr != nil && heartbeatErr != nil {
		return errors.Join(processErr, heartbeatErr)
	}
	return processErr
}

func (w *Worker) transition(ctx context.Context, claim operations.ExecutionClaim, op models.Operation, to operations.OperationStatus, message string) (*models.Operation, error) {
	return w.svc.TransitionClaimed(ctx, claim, op, to, "hai", "", message)
}

func reportFailures(rep Report) error {
	if len(rep.Errors) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %d error(s) retained in report", ErrReportedFailures, len(rep.Errors))
}

func (w *Worker) processOne(ctx context.Context, op models.Operation, claim operations.ExecutionClaim, rep *Report) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	status := operations.OperationStatus(op.Status)
	if status == operations.StatusDrafting {
		return w.finishDraft(ctx, op, claim, rep)
	}
	if status == operations.StatusApproved {
		if !operations.IsSourceDerived(op) {
			return w.svc.ReleaseClaim(ctx, claim)
		}
		return w.runSafe(ctx, op, claim, rep)
	}
	if status == operations.StatusClassified || status == operations.StatusReady {
		return w.resumeClassified(ctx, op, claim, rep)
	}
	now := w.now().UTC()
	scan := privacyfilter.Scan(op.Title+"\n"+op.Description, 280)
	decision := autonomypolicy.Decide(autonomypolicy.Input{
		Title:         op.Title,
		Content:       op.Description,
		OperationType: op.OperationType,
		SourceDerived: op.SourceIdentityHash != "" || op.SourceID != nil || op.AccountFeedID != nil,
		Privacy:       scan,
		Mode:          w.effectiveMode(),
		Reversible:    true, // feed items default reversible; risk classifier bumps dangerous ones to approval
		EmergencyStop: w.effectiveEmergencyStop(),
	}, now)

	// Operator "block similar" rules override the decision: matching work is
	// blocked without any autonomous processing (§10.19 block-similar).
	if w.blockRules != nil {
		if blocked, reason := w.blockRules.ShouldBlock(op.OperationType, op.Title); blocked {
			applyDecision(&op, decision, scan)
			op.RiskLevel = string(operations.RiskHigh)
			op.CurrentDecision = string(operations.DecisionBlock)
			op.RecommendedAction = "blocked by operator rule"
			classified, err := w.transition(ctx, claim, op, operations.StatusClassified, "classified (block rule): "+reason)
			if err != nil {
				return err
			}
			rep.Classified++
			return w.routeClassified(ctx, *classified, claim, "blocked by operator rule: "+reason, rep)
		}
	}

	applyDecision(&op, decision, scan)

	// Fast-triage lane (§16): if a model-intelligence service is attached, run a
	// real bounded local model call to categorize the item. The lane affects the
	// operation record (model provider/model fields) and records telemetry that
	// surfaces on the model-intelligence dashboard.
	classifyMsg := "classified: " + decision.Reason
	highRisk := decision.Risk == operations.RiskHigh
	if w.modelInt != nil {
		if tri, tErr := w.modelInt.Triage(ctx, op.OperationType, op.Title, op.Description, scan.SafeForCloudModel, highRisk, op.ID.String()); tErr == nil && tri.Routed {
			op.ModelProviderID = tri.ProviderID
			op.ModelID = tri.ModelID
			classifyMsg = "classified [" + tri.Category + "]: " + decision.Reason
			rep.Triaged++
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	classified, err := w.transition(ctx, claim, op, operations.StatusClassified, classifyMsg)
	if err != nil {
		return err
	}
	rep.Classified++
	op = *classified
	return w.routeClassified(ctx, op, claim, decision.Reason, rep)
}

func (w *Worker) resumeClassified(ctx context.Context, op models.Operation, claim operations.ExecutionClaim, rep *Report) error {
	if operations.OperationStatus(op.Status) == operations.StatusReady {
		if operations.CurrentDecision(op.CurrentDecision) != operations.DecisionRunSafeLocalWorker {
			return fmt.Errorf("ready operation %s has non-executable decision %q", op.ID, op.CurrentDecision)
		}
		return w.runSafe(ctx, op, claim, rep)
	}
	return w.routeClassified(ctx, op, claim, "", rep)
}

func (w *Worker) routeClassified(ctx context.Context, op models.Operation, claim operations.ExecutionClaim, reason string, rep *Report) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if reason == "" {
		reason = op.RecommendedAction
	}
	if reason == "" {
		reason = "previously classified operation"
	}
	switch operations.CurrentDecision(op.CurrentDecision) {
	case operations.DecisionRunSafeLocalWorker:
		return w.runSafe(ctx, op, claim, rep)
	case operations.DecisionBlock:
		_, err := w.transition(ctx, claim, op, operations.StatusBlocked, "blocked: "+reason)
		if err == nil {
			rep.Blocked++
		}
		return err
	case operations.DecisionCreateDraft:
		return w.runDraft(ctx, op, claim, rep, autonomypolicy.Decision{RecommendedAction: op.RecommendedAction})
	case operations.DecisionObserveOnly:
		rep.Observed++
		return w.svc.ReleaseClaim(ctx, claim)
	default:
		// ask_robert and any approval-gated decision.
		if op.RequiresApproval {
			_, err := w.transition(ctx, claim, op, operations.StatusAwaitingApproval, "awaiting approval: "+reason)
			if err == nil {
				rep.AwaitingApproval++
			}
			return err
		}
		rep.Observed++
		return w.svc.ReleaseClaim(ctx, claim)
	}
}

// runSafe executes the local safe worker for a low-risk reversible operation and
// gates completion on passing verification (§8/§10.15).
func (w *Worker) runSafe(ctx context.Context, op models.Operation, claim operations.ExecutionClaim, rep *Report) error {
	sourceApproved := operations.IsSourceDerived(op)
	if sourceApproved {
		if _, err := w.svc.SourceApprovalForExecution(op); err != nil {
			return err
		}
	}
	if err := safeExecutionPolicyErrorWithSourceApproval(op, sourceApproved); err != nil {
		return err
	}
	policyAllowsExecution := w.safeExecutionPolicyAllows(op)
	if !policyAllowsExecution {
		if err := w.svc.ReleaseClaim(ctx, claim); err != nil {
			return fmt.Errorf("release claim after policy change: %w", err)
		}
		// Save uses the operation version and rejects concurrent claims/writes;
		// it cannot overwrite a replacement worker's state after release.
		nextReview := w.now().UTC().Add(time.Minute)
		op.NextReviewAt = &nextReview
		if _, err := w.svc.Save(op, "execution_deferred", "hai", "execution deferred by current policy"); err != nil {
			return fmt.Errorf("persist policy deferral: %w", err)
		}
		rep.DeferredByPolicy++
		return errExecutionDeferredByPolicy
	}
	rep.AutoExecuted++
	outcome, err := ExecuteSafeOperationClaimed(ctx, w.svc, w.broker, op, claim, w.now().UTC(), w.finalSafeExecutionPolicyAllows)
	if outcome.Interrupted {
		rep.Interrupted++
	}
	if err != nil {
		return err
	}
	if outcome.Verified {
		rep.Verified++
	}
	if outcome.Failed {
		rep.Failed++
	}
	return nil
}

func (w *Worker) safeExecutionPolicyAllows(op models.Operation) bool {
	if operations.IsSourceDerived(op) {
		approved := false
		switch operations.OperationStatus(op.Status) {
		case operations.StatusApproved:
			_, err := w.svc.SourceApprovalForExecution(op)
			approved = err == nil
		case operations.StatusRunning:
			approved = w.svc.ValidateConsumedSourceApproval(op) == nil
		}
		if !approved || w.effectiveMode() != autonomypolicy.ModeAutonomousSafe || w.effectiveEmergencyStop() {
			return false
		}
		if w.blockRules != nil {
			blocked, _ := w.blockRules.ShouldBlock(op.OperationType, op.Title)
			if blocked {
				return false
			}
		}
		return true
	}
	decision := autonomypolicy.Decide(autonomypolicy.Input{
		Title:         op.Title,
		Content:       op.Description,
		OperationType: op.OperationType,
		SourceDerived: op.SourceIdentityHash != "" || op.SourceID != nil || op.AccountFeedID != nil,
		Privacy:       privacyfilter.Scan(op.Title+"\n"+op.Description, 280),
		Mode:          w.effectiveMode(),
		Reversible:    true,
		EmergencyStop: w.effectiveEmergencyStop(),
	}, w.now().UTC())
	policyAllowsExecution := decision.Decision == operations.DecisionRunSafeLocalWorker
	if policyAllowsExecution && w.blockRules != nil {
		blocked, _ := w.blockRules.ShouldBlock(op.OperationType, op.Title)
		policyAllowsExecution = !blocked
	}
	return policyAllowsExecution
}

// finalSafeExecutionPolicyAllows runs inside the repository's locked effect
// callback. Receipt integrity was checked after the claimed running transition;
// do not re-enter repository storage here because the memory implementation
// holds its mutex across the callback. Snapshot integrity is rechecked there.
func (w *Worker) finalSafeExecutionPolicyAllows(op models.Operation) bool {
	if !operations.IsSourceDerived(op) {
		return w.safeExecutionPolicyAllows(op)
	}
	if op.Status != string(operations.StatusRunning) || op.RuntimeID != executionbroker.LocalSafeWorkerID ||
		op.VerificationStatus != string(operations.VerificationPending) || !op.RequiresApproval ||
		op.CurrentDecision != string(operations.DecisionAskRobert) ||
		op.AutonomyLevel != string(operations.AutonomyApproval) || op.OwnerType != string(operations.OwnerRobert) ||
		w.effectiveMode() != autonomypolicy.ModeAutonomousSafe || w.effectiveEmergencyStop() {
		return false
	}
	if w.blockRules != nil {
		blocked, _ := w.blockRules.ShouldBlock(op.OperationType, op.Title)
		if blocked {
			return false
		}
	}
	return operations.Validate(op) == nil
}

// runDraft records an internal draft for a draft-mode operation.
func (w *Worker) runDraft(ctx context.Context, op models.Operation, claim operations.ExecutionClaim, rep *Report, decision autonomypolicy.Decision) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	drafting, err := w.transition(ctx, claim, op, operations.StatusDrafting, "preparing internal draft")
	if err != nil {
		return err
	}
	op = *drafting
	op.ResultSummary = decision.RecommendedAction
	return w.finishDraft(ctx, op, claim, rep)
}

func (w *Worker) finishDraft(ctx context.Context, op models.Operation, claim operations.ExecutionClaim, rep *Report) error {
	if op.ResultSummary == "" {
		op.ResultSummary = op.RecommendedAction
	}
	finalizeCtx := ctx
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		finalizeCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
	}
	if _, err := w.transition(finalizeCtx, claim, op, operations.StatusDraftReady, "internal draft ready for review"); err != nil {
		if releaseErr := w.svc.ReleaseClaim(finalizeCtx, claim); releaseErr != nil {
			return errors.Join(err, fmt.Errorf("release claim after draft persistence failure: %w", releaseErr))
		}
		return err
	}
	rep.Drafted++
	return nil
}

// applyDecision maps the policy decision + privacy scan onto Operation fields.
func applyDecision(op *models.Operation, d autonomypolicy.Decision, scan privacyfilter.ScanResult) {
	op.RiskLevel = string(d.Risk)
	op.AutonomyLevel = string(d.Autonomy)
	op.CurrentDecision = string(d.Decision)
	op.RequiresApproval = d.RequiresApproval
	op.OwnerType = string(d.Owner)
	op.RecommendedAction = d.RecommendedAction
	op.NextReviewAt = d.NextReviewAt
	if d.Decision == operations.DecisionRunSafeLocalWorker {
		op.VerificationStatus = string(operations.VerificationPending)
	}
	if wm, err := idempotency.CanonicalJSONString(map[string]any{
		"policyRule":       d.PolicyRule,
		"reason":           d.Reason,
		"privacyRiskLevel": string(scan.PrivacyRiskLevel),
		"sensitiveFields":  scan.SensitiveFields,
		"safeForCloud":     scan.SafeForCloudModel,
		"redactedPreview":  scan.RedactedPreview,
	}); err == nil {
		op.WorldModelStateJSON = wm
	}
}

// safePayload derives the deterministic, bounded safe-worker payload for an
// operation. artifactName is a basename only; the marker binds the artifact to
// the operation identity + source revision.
func safePayload(op models.Operation) executionbroker.SafeWorkerInput {
	name := "operation-" + op.ID.String() + ".txt"
	marker := "HAI-OP " + op.ID.String() + " rev " + op.SourceRevisionHash
	return executionbroker.SafeWorkerInput{ArtifactName: name, Marker: marker}
}
