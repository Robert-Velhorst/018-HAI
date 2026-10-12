package opscontrol

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"automation-hub-backend/internal/executionauth"
)

// OwnerControlApprovalPrefix identifies the narrowly-scoped, owner-confirmed
// approvals used to weaken a runtime safety control. It is handled by the
// shared execution authorization ledger just like task and workflow approvals.
const OwnerControlApprovalPrefix = "opscontrol-owner:"

const ownerControlApprovalTTL = 5 * time.Minute

var ErrOwnerControlApprovalInvalid = errors.New("owner control approval is invalid")

// OwnerControlApprovalIssuer prepares an approval after an owner has expressly
// confirmed a single safety-control change. It never changes runtime state.
type OwnerControlApprovalIssuer interface {
	Prepare(ownerIdentity, bindingDigest string) (executionauth.ResolvedApproval, error)
}

// OwnerControlApprovalService signs short-lived, effect-bound owner approvals.
// The execution authorization receipt persists the resulting approval evidence
// and consumes it immediately before the control state is changed.
type OwnerControlApprovalService struct {
	key []byte
	now func() time.Time
}

var _ OwnerControlApprovalIssuer = (*OwnerControlApprovalService)(nil)
var _ executionauth.ApprovalResolver = (*OwnerControlApprovalService)(nil)

func NewOwnerControlApprovalService(secret []byte) (*OwnerControlApprovalService, error) {
	if len(secret) < 32 {
		return nil, fmt.Errorf("owner control approval signing key must contain at least 32 bytes")
	}
	return &OwnerControlApprovalService{key: append([]byte(nil), secret...), now: time.Now}, nil
}

func (s *OwnerControlApprovalService) Prepare(ownerIdentity, bindingDigest string) (executionauth.ResolvedApproval, error) {
	if s == nil || len(s.key) < 32 {
		return executionauth.ResolvedApproval{}, fmt.Errorf("%w: signer is unavailable", ErrOwnerControlApprovalInvalid)
	}
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	bindingDigest = strings.ToLower(strings.TrimSpace(bindingDigest))
	if ownerIdentity == "" || !isSHA256Digest(bindingDigest) {
		return executionauth.ResolvedApproval{}, fmt.Errorf("%w: owner and exact effect binding are required", ErrOwnerControlApprovalInvalid)
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return executionauth.ResolvedApproval{}, fmt.Errorf("generate owner-control approval nonce: %w", err)
	}
	approvedAt := s.now().UTC()
	timestamp := strconv.FormatInt(approvedAt.UnixNano(), 10)
	nonceValue := hex.EncodeToString(nonce)
	signature := s.sign(ownerIdentity, bindingDigest, timestamp, nonceValue)
	sourceID := OwnerControlApprovalPrefix + timestamp + ":" + nonceValue + ":" + signature
	return s.resolved(ownerIdentity, bindingDigest, sourceID, approvedAt)
}

func (s *OwnerControlApprovalService) Resolve(_ context.Context, ownerIdentity, sourceID, bindingDigest string) (executionauth.ResolvedApproval, error) {
	if s == nil || len(s.key) < 32 {
		return executionauth.ResolvedApproval{}, fmt.Errorf("%w: signer is unavailable", ErrOwnerControlApprovalInvalid)
	}
	ownerIdentity = strings.TrimSpace(ownerIdentity)
	bindingDigest = strings.ToLower(strings.TrimSpace(bindingDigest))
	if ownerIdentity == "" || !isSHA256Digest(bindingDigest) {
		return executionauth.ResolvedApproval{}, fmt.Errorf("%w: owner and exact effect binding are required", ErrOwnerControlApprovalInvalid)
	}
	timestamp, nonce, signature, err := parseOwnerControlSource(sourceID)
	if err != nil {
		return executionauth.ResolvedApproval{}, err
	}
	if !hmac.Equal([]byte(s.sign(ownerIdentity, bindingDigest, timestamp, nonce)), []byte(signature)) {
		return executionauth.ResolvedApproval{}, fmt.Errorf("%w: signature does not match this owner or effect", ErrOwnerControlApprovalInvalid)
	}
	nanos, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return executionauth.ResolvedApproval{}, fmt.Errorf("%w: timestamp is malformed", ErrOwnerControlApprovalInvalid)
	}
	approvedAt := time.Unix(0, nanos).UTC()
	now := s.now().UTC()
	if approvedAt.After(now.Add(5*time.Second)) || !now.Before(approvedAt.Add(ownerControlApprovalTTL)) {
		return executionauth.ResolvedApproval{}, fmt.Errorf("%w: approval is not fresh", ErrOwnerControlApprovalInvalid)
	}
	return s.resolved(ownerIdentity, bindingDigest, sourceID, approvedAt)
}

func (s *OwnerControlApprovalService) resolved(ownerIdentity, bindingDigest, sourceID string, approvedAt time.Time) (executionauth.ResolvedApproval, error) {
	decisionDigest := digestOwnerControlApproval(ownerIdentity, bindingDigest, sourceID)
	return executionauth.ResolvedApproval{
		SourceID: sourceID, DecisionID: decisionDigest, DecisionDigest: decisionDigest,
		ApprovedBy: ownerIdentity, ApproverRoles: []string{"owner"},
		ApprovedAt: approvedAt.UTC(), ExpiresAt: approvedAt.UTC().Add(ownerControlApprovalTTL),
		BindingDigest: bindingDigest,
	}, nil
}

func (s *OwnerControlApprovalService) sign(ownerIdentity, bindingDigest, timestamp, nonce string) string {
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write([]byte(strings.Join([]string{"owner-control-approval.v1", ownerIdentity, bindingDigest, timestamp, nonce}, "\x00")))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func parseOwnerControlSource(sourceID string) (string, string, string, error) {
	if !strings.HasPrefix(sourceID, OwnerControlApprovalPrefix) {
		return "", "", "", fmt.Errorf("%w: source type is not supported", ErrOwnerControlApprovalInvalid)
	}
	parts := strings.Split(strings.TrimPrefix(sourceID, OwnerControlApprovalPrefix), ":")
	if len(parts) != 3 || parts[0] == "" || len(parts[1]) != 32 || parts[2] == "" {
		return "", "", "", fmt.Errorf("%w: source is malformed", ErrOwnerControlApprovalInvalid)
	}
	if _, err := hex.DecodeString(parts[1]); err != nil {
		return "", "", "", fmt.Errorf("%w: nonce is malformed", ErrOwnerControlApprovalInvalid)
	}
	if _, err := base64.RawURLEncoding.DecodeString(parts[2]); err != nil {
		return "", "", "", fmt.Errorf("%w: signature is malformed", ErrOwnerControlApprovalInvalid)
	}
	return parts[0], parts[1], parts[2], nil
}

func digestOwnerControlApproval(ownerIdentity, bindingDigest, sourceID string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{"owner-control-approval.v1", ownerIdentity, bindingDigest, sourceID}, "\x00")))
	return hex.EncodeToString(sum[:])
}

func isSHA256Digest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == strings.ToLower(value)
}
