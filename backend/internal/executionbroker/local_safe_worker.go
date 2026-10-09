package executionbroker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"automation-hub-backend/internal/operations"
	"automation-hub-backend/internal/pathsafety"
)

// LocalSafeWorkerID is the runtime id of the local safe worker.
const LocalSafeWorkerID = "hai-local-safe-worker"

const (
	maxSafeOutput        = 4096
	maxSafeArtifactBytes = 64 * 1024
)

// SafeWorkerInput is the safe worker's bounded payload (§10.15).
type SafeWorkerInput struct {
	ArtifactName  string               `json:"artifactName"`
	Marker        string               `json:"marker"`
	Authorization AuthorizationBinding `json:"authorization"`
}

// SafeWorkerOutput is the safe worker's bounded result.
type SafeWorkerOutput struct {
	ArtifactPath  string             `json:"artifactPath"`
	ArtifactHash  string             `json:"artifactHash"`
	MarkerFound   bool               `json:"markerFound"`
	BoundedOutput string             `json:"boundedOutput"`
	Progress      SafeWorkerProgress `json:"progress"`

	// artifactInfo is retained in-process so Verify can detect a target that
	// was replaced after Run, even when the replacement has identical content.
	artifactInfo os.FileInfo
}

// SafeWorkerProgress records observations, not retry authority or durable
// receipts. Empty authorization means consumption was not attempted; unknown
// includes a verifier error or an unmatched grant. EffectStarted means a
// potentially mutating filesystem call was entered, not that it succeeded.
// ArtifactPath is the creation-time location, not proof of its current target.
// True stage flags describe successful observations; false means unconfirmed,
// not proof of absence. IdentityVerified is only the last identity check, not
// full task verification. FileClosed means Close returned nil.
type SafeWorkerProgress struct {
	Authorization    string `json:"authorization,omitempty"`
	EffectStarted    bool   `json:"effectStarted"`
	ArtifactCreated  bool   `json:"artifactCreated"`
	BytesWritten     int    `json:"bytesWritten"`
	WriteComplete    bool   `json:"writeComplete"`
	SyncComplete     bool   `json:"syncComplete"`
	ReadBytes        int    `json:"readBytes"`
	ReadComplete     bool   `json:"readComplete"`
	IdentityVerified bool   `json:"identityVerified"`
	FileClosed       bool   `json:"fileClosed"`
}

// LocalSafeWorker creates a small text file in a configured workspace, reads it
// back, hashes it, and returns bounded output. It performs NO os/exec, network,
// arbitrary paths, symlink escape, deletion, home traversal, or account access.
type LocalSafeWorker struct {
	WorkspaceRoot string
	verifier      AuthorizationVerifier
	issuer        AuthorizationIssuer
}

// NewLocalSafeWorker builds a safe worker confined to workspaceRoot.
func NewLocalSafeWorker(workspaceRoot string) *LocalSafeWorker {
	return &LocalSafeWorker{WorkspaceRoot: workspaceRoot}
}

// NewAuthorizedLocalSafeWorker injects the final-effect verifier. A nil
// verifier is accepted only to preserve fail-closed composition.
func NewAuthorizedLocalSafeWorker(
	workspaceRoot string,
	verifier AuthorizationVerifier,
) *LocalSafeWorker {
	return &LocalSafeWorker{WorkspaceRoot: workspaceRoot, verifier: verifier}
}

func newProductionLocalSafeWorker(
	workspaceRoot string,
	issuer AuthorizationIssuer,
	verifier AuthorizationVerifier,
) *LocalSafeWorker {
	return &LocalSafeWorker{
		WorkspaceRoot: workspaceRoot,
		verifier:      verifier,
		issuer:        issuer,
	}
}

func (w *LocalSafeWorker) ID() string          { return LocalSafeWorkerID }
func (w *LocalSafeWorker) DisplayName() string { return "HAI Local Safe Worker" }

func (w *LocalSafeWorker) ClaimLevel(ctx context.Context) ClaimLevel {
	if strings.TrimSpace(w.WorkspaceRoot) == "" {
		return ClaimContractDefined
	}
	return ClaimExercisedSafeTask
}

