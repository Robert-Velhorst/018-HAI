package runtimelab

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"automation-hub-backend/internal/executionbroker"
	"automation-hub-backend/internal/idempotency"
	"automation-hub-backend/internal/operations"
)

const (
	openClawDiscoveryOperationType = "runtime_discovery"
	openClawDiscoverySchema        = "openclaw-gateway-protocol-v4"
	runtimeDiscoveryFreshnessTTL   = 24 * time.Hour
	openClawDiscoveryEvidenceTTL   = runtimeDiscoveryFreshnessTTL
)

// openClawDiscoveryEvidence is the intentionally small durable proof that an
// owner-scoped, read-only OpenClaw discovery succeeded. It never contains a
// Gateway URL, token, task ID, prompt, owner, raw response, or output.
type openClawDiscoveryEvidence struct {
	SchemaRevision              string                              `json:"schemaRevision"`
	RuntimeID                   string                              `json:"runtimeId"`
	Protocol                    string                              `json:"protocol"`
	ProtocolValid               bool                                `json:"protocolValid"`
	RuntimeVersion              string                              `json:"runtimeVersion,omitempty"`
	IdentityVerified            bool                                `json:"identityVerified"`
	Authenticated               bool                                `json:"authenticated"`
	GatewayScope                string                              `json:"gatewayScope,omitempty"`
	EndpointSHA256              string                              `json:"endpointSha256"`
	GatewayTaskLedger           *GatewayTaskLedgerSummary           `json:"gatewayTaskLedger,omitempty"`
	GatewayCapabilityCatalog    *GatewayCapabilityCatalogSummary    `json:"gatewayCapabilityCatalog,omitempty"`
	GatewayPreparedModelCatalog *GatewayPreparedModelCatalogSummary `json:"gatewayPreparedModelCatalog,omitempty"`
	GatewayAgentRoster          *GatewayAgentRosterSummary          `json:"gatewayAgentRoster,omitempty"`
	CheckedAt                   time.Time                           `json:"checkedAt"`
	ExpiresAt                   time.Time                           `json:"expiresAt"`
	EvidenceSHA256              string                              `json:"evidenceSha256"`
}

func (s *Service) persistOpenClawDiscovery(result ProbeResult) ProbeResult {
	if s.ops == nil || normalizeRuntimeID(result.RuntimeID) != "openclaw" || !result.ProtocolValid {
		return result
	}
	evidence, err := newOpenClawDiscoveryEvidence(result, s.now().UTC())
	if err != nil {
		return result
	}
	evidenceJSON, err := json.Marshal(evidence)
	if err != nil {
		return result
	}

	ingest, err := s.ops.Ingest(operations.NewOperationInput{
		OwnerUserID:        s.owner,
		WorkspaceID:        s.space,
		Title:              "OpenClaw read-only Gateway discovery",
		Description:        "Redacted protocol readiness evidence retained without Gateway execution authority.",
		OperationType:      openClawDiscoveryOperationType,
		SourceType:         "runtime_lab",
		SourceRevisionHash: evidence.EvidenceSHA256,
		DedupeKey: idempotency.OperationDedupeKey(
			s.space,
			openClawDiscoveryOperationType,
			"runtime_lab",
			"openclaw",
			evidence.CheckedAt.UTC().Format(time.RFC3339Nano),
		),
		EvidenceJSON: string(evidenceJSON),
	})
	if err != nil {
		return result
	}

	op := ingest.Operation
	if !ingest.Created && operations.OperationStatus(op.Status) == operations.StatusCompleted {
		return resultWithPersistedOpenClawEvidence(result, evidence)
	}
	op.RuntimeID = "openclaw"
	op.ResultSummary = "Read-only OpenClaw Gateway protocol discovery recorded; it expires automatically and grants no execution authority."
	saved, err := s.ops.Save(op, "runtime_discovery_recorded", string(operations.OwnerHAI), "redacted OpenClaw read-only discovery evidence recorded")
	if err != nil {
		return result
	}
	op = *saved
	for _, status := range []operations.OperationStatus{
		operations.StatusClassified,
		operations.StatusReady,
		operations.StatusRunning,
		operations.StatusVerifying,
		operations.StatusCompleted,
	} {
		updated, err := s.ops.Transition(op, status, string(operations.OwnerHAI), "", "read-only OpenClaw discovery ledger progression")
		if err != nil {
			return result
		}
		op = *updated
	}
	return resultWithPersistedOpenClawEvidence(result, evidence)
}

