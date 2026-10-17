package automation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestApprovalProofBindsActionAndConsumesOnce(t *testing.T) {
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	service := newApprovalProofTestService(t, func() time.Time { return now })
	automationID := uuid.New()
	digest := approvalTestDigest("approved action")
	sourceID := "task-review:11111111-1111-4111-8111-111111111111"

	proof, err := service.Issue(ApprovalProofIssueRequest{
		OwnerIdentity:    "alice",
		AutomationID:     automationID,
		ActionDigest:     digest,
		Scope:            ApprovalScopeScript,
		ApprovalSourceID: sourceID,
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	expected := ApprovalProofExpectation{
		OwnerIdentity:    "alice",
		AutomationID:     automationID,
		ActionDigest:     digest,
		Scope:            ApprovalScopeScript,
		ApprovalSourceID: sourceID,
	}
	if err := service.VerifyAndConsume(context.Background(), proof, expected); err != nil {
		t.Fatalf("VerifyAndConsume: %v", err)
	}
	if err := service.VerifyAndConsume(context.Background(), proof, expected); !errors.Is(err, ErrApprovalProofConsumed) {
		t.Fatalf("replay error = %v, want ErrApprovalProofConsumed", err)
	}
}

func TestApprovalDecisionCanIssueOnlyOneProofAcrossServiceInstances(t *testing.T) {
	now := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	secret := []byte("0123456789abcdef0123456789abcdef")
	store := &memoryApprovalProofConsumptionStore{consumed: map[string]time.Time{}}
	issuer, err := NewApprovalProofService(secret, store, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := NewApprovalProofService(secret, store, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	request := ApprovalProofIssueRequest{
		OwnerIdentity:    "alice",
		AutomationID:     uuid.New(),
		ActionDigest:     approvalTestDigest("one decision, one proof"),
		Scope:            ApprovalScopeScript,
		ApprovalSourceID: "task-review:" + uuid.NewString(),
	}
	if _, err := issuer.Issue(request); err != nil {
		t.Fatalf("first Issue: %v", err)
	}
	if _, err := restarted.Issue(request); !errors.Is(err, ErrApprovalProofAlreadyIssued) {
		t.Fatalf("second Issue error = %v, want ErrApprovalProofAlreadyIssued", err)
	}
}

func TestConcurrentApprovalProofIssuanceReservesSourceOnceAndIsOwnerScoped(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	store := &memoryApprovalProofConsumptionStore{consumed: map[string]time.Time{}}
	var services []ApprovalProofService
	for range 12 {
		service, err := NewApprovalProofService(secret, store, time.Now)
		if err != nil {
			t.Fatal(err)
		}
		services = append(services, service)
	}
	request := ApprovalProofIssueRequest{
		OwnerIdentity:    "alice",
		AutomationID:     uuid.New(),
		ActionDigest:     approvalTestDigest("concurrent issue"),
		Scope:            ApprovalScopeDocker,
		ApprovalSourceID: "workflow-decision:" + uuid.NewString(),
	}
	var successes atomic.Int32
	var rejected atomic.Int32
	var unexpected atomic.Int32
	start := make(chan struct{})
	var wait sync.WaitGroup
	for _, service := range services {
		wait.Add(1)
		go func(service ApprovalProofService) {
			defer wait.Done()
			<-start
			_, err := service.Issue(request)
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, ErrApprovalProofAlreadyIssued):
				rejected.Add(1)
			default:
				unexpected.Add(1)
			}
		}(service)
	}
	close(start)
	wait.Wait()
	if successes.Load() != 1 || rejected.Load() != 11 || unexpected.Load() != 0 {
		t.Fatalf("concurrent issuance successes=%d rejected=%d unexpected=%d, want 1/11/0", successes.Load(), rejected.Load(), unexpected.Load())
	}
	otherOwner := request
	otherOwner.OwnerIdentity = "bob"
	if _, err := services[0].Issue(otherOwner); err != nil {
		t.Fatalf("same source for separate owner should have independent scope: %v", err)
	}
}

func TestApprovalPolicySnapshotBindsScriptExecutionLimitsAndPin(t *testing.T) {
	t.Setenv("AUTOMATION_SCRIPT_TIMEOUT_SECONDS", "17")
	t.Setenv("AUTOMATION_SCRIPT_OUTPUT_LIMIT_BYTES", "2048")
	t.Setenv("AUTOMATION_SCRIPT_SHA256_ALLOWLIST", "reviewed.sh="+strings.Repeat("a", 64))

	snapshot := strings.Join(approvalPolicySnapshot(), "\n")
	for _, expected := range []string{
		"AUTOMATION_SCRIPT_TIMEOUT_SECONDS=17",
		"AUTOMATION_SCRIPT_OUTPUT_LIMIT_BYTES=2048",
		"AUTOMATION_SCRIPT_SHA256_ALLOWLIST=reviewed.sh=" + strings.Repeat("a", 64),
	} {
		if !strings.Contains(snapshot, expected) {
			t.Fatalf("approval policy snapshot does not bind %q: %s", expected, snapshot)
		}
	}
}

func TestApprovalProofAllowsOnlyOneConcurrentConsumer(t *testing.T) {
	service := newApprovalProofTestService(t, time.Now)
	request := ApprovalProofIssueRequest{
		OwnerIdentity:    "alice",
		AutomationID:     uuid.New(),
		ActionDigest:     approvalTestDigest("single consumer"),
		Scope:            ApprovalScopeDocker,
		ApprovalSourceID: "task-review:11111111-1111-4111-8111-111111111111",
	}
	proof, err := service.Issue(request)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	expected := ApprovalProofExpectation{
		OwnerIdentity:    request.OwnerIdentity,
		AutomationID:     request.AutomationID,
		ActionDigest:     request.ActionDigest,
		Scope:            request.Scope,
		ApprovalSourceID: request.ApprovalSourceID,
	}

	var successes atomic.Int32
	var unexpected atomic.Int32
	var wait sync.WaitGroup
	for i := 0; i < 16; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			err := service.VerifyAndConsume(context.Background(), proof, expected)
			if err == nil {
				successes.Add(1)
				return
			}
			if !errors.Is(err, ErrApprovalProofConsumed) {
				unexpected.Add(1)
			}
		}()
	}
	wait.Wait()
	if successes.Load() != 1 || unexpected.Load() != 0 {
		t.Fatalf("concurrent consumption successes=%d unexpectedErrors=%d, want 1/0", successes.Load(), unexpected.Load())
	}
}

