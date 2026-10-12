package task

import (
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"automation-hub-backend/internal/automation"
	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

type readOnlyRuntimeTestTransport struct{ calls int }

func (t *readOnlyRuntimeTestTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.calls++
	return nil, errors.New("unexpected HTTP dispatch")
}

type readOnlyRuntimeTestEffects struct {
	launchCalls     int
	launchTaskCalls int
	proofCalls      int
	decisionCalls   int
	approvalCalls   int
	transport       readOnlyRuntimeTestTransport
}

func (e *readOnlyRuntimeTestEffects) Launch(uuid.UUID) (*automation.LaunchResult, error) {
	e.launchCalls++
	_, err := e.transport.RoundTrip(nil)
	return nil, err
}

func (e *readOnlyRuntimeTestEffects) LaunchTask(uuid.UUID, automation.TaskLaunchRequest) (*automation.LaunchResult, error) {
	e.launchTaskCalls++
	_, err := e.transport.RoundTrip(nil)
	return nil, err
}

func (e *readOnlyRuntimeTestEffects) IssueApprovalProof(uuid.UUID, automation.TaskApprovalProofRequest) (*automation.ApprovalProof, error) {
	e.proofCalls++
	return nil, errors.New("unexpected proof issuance")
}

func (e *readOnlyRuntimeTestEffects) RecordApprovalDecision(uuid.UUID, automation.TaskApprovalDecisionRequest) error {
	e.decisionCalls++
	return errors.New("unexpected approval write")
}

func (e *readOnlyRuntimeTestEffects) ActionApprovalRequired(uuid.UUID) (bool, error) {
	e.approvalCalls++
	return true, errors.New("unexpected approval inspection")
}

func (e *readOnlyRuntimeTestEffects) assertUnused(t *testing.T) {
	t.Helper()
	if e.launchCalls != 0 || e.launchTaskCalls != 0 || e.proofCalls != 0 || e.decisionCalls != 0 || e.approvalCalls != 0 || e.transport.calls != 0 {
		t.Fatalf("metadata inspection reached execution/approval/HTTP: %#v", e)
	}
}

type readOnlyRuntimeTestReader struct {
	readOnlyRuntimeTestEffects
	record    *models.Automation
	err       error
	readCalls int
	requested uuid.UUID
}

func (r *readOnlyRuntimeTestReader) FindByID(id uuid.UUID) (*models.Automation, error) {
	r.readCalls++
	r.requested = id
	return r.record, r.err
}

func TestReadOnlyRuntimeInspectorClassifiesStoredTargetsWithoutExecution(t *testing.T) {
	cases := []struct {
		name       string
		launchType string
		target     string
		want       bool
	}{
		{"get", "api", "GET https://example.invalid/health", true},
		{"head", "api", "HEAD http://localhost:8080/health", true},
		{"lowercase-method", "api", "get https://example.invalid/health", true},
		{"parser-whitespace", "api", " \tHead \n https://example.invalid/health \t", true},
		{"userinfo-and-query", "api", "GET https://synthetic-user:synthetic-password@example.invalid/health?token=synthetic-token&verbose=true", true},
		{"ipv6", "api", "HEAD http://[::1]:8080/health", true},
		{"encoded-path", "api", "GET https://example.invalid/a%20b?view=table", true},
		{"methodless-default-is-post", "api", "https://example.invalid/health", false},
		{"post", "api", "POST https://example.invalid/health", false},
		{"delete", "api", "DELETE https://example.invalid/health", false},
		{"put", "api", "PUT https://example.invalid/health", false},
		{"patch", "api", "PATCH https://example.invalid/health", false},
		{"options", "api", "OPTIONS https://example.invalid/health", false},
		{"unknown-method", "api", "READ https://example.invalid/health", false},
		{"script", "script", "GET https://example.invalid/health", false},
		{"browser", "browser_url", "GET https://example.invalid/health", false},
		{"runtime", "agent_runtime", "GET https://example.invalid/health", false},
		{"missing-type", "", "GET https://example.invalid/health", false},
		{"uppercase-type", "API", "GET https://example.invalid/health", false},
		{"padded-type", " api ", "GET https://example.invalid/health", false},
		{"empty", "api", "", false},
		{"missing-url", "api", "GET", false},
		{"relative", "api", "GET /health", false},
		{"scheme-relative", "api", "GET //example.invalid/health", false},
		{"ftp", "api", "GET ftp://example.invalid/health", false},
		{"file", "api", "GET file:///health", false},
		{"javascript", "api", "GET javascript:alert(1)", false},
		{"opaque", "api", "GET http:example.invalid/health", false},
		{"missing-host", "api", "GET https:///health", false},
		{"empty-hostname", "api", "GET http://:8080/health", false},
		{"invalid-host", "api", "GET https://bad host/health", false},
		{"bad-port", "api", "GET https://example.invalid:abc/health", false},
		{"out-of-range-port", "api", "GET https://example.invalid:65536/health", false},
		{"zero-port", "api", "GET https://example.invalid:0/health", false},
		{"empty-port", "api", "GET https://example.invalid:/health", false},
		{"invalid-escape", "api", "GET https://example.invalid/%zz", false},
		{"extra-input", "api", "GET https://example.invalid/health DELETE", false},
		{"query-control", "api", "GET https://example.invalid/health?x=ok\r\nPOST", false},
		{"query-whitespace", "api", "GET https://example.invalid/health?x=two words", false},
		{"invalid-userinfo", "api", "GET https://synthetic-user:%zz@example.invalid/health", false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			id := uuid.New()
			raw := &models.Automation{ID: id, LaunchType: test.launchType, LaunchTarget: test.target}
			original := *raw
			reader := &readOnlyRuntimeTestReader{record: raw}
			var inspector ReadOnlyRuntimeInspector = NewAutomationToolExecutor(reader)
			got, err := inspector.IsReadOnlyRuntime(" " + id.String() + " ")
			if err != nil || got != test.want {
				t.Fatalf("metadata=%v err=%v, want %v", got, err, test.want)
			}
			if reader.readCalls != 1 || reader.requested != id || !reflect.DeepEqual(*raw, original) {
				t.Fatal("metadata lookup used a different ID or mutated raw configuration")
			}
			reader.assertUnused(t)
		})
	}
}