func newOpenClawDiscoveryEvidence(result ProbeResult, now time.Time) (openClawDiscoveryEvidence, error) {
	if !result.ProtocolValid || strings.TrimSpace(result.EndpointSHA256) == "" || strings.TrimSpace(result.EvidenceSchema) != openClawDiscoverySchema {
		return openClawDiscoveryEvidence{}, errInvalidOpenClawDiscoveryEvidence
	}
	if (result.Authenticated && strings.TrimSpace(result.GatewayScope) != "operator.read") ||
		(!result.Authenticated && strings.TrimSpace(result.GatewayScope) != "") {
		return openClawDiscoveryEvidence{}, errInvalidOpenClawDiscoveryEvidence
	}
	if openClawHasAggregateDiscovery(result) && !openClawAuthenticatedRead(result) {
		return openClawDiscoveryEvidence{}, errInvalidOpenClawDiscoveryEvidence
	}
	checkedAt := result.CheckedAt.UTC()
	if checkedAt.IsZero() {
		checkedAt = now.UTC()
	}
	age := now.UTC().Sub(checkedAt)
	if age < 0 || age >= openClawDiscoveryEvidenceTTL {
		return openClawDiscoveryEvidence{}, errInvalidOpenClawDiscoveryEvidence
	}
	protocol := firstNonEmpty(strings.TrimSpace(result.Protocol), "openclaw-gateway-v4")
	if protocol != "openclaw-gateway-v4" {
		return openClawDiscoveryEvidence{}, errInvalidOpenClawDiscoveryEvidence
	}
	evidence := openClawDiscoveryEvidence{
		SchemaRevision:              openClawDiscoverySchema,
		RuntimeID:                   "openclaw",
		Protocol:                    protocol,
		ProtocolValid:               true,
		RuntimeVersion:              strings.TrimSpace(result.RuntimeVersion),
		IdentityVerified:            result.IdentityVerified,
		Authenticated:               result.Authenticated,
		GatewayScope:                strings.TrimSpace(result.GatewayScope),
		EndpointSHA256:              strings.TrimSpace(result.EndpointSHA256),
		GatewayTaskLedger:           cloneGatewayTaskLedgerSummary(result.GatewayTaskLedger),
		GatewayCapabilityCatalog:    cloneGatewayCapabilityCatalogSummary(result.GatewayCapabilityCatalog),
		GatewayPreparedModelCatalog: cloneGatewayPreparedModelCatalogSummary(result.GatewayPreparedModelCatalog),
		GatewayAgentRoster:          cloneGatewayAgentRosterSummary(result.GatewayAgentRoster),
		CheckedAt:                   checkedAt,
		ExpiresAt:                   checkedAt.Add(openClawDiscoveryEvidenceTTL),
	}
	evidence.EvidenceSHA256 = openClawDiscoveryEvidenceSHA256(evidence)
	return evidence, nil
}

func (s *Service) durableOpenClawDiscovery() (ProbeResult, bool) {
	if s.ops == nil {
		return ProbeResult{}, false
	}
	ledger, err := s.ops.List(operations.Filter{
		OwnerUserID:   s.owner,
		WorkspaceID:   s.space,
		OperationType: openClawDiscoveryOperationType,
		RuntimeID:     "openclaw",
		Status:        operations.StatusCompleted,
		Limit:         1,
	})
	if err != nil || len(ledger) == 0 {
		return ProbeResult{}, false
	}
	var evidence openClawDiscoveryEvidence
	if err := json.Unmarshal([]byte(ledger[0].EvidenceJSON), &evidence); err != nil || !validOpenClawDiscoveryEvidence(evidence, s.now().UTC()) {
		return ProbeResult{}, false
	}
	return probeResultFromOpenClawDiscoveryEvidence(evidence, true), true
}