func TestApprovalProofCanBeIssuedAndConsumedAcrossServiceInstances(t *testing.T) {
	now := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	secret := make([]byte, 32)
	for index := range secret {
		secret[index] = byte(index + 1)
	}
	store := &memoryApprovalProofConsumptionStore{consumed: map[string]time.Time{}}
	issuer, err := NewApprovalProofService(secret, store, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewApprovalProofService issuer: %v", err)
	}
	consumer, err := NewApprovalProofService(secret, store, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewApprovalProofService consumer: %v", err)
	}
	request := ApprovalProofIssueRequest{
		OwnerIdentity:    "alice",
		AutomationID:     uuid.New(),
		ActionDigest:     approvalTestDigest("cross-instance action"),
		Scope:            ApprovalScopeAgentRuntime,
		ApprovalSourceID: "workflow-decision:" + uuid.NewString(),
	}
	proof, err := issuer.Issue(request)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	expected := ApprovalProofExpectation{
		OwnerIdentity:    request.OwnerIdentity,
		AutomationID:     request.AutomationID,
		ActionDigest:     request.ActionDigest,
		Scope:            request.Scope,
		ApprovalSourceID: request.ApprovalSourceID,
	}
	if err := consumer.VerifyAndConsume(context.Background(), proof, expected); err != nil {
		t.Fatalf("cross-instance VerifyAndConsume: %v", err)
	}
	restarted, err := NewApprovalProofService(secret, store, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewApprovalProofService restart: %v", err)
	}
	if err := restarted.VerifyAndConsume(context.Background(), proof, expected); !errors.Is(err, ErrApprovalProofConsumed) {
		t.Fatalf("restart replay error = %v, want ErrApprovalProofConsumed", err)
	}
}

