package lifeontology

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const maximumProjectionLinks = 16

// OperationalLinkRequest links a source-backed operational record to another
// stable record. Direction is primary_to_related unless explicitly set to
// related_to_primary. The projector is advisory and cannot execute either
// record.
type OperationalLinkRequest struct {
	Type               EntityType
	RecordID           string
	Name               string
	Summary            string
	Relation           RelationType
	Direction          string
	Status             LifecycleStatus
	Priority           int
	Attributes         map[string]string
	ExternalKey        *ExternalKey
	Confidence         float64
	VerificationStatus VerificationStatus
	Sensitivity        Sensitivity
	LocalOnly          bool
}

type OperationalProjectionRequest struct {
	OwnerIdentity      string
	Type               EntityType
	RecordID           string
	Domain             Domain
	Name               string
	Summary            string
	Status             LifecycleStatus
	Priority           int
	DueAt              *time.Time
	ObservedAt         time.Time
	Confidence         float64
	VerificationStatus VerificationStatus
	Attributes         map[string]string
	Provenance         []Provenance
	Sensitivity        Sensitivity
	LocalOnly          bool
	Links              []OperationalLinkRequest
}

type OperationalProjectionResult struct {
	Primary         Entity     `json:"primary"`
	LinkedEntities  []Entity   `json:"linkedEntities"`
	Relations       []Relation `json:"relations"`
	AlreadyExisted  bool       `json:"alreadyExisted"`
	AdvisoryOnly    bool       `json:"advisoryOnly"`
	CanExecute      bool       `json:"canExecute"`
	GrantsAuthority bool       `json:"grantsAuthority"`
}

var operationalEntityTypes = map[EntityType]struct{}{
	EntityPerson: {}, EntitySource: {}, EntityDocument: {}, EntityPursuit: {}, EntityWorkflow: {}, EntityTask: {},
	EntityMemory: {}, EntityCommitment: {}, EntityCost: {}, EntityOutcome: {}, EntityProject: {},
	EntityCase: {}, EntityGoal: {}, EntityObligation: {}, EntityOpportunity: {}, EntityRisk: {},
}

// ProjectOperationalRecord materializes an immutable operational record and
// its stable links into the owner-scoped life graph. It reuses existing linked
// entities by exact external key and never turns graph presence into approval
// or execution authority.
func (s *Service) ProjectOperationalRecord(ctx context.Context, request OperationalProjectionRequest) (OperationalProjectionResult, error) {
	result := OperationalProjectionResult{AdvisoryOnly: true, CanExecute: false, GrantsAuthority: false}
	if s == nil || s.repo == nil || s.clock == nil {
		return result, fmt.Errorf("life ontology service is unavailable")
	}
	if err := validateOperationalProjection(request); err != nil {
		return result, err
	}

	primaryRequest := projectionEntityRequest(request, request.Type, request.RecordID, request.Name, request.Summary, request.Status, request.Priority, request.Attributes, nil)
	primary, err := s.RecordEntity(ctx, primaryRequest)
	if err != nil {
		return result, fmt.Errorf("project primary operational record: %w", err)
	}
	result.Primary = primary.Entity
	result.AlreadyExisted = primary.AlreadyExisted

	for _, link := range request.Links {
		externalKey := projectionExternalKey(link.Type, link.RecordID, link.ExternalKey)
		linkedRequest := projectionEntityRequest(
			request,
			link.Type,
			link.RecordID,
			link.Name,
			link.Summary,
			link.Status,
			link.Priority,
			link.Attributes,
			&externalKey,
		)
		linkConfidence, linkVerification, linkSensitivity, linkLocalOnly := projectionLinkControls(request, link)
		linkedRequest.Confidence = linkConfidence
		linkedRequest.VerificationStatus = linkVerification
		linkedRequest.Sensitivity = linkSensitivity
		linkedRequest.LocalOnly = linkLocalOnly
		linked, err := s.findOrRecordProjectionEntity(ctx, linkedRequest)
		if err != nil {
			return result, fmt.Errorf("project linked %s record: %w", link.Type, err)
		}
		result.LinkedEntities = append(result.LinkedEntities, linked)

		fromID, toID := primary.Entity.ID, linked.ID
		if strings.EqualFold(strings.TrimSpace(link.Direction), "related_to_primary") {
			fromID, toID = linked.ID, primary.Entity.ID
		}
		relation, err := s.RecordRelation(ctx, RecordRelationRequest{
			OwnerIdentity: request.OwnerIdentity,
			Type:          link.Relation, FromEntityID: fromID, ToEntityID: toID,
			Summary:    compact("Operational projection: " + request.Name + " -> " + link.Name),
			Attributes: map[string]string{"projection": "operational", "record_type": string(request.Type)},
			ValidFrom:  request.ObservedAt, ObservedAt: request.ObservedAt,
			Confidence: linkConfidence, VerificationStatus: linkVerification,
			Provenance: request.Provenance, Sensitivity: linkSensitivity, LocalOnly: linkLocalOnly,
		})
		if err != nil {
			return result, fmt.Errorf("project %s relation: %w", link.Relation, err)
		}
		result.Relations = append(result.Relations, relation.Relation)
	}
	return result, nil
}