func (w *LocalSafeWorker) HealthCheck(ctx context.Context) RuntimeHealth {
	if strings.TrimSpace(w.WorkspaceRoot) == "" {
		return RuntimeHealth{Status: RuntimeNotConfigured, Detail: "workspace root not configured", Claim: ClaimContractDefined}
	}
	if w.verifier == nil {
		return RuntimeHealth{
			Status: RuntimeBlocked,
			Detail: "final-effect authorization verifier not configured",
			Claim:  ClaimContractDefined,
		}
	}
	return RuntimeHealth{Status: RuntimeReady, Detail: "workspace configured", Claim: ClaimExercisedSafeTask}
}

func (w *LocalSafeWorker) DryRun(ctx context.Context, payload map[string]any) (DryRunResult, error) {
	in, err := parseSafeWorkerInput(payload)
	if err != nil {
		return DryRunResult{OK: false, Summary: err.Error()}, err
	}
	if _, err := w.resolvePath(in.ArtifactName); err != nil {
		return DryRunResult{OK: false, Summary: err.Error()}, err
	}
	return DryRunResult{OK: true, Summary: "will write, read back, and hash " + in.ArtifactName}, nil
}

func (w *LocalSafeWorker) Execute(ctx context.Context, payload map[string]any) (RuntimeResult, error) {
	if err := operations.ValidateExecutionContext(ctx); err != nil {
		return RuntimeResult{OK: false, Error: err.Error()}, err
	}
	if w.issuer == nil {
		err := fmt.Errorf("safe worker: %w", ErrAuthorizationRequired)
		return RuntimeResult{OK: false, Error: err.Error()}, err
	}
	input, err := w.issuer.Issue(
		ctx,
		w.WorkspaceRoot,
		parseSafeWorkerInputOrZero(payload),
	)
	if err != nil {
		return RuntimeResult{OK: false, Error: err.Error()}, err
	}
	out, err := w.Run(ctx, input)
	if err != nil {
		return RuntimeResult{OK: false, BoundedOutput: out.BoundedOutput, Error: err.Error()}, err
	}
	return RuntimeResult{OK: true, BoundedOutput: out.BoundedOutput}, nil
}

