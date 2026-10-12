package executionauth

import (
	"context"
	"fmt"
)

func (s *Service) verifyFrameworkSelection(
	ctx context.Context,
	request Request,
	receipt *Receipt,
) error {
	if request.Governance == nil || request.Governance.FrameworkSelectionID == "" {
		return nil
	}
	if request.Governance.FrameworkSelectorAlgorithmVersion != frameworkSelectorV5 {
		return fmt.Errorf("legacy framework selections are read-only; fresh selector-v5 planning is required before execution")
	}
	if s.frameworks == nil {
		return fmt.Errorf("framework selection resolver is unavailable")
	}
	governance := request.Governance
	resolved, err := s.frameworks.ResolveFrameworkSelection(
		ctx,
		request.OwnerIdentity,
		governance.FrameworkSelectionID,
	)
	if err != nil {
		return fmt.Errorf("resolve framework selection: %w", err)
	}
	if governance.FrameworkMaximumAutonomyLevel == nil ||
		governance.FrameworkRequiresApproval == nil {
		return fmt.Errorf("selector-v5 execution contract is incomplete")
	}
	if resolved.SelectionID != governance.FrameworkSelectionID ||
		resolved.TaskPlanID != governance.TaskPlanID ||
		resolved.CatalogVersion != governance.FrameworkCatalogVersion ||
		resolved.SelectorAlgorithmVersion != governance.FrameworkSelectorAlgorithmVersion ||
		resolved.TaskRiskLevel != governance.FrameworkTaskRiskLevel ||
		resolved.EffectiveRiskCeiling != governance.FrameworkEffectiveRiskCeiling ||
		resolved.MaximumAutonomyLevel != *governance.FrameworkMaximumAutonomyLevel ||
		resolved.RequiresApproval != *governance.FrameworkRequiresApproval ||
		resolved.CatalogDigest != governance.FrameworkCatalogDigest ||
		resolved.PreferenceDigest != governance.FrameworkPreferenceDigest ||
		resolved.ConstitutionDigest != governance.FrameworkConstitutionDigest ||
		resolved.OperatingContractDigest != governance.FrameworkOperatingContractDigest {
		return fmt.Errorf("framework governance does not match the immutable selection")
	}
	receipt.Evidence.FrameworkSelection = FrameworkSelectionVerificationEvidence{
		SelectionID:              resolved.SelectionID,
		SelectorAlgorithmVersion: resolved.SelectorAlgorithmVersion,
		OwnerScoped:              true,
		Verified:                 true,
	}
	return nil
}
