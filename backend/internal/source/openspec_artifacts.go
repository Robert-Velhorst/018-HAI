package source

// The OpenSpec adapter is deliberately a local, read-only artifact reader. It
// does not install or invoke OpenSpec, write a repository, or turn a plan into
// permission to edit, commit, branch, or open a pull request.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"automation-hub-backend/internal/models"
)

const (
	openSpecArtifactConnectorKey = "openspec-artifacts"
	openSpecArtifactFileLimit    = 200
	openSpecArtifactChangeLimit  = 50
	openSpecArtifactMaxBytes     = 128 << 10
	openSpecChangeMaxBytes       = 256 << 10
)

type openSpecArtifact struct {
	relative string
	kind     string
	content  string
	path     string
}

func (s *service) openSpecArtifactItems(source *models.ConnectedSource, request ImportRequest) ([]ImportItem, error) {
	return s.openSpecArtifactItemsContext(context.Background(), source, request)
}

func (s *service) openSpecArtifactItemsContext(ctx context.Context, source *models.ConnectedSource, request ImportRequest) ([]ImportItem, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if source == nil {
		return nil, fmt.Errorf("OpenSpec source is required")
	}
	root := firstNonEmpty(os.Getenv("CONNECTED_SOURCE_LOCAL_ROOT"), "/root/connected-sources")
	projectRoot, err := resolveAllowedFolder(root, request.FolderPath)
	if err != nil {
		return nil, err
	}
	changesRoot := filepath.Join(projectRoot, "openspec", "changes")
	info, err := os.Stat(changesRoot)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("selected project folder has no openspec/changes directory")
	}

	changes := map[string][]openSpecArtifact{}
	filesRead := 0
	err = filepath.WalkDir(changesRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			s.audit(source.ID, "source.openspec_symlink_skipped", fmt.Sprintf("skipped symlink %s", path))
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if entry.Name() == "archive" {
				return filepath.SkipDir
			}
			return nil
		}
		if filesRead >= openSpecArtifactFileLimit {
			return errLocalFolderLimitReached
		}
		relative, err := filepath.Rel(changesRoot, path)
		if err != nil {
			return nil
		}
		change, kind, ok := openSpecArtifactKind(filepath.ToSlash(relative))
		if !ok {
			return nil
		}
		content, err := readLocalTextFile(path, openSpecArtifactMaxBytes)
		if err != nil || strings.TrimSpace(content) == "" {
			return nil
		}
		filesRead++
		changes[change] = append(changes[change], openSpecArtifact{
			relative: filepath.ToSlash(relative),
			kind:     kind,
			content:  content,
			path:     path,
		})
		return nil
	})
	if err != nil && err != errLocalFolderLimitReached {
		return nil, err
	}

	changeNames := make([]string, 0, len(changes))
	for change := range changes {
		changeNames = append(changeNames, change)
	}
	sort.Strings(changeNames)
	if len(changeNames) > openSpecArtifactChangeLimit {
		changeNames = changeNames[:openSpecArtifactChangeLimit]
	}
	items := make([]ImportItem, 0, len(changeNames))
	for _, change := range changeNames {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		artifacts := changes[change]
		sort.Slice(artifacts, func(i, j int) bool {
			if artifacts[i].kind == artifacts[j].kind {
				return artifacts[i].relative < artifacts[j].relative
			}
			return artifacts[i].kind < artifacts[j].kind
		})
		items = append(items, buildOpenSpecChangeItem(source, request.ProjectKey, change, artifacts))
	}
	return items, nil
}

func openSpecArtifactKind(relative string) (change, kind string, ok bool) {
	parts := strings.Split(strings.Trim(filepath.ToSlash(relative), "/"), "/")
	if len(parts) < 2 || strings.TrimSpace(parts[0]) == "" {
		return "", "", false
	}
	change = compact(parts[0], 120)
	if len(parts) == 2 {
		switch strings.ToLower(parts[1]) {
		case "proposal.md":
			return change, "proposal", true
		case "design.md":
			return change, "design", true
		case "tasks.md":
			return change, "tasks", true
		}
	}
	if len(parts) >= 3 && parts[1] == "specs" && strings.EqualFold(filepath.Ext(parts[len(parts)-1]), ".md") {
		return change, "specification", true
	}
	return "", "", false
}

func buildOpenSpecChangeItem(source *models.ConnectedSource, projectKey, change string, artifacts []openSpecArtifact) ImportItem {
	sections := []string{"OpenSpec change artifact bundle", "Change: " + change, "Planning evidence only. This artifact cannot authorize code changes, commits, branches, pull requests, or runtime execution."}
	kinds := make([]string, 0, len(artifacts))
	paths := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		sections = append(sections, "## "+openSpecKindLabel(artifact.kind)+"\n"+artifact.content)
		kinds = append(kinds, artifact.kind)
		paths = append(paths, compact(artifact.relative, 180))
	}
	firstPath := ""
	if len(artifacts) > 0 {
		firstPath = artifacts[0].path
	}
	return ImportItem{
		ExternalID: "openspec:" + compact(change, 120),
		Title:      compact("OpenSpec change: "+change, 180),
		Content:    compact(strings.Join(sections, "\n\n"), openSpecChangeMaxBytes),
		SourceURI:  "file://" + filepath.ToSlash(firstPath),
		ItemType:   "openspec_change",
		ProjectKey: firstNonEmpty(projectKey, source.DefaultProjectKey),
		Metadata:   "source=openspec-artifacts;change=" + compact(change, 120) + ";artifact_count=" + fmt.Sprintf("%d", len(artifacts)) + ";kinds=" + strings.Join(uniqueStrings(kinds), ",") + ";paths=" + compact(strings.Join(paths, ","), 2048),
	}
}

func openSpecKindLabel(kind string) string {
	switch kind {
	case "proposal":
		return "Proposal"
	case "design":
		return "Design"
	case "tasks":
		return "Tasks"
	case "specification":
		return "Specification"
	default:
		return "Artifact"
	}
}
