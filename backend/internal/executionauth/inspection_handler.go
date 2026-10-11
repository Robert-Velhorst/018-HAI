package executionauth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"automation-hub-backend/internal/identity"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	defaultInspectionLimit  = 50
	maxInspectionLimit      = 100
	maxInspectionTextRunes  = 256
	maxInspectionListValues = 24
	inspectionFingerprintN  = 12
)

var (
	inspectionSecretAssignment = regexp.MustCompile(
		`(?i)\b(authorization|api[-_ ]?key|access[-_ ]?token|refresh[-_ ]?token|token|secret|password|passwd|client[-_ ]?secret)\b\s*[:=]\s*[^\s,;]+`,
	)
	inspectionBearer = regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._~+/=-]+`)
)

// InspectionReader is the owner-scoped read contract required by the HTTP
// inspection surface. *Service satisfies this interface.
type InspectionReader interface {
	List(context.Context, string, int) ([]Receipt, error)
	Get(context.Context, string, uuid.UUID) (Receipt, error)
	GetConsumption(context.Context, string, uuid.UUID) (Consumption, error)
}

// InspectionHandler exposes immutable authorization decisions and their
// single-use consumption record. It deliberately returns a bounded public view
// instead of serializing persistence models directly.
type InspectionHandler struct {
	reader InspectionReader
}

func NewInspectionHandler(reader InspectionReader) *InspectionHandler {
	return &InspectionHandler{reader: reader}
}

// RegisterRoutes mounts read-only endpoints on a router group:
//
//	GET /                     list recent receipts
//	GET /:id                  inspect one receipt
//	GET /:id/consumption      inspect its single-use consumption
//
// Authentication is enforced again in every handler so accidental router
// wiring without an auth middleware still fails closed.
func (h *InspectionHandler) RegisterRoutes(routes gin.IRoutes) {
	routes.GET("", h.List)
	routes.GET("/", h.List)
	routes.GET("/:id", h.Get)
	routes.GET("/:id/consumption", h.GetConsumption)
}

func (h *InspectionHandler) List(c *gin.Context) {
	owner, ok := h.authenticatedOwner(c)
	if !ok {
		return
	}
	limit, ok := inspectionLimit(c)
	if !ok {
		return
	}
	view, ok := inspectionListView(c)
	if !ok {
		return
	}
	receipts, err := h.reader.List(c.Request.Context(), owner, limit)
	if err != nil {
		writeInspectionError(c, http.StatusServiceUnavailable, "inspection_unavailable",
			"execution authorization inspection is unavailable")
		return
	}
	if view == "summary" {
		views := make([]inspectionReceiptSummary, 0, len(receipts))
		for _, receipt := range receipts {
			if len(views) >= limit {
				break
			}
			if strings.TrimSpace(receipt.OwnerIdentity) != owner {
				continue
			}
			views = append(views, publicReceiptSummary(receipt))
		}
		c.JSON(http.StatusOK, gin.H{"receipts": views, "count": len(views), "limit": limit})
		return
	}

	views := make([]inspectionReceipt, 0, len(receipts))
	for _, receipt := range receipts {
		if len(views) >= limit {
			break
		}
		// The repository is the primary owner boundary. This additional check
		// prevents a faulty adapter from turning into cross-owner disclosure.
		if strings.TrimSpace(receipt.OwnerIdentity) != owner {
			continue
		}
		views = append(views, publicReceipt(receipt))
	}
	c.JSON(http.StatusOK, gin.H{"receipts": views, "count": len(views), "limit": limit})
}

func (h *InspectionHandler) Get(c *gin.Context) {
	owner, ok := h.authenticatedOwner(c)
	if !ok {
		return
	}
	id, ok := inspectionReceiptID(c)
	if !ok {
		return
	}
	receipt, err := h.reader.Get(c.Request.Context(), owner, id)
	if err != nil {
		h.writeReadError(c, err, "receipt_not_found",
			"execution authorization receipt was not found")
		return
	}
	if strings.TrimSpace(receipt.OwnerIdentity) != owner {
		writeInspectionError(c, http.StatusNotFound, "receipt_not_found",
			"execution authorization receipt was not found")
		return
	}
	c.JSON(http.StatusOK, publicReceipt(receipt))
}

func (h *InspectionHandler) GetConsumption(c *gin.Context) {
	owner, ok := h.authenticatedOwner(c)
	if !ok {
		return
	}
	id, ok := inspectionReceiptID(c)
	if !ok {
		return
	}
	consumption, err := h.reader.GetConsumption(c.Request.Context(), owner, id)
	if err != nil {
		h.writeReadError(c, err, "consumption_not_found",
			"execution authorization consumption was not found")
		return
	}
	if strings.TrimSpace(consumption.OwnerIdentity) != owner ||
		consumption.ReceiptID != id {
		writeInspectionError(c, http.StatusNotFound, "consumption_not_found",
			"execution authorization consumption was not found")
		return
	}
	c.JSON(http.StatusOK, publicConsumption(consumption))
}

func (h *InspectionHandler) authenticatedOwner(c *gin.Context) (string, bool) {
	value, exists := c.Get(identity.ContextSubjectKey)
	owner, ok := value.(string)
	owner = strings.TrimSpace(owner)
	if !exists || !ok || owner == "" {
		writeInspectionError(c, http.StatusUnauthorized, "authentication_required",
			"an authenticated owner session is required for execution authorization inspection")
		return "", false
	}
	if h == nil || h.reader == nil {
		writeInspectionError(c, http.StatusServiceUnavailable, "inspection_unavailable",
			"execution authorization inspection is unavailable")
		return "", false
	}
	return owner, true
}

func (h *InspectionHandler) writeReadError(
	c *gin.Context,
	err error,
	notFoundCode string,
	notFoundMessage string,
) {
	if errors.Is(err, ErrNotFound) {
		writeInspectionError(c, http.StatusNotFound, notFoundCode, notFoundMessage)
		return
	}
	// Do not attach repository errors to Gin. Driver errors can contain DSNs,
	// SQL fragments, filesystem paths, or provider payloads.
	writeInspectionError(c, http.StatusServiceUnavailable, "inspection_unavailable",
		"execution authorization inspection is unavailable")
}

func inspectionReceiptID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(strings.TrimSpace(c.Param("id")))
	if err != nil {
		writeInspectionError(c, http.StatusBadRequest, "invalid_receipt_id",
			"execution authorization receipt id must be a valid UUID")
		return uuid.Nil, false
	}
	return id, true
}

func inspectionLimit(c *gin.Context) (int, bool) {
	raw := strings.TrimSpace(c.Query("limit"))
	if raw == "" {
		return defaultInspectionLimit, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		writeInspectionError(c, http.StatusBadRequest, "invalid_limit",
			"limit must be a positive integer")
		return 0, false
	}
	if value > maxInspectionLimit {
		value = maxInspectionLimit
	}
	return value, true
}

func inspectionListView(c *gin.Context) (string, bool) {
	view := strings.ToLower(strings.TrimSpace(c.Query("view")))
	if view == "" || view == "full" {
		return "full", true
	}
	if view == "summary" {
		return view, true
	}
	writeInspectionError(c, http.StatusBadRequest, "invalid_view",
		"view must be full or summary")
	return "", false
}

type inspectionErrorEnvelope struct {
	Error inspectionError `json:"error"`
}

type inspectionError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeInspectionError(c *gin.Context, status int, code, message string) {
	c.JSON(status, inspectionErrorEnvelope{
		Error: inspectionError{Code: code, Message: message},
	})
}

type inspectionReceipt struct {
	ID                         uuid.UUID                      `json:"id"`
	ContractVersion            int                            `json:"contractVersion"`
	ActorKind                  ActorKind                      `json:"actorKind"`
	ActorFingerprint           string                         `json:"actorFingerprint,omitempty"`
	TaskID                     string                         `json:"taskId"`
	Action                     string                         `json:"action"`
	Stage                      Stage                          `json:"stage"`
	ResourceType               string                         `json:"resourceType"`
	ResourceID                 string                         `json:"resourceId,omitempty"`
	Domain                     string                         `json:"domain,omitempty"`
	Outcome                    Outcome                        `json:"outcome"`
	Reason                     string                         `json:"reason"`
	RequestFingerprint         string                         `json:"requestFingerprint"`
	DecisionFingerprint        string                         `json:"decisionFingerprint"`
	RequiredAuthority          int                            `json:"requiredAuthority"`
	RequestedAutonomy          int                            `json:"requestedAutonomy"`
	EffectiveAutonomy          int                            `json:"effectiveAutonomy"`
	Risk                       RiskLevel                      `json:"risk"`
	Reversible                 bool                           `json:"reversible"`
	EstimatedCostEUR           float64                        `json:"estimatedCostEur"`
	NotificationRequired       bool                           `json:"notificationRequired"`
	EvaluatedAt                time.Time                      `json:"evaluatedAt"`
	Evidence                   inspectionEvidence             `json:"evidence"`
	LifeGraphProjection        *inspectionLifeGraphProjection `json:"lifeGraphProjection,omitempty"`
	LifeGraphProjectionWarning string                         `json:"lifeGraphProjectionWarning,omitempty"`
}

type inspectionReceiptSummary struct {
	ID              uuid.UUID `json:"id"`
	ContractVersion int       `json:"contractVersion"`
	Action          string    `json:"action"`
	Stage           Stage     `json:"stage"`
	ResourceType    string    `json:"resourceType"`
	ResourceID      string    `json:"resourceId,omitempty"`
	Domain          string    `json:"domain,omitempty"`
	Outcome         Outcome   `json:"outcome"`
	Reason          string    `json:"reason"`
	Risk            RiskLevel `json:"risk"`
	EvaluatedAt     time.Time `json:"evaluatedAt"`
}

type inspectionLifeGraphProjection struct {
	PrimaryID       string `json:"primaryId"`
	Domain          string `json:"domain"`
	LinkedRecords   int    `json:"linkedRecords"`
	Relations       int    `json:"relations"`
	AlreadyExisted  bool   `json:"alreadyExisted"`
	AdvisoryOnly    bool   `json:"advisoryOnly"`
	CanExecute      bool   `json:"canExecute"`
	GrantsAuthority bool   `json:"grantsAuthority"`
}

type inspectionEvidence struct {
	EmergencyStop              inspectionEmergencyStop              `json:"emergencyStop"`
	SystemWorkload             inspectionSystemWorkload             `json:"systemWorkload"`
	Constitution               inspectionConstitution               `json:"constitution"`
	Mandate                    inspectionMandate                    `json:"mandate"`
	Agent                      inspectionAgent                      `json:"agent"`
	Approval                   inspectionApproval                   `json:"approval"`
	FrameworkSelection         inspectionFrameworkSelection         `json:"frameworkSelection"`
	FrameworkEvidencePreflight inspectionFrameworkEvidencePreflight `json:"frameworkEvidencePreflight"`
	ReasonCodes                []string                             `json:"reasonCodes"`
	Trace                      []string                             `json:"trace"`
}

type inspectionFrameworkSelection struct {
	SelectionID              string `json:"selectionId,omitempty"`
	SelectorAlgorithmVersion string `json:"selectorAlgorithmVersion,omitempty"`
	OwnerScoped              bool   `json:"ownerScoped"`
	Verified                 bool   `json:"verified"`
}

type inspectionFrameworkEvidencePreflight struct {
	Digest               string `json:"digest,omitempty"`
	OwnerScoped          bool   `json:"ownerScoped"`
	Verified             bool   `json:"verified"`
	SourceClaimsVerified int    `json:"sourceClaimsVerified,omitempty"`
	SourceClaimsDigest   string `json:"sourceClaimsDigest,omitempty"`
}

type inspectionEmergencyStop struct {
	Active bool   `json:"active"`
	Source string `json:"source,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type inspectionSystemWorkload struct {
	PolicyID         string `json:"policyId,omitempty"`
	ActorFingerprint string `json:"actorFingerprint,omitempty"`
	Matched          bool   `json:"matched"`
}

