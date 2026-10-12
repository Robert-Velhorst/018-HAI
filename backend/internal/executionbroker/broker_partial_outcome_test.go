package executionbroker

import (
	"errors"
	"strings"
	"testing"
)

func TestBrokerPartialOutcomeRetainsOutputWithoutVerificationOrErrorReclassification(t *testing.T) {
	lateError := errors.New("post-effect failure")
	for _, cause := range []error{
		lateError,
		ErrAuthorizationDenied,
		errors.Join(ErrAuthorizationDenied, lateError),
	} {
		for _, output := range []SafeWorkerOutput{
			{},
			{ArtifactPath: "workspace/partial.txt", ArtifactHash: "observed-hash", MarkerFound: true, BoundedOutput: "observed output"},
		} {
			// A nil broker proves the error branch does not call Verify or rerun
			// the worker. The output here is supplied test evidence, not a file.
			var broker *Broker
			result, err := broker.finishSafeWorkerExecution(SafeWorkerInput{}, output, cause)
			if err != cause || !errors.Is(err, cause) || result.RuntimeID != LocalSafeWorkerID || result.Output != output {
				t.Fatalf("partial output/error discarded or reclassified: %#v, %v", result, err)
			}
			if result.OK || result.Verification != (SafeWorkerVerification{}) {
				t.Fatalf("failed run manufactured verification: %#v", result)
			}
		}
	}
}

func TestBrokerPublicReceiptIsRedactedBoundedAndDoesNotMutateEvidence(t *testing.T) {
	original := ExecutionResult{
		RuntimeID: LocalSafeWorkerID, OK: true,
		Output: SafeWorkerOutput{
			ArtifactPath: "workspace/receipt.txt", ArtifactHash: "observed-hash", MarkerFound: true,
			BoundedOutput: "token=synthetic-receipt-secret",
		},
		Verification: SafeWorkerVerification{Passed: true, FileExists: true},
	}
	public := PublicExecutionResult(original)
	if strings.Contains(public.Output.BoundedOutput, "synthetic-receipt-secret") || !strings.Contains(public.Output.BoundedOutput, "REDACTED") {
		t.Fatal("public receipt leaked a synthetic secret")
	}
	if public.Output.ArtifactPath != original.Output.ArtifactPath || public.Output.ArtifactHash != original.Output.ArtifactHash || public.Verification != original.Verification || public.OK != original.OK {
		t.Fatal("display projection replaced actual artifact/verification evidence")
	}
	if original.Output.BoundedOutput != "token=synthetic-receipt-secret" {
		t.Fatal("redaction mutated the reconciliation receipt")
	}
	original.Output.BoundedOutput = strings.Repeat("x", maxSafeOutput+50)
	if len(PublicExecutionResult(original).Output.BoundedOutput) > maxSafeOutput {
		t.Fatal("public receipt is not bounded")
	}
	original.Output.Progress.Authorization = "token=synthetic-progress-secret"
	if projected := PublicExecutionResult(original); projected.Output.Progress.Authorization != "unknown" ||
		original.Output.Progress.Authorization != "token=synthetic-progress-secret" {
		t.Fatal("unknown progress was exposed or evidence was mutated")
	}
}
