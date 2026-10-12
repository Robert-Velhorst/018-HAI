package executionbroker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

const marker = "HAI_LOCAL_SAFE_WORKER_OK"

func TestLocalSafeWorkerSuccess(t *testing.T) {
	ws := t.TempDir()
	verifier := newTestAuthorizationVerifier()
	b := newTestVerifierBroker(ws, verifier)
	in := authorizedInput(t, ws, "safe-worker-result.txt", marker)
	res, err := b.ExecuteLocalSafeWorker(context.Background(), in)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !res.OK || !res.Verification.Passed {
		t.Fatalf("safe worker should pass verification: %+v", res.Verification)
	}
	if !res.Verification.FileExists || !res.Verification.HashMatches || !res.Verification.MarkerFound || !res.Verification.InsideWorkspace {
		t.Fatalf("verification postconditions not met: %+v", res.Verification)
	}
	if res.Output.BoundedOutput != marker {
		t.Fatalf("bounded output = %q", res.Output.BoundedOutput)
	}
}

func TestLocalSafeWorkerRejectsTraversalAndSeparatorsAndAbsolute(t *testing.T) {
	ws := t.TempDir()
	w := NewAuthorizedLocalSafeWorker(ws, newTestAuthorizationVerifier())
	for _, name := range []string{"../escape.txt", "sub/dir.txt", "/etc/passwd", "..", "a/../b.txt"} {
		in := SafeWorkerInput{
			ArtifactName: name,
			Marker:       marker,
			Authorization: AuthorizationBinding{
				OwnerIdentity: "robert",
				TaskID:        "task-invalid-path",
				Action:        LocalSafeWorkerAction,
			},
		}
		if _, err := w.Run(context.Background(), in); err == nil {
			t.Fatalf("artifactName %q must be rejected", name)
		}
	}
}

func TestLocalSafeWorkerRequiresMarker(t *testing.T) {
	w := NewAuthorizedLocalSafeWorker(t.TempDir(), newTestAuthorizationVerifier())
	if _, err := w.Run(context.Background(), SafeWorkerInput{ArtifactName: "x.txt", Marker: ""}); err == nil {
		t.Fatalf("missing marker must be rejected")
	}
}

func TestLocalSafeWorkerRejectsOversizedMarkerWithoutCreatingArtifact(t *testing.T) {
	workspace := t.TempDir()
	w := NewAuthorizedLocalSafeWorker(workspace, newTestAuthorizationVerifier())
	input := authorizedInput(t, workspace, "oversized.txt", strings.Repeat("x", maxSafeArtifactBytes+1))

	if _, err := w.Run(context.Background(), input); err == nil {
		t.Fatalf("oversized marker must be rejected")
	}
	if _, err := os.Stat(filepath.Join(workspace, "oversized.txt")); !os.IsNotExist(err) {
		t.Fatalf("oversized marker must not create an artifact, stat error = %v", err)
	}
}

func TestVerifyFailsWhenMarkerAbsent(t *testing.T) {
	ws := t.TempDir()
	w := NewAuthorizedLocalSafeWorker(ws, newTestAuthorizationVerifier())
	in := authorizedInput(t, ws, "x.txt", marker)
	out, err := w.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// Verifying against a different expected marker must fail.
	v := w.Verify(SafeWorkerInput{ArtifactName: "x.txt", Marker: "DIFFERENT_MARKER"}, out)
	if v.Passed {
		t.Fatalf("verification must fail when the expected marker is not in the file")
	}
}

func TestBrokerBlocksRuntimeNotConfigured(t *testing.T) {
	b := NewBroker("") // no workspace configured -> not_configured -> not executable
	if _, err := b.ExecuteLocalSafeWorker(context.Background(), SafeWorkerInput{ArtifactName: "x.txt", Marker: marker}); err == nil {
		t.Fatalf("broker must refuse to execute an unconfigured runtime")
	}
	if b.SafeWorker().HealthCheck(context.Background()).Status.CanExecute() {
		t.Fatalf("unconfigured runtime must not be executable")
	}
}

func TestBrokerBoundsOutput(t *testing.T) {
	ws := t.TempDir()
	w := NewAuthorizedLocalSafeWorker(ws, newTestAuthorizationVerifier())
	big := strings.Repeat("A", maxSafeOutput*2)
	out, err := w.Run(context.Background(), authorizedInput(t, ws, "big.txt", big))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(out.BoundedOutput) > maxSafeOutput {
		t.Fatalf("output must be bounded to %d, got %d", maxSafeOutput, len(out.BoundedOutput))
	}
}

