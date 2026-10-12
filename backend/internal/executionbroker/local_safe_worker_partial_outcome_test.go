package executionbroker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"automation-hub-backend/internal/pathsafety"
)

func TestLocalSafeWorkerPartialOutcomePreflightHasNoEffectEvidence(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "not-created")
	verifier := newTestAuthorizationVerifier()
	worker := NewAuthorizedLocalSafeWorker(workspace, verifier)
	in := authorizedInput(t, workspace, "artifact.txt", marker)
	changed := in
	changed.Marker = "changed"
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, test := range []struct {
		name  string
		ctx   context.Context
		input SafeWorkerInput
	}{
		{"canceled", canceled, in},
		{"missing marker", context.Background(), SafeWorkerInput{ArtifactName: in.ArtifactName}},
		{"changed payload", context.Background(), changed},
	} {
		t.Run(test.name, func(t *testing.T) {
			out, err := worker.Run(test.ctx, test.input)
			if err == nil || out != (SafeWorkerOutput{}) || verifier.callCount.Load() != 0 {
				t.Fatalf("preflight consumed authority or fabricated evidence: %+v, %v", out, err)
			}
			assertPathAbsent(t, workspace)
		})
	}
}

func TestLocalSafeWorkerPartialOutcomeVerifierFailureIsNotUnusedAuthority(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		t.Run(map[bool]string{false: "consume error", true: "unmatched grant"}[mismatch], func(t *testing.T) {
			workspace := filepath.Join(t.TempDir(), "not-created")
			verifier := newTestAuthorizationVerifier()
			if mismatch {
				verifier.mutateGrant = func(grant VerifiedAuthorization) VerifiedAuthorization {
					grant.TaskID = "wrong-task"
					return grant
				}
			} else {
				verifier.beforeConsume = func(AuthorizationVerification) error {
					return errors.New("consume unavailable")
				}
			}
			result, err := newTestVerifierBroker(workspace, verifier).ExecuteLocalSafeWorker(
				context.Background(), authorizedInput(t, workspace, "artifact.txt", marker),
			)
			out := result.Output
			if err == nil || out.Progress.Authorization != "unknown" || out.Progress.EffectStarted ||
				out.Progress.ArtifactCreated || out.ArtifactPath != "" || out.artifactInfo != nil ||
				out.BoundedOutput == "" || out.ArtifactHash != "" || out.MarkerFound ||
				result.OK || result.Verification != (SafeWorkerVerification{}) {
				t.Fatalf("ambiguous consumption was lost or promoted: %+v, %v", result, err)
			}
			assertPathAbsent(t, workspace)
			public := PublicExecutionResult(result)
			if public.Output.Progress != out.Progress || public.Output.BoundedOutput != out.BoundedOutput {
				t.Fatalf("public receipt discarded progress: %+v", public)
			}
		})
	}
}

func TestLocalSafeWorkerPartialOutcomeOpenFailureRetainsConsumedAuthority(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(workspace, []byte("operator-owned"), 0o600); err != nil {
		t.Fatal(err)
	}
	verifier := newTestAuthorizationVerifier()
	worker := NewAuthorizedLocalSafeWorker(workspace, verifier)
	in := authorizedInput(t, workspace, "artifact.txt", marker)
	out, err := worker.Run(context.Background(), in)
	if err == nil || out.Progress.Authorization != "consumed" || !out.Progress.EffectStarted ||
		out.Progress.ArtifactCreated || out.ArtifactPath != "" || out.ArtifactHash != "" || out.MarkerFound ||
		out.BoundedOutput == "" || strings.Contains(out.BoundedOutput, marker) {
		t.Fatalf("open failure fabricated an artifact or discarded authority: %+v, %v", out, err)
	}
	if _, err := worker.Run(context.Background(), in); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("consumed receipt replay was not denied: %v", err)
	}
	data, err := os.ReadFile(workspace)
	if err != nil || string(data) != "operator-owned" {
		t.Fatalf("open failure modified existing file: %q, %v", data, err)
	}
}

