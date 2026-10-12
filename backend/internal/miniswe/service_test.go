package miniswe

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/workflow"

	"github.com/google/uuid"
)

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

type miniSWEGateStub struct {
	endpoint, modelID string
	err               error
}

func (s *miniSWEGateStub) EnsureMiniSWEOllamaModel(endpointURL, modelID string) error {
	s.endpoint, s.modelID = endpointURL, modelID
	return s.err
}

type workflowLookupStub struct{ record *workflow.WorkflowRecord }

func (s workflowLookupStub) GetForOwner(owner string, id uuid.UUID) (*workflow.WorkflowRecord, error) {
	if s.record == nil || owner != s.record.Item.OwnerIdentity || id != s.record.Item.ID {
		return nil, ErrInvalidRequest
	}
	return s.record, nil
}

type workflowPatchLinkerStub struct {
	workflowLookupStub
	owner, workflowID, proposalID, workspaceID, digest string
	changedFiles                                       int
	err                                                error
}

func (s *workflowPatchLinkerStub) AttachMiniSWEPatchProposal(ownerIdentity, workflowID, proposalID, workspaceID, diffDigest string, changedFiles int) error {
	s.owner, s.workflowID, s.proposalID, s.workspaceID, s.digest = ownerIdentity, workflowID, proposalID, workspaceID, diffDigest
	s.changedFiles = changedFiles
	return s.err
}

type repositoryStub struct {
	created []models.MiniSWEPatchProposal
	saved   []models.MiniSWEPatchProposal
}

func (r *repositoryStub) Create(record *models.MiniSWEPatchProposal) error {
	r.created = append(r.created, *record)
	return nil
}
func (r *repositoryStub) Save(record *models.MiniSWEPatchProposal) error {
	r.saved = append(r.saved, *record)
	return nil
}
func (r *repositoryStub) ListForOwner(owner string, _ int) ([]models.MiniSWEPatchProposal, error) {
	result := make([]models.MiniSWEPatchProposal, 0, len(r.saved))
	for _, record := range r.saved {
		if record.OwnerIdentity == owner {
			result = append(result, record)
		}
	}
	return result, nil
}

func approvedWorkflow(owner string) *workflow.WorkflowRecord {
	return &workflow.WorkflowRecord{Item: models.WorkflowItem{
		ID: uuid.New(), OwnerIdentity: owner, Title: "Fix the local parser", Description: "Return a minimal reviewed patch.",
		CurrentState: workflow.StateReady, RequiresApproval: true, ApprovalStatus: "approved",
	}}
}

func configuredService(repo Repository, lookup WorkflowLookup, client *http.Client) Service {
	return WithModelMaintenance(NewService(repo, lookup, Config{
		Enabled: true, RunnerURL: "http://miniswe-runner:8080", Token: "a-separate-local-token", Workspaces: []string{"hai-source"}, Timeout: time.Minute,
	}, client), &miniSWEGateStub{})
}

func TestProposalRequiresApprovedReadyWorkflowBeforeCallingRunner(t *testing.T) {
	owner := "owner@example.test"
	record := approvedWorkflow(owner)
	record.Item.ApprovalStatus = "pending"
	repo := &repositoryStub{}
	called := false
	service := configuredService(repo, workflowLookupStub{record: record}, &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, nil
	})})
	proposal, err := service.ProposePatch(context.Background(), owner, record.Item.ID, "hai-source")
	if !errors.Is(err, ErrApprovalRequired) || proposal != nil || called || len(repo.created) != 0 {
		t.Fatalf("proposal=%#v err=%v called=%v jobs=%d", proposal, err, called, len(repo.created))
	}
}