// Run performs the safe workspace-only task and returns the bounded output.
func (w *LocalSafeWorker) Run(ctx context.Context, in SafeWorkerInput) (out SafeWorkerOutput, runErr error) {
	ctx, finish, err := operations.BindExecutionContext(ctx)
	if err != nil {
		return SafeWorkerOutput{}, err
	}
	defer finish()
	if strings.TrimSpace(w.WorkspaceRoot) == "" {
		return SafeWorkerOutput{}, fmt.Errorf("safe worker: workspace root not configured")
	}
	if strings.TrimSpace(in.ArtifactName) == "" {
		return SafeWorkerOutput{}, fmt.Errorf("safe worker: artifactName required")
	}
	if strings.TrimSpace(in.Marker) == "" {
		return SafeWorkerOutput{}, fmt.Errorf("safe worker: marker required")
	}
	if len(in.Marker) > maxSafeArtifactBytes {
		return SafeWorkerOutput{}, fmt.Errorf("safe worker: marker exceeds %d byte artifact limit", maxSafeArtifactBytes)
	}
	if err := validateArtifactName(in.ArtifactName); err != nil {
		return SafeWorkerOutput{}, err
	}
	if in.Authorization.OperationScope == nil && managedSafeWorkerInput(in) {
		return SafeWorkerOutput{}, ErrAuthorizationRequired
	}
	effect, err := buildFinalEffect(w.WorkspaceRoot, in)
	if err != nil {
		return SafeWorkerOutput{}, fmt.Errorf("safe worker: %w", err)
	}
	if w.verifier == nil {
		return SafeWorkerOutput{}, fmt.Errorf("safe worker: %w", ErrAuthorizationRequired)
	}
	if err := validateActiveOperationScope(ctx, effect.OperationScope); err != nil {
		return SafeWorkerOutput{}, err
	}
	verification := AuthorizationVerification{
		Binding:         in.Authorization,
		Effect:          effect,
		Consumer:        LocalSafeWorkerID,
		ExecutionTarget: effect.WorkspaceRoot + string(filepath.Separator) + effect.ArtifactName,
	}
	if err := operations.ValidateExecutionContext(ctx); err != nil {
		return SafeWorkerOutput{}, err
	}
	// The verifier contract does not distinguish a pre-consumption denial from
	// an error after durable consumption. Never infer unused authority from it.
	out.Progress.Authorization = "unknown"
	defer func() {
		if runErr == nil {
			runErr = operations.ValidateExecutionContext(ctx)
		}
		if runErr != nil {
			out.BoundedOutput = partialSafeWorkerSummary(out)
		}
	}()
	grant, err := w.verifier.VerifyAndConsume(ctx, verification)
	if err != nil {
		if ctxErr := operations.ValidateExecutionContext(ctx); ctxErr != nil {
			return out, errors.Join(ErrAuthorizationDenied, ctxErr)
		}
		return out, fmt.Errorf("safe worker: %w", ErrAuthorizationDenied)
	}
	if err := verifyGrant(in.Authorization, effect, grant); err != nil {
		return out, fmt.Errorf("safe worker: %w", err)
	}
	out.Progress.Authorization = "consumed"
	if err := operations.ValidateExecutionContext(ctx); err != nil {
		return out, err
	}
	if err := validateActiveOperationScope(ctx, effect.OperationScope); err != nil {
		return out, err
	}

	// VerifyAndConsume is deliberately the final operation before opening the
	// secure root. OpenSecureRoot(..., true) is the first call below that may
	// create a directory, so an emergency-stop denial has zero filesystem
	// effect.
	out.Progress.EffectStarted = true
	root, err := pathsafety.OpenSecureRoot(effect.WorkspaceRoot, true)
	if err != nil {
		return out, fmt.Errorf("safe worker: open workspace: %w", err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("safe worker: close workspace: %w", err))
		}
	}()
	if err := operations.ValidateExecutionContext(ctx); err != nil {
		return out, err
	}

	file, artifactInfo, err := root.CreateExclusiveFile(in.ArtifactName, 0o600)
	if err != nil {
		// CreateExclusiveFile can fail after creation and best-effort cleanup.
		// With no returned handle/identity, do not claim absence or a retained file.
		return out, fmt.Errorf("safe worker: create artifact: %w", err)
	}
	out.ArtifactPath = filepath.Join(root.Path(), in.ArtifactName)
	out.artifactInfo = artifactInfo
	out.Progress.ArtifactCreated = true
	out.Progress.IdentityVerified = true
	defer func() {
		// Retain the original artifact on error. RemoveIfSame has neither an
		// observable cleanup result nor atomic identity-checked removal.
		if err := file.Close(); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("safe worker: close artifact: %w", err))
		} else {
			out.Progress.FileClosed = true
		}
	}()

	if err := writeSafeArtifact(ctx, file, in.Marker, &out); err != nil {
		return out, err
	}
	if err := readVerifiedSafeArtifact(ctx, root, file, in, &out); err != nil {
		return out, err
	}
	return out, nil
}

type safeArtifactWriter interface {
	io.Writer
	Sync() error
}

func writeSafeArtifact(ctx context.Context, file safeArtifactWriter, marker string, out *SafeWorkerOutput) error {
	if err := operations.ValidateExecutionContext(ctx); err != nil {
		return err
	}
	n, err := file.Write([]byte(marker))
	out.Progress.BytesWritten = n
	if err != nil {
		return fmt.Errorf("safe worker: write artifact: %w", err)
	}
	if n != len(marker) {
		return fmt.Errorf("safe worker: write artifact: %w", io.ErrShortWrite)
	}
	out.Progress.WriteComplete = true
	if err := operations.ValidateExecutionContext(ctx); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("safe worker: sync artifact: %w", err)
	}
	out.Progress.SyncComplete = true
	return nil
}

