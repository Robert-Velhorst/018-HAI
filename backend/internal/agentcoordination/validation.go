package agentcoordination

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"automation-hub-backend/internal/safety"

	"github.com/google/uuid"
)

var (
	ErrDuplicateDispatch   = errors.New("agent coordination dispatch already completed")
	ErrIdempotencyConflict = errors.New("agent coordination idempotency key was reused for different content")
)

type ValidationPolicy struct {
	SchemaVersion           string
	MaximumAuthority        int
	MaximumMessageTTL       time.Duration
	MaximumPayloadBytes     int
	AllowedMessageTypes     []MessageType
	AllowedConfidentiality  []Confidentiality
	AllowedExecutionModes   []ExecutionMode
	RequireProvenance       bool
	RequireDecisionEvidence bool
	RequireRedaction        bool
}

func DefaultValidationPolicy() ValidationPolicy {
	return ValidationPolicy{
		SchemaVersion:       "hai-agent-coordination-v1",
		MaximumAuthority:    4,
		MaximumMessageTTL:   24 * time.Hour,
		MaximumPayloadBytes: 64 * 1024,
		AllowedMessageTypes: []MessageType{
			MessageTypeRequest,
			MessageTypeProposal,
			MessageTypeEvidence,
			MessageTypeStatus,
			MessageTypeDecision,
			MessageTypeAcknowledgment,
			MessageTypeEscalation,
			MessageTypeConflict,
		},
		AllowedConfidentiality: []Confidentiality{
			ConfidentialityInternal,
			ConfidentialityRestricted,
		},
		AllowedExecutionModes: []ExecutionMode{
			ExecutionModePlanOnly,
			ExecutionModeRecommend,
			ExecutionModeDraft,
			ExecutionModeExecuteLowRisk,
		},
		RequireProvenance:       true,
		RequireDecisionEvidence: true,
		RequireRedaction:        true,
	}
}

