// Package agentguidance validates owner-consented HAI catalog summaries before
// they are passed to isolated, review-only agent proposal runners.
package agentguidance

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"automation-hub-backend/internal/brain_skill_selection"
	"automation-hub-backend/internal/brainskills"
)

const (
	maxItems       = 4
	maxItemBytes   = 1024
	maxTotalBytes  = 4096
	statusApplied  = "applied"
	statusNoMatch  = "not_applied"
	statusNoOwner  = "owner_unavailable"
	statusNoSource = "not_configured"
	statusLookup   = "unavailable"
	statusInvalid  = "invalid_selection"
)

var ErrInvalidGuidance = errors.New("agent skill guidance is not a current catalog-matched consent selection")

// Item is an internal runner payload. Its text is HAI-authored advisory
// context; it is never accepted from the public proposal request.
type Item struct {
	SkillID           string `json:"skillId"`
	Name              string `json:"name"`
	SourceCommit      string `json:"sourceCommit"`
	SourceSHA256      string `json:"sourceSHA256"`
	GuidanceSHA256    string `json:"guidanceSHA256"`
	ConsentDecisionID int64  `json:"consentDecisionId"`
	Guidance          string `json:"guidance"`
}

// Pin is safe response metadata showing exactly which consented summaries
// were included in a successful proposal request, without returning them.
type Pin struct {
	SkillID           string `json:"skillId"`
	Name              string `json:"name"`
	Repository        string `json:"repository"`
	SourcePath        string `json:"sourcePath"`
	SourceURL         string `json:"sourceUrl"`
	SourceCommit      string `json:"sourceCommit"`
	SourceCommitDate  string `json:"sourceCommitDate"`
	SourceSHA256      string `json:"sourceSHA256"`
	GuidanceSHA256    string `json:"guidanceSHA256"`
	License           string `json:"license"`
	LicensePath       string `json:"licensePath"`
	LicenseURL        string `json:"licenseUrl"`
	CatalogStatus     string `json:"catalogStatus"`
	Scope             string `json:"scope"`
	Bundled           bool   `json:"bundled"`
	Boundary          string `json:"boundary"`
	ConsentDecisionID int64  `json:"consentDecisionId"`
}

// Provider must return only current, owner-consented catalog selections.
type Provider interface {
	GuidanceForTask(context.Context, string, string, string) ([]brain_skill_selection.AppliedGuidance, error)
}

// Resolve is optional enrichment: it fails closed to no guidance while leaving
// the bounded proposal path available. The caller must supply authenticated
// owner identity from server context.
func Resolve(ctx context.Context, provider Provider, owner, taskType, request string) ([]Item, string) {
	if provider == nil {
		return nil, statusNoSource
	}
	if strings.TrimSpace(owner) == "" {
		return nil, statusNoOwner
	}
	selected, err := provider.GuidanceForTask(ctx, owner, taskType, request)
	if err != nil {
		return nil, statusLookup
	}
	items := make([]Item, 0, len(selected))
	for _, selection := range selected {
		items = append(items, Item{
			SkillID:           selection.ID,
			Name:              selection.Name,
			SourceCommit:      selection.Commit,
			SourceSHA256:      selection.SourceSHA256,
			GuidanceSHA256:    selection.GuidanceSHA256,
			ConsentDecisionID: selection.SelectionDecisionID,
			Guidance:          selection.Summary,
		})
	}
	if err := Validate(taskType, request, items); err != nil {
		return nil, statusInvalid
	}
	if len(items) == 0 {
		return nil, statusNoMatch
	}
	return items, statusApplied
}

// Validate checks both applicability and exact server-catalog values. A
// caller cannot smuggle arbitrary prompt text or stale consent pins to a
// runner through this internal API.
func Validate(taskType, request string, items []Item) error {
	if len(items) > maxItems {
		return fmt.Errorf("%w: too many items", ErrInvalidGuidance)
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
	seen := make(map[string]struct{}, len(items))
	totalBytes := 0
	for _, item := range items {
		skill, exists := known[item.SkillID]
		_, relevant := matched[item.SkillID]
		guidance, hasGuidance := catalog.GuidanceFor(item.SkillID)
		if !matchesCatalog(item, skill, exists, guidance, hasGuidance) || !relevant {
			return fmt.Errorf("%w: invalid item %q", ErrInvalidGuidance, item.SkillID)
		}
		if _, duplicate := seen[item.SkillID]; duplicate {
			return fmt.Errorf("%w: duplicate item %q", ErrInvalidGuidance, item.SkillID)
		}
		seen[item.SkillID] = struct{}{}
		totalBytes += len(item.Guidance)
		if totalBytes > maxTotalBytes {
			return fmt.Errorf("%w: total guidance exceeds limit", ErrInvalidGuidance)
		}
	}
	return nil
}

func Pins(items []Item) []Pin {
	if len(items) > maxItems {
		return nil
	}
	pins := make([]Pin, 0, len(items))
	catalog := brainskills.DefaultCatalog()
	known := make(map[string]brainskills.Skill)
	seen := make(map[string]struct{}, len(items))
	for _, skill := range catalog.List() {
		known[skill.ID] = skill
	}
	totalBytes := 0
	for _, item := range items {
		skill, exists := known[item.SkillID]
		guidance, hasGuidance := catalog.GuidanceFor(item.SkillID)
		if !matchesCatalog(item, skill, exists, guidance, hasGuidance) {
			return nil
		}
		if _, duplicate := seen[item.SkillID]; duplicate {
			return nil
		}
		seen[item.SkillID] = struct{}{}
		totalBytes += len(item.Guidance)
		if totalBytes > maxTotalBytes {
			return nil
		}
		licensePath := path.Join(path.Dir(skill.SourcePath), "LICENSE.txt")
		pins = append(pins, Pin{
			SkillID:           item.SkillID,
			Name:              item.Name,
			Repository:        skill.Repository,
			SourcePath:        skill.SourcePath,
			SourceURL:         skill.SourceURL,
			SourceCommit:      item.SourceCommit,
			SourceCommitDate:  brainskills.SourceCommitDate,
			SourceSHA256:      item.SourceSHA256,
			GuidanceSHA256:    item.GuidanceSHA256,
			License:           skill.License,
			LicensePath:       licensePath,
			LicenseURL:        skill.Repository + "/blob/" + skill.Commit + "/" + licensePath,
			CatalogStatus:     skill.Status,
			Scope:             skill.Scope,
			Bundled:           skill.Bundled,
			Boundary:          skill.Boundary,
			ConsentDecisionID: item.ConsentDecisionID,
		})
	}
	return pins
}

func matchesCatalog(item Item, skill brainskills.Skill, exists bool, guidance string, hasGuidance bool) bool {
	return exists && hasGuidance && item.Name == skill.Name &&
		item.SourceCommit == skill.Commit && item.SourceSHA256 == skill.SourceSHA256 &&
		item.GuidanceSHA256 == skill.GuidanceSHA256 && item.ConsentDecisionID > 0 &&
		item.Guidance == guidance && len(item.Guidance) > 0 && len(item.Guidance) <= maxItemBytes &&
		strings.TrimSpace(item.Guidance) == item.Guidance && !strings.ContainsAny(item.Guidance, "\r\n")
}
