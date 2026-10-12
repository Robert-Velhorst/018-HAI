package executionbroker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"automation-hub-backend/internal/pathsafety"
)

// LocalSafeWorkerID is the runtime id of the local safe worker.
const LocalSafeWorkerID = "hai-local-safe-worker"

const (
	maxSafeOutput        = 4096
	maxSafeArtifactBytes = maxSafeOutput * 2
)

// SafeWorkerInput is the safe worker's bounded payload (§10.15).
type SafeWorkerInput struct {
	ArtifactName  string               `json:"artifactName"`
	Marker        string               `json:"marker"`
	Authorization AuthorizationBinding `json:"authorization"`
}

// SafeWorkerOutput is the safe worker's bounded result.
type SafeWorkerOutput struct {
	ArtifactPath  string `json:"artifactPath"`
	ArtifactHash  string `json:"artifactHash"`
	MarkerFound   bool   `json:"markerFound"`
	BoundedOutput string `json:"boundedOutput"`

	// artifactInfo is retained in-process so Verify can detect a target that
	// was replaced after Run, even when the replacement has identical content.
	artifactInfo os.FileInfo
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
		return RuntimeResult{OK: false, Error: err.Error()}, err
	}
	return RuntimeResult{OK: true, BoundedOutput: out.BoundedOutput}, nil
}

// Run performs the safe workspace-only task and returns the bounded output.
func (w *LocalSafeWorker) Run(ctx context.Context, in SafeWorkerInput) (SafeWorkerOutput, error) {
	if strings.TrimSpace(in.ArtifactName) == "" {
		return SafeWorkerOutput{}, fmt.Errorf("safe worker: artifactName required")
	}
	if strings.TrimSpace(in.Marker) == "" {
		return SafeWorkerOutput{}, fmt.Errorf("safe worker: marker required")
	}
	if len(in.Marker) > maxSafeArtifactBytes {
		return SafeWorkerOutput{}, fmt.Errorf("safe worker: marker exceeds %d bytes", maxSafeArtifactBytes)
	}
	if err := validateArtifactName(in.ArtifactName); err != nil {
		return SafeWorkerOutput{}, err
	}
	effect, err := buildFinalEffect(w.WorkspaceRoot, in)
	if err != nil {
		return SafeWorkerOutput{}, fmt.Errorf("safe worker: %w", err)
	}
	if w.verifier == nil {
		return SafeWorkerOutput{}, fmt.Errorf("safe worker: %w", ErrAuthorizationRequired)
	}
	verification := AuthorizationVerification{
		Binding:         in.Authorization,
		Effect:          effect,
		Consumer:        LocalSafeWorkerID,
		ExecutionTarget: effect.WorkspaceRoot + string(filepath.Separator) + effect.ArtifactName,
	}
	grant, err := w.verifier.VerifyAndConsume(ctx, verification)
	if err != nil {
		return SafeWorkerOutput{}, fmt.Errorf("safe worker: %w", ErrAuthorizationDenied)
	}
	if err := verifyGrant(in.Authorization, effect, grant); err != nil {
		return SafeWorkerOutput{}, fmt.Errorf("safe worker: %w", err)
	}

	// VerifyAndConsume is deliberately the final operation before opening the
	// secure root. OpenSecureRoot(..., true) is the first call below that may
	// create a directory, so an emergency-stop denial has zero filesystem
	// effect.
	root, err := pathsafety.OpenSecureRoot(w.WorkspaceRoot, true)
	if err != nil {
		return SafeWorkerOutput{}, fmt.Errorf("safe worker: open workspace: %w", err)
	}
	defer root.Close()

	file, artifactInfo, err := root.CreateExclusiveFile(in.ArtifactName, 0o600)
	if err != nil {
		return SafeWorkerOutput{}, fmt.Errorf("safe worker: create artifact: %w", err)
	}
	keep := false
	defer func() {
		if !keep {
			root.RemoveIfSame(in.ArtifactName, artifactInfo)
		}
		_ = file.Close()
	}()

	if _, err := io.WriteString(file, in.Marker); err != nil {
		return SafeWorkerOutput{}, fmt.Errorf("safe worker: write artifact: %w", err)
	}
	if err := file.Sync(); err != nil {
		return SafeWorkerOutput{}, fmt.Errorf("safe worker: sync artifact: %w", err)
	}
	if err := root.VerifyFile(in.ArtifactName, file, artifactInfo); err != nil {
		return SafeWorkerOutput{}, fmt.Errorf("safe worker: verify written artifact: %w", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return SafeWorkerOutput{}, fmt.Errorf("safe worker: seek artifact: %w", err)
	}
	read, err := readBoundedArtifact(file)
	if err != nil {
		return SafeWorkerOutput{}, fmt.Errorf("safe worker: read artifact: %w", err)
	}
	if err := root.VerifyFile(in.ArtifactName, file, artifactInfo); err != nil {
		return SafeWorkerOutput{}, fmt.Errorf("safe worker: verify artifact after read: %w", err)
	}
	sum := sha256.Sum256(read)
	out := SafeWorkerOutput{
		ArtifactPath:  filepath.Join(root.Path(), in.ArtifactName),
		ArtifactHash:  hex.EncodeToString(sum[:]),
		MarkerFound:   strings.Contains(string(read), in.Marker),
		BoundedOutput: boundOutput(string(read), maxSafeOutput),
		artifactInfo:  artifactInfo,
	}
	keep = true
	return out, nil
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
		if out.artifactInfo != nil && !os.SameFile(currentInfo, out.artifactInfo) {
			v.OutputBounded = len(out.BoundedOutput) <= maxSafeOutput
			return v
		}
		data, readErr := readBoundedArtifact(file)
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
	return in, nil
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

func readBoundedArtifact(file *os.File) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(file, maxSafeArtifactBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSafeArtifactBytes {
		return nil, fmt.Errorf("artifact exceeds %d bytes", maxSafeArtifactBytes)
	}
	return data, nil
}

func boundOutput(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