func TestLocalSafeWorkerPartialOutcomeCreateFailureCannotReplayAfterTargetMoves(t *testing.T) {
	workspace := t.TempDir()
	target := filepath.Join(workspace, "artifact.txt")
	if err := os.WriteFile(target, []byte("operator-owned"), 0o600); err != nil {
		t.Fatal(err)
	}
	worker := NewAuthorizedLocalSafeWorker(workspace, newTestAuthorizationVerifier())
	in := authorizedInput(t, workspace, "artifact.txt", marker)
	out, err := worker.Run(context.Background(), in)
	if !errors.Is(err, pathsafety.ErrPathExists) || out.Progress.Authorization != "consumed" ||
		!out.Progress.EffectStarted || out.Progress.ArtifactCreated || out.artifactInfo != nil || out.ArtifactPath != "" {
		t.Fatalf("create failure did not retain honest evidence: %+v, %v", out, err)
	}
	moved := filepath.Join(workspace, "operator-original.txt")
	if err := os.Rename(target, moved); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.Run(context.Background(), in); !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("replayed consumed authority: %v", err)
	}
	assertPathAbsent(t, target)
	data, err := os.ReadFile(moved)
	if err != nil || string(data) != "operator-owned" {
		t.Fatalf("operator artifact was not preserved: %q, %v", data, err)
	}
}

func TestLocalSafeWorkerPartialOutcomeCanceledAfterConsumptionHasNoFilesystemCall(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "not-created")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	verifier := newTestAuthorizationVerifier()
	verifier.mutateGrant = func(grant VerifiedAuthorization) VerifiedAuthorization {
		cancel()
		return grant
	}
	worker := NewAuthorizedLocalSafeWorker(workspace, verifier)
	out, err := worker.Run(ctx, authorizedInput(t, workspace, "artifact.txt", marker))
	if !errors.Is(err, context.Canceled) || out.Progress.Authorization != "consumed" ||
		out.Progress.EffectStarted || out.ArtifactPath != "" || out.BoundedOutput == "" {
		t.Fatalf("cancellation lost consumed authority or started an effect: %+v, %v", out, err)
	}
	assertPathAbsent(t, workspace)
}

func TestLocalSafeWorkerPartialOutcomeCanceledDuringVerifierRetainsUncertainty(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "not-created")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	verifier := newTestAuthorizationVerifier()
	verifier.beforeConsume = func(AuthorizationVerification) error {
		cancel()
		return ctx.Err()
	}
	out, err := NewAuthorizedLocalSafeWorker(workspace, verifier).Run(ctx, authorizedInput(t, workspace, "artifact.txt", marker))
	if !errors.Is(err, context.Canceled) || !errors.Is(err, ErrAuthorizationDenied) ||
		out.Progress.Authorization != "unknown" || out.Progress.EffectStarted || out.BoundedOutput == "" {
		t.Fatalf("consume cancellation lost cause or uncertainty: %+v, %v", out, err)
	}
	assertPathAbsent(t, workspace)
}

func TestLocalSafeWorkerUsesTheAuthorizedWorkspaceSnapshot(t *testing.T) {
	parent := t.TempDir()
	workspace, other := filepath.Join(parent, "authorized"), filepath.Join(parent, "other")
	verifier := newTestAuthorizationVerifier()
	worker := NewAuthorizedLocalSafeWorker(workspace, verifier)
	verifier.mutateGrant = func(grant VerifiedAuthorization) VerifiedAuthorization {
		worker.WorkspaceRoot = other
		return grant
	}
	out, err := worker.Run(context.Background(), authorizedInput(t, workspace, "artifact.txt", marker))
	if err != nil || out.ArtifactPath != filepath.Join(workspace, "artifact.txt") {
		t.Fatalf("effect drifted from the authorized workspace: %+v, %v", out, err)
	}
	assertPathAbsent(t, other)
}

// These adapters operate on real files. They arrange interrupted/short writes
// and closed-descriptor sync failures without production fault flags or hooks.
type interruptedSafeArtifactWriter struct {
	*os.File
	afterWrite func()
	short      bool
}

func (w interruptedSafeArtifactWriter) Write(data []byte) (int, error) {
	if w.short {
		return w.File.Write(data[:3])
	}
	n, err := w.File.Write(data)
	if w.afterWrite != nil {
		w.afterWrite()
	}
	return n, err
}