func TestApprovalProofRejectsDifferentInstanceSigningKeyWithoutConsuming(t *testing.T) {
	now := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	validKey := []byte("0123456789abcdef0123456789abcdef")
	wrongKey := []byte("abcdef0123456789abcdef0123456789")
	store := &memoryApprovalProofConsumptionStore{consumed: map[string]time.Time{}}
	issuer, err := NewApprovalProofService(validKey, store, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	wrongConsumer, err := NewApprovalProofService(wrongKey, store, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	validConsumer, err := NewApprovalProofService(validKey, store, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	request := ApprovalProofIssueRequest{
		OwnerIdentity:    "alice",
		AutomationID:     uuid.New(),
		ActionDigest:     approvalTestDigest("key mismatch"),
		Scope:            ApprovalScopeScript,
		ApprovalSourceID: "task-review:" + uuid.NewString(),
	}
	proof, err := issuer.Issue(request)
	if err != nil {
		t.Fatal(err)
	}
	expected := ApprovalProofExpectation{
		OwnerIdentity: request.OwnerIdentity, AutomationID: request.AutomationID,
		ActionDigest: request.ActionDigest, Scope: request.Scope,
		ApprovalSourceID: request.ApprovalSourceID,
	}
	if err := wrongConsumer.VerifyAndConsume(context.Background(), proof, expected); !errors.Is(err, ErrApprovalProofInvalid) {
		t.Fatalf("wrong-key error = %v, want ErrApprovalProofInvalid", err)
	}
	if err := validConsumer.VerifyAndConsume(context.Background(), proof, expected); err != nil {
		t.Fatalf("wrong-key attempt consumed proof: %v", err)
	}
}

func TestApprovalProofFailsClosedOnBindingMismatch(t *testing.T) {
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	automationID := uuid.New()
	digest := approvalTestDigest("approved action")
	base := ApprovalProofExpectation{
		OwnerIdentity:    "alice",
		AutomationID:     automationID,
		ActionDigest:     digest,
		Scope:            ApprovalScopeScript,
		ApprovalSourceID: "task-review:11111111-1111-4111-8111-111111111111",
	}
	tests := []struct {
		name   string
		mutate func(*ApprovalProofExpectation)
	}{
		{name: "owner", mutate: func(value *ApprovalProofExpectation) { value.OwnerIdentity = "bob" }},
		{name: "automation", mutate: func(value *ApprovalProofExpectation) { value.AutomationID = uuid.New() }},
		{name: "action digest", mutate: func(value *ApprovalProofExpectation) { value.ActionDigest = approvalTestDigest("different action") }},
		{name: "scope", mutate: func(value *ApprovalProofExpectation) { value.Scope = ApprovalScopeDocker }},
		{name: "approval source", mutate: func(value *ApprovalProofExpectation) {
			value.ApprovalSourceID = "task-review:22222222-2222-4222-8222-222222222222"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := newApprovalProofTestService(t, func() time.Time { return now })
			proof, err := service.Issue(ApprovalProofIssueRequest{
				OwnerIdentity:    base.OwnerIdentity,
				AutomationID:     base.AutomationID,
				ActionDigest:     base.ActionDigest,
				Scope:            base.Scope,
				ApprovalSourceID: base.ApprovalSourceID,
			})
			if err != nil {
				t.Fatalf("Issue: %v", err)
			}
			expected := base
			test.mutate(&expected)
			if err := service.VerifyAndConsume(context.Background(), proof, expected); !errors.Is(err, ErrApprovalProofInvalid) {
				t.Fatalf("mismatch error = %v, want ErrApprovalProofInvalid", err)
			}
			if err := service.VerifyAndConsume(context.Background(), proof, base); err != nil {
				t.Fatalf("mismatch consumed proof or corrupted valid binding: %v", err)
			}
		})
	}
}

func TestApprovalProofRejectsTamperingAndExpiry(t *testing.T) {
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	service := newApprovalProofTestService(t, func() time.Time { return now })
	request := ApprovalProofIssueRequest{
		OwnerIdentity:    "alice",
		AutomationID:     uuid.New(),
		ActionDigest:     approvalTestDigest("approved action"),
		Scope:            ApprovalScopeAPIMutate,
		ApprovalSourceID: "workflow-decision:33333333-3333-4333-8333-333333333333",
		TTL:              time.Minute,
	}
	expected := ApprovalProofExpectation{
		OwnerIdentity:    request.OwnerIdentity,
		AutomationID:     request.AutomationID,
		ActionDigest:     request.ActionDigest,
		Scope:            request.Scope,
		ApprovalSourceID: request.ApprovalSourceID,
	}

	tampered, err := service.Issue(request)
	if err != nil {
		t.Fatalf("Issue tampered proof: %v", err)
	}
	tampered.Signature = "tampered"
	if err := service.VerifyAndConsume(context.Background(), tampered, expected); !errors.Is(err, ErrApprovalProofInvalid) {
		t.Fatalf("tamper error = %v, want ErrApprovalProofInvalid", err)
	}

	request.ApprovalSourceID = "workflow-decision:" + uuid.NewString()
	expected.ApprovalSourceID = request.ApprovalSourceID
	expired, err := service.Issue(request)
	if err != nil {
		t.Fatalf("Issue expiring proof: %v", err)
	}
	now = now.Add(time.Minute)
	if err := service.VerifyAndConsume(context.Background(), expired, expected); !errors.Is(err, ErrApprovalProofExpired) {
		t.Fatalf("expiry error = %v, want ErrApprovalProofExpired", err)
	}
}

func TestApprovalProofRequiresCompleteTrustedBinding(t *testing.T) {
	service := newApprovalProofTestService(t, time.Now)
	valid := ApprovalProofIssueRequest{
		OwnerIdentity:    "alice",
		AutomationID:     uuid.New(),
		ActionDigest:     approvalTestDigest("approved action"),
		Scope:            ApprovalScopeDocker,
		ApprovalSourceID: "task-review:11111111-1111-4111-8111-111111111111",
	}
	tests := []struct {
		name   string
		mutate func(*ApprovalProofIssueRequest)
	}{
		{name: "owner", mutate: func(value *ApprovalProofIssueRequest) { value.OwnerIdentity = "" }},
		{name: "automation", mutate: func(value *ApprovalProofIssueRequest) { value.AutomationID = uuid.Nil }},
		{name: "digest", mutate: func(value *ApprovalProofIssueRequest) { value.ActionDigest = "" }},
		{name: "scope", mutate: func(value *ApprovalProofIssueRequest) { value.Scope = "" }},
		{name: "source", mutate: func(value *ApprovalProofIssueRequest) { value.ApprovalSourceID = "" }},
		{name: "unsupported source", mutate: func(value *ApprovalProofIssueRequest) { value.ApprovalSourceID = "manual:" + uuid.NewString() }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := valid
			test.mutate(&request)
			if _, err := service.Issue(request); err == nil {
				t.Fatalf("expected incomplete %s binding to be rejected", test.name)
			}
		})
	}
}

func newApprovalProofTestService(t *testing.T, now func() time.Time) ApprovalProofService {
	t.Helper()
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = byte(i + 1)
	}
	service, err := NewInMemoryApprovalProofService(secret, now)
	if err != nil {
		t.Fatalf("NewInMemoryApprovalProofService: %v", err)
	}
	return service
}

func approvalTestDigest(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
