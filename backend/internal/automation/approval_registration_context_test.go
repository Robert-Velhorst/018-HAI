package automation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/events"
	"automation-hub-backend/internal/models"
	"github.com/google/uuid"
)

type contextualDecisionRecorder interface {
	RecordApprovalDecisionContext(context.Context, uuid.UUID, TaskApprovalDecisionRequest) error
}

type approvalRegistrationProbe struct {
	Repository
	events                      []string
	ctx                         context.Context
	afterAck                    context.CancelFunc
	readError                   error
	mismatchField               string
	afterRead, afterSave        context.CancelFunc
	saveError                   error
	missing, mismatch, nilScope bool
}

type approvalRegistrationView struct {
	Repository
	root *approvalRegistrationProbe
	ctx  context.Context
}

func (r *approvalRegistrationProbe) WithAutomationRepositoryContext(ctx context.Context) (Repository, error) {
	r.ctx = ctx
	if r.nilScope {
		return nil, nil
	}
	return &approvalRegistrationView{Repository: r.Repository, root: r, ctx: ctx}, ctx.Err()
}

func (v *approvalRegistrationView) FindByID(id uuid.UUID) (*models.Automation, error) {
	v.root.events = append(v.root.events, "configuration")
	item, err := v.Repository.FindByID(id)
	if v.root.afterRead != nil {
		v.root.afterRead()
	}
	return item, err
}

func (v *approvalRegistrationView) SaveApprovalDecision(record *ApprovalDecisionRecord) error {
	v.root.events = append(v.root.events, "save")
	if v.root.saveError != nil {
		return v.root.saveError
	}
	err := v.Repository.SaveApprovalDecision(record)
	if v.root.afterSave != nil {
		v.root.afterSave()
	}
	return err
}

func (v *approvalRegistrationView) FindApprovalDecision(sourceID string) (*ApprovalDecisionRecord, error) {
	v.root.events = append(v.root.events, "readback")
	if v.root.readError != nil {
		return nil, v.root.readError
	}
	if v.root.missing {
		return nil, nil
	}
	record, err := v.Repository.FindApprovalDecision(sourceID)
	if record != nil && v.root.mismatch {
		copy := *record
		copy.OwnerIdentity = "bob"
		record = &copy
	}
	if record != nil && v.root.mismatchField != "" {
		copy := *record
		switch v.root.mismatchField {
		case "source":
			copy.SourceID = "task-review:" + uuid.NewString()
		case "digest":
			copy.ActionDigest = strings.Repeat("b", 64)
		case "time":
			copy.ApprovedAt = copy.ApprovedAt.Add(time.Microsecond)
		}
		record = &copy
	}
	if v.root.afterAck != nil {
		v.root.afterAck()
	}
	return record, err
}

func TestApprovalRegistrationRequiresBoundedExactAcknowledgement(t *testing.T) {
	for _, boundary := range []string{"valid", "cancel_before", "cancel_after_read", "cancel_after_save", "save_error", "missing", "mismatch", "legacy", "nil_scope", "wrong_source", "wrong_digest", "wrong_time", "read_error", "cancel_after_ack"} {
		t.Run(boundary, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), configurationContextKey{}, "registration"))
			defer cancel()
			id := uuid.New()
			repo := &approvalRegistrationProbe{Repository: newFakeAutomationRepo(&models.Automation{ID: id, Name: "local script", LaunchType: "script", LaunchTarget: "script.ps1"})}
			var repository Repository = repo
			if boundary == "legacy" {
				repository = repo.Repository
			}
			svc := newTestService(repository, events.Publisher{})
			snapshot, err := svc.(ReviewConfigurationInspector).InspectReviewConfiguration(id)
			if err != nil {
				t.Fatal(err)
			}
			recorder, ok := svc.(contextualDecisionRecorder)
			if !ok {
				t.Fatal("canonical service lacks contextual approval registration")
			}
			sourceID := "task-review:" + uuid.NewString()
			request := TaskApprovalDecisionRequest{OwnerIdentity: "alice", Task: "Run tests", ProjectKey: "project", ApprovalSourceID: sourceID, ApprovalBindingDigest: strings.Repeat("a", 64), ApprovedAt: time.Now().UTC(), ReviewConfiguration: snapshot}
			storageError := errors.New("controlled registration storage failure")
			switch boundary {
			case "cancel_before":
				cancel()
			case "cancel_after_read":
				repo.afterRead = cancel
			case "cancel_after_save":
				repo.afterSave = cancel
			case "save_error":
				repo.saveError = storageError
			case "missing":
				repo.missing = true
			case "mismatch":
				repo.mismatch = true
			case "nil_scope":
				repo.nilScope = true
			case "wrong_source":
				repo.mismatchField = "source"
			case "wrong_digest":
				repo.mismatchField = "digest"
			case "wrong_time":
				repo.mismatchField = "time"
			case "read_error":
				repo.readError = storageError
			case "cancel_after_ack":
				repo.afterAck = cancel
			}
			err = recorder.RecordApprovalDecisionContext(ctx, id, request)
			if boundary == "valid" {
				if err != nil || strings.Join(repo.events, ",") != "configuration,save,readback" {
					t.Fatalf("registration not acknowledged: %v events=%v", err, repo.events)
				}
				stored, readErr := repo.Repository.FindApprovalDecision(sourceID)
				if readErr != nil || stored == nil || !stored.ApprovedAt.Equal(request.ApprovedAt.UTC().Truncate(time.Microsecond)) {
					t.Fatal("approval timestamp does not match storage precision")
				}
			} else if err == nil {
				t.Fatal("unconfirmed registration reported success")
			}
			if ctx.Err() != nil && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation identity lost: %v", err)
			}
			if (boundary == "save_error" || boundary == "read_error") && !errors.Is(err, storageError) {
				t.Fatal("storage failure identity lost")
			}
			if boundary == "cancel_after_read" && len(repo.events) != 1 {
				t.Fatal("registration write started after cancelled configuration read")
			}
			if boundary == "cancel_after_save" || boundary == "missing" || boundary == "mismatch" || strings.HasPrefix(boundary, "wrong_") || boundary == "read_error" || boundary == "cancel_after_ack" {
				if !errors.Is(err, ErrApprovalRegistrationUnconfirmed) {
					t.Fatal("post-write uncertainty was not identified")
				}
				stored, readErr := repo.Repository.FindApprovalDecision(sourceID)
				if readErr != nil || stored == nil || stored.OwnerIdentity != "alice" {
					t.Fatal("original stored approval was changed during failed acknowledgement")
				}
			}
			if repo.ctx != nil {
				if repo.ctx.Value(configurationContextKey{}) != "registration" {
					t.Fatal("registration lost caller context")
				}
				if _, ok := repo.ctx.Deadline(); !ok {
					t.Fatal("registration scope has no deadline")
				}
			}
		})
	}
}
