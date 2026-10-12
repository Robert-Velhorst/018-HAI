package autonomy

import (
	"automation-hub-backend/internal/models"
	"automation-hub-backend/internal/safety"
)

func publicTelemetryText(value string) string {
	// Redact complete text before applying the presentation bound.
	runes := []rune(safety.RedactSecrets(value))
	if len(runes) > 4096 {
		return string(runes[:4096]) + " [truncated]"
	}
	return string(runes)
}

// HTTP projections must not mutate stored evidence or expose raw execution
// parameters. IDs, timing and numeric results remain available for correlation.
func publicOverview(source *Overview) *Overview {
	if source == nil {
		return nil
	}
	result := *source
	result.DecisionDiscipline = make(map[string]any)
	for _, key := range []string{"name", "newDependenciesDefault", "benchmarkClaims"} {
		if value, ok := source.DecisionDiscipline[key].(string); ok {
			result.DecisionDiscipline[key] = publicTelemetryText(value)
		}
	}
	if enabled, ok := source.DecisionDiscipline["enabled"].(bool); ok {
		result.DecisionDiscipline["enabled"] = enabled
	}
	if order, ok := source.DecisionDiscipline["order"].([]string); ok {
		publicOrder := make([]string, len(order))
		for index, value := range order {
			publicOrder[index] = publicTelemetryText(value)
		}
		result.DecisionDiscipline["order"] = publicOrder
	}
	result.RecentWorldStates = append([]models.AutonomyWorldState{}, source.RecentWorldStates...)
	for index := range result.RecentWorldStates {
		state := &result.RecentWorldStates[index]
		state.Snapshot = "[withheld from telemetry]"
		state.ObservationType = publicTelemetryText(state.ObservationType)
		state.State = publicTelemetryText(state.State)
		state.SourceRevision = publicTelemetryText(state.SourceRevision)
	}
	result.RecentActions = append([]models.AutonomyActionTrace{}, source.RecentActions...)
	for index := range result.RecentActions {
		action := &result.RecentActions[index]
		action.ActionPayload = ""
		action.InterfaceType = publicTelemetryText(action.InterfaceType)
		action.ActionType = publicTelemetryText(action.ActionType)
		action.Status = publicTelemetryText(action.Status)
		action.PolicyDecision = publicTelemetryText(action.PolicyDecision)
		action.PolicyReason = publicTelemetryText(action.PolicyReason)
		action.VerificationStatus = publicTelemetryText(action.VerificationStatus)
		action.ResultSummary = publicTelemetryText(action.ResultSummary)
	}
	result.RecentEvaluations = append([]models.AutonomyEvaluation{}, source.RecentEvaluations...)
	for index := range result.RecentEvaluations {
		result.RecentEvaluations[index].FailureMode = publicTelemetryText(result.RecentEvaluations[index].FailureMode)
	}
	result.RecentStressRuns = append([]models.AutonomyStressRun{}, source.RecentStressRuns...)
	for index := range result.RecentStressRuns {
		result.RecentStressRuns[index].Results = ""
	}
	result.Warnings = make([]string, len(source.Warnings))
	for index, warning := range source.Warnings {
		result.Warnings[index] = publicTelemetryText(warning)
	}
	return &result
}
