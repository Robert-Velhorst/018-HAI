package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"automation-hub-backend/internal/models"
)

func TestFrameworkSelectionDecisionReadsLegacyDescriptiveReason(t *testing.T) {
	selection := testFrameworkSelection("legacy-descriptive-plan")
	payload, err := json.Marshal(selection)
	if err != nil {
		t.Fatal(err)
	}
	reason := fmt.Sprintf("catalog %s digest %s; constitution v%d digest %s",
		selection.CatalogVersion, selection.CatalogDigest,
		selection.ConstitutionVersion, selection.ConstitutionDigest)
	decoded, err := decodeFrameworkSelectionDecision(models.WorkflowDecision{
		DecisionType: frameworkSelectionDecisionType,
		Decision:     selection.SelectionDecisionID,
		Reason:       reason,
		RuleApplied:  string(payload),
	})
	if err != nil {
		t.Fatalf("decode legacy descriptive reason: %v", err)
	}
	if decoded != selection {
		t.Fatal("decoded legacy provenance differs from its original payload")
	}
}

func TestFrameworkSelectionLegacyReasonDoesNotBypassProvenanceValidation(t *testing.T) {
	selection := testFrameworkSelection("legacy-summary-guards")
	payload, err := json.Marshal(selection)
	if err != nil {
		t.Fatal(err)
	}
	reason := fmt.Sprintf("catalog %s digest %s; constitution v%d digest %s",
		selection.CatalogVersion, selection.CatalogDigest,
		selection.ConstitutionVersion, selection.ConstitutionDigest)
	for _, test := range []struct {
		name   string
		mutate func(*models.WorkflowDecision)
		valid  bool
	}{
		{name: "catalog version", mutate: func(d *models.WorkflowDecision) {
			d.Reason = strings.Replace(d.Reason, "catalog v1 ", "catalog v2 ", 1)
		}},
		{name: "catalog digest", mutate: func(d *models.WorkflowDecision) {
			d.Reason = strings.Replace(d.Reason, selection.CatalogDigest, strings.Repeat("f", 64), 1)
		}},
		{name: "constitution version", mutate: func(d *models.WorkflowDecision) {
			d.Reason = strings.Replace(d.Reason, "constitution v1 ", "constitution v2 ", 1)
		}},
		{name: "constitution digest", mutate: func(d *models.WorkflowDecision) {
			d.Reason = strings.Replace(d.Reason, selection.ConstitutionDigest, strings.Repeat("f", 64), 1)
		}},
		{name: "malformed legacy JSON", mutate: func(d *models.WorkflowDecision) {
			d.RuleApplied = "{"
		}},
		{name: "decision identity", mutate: func(d *models.WorkflowDecision) {
			d.Decision = "different-decision"
		}},
		{name: "malformed primary JSON never falls back", mutate: func(d *models.WorkflowDecision) {
			d.Reason = "{"
		}},
		{name: "unrecognized summary", mutate: func(d *models.WorkflowDecision) {
			d.Reason = "unrecognized historical text"
		}},
		{name: "primary JSON ignores legacy field", valid: true, mutate: func(d *models.WorkflowDecision) {
			d.Reason, d.RuleApplied = string(payload), "invalid legacy field"
		}},
		{name: "empty summary remains compatible", valid: true, mutate: func(d *models.WorkflowDecision) {
			d.Reason = ""
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			decision := models.WorkflowDecision{
				DecisionType: frameworkSelectionDecisionType, Decision: selection.SelectionDecisionID,
				Reason: reason, RuleApplied: string(payload),
			}
			test.mutate(&decision)
			decoded, err := decodeFrameworkSelectionDecision(decision)
			if test.valid {
				if err != nil || decoded != selection {
					t.Fatalf("valid provenance rejected: %v", err)
				}
			} else if err == nil {
				t.Fatal("invalid provenance accepted through legacy compatibility")
			}
		})
	}
}
