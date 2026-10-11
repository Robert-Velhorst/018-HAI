package automation

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"automation-hub-backend/internal/models"

	"github.com/google/uuid"
)

const (
	defaultApprovalProofTTL           = 5 * time.Minute
	maximumApprovalProofTTL           = 15 * time.Minute
	maximumApprovalDecisionAge        = 15 * time.Minute
	maximumApprovalDecisionFutureSkew = 5 * time.Second
)

type ApprovalScope string

const (
	// APIRead is retained as the approval scope for GET/HEAD for wire compatibility;
	// it is not a declaration that the configured remote endpoint is side-effect-free.
	ApprovalScopeAPIRead      ApprovalScope = "automation.api.read"
	ApprovalScopeAPIMutate    ApprovalScope = "automation.api.mutate"
	ApprovalScopeScript       ApprovalScope = "automation.script.execute"
	ApprovalScopeDocker       ApprovalScope = "automation.docker.start"
	ApprovalScopeAgentRuntime ApprovalScope = "automation.agent-runtime.execute"
)

var (
	ErrApprovalProofRequired     = errors.New("action-bound approval proof is required")
	ErrApprovalProofInvalid      = errors.New("action-bound approval proof is invalid")
	ErrApprovalProofExpired      = errors.New("action-bound approval proof has expired")
	ErrApprovalProofConsumed     = errors.New("action-bound approval proof was already consumed")
	ErrApprovalDecisionMissing   = errors.New("recorded approval decision was not found")
	ErrActionApprovalNotRequired = errors.New("automation action does not require an execution approval proof")
)

// ApprovalProof is a short-lived, signed, single-use capability. It is
// intentionally internal: HTTP launch handlers cannot construct or submit one.
type ApprovalProof struct {
	ID               string        `json:"id"`
	OwnerIdentity    string        `json:"ownerIdentity"`
	AutomationID     uuid.UUID     `json:"automationId"`
	ActionDigest     string        `json:"actionDigest"`
	Scope            ApprovalScope `json:"scope"`
	ApprovalSourceID string        `json:"approvalSourceId"`
	IssuedAt         time.Time     `json:"issuedAt"`
	ExpiresAt        time.Time     `json:"expiresAt"`
	Nonce            string        `json:"nonce"`
	Signature        string        `json:"signature"`
}

type ApprovalProofIssueRequest struct {
	OwnerIdentity    string
	AutomationID     uuid.UUID
	ActionDigest     string
	Scope            ApprovalScope
	ApprovalSourceID string
	TTL              time.Duration
}

// ApprovalDecisionRecord is the repository-backed authorization fact checked
// before a proof can be minted. ActionDigest binds the decision to the exact
// automation configuration, task, project, policy snapshot, and action scope.
// Review notes are deliberately excluded so secrets cannot become capability
// material or leak through launch diagnostics.
type ApprovalDecisionRecord struct {
	SourceID      string
	DecisionType  string
	OwnerIdentity string
	AutomationID  uuid.UUID
	WorkflowID    uuid.UUID
	ActionDigest  string
	Scope         ApprovalScope
	ApprovedAt    time.Time
}

// TaskApprovalDecisionRequest is accepted only from the task package after it
// has verified an owner-scoped queued review decision, including the historical
// configuration snapshot in its immutable request digest.
type TaskApprovalDecisionRequest struct {
	OwnerIdentity         string
	Task                  string
	ProjectKey            string
	MandateID             string
	ApprovalSourceID      string
	ApprovalBindingDigest string
	ReviewConfiguration   *ReviewConfigurationSnapshot `json:"-"`
	ApprovedAt            time.Time
}

const reviewConfigurationSnapshotVersion = "automation-review-configuration.v1"

// ReviewConfigurationSnapshot is captured before task review, independently of
// the task's inferred execution goal. Only its digest, not secrets, is retained.
type ReviewConfigurationSnapshot struct {
	Version             string        `json:"version"`
	AutomationID        uuid.UUID     `json:"automationId"`
	Scope               ApprovalScope `json:"scope"`
	ConfigurationDigest string        `json:"configurationDigest"`
}

// ReviewConfigurationInspector reads configuration without recording approval
// or issuing authority. Task review persists the returned value before approval.
type ReviewConfigurationInspector interface {
	InspectReviewConfiguration(id uuid.UUID) (*ReviewConfigurationSnapshot, error)
}

type ContextualReviewConfigurationInspector interface {
	InspectReviewConfigurationContext(context.Context, uuid.UUID) (*ReviewConfigurationSnapshot, error)
}

var ErrReviewConfigurationContextUnavailable = errors.New("owned review configuration inspection is unavailable")

func ValidateReviewConfigurationSnapshot(snapshot *ReviewConfigurationSnapshot, id uuid.UUID) error {
	if snapshot == nil {
		return fmt.Errorf("pre-review automation configuration snapshot is missing; create a new review")
	}
	if snapshot.Version != reviewConfigurationSnapshotVersion {
		return fmt.Errorf("pre-review automation configuration snapshot version is unsupported; create a new review")
	}
	if id == uuid.Nil || snapshot.AutomationID != id {
		return fmt.Errorf("pre-review automation configuration snapshot target does not match")
	}
	if !snapshot.Scope.valid() {
		return fmt.Errorf("pre-review automation configuration snapshot scope is invalid")
	}
	digest, err := hex.DecodeString(snapshot.ConfigurationDigest)
	if err != nil || len(digest) != sha256.Size || snapshot.ConfigurationDigest != strings.ToLower(snapshot.ConfigurationDigest) {
		return fmt.Errorf("pre-review automation configuration snapshot digest is invalid")
	}
	return nil
}