func projectionLinkControls(request OperationalProjectionRequest, link OperationalLinkRequest) (float64, VerificationStatus, Sensitivity, bool) {
	confidence := request.Confidence
	if link.Confidence > 0 {
		confidence = link.Confidence
	}
	verification := request.VerificationStatus
	if link.VerificationStatus != "" {
		verification = link.VerificationStatus
	}
	sensitivity := request.Sensitivity
	if link.Sensitivity != "" {
		sensitivity = link.Sensitivity
	}
	return confidence, verification, sensitivity, request.LocalOnly || link.LocalOnly
}

func validateOperationalProjection(request OperationalProjectionRequest) error {
	if _, ok := operationalEntityTypes[request.Type]; !ok {
		return fmt.Errorf("entity type %q is not an operational projection type", request.Type)
	}
	if compact(request.RecordID) == "" || len(compact(request.RecordID)) > 256 {
		return fmt.Errorf("operational record id is required and must be bounded")
	}
	if len(request.Links) > maximumProjectionLinks {
		return fmt.Errorf("operational projection exceeds %d links", maximumProjectionLinks)
	}
	for index, link := range request.Links {
		if _, ok := operationalEntityTypes[link.Type]; !ok {
			return fmt.Errorf("link %d has unsupported entity type %q", index, link.Type)
		}
		if compact(link.RecordID) == "" || compact(link.Name) == "" {
			return fmt.Errorf("link %d requires record id and name", index)
		}
		if link.Direction != "" && link.Direction != "primary_to_related" && link.Direction != "related_to_primary" {
			return fmt.Errorf("link %d has invalid direction %q", index, link.Direction)
		}
		if !validUnitConfidence(link.Confidence) {
			return fmt.Errorf("link %d has invalid confidence", index)
		}
		if link.VerificationStatus != "" {
			if _, ok := validVerificationStatuses[link.VerificationStatus]; !ok {
				return fmt.Errorf("link %d has invalid verification status %q", index, link.VerificationStatus)
			}
		}
		if link.Sensitivity != "" {
			if _, ok := validSensitivities[link.Sensitivity]; !ok {
				return fmt.Errorf("link %d has invalid sensitivity %q", index, link.Sensitivity)
			}
		}
		from, to := request.Type, link.Type
		if link.Direction == "related_to_primary" {
			from, to = link.Type, request.Type
		}
		if err := validateRelationEndpoints(link.Relation, from, to); err != nil {
			return fmt.Errorf("link %d: %w", index, err)
		}
	}
	return nil
}

func projectionEntityRequest(
	request OperationalProjectionRequest,
	kind EntityType,
	recordID, name, summary string,
	status LifecycleStatus,
	priority int,
	attributes map[string]string,
	externalKey *ExternalKey,
) RecordEntityRequest {
	if status == "" {
		status = StatusActive
	}
	keys := []ExternalKey{projectionExternalKey(kind, recordID, externalKey)}
	return RecordEntityRequest{
		OwnerIdentity: request.OwnerIdentity, Type: kind, Domain: request.Domain,
		Name: name, Summary: summary, ExternalKeys: keys, Attributes: attributes,
		Status: status, Priority: priority, DueAt: cloneTime(request.DueAt),
		ValidFrom: request.ObservedAt, ObservedAt: request.ObservedAt,
		Confidence: request.Confidence, VerificationStatus: request.VerificationStatus,
		Provenance: request.Provenance, Sensitivity: request.Sensitivity, LocalOnly: request.LocalOnly,
	}
}

func projectionExternalKey(kind EntityType, recordID string, explicit *ExternalKey) ExternalKey {
	if explicit != nil && compact(explicit.Namespace) != "" && compact(explicit.Value) != "" {
		return *explicit
	}
	return ExternalKey{Namespace: "hai/" + string(kind), Value: compact(recordID)}
}

func (s *Service) findOrRecordProjectionEntity(ctx context.Context, request RecordEntityRequest) (Entity, error) {
	existing, err := s.QueryEntities(ctx, request.OwnerIdentity, EntityQuery{
		Types: []EntityType{request.Type}, ExternalKeys: request.ExternalKeys,
		AllowLocalOnly: true, Limit: maximumLimit,
	})
	if err != nil {
		return Entity{}, err
	}
	if len(existing) > 0 {
		return existing[0], nil
	}
	created, err := s.RecordEntity(ctx, request)
	if err != nil {
		return Entity{}, err
	}
	return created.Entity, nil
}