type inspectionConstitution struct {
	ID                           string   `json:"id,omitempty"`
	Version                      int      `json:"version"`
	Source                       string   `json:"source,omitempty"`
	Fingerprint                  string   `json:"fingerprint,omitempty"`
	RequestedCapabilities        []string `json:"requestedCapabilities"`
	DeniedCapabilities           []string `json:"deniedCapabilities"`
	ApprovalRequiredCapabilities []string `json:"approvalRequiredCapabilities"`
	AuthorityCeiling             int      `json:"authorityCeiling"`
}

type inspectionMandate struct {
	ID                  string `json:"id,omitempty"`
	Revision            uint64 `json:"revision,omitempty"`
	DecisionID          string `json:"decisionId,omitempty"`
	DecisionFingerprint string `json:"decisionFingerprint,omitempty"`
	Outcome             string `json:"outcome,omitempty"`
}

type inspectionAgent struct {
	AgentID          string `json:"agentId,omitempty"`
	AgentRevision    uint64 `json:"agentRevision,omitempty"`
	AssignmentID     string `json:"assignmentId,omitempty"`
	GrantedAuthority int    `json:"grantedAuthority,omitempty"`
	GrantedAutonomy  int    `json:"grantedAutonomy,omitempty"`
	RuntimeID        string `json:"runtimeId,omitempty"`
}