func ValidateMessage(policy ValidationPolicy, message Message, now time.Time) error {
	if err := validatePolicy(policy); err != nil {
		return err
	}
	if err := requireUUID("message ID", message.ID); err != nil {
		return err
	}
	if err := requireUUID("idempotency key", message.IdempotencyKey); err != nil {
		return err
	}
	if err := requireUUID("correlation ID", message.CorrelationID); err != nil {
		return err
	}
	if message.CausationID != "" {
		if err := requireUUID("causation ID", message.CausationID); err != nil {
			return err
		}
		if message.CausationID == message.ID {
			return fmt.Errorf("causation ID cannot equal message ID")
		}
	}
	if strings.TrimSpace(message.SchemaVersion) != strings.TrimSpace(policy.SchemaVersion) {
		return fmt.Errorf("message schema version is not accepted")
	}
	if !containsMessageType(policy.AllowedMessageTypes, message.Type) {
		return fmt.Errorf("message type %q is not accepted", message.Type)
	}
	if !containsConfidentiality(policy.AllowedConfidentiality, message.Confidentiality) {
		return fmt.Errorf("message confidentiality %q is not accepted", message.Confidentiality)
	}
	if err := validateAgent("sender", message.Sender, policy.MaximumAuthority); err != nil {
		return err
	}
	if err := validateAgent("recipient", message.Recipient, policy.MaximumAuthority); err != nil {
		return err
	}
	if message.Sender.ID == message.Recipient.ID {
		return fmt.Errorf("message sender and recipient must differ")
	}
	if message.AuthorityLevel < 0 ||
		message.AuthorityLevel > policy.MaximumAuthority ||
		message.AuthorityLevel > message.Sender.AuthorityCeiling ||
		message.AuthorityLevel > message.Recipient.AuthorityCeiling {
		return fmt.Errorf("message authority exceeds an allowed ceiling")
	}
	if strings.TrimSpace(message.Payload.Schema) == "" ||
		strings.TrimSpace(message.Payload.Subject) == "" ||
		len(bytes.TrimSpace(message.Payload.Data)) == 0 ||
		!json.Valid(message.Payload.Data) {
		return fmt.Errorf("message payload requires a schema, subject, and valid JSON data")
	}
	if len(message.Payload.Data) > policy.MaximumPayloadBytes {
		return fmt.Errorf("message payload exceeds the configured byte limit")
	}
	if message.CreatedAt.IsZero() || message.ExpiresAt.IsZero() {
		return fmt.Errorf("message creation and expiry times are required")
	}
	createdAt := message.CreatedAt.UTC()
	expiresAt := message.ExpiresAt.UTC()
	now = now.UTC()
	if createdAt.After(now.Add(5 * time.Minute)) {
		return fmt.Errorf("message creation time cannot be in the future")
	}
	if !expiresAt.After(createdAt) || !expiresAt.After(now) {
		return fmt.Errorf("message expiry must be after creation and in the future")
	}
	if expiresAt.Sub(createdAt) > policy.MaximumMessageTTL {
		return fmt.Errorf("message expiry exceeds the configured TTL")
	}
	if policy.RequireProvenance && strings.TrimSpace(message.ProvenanceSummary) == "" {
		return fmt.Errorf("message provenance is required")
	}
	if policy.RequireDecisionEvidence &&
		message.Type == MessageTypeDecision &&
		len(normalizeStrings(message.EvidenceRefs)) == 0 {
		return fmt.Errorf("decision messages require evidence")
	}
	if policy.RequireRedaction && containsSecret(message) {
		return fmt.Errorf("message contains secret material")
	}
	digest, err := ComputeMessageDigest(message)
	if err != nil {
		return fmt.Errorf("compute message digest: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(message.PayloadDigest), digest) {
		return fmt.Errorf("message payload digest does not match the envelope")
	}
	return nil
}

func ComputeMessageDigest(message Message) (string, error) {
	payload := struct {
		SchemaVersion     string          `json:"schemaVersion"`
		CorrelationID     string          `json:"correlationId"`
		CausationID       string          `json:"causationId,omitempty"`
		Type              MessageType     `json:"type"`
		SenderID          string          `json:"senderId"`
		RecipientID       string          `json:"recipientId"`
		Confidentiality   Confidentiality `json:"confidentiality"`
		AuthorityLevel    int             `json:"authorityLevel"`
		PayloadSchema     string          `json:"payloadSchema"`
		PayloadSubject    string          `json:"payloadSubject"`
		PayloadData       json.RawMessage `json:"payloadData"`
		EvidenceRefs      []string        `json:"evidenceRefs,omitempty"`
		RequiresAck       bool            `json:"requiresAck"`
		HumanApprovalRef  string          `json:"humanApprovalRef,omitempty"`
		ProvenanceSummary string          `json:"provenanceSummary"`
	}{
		SchemaVersion:     strings.TrimSpace(message.SchemaVersion),
		CorrelationID:     strings.TrimSpace(message.CorrelationID),
		CausationID:       strings.TrimSpace(message.CausationID),
		Type:              message.Type,
		SenderID:          normalizeID(message.Sender.ID),
		RecipientID:       normalizeID(message.Recipient.ID),
		Confidentiality:   message.Confidentiality,
		AuthorityLevel:    message.AuthorityLevel,
		PayloadSchema:     strings.TrimSpace(message.Payload.Schema),
		PayloadSubject:    normalizeText(message.Payload.Subject),
		PayloadData:       bytes.TrimSpace(message.Payload.Data),
		EvidenceRefs:      normalizeStrings(message.EvidenceRefs),
		RequiresAck:       message.RequiresAck,
		HumanApprovalRef:  strings.TrimSpace(message.HumanApprovalRef),
		ProvenanceSummary: normalizeText(message.ProvenanceSummary),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func ValidateDelegation(policy ValidationPolicy, delegation DelegationEnvelope, now time.Time) error {
	if err := validatePolicy(policy); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"delegation ID":   delegation.ID,
		"task ID":         delegation.TaskID,
		"idempotency key": delegation.IdempotencyKey,
		"correlation ID":  delegation.CorrelationID,
	} {
		if err := requireUUID(name, value); err != nil {
			return err
		}
	}
	if err := validateAgent("principal", delegation.Principal, policy.MaximumAuthority); err != nil {
		return err
	}
	if err := validateAgent("delegate", delegation.Delegate, policy.MaximumAuthority); err != nil {
		return err
	}
	if delegation.Principal.ID == delegation.Delegate.ID {
		return fmt.Errorf("delegation principal and delegate must differ")
	}
	if normalizeText(delegation.Objective) == "" {
		return fmt.Errorf("delegation objective is required")
	}
	if len(normalizeStrings(delegation.SuccessCriteria)) == 0 {
		return fmt.Errorf("delegation success criteria are required")
	}
	if len(normalizeStrings(delegation.StopConditions)) == 0 {
		return fmt.Errorf("delegation stop conditions are required")
	}
	if len(normalizeStrings(delegation.ProhibitedActions)) == 0 {
		return fmt.Errorf("delegation prohibited actions are required")
	}
	if !containsExecutionMode(policy.AllowedExecutionModes, delegation.ExecutionMode) {
		return fmt.Errorf("delegation execution mode %q is not accepted", delegation.ExecutionMode)
	}
	if delegation.ApprovalMode != ApprovalNotRequired &&
		delegation.ApprovalMode != ApprovalBeforeExecution {
		return fmt.Errorf("delegation approval mode is invalid")
	}
	if delegation.RequiredAuthority < 0 ||
		delegation.RequiredAuthority > policy.MaximumAuthority ||
		delegation.RequiredAuthority > delegation.Principal.AuthorityCeiling ||
		delegation.RequiredAuthority > delegation.Delegate.AuthorityCeiling {
		return fmt.Errorf("delegation authority exceeds an allowed ceiling")
	}
	if delegation.ExecutionMode == ExecutionModeExecuteLowRisk &&
		delegation.RequiredAuthority > 2 &&
		delegation.ApprovalMode != ApprovalBeforeExecution {
		return fmt.Errorf("execution above low authority requires human approval")
	}
	if delegation.ApprovalMode == ApprovalBeforeExecution &&
		(delegation.Status == DelegationInProgress ||
			delegation.Status == DelegationCompleted) &&
		strings.TrimSpace(delegation.HumanApprovalRef) == "" {
		return fmt.Errorf("approved execution requires an external human approval reference")
	}
	if !validDelegationStatus(delegation.Status) {
		return fmt.Errorf("delegation status is invalid")
	}
	if delegation.CreatedAt.IsZero() || delegation.DueAt.IsZero() ||
		!delegation.DueAt.After(delegation.CreatedAt) ||
		!delegation.DueAt.After(now.UTC()) {
		return fmt.Errorf("delegation due time must be after creation and in the future")
	}
	if !delegation.UpdatedAt.IsZero() && delegation.UpdatedAt.Before(delegation.CreatedAt) {
		return fmt.Errorf("delegation update time cannot precede creation")
	}
	seenResources := map[string]struct{}{}
	for _, claim := range delegation.ResourceClaims {
		resource := normalizeID(claim.Resource)
		if resource == "" {
			return fmt.Errorf("resource claim requires a resource")
		}
		if claim.Access != ResourceRead && claim.Access != ResourceWrite {
			return fmt.Errorf("resource claim access is invalid")
		}
		if _, exists := seenResources[resource]; exists {
			return fmt.Errorf("delegation contains a duplicate resource claim")
		}
		seenResources[resource] = struct{}{}
	}
	if policy.RequireRedaction && delegationContainsSecret(delegation) {
		return fmt.Errorf("delegation contains secret material")
	}
	return nil
}

func ValidateAcknowledgment(message Message, acknowledgment Acknowledgment, now time.Time) error {
	return validateAcknowledgment(message, acknowledgment, now, true)
}

func validateAcknowledgment(message Message, acknowledgment Acknowledgment, now time.Time, requireFutureRetry bool) error {
	if err := requireUUID("acknowledgment ID", acknowledgment.ID); err != nil {
		return err
	}
	if err := requireUUID("acknowledgment idempotency key", acknowledgment.IdempotencyKey); err != nil {
		return err
	}
	if acknowledgment.MessageID != message.ID ||
		acknowledgment.CorrelationID != message.CorrelationID ||
		normalizeID(acknowledgment.RecipientID) != normalizeID(message.Recipient.ID) {
		return fmt.Errorf("acknowledgment does not match the message envelope")
	}
	switch acknowledgment.Status {
	case AcknowledgmentAccepted:
		if acknowledgment.RetryAfter != nil {
			return fmt.Errorf("accepted acknowledgment cannot set retry time")
		}
	case AcknowledgmentRejected:
		if normalizeText(acknowledgment.Reason) == "" {
			return fmt.Errorf("rejected acknowledgment requires a reason")
		}
	case AcknowledgmentDeferred:
		if acknowledgment.RetryAfter == nil ||
			!acknowledgment.RetryAfter.After(acknowledgment.CreatedAt) ||
			(requireFutureRetry && !acknowledgment.RetryAfter.After(now.UTC())) {
			return fmt.Errorf("deferred acknowledgment requires a future retry time")
		}
	default:
		return fmt.Errorf("acknowledgment status is invalid")
	}
	if acknowledgment.CreatedAt.IsZero() ||
		acknowledgment.CreatedAt.Before(message.CreatedAt) ||
		acknowledgment.CreatedAt.After(now.UTC().Add(5*time.Minute)) {
		return fmt.Errorf("acknowledgment creation time is invalid")
	}
	if safety.RedactSecrets(acknowledgment.Reason) != acknowledgment.Reason {
		return fmt.Errorf("acknowledgment contains secret material")
	}
	return nil
}

// ComputeAcknowledgmentDigest returns the stable content identity used by
// durable acknowledgment stores. The digest deliberately excludes storage
// metadata and cannot be interpreted as execution or approval authority.
func ComputeAcknowledgmentDigest(acknowledgment Acknowledgment) (string, error) {
	payload := struct {
		MessageID      string               `json:"messageId"`
		CorrelationID  string               `json:"correlationId"`
		RecipientID    string               `json:"recipientId"`
		Status         AcknowledgmentStatus `json:"status"`
		Reason         string               `json:"reason,omitempty"`
		CreatedAt      time.Time            `json:"createdAt"`
		RetryAfter     *time.Time           `json:"retryAfter,omitempty"`
		IdempotencyKey string               `json:"idempotencyKey"`
	}{
		MessageID:      strings.TrimSpace(acknowledgment.MessageID),
		CorrelationID:  strings.TrimSpace(acknowledgment.CorrelationID),
		RecipientID:    normalizeID(acknowledgment.RecipientID),
		Status:         acknowledgment.Status,
		Reason:         normalizeText(acknowledgment.Reason),
		CreatedAt:      acknowledgment.CreatedAt.UTC(),
		RetryAfter:     acknowledgmentTimePointer(acknowledgment.RetryAfter),
		IdempotencyKey: strings.TrimSpace(acknowledgment.IdempotencyKey),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func acknowledgmentTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := value.UTC()
	return &normalized
}

func validatePolicy(policy ValidationPolicy) error {
	if strings.TrimSpace(policy.SchemaVersion) == "" ||
		policy.MaximumAuthority < 0 ||
		policy.MaximumAuthority > 10 ||
		policy.MaximumMessageTTL <= 0 ||
		policy.MaximumPayloadBytes <= 0 ||
		len(policy.AllowedMessageTypes) == 0 ||
		len(policy.AllowedConfidentiality) == 0 ||
		len(policy.AllowedExecutionModes) == 0 {
		return fmt.Errorf("agent coordination validation policy is incomplete")
	}
	return nil
}

func validateAgent(name string, agent AgentRef, maximumAuthority int) error {
	if normalizeID(agent.ID) == "" || normalizeText(agent.Role) == "" {
		return fmt.Errorf("%s ID and role are required", name)
	}
	if agent.AuthorityCeiling < 0 || agent.AuthorityCeiling > maximumAuthority {
		return fmt.Errorf("%s authority ceiling is invalid", name)
	}
	return nil
}

func requireUUID(name, value string) error {
	if _, err := uuid.Parse(strings.TrimSpace(value)); err != nil {
		return fmt.Errorf("%s must be a UUID", name)
	}
	return nil
}

func containsSecret(message Message) bool {
	if jsonContainsSecret(message.Payload.Data) {
		return true
	}
	values := append([]string{
		message.Sender.ID,
		message.Sender.Role,
		message.Recipient.ID,
		message.Recipient.Role,
		message.Payload.Subject,
		string(message.Payload.Data),
		message.HumanApprovalRef,
		message.ProvenanceSummary,
	}, message.EvidenceRefs...)
	for _, value := range values {
		if safety.RedactSecrets(value) != value {
			return true
		}
	}
	return false
}

func jsonContainsSecret(raw json.RawMessage) bool {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return false
	}
	var inspect func(any) bool
	inspect = func(current any) bool {
		switch typed := current.(type) {
		case map[string]any:
			for key, child := range typed {
				normalizedKey := strings.ToLower(strings.NewReplacer(
					"-", "",
					"_", "",
					" ", "",
				).Replace(strings.TrimSpace(key)))
				switch normalizedKey {
				case "password",
					"passwd",
					"pwd",
					"secret",
					"token",
					"apikey",
					"accesstoken",
					"refreshtoken",
					"clientsecret",
					"authorization",
					"privatekey":
					return true
				}
				if inspect(child) {
					return true
				}
			}
		case []any:
			for _, child := range typed {
				if inspect(child) {
					return true
				}
			}
		case string:
			return safety.RedactSecrets(typed) != typed
		}
		return false
	}
	return inspect(value)
}

func delegationContainsSecret(delegation DelegationEnvelope) bool {
	values := []string{
		delegation.Objective,
		delegation.StatusReason,
		delegation.HumanApprovalRef,
	}
	values = append(values, delegation.SuccessCriteria...)
	values = append(values, delegation.StopConditions...)
	values = append(values, delegation.AllowedTools...)
	values = append(values, delegation.ProhibitedActions...)
	values = append(values, delegation.EvidenceRefs...)
	values = append(values, delegation.CompletionEvidence...)
	for _, value := range values {
		if safety.RedactSecrets(value) != value {
			return true
		}
	}
	return false
}

func normalizeID(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), "-"))
}

func normalizeText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func normalizeStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = normalizeText(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func containsMessageType(values []MessageType, target MessageType) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsConfidentiality(values []Confidentiality, target Confidentiality) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsExecutionMode(values []ExecutionMode, target ExecutionMode) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func validDelegationStatus(status DelegationStatus) bool {
	switch status {
	case DelegationProposed,
		DelegationAccepted,
		DelegationRejected,
		DelegationInProgress,
		DelegationBlocked,
		DelegationCompleted,
		DelegationCancelled,
		DelegationExpired,
		DelegationEscalated:
		return true
	default:
		return false
	}
}