func TestLocalSafeWorkerRejectsOverwriteAndPreservesExistingFile(t *testing.T) {
	ws := t.TempDir()
	target := filepath.Join(ws, "existing.txt")
	const original = "operator-owned content"
	if err := os.WriteFile(target, []byte(original), 0o600); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	w := NewAuthorizedLocalSafeWorker(ws, newTestAuthorizationVerifier())
	if _, err := w.Run(context.Background(), authorizedInput(t, ws, "existing.txt", marker)); err == nil {
		t.Fatalf("worker must reject an existing target")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read existing target: %v", err)
	}
	if string(data) != original {
		t.Fatalf("existing target changed to %q", data)
	}
}

func TestLocalSafeWorkerRejectsSymlinkTargetWithoutChangingOutsideFile(t *testing.T) {
	parent := t.TempDir()
	ws := filepath.Join(parent, "workspace")
	if err := os.Mkdir(ws, 0o755); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	outside := filepath.Join(parent, "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatalf("create outside target: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(ws, "artifact.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	w := NewAuthorizedLocalSafeWorker(ws, newTestAuthorizationVerifier())
	if _, err := w.Run(context.Background(), authorizedInput(t, ws, "artifact.txt", marker)); err == nil {
		t.Fatalf("worker must reject a symlink target")
	}
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != "outside" {
		t.Fatalf("outside file was changed: data=%q err=%v", data, err)
	}
}

func TestLocalSafeWorkerRejectsSymlinkWorkspaceComponent(t *testing.T) {
	parent := t.TempDir()
	outside := filepath.Join(parent, "outside")
	if err := os.MkdirAll(filepath.Join(outside, "workspace"), 0o755); err != nil {
		t.Fatalf("create outside workspace: %v", err)
	}
	link := filepath.Join(parent, "linked")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	workspace := filepath.Join(link, "workspace")
	w := NewAuthorizedLocalSafeWorker(workspace, newTestAuthorizationVerifier())
	if _, err := w.Run(context.Background(), authorizedInput(t, workspace, "artifact.txt", marker)); err == nil {
		t.Fatalf("worker must reject a linked workspace component")
	}
	if _, err := os.Stat(filepath.Join(outside, "workspace", "artifact.txt")); !os.IsNotExist(err) {
		t.Fatalf("worker wrote through linked workspace: %v", err)
	}
}

func TestVerifyDetectsByteIdenticalTargetSubstitution(t *testing.T) {
	ws := t.TempDir()
	w := NewAuthorizedLocalSafeWorker(ws, newTestAuthorizationVerifier())
	in := authorizedInput(t, ws, "artifact.txt", marker)
	out, err := w.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	moved := filepath.Join(ws, "original.txt")
	if err := os.Rename(out.ArtifactPath, moved); err != nil {
		t.Fatalf("move original artifact: %v", err)
	}
	if err := os.WriteFile(out.ArtifactPath, []byte(marker), 0o600); err != nil {
		t.Fatalf("create byte-identical substitute: %v", err)
	}
	if verification := w.Verify(in, out); verification.Passed {
		t.Fatalf("verification must reject a substituted target: %+v", verification)
	}
}

func TestVerifyRejectsOversizedArtifactWithoutReadingItUnbounded(t *testing.T) {
	ws := t.TempDir()
	w := NewAuthorizedLocalSafeWorker(ws, newTestAuthorizationVerifier())
	in := authorizedInput(t, ws, "artifact.txt", marker)
	out, err := w.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	file, err := os.OpenFile(out.ArtifactPath, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		t.Fatalf("open artifact: %v", err)
	}
	if _, err := file.WriteString(strings.Repeat("x", maxSafeArtifactBytes+1)); err != nil {
		_ = file.Close()
		t.Fatalf("expand artifact: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close artifact: %v", err)
	}

	if verification := w.Verify(in, out); verification.Passed {
		t.Fatalf("verification must reject oversized artifacts: %+v", verification)
	}
}

func TestVerifyRejectsSiblingPrefixPath(t *testing.T) {
	parent := t.TempDir()
	ws := filepath.Join(parent, "workspace")
	if err := os.Mkdir(ws, 0o755); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	w := NewAuthorizedLocalSafeWorker(ws, newTestAuthorizationVerifier())
	in := authorizedInput(t, ws, "artifact.txt", marker)
	out, err := w.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	sibling := filepath.Join(parent, "workspace-outside")
	if err := os.Mkdir(sibling, 0o755); err != nil {
		t.Fatalf("create sibling: %v", err)
	}
	out.ArtifactPath = filepath.Join(sibling, in.ArtifactName)
	if err := os.WriteFile(out.ArtifactPath, []byte(marker), 0o600); err != nil {
		t.Fatalf("create sibling artifact: %v", err)
	}
	verification := w.Verify(in, out)
	if verification.Passed || verification.InsideWorkspace {
		t.Fatalf("verification accepted sibling-prefix path: %+v", verification)
	}
}

func TestBrokerWithoutVerifierFailsClosedBeforeCreatingWorkspace(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "not-created")
	in := authorizedInput(t, workspace, "artifact.txt", marker)

	_, err := NewBroker(workspace).ExecuteLocalSafeWorker(context.Background(), in)
	if !errors.Is(err, ErrAuthorizationRequired) {
		t.Fatalf("execute error = %v, want ErrAuthorizationRequired", err)
	}
	assertPathAbsent(t, workspace)
	health := NewBroker(workspace).SafeWorker().HealthCheck(context.Background())
	if health.Status != RuntimeBlocked || health.Status.CanExecute() {
		t.Fatalf("health = %+v, want blocked authorization state", health)
	}
}

func TestDirectWorkerRunCannotBypassAuthorization(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "not-created")
	in := authorizedInput(t, workspace, "artifact.txt", marker)

	_, err := NewLocalSafeWorker(workspace).Run(context.Background(), in)
	if !errors.Is(err, ErrAuthorizationRequired) {
		t.Fatalf("run error = %v, want ErrAuthorizationRequired", err)
	}
	assertPathAbsent(t, workspace)
}