type inspectionApproval struct {
	SourceID            string    `json:"sourceId,omitempty"`
	DecisionID          string    `json:"decisionId,omitempty"`
	DecisionFingerprint string    `json:"decisionFingerprint,omitempty"`
	ApproverFingerprint string    `json:"approverFingerprint,omitempty"`
	ApprovedAt          time.Time `json:"approvedAt,omitempty"`
	ExpiresAt           time.Time `json:"expiresAt,omitempty"`
}

type inspectionConsumption struct {
	ReceiptID          uuid.UUID `json:"receiptId"`
	Consumer           string    `json:"consumer"`
	ExecutionTarget    string    `json:"executionTarget"`
	ReceiptFingerprint string    `json:"receiptFingerprint"`
	ConsumedAt         time.Time `json:"consumedAt"`
}

func publicReceipt(receipt Receipt) inspectionReceipt {
	return inspectionReceipt{
		ID:                         receipt.ID,
		ContractVersion:            receipt.ContractVersion,
		ActorKind:                  receipt.ActorKind,
		ActorFingerprint:           inspectionFingerprint(receipt.ActorIdentity),
		TaskID:                     inspectionPublicText(receipt.TaskID),
		Action:                     inspectionPublicText(receipt.Action),
		Stage:                      receipt.Stage,
		ResourceType:               inspectionPublicText(receipt.ResourceType),
		ResourceID:                 inspectionPublicText(receipt.ResourceID),
		Domain:                     inspectionPublicText(receipt.Domain),
		Outcome:                    receipt.Outcome,
		Reason:                     inspectionPublicText(receipt.Reason),
		RequestFingerprint:         inspectionFingerprint(receipt.RequestDigest),
		DecisionFingerprint:        inspectionFingerprint(receipt.DecisionDigest),
		RequiredAuthority:          receipt.RequiredAuthority,
		RequestedAutonomy:          receipt.RequestedAutonomy,
		EffectiveAutonomy:          receipt.EffectiveAutonomy,
		Risk:                       receipt.Risk,
		Reversible:                 receipt.Reversible,
		EstimatedCostEUR:           receipt.EstimatedCostEUR,
		NotificationRequired:       receipt.NotificationRequired,
		EvaluatedAt:                receipt.EvaluatedAt,
		Evidence:                   publicEvidence(receipt.Evidence),
		LifeGraphProjection:        publicLifeGraphProjection(receipt),
		LifeGraphProjectionWarning: inspectionPublicText(receipt.LifeGraphProjectionWarning),
	}
}