func newPartialArtifactFixture(t *testing.T) (*pathsafety.SecureRoot, *os.File, SafeWorkerInput, SafeWorkerOutput) {
	t.Helper()
	root, err := pathsafety.OpenSecureRoot(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	in := SafeWorkerInput{ArtifactName: "artifact.txt", Marker: marker}
	file, info, err := root.CreateExclusiveFile(in.ArtifactName, 0o600)
	if err != nil {
		_ = root.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = file.Close()
		_ = root.Close()
	})
	out := SafeWorkerOutput{
		ArtifactPath: filepath.Join(root.Path(), in.ArtifactName),
		artifactInfo: info,
		Progress:     SafeWorkerProgress{Authorization: "consumed", EffectStarted: true, ArtifactCreated: true, IdentityVerified: true},
	}
	return root, file, in, out
}

func TestLocalSafeWorkerPartialOutcomeWriteAndSyncFailures(t *testing.T) {
	for _, name := range []string{"closed write", "read-only write", "short write", "closed sync", "canceled before write", "canceled after write"} {
		t.Run(name, func(t *testing.T) {
			root, file, in, out := newPartialArtifactFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var writer safeArtifactWriter = file
			wantBytes, wantComplete := 0, false
			switch name {
			case "closed write":
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
			case "read-only write":
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
				readOnly, _, err := root.OpenExistingFile(in.ArtifactName)
				if err != nil {
					t.Fatal(err)
				}
				defer readOnly.Close()
				writer = readOnly
			case "short write":
				writer = interruptedSafeArtifactWriter{File: file, short: true}
				wantBytes = 3
			case "closed sync":
				writer = interruptedSafeArtifactWriter{File: file, afterWrite: func() {
					if err := file.Close(); err != nil {
						t.Fatal(err)
					}
				}}
				wantBytes, wantComplete = len(in.Marker), true
			case "canceled after write":
				writer = interruptedSafeArtifactWriter{File: file, afterWrite: cancel}
				wantBytes, wantComplete = len(in.Marker), true
			case "canceled before write":
				cancel()
			}
			err := writeSafeArtifact(ctx, writer, in.Marker, &out)
			if err == nil || out.Progress.BytesWritten != wantBytes || out.Progress.WriteComplete != wantComplete ||
				out.Progress.SyncComplete || out.Progress.ReadComplete || out.ArtifactHash != "" || out.MarkerFound {
				t.Fatalf("write/sync failure claimed unobserved success: %+v, %v", out, err)
			}
			if name == "short write" && !errors.Is(err, io.ErrShortWrite) {
				t.Fatalf("short write error: %v", err)
			}
			if strings.HasPrefix(name, "canceled") && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation error: %v", err)
			}
			data, readErr := os.ReadFile(out.ArtifactPath)
			info, statErr := os.Stat(out.ArtifactPath)
			if readErr != nil || statErr != nil || len(data) != wantBytes || !os.SameFile(info, out.artifactInfo) {
				t.Fatalf("partial artifact/identity lost: data=%q, read=%v, stat=%v", data, readErr, statErr)
			}
			out.BoundedOutput = partialSafeWorkerSummary(out)
			result, resultErr := newTestVerifierBroker(filepath.Dir(out.ArtifactPath), newTestAuthorizationVerifier()).finishSafeWorkerExecution(in, out, err)
			if resultErr != err || result.Output != out || result.OK || result.Verification.Passed {
				t.Fatalf("broker promoted/discarded failed worker evidence: %+v, %v", result, resultErr)
			}
		})
	}
}