func TestMismatchedEffectBindingFailsBeforeVerifierAndFilesystem(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "not-created")
	verifier := newTestAuthorizationVerifier()
	in := authorizedInput(t, workspace, "artifact.txt", marker)
	in.Marker = "changed after authorization"

	_, err := newTestVerifierBroker(workspace, verifier).
		ExecuteLocalSafeWorker(context.Background(), in)
	if !errors.Is(err, ErrAuthorizationMismatch) {
		t.Fatalf("execute error = %v, want ErrAuthorizationMismatch", err)
	}
	if got := verifier.callCount.Load(); got != 0 {
		t.Fatalf("verifier called %d times for mismatched effect", got)
	}
	assertPathAbsent(t, workspace)
}

func TestVerifierDenialHasZeroFilesystemEffect(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "not-created")
	verifier := newTestAuthorizationVerifier()
	verifier.beforeConsume = func(request AuthorizationVerification) error {
		if _, err := os.Stat(workspace); !os.IsNotExist(err) {
			t.Fatalf("workspace existed before authorization decision: %v", err)
		}
		return errors.New("policy denied")
	}
	in := authorizedInput(t, workspace, "artifact.txt", marker)

	_, err := newTestVerifierBroker(workspace, verifier).
		ExecuteLocalSafeWorker(context.Background(), in)
	if !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("execute error = %v, want ErrAuthorizationDenied", err)
	}
	assertPathAbsent(t, workspace)
}

func TestVerifierReceivesCanonicalNonSecretFinalEffect(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	verifier := newTestAuthorizationVerifier()
	in := authorizedInput(t, workspace, "artifact.txt", marker)
	verifier.beforeConsume = func(request AuthorizationVerification) error {
		if request.Effect.ResourceType != LocalSafeWorkerResourceType ||
			request.Effect.Action != LocalSafeWorkerAction ||
			request.Effect.RuntimeID != LocalSafeWorkerID ||
			request.Effect.EffectDigest != in.Authorization.EffectDigest {
			t.Fatalf("unexpected final effect: %+v", request.Effect)
		}
		if request.Consumer != LocalSafeWorkerID {
			t.Fatalf("consumer = %q", request.Consumer)
		}
		if request.ExecutionTarget != filepath.Join(request.Effect.WorkspaceRoot, in.ArtifactName) {
			t.Fatalf("execution target = %q", request.ExecutionTarget)
		}
		encoded, err := json.Marshal(request.Effect)
		if err != nil {
			t.Fatalf("encode effect: %v", err)
		}
		if strings.Contains(string(encoded), marker) {
			t.Fatal("raw payload leaked into final-effect authorization request")
		}
		return nil
	}

	if _, err := newTestVerifierBroker(workspace, verifier).
		ExecuteLocalSafeWorker(context.Background(), in); err != nil {
		t.Fatalf("execute: %v", err)
	}
}

func TestEmergencyStopIsRecheckedImmediatelyBeforeMutation(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "not-created")
	var emergencyStop atomic.Bool
	emergencyStop.Store(true)
	verifier := newTestAuthorizationVerifier()
	verifier.beforeConsume = func(request AuthorizationVerification) error {
		if _, err := os.Stat(request.Effect.WorkspaceRoot); !os.IsNotExist(err) {
			t.Fatalf("workspace changed before emergency-stop recheck: %v", err)
		}
		if emergencyStop.Load() {
			return errors.New("emergency stop is active")
		}
		return nil
	}
	in := authorizedInput(t, workspace, "artifact.txt", marker)

	_, err := newTestVerifierBroker(workspace, verifier).
		ExecuteLocalSafeWorker(context.Background(), in)
	if !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("execute error = %v, want ErrAuthorizationDenied", err)
	}
	assertPathAbsent(t, workspace)
}