// ValidateTaskApprovalDecisionRequest applies the same freshness and digest
// contract used when the automation service records a trusted task approval.
// It validates evidence only; it does not record approval or grant authority.
func ValidateTaskApprovalDecisionRequest(request TaskApprovalDecisionRequest, now time.Time) error {
	if strings.TrimSpace(request.OwnerIdentity) == "" {
		return fmt.Errorf("approval decision owner identity is required")
	}
	if _, _, err := approvalSourceKind(request.ApprovalSourceID); err != nil {
		return err
	}
	digest := strings.ToLower(strings.TrimSpace(request.ApprovalBindingDigest))
	if digest != "" {
		decoded, err := hex.DecodeString(digest)
		if err != nil || len(decoded) != sha256.Size {
			return fmt.Errorf("approval decision action digest is invalid")
		}
	}
	approvedAt := request.ApprovedAt.UTC()
	now = now.UTC()
	if approvedAt.IsZero() {
		return fmt.Errorf("approval decision time is required")
	}
	if approvedAt.After(now.Add(maximumApprovalDecisionFutureSkew)) {
		return fmt.Errorf("approval decision time is in the future")
	}
	if now.Sub(approvedAt) > maximumApprovalDecisionAge {
		return fmt.Errorf("approval decision is stale")
	}
	return nil
}

// ApprovalDecisionRecorder is an internal process boundary used by the task
// engine to persist an exact task-review approval before asking for a proof.
type ApprovalDecisionRecorder interface {
	RecordApprovalDecision(id uuid.UUID, request TaskApprovalDecisionRequest) error
}

// TaskApprovalProofRequest is the trusted in-process handoff from a recorded
// human review to one exact automation action. It is deliberately not part of
// the HTTP automation API.
type TaskApprovalProofRequest struct {
	OwnerIdentity    string
	Task             string
	OriginalRequest  string
	ProjectKey       string
	MandateID        string
	WorkflowID       string
	ApprovalSourceID string
	TTL              time.Duration
}

type ApprovalProofIssuer interface {
	IssueApprovalProof(id uuid.UUID, request TaskApprovalProofRequest) (*ApprovalProof, error)
}

// WorkflowApprovalBindingPreparer exposes the deterministic action identity a
// workflow decision must persist before that decision can authorize execution.
// It does not record approval, mint a proof, or execute the automation.
type WorkflowApprovalBindingPreparer interface {
	PrepareWorkflowApprovalBinding(id uuid.UUID, request TaskLaunchRequest) (string, error)
}

type ActionApprovalRequirementInspector interface {
	ActionApprovalRequired(id uuid.UUID) (bool, error)
}

type ApprovalProofExpectation struct {
	OwnerIdentity    string
	AutomationID     uuid.UUID
	ActionDigest     string
	Scope            ApprovalScope
	ApprovalSourceID string
}

type ApprovalProofService interface {
	Issue(request ApprovalProofIssueRequest) (*ApprovalProof, error)
	VerifyAndConsume(ctx context.Context, proof *ApprovalProof, expected ApprovalProofExpectation) error
}

const approvalProofConsumptionContractVersion = "automation-approval-proof-consumption.v1"

const approvalProofConsumptionTimeout = 30 * time.Second

var ErrApprovalProofConsumptionUnconfirmed = errors.New("approval proof consumption requires reconciliation")

type ApprovalProofConsumption struct {
	ContractVersion  string
	ProofID          uuid.UUID
	OwnerIdentity    string
	AutomationID     uuid.UUID
	ActionDigest     string
	Scope            ApprovalScope
	ApprovalSourceID string
	NonceDigest      string
	SignatureDigest  string
	RecordDigest     string
	IssuedAt         time.Time
	ExpiresAt        time.Time
	ConsumedAt       time.Time
}

type ApprovalProofConsumptionStore interface {
	Consume(ctx context.Context, consumption ApprovalProofConsumption) error
}

type approvalProofService struct {
	key   []byte
	now   func() time.Time
	store ApprovalProofConsumptionStore
}

type memoryApprovalProofConsumptionStore struct {
	mu       sync.Mutex
	consumed map[string]time.Time
}

type unavailableApprovalProofService struct {
	err error
}

func NewInMemoryApprovalProofService(secret []byte, now func() time.Time) (ApprovalProofService, error) {
	return NewApprovalProofService(secret, &memoryApprovalProofConsumptionStore{
		consumed: make(map[string]time.Time),
	}, now)
}