func publicReceiptSummary(receipt Receipt) inspectionReceiptSummary {
	return inspectionReceiptSummary{
		ID:              receipt.ID,
		ContractVersion: receipt.ContractVersion,
		Action:          inspectionPublicText(receipt.Action),
		Stage:           receipt.Stage,
		ResourceType:    inspectionPublicText(receipt.ResourceType),
		ResourceID:      inspectionPublicText(receipt.ResourceID),
		Domain:          inspectionPublicText(receipt.Domain),
		Outcome:         receipt.Outcome,
		Reason:          inspectionPublicText(receipt.Reason),
		Risk:            receipt.Risk,
		EvaluatedAt:     receipt.EvaluatedAt,
	}
}

func publicLifeGraphProjection(receipt Receipt) *inspectionLifeGraphProjection {
	if receipt.LifeGraphProjection == nil {
		return nil
	}
	projection := receipt.LifeGraphProjection
	return &inspectionLifeGraphProjection{
		PrimaryID:     inspectionPublicText(projection.Primary.ID),
		Domain:        inspectionPublicText(string(projection.Primary.Domain)),
		LinkedRecords: len(projection.LinkedEntities), Relations: len(projection.Relations),
		AlreadyExisted: projection.AlreadyExisted, AdvisoryOnly: projection.AdvisoryOnly,
		CanExecute: projection.CanExecute, GrantsAuthority: projection.GrantsAuthority,
	}
}