func readVerifiedSafeArtifact(ctx context.Context, root *pathsafety.SecureRoot, file *os.File, in SafeWorkerInput, out *SafeWorkerOutput) error {
	if err := operations.ValidateExecutionContext(ctx); err != nil {
		return err
	}
	out.Progress.IdentityVerified = false
	if err := root.VerifyFile(in.ArtifactName, file, out.artifactInfo); err != nil {
		return fmt.Errorf("safe worker: verify written artifact: %w", err)
	}
	out.Progress.IdentityVerified = true
	if err := operations.ValidateExecutionContext(ctx); err != nil {
		return err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("safe worker: seek artifact: %w", err)
	}
	read, err := readSafeArtifact(file)
	out.Progress.ReadBytes = len(read)
	out.BoundedOutput = boundOutput(string(read), maxSafeOutput)
	if err != nil {
		return fmt.Errorf("safe worker: read artifact: %w", err)
	}
	out.Progress.ReadComplete = true
	return verifySafeArtifactRead(ctx, root, file, in, read, out)
}

func verifySafeArtifactRead(ctx context.Context, root *pathsafety.SecureRoot, file *os.File, in SafeWorkerInput, read []byte, out *SafeWorkerOutput) error {
	if err := operations.ValidateExecutionContext(ctx); err != nil {
		return err
	}
	out.Progress.IdentityVerified = false
	if err := root.VerifyFile(in.ArtifactName, file, out.artifactInfo); err != nil {
		return fmt.Errorf("safe worker: verify artifact after read: %w", err)
	}
	out.Progress.IdentityVerified = true
	if err := operations.ValidateExecutionContext(ctx); err != nil {
		return err
	}
	sum := sha256.Sum256(read)
	out.ArtifactHash = hex.EncodeToString(sum[:])
	out.MarkerFound = strings.Contains(string(read), in.Marker)
	return nil
}

func partialSafeWorkerSummary(out SafeWorkerOutput) string {
	p := out.Progress
	summary := fmt.Sprintf("safe worker incomplete; reconcile before retry: authorizationState=%s; filesystemCallStarted=%t; artifactCreated=%t; bytesWritten=%d; writeComplete=%t; syncComplete=%t; readBytes=%d; readComplete=%t; identityVerifiedAtLastCheck=%t; fileClosed=%t",
		p.Authorization, p.EffectStarted, p.ArtifactCreated, p.BytesWritten, p.WriteComplete,
		p.SyncComplete, p.ReadBytes, p.ReadComplete, p.IdentityVerified, p.FileClosed)
	if out.BoundedOutput != "" {
		summary += "\nobserved read bytes (not completion proof):\n" + out.BoundedOutput
	}
	return boundOutput(summary, maxSafeOutput)
}

// resolvePath validates artifactName (basename only, no separators/dot-dot/
// absolute) and confines it inside the workspace root.
func (w *LocalSafeWorker) resolvePath(artifactName string) (string, error) {
	if strings.TrimSpace(w.WorkspaceRoot) == "" {
		return "", fmt.Errorf("safe worker: workspace root not configured")
	}
	if err := validateArtifactName(artifactName); err != nil {
		return "", err
	}
	root, err := filepath.Abs(strings.TrimSpace(w.WorkspaceRoot))
	if err != nil {
		return "", fmt.Errorf("safe worker: canonicalize workspace: %w", err)
	}
	full, err := pathsafety.SafeJoin(root, artifactName)
	if err != nil {
		return "", fmt.Errorf("safe worker: %w", err)
	}
	if info, err := os.Lstat(full); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("safe worker: existing artifact is a link")
		}
		return "", fmt.Errorf("safe worker: artifact already exists")
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("safe worker: inspect artifact: %w", err)
	}
	return full, nil
}

func validateArtifactName(artifactName string) error {
	if filepath.Base(artifactName) != artifactName {
		return fmt.Errorf("safe worker: artifactName must be a basename with no path separators")
	}
	if !pathsafety.IsSafeRelative(artifactName) {
		return fmt.Errorf("safe worker: unsafe artifactName %q", artifactName)
	}
	return nil
}

// VerifySafeWorker checks the postconditions of a safe worker run (§10.15):
// file exists, path inside workspace, hash matches, marker found, output bounded.
type SafeWorkerVerification struct {
	FileExists      bool `json:"fileExists"`
	InsideWorkspace bool `json:"insideWorkspace"`
	HashMatches     bool `json:"hashMatches"`
	MarkerFound     bool `json:"markerFound"`
	OutputBounded   bool `json:"outputBounded"`
	Passed          bool `json:"passed"`
}

