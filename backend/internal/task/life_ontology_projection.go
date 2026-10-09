package task

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"automation-hub-backend/internal/lifeontology"
	"automation-hub-backend/internal/safety"
)

const (
	maximumProjectedMemories   = 8
	maximumTaskProjectionLinks = 16
)

type LifeOntologyProjectionRecorder interface {
	ProjectOperationalRecord(context.Context, lifeontology.OperationalProjectionRequest) (lifeontology.OperationalProjectionResult, error)
}

func WithLifeOntologyProjection(base Service, recorder LifeOntologyProjectionRecorder) (Service, error) {
	implementation, ok := base.(*service)
	if !ok {
		return nil, fmt.Errorf("whole-life projection requires the built-in task service")
	}
	if recorder == nil {
		return nil, fmt.Errorf("whole-life projection recorder is required")
	}
	implementation.lifeOntologyProjector = recorder
	return implementation, nil
}

func (s *service) projectDurableCompletionPlan(plan *CompletionPlan, request IntakeRequest, mode string) {
	if s == nil || s.lifeOntologyProjector == nil || plan == nil {
		return
	}
	if strings.TrimSpace(plan.OwnerIdentity) == "" {
		plan.Events = append(plan.Events, event("life-ontology", "durable life-graph projection skipped because no verified owner scope is available"))
		return
	}
	projection, err := s.lifeOntologyProjector.ProjectOperationalRecord(
		context.Background(), completionPlanProjectionRequest(plan, request, mode),
	)
	if err != nil {
		plan.LifeGraphProjectionError = compactTaskRequest(safety.RedactSecrets(err.Error()))
		plan.Events = append(plan.Events, event("life-ontology", "durable life-graph projection needs review: "+plan.LifeGraphProjectionError))
		return
	}
	if !projection.AdvisoryOnly || projection.CanExecute || projection.GrantsAuthority {
		plan.LifeGraphProjectionError = "life-graph projection crossed its advisory-only authority boundary"
		plan.Events = append(plan.Events, event("life-ontology", plan.LifeGraphProjectionError))
		return
	}
	plan.LifeGraphProjection = &projection
	plan.Events = append(plan.Events, event(
		"life-ontology",
		fmt.Sprintf("projected task plan into the owner-scoped life graph with %d linked records and %d source-backed relations", len(projection.LinkedEntities), len(projection.Relations)),
	))
}