func publicEvidence(evidence DecisionEvidence) inspectionEvidence {
	return inspectionEvidence{
		EmergencyStop: inspectionEmergencyStop{
			Active: evidence.EmergencyStop.Active,
			Source: inspectionPublicText(evidence.EmergencyStop.Source),
			Reason: inspectionPublicText(evidence.EmergencyStop.Reason),
		},
		SystemWorkload: inspectionSystemWorkload{
			PolicyID:         inspectionPublicText(evidence.SystemWorkload.PolicyID),
			ActorFingerprint: inspectionFingerprint(evidence.SystemWorkload.ActorIdentity),
			Matched:          evidence.SystemWorkload.Matched,
		},
		Constitution: inspectionConstitution{
			ID:                           inspectionPublicText(evidence.Constitution.ID),
			Version:                      evidence.Constitution.Version,
			Source:                       inspectionPublicText(evidence.Constitution.Source),
			Fingerprint:                  inspectionFingerprint(evidence.Constitution.Digest),
			RequestedCapabilities:        inspectionPublicList(evidence.Constitution.RequestedCapabilities),
			DeniedCapabilities:           inspectionPublicList(evidence.Constitution.DeniedCapabilities),
			ApprovalRequiredCapabilities: inspectionPublicList(evidence.Constitution.ApprovalRequiredCapabilities),
			AuthorityCeiling:             evidence.Constitution.AuthorityCeiling,
		},
		Mandate: inspectionMandate{
			ID:                  inspectionPublicText(evidence.Mandate.ID),
			Revision:            evidence.Mandate.Revision,
			DecisionID:          inspectionPublicText(evidence.Mandate.DecisionID),
			DecisionFingerprint: inspectionFingerprint(evidence.Mandate.DecisionDigest),
			Outcome:             inspectionPublicText(evidence.Mandate.Outcome),
		},
		Agent: inspectionAgent{
			AgentID:          inspectionPublicText(evidence.Agent.AgentID),
			AgentRevision:    evidence.Agent.AgentRevision,
			AssignmentID:     inspectionPublicText(evidence.Agent.AssignmentID),
			GrantedAuthority: evidence.Agent.GrantedAuthority,
			GrantedAutonomy:  evidence.Agent.GrantedAutonomy,
			RuntimeID:        inspectionPublicText(evidence.Agent.RuntimeID),
		},
		Approval: inspectionApproval{
			SourceID:            inspectionPublicText(evidence.Approval.SourceID),
			DecisionID:          inspectionPublicText(evidence.Approval.DecisionID),
			DecisionFingerprint: inspectionFingerprint(evidence.Approval.DecisionDigest),
			ApproverFingerprint: inspectionFingerprint(evidence.Approval.ApprovedBy),
			ApprovedAt:          evidence.Approval.ApprovedAt,
			ExpiresAt:           evidence.Approval.ExpiresAt,
		},
		FrameworkSelection: inspectionFrameworkSelection{
			SelectionID: inspectionPublicText(evidence.FrameworkSelection.SelectionID),
			SelectorAlgorithmVersion: inspectionPublicText(
				evidence.FrameworkSelection.SelectorAlgorithmVersion,
			),
			OwnerScoped: evidence.FrameworkSelection.OwnerScoped,
			Verified:    evidence.FrameworkSelection.Verified,
		},
		FrameworkEvidencePreflight: inspectionFrameworkEvidencePreflight{
			Digest: inspectionFingerprint(
				evidence.FrameworkEvidencePreflight.Digest,
			),
			OwnerScoped:          evidence.FrameworkEvidencePreflight.OwnerScoped,
			Verified:             evidence.FrameworkEvidencePreflight.Verified,
			SourceClaimsVerified: evidence.FrameworkEvidencePreflight.SourceClaimsVerified,
			SourceClaimsDigest: inspectionFingerprint(
				evidence.FrameworkEvidencePreflight.SourceClaimsDigest,
			),
		},
		ReasonCodes: inspectionPublicList(evidence.ReasonCodes),
		Trace:       inspectionPublicList(evidence.Trace),
	}
}

func publicConsumption(value Consumption) inspectionConsumption {
	return inspectionConsumption{
		ReceiptID:          value.ReceiptID,
		Consumer:           inspectionPublicText(value.Consumer),
		ExecutionTarget:    inspectionPublicText(value.ExecutionTarget),
		ReceiptFingerprint: inspectionFingerprint(value.ReceiptDigest),
		ConsumedAt:         value.ConsumedAt,
	}
}

func inspectionPublicList(values []string) []string {
	if len(values) > maxInspectionListValues {
		values = values[:maxInspectionListValues]
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if cleaned := inspectionPublicText(value); cleaned != "" {
			result = append(result, cleaned)
		}
	}
	return result
}

func inspectionPublicText(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	value = inspectionBearer.ReplaceAllString(value, "Bearer [redacted]")
	value = inspectionSecretAssignment.ReplaceAllString(value, "${1}=[redacted]")
	if utf8.RuneCountInString(value) <= maxInspectionTextRunes {
		return value
	}
	runes := []rune(value)
	return string(runes[:maxInspectionTextRunes-1]) + "…"
}

func inspectionFingerprint(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	// Cryptographic receipt digests stay non-reversible while identities and
	// other labels receive a one-way fingerprint before public inspection.
	decoded, err := hex.DecodeString(value)
	if err == nil && len(decoded) >= inspectionFingerprintN/2 {
		if len(value) > inspectionFingerprintN {
			return strings.ToLower(value[:inspectionFingerprintN])
		}
		return strings.ToLower(value)
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:inspectionFingerprintN]
}