func NewApprovalProofService(
	secret []byte,
	store ApprovalProofConsumptionStore,
	now func() time.Time,
) (ApprovalProofService, error) {
	if len(secret) < 32 {
		return nil, fmt.Errorf("approval proof signing secret must contain at least 32 bytes")
	}
	if store == nil {
		return nil, fmt.Errorf("approval proof consumption store is required")
	}
	if now == nil {
		now = time.Now
	}
	return &approvalProofService{
		key:   append([]byte(nil), secret...),
		now:   now,
		store: store,
	}, nil
}

func newDefaultApprovalProofService() ApprovalProofService {
	return unavailableApprovalProofService{err: errors.New(
		"durable approval proof service must be injected explicitly",
	)}
}

func (s unavailableApprovalProofService) Issue(ApprovalProofIssueRequest) (*ApprovalProof, error) {
	return nil, fmt.Errorf("approval proof service is unavailable: %w", s.err)
}

func (s unavailableApprovalProofService) VerifyAndConsume(context.Context, *ApprovalProof, ApprovalProofExpectation) error {
	return fmt.Errorf("approval proof service is unavailable: %w", s.err)
}

func (s *approvalProofService) Issue(request ApprovalProofIssueRequest) (*ApprovalProof, error) {
	request.OwnerIdentity = strings.TrimSpace(request.OwnerIdentity)
	request.ActionDigest = strings.ToLower(strings.TrimSpace(request.ActionDigest))
	request.ApprovalSourceID = strings.TrimSpace(request.ApprovalSourceID)
	if err := validateApprovalBinding(
		request.OwnerIdentity,
		request.AutomationID,
		request.ActionDigest,
		request.Scope,
		request.ApprovalSourceID,
	); err != nil {
		return nil, err
	}
	ttl := request.TTL
	if ttl == 0 {
		ttl = defaultApprovalProofTTL
	}
	if ttl <= 0 || ttl > maximumApprovalProofTTL {
		return nil, fmt.Errorf("approval proof TTL must be between 1ns and %s", maximumApprovalProofTTL)
	}
	nonceBytes := make([]byte, 24)
	if _, err := rand.Read(nonceBytes); err != nil {
		return nil, fmt.Errorf("generate approval proof nonce: %w", err)
	}
	now := s.now().UTC()
	proof := &ApprovalProof{
		ID:               uuid.NewString(),
		OwnerIdentity:    request.OwnerIdentity,
		AutomationID:     request.AutomationID,
		ActionDigest:     request.ActionDigest,
		Scope:            request.Scope,
		ApprovalSourceID: request.ApprovalSourceID,
		IssuedAt:         now,
		ExpiresAt:        now.Add(ttl),
		Nonce:            base64.RawURLEncoding.EncodeToString(nonceBytes),
	}
	proof.Signature = s.sign(proof)
	return proof, nil
}