func TestProposalSendsOnlyServerDerivedWorkflowTaskAndDoesNotPersistDiff(t *testing.T) {
	owner := "owner@example.test"
	record := approvedWorkflow(owner)
	diff := "--- a/parser.go\n+++ b/parser.go\n@@\n-before\n+after\n"
	digest := Digest(diff)
	repo := &repositoryStub{}
	service := configuredService(repo, workflowLookupStub{record: record}, &http.Client{Transport: roundTripper(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodGet && request.URL.Path == "/healthz" {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"status":"ok","configured":true,"modelId":"qwen2.5:7b","modelEndpoint":"http://ollama-miniswe:11434"}`))}, nil
		}
		if request.Method != http.MethodPost || request.URL.String() != "http://miniswe-runner:8080/v1/propose-patch" || request.Header.Get("X-HAI-MiniSWE-Token") != "a-separate-local-token" {
			t.Fatalf("unexpected runner request: %s %s", request.Method, request.URL)
		}
		var payload map[string]string
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["workspaceId"] != "hai-source" || !strings.Contains(payload["task"], "Fix the local parser") || !strings.Contains(payload["task"], "minimal reviewed patch") {
			t.Fatalf("payload=%#v", payload)
		}
		body := `{"status":"completed","summary":"One file proposed.","diff":` + jsonString(diff) + `,"diffDigest":"` + digest + `","changedFiles":1,"truncated":false}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})})
	proposal, err := service.ProposePatch(context.Background(), owner, record.Item.ID, "hai-source")
	if err != nil || proposal == nil || proposal.Diff != diff || proposal.DiffDigest != digest {
		t.Fatalf("proposal=%#v err=%v", proposal, err)
	}
	if len(repo.saved) != 1 || repo.saved[0].DiffDigest != digest || repo.saved[0].Summary == "" {
		t.Fatalf("saved=%#v", repo.saved)
	}
	encoded, _ := json.Marshal(repo.saved[0])
	if strings.Contains(string(encoded), "after") || strings.Contains(string(encoded), "before") {
		t.Fatalf("persisted record must not contain diff content: %s", encoded)
	}
}

func TestProposalLinksOnlyOpaqueReviewSignalWhenWorkflowSupportsIt(t *testing.T) {
	owner := "owner@example.test"
	record := approvedWorkflow(owner)
	diff := "--- a/parser.go\n+++ b/parser.go\n@@\n-before\n+after\n"
	linker := &workflowPatchLinkerStub{workflowLookupStub: workflowLookupStub{record: record}}
	service := configuredService(&repositoryStub{}, linker, &http.Client{Transport: roundTripper(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodGet && request.URL.Path == "/healthz" {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"status":"ok","configured":true,"modelId":"qwen2.5:7b","modelEndpoint":"http://ollama-miniswe:11434"}`))}, nil
		}
		body := `{"status":"completed","summary":"One file proposed.","diff":` + jsonString(diff) + `,"diffDigest":"` + Digest(diff) + `","changedFiles":1,"truncated":false}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})})
	proposal, err := service.ProposePatch(context.Background(), owner, record.Item.ID, "hai-source")
	if err != nil || proposal == nil || proposal.WorkflowLinkStatus != "linked_review_signal" || linker.owner != owner || linker.workflowID != record.Item.ID.String() || linker.digest != Digest(diff) || linker.changedFiles != 1 {
		t.Fatalf("proposal=%#v linker=%#v err=%v", proposal, linker, err)
	}
	if strings.Contains(linker.digest, "after") {
		t.Fatalf("workflow link must receive only a digest, got %q", linker.digest)
	}
	linker.err = errors.New("workflow not found")
	proposal, err = service.ProposePatch(context.Background(), owner, record.Item.ID, "hai-source")
	if err != nil || proposal == nil || proposal.WorkflowLinkStatus != "link_failed" || proposal.WorkflowLinkError == "" || proposal.Diff != diff {
		t.Fatalf("link failure must preserve response-only diff: proposal=%#v err=%v", proposal, err)
	}
}