func (w *LocalSafeWorker) Verify(in SafeWorkerInput, out SafeWorkerOutput) SafeWorkerVerification {
	v := SafeWorkerVerification{}
	// A serialized path/hash cannot recover the original filesystem identity.
	if out.artifactInfo == nil {
		v.OutputBounded = len(out.BoundedOutput) <= maxSafeOutput
		return v
	}
	if filepath.Base(in.ArtifactName) != in.ArtifactName || !pathsafety.IsSafeRelative(in.ArtifactName) {
		v.OutputBounded = len(out.BoundedOutput) <= maxSafeOutput
		return v
	}
	root, err := pathsafety.OpenSecureRoot(w.WorkspaceRoot, false)
	if err != nil {
		v.OutputBounded = len(out.BoundedOutput) <= maxSafeOutput
		return v
	}
	defer root.Close()

	expectedPath := filepath.Join(root.Path(), in.ArtifactName)
	relative, relErr := filepath.Rel(root.Path(), filepath.Clean(out.ArtifactPath))
	v.InsideWorkspace = relErr == nil && relative == in.ArtifactName &&
		filepath.Clean(out.ArtifactPath) == expectedPath
	if !v.InsideWorkspace {
		v.OutputBounded = len(out.BoundedOutput) <= maxSafeOutput
		return v
	}

	file, currentInfo, err := root.OpenExistingFile(in.ArtifactName)
	v.FileExists = err == nil
	if err == nil {
		defer file.Close()
		if !os.SameFile(currentInfo, out.artifactInfo) {
			v.OutputBounded = len(out.BoundedOutput) <= maxSafeOutput
			return v
		}
		data, readErr := readSafeArtifact(file)
		if readErr != nil || root.VerifyFile(in.ArtifactName, file, currentInfo) != nil {
			v.OutputBounded = len(out.BoundedOutput) <= maxSafeOutput
			return v
		}
		sum := sha256.Sum256(data)
		v.HashMatches = hex.EncodeToString(sum[:]) == out.ArtifactHash
		v.MarkerFound = strings.Contains(string(data), in.Marker)
	}
	v.OutputBounded = len(out.BoundedOutput) <= maxSafeOutput
	v.Passed = v.FileExists && v.InsideWorkspace && v.HashMatches && v.MarkerFound && v.OutputBounded
	return v
}

func parseSafeWorkerInput(payload map[string]any) (SafeWorkerInput, error) {
	in := parseSafeWorkerInputOrZero(payload)
	if strings.TrimSpace(in.ArtifactName) == "" || strings.TrimSpace(in.Marker) == "" {
		return in, fmt.Errorf("safe worker: artifactName and marker required")
	}
	if len(in.Marker) > maxSafeArtifactBytes {
		return in, fmt.Errorf("safe worker: marker exceeds %d byte artifact limit", maxSafeArtifactBytes)
	}
	return in, nil
}

func readSafeArtifact(file *os.File) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(file, maxSafeArtifactBytes+1))
	if err != nil {
		return data, err
	}
	if len(data) > maxSafeArtifactBytes {
		return data, fmt.Errorf("artifact exceeds %d byte limit", maxSafeArtifactBytes)
	}
	return data, nil
}

func parseSafeWorkerInputOrZero(payload map[string]any) SafeWorkerInput {
	in := SafeWorkerInput{}
	if v, ok := payload["artifactName"].(string); ok {
		in.ArtifactName = v
	}
	if v, ok := payload["marker"].(string); ok {
		in.Marker = v
	}
	switch value := payload["authorization"].(type) {
	case AuthorizationBinding:
		in.Authorization = value
	case map[string]any:
		in.Authorization = AuthorizationBinding{
			OwnerIdentity: stringValue(value["ownerIdentity"]),
			TaskID:        stringValue(value["taskId"]),
			Action:        stringValue(value["action"]),
			ReceiptID:     stringValue(value["receiptId"]),
			ReceiptDigest: stringValue(value["receiptDigest"]),
			EffectDigest:  stringValue(value["effectDigest"]),
		}
	}
	return in
}

func stringValue(value any) string {
	result, _ := value.(string)
	return result
}

func boundOutput(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
