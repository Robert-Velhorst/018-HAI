package automation

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"automation-hub-backend/internal/events"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

func TestTaskReviewConfigurationBindingRegistersAndLaunchesExactAction(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			var calls atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != method {
					t.Errorf("method = %q, want %q", r.Method, method)
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer target.Close()
			t.Setenv("AUTOMATION_API_ALLOWED_HOSTS", "127.0.0.1")
			id := uuid.New()
			repo := newFakeAutomationRepo(&models.Automation{
				ID: id, Name: "Reviewed API", URLPath: "reviewed-api",
				LaunchType: "api", LaunchTarget: method + " " + target.URL,
				ExpectedHTTPStatus: http.StatusNoContent,
			})
			svc := newTestService(repo, events.Publisher{})
			snapshot, err := svc.(ReviewConfigurationInspector).InspectReviewConfiguration(id)
			if err != nil {
				t.Fatal(err)
			}
			// The inferred goal may differ from original intake prose. The
			// configuration-only historical digest must not depend on either.
			request := TaskLaunchRequest{
				OwnerIdentity: "alice", Task: "Perform the inferred reviewed API action",
				ProjectKey: "project", ApprovalSourceID: "task-review:" + uuid.NewString(),
				IdempotencyKey: "review-binding:" + uuid.NewString(),
			}
			err = svc.(ApprovalDecisionRecorder).RecordApprovalDecision(id, TaskApprovalDecisionRequest{
				OwnerIdentity: request.OwnerIdentity, Task: request.Task, ProjectKey: request.ProjectKey,
				ApprovalSourceID: request.ApprovalSourceID, ApprovalBindingDigest: strings.Repeat("b", 64),
				ReviewConfiguration: snapshot, ApprovedAt: time.Now().UTC(),
			})
			if err != nil {
				t.Fatalf("record exact reviewed configuration: %v", err)
			}
			request.ApprovalProof, err = svc.(ApprovalProofIssuer).IssueApprovalProof(id, TaskApprovalProofRequest{
				OwnerIdentity: request.OwnerIdentity, Task: request.Task, ProjectKey: request.ProjectKey,
				OriginalRequest:  "Please carry out the requested API operation",
				ApprovalSourceID: request.ApprovalSourceID,
			})
			if err != nil {
				t.Fatalf("issue proof: %v", err)
			}
			request.ApprovalBindingDigest = request.ApprovalProof.ActionDigest
			if request.ApprovalBindingDigest == snapshot.ConfigurationDigest {
				t.Fatal("configuration digest was conflated with contextual execution digest")
			}
			result, err := svc.LaunchTask(id, request)
			if err != nil || result == nil || result.Status != "completed" || result.LaunchEventID == uuid.Nil || calls.Load() != 1 {
				t.Fatalf("exact action result=%#v err=%v calls=%d", result, err, calls.Load())
			}
		})
	}
}