func TestProposalDoesNotCreateJobWhenMaintenanceCannotAdmitModel(t *testing.T) {
	owner := "owner@example.test"
	record := approvedWorkflow(owner)
	repo := &repositoryStub{}
	gate := &miniSWEGateStub{err: errors.New("daily refresh failed")}
	service := WithModelMaintenance(NewService(repo, workflowLookupStub{record: record}, Config{
		Enabled: true, RunnerURL: "http://miniswe-runner:8080", Token: "a-separate-local-token", Workspaces: []string{"hai-source"}, Timeout: time.Minute,
	}, &http.Client{Transport: roundTripper(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || request.URL.Path != "/healthz" {
			t.Fatalf("patch proposal started before maintenance completed: %s %s", request.Method, request.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"status":"ok","configured":true,"modelId":"qwen2.5:7b","modelEndpoint":"http://ollama-miniswe:11434"}`))}, nil
	})}), gate)

	proposal, err := service.ProposePatch(context.Background(), owner, record.Item.ID, "hai-source")
	if !errors.Is(err, ErrUnavailable) || proposal != nil {
		t.Fatalf("proposal=%#v err=%v", proposal, err)
	}
	if len(repo.created) != 0 || len(repo.saved) != 0 {
		t.Fatalf("maintenance-blocked proposal must not create a job: created=%d saved=%d", len(repo.created), len(repo.saved))
	}
	if gate.endpoint != "http://ollama-miniswe:11434" || gate.modelID != "qwen2.5:7b" {
		t.Fatalf("unexpected maintenance gate call: %#v", gate)
	}
}

func TestProposalFailsClosedWithoutMaintenanceGateBeforeRunnerOrPersistence(t *testing.T) {
	owner := "owner@example.test"
	record := approvedWorkflow(owner)
	repo := &repositoryStub{}
	called := false
	service := NewService(repo, workflowLookupStub{record: record}, Config{
		Enabled: true, RunnerURL: "http://miniswe-runner:8080", Token: "a-separate-local-token", Workspaces: []string{"hai-source"}, Timeout: time.Minute,
	}, &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("runner must not be contacted without maintenance")
	})})

	proposal, err := service.ProposePatch(context.Background(), owner, record.Item.ID, "hai-source")
	if !errors.Is(err, ErrUnavailable) || proposal != nil || called || len(repo.created) != 0 || len(repo.saved) != 0 {
		t.Fatalf("missing maintenance gate must block before runner and persistence: proposal=%#v err=%v called=%v created=%d saved=%d", proposal, err, called, len(repo.created), len(repo.saved))
	}
}

func TestProposalFailsClosedWhenTheRunnerReturnsTruncatedDiff(t *testing.T) {
	owner := "owner@example.test"
	record := approvedWorkflow(owner)
	diff := "--- a/parser.go\n+++ b/parser.go\n@@\n-before\n+after\n"
	repo := &repositoryStub{}
	service := configuredService(repo, workflowLookupStub{record: record}, &http.Client{Transport: roundTripper(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodGet && request.URL.Path == "/healthz" {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"status":"ok","configured":true,"modelId":"qwen2.5:7b","modelEndpoint":"http://ollama-miniswe:11434"}`))}, nil
		}
		body := `{"status":"completed","summary":"Output was truncated.","diff":` + jsonString(diff) + `,"diffDigest":"` + Digest(diff) + `","changedFiles":1,"truncated":true}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})})
	proposal, err := service.ProposePatch(context.Background(), owner, record.Item.ID, "hai-source")
	if !errors.Is(err, ErrUnavailable) || proposal == nil || proposal.Diff != "" {
		t.Fatalf("proposal=%#v err=%v", proposal, err)
	}
	if len(repo.saved) != 1 || repo.saved[0].Status != "failed" || repo.saved[0].DiffDigest != "" || repo.saved[0].ChangedFiles != 0 {
		t.Fatalf("truncated output must not become a stored review artifact: %#v", repo.saved)
	}
}

func TestStatusRejectsInvalidSnapshotConfigAndRemoteRunner(t *testing.T) {
	repo := &repositoryStub{}
	lookup := workflowLookupStub{}
	service := NewService(repo, lookup, Config{Enabled: true, RunnerURL: "https://example.com", Token: "a-separate-local-token", Workspaces: []string{"approved"}}, nil)
	if service.Status().Configured {
		t.Fatal("remote runner URL must not configure mini-SWE")
	}
	service = NewService(repo, lookup, Config{Enabled: true, RunnerURL: "http://miniswe-runner:8080", Token: "a-separate-local-token", Workspaces: []string{"approved", "approved"}}, nil)
	if service.Status().Configured {
		t.Fatal("duplicate workspace configuration must not configure mini-SWE")
	}
}

func TestStatusRejectsMultipleSnapshotRoots(t *testing.T) {
	service := NewService(&repositoryStub{}, workflowLookupStub{}, Config{Enabled: true, RunnerURL: "http://miniswe-runner:8080", Token: "a-separate-local-token", Workspaces: []string{"zeta", "alpha"}}, nil)
	if service.Status().Configured {
		t.Fatalf("multiple source snapshots must not configure the disposable worker: %#v", service.Status())
	}
}

func Digest(value string) string {
	// The production service independently verifies this digest before accepting
	// a response. Keeping the test helper small makes the boundary explicit.
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func jsonString(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