func validOpenClawDiscoveryEvidence(evidence openClawDiscoveryEvidence, now time.Time) bool {
	if evidence.SchemaRevision != openClawDiscoverySchema || evidence.RuntimeID != "openclaw" || evidence.Protocol != "openclaw-gateway-v4" || !evidence.ProtocolValid ||
		!validSHA256Digest(evidence.EndpointSHA256) || evidence.CheckedAt.IsZero() || evidence.ExpiresAt.IsZero() ||
		evidence.CheckedAt.After(now) || !evidence.ExpiresAt.After(now) || evidence.ExpiresAt.Sub(evidence.CheckedAt) != openClawDiscoveryEvidenceTTL ||
		evidence.EvidenceSHA256 == "" || evidence.EvidenceSHA256 != openClawDiscoveryEvidenceSHA256(evidence) {
		return false
	}
	if (evidence.Authenticated && evidence.GatewayScope != "operator.read") || (!evidence.Authenticated && evidence.GatewayScope != "") {
		return false
	}
	if openClawEvidenceHasAggregateDiscovery(evidence) && (!evidence.Authenticated || evidence.GatewayScope != "operator.read") {
		return false
	}
	return validGatewayTaskLedgerSummary(evidence.GatewayTaskLedger) && validGatewayCapabilityCatalogSummary(evidence.GatewayCapabilityCatalog) && validGatewayPreparedModelCatalogSummary(evidence.GatewayPreparedModelCatalog) && validGatewayAgentRosterSummary(evidence.GatewayAgentRoster)
}

func openClawHasAggregateDiscovery(result ProbeResult) bool {
	return result.GatewayTaskLedger != nil || result.GatewayCapabilityCatalog != nil ||
		result.GatewayPreparedModelCatalog != nil || result.GatewayAgentRoster != nil
}

func openClawEvidenceHasAggregateDiscovery(evidence openClawDiscoveryEvidence) bool {
	return evidence.GatewayTaskLedger != nil || evidence.GatewayCapabilityCatalog != nil ||
		evidence.GatewayPreparedModelCatalog != nil || evidence.GatewayAgentRoster != nil
}

func resultWithPersistedOpenClawEvidence(result ProbeResult, evidence openClawDiscoveryEvidence) ProbeResult {
	expiresAt := evidence.ExpiresAt
	result.EvidenceSHA256 = evidence.EvidenceSHA256
	result.EvidenceSchema = evidence.SchemaRevision
	result.EndpointSHA256 = evidence.EndpointSHA256
	result.GatewayScope = evidence.GatewayScope
	result.GatewayTaskLedger = cloneGatewayTaskLedgerSummary(evidence.GatewayTaskLedger)
	result.GatewayCapabilityCatalog = cloneGatewayCapabilityCatalogSummary(evidence.GatewayCapabilityCatalog)
	result.GatewayPreparedModelCatalog = cloneGatewayPreparedModelCatalogSummary(evidence.GatewayPreparedModelCatalog)
	result.GatewayAgentRoster = cloneGatewayAgentRosterSummary(evidence.GatewayAgentRoster)
	result.EvidenceExpiresAt = &expiresAt
	result.EvidencePersisted = true
	return result
}

func validGatewayTaskLedgerSummary(summary *GatewayTaskLedgerSummary) bool {
	if summary == nil {
		return true
	}
	if summary.SampledTasks < 0 || summary.SampledTasks > 50 || len(summary.StatusCounts) > 50 {
		return false
	}
	total := 0
	for status, count := range summary.StatusCounts {
		if !validOpenClawDiscoveryTaskStatus(status) || count < 0 {
			return false
		}
		total += count
	}
	return total == summary.SampledTasks
}