func TestReadOnlyRuntimeInspectorRejectsInvalidIDsBeforeReading(t *testing.T) {
	for _, invalid := range []string{"", "not-a-uuid", uuid.Nil.String(), "GET https://synthetic-user:synthetic-password@example.invalid/?token=synthetic-token"} {
		reader := &readOnlyRuntimeTestReader{}
		got, err := NewAutomationToolExecutor(reader).IsReadOnlyRuntime(invalid)
		if got || err == nil || reader.readCalls != 0 {
			t.Fatalf("invalid ID admitted/read: result=%v error=%v reads=%d", got, err, reader.readCalls)
		}
		assertReadOnlyRuntimeErrorHasNoCredentials(t, err)
		reader.assertUnused(t)
	}
}

func TestReadOnlyRuntimeInspectorFailsClosedForMissingOrMismatchedRecords(t *testing.T) {
	id := uuid.New()
	for _, record := range []*models.Automation{nil, {ID: uuid.New(), LaunchType: "api", LaunchTarget: "GET https://example.invalid/health"}, {ID: uuid.Nil, LaunchType: "api", LaunchTarget: "GET https://example.invalid/health"}} {
		reader := &readOnlyRuntimeTestReader{record: record}
		got, err := NewAutomationToolExecutor(reader).IsReadOnlyRuntime(id.String())
		if got || err != nil || reader.readCalls != 1 {
			t.Fatalf("missing/mismatched record admitted: %v %v", got, err)
		}
		reader.assertUnused(t)
	}
}

func TestReadOnlyRuntimeInspectorLookupErrorsDoNotLeakCredentials(t *testing.T) {
	id := uuid.New()
	reader := &readOnlyRuntimeTestReader{
		record: &models.Automation{ID: id, LaunchType: "api", LaunchTarget: "GET https://example.invalid/health"},
		err:    errors.New("DB failure for https://synthetic-user:synthetic-password@example.invalid/?token=synthetic-token"),
	}
	got, err := NewAutomationToolExecutor(reader).IsReadOnlyRuntime(id.String())
	if got || err == nil || reader.readCalls != 1 {
		t.Fatalf("DB error did not fail closed: %v %v", got, err)
	}
	assertReadOnlyRuntimeErrorHasNoCredentials(t, err)
	reader.assertUnused(t)
}

func TestReadOnlyRuntimeInspectorUnavailableReadersDoNotPanic(t *testing.T) {
	var nilExecutor *AutomationToolExecutor
	var nilReader *readOnlyRuntimeTestReader
	unsupported := &readOnlyRuntimeTestEffects{}
	for _, executor := range []*AutomationToolExecutor{nilExecutor, {}, NewAutomationToolExecutor(nil), NewAutomationToolExecutor(nilReader), NewAutomationToolExecutor(unsupported)} {
		got, err := executor.IsReadOnlyRuntime(uuid.NewString())
		if got || err != nil {
			t.Fatalf("unavailable reader admitted: %v %v", got, err)
		}
	}
	unsupported.assertUnused(t)
}

func TestReadOnlyRuntimeInspectorRefreshesCurrentServerConfiguration(t *testing.T) {
	id := uuid.New()
	reader := &readOnlyRuntimeTestReader{record: &models.Automation{ID: id, LaunchType: "api", LaunchTarget: "GET https://example.invalid/health"}}
	executor := NewAutomationToolExecutor(reader)
	if got, err := executor.IsReadOnlyRuntime(id.String()); !got || err != nil {
		t.Fatalf("GET metadata=%v err=%v", got, err)
	}
	reader.record.LaunchTarget = "POST https://example.invalid/health"
	if got, err := executor.IsReadOnlyRuntime(id.String()); got || err != nil {
		t.Fatalf("changed POST metadata=%v err=%v", got, err)
	}
	if reader.readCalls != 2 {
		t.Fatal("inspector cached stale configuration")
	}
	reader.assertUnused(t)
}

func assertReadOnlyRuntimeErrorHasNoCredentials(t *testing.T, err error) {
	t.Helper()
	for _, secret := range []string{"synthetic-user", "synthetic-password", "synthetic-token", "https://", "DB failure"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("metadata error leaked %q", secret)
		}
	}
}