func (s *approvalProofService) VerifyAndConsume(
	ctx context.Context,
	proof *ApprovalProof,
	expected ApprovalProofExpectation,
) error {
	if ctx == nil {
		return fmt.Errorf("%w: verification context is required", ErrApprovalProofInvalid)
	}
	ctx, cancel := context.WithTimeout(ctx, approvalProofConsumptionTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	if proof == nil {
		return ErrApprovalProofRequired
	}
	expected.OwnerIdentity = strings.TrimSpace(expected.OwnerIdentity)
	expected.ActionDigest = strings.ToLower(strings.TrimSpace(expected.ActionDigest))
	expected.ApprovalSourceID = strings.TrimSpace(expected.ApprovalSourceID)
	if err := validateApprovalBinding(
		expected.OwnerIdentity,
		expected.AutomationID,
		expected.ActionDigest,
		expected.Scope,
		expected.ApprovalSourceID,
	); err != nil {
		return fmt.Errorf("%w: invalid launcher expectation: %v", ErrApprovalProofInvalid, err)
	}
	if err := validateApprovalBinding(
		strings.TrimSpace(proof.OwnerIdentity),
		proof.AutomationID,
		strings.ToLower(strings.TrimSpace(proof.ActionDigest)),
		proof.Scope,
		strings.TrimSpace(proof.ApprovalSourceID),
	); err != nil {
		return fmt.Errorf("%w: %v", ErrApprovalProofInvalid, err)
	}
	if strings.TrimSpace(proof.ID) == "" || strings.TrimSpace(proof.Nonce) == "" || strings.TrimSpace(proof.Signature) == "" {
		return fmt.Errorf("%w: proof identity, nonce, and signature are required", ErrApprovalProofInvalid)
	}
	if !hmac.Equal([]byte(s.sign(proof)), []byte(proof.Signature)) {
		return fmt.Errorf("%w: signature mismatch", ErrApprovalProofInvalid)
	}
	if proof.OwnerIdentity != expected.OwnerIdentity {
		return fmt.Errorf("%w: owner mismatch", ErrApprovalProofInvalid)
	}
	if proof.AutomationID != expected.AutomationID {
		return fmt.Errorf("%w: automation mismatch", ErrApprovalProofInvalid)
	}
	if proof.ActionDigest != expected.ActionDigest {
		return fmt.Errorf("%w: action digest mismatch", ErrApprovalProofInvalid)
	}
	if proof.Scope != expected.Scope {
		return fmt.Errorf("%w: scope mismatch", ErrApprovalProofInvalid)
	}
	if proof.ApprovalSourceID != expected.ApprovalSourceID {
		return fmt.Errorf("%w: approval source mismatch", ErrApprovalProofInvalid)
	}
	now := s.now().UTC()
	if proof.IssuedAt.IsZero() || proof.ExpiresAt.IsZero() ||
		proof.ExpiresAt.After(proof.IssuedAt.Add(maximumApprovalProofTTL)) ||
		now.Before(proof.IssuedAt.Add(-5*time.Second)) {
		return fmt.Errorf("%w: invalid issuance window", ErrApprovalProofInvalid)
	}
	if !now.Before(proof.ExpiresAt) {
		return ErrApprovalProofExpired
	}

	proofID, err := uuid.Parse(strings.TrimSpace(proof.ID))
	if err != nil || proofID == uuid.Nil {
		return fmt.Errorf("%w: proof identity must be a UUID", ErrApprovalProofInvalid)
	}
	consumption := ApprovalProofConsumption{
		ContractVersion:  approvalProofConsumptionContractVersion,
		ProofID:          proofID,
		OwnerIdentity:    proof.OwnerIdentity,
		AutomationID:     proof.AutomationID,
		ActionDigest:     proof.ActionDigest,
		Scope:            proof.Scope,
		ApprovalSourceID: proof.ApprovalSourceID,
		NonceDigest:      sha256Hex(proof.Nonce),
		SignatureDigest:  sha256Hex(proof.Signature),
		IssuedAt:         proof.IssuedAt.UTC(),
		ExpiresAt:        proof.ExpiresAt.UTC(),
		ConsumedAt:       now,
	}
	consumption.RecordDigest = approvalProofConsumptionDigest(consumption)
	if err := ctx.Err(); err != nil {
		return err
	}
	err = s.store.Consume(ctx, consumption)
	if contextErr := ctx.Err(); contextErr != nil {
		return errors.Join(ErrApprovalProofConsumptionUnconfirmed, err, contextErr)
	}
	if err != nil {
		if errors.Is(err, ErrApprovalProofConsumed) {
			return err
		}
		return errors.Join(ErrApprovalProofConsumptionUnconfirmed, err)
	}
	return nil
}

func (s *memoryApprovalProofConsumptionStore) Consume(
	ctx context.Context,
	consumption ApprovalProofConsumption,
) error {
	if ctx == nil {
		return fmt.Errorf("approval proof consumption context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateApprovalProofConsumption(consumption); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	for id, expiresAt := range s.consumed {
		if !consumption.ConsumedAt.Before(expiresAt) {
			delete(s.consumed, id)
		}
	}
	key := consumption.OwnerIdentity + "\x00" + consumption.ProofID.String()
	if _, exists := s.consumed[key]; exists {
		return ErrApprovalProofConsumed
	}
	s.consumed[key] = consumption.ExpiresAt
	return nil
}

// ValidateIssuedApprovalProofEnvelope lets a caller fail closed before it
// invokes LaunchTask when a faulty issuer returns nil or malformed capability
// material. Signature and action-digest verification remain the launcher's
// responsibility because only the proof service owns the signing key and
// current automation configuration.
func ValidateIssuedApprovalProofEnvelope(
	proof *ApprovalProof,
	ownerIdentity string,
	automationID uuid.UUID,
	approvalSourceID string,
	now time.Time,
) error {
	if proof == nil {
		return ErrApprovalProofRequired
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	if err := validateApprovalBinding(
		strings.TrimSpace(proof.OwnerIdentity),
		proof.AutomationID,
		strings.ToLower(strings.TrimSpace(proof.ActionDigest)),
		proof.Scope,
		strings.TrimSpace(proof.ApprovalSourceID),
	); err != nil {
		return fmt.Errorf("%w: %v", ErrApprovalProofInvalid, err)
	}
	if strings.TrimSpace(proof.ID) == "" ||
		strings.TrimSpace(proof.Nonce) == "" ||
		strings.TrimSpace(proof.Signature) == "" {
		return fmt.Errorf("%w: proof identity, nonce, and signature are required", ErrApprovalProofInvalid)
	}
	if proof.OwnerIdentity != strings.TrimSpace(ownerIdentity) {
		return fmt.Errorf("%w: owner mismatch", ErrApprovalProofInvalid)
	}
	if proof.AutomationID != automationID {
		return fmt.Errorf("%w: automation mismatch", ErrApprovalProofInvalid)
	}
	if proof.ApprovalSourceID != strings.TrimSpace(approvalSourceID) {
		return fmt.Errorf("%w: approval source mismatch", ErrApprovalProofInvalid)
	}
	if proof.IssuedAt.IsZero() || proof.ExpiresAt.IsZero() ||
		proof.ExpiresAt.After(proof.IssuedAt.Add(maximumApprovalProofTTL)) ||
		now.Before(proof.IssuedAt.Add(-5*time.Second)) {
		return fmt.Errorf("%w: invalid issuance window", ErrApprovalProofInvalid)
	}
	if !now.Before(proof.ExpiresAt) {
		return ErrApprovalProofExpired
	}
	return nil
}

func (s *approvalProofService) sign(proof *ApprovalProof) string {
	payload := struct {
		ID               string        `json:"id"`
		OwnerIdentity    string        `json:"ownerIdentity"`
		AutomationID     string        `json:"automationId"`
		ActionDigest     string        `json:"actionDigest"`
		Scope            ApprovalScope `json:"scope"`
		ApprovalSourceID string        `json:"approvalSourceId"`
		IssuedAtUnixNano int64         `json:"issuedAtUnixNano"`
		ExpiresUnixNano  int64         `json:"expiresAtUnixNano"`
		Nonce            string        `json:"nonce"`
	}{
		ID:               proof.ID,
		OwnerIdentity:    proof.OwnerIdentity,
		AutomationID:     proof.AutomationID.String(),
		ActionDigest:     proof.ActionDigest,
		Scope:            proof.Scope,
		ApprovalSourceID: proof.ApprovalSourceID,
		IssuedAtUnixNano: proof.IssuedAt.UnixNano(),
		ExpiresUnixNano:  proof.ExpiresAt.UnixNano(),
		Nonce:            proof.Nonce,
	}
	encoded, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write(encoded)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func sha256Hex(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func approvalProofConsumptionDigest(value ApprovalProofConsumption) string {
	payload := struct {
		ContractVersion  string        `json:"contractVersion"`
		ProofID          string        `json:"proofId"`
		OwnerIdentity    string        `json:"ownerIdentity"`
		AutomationID     string        `json:"automationId"`
		ActionDigest     string        `json:"actionDigest"`
		Scope            ApprovalScope `json:"scope"`
		ApprovalSourceID string        `json:"approvalSourceId"`
		NonceDigest      string        `json:"nonceDigest"`
		SignatureDigest  string        `json:"signatureDigest"`
		IssuedAt         string        `json:"issuedAt"`
		ExpiresAt        string        `json:"expiresAt"`
		ConsumedAt       string        `json:"consumedAt"`
	}{
		ContractVersion:  value.ContractVersion,
		ProofID:          value.ProofID.String(),
		OwnerIdentity:    value.OwnerIdentity,
		AutomationID:     value.AutomationID.String(),
		ActionDigest:     value.ActionDigest,
		Scope:            value.Scope,
		ApprovalSourceID: value.ApprovalSourceID,
		NonceDigest:      value.NonceDigest,
		SignatureDigest:  value.SignatureDigest,
		IssuedAt:         value.IssuedAt.UTC().Format(time.RFC3339Nano),
		ExpiresAt:        value.ExpiresAt.UTC().Format(time.RFC3339Nano),
		ConsumedAt:       value.ConsumedAt.UTC().Format(time.RFC3339Nano),
	}
	encoded, _ := json.Marshal(payload)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func validateApprovalProofConsumption(value ApprovalProofConsumption) error {
	if value.ContractVersion != approvalProofConsumptionContractVersion {
		return fmt.Errorf("approval proof consumption contract version is invalid")
	}
	if value.ProofID == uuid.Nil {
		return fmt.Errorf("approval proof consumption proof ID is required")
	}
	if err := validateApprovalBinding(
		strings.TrimSpace(value.OwnerIdentity),
		value.AutomationID,
		strings.ToLower(strings.TrimSpace(value.ActionDigest)),
		value.Scope,
		strings.TrimSpace(value.ApprovalSourceID),
	); err != nil {
		return err
	}
	for label, digest := range map[string]string{
		"nonce":     value.NonceDigest,
		"signature": value.SignatureDigest,
		"record":    value.RecordDigest,
	} {
		decoded, err := hex.DecodeString(strings.ToLower(strings.TrimSpace(digest)))
		if err != nil || len(decoded) != sha256.Size {
			return fmt.Errorf("approval proof consumption %s digest must be SHA-256", label)
		}
	}
	if value.IssuedAt.IsZero() || value.ExpiresAt.IsZero() || value.ConsumedAt.IsZero() ||
		!value.ExpiresAt.After(value.IssuedAt) ||
		value.ExpiresAt.After(value.IssuedAt.Add(maximumApprovalProofTTL)) ||
		value.ConsumedAt.Before(value.IssuedAt.Add(-maximumApprovalDecisionFutureSkew)) ||
		!value.ConsumedAt.Before(value.ExpiresAt) {
		return fmt.Errorf("approval proof consumption timestamps are invalid")
	}
	if approvalProofConsumptionDigest(value) != value.RecordDigest {
		return fmt.Errorf("approval proof consumption record digest is invalid")
	}
	return nil
}

func validateApprovalBinding(
	ownerIdentity string,
	automationID uuid.UUID,
	actionDigest string,
	scope ApprovalScope,
	approvalSourceID string,
) error {
	if ownerIdentity == "" {
		return fmt.Errorf("owner identity is required")
	}
	if automationID == uuid.Nil {
		return fmt.Errorf("automation ID is required")
	}
	decodedDigest, err := hex.DecodeString(actionDigest)
	if err != nil || len(decodedDigest) != sha256.Size {
		return fmt.Errorf("action digest must be a SHA-256 hex digest")
	}
	if !scope.valid() {
		return fmt.Errorf("approval scope is not supported")
	}
	if approvalSourceID == "" || len(approvalSourceID) > 256 ||
		strings.ContainsAny(approvalSourceID, "\r\n\x00") {
		return fmt.Errorf("approval source ID is required and must be a single bounded value")
	}
	if _, _, err := approvalSourceKind(approvalSourceID); err != nil {
		return err
	}
	return nil
}

func (s ApprovalScope) valid() bool {
	switch s {
	case ApprovalScopeAPIRead, ApprovalScopeAPIMutate, ApprovalScopeScript, ApprovalScopeDocker, ApprovalScopeAgentRuntime:
		return true
	default:
		return false
	}
}

func approvalScopeForAutomation(automation *models.Automation) (ApprovalScope, bool) {
	if automation == nil {
		return "", false
	}
	switch strings.ToLower(strings.TrimSpace(automation.LaunchType)) {
	case "api":
		method, _ := parseLaunchMethodTarget(automation.LaunchTarget, http.MethodPost)
		switch method {
		case http.MethodGet, http.MethodHead:
			// The HTTP verb does not establish that a remote endpoint is read-only.
			// No digest-bound immutable read-only declaration exists on Automation,
			// so GET and HEAD require the same owner approval proof as other effects.
			return ApprovalScopeAPIRead, true
		case http.MethodPost:
			return ApprovalScopeAPIMutate, true
		default:
			return "", false
		}
	case "script":
		return ApprovalScopeScript, true
	case "docker_service":
		return ApprovalScopeDocker, true
	case "agent_runtime":
		return ApprovalScopeAgentRuntime, true
	default:
		return "", false
	}
}

func approvalSourceKind(sourceID string) (string, uuid.UUID, error) {
	sourceID = strings.TrimSpace(sourceID)
	for _, prefix := range []string{"task-review:", "workflow-decision:"} {
		if !strings.HasPrefix(sourceID, prefix) {
			continue
		}
		id, err := uuid.Parse(strings.TrimPrefix(sourceID, prefix))
		if err != nil || id == uuid.Nil {
			return "", uuid.Nil, fmt.Errorf("approval source must contain a valid decision UUID")
		}
		return strings.TrimSuffix(prefix, ":"), id, nil
	}
	return "", uuid.Nil, fmt.Errorf("approval source type is not supported")
}

func validateApprovalDecisionRecord(record *ApprovalDecisionRecord) error {
	if record == nil {
		return ErrApprovalDecisionMissing
	}
	kind, _, err := approvalSourceKind(record.SourceID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(record.DecisionType) != kind {
		return fmt.Errorf("approval decision type does not match its source")
	}
	if err := validateApprovalBinding(
		strings.TrimSpace(record.OwnerIdentity),
		record.AutomationID,
		strings.ToLower(strings.TrimSpace(record.ActionDigest)),
		record.Scope,
		strings.TrimSpace(record.SourceID),
	); err != nil {
		return err
	}
	if kind == "workflow-decision" && record.WorkflowID == uuid.Nil {
		return fmt.Errorf("workflow approval decision is missing its workflow binding")
	}
	if record.ApprovedAt.IsZero() {
		return fmt.Errorf("approval decision time is required")
	}
	return nil
}

func validateApprovalDecisionFreshness(record *ApprovalDecisionRecord, now time.Time) error {
	if err := validateApprovalDecisionRecord(record); err != nil {
		return err
	}
	approvedAt := record.ApprovedAt.UTC()
	now = now.UTC()
	if approvedAt.After(now.Add(maximumApprovalDecisionFutureSkew)) {
		return fmt.Errorf("approval decision time is in the future")
	}
	if now.Sub(approvedAt) > maximumApprovalDecisionAge {
		return fmt.Errorf("approval decision is stale")
	}
	return nil
}

func sameApprovalDecision(left, right *ApprovalDecisionRecord) bool {
	if left == nil || right == nil {
		return false
	}
	return left.SourceID == right.SourceID &&
		left.DecisionType == right.DecisionType &&
		left.OwnerIdentity == right.OwnerIdentity &&
		left.AutomationID == right.AutomationID &&
		left.WorkflowID == right.WorkflowID &&
		left.ActionDigest == right.ActionDigest &&
		left.Scope == right.Scope &&
		left.ApprovedAt.UTC().Equal(right.ApprovedAt.UTC())
}

func parseWorkflowApprovalBinding(value string) (ApprovalScope, string, error) {
	const prefix = "automation-action:"
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, prefix) {
		return "", "", fmt.Errorf("workflow decision has no exact automation action binding")
	}
	parts := strings.Split(strings.TrimPrefix(value, prefix), ":")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("workflow decision automation binding is malformed")
	}
	scope := ApprovalScope(strings.TrimSpace(parts[0]))
	digest := strings.ToLower(strings.TrimSpace(parts[1]))
	if !scope.valid() {
		return "", "", fmt.Errorf("workflow decision approval scope is not supported")
	}
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != sha256.Size {
		return "", "", fmt.Errorf("workflow decision action digest is invalid")
	}
	return scope, digest, nil
}

func automationActionDigest(automation *models.Automation, request TaskLaunchRequest) string {
	return automationActionDigestWithPolicy(automation, request, approvalPolicySnapshot())
}

func automationActionDigestWithPolicy(automation *models.Automation, request TaskLaunchRequest, policySnapshot []string) string {
	type actionIdentity struct {
		AutomationID       string   `json:"automationId"`
		Name               string   `json:"name"`
		LaunchType         string   `json:"launchType"`
		LaunchTarget       string   `json:"launchTarget"`
		RuntimeType        string   `json:"runtimeType"`
		RuntimeModel       string   `json:"runtimeModel,omitempty"`
		ServiceName        string   `json:"serviceName"`
		RoutePath          string   `json:"routePath"`
		PublicURL          string   `json:"publicUrl"`
		LocalURL           string   `json:"localUrl"`
		Host               string   `json:"host"`
		Port               int      `json:"port"`
		ExpectedHTTPStatus int      `json:"expectedHttpStatus"`
		Task               string   `json:"task"`
		ProjectKey         string   `json:"projectKey"`
		MandateID          string   `json:"mandateId,omitempty"`
		PolicySnapshot     []string `json:"policySnapshot"`
	}
	identity := actionIdentity{
		AutomationID:       automation.ID.String(),
		Name:               strings.TrimSpace(automation.Name),
		LaunchType:         strings.ToLower(strings.TrimSpace(automation.LaunchType)),
		LaunchTarget:       strings.TrimSpace(automation.LaunchTarget),
		RuntimeType:        strings.ToLower(strings.TrimSpace(automation.RuntimeType)),
		RuntimeModel:       automation.RuntimeModel,
		ServiceName:        strings.TrimSpace(automation.ServiceName),
		RoutePath:          strings.TrimSpace(automation.RoutePath),
		PublicURL:          strings.TrimSpace(automation.PublicURL),
		LocalURL:           strings.TrimSpace(automation.LocalURL),
		Host:               strings.TrimSpace(automation.Host),
		Port:               automation.Port,
		ExpectedHTTPStatus: automation.ExpectedHTTPStatus,
		Task:               strings.TrimSpace(request.Task),
		ProjectKey:         strings.TrimSpace(request.ProjectKey),
		MandateID:          strings.TrimSpace(request.MandateID),
		PolicySnapshot:     policySnapshot,
	}
	encoded, _ := json.Marshal(identity)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func approvalPolicySnapshot() []string {
	keys := []string{
		"AUTOMATION_API_ALLOWED_HOSTS",
		"AUTOMATION_API_ALLOW_LINK_LOCAL",
		"AUTOMATION_SCRIPT_EXECUTION_ENABLED",
		"AUTOMATION_SCRIPT_DIR",
		"AUTOMATION_SCRIPT_ENV_ALLOWLIST",
		"AUTOMATION_SCRIPT_OUTPUT_LIMIT_BYTES",
		"AUTOMATION_SCRIPT_SHA256_ALLOWLIST",
		"AUTOMATION_SCRIPT_TIMEOUT_SECONDS",
		"AUTOMATION_DOCKER_CONTROL_ENABLED",
		"AUTOMATION_DOCKER_ALLOWED_CONTAINERS",
		"AUTOMATION_DOCKER_SOCKET",
		"AGENT_RUNTIME_ALLOWED_HOSTS",
		"AGENT_RUNTIME_OUTPUT_LIMIT_BYTES",
		"AGENT_RUNTIME_WORKSPACE_ROOT",
		"HERMES_ACP_ENABLED",
		"HERMES_AGENT_ENABLED",
		"HERMES_CRON_ENABLED",
		"HERMES_ENV_ALLOWLIST",
		"HERMES_EXECUTABLE",
		"HERMES_GATEWAY_ENABLED",
		"HERMES_HOME",
		"HERMES_IGNORE_USER_CONFIG",
		"HERMES_MAX_TURNS",
		"HERMES_MCP_ENABLED",
		"HERMES_MEMORY_SYNC_ENABLED",
		"HERMES_MOA_ENABLED",
		"HERMES_PROFILE",
		"HERMES_SKILLS",
		"HERMES_SUBAGENTS_ENABLED",
		"HERMES_TERMINAL_BACKENDS",
		"HERMES_TIMEOUT_SECONDS",
		"HERMES_TOOLSETS",
		"HERMES_WORKSPACE",
		"ODYSSEUS_AGENT_ALLOW_BASH",
		"ODYSSEUS_AGENT_ALLOW_RESEARCH",
		"ODYSSEUS_AGENT_ALLOW_WEB_SEARCH",
		"ODYSSEUS_AGENT_ENABLED",
		"ODYSSEUS_AGENT_MIGRATION_ENABLED",
		"ODYSSEUS_AGENT_SESSION_ID",
		"ODYSSEUS_AGENT_TIMEOUT_SECONDS",
		"ODYSSEUS_AGENT_WORKSPACE",
		"ODYSSEUS_API_TOKEN",
		"ODYSSEUS_BASE_URL",
		"ODYSSEUS_BROWSER_ENABLED",
		"ODYSSEUS_CALENDAR_ENABLED",
		"ODYSSEUS_CLAUDE_BRIDGE_ENABLED",
		"ODYSSEUS_CODEX_BRIDGE_ENABLED",
		"ODYSSEUS_COMPANION_ENABLED",
		"ODYSSEUS_CONTACTS_ENABLED",
		"ODYSSEUS_CONTEXT_BUDGET_ENABLED",
		"ODYSSEUS_COOKBOOK_ENABLED",
		"ODYSSEUS_DOCUMENTS_ENABLED",
		"ODYSSEUS_EMAIL_ENABLED",
		"ODYSSEUS_GALLERY_ENABLED",
		"ODYSSEUS_LOCAL_MODEL_DISCOVERY_ENABLED",
		"ODYSSEUS_MCP_ENABLED",
		"ODYSSEUS_MEMORY_SYNC_ENABLED",
		"ODYSSEUS_NOTES_ENABLED",
		"ODYSSEUS_RESEARCH_ENABLED",
		"ODYSSEUS_SEARCH_ENABLED",
		"ODYSSEUS_SHELL_ENABLED",
		"ODYSSEUS_STT_ENABLED",
		"ODYSSEUS_TASKS_ENABLED",
		"ODYSSEUS_TODOS_ENABLED",
		"ODYSSEUS_TTS_ENABLED",
		"ODYSSEUS_VAULT_ENABLED",
		"ODYSSEUS_WEBHOOKS_ENABLED",
		"OPENCLAW_AGENT_CLI_ENABLED",
		"OPENCLAW_AGENT_ENABLED",
		"OPENCLAW_ALLOW_HIGH_RISK_EXECUTION",
		"OPENCLAW_APP_SDK_ENABLED",
		"OPENCLAW_BROWSER_ENABLED",
		"OPENCLAW_CANVAS_ENABLED",
		"OPENCLAW_CHANNELS_ENABLED",
		"OPENCLAW_COMPANION_APPS",
		"OPENCLAW_CONFIG_PATH",
		"OPENCLAW_CRON_ENABLED",
		"OPENCLAW_ECOSYSTEM_ALLOWED_ROOTS",
		"OPENCLAW_ECOSYSTEM_PATH",
		"OPENCLAW_ENV_ALLOWLIST",
		"OPENCLAW_EXEC_APPROVALS_ENABLED",
		"OPENCLAW_EXECUTABLE",
		"OPENCLAW_GATEWAY_ENABLED",
		"OPENCLAW_GATEWAY_AUTH_DISCOVERY_ENABLED",
		"OPENCLAW_GATEWAY_ALLOWED_MODELS",
		"OPENCLAW_GATEWAY_TOKEN",
		"OPENCLAW_GATEWAY_URL",
		"OPENCLAW_HOST_TOOLS_ENABLED",
		"OPENCLAW_LOCAL_MODELS_ENABLED",
		"OPENCLAW_MCP_ENABLED",
		"OPENCLAW_MEMORY_ENABLED",
		"OPENCLAW_MESSAGES_ENABLED",
		"OPENCLAW_MULTI_AGENT_ENABLED",
		"OPENCLAW_NODES_ENABLED",
		"OPENCLAW_PAIRING_ENABLED",
		"OPENCLAW_PLUGIN_SDK_ENABLED",
		"OPENCLAW_PLUGINS_ENABLED",
		"OPENCLAW_PROVIDERS_ENABLED",
		"OPENCLAW_PUBLIC_POSTING_ENABLED",
		"OPENCLAW_SANDBOX_DOCKER_ENABLED",
		"OPENCLAW_SANDBOX_MODE",
		"OPENCLAW_SANDBOX_OPENSHELL_ENABLED",
		"OPENCLAW_SANDBOX_REQUIRED",
		"OPENCLAW_SANDBOX_SSH_ENABLED",
		"OPENCLAW_SKILLS_ENABLED",
		"OPENCLAW_STATE_DIR",
		"OPENCLAW_TALK_ENABLED",
		"OPENCLAW_THINKING",
		"OPENCLAW_TIMEOUT_SECONDS",
		"OPENCLAW_VOICE_ENABLED",
		"OPENCLAW_WEB_SEARCH_ENABLED",
		"OPENCLAW_WEBCHAT_ENABLED",
		"OPENCLAW_WORKSPACE",
	}
	for _, key := range strings.Split(os.Getenv("AUTOMATION_SCRIPT_ENV_ALLOWLIST"), ",") {
		key = strings.TrimSpace(key)
		if key != "" {
			keys = append(keys, key)
		}
	}
	uniqueKeys := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		uniqueKeys[key] = struct{}{}
	}
	keys = keys[:0]
	for key := range uniqueKeys {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		values = append(values, key+"="+approvalPolicyValue(key))
	}
	return values
}

func approvalPolicyValue(key string) string {
	value := os.Getenv(key)
	upperKey := strings.ToUpper(key)
	if strings.Contains(upperKey, "TOKEN") ||
		strings.Contains(upperKey, "SECRET") ||
		strings.Contains(upperKey, "PASSWORD") ||
		strings.Contains(upperKey, "API_KEY") {
		if value == "" {
			return ""
		}
		digest := sha256.Sum256([]byte(value))
		return "sha256:" + hex.EncodeToString(digest[:])
	}
	return value
}