func TestTaskReviewConfigurationBindingRejectsMissingOldAndChangedSnapshots(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*testing.T, *fakeAutomationRepo, *TaskApprovalDecisionRequest)
	}{
		{"missing", func(_ *testing.T, _ *fakeAutomationRepo, r *TaskApprovalDecisionRequest) { r.ReviewConfiguration = nil }},
		{"old-version", func(_ *testing.T, _ *fakeAutomationRepo, r *TaskApprovalDecisionRequest) {
			r.ReviewConfiguration.Version = "automation-review-configuration.v0"
		}},
		{"wrong-automation", func(_ *testing.T, _ *fakeAutomationRepo, r *TaskApprovalDecisionRequest) {
			r.ReviewConfiguration.AutomationID = uuid.New()
		}},
		{"malformed-digest", func(_ *testing.T, _ *fakeAutomationRepo, r *TaskApprovalDecisionRequest) {
			r.ReviewConfiguration.ConfigurationDigest = "not-a-digest"
		}},
		{"changed-target", func(_ *testing.T, repo *fakeAutomationRepo, _ *TaskApprovalDecisionRequest) {
			repo.automation.LaunchTarget = "POST http://localhost/changed"
		}},
		{"changed-scope", func(_ *testing.T, repo *fakeAutomationRepo, _ *TaskApprovalDecisionRequest) {
			repo.automation.LaunchTarget = "GET http://localhost/original"
		}},
		{"changed-type", func(_ *testing.T, repo *fakeAutomationRepo, _ *TaskApprovalDecisionRequest) {
			repo.automation.LaunchType = "script"
			repo.automation.LaunchTarget = "changed.sh"
		}},
		{"changed-runtime-model", func(_ *testing.T, repo *fakeAutomationRepo, _ *TaskApprovalDecisionRequest) {
			repo.automation.RuntimeModel = "changed-model"
		}},
		{"changed-policy", func(t *testing.T, _ *fakeAutomationRepo, _ *TaskApprovalDecisionRequest) {
			t.Setenv("AUTOMATION_API_ALLOWED_HOSTS", "changed.example")
		}},
		{"missing-request-binding", func(_ *testing.T, _ *fakeAutomationRepo, r *TaskApprovalDecisionRequest) {
			r.ApprovalBindingDigest = ""
		}},
		{"invalid-request-binding", func(_ *testing.T, _ *fakeAutomationRepo, r *TaskApprovalDecisionRequest) {
			r.ApprovalBindingDigest = "invalid"
		}},
		{"stale-decision", func(_ *testing.T, _ *fakeAutomationRepo, r *TaskApprovalDecisionRequest) {
			r.ApprovedAt = time.Now().UTC().Add(-maximumApprovalDecisionAge - time.Second)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AUTOMATION_API_ALLOWED_HOSTS", "localhost")
			id := uuid.New()
			repo := newFakeAutomationRepo(&models.Automation{
				ID: id, Name: "Historical configuration", URLPath: "historical-configuration",
				LaunchType: "api", LaunchTarget: "POST http://localhost/original",
			})
			svc := newTestService(repo, events.Publisher{})
			snapshot, err := svc.(ReviewConfigurationInspector).InspectReviewConfiguration(id)
			if err != nil {
				t.Fatal(err)
			}
			request := TaskApprovalDecisionRequest{
				OwnerIdentity: "alice", Task: "Execute inferred goal", ProjectKey: "project",
				ApprovalSourceID:      "task-review:" + uuid.NewString(),
				ApprovalBindingDigest: strings.Repeat("b", 64), ReviewConfiguration: snapshot,
				ApprovedAt: time.Now().UTC(),
			}
			tc.mutate(t, repo, &request)
			if err := svc.(ApprovalDecisionRecorder).RecordApprovalDecision(id, request); err == nil {
				t.Fatal("unsafe historical binding was registered")
			}
			if len(repo.approvalDecisions) != 0 || len(repo.launchIntents) != 0 || len(repo.launchEvents) != 0 {
				t.Fatalf("rejection recorded authority or execution: %#v", repo)
			}
		})
	}
}

func TestTaskReviewConfigurationBindingCannotRebindExistingDecision(t *testing.T) {
	id := uuid.New()
	repo := newFakeAutomationRepo(&models.Automation{
		ID: id, Name: "Immutable authority", URLPath: "immutable-authority",
		LaunchType: "api", LaunchTarget: "POST http://localhost/original",
	})
	svc := newTestService(repo, events.Publisher{})
	snapshot, err := svc.(ReviewConfigurationInspector).InspectReviewConfiguration(id)
	if err != nil {
		t.Fatal(err)
	}
	request := TaskApprovalDecisionRequest{
		OwnerIdentity: "alice", Task: "Execute original goal", ProjectKey: "project",
		ApprovalSourceID:      "task-review:" + uuid.NewString(),
		ApprovalBindingDigest: strings.Repeat("b", 64), ReviewConfiguration: snapshot,
		ApprovedAt: time.Now().UTC(),
	}
	recorder := svc.(ApprovalDecisionRecorder)
	if err := recorder.RecordApprovalDecision(id, request); err != nil {
		t.Fatal(err)
	}
	original := repo.approvalDecisions[request.ApprovalSourceID]
	repo.automation.LaunchTarget = "POST http://localhost/changed"
	if err := recorder.RecordApprovalDecision(id, request); err == nil {
		t.Fatal("old historical snapshot authorized changed configuration")
	}
	if actual := repo.approvalDecisions[request.ApprovalSourceID]; actual != original {
		t.Fatalf("immutable recorded authority changed: %#v", actual)
	}
	if proof, err := svc.(ApprovalProofIssuer).IssueApprovalProof(id, TaskApprovalProofRequest{
		OwnerIdentity: request.OwnerIdentity, Task: request.Task, ProjectKey: request.ProjectKey,
		ApprovalSourceID: request.ApprovalSourceID,
	}); err == nil || proof != nil {
		t.Fatalf("changed configuration minted a proof: %#v, %v", proof, err)
	}
}