func TestLocalSafeWorkerPartialOutcomeReadFailureDoesNotClaimHash(t *testing.T) {
	root, file, in, out := newPartialArtifactFixture(t)
	if err := writeSafeArtifact(context.Background(), file, in.Marker, &out); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	writeOnly, err := os.OpenFile(out.ArtifactPath, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer writeOnly.Close()
	err = readVerifiedSafeArtifact(context.Background(), root, writeOnly, in, &out)
	if err == nil || !strings.Contains(err.Error(), "read artifact") || out.Progress.ReadComplete ||
		out.Progress.ReadBytes != 0 || out.ArtifactHash != "" || out.MarkerFound || !out.Progress.SyncComplete {
		t.Fatalf("failed OS read manufactured read/hash evidence: %+v, %v", out, err)
	}
}

func TestLocalSafeWorkerPartialOutcomeOversizedReadIsBoundedAndPubliclyRedacted(t *testing.T) {
	root, file, in, out := newPartialArtifactFixture(t)
	data := "token=synthetic-worker-read-secret\n" + strings.Repeat("x", maxSafeArtifactBytes+1)
	if _, err := file.WriteString(data); err != nil {
		t.Fatal(err)
	}
	err := readVerifiedSafeArtifact(context.Background(), root, file, in, &out)
	if err == nil || out.Progress.ReadComplete || out.Progress.ReadBytes != maxSafeArtifactBytes+1 ||
		out.ArtifactHash != "" || out.MarkerFound || len(out.BoundedOutput) != maxSafeOutput {
		t.Fatalf("oversized read was promoted or discarded: %+v, %v", out.Progress, err)
	}
	out.BoundedOutput = partialSafeWorkerSummary(out)
	public := PublicExecutionResult(ExecutionResult{RuntimeID: LocalSafeWorkerID, Output: out})
	if len(public.Output.BoundedOutput) > maxSafeOutput || strings.Contains(public.Output.BoundedOutput, "synthetic-worker-read-secret") ||
		!strings.Contains(public.Output.BoundedOutput, "REDACTED") || public.Output.Progress != out.Progress || public.OK {
		t.Fatalf("partial public evidence leaked or changed: %+v", public)
	}
	if !strings.Contains(out.BoundedOutput, "synthetic-worker-read-secret") {
		t.Fatal("public redaction mutated original evidence")
	}
}

func TestLocalSafeWorkerPartialOutcomeUnavailableCleanupRetainsOriginalIdentity(t *testing.T) {
	root, file, in, out := newPartialArtifactFixture(t)
	if err := writeSafeArtifact(context.Background(), file, in.Marker, &out); err != nil {
		t.Fatal(err)
	}
	// A closed root cannot perform the former best-effort failure cleanup.
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	err := readVerifiedSafeArtifact(context.Background(), root, file, in, &out)
	if err == nil || out.Progress.IdentityVerified || out.ArtifactHash != "" || out.MarkerFound {
		t.Fatalf("closed root was verified: %+v, %v", out, err)
	}
	info, statErr := os.Stat(out.ArtifactPath)
	if statErr != nil || !os.SameFile(info, out.artifactInfo) {
		t.Fatalf("failure lost the retained identity: %v", statErr)
	}
}

func TestLocalSafeWorkerPartialOutcomeReplacementPreservesBothArtifacts(t *testing.T) {
	root, file, in, out := newPartialArtifactFixture(t)
	if err := writeSafeArtifact(context.Background(), file, in.Marker, &out); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(root.Path(), "retained-original.txt")
	if err := os.Rename(out.ArtifactPath, original); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out.ArtifactPath, []byte(in.Marker), 0o600); err != nil {
		t.Fatal(err)
	}
	originalHandle, _, err := root.OpenExistingFile("retained-original.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer originalHandle.Close()
	err = readVerifiedSafeArtifact(context.Background(), root, originalHandle, in, &out)
	if !errors.Is(err, pathsafety.ErrPathSubstituted) || out.Progress.IdentityVerified || out.ArtifactHash != "" || out.MarkerFound {
		t.Fatalf("replacement was accepted: %+v, %v", out, err)
	}
	originalInfo, originalErr := os.Stat(original)
	replacementInfo, replacementErr := os.Stat(out.ArtifactPath)
	if originalErr != nil || replacementErr != nil || !os.SameFile(originalInfo, out.artifactInfo) || os.SameFile(replacementInfo, out.artifactInfo) {
		t.Fatalf("failure removed or confused original/replacement: %v, %v", originalErr, replacementErr)
	}
}

func TestLocalSafeWorkerPartialOutcomeAfterReadStillNeedsIdentityAndUncanceledContext(t *testing.T) {
	for _, name := range []string{"replaced after read", "canceled after read"} {
		t.Run(name, func(t *testing.T) {
			root, file, in, out := newPartialArtifactFixture(t)
			if err := writeSafeArtifact(context.Background(), file, in.Marker, &out); err != nil {
				t.Fatal(err)
			}
			if _, err := file.Seek(0, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			read, err := readSafeArtifact(file)
			if err != nil {
				t.Fatal(err)
			}
			out.Progress.ReadBytes, out.Progress.ReadComplete = len(read), true
			out.BoundedOutput = boundOutput(string(read), maxSafeOutput)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			wantErr := context.Canceled
			if name == "replaced after read" {
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(out.ArtifactPath, filepath.Join(root.Path(), "retained-original.txt")); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(out.ArtifactPath, read, 0o600); err != nil {
					t.Fatal(err)
				}
				file, _, err = root.OpenExistingFile("retained-original.txt")
				if err != nil {
					t.Fatal(err)
				}
				defer file.Close()
				wantErr = pathsafety.ErrPathSubstituted
			} else {
				cancel()
			}
			err = verifySafeArtifactRead(ctx, root, file, in, read, &out)
			if !errors.Is(err, wantErr) || !out.Progress.ReadComplete || out.Progress.ReadBytes != len(marker) ||
				out.ArtifactHash != "" || out.MarkerFound || (name == "replaced after read" && out.Progress.IdentityVerified) {
				t.Fatalf("observed read was promoted despite final-stage failure: %+v, %v", out, err)
			}
			if _, err := os.Stat(out.ArtifactPath); err != nil {
				t.Fatalf("failure removed the current target: %v", err)
			}
		})
	}
}

func TestLocalSafeWorkerPartialOutcomeRuntimeExecutePreservesFailureOutput(t *testing.T) {
	harness := newBridgeHarness(t, "robert")
	workspace := t.TempDir()
	target := filepath.Join(workspace, "artifact.txt")
	if err := os.WriteFile(target, []byte("operator-owned"), 0o600); err != nil {
		t.Fatal(err)
	}
	worker := newProductionLocalSafeWorker(workspace, harness.bridge, harness.bridge)
	result, err := worker.Execute(context.Background(), map[string]any{"artifactName": "artifact.txt", "marker": marker})
	if !errors.Is(err, pathsafety.ErrPathExists) || result.OK || result.Error == "" ||
		!strings.Contains(result.BoundedOutput, "authorizationState=consumed") || len(result.BoundedOutput) > maxSafeOutput || strings.Contains(result.BoundedOutput, marker) {
		t.Fatalf("runtime discarded failure output or claimed completion: %+v, %v", result, err)
	}
	data, readErr := os.ReadFile(target)
	if readErr != nil || string(data) != "operator-owned" {
		t.Fatalf("runtime modified operator artifact: %q, %v", data, readErr)
	}
	if err := os.Rename(target, filepath.Join(workspace, "operator-original.txt")); err != nil {
		t.Fatal(err)
	}
	replayed, replayErr := worker.Execute(context.Background(), map[string]any{"artifactName": "artifact.txt", "marker": marker})
	if !errors.Is(replayErr, ErrAuthorizationDenied) || replayed.OK {
		t.Fatalf("runtime reissued consumed effect authority: %+v, %v", replayed, replayErr)
	}
	assertPathAbsent(t, target)
}

func TestLocalSafeWorkerSuccessfulProgressAndSerializedIdentityAreDistinct(t *testing.T) {
	workspace := t.TempDir()
	worker := NewAuthorizedLocalSafeWorker(workspace, newTestAuthorizationVerifier())
	in := authorizedInput(t, workspace, "artifact.txt", marker)
	out, err := worker.Run(context.Background(), in)
	if err != nil || out.Progress.Authorization != "consumed" || !out.Progress.ArtifactCreated || !out.Progress.EffectStarted ||
		out.Progress.BytesWritten != len(marker) || !out.Progress.WriteComplete || !out.Progress.SyncComplete ||
		out.Progress.ReadBytes != len(marker) || !out.Progress.ReadComplete || !out.Progress.IdentityVerified || !out.Progress.FileClosed ||
		!worker.Verify(in, out).Passed {
		t.Fatalf("successful run did not record observed stages: %+v, %v", out, err)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var restored SafeWorkerOutput
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Progress != out.Progress || restored.ArtifactHash != out.ArtifactHash || worker.Verify(in, restored).Passed {
		t.Fatal("serialized observations recovered nonexistent filesystem identity authority")
	}
}
