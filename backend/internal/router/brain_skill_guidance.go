package router

import (
	"context"
	"errors"
	"fmt"
	"time"

	brainskillselection "automation-hub-backend/internal/brain_skill_selection"
	"automation-hub-backend/internal/brainskills"
	"automation-hub-backend/internal/task"
)

const brainSkillSelectionLookupTimeout = 5 * time.Second

type brainSkillTaskGuidanceSource interface {
	GuidanceForTask(context.Context, string, string, string) ([]brainskillselection.AppliedGuidance, error)
}

type brainSkillTaskGuidanceAdapter struct {
	runtimeContext context.Context
	source         brainSkillTaskGuidanceSource
}

func newBrainSkillTaskGuidanceAdapter(runtimeContext context.Context, source brainSkillTaskGuidanceSource) *brainSkillTaskGuidanceAdapter {
	if runtimeContext == nil {
		runtimeContext = context.Background()
	}
	return &brainSkillTaskGuidanceAdapter{runtimeContext: runtimeContext, source: source}
}

func (a *brainSkillTaskGuidanceAdapter) GuidanceForTask(ownerIdentity, taskType, request string) ([]task.BrainSkillGuidanceItem, error) {
	if a == nil || a.source == nil {
		return nil, errors.New("brain skill guidance source is unavailable")
	}
	if err := brainskills.ValidateMatchInput(taskType, request); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(a.runtimeContext, brainSkillSelectionLookupTimeout)
	defer cancel()
	selected, err := a.source.GuidanceForTask(ctx, ownerIdentity, taskType, request)
	if err != nil {
		return nil, err
	}
	if err := validateBrainSkillTaskGuidance(selected, taskType, request); err != nil {
		return nil, err
	}
	items := make([]task.BrainSkillGuidanceItem, 0, len(selected))
	for _, guidance := range selected {
		items = append(items, task.BrainSkillGuidanceItem{
			SkillID:           guidance.ID,
			SkillName:         guidance.Name,
			Guidance:          guidance.Summary,
			SourceCommit:      guidance.Commit,
			SourceSHA256:      guidance.SourceSHA256,
			GuidanceSHA256:    guidance.GuidanceSHA256,
			ConsentDecisionID: guidance.SelectionDecisionID,
		})
	}
	return items, nil
}

func validateBrainSkillTaskGuidance(selected []brainskillselection.AppliedGuidance, taskType, request string) error {
	if len(selected) == 0 {
		return nil
	}
	catalog := brainskills.DefaultCatalog()
	known := make(map[string]brainskills.Skill)
	for _, skill := range catalog.List() {
		known[skill.ID] = skill
	}
	matched := make(map[string]struct{})
	for _, skill := range catalog.Match(taskType, request) {
		matched[skill.ID] = struct{}{}
	}
	if len(selected) > len(matched) {
		return errors.New("brain skill guidance exceeds the reviewed task matches")
	}

	seen := make(map[string]struct{}, len(selected))
	for _, guidance := range selected {
		skill, exists := known[guidance.ID]
		_, relevant := matched[guidance.ID]
		summary, hasSummary := catalog.GuidanceFor(guidance.ID)
		if !exists || !relevant || !hasSummary || guidance.Name != skill.Name ||
			guidance.Commit != skill.Commit || guidance.SourceSHA256 != skill.SourceSHA256 ||
			guidance.GuidanceSHA256 != skill.GuidanceSHA256 || guidance.Summary != summary ||
			guidance.SelectionDecisionID <= 0 {
			return fmt.Errorf("brain skill guidance for %q is not the current reviewed task-scoped selection", guidance.ID)
		}
		if _, duplicate := seen[guidance.ID]; duplicate {
			return fmt.Errorf("brain skill guidance contains duplicate skill %q", guidance.ID)
		}
		seen[guidance.ID] = struct{}{}
	}
	return nil
}

var _ task.BrainSkillGuidanceProvider = (*brainSkillTaskGuidanceAdapter)(nil)
