package source

// Fabric patterns are imported for explicit human review only. HAI never
// executes them, attaches them automatically, or lets them change policy.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"automation-hub-backend/internal/models"
)

const (
	fabricPatternsConnectorKey = "fabric-patterns"
	fabricPatternMaxCount      = 24
	fabricPatternMaxBytes      = 48 << 10
)

func ManualPlanningContextOnlyConnectorKeys() []string {
	return []string{projectInstructionsConnectorKey, fabricPatternsConnectorKey}
}

func isManualPlanningContextOnlyConnector(connectorKey string) bool {
	switch strings.TrimSpace(connectorKey) {
	case projectInstructionsConnectorKey, fabricPatternsConnectorKey:
		return true
	default:
		return false
	}
}

func (s *service) fabricPatternItems(source *models.ConnectedSource, request ImportRequest) ([]ImportItem, error) {
	return s.fabricPatternItemsContext(context.Background(), source, request)
}

func (s *service) fabricPatternItemsContext(ctx context.Context, source *models.ConnectedSource, request ImportRequest) ([]ImportItem, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if source == nil {
		return nil, fmt.Errorf("Fabric patterns source is required")
	}
	root := firstNonEmpty(os.Getenv("CONNECTED_SOURCE_LOCAL_ROOT"), "/root/connected-sources")
	patternsRoot, err := resolveAllowedFolder(root, request.FolderPath)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(patternsRoot)
	if err != nil {
		return nil, fmt.Errorf("read Fabric pattern directory: %w", err)
	}

	items := make([]ImportItem, 0, fabricPatternMaxCount)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(items) == fabricPatternMaxCount {
			break
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			continue
		}
		patternName := strings.TrimSpace(entry.Name())
		if patternName == "" {
			continue
		}
		patternPath := filepath.Join(patternsRoot, patternName)
		patternInfo, err := os.Lstat(patternPath)
		if err != nil {
			return nil, fmt.Errorf("inspect Fabric pattern %s: %w", patternName, err)
		}
		if patternInfo.Mode()&os.ModeSymlink != 0 || !patternInfo.IsDir() {
			s.audit(source.ID, "source.fabric_pattern_skipped", "skipped non-directory or symlink Fabric pattern "+patternName)
			continue
		}

		systemPath := filepath.Join(patternPath, "system.md")
		systemInfo, err := os.Lstat(systemPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("inspect Fabric pattern %s system.md: %w", patternName, err)
		}
		if systemInfo.Mode()&os.ModeSymlink != 0 || !systemInfo.Mode().IsRegular() {
			s.audit(source.ID, "source.fabric_pattern_skipped", "skipped non-regular Fabric pattern system.md for "+patternName)
			continue
		}
		content, err := readLocalTextFile(systemPath, fabricPatternMaxBytes)
		if err != nil {
			return nil, fmt.Errorf("read Fabric pattern %s system.md: %w", patternName, err)
		}
		if strings.TrimSpace(content) == "" {
			continue
		}
		items = append(items, ImportItem{
			ExternalID: "fabric-pattern:" + strings.ToLower(patternName),
			Title:      "Fabric pattern: " + patternName,
			Content: strings.Join([]string{
				"Fabric prompt pattern (untrusted manual-review context)",
				"HAI policy, source-grounding, approval gates, model routing, tool allowlists, workspace limits, and emergency stop always override this pattern. It is never automatically attached to a model or treated as authority to execute an action.",
				"## system.md",
				content,
			}, "\n\n"),
			SourceURI:  "file://" + filepath.ToSlash(systemPath),
			ItemType:   "fabric_prompt_pattern",
			ProjectKey: firstNonEmpty(request.ProjectKey, source.DefaultProjectKey),
			Metadata:   "source=fabric-patterns;pattern=" + patternName + ";trust=untrusted;usage=manual-planning-context-only",
		})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("selected Fabric pattern directory has no regular immediate-child system.md files")
	}
	return items, nil
}