func validGatewayCapabilityCatalogSummary(summary *GatewayCapabilityCatalogSummary) bool {
	if summary == nil {
		return true
	}
	if summary.SampledSkills < 0 || summary.SampledSkills > 500 || summary.EligibleSkills < 0 || summary.EligibleSkills > summary.SampledSkills || summary.SampledCommands < 0 || summary.SampledCommands > 500 || len(summary.ToolCountsBySource) == 0 || len(summary.ToolCountsBySource) > 2 {
		return false
	}
	totalTools := 0
	for source, count := range summary.ToolCountsBySource {
		if (source != "core" && source != "plugin") || count < 0 {
			return false
		}
		totalTools += count
	}
	return totalTools <= 500
}

func validGatewayPreparedModelCatalogSummary(summary *GatewayPreparedModelCatalogSummary) bool {
	if summary == nil {
		return true
	}
	if summary.SampledModels < 0 || summary.SampledModels > 500 || summary.AvailableModels < 0 || summary.UnavailableModels < 0 || summary.UnknownAvailabilityModels < 0 {
		return false
	}
	return summary.AvailableModels+summary.UnavailableModels+summary.UnknownAvailabilityModels == summary.SampledModels
}

func validGatewayAgentRosterSummary(summary *GatewayAgentRosterSummary) bool {
	if summary == nil {
		return true
	}
	if summary.SampledAgents < 0 || summary.SampledAgents > 500 || summary.AgentCount < 0 || summary.SystemCount < 0 || summary.UnknownKindCount < 0 {
		return false
	}
	return summary.AgentCount+summary.SystemCount+summary.UnknownKindCount == summary.SampledAgents
}

func validOpenClawDiscoveryTaskStatus(status string) bool {
	switch status {
	case "queued", "running", "completed", "failed", "cancelled", "timed_out":
		return true
	}
	return false
}

func probeResultFromOpenClawDiscoveryEvidence(evidence openClawDiscoveryEvidence, persisted bool) ProbeResult {
	expiresAt := evidence.ExpiresAt
	return ProbeResult{
		RuntimeID:                   "openclaw",
		Status:                      executionbroker.RuntimeBlocked,
		DiscoveryState:              "succeeded",
		ReadinessLevel:              ReadinessAvailable,
		Protocol:                    evidence.Protocol,
		RuntimeVersion:              evidence.RuntimeVersion,
		ProtocolValid:               evidence.ProtocolValid,
		IdentityVerified:            evidence.IdentityVerified,
		Authenticated:               evidence.Authenticated,
		EvidenceSHA256:              evidence.EvidenceSHA256,
		EvidenceSchema:              evidence.SchemaRevision,
		EndpointSHA256:              evidence.EndpointSHA256,
		GatewayScope:                evidence.GatewayScope,
		GatewayTaskLedger:           cloneGatewayTaskLedgerSummary(evidence.GatewayTaskLedger),
		GatewayCapabilityCatalog:    cloneGatewayCapabilityCatalogSummary(evidence.GatewayCapabilityCatalog),
		GatewayPreparedModelCatalog: cloneGatewayPreparedModelCatalogSummary(evidence.GatewayPreparedModelCatalog),
		GatewayAgentRoster:          cloneGatewayAgentRosterSummary(evidence.GatewayAgentRoster),
		EvidenceExpiresAt:           &expiresAt,
		EvidencePersisted:           persisted,
		Detail:                      "durable, read-only OpenClaw protocol discovery is available; it grants no execution authority",
		CheckedAt:                   evidence.CheckedAt,
	}
}

func openClawDiscoveryEvidenceSHA256(evidence openClawDiscoveryEvidence) string {
	evidence.EvidenceSHA256 = ""
	encoded, err := json.Marshal(evidence)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func validSHA256Digest(value string) bool {
	const prefix = "sha256:"
	if !strings.HasPrefix(value, prefix) || len(value) != len(prefix)+64 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, prefix))
	return err == nil
}

var errInvalidOpenClawDiscoveryEvidence = &invalidOpenClawDiscoveryEvidenceError{}

type invalidOpenClawDiscoveryEvidenceError struct{}

func (*invalidOpenClawDiscoveryEvidenceError) Error() string {
	return "invalid OpenClaw read-only discovery evidence"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