func completionPlanProjectionRequest(plan *CompletionPlan, request IntakeRequest, mode string) lifeontology.OperationalProjectionRequest {
	domain := lifeontology.DomainPersonalAdmin
	if plan.FrameworkDecision != nil {
		for _, assignment := range plan.FrameworkDecision.LifeDomains {
			if assignment.Primary {
				domain = firstMappedLifeOntologyDomain(assignment.ID, domain)
				break
			}
		}
	}
	verification := lifeontology.VerificationSchemaValidated
	confidence := 0.7
	outcomeUncertain := executionOutcomeUncertain(plan.ExecutionResult)
	completionStatus := plan.CompletionStatus
	if outcomeUncertain {
		completionStatus = "review_required"
		verification = lifeontology.VerificationNeedsReview
		confidence = 0.6
	} else if plan.ValidationResult.Passed {
		verification = lifeontology.VerificationVerified
		confidence = 1
	} else if request.HumanApproved {
		verification = lifeontology.VerificationHumanApproved
		confidence = 0.9
	} else if plan.CompletionStatus == "review_required" || plan.CompletionStatus == "retry_needed" {
		verification = lifeontology.VerificationNeedsReview
		confidence = 0.6
	}

	projection := lifeontology.OperationalProjectionRequest{
		OwnerIdentity: plan.OwnerIdentity, Type: lifeontology.EntityTask, RecordID: plan.ID,
		Domain: domain, Name: boundedText(firstNonEmpty(plan.RealGoal, plan.Request, "HAI task plan"), 256),
		Summary: compactTaskRequest(plan.Request), Status: completionLifeStatus(completionStatus),
		Priority: completionLifePriority(plan.RiskAssessment.Level), DueAt: request.Deadline,
		ObservedAt: plan.CreatedAt.UTC(), Confidence: confidence, VerificationStatus: verification,
		Attributes: map[string]string{
			"completion_status": strings.TrimSpace(completionStatus),
			"mode":              strings.TrimSpace(mode), "risk": strings.TrimSpace(plan.RiskAssessment.Level),
			"task_type":  strings.TrimSpace(plan.Intake.TaskType),
			"model_tier": strings.TrimSpace(plan.ModelDecision.Tier),
		},
		Provenance: []lifeontology.Provenance{{
			ReferenceID: plan.ID, URI: "hai://task-plans/" + plan.ID,
			ContentDigest: completionPlanProjectionDigest(plan, request, mode),
			Authority:     "hai_task_ledger", CapturedAt: plan.CreatedAt.UTC(), LocalOnly: true,
		}},
		Sensitivity: lifeontology.SensitivityInternal, LocalOnly: true,
	}
	if outcomeUncertain {
		projection.Attributes["runtime_outcome"] = "uncertain"
	}
	if strings.TrimSpace(plan.ProjectKey) != "" {
		projection.Links = append(projection.Links, lifeontology.OperationalLinkRequest{
			Type: lifeontology.EntityProject, RecordID: plan.ProjectKey,
			Name: boundedText("Project "+plan.ProjectKey, 256), Relation: lifeontology.RelationBelongsToProject,
		})
	}
	if strings.TrimSpace(plan.PursuitID) != "" {
		projection.Links = append(projection.Links, lifeontology.OperationalLinkRequest{
			Type: lifeontology.EntityPursuit, RecordID: plan.PursuitID,
			Name: boundedText("Pursuit "+plan.PursuitID, 256), Relation: lifeontology.RelationBelongsToPursuit,
		})
	}
	if strings.TrimSpace(request.WorkflowID) != "" {
		projection.Links = append(projection.Links, lifeontology.OperationalLinkRequest{
			Type: lifeontology.EntityWorkflow, RecordID: request.WorkflowID,
			Name: boundedText("Workflow "+request.WorkflowID, 256), Relation: lifeontology.RelationBelongsToWorkflow,
		})
	}
	for index, memoryID := range plan.StoredMemoryIDs {
		if outcomeUncertain || index == maximumProjectedMemories || len(projection.Links) == maximumTaskProjectionLinks {
			break
		}
		if strings.TrimSpace(memoryID) == "" {
			continue
		}
		projection.Links = append(projection.Links, lifeontology.OperationalLinkRequest{
			Type: lifeontology.EntityMemory, RecordID: memoryID,
			Name: boundedText("Verified task lesson "+memoryID, 256), Relation: lifeontology.RelationProduces,
		})
	}
	if plan.ExecutionResult != nil && len(projection.Links) < maximumTaskProjectionLinks {
		output := plan.ExecutionResult.Output
		if outcomeUncertain {
			output = "Unverified runtime output; execution may have occurred and requires reconciliation. " + output
		}
		projection.Links = append(projection.Links, lifeontology.OperationalLinkRequest{
			Type: lifeontology.EntityOutcome, RecordID: plan.ID + ":" + firstNonEmpty(completionStatus, "executed"),
			Name:    boundedText("Task outcome: "+firstNonEmpty(completionStatus, "executed"), 256),
			Summary: boundedText(output, 2048), Status: completionLifeStatus(completionStatus),
			Relation: lifeontology.RelationProduces,
		})
	}
	return projection
}

func completionPlanProjectionDigest(plan *CompletionPlan, request IntakeRequest, mode string) string {
	payload := struct {
		PlanID, Owner, PursuitID, WorkflowID, ProjectKey, Completion, Validation, Mode string
		CreatedAt                                                                      string
		OutcomeUncertain                                                               bool `json:"outcomeUncertain,omitempty"`
	}{
		PlanID: plan.ID, Owner: plan.OwnerIdentity, PursuitID: plan.PursuitID,
		WorkflowID: request.WorkflowID, ProjectKey: plan.ProjectKey,
		Completion: plan.CompletionStatus, Validation: plan.ValidationResult.Status,
		Mode: mode, CreatedAt: plan.CreatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
		OutcomeUncertain: executionOutcomeUncertain(plan.ExecutionResult),
	}
	encoded, _ := json.Marshal(payload)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func firstMappedLifeOntologyDomain(value string, fallback lifeontology.Domain) lifeontology.Domain {
	if mapped := mapLifeOntologyDomain(value); mapped != "" {
		return mapped
	}
	return fallback
}

func completionLifeStatus(value string) lifeontology.LifecycleStatus {
	switch strings.TrimSpace(value) {
	case "validated", "completed":
		return lifeontology.StatusCompleted
	case "review_required":
		return lifeontology.StatusWaiting
	case "planned", "retry_needed":
		return lifeontology.StatusActive
	default:
		return lifeontology.StatusUnknown
	}
}

func completionLifePriority(risk string) int {
	switch strings.TrimSpace(strings.ToLower(risk)) {
	case "critical":
		return 100
	case "high":
		return 90
	case "medium":
		return 65
	case "low":
		return 40
	default:
		return 50
	}
}

func boundedText(value string, limit int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(safety.RedactSecrets(value))), " ")
	if limit <= 0 || len([]rune(value)) <= limit {
		return value
	}
	return string([]rune(value)[:limit])
}
