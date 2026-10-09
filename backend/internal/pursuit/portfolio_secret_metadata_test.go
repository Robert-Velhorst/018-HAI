package pursuit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"automation-hub-backend/internal/resourceplanner"
)

func TestPortfolioTypedTokenBudgetsRemainDigestBoundAndSecretChecked(t *testing.T) {
	in, out := int64(300), int64(150)
	request := PortfolioPlanningRequest{
		PlanID:         "bounded-plan",
		Budget:         resourceplanner.Budget{MaxInputTokens: &in, MaxOutputTokens: &out},
		ApprovalPolicy: resourceplanner.ApprovalPolicy{InputTokenThreshold: &in, OutputTokenThreshold: &out},
		Pursuits: []PortfolioPursuitPlanningInput{{
			EstimatedUsage: resourceplanner.Usage{InputTokens: 40, OutputTokens: 20},
			Calibration: &PortfolioEstimateCalibrationBinding{
				ScopeKey: "bounded-scope", SourceEstimatedUsage: resourceplanner.Usage{InputTokens: 80, OutputTokens: 10},
			},
		}},
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(encoded)
	got, err := digestPortfolioPayload(request)
	if err != nil || got != hex.EncodeToString(want[:]) {
		t.Fatalf("typed metadata rejected or digest no longer binds original bytes: %s, %v", got, err)
	}
	in++
	changed, err := digestPortfolioPayload(request)
	if err != nil || changed == got {
		t.Fatal("changed token budget was absent from the digest")
	}
	request.PlanID = "token=synthetic-secret"
	if _, err := digestPortfolioPayload(request); err == nil {
		t.Fatal("typed payload secret escaped scanning")
	}
	request.PlanID = "bounded-plan"
	request.Pursuits[0].Calibration.ScopeKey = "token=synthetic-nested-secret"
	if _, err := digestPortfolioPayload(request); err == nil {
		t.Fatal("nested calibration secret escaped scanning")
	}
	for _, untyped := range []any{
		map[string]any{"maxInputTokens": "synthetic-secret"},
		map[string]any{"token": 123},
		map[string]any{"budget": map[string]any{"maxInputTokens": 300}},
	} {
		if _, err := digestPortfolioPayload(untyped); err == nil {
			t.Fatal("generic sensitive-key payload was permitted by typed exception")
		}
	}
}

func TestSettlementExecutionTargetRetainsSecretScanAndOriginalDigest(t *testing.T) {
	payload := portfolioWorkflowSettlementDigestPayload{AuthorizationTarget: "hai://workflow/verified"}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	legacy := struct {
		OwnerIdentity, ProposalItemID, ProposalItemDigest, ApprovalDecisionID, ApprovalDecisionDigest string
		ReceiptID, ReceiptDigest, ConsumptionDigest, AuthorizationTarget, WorkflowID                  string
		AttestationID, AttestationDigest, ReservationID                                               string
		ActualEffortMinutes, ActualCostMicros                                                         int64
	}{AuthorizationTarget: payload.AuthorizationTarget}
	legacyEncoded, err := json.Marshal(legacy)
	if err != nil || string(legacyEncoded) != string(encoded) {
		t.Fatal("named settlement payload changed legacy digest serialization")
	}
	want := sha256.Sum256(encoded)
	got, err := digestReservationPayload(payload)
	if err != nil || got != hex.EncodeToString(want[:]) {
		t.Fatalf("destination metadata rejected or original digest changed: %s, %v", got, err)
	}
	payload.AuthorizationTarget = "https://example.invalid/action?sessionToken=synthetic-secret"
	if _, err := digestReservationPayload(payload); err == nil {
		t.Fatal("credential-bearing execution destination was accepted")
	}
	if _, err := digestReservationPayload(map[string]any{"AuthorizationTarget": "synthetic-secret"}); err == nil {
		t.Fatal("generic credential-labelled field received typed exception")
	}
}
