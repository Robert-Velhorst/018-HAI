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

type contextualProofIssuer interface {
	IssueApprovalProofContext(context.Context, uuid.UUID, TaskApprovalProofRequest) (*ApprovalProof, error)
}

type legacyProofSigner struct{ ApprovalProofService }

func TestApprovalIssuanceOwnsDecisionReads(t *testing.T) {
	for _, boundary := range []string{"valid", "cancel_before", "cancel_configuration", "cancel_decision", "read_error", "missing_decision", "wrong_decision", "legacy", "nil_scope", "cancel_signing", "legacy_signer", "nil_signer"} {
		t.Run(boundary, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), configurationContextKey{}, "issuance"))
			defer cancel()
			id := uuid.New()
			repo := &approvalRegistrationProbe{Repository: newFakeAutomationRepo(&models.Automation{ID: id, Name: "local script", LaunchType: "script", LaunchTarget: "script.ps1"})}
			svc := newTestService(repo, events.Publisher{})
			snapshot, err := svc.(ReviewConfigurationInspector).InspectReviewConfiguration(id)
			if err != nil {
				t.Fatal(err)
			}
			source := "task-review:" + uuid.NewString()
			err = svc.(interface {
				RecordApprovalDecision(uuid.UUID, TaskApprovalDecisionRequest) error
			}).RecordApprovalDecision(id, TaskApprovalDecisionRequest{OwnerIdentity: "alice", Task: "Run tests", ProjectKey: "project", ApprovalSourceID: source, ApprovalBindingDigest: strings.Repeat("a", 64), ApprovedAt: time.Now().UTC(), ReviewConfiguration: snapshot})
			if err != nil {
				t.Fatal(err)
			}
			repo.events = nil
			if boundary == "legacy" {
				svc = newTestService(repo.Repository, events.Publisher{})
			}
			storageError := errors.New("controlled approval read failure")
			switch boundary {
			case "cancel_before":
				cancel()
			case "cancel_configuration":
				repo.afterRead = cancel
			case "cancel_decision":
				repo.afterAck = cancel
			case "read_error":
				repo.readError = storageError
			case "missing_decision":
				repo.missing = true
			case "wrong_decision":
				repo.mismatch = true
			case "nil_scope":
				repo.nilScope = true
			case "cancel_signing":
				svc.(*service).approvalProofs.(*approvalProofService).now = func() time.Time { cancel(); return time.Now() }
			case "legacy_signer":
				svc.(*service).approvalProofs = legacyProofSigner{ApprovalProofService: svc.(*service).approvalProofs}
			case "nil_signer":
				svc.(*service).approvalProofs = nil
			}
			issuer, ok := svc.(contextualProofIssuer)
			if !ok {
				t.Fatal("canonical service has no contextual proof issuer")
			}
			proof, err := issuer.IssueApprovalProofContext(ctx, id, TaskApprovalProofRequest{OwnerIdentity: "alice", Task: "Run tests", ProjectKey: "project", ApprovalSourceID: source})
			if boundary == "valid" {
				if err != nil || proof == nil || len(repo.events) != 2 {
					t.Fatalf("owned issuance failed: %v", err)
				}
				if repo.ctx.Value(configurationContextKey{}) != "issuance" {
					t.Fatal("issuance lost caller context")
				}
				if _, ok := repo.ctx.Deadline(); !ok {
					t.Fatal("issuance has no deadline")
				}
			} else if err == nil || proof != nil {
				t.Fatal("uncertain approval produced a proof")
			}
			if ctx.Err() != nil && !errors.Is(err, context.Canceled) {
				t.Fatal("issuance lost cancellation")
			}
			if boundary == "read_error" && !errors.Is(err, storageError) {
				t.Fatal("issuance lost storage error")
			}
			if (boundary == "cancel_before" || boundary == "legacy" || boundary == "nil_scope") && len(repo.events) != 0 {
				t.Fatal("issuance read without capability")
			}
			if boundary == "cancel_configuration" && len(repo.events) != 1 {
				t.Fatal("cancelled configuration entered decision read")
			}
		})
	}
}