func TestVerifierGrantMustMatchExactBinding(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "not-created")
	verifier := newTestAuthorizationVerifier()
	verifier.mutateGrant = func(grant VerifiedAuthorization) VerifiedAuthorization {
		grant.TaskID = "different-task"
		return grant
	}
	in := authorizedInput(t, workspace, "artifact.txt", marker)

	_, err := newTestVerifierBroker(workspace, verifier).
		ExecuteLocalSafeWorker(context.Background(), in)
	if !errors.Is(err, ErrAuthorizationMismatch) {
		t.Fatalf("execute error = %v, want ErrAuthorizationMismatch", err)
	}
	assertPathAbsent(t, workspace)
}

func TestAuthorizationBindingIsSingleUseUnderConcurrency(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	verifier := newTestAuthorizationVerifier()
	in := authorizedInput(t, workspace, "artifact.txt", marker)
	broker := newTestVerifierBroker(workspace, verifier)

	const attempts = 12
	var successes atomic.Int32
	var denied atomic.Int32
	var wg sync.WaitGroup
	wg.Add(attempts)
	for range attempts {
		go func() {
			defer wg.Done()
			result, err := broker.ExecuteLocalSafeWorker(context.Background(), in)
			if err == nil && result.OK {
				successes.Add(1)
				return
			}
			if errors.Is(err, ErrAuthorizationDenied) {
				denied.Add(1)
			}
		}()
	}
	wg.Wait()

	if successes.Load() != 1 {
		t.Fatalf("successful effects = %d, want exactly 1", successes.Load())
	}
	if denied.Load() != attempts-1 {
		t.Fatalf("authorization denials = %d, want %d", denied.Load(), attempts-1)
	}
	data, err := os.ReadFile(filepath.Join(workspace, in.ArtifactName))
	if err != nil || string(data) != marker {
		t.Fatalf("authorized artifact mismatch: data=%q err=%v", data, err)
	}
}

type testAuthorizationVerifier struct {
	mu            sync.Mutex
	consumed      map[string]struct{}
	beforeConsume func(AuthorizationVerification) error
	mutateGrant   func(VerifiedAuthorization) VerifiedAuthorization
	callCount     atomic.Int32
}

func newTestAuthorizationVerifier() *testAuthorizationVerifier {
	return &testAuthorizationVerifier{consumed: map[string]struct{}{}}
}

func newTestVerifierBroker(
	workspace string,
	verifier AuthorizationVerifier,
) *Broker {
	return &Broker{
		safeWorker: NewAuthorizedLocalSafeWorker(workspace, verifier),
	}
}

func (v *testAuthorizationVerifier) VerifyAndConsume(
	_ context.Context,
	request AuthorizationVerification,
) (VerifiedAuthorization, error) {
	v.callCount.Add(1)
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.beforeConsume != nil {
		if err := v.beforeConsume(request); err != nil {
			return VerifiedAuthorization{}, err
		}
	}
	if _, exists := v.consumed[request.Binding.ReceiptID]; exists {
		return VerifiedAuthorization{}, errors.New("receipt already consumed")
	}
	v.consumed[request.Binding.ReceiptID] = struct{}{}
	grant := VerifiedAuthorization{
		OwnerIdentity: request.Binding.OwnerIdentity,
		TaskID:        request.Binding.TaskID,
		Action:        request.Binding.Action,
		ReceiptID:     request.Binding.ReceiptID,
		ReceiptDigest: request.Binding.ReceiptDigest,
		EffectDigest:  request.Effect.EffectDigest,
	}
	if v.mutateGrant != nil {
		grant = v.mutateGrant(grant)
	}
	return grant, nil
}

func authorizedInput(
	t *testing.T,
	workspace string,
	artifactName string,
	value string,
) SafeWorkerInput {
	t.Helper()
	sum := sha256.Sum256([]byte("receipt-" + t.Name()))
	in := SafeWorkerInput{
		ArtifactName: artifactName,
		Marker:       value,
		Authorization: AuthorizationBinding{
			OwnerIdentity: "robert",
			TaskID:        "task-" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-")),
			Action:        LocalSafeWorkerAction,
			ReceiptID:     uuid.NewString(),
			ReceiptDigest: hex.EncodeToString(sum[:]),
		},
	}
	digest, err := BindLocalSafeWorkerEffect(workspace, in)
	if err != nil {
		t.Fatalf("bind effect: %v", err)
	}
	in.Authorization.EffectDigest = digest
	return in
}

func assertPathAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("path %q exists or is inaccessible after denial: %v", path, err)
	}
}
