package operationalgraph

import (
	"automation-hub-backend/internal/agentregistry"
	"automation-hub-backend/internal/frameworkregistry"
	"automation-hub-backend/internal/knowledgegraph"
	"automation-hub-backend/internal/memory"
	"automation-hub-backend/internal/models"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultMaxNodes    = 1000
	maximumMaxNodes    = 5000
	defaultQueryLimit  = 40
	maximumQueryLimit  = 100
	maximumDepth       = 5
	maximumCacheOwners = 64
	rootID             = "hai:operational-root"
)

type AgentLister interface {
	List(context.Context, string) ([]agentregistry.Agent, error)
	Get(context.Context, string, string) (agentregistry.Agent, error)
}
type TeamLister interface {
	ListTeams(string) ([]frameworkregistry.AgentTeamContract, error)
}
type WorkflowLister interface {
	ItemsForOwner(string, bool) ([]models.WorkflowItem, error)
}
type PursuitLister interface {
	ListForOwner(string, bool) ([]models.Pursuit, error)
}
type SourceLister interface {
	Sources(bool) ([]models.ConnectedSource, error)
}
type MemoryStore interface {
	FindAllForOwner(string, string, bool) ([]models.ContextMemory, error)
	CreateForOwner(string, memory.CreateRequest) (*models.ContextMemory, error)
}

type Service struct {
	knowledge       knowledgegraph.Repository
	knowledgeWriter *knowledgegraph.Service
	agents          AgentLister
	teams           TeamLister
	workflows       WorkflowLister
	pursuits        PursuitLister
	sources         SourceLister
	memories        MemoryStore
	clock           func() time.Time
	maxNodes        int
	cacheTTL        time.Duration
	cacheMu         sync.Mutex
	cache           map[string]cachedSnapshot
}

type cachedSnapshot struct {
	snapshot  Snapshot
	expiresAt time.Time
}

func NewService(knowledge knowledgegraph.Repository, writer *knowledgegraph.Service, agents AgentLister, teams TeamLister, workflows WorkflowLister, pursuits PursuitLister, sources SourceLister, memories MemoryStore, clock func() time.Time) (*Service, error) {
	if knowledge == nil || writer == nil || agents == nil || teams == nil || workflows == nil || pursuits == nil || sources == nil || memories == nil {
		return nil, fmt.Errorf("operational graph requires every governed projection dependency")
	}
	if clock == nil {
		clock = time.Now
	}
	return &Service{
		knowledge:       knowledge,
		knowledgeWriter: writer,
		agents:          agents,
		teams:           teams,
		workflows:       workflows,
		pursuits:        pursuits,
		sources:         sources,
		memories:        memories,
		clock:           clock,
		maxNodes:        envBoundedInt("OPERATIONAL_GRAPH_MAX_NODES", defaultMaxNodes, 100, maximumMaxNodes),
		cacheTTL:        time.Duration(envBoundedInt("OPERATIONAL_GRAPH_CACHE_SECONDS", 5, 0, 60)) * time.Second,
		cache:           make(map[string]cachedSnapshot),
	}, nil
}

func (s *Service) Snapshot(ctx context.Context, owner string) (Snapshot, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return Snapshot{}, fmt.Errorf("authenticated owner is required")
	}
	if snapshot, ok := s.cached(owner); ok {
		return snapshot, nil
	}
	snapshot, err := s.buildSnapshot(ctx, owner)
	if err == nil {
		s.storeCached(owner, snapshot)
	}
	return snapshot, err
}

func (s *Service) buildSnapshot(ctx context.Context, owner string) (Snapshot, error) {
	b := newBuilder(s.maxNodes, s.clock().UTC())
	b.addNode(Node{ID: rootID, Kind: "system", Layer: "system", Label: "HAI operational brain", Summary: "Owner-scoped map of governed work, knowledge, sources, and agents.", Status: "active", Weight: 1, VerificationStatus: "system_defined", Sensitivity: "internal", LocalOnly: true, UpdatedAt: s.clock().UTC()})

	agents, err := s.agents.List(ctx, owner)
	if err != nil {
		b.warn("Agent registry unavailable: " + safeError(err))
	} else {
		s.projectAgents(b, agents)
	}
	teams, err := s.teams.ListTeams(owner)
	if err != nil {
		b.warn("Agent teams unavailable: " + safeError(err))
	} else {
		s.projectTeams(b, teams)
	}
	pursuits, err := s.pursuits.ListForOwner(owner, false)
	if err != nil {
		b.warn("Pursuits unavailable: " + safeError(err))
	} else {
		s.projectPursuits(b, pursuits)
	}
	workflows, err := s.workflows.ItemsForOwner(owner, false)
	if err != nil {
		b.warn("Workflows unavailable: " + safeError(err))
	} else {
		s.projectWorkflows(b, workflows)
	}
	sources, err := s.sources.Sources(true)
	if err != nil {
		b.warn("Connected sources unavailable: " + safeError(err))
	} else {
		s.projectSources(b, owner, sources)
	}
	memories, err := s.memories.FindAllForOwner(owner, "", false)
	if err != nil {
		b.warn("Context memory unavailable: " + safeError(err))
	} else {
		s.projectMemories(b, memories)
	}
	knowledgeNodes, err := s.knowledge.ListNodes(ctx, owner, knowledgegraph.ListOptions{})
	if err != nil {
		b.warn("Knowledge nodes unavailable: " + safeError(err))
	} else {
		s.projectKnowledgeNodes(b, knowledgeNodes)
	}
	knowledgeEdges, err := s.knowledge.ListEdges(ctx, owner, knowledgegraph.ListOptions{})
	if err != nil {
		b.warn("Knowledge links unavailable: " + safeError(err))
	} else {
		s.projectKnowledgeEdges(b, knowledgeEdges)
	}

	return b.snapshot(), nil
}

func (s *Service) Search(ctx context.Context, owner, query, layer, status string, limit int) (SearchResult, error) {
	snapshot, err := s.Snapshot(ctx, owner)
	if err != nil {
		return SearchResult{}, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	layer = strings.TrimSpace(layer)
	status = strings.TrimSpace(status)
	limit = bounded(limit, defaultQueryLimit, 1, maximumQueryLimit)
	type scored struct {
		node  Node
		score int
	}
	matches := make([]scored, 0)
	for _, node := range snapshot.Nodes {
		if layer != "" && node.Layer != layer {
			continue
		}
		if status != "" && node.Status != status {
			continue
		}
		haystack := strings.ToLower(strings.Join([]string{node.Label, node.Summary, strings.Join(node.ProjectKeys, " "), strings.Join(node.Tags, " ")}, " "))
		score := 1
		if query != "" {
			label := strings.ToLower(node.Label)
			switch {
			case label == query:
				score = 100
			case strings.HasPrefix(label, query):
				score = 80
			case strings.Contains(label, query):
				score = 60
			case strings.Contains(haystack, query):
				score = 40
			default:
				continue
			}
		}
		matches = append(matches, scored{node: node, score: score})
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score == matches[j].score {
			if matches[i].node.Weight == matches[j].node.Weight {
				return matches[i].node.Label < matches[j].node.Label
			}
			return matches[i].node.Weight > matches[j].node.Weight
		}
		return matches[i].score > matches[j].score
	})
	result := SearchResult{Query: query, Total: len(matches), Explanation: "Exact, prefix, label substring, and metadata substring matches are ranked before operational weight."}
	for i := 0; i < len(matches) && i < limit; i++ {
		result.Results = append(result.Results, matches[i].node)
	}
	result.Truncated = len(matches) > len(result.Results)
	return result, nil
}

func (s *Service) Neighborhood(ctx context.Context, owner, id string, depth, limit int) (Neighborhood, error) {
	snapshot, err := s.Snapshot(ctx, owner)
	if err != nil {
		return Neighborhood{}, err
	}
	depth = bounded(depth, 1, 1, maximumDepth)
	limit = bounded(limit, 100, 1, 500)
	byID := nodeIndex(snapshot.Nodes)
	if _, ok := byID[id]; !ok {
		return Neighborhood{}, knowledgegraph.ErrNotFound
	}
	adjacency := adjacencyIndex(snapshot.Links)
	seen := map[string]int{id: 0}
	queue := []string{id}
	includedLinks := map[string]Link{}
	for len(queue) > 0 && len(seen) < limit {
		current := queue[0]
		queue = queue[1:]
		currentDepth := seen[current]
		if currentDepth >= depth {
			continue
		}
		for _, link := range adjacency[current] {
			next := link.TargetID
			if next == current {
				next = link.SourceID
			}
			if _, exists := byID[next]; !exists {
				continue
			}
			includedLinks[link.ID] = link
			if _, exists := seen[next]; !exists {
				seen[next] = currentDepth + 1
				queue = append(queue, next)
				if len(seen) == limit {
					break
				}
			}
		}
	}
	result := Neighborhood{RootID: id, Depth: depth, Truncated: len(seen) == limit, Explanation: "Bounded breadth-first traversal across owner-visible operational links."}
	for _, node := range snapshot.Nodes {
		if _, ok := seen[node.ID]; ok {
			result.Nodes = append(result.Nodes, node)
		}
	}
	for _, link := range snapshot.Links {
		if _, ok := includedLinks[link.ID]; ok {
			result.Links = append(result.Links, link)
		}
	}
	return result, nil
}

func (s *Service) Path(ctx context.Context, owner, fromID, toID string, maxHops int) (PathResult, error) {
	snapshot, err := s.Snapshot(ctx, owner)
	if err != nil {
		return PathResult{}, err
	}
	byID := nodeIndex(snapshot.Nodes)
	if _, ok := byID[fromID]; !ok {
		return PathResult{}, knowledgegraph.ErrNotFound
	}
	if _, ok := byID[toID]; !ok {
		return PathResult{}, knowledgegraph.ErrNotFound
	}
	maxHops = bounded(maxHops, 12, 1, 64)
	adjacency := adjacencyIndex(snapshot.Links)
	previous := map[string]string{}
	via := map[string]Link{}
	distance := map[string]int{fromID: 0}
	queue := []string{fromID}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current == toID {
			break
		}
		if distance[current] >= maxHops {
			continue
		}
		for _, link := range adjacency[current] {
			next := link.TargetID
			if next == current {
				next = link.SourceID
			}
			if _, seen := distance[next]; seen {
				continue
			}
			distance[next] = distance[current] + 1
			previous[next] = current
			via[next] = link
			queue = append(queue, next)
		}
	}
	result := PathResult{FromID: fromID, ToID: toID, NodeIDs: []string{}, Links: []Link{}, Explanation: "Shortest owner-visible path using bounded breadth-first search."}
	if _, found := distance[toID]; !found {
		return result, nil
	}
	result.Found = true
	cursor := toID
	reverseNodes := []string{cursor}
	reverseLinks := []Link{}
	for cursor != fromID {
		reverseLinks = append(reverseLinks, via[cursor])
		cursor = previous[cursor]
		reverseNodes = append(reverseNodes, cursor)
	}
	for i := len(reverseNodes) - 1; i >= 0; i-- {
		result.NodeIDs = append(result.NodeIDs, reverseNodes[i])
	}
	for i := len(reverseLinks) - 1; i >= 0; i-- {
		result.Links = append(result.Links, reverseLinks[i])
	}
	return result, nil
}

func (s *Service) AgentBoot(ctx context.Context, owner, agentID string) (AgentBootContext, error) {
	agent, err := s.agents.Get(ctx, owner, strings.TrimSpace(agentID))
	if err != nil {
		return AgentBootContext{}, err
	}
	teams, err := s.teams.ListTeams(owner)
	if err != nil {
		return AgentBootContext{}, err
	}
	boot := AgentBootContext{ContractVersion: ContractVersion, GeneratedAt: s.clock().UTC(), AgentID: agent.ID, AgentName: agent.Name, State: string(agent.State), Health: string(agent.Health.Status), RuntimeID: agent.Runtime.ID, RuntimeType: agent.Runtime.Type, AuthorityCeiling: agent.AuthorityCeiling, AutonomyCeiling: agent.AutonomyCeiling, RiskCeiling: "critical", ToolAllowlist: uniqueSorted(agent.ToolAllowlist), DataAllowlist: uniqueSorted(agent.DataAllowlist), FolderAllowlist: uniqueSorted(agent.FolderAllowlist), GrantsExecutionAuthority: false, ExecutionAuthorizationRequired: true, Explanation: "Context is inherited from active advisory team membership. It never grants execution, tool, approval, or external-effect authority."}
	for _, capability := range agent.Capabilities {
		boot.Capabilities = append(boot.Capabilities, capability.ID)
	}
	for _, team := range teams {
		if team.Status != frameworkregistry.AgentTeamActive {
			continue
		}
		for _, member := range team.Members {
			if member.AgentID != agent.ID || member.Status != frameworkregistry.TeamMemberActive {
				continue
			}
			boot.AuthorityCeiling = minimum(boot.AuthorityCeiling, team.AuthorityCeiling, member.AuthorityCeiling)
			boot.RiskCeiling = stricterRisk(boot.RiskCeiling, team.RiskCeiling, member.RiskCeiling)
			context := AgentTeamContext{ID: team.ID, Name: team.Name, Version: team.Version, Status: team.Status, RoleIDs: uniqueSorted(member.RoleIDs), CapabilityIDs: uniqueSorted(member.CapabilityIDs), AuthorityCeiling: minimum(team.AuthorityCeiling, member.AuthorityCeiling), RiskCeiling: stricterRisk(team.RiskCeiling, member.RiskCeiling), AdvisoryOnly: true}
			boot.Teams = append(boot.Teams, context)
			for _, role := range team.Roles {
				if !contains(member.RoleIDs, role.ID) {
					continue
				}
				boot.AuthorityCeiling = minimum(boot.AuthorityCeiling, role.AuthorityCeiling)
				boot.RiskCeiling = stricterRisk(boot.RiskCeiling, role.RiskCeiling)
				boot.ProhibitedActions = append(boot.ProhibitedActions, role.ProhibitedActions...)
				boot.EvidenceRequirements = append(boot.EvidenceRequirements, role.EvidenceRequirements...)
			}
		}
	}
	boot.Capabilities = uniqueSorted(boot.Capabilities)
	boot.ProhibitedActions = uniqueSorted(boot.ProhibitedActions)
	boot.EvidenceRequirements = uniqueSorted(boot.EvidenceRequirements)
	sort.Slice(boot.Teams, func(i, j int) bool { return boot.Teams[i].ID < boot.Teams[j].ID })
	return boot, nil
}

func (s *Service) RecordMemory(owner string, request MemoryWriteRequest) (*models.ContextMemory, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return nil, fmt.Errorf("authenticated owner is required")
	}
	if err := validateText("content", request.Content, 1, 20000); err != nil {
		return nil, err
	}
	if err := validateText("summary", request.Summary, 0, 1000); err != nil {
		return nil, err
	}
	created, err := s.memories.CreateForOwner(owner, memory.CreateRequest{ProjectKey: clean(request.ProjectKey, 255), Kind: clean(firstNonEmpty(request.Kind, "operational"), 50), Content: strings.TrimSpace(request.Content), Summary: strings.TrimSpace(request.Summary), Tags: boundedStrings(request.Tags, 20, 80), Confidence: request.Confidence, SourceURI: clean(request.SourceURI, 1024), SourceLabel: clean(request.SourceLabel, 255)})
	if err == nil {
		s.invalidate(owner)
	}
	return created, err
}

func (s *Service) RecordReport(ctx context.Context, owner string, request ReportWriteRequest) (knowledgegraph.NodeWriteResult, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return knowledgegraph.NodeWriteResult{}, fmt.Errorf("authenticated owner is required")
	}
	status := strings.ToLower(strings.TrimSpace(request.Status))
	if status != "ok" && status != "warn" && status != "fail" {
		return knowledgegraph.NodeWriteResult{}, fmt.Errorf("status must be ok, warn, or fail")
	}
	if err := validateText("summary", request.Summary, 1, 2000); err != nil {
		return knowledgegraph.NodeWriteResult{}, err
	}
	if err := validateText("details", request.Details, 0, 12000); err != nil {
		return knowledgegraph.NodeWriteResult{}, err
	}
	now := s.clock().UTC()
	digest := sha256.Sum256([]byte(strings.Join([]string{owner, request.AgentID, status, request.Summary, request.Details, now.Format(time.RFC3339Nano)}, "\x00")))
	sources := []knowledgegraph.SourceReference{}
	if strings.TrimSpace(request.SourceURI) != "" {
		sources = append(sources, knowledgegraph.SourceReference{URI: clean(request.SourceURI, 1024), Label: "Operational report source", CapturedAt: now, LocalOnly: strings.HasPrefix(strings.TrimSpace(request.SourceURI), "local:")})
	}
	created, err := s.knowledgeWriter.CreateNode(ctx, knowledgegraph.CreateNodeRequest{OwnerIdentity: owner, Kind: knowledgegraph.NodeEvent, DeduplicationKey: "operational-report:" + hex.EncodeToString(digest[:]), Label: clean(request.Summary, 255), Content: strings.TrimSpace(request.Details), Properties: map[string]string{"operationalKind": "report", "reportStatus": status, "agentId": clean(request.AgentID, 160)}, ProjectKeys: boundedStrings([]string{request.ProjectKey}, 1, 255), Tags: append([]string{"operational-report", status}, boundedStrings(request.Tags, 20, 80)...), Confidence: 1, VerificationStatus: knowledgegraph.VerificationNeedsReview, Sources: sources, Sensitivity: knowledgegraph.SensitivityInternal, LocalOnly: true})
	if err == nil {
		s.invalidate(owner)
	}
	return created, err
}

func (s *Service) cached(owner string) (Snapshot, bool) {
	if s.cacheTTL <= 0 {
		return Snapshot{}, false
	}
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	entry, ok := s.cache[owner]
	if !ok {
		return Snapshot{}, false
	}
	if !s.clock().Before(entry.expiresAt) {
		delete(s.cache, owner)
		return Snapshot{}, false
	}
	return entry.snapshot, true
}

func (s *Service) storeCached(owner string, snapshot Snapshot) {
	if s.cacheTTL <= 0 {
		return
	}
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if len(s.cache) >= maximumCacheOwners {
		oldestOwner := ""
		var oldestExpiry time.Time
		for candidate, entry := range s.cache {
			if oldestOwner == "" || entry.expiresAt.Before(oldestExpiry) {
				oldestOwner = candidate
				oldestExpiry = entry.expiresAt
			}
		}
		delete(s.cache, oldestOwner)
	}
	s.cache[owner] = cachedSnapshot{snapshot: snapshot, expiresAt: s.clock().Add(s.cacheTTL)}
}

func (s *Service) invalidate(owner string) {
	s.cacheMu.Lock()
	delete(s.cache, strings.TrimSpace(owner))
	s.cacheMu.Unlock()
}

func (s *Service) projectAgents(b *builder, agents []agentregistry.Agent) {
	for _, a := range agents {
		id := "agent:" + a.ID
		b.addNode(Node{ID: id, Kind: "agent", Layer: "agents", Label: a.Name, Summary: string(a.Type) + " via " + a.Runtime.Type, Status: string(a.State), Weight: 0.9, ParentID: rootID, VerificationStatus: string(a.Health.Status), Sensitivity: "internal", LocalOnly: a.Performance.Locality == agentregistry.LocalityLocal, Details: sanitize(map[string]string{"runtimeId": a.Runtime.ID, "runtimeType": a.Runtime.Type, "authorityCeiling": strconv.Itoa(a.AuthorityCeiling), "autonomyCeiling": strconv.Itoa(a.AutonomyCeiling), "reliability": fmt.Sprintf("%.3f", a.Reliability.Score())}), UpdatedAt: a.UpdatedAt})
		b.addLink(rootID, id, "contains", "", 1)
	}
}
func (s *Service) projectTeams(b *builder, teams []frameworkregistry.AgentTeamContract) {
	for _, t := range teams {
		id := "team:" + t.ID
		b.addNode(Node{ID: id, Kind: "agent_team", Layer: "agents", Label: t.Name, Summary: t.Purpose, Status: t.Status, Weight: 0.85, ParentID: rootID, VerificationStatus: "contract_bound", Sensitivity: "internal", LocalOnly: true, SourceCount: len(t.EvidenceRefs), Details: sanitize(map[string]string{"version": t.Version, "riskCeiling": t.RiskCeiling, "authorityCeiling": strconv.Itoa(t.AuthorityCeiling), "advisoryOnly": strconv.FormatBool(t.AdvisoryOnly)}), UpdatedAt: t.UpdatedAt})
		b.addLink(rootID, id, "contains", "", 1)
		for _, m := range t.Members {
			b.addLink(id, "agent:"+m.AgentID, "has_member", "", 0.9)
		}
	}
}
func (s *Service) projectPursuits(b *builder, items []models.Pursuit) {
	for _, p := range items {
		project := b.ensureProject(p.ProjectKey)
		id := "pursuit:" + p.ID.String()
		parent := rootID
		if project != "" {
			parent = project
		}
		b.addNode(Node{ID: id, Kind: "pursuit", Layer: "work", Label: p.Title, Summary: firstNonEmpty(p.NextRecommendedAction, p.DesiredOutcome, p.Description), Status: p.Status, Weight: priorityWeight(p.PriorityScore), ParentID: parent, ProjectKeys: boundedStrings([]string{p.ProjectKey}, 1, 255), VerificationStatus: p.CompletionState, Sensitivity: "internal", LocalOnly: true, Details: sanitize(map[string]string{"risk": p.RiskLevel, "autonomy": p.AutonomyLevel, "domain": p.Domain, "completion": p.CompletionState}), UpdatedAt: p.UpdatedAt})
		b.addLink(parent, id, "contains", "", 1)
	}
}
func (s *Service) projectWorkflows(b *builder, items []models.WorkflowItem) {
	for _, w := range items {
		project := b.ensureProject(w.ProjectKey)
		id := "workflow:" + w.ID.String()
		parent := rootID
		if project != "" {
			parent = project
		}
		b.addNode(Node{ID: id, Kind: "workflow", Layer: "work", Label: w.Title, Summary: firstNonEmpty(w.NextAction, w.BlockedReason, w.Description), Status: w.CurrentState, Weight: priorityWeight(w.PriorityScore), ParentID: parent, ProjectKeys: boundedStrings([]string{w.ProjectKey}, 1, 255), VerificationStatus: w.VerificationStatus, Sensitivity: "internal", LocalOnly: true, SourceCount: boolCount(w.SourceURI != "" || w.SourceID != ""), Details: sanitize(map[string]string{"risk": w.RiskLevel, "approval": w.ApprovalStatus, "taskType": w.TaskType, "autonomy": w.AutonomyLevel, "sourceType": w.SourceType, "sourceUri": w.SourceURI}), UpdatedAt: w.UpdatedAt})
		b.addLink(parent, id, "contains", "", 1)
		if w.AutomationID != "" {
			b.addLink(id, "automation:"+w.AutomationID, "uses_automation", "", 0.7)
		}
	}
}
func (s *Service) projectSources(b *builder, owner string, items []models.ConnectedSource) {
	for _, src := range items {
		if strings.TrimSpace(src.OwnerIdentity) != owner {
			continue
		}
		id := "source:" + src.ID.String()
		project := b.ensureProject(src.DefaultProjectKey)
		parent := rootID
		if project != "" {
			parent = project
		}
		b.addNode(Node{ID: id, Kind: "source", Layer: "sources", Label: src.Name, Summary: src.Category + " via " + src.ConnectorKey, Status: src.Status, Weight: 0.7, ParentID: parent, ProjectKeys: boundedStrings([]string{src.DefaultProjectKey}, 1, 255), VerificationStatus: "configured", Sensitivity: "sensitive", LocalOnly: src.LocalOnly, Details: sanitize(map[string]string{"connector": src.ConnectorKey, "syncFrequency": src.SyncFrequency, "lastSyncedAt": timeString(src.LastSyncedAt), "permissions": src.Permissions}), UpdatedAt: src.UpdatedAt})
		b.addLink(parent, id, "contains", "", 1)
	}
}
func (s *Service) projectMemories(b *builder, items []models.ContextMemory) {
	for _, m := range items {
		id := "memory:" + m.ID.String()
		parent := b.ensureProject(m.ProjectKey)
		if parent == "" {
			parent = rootID
		}
		b.addNode(Node{ID: id, Kind: "memory", Layer: "memory", Label: firstNonEmpty(m.Summary, clean(m.Content, 120)), Summary: clean(m.Content, 500), Status: "active", Weight: m.Confidence, ParentID: parent, ProjectKeys: boundedStrings([]string{m.ProjectKey}, 1, 255), Tags: splitTags(m.Tags), VerificationStatus: verificationForMemory(m), Sensitivity: "internal", LocalOnly: true, SourceCount: boolCount(m.SourceURI != ""), Details: sanitize(map[string]string{"kind": m.Kind, "sourceUri": m.SourceURI, "sourceLabel": m.SourceLabel}), UpdatedAt: m.UpdatedAt})
		b.addLink(parent, id, "remembers", "", m.Confidence)
	}
}
func (s *Service) projectKnowledgeNodes(b *builder, items []knowledgegraph.Node) {
	for _, n := range items {
		id := "knowledge:" + n.ID
		parent := rootID
		if len(n.ProjectKeys) > 0 {
			if project := b.ensureProject(n.ProjectKeys[0]); project != "" {
				parent = project
			}
		}
		b.addNode(Node{ID: id, Kind: string(n.Kind), Layer: "knowledge", Label: n.Label, Summary: clean(n.Content, 500), Status: statusForVerification(string(n.VerificationStatus)), Weight: n.Confidence, ParentID: parent, ProjectKeys: n.ProjectKeys, Tags: n.Tags, VerificationStatus: string(n.VerificationStatus), Sensitivity: string(n.Sensitivity), LocalOnly: n.LocalOnly, SourceCount: len(n.Sources), Details: sanitize(n.Properties), UpdatedAt: n.UpdatedAt})
		b.addLink(parent, id, "contains", "", n.Confidence)
	}
}
func (s *Service) projectKnowledgeEdges(b *builder, items []knowledgegraph.Edge) {
	for _, e := range items {
		b.addLink("knowledge:"+e.FromNodeID, "knowledge:"+e.ToNodeID, string(e.Relationship), e.Label, e.Confidence)
	}
}

type builder struct {
	max       int
	generated time.Time
	nodes     map[string]Node
	order     []string
	links     map[string]Link
	linkOrder []string
	warnings  []string
	truncated bool
}

func newBuilder(max int, at time.Time) *builder {
	return &builder{max: max, generated: at, nodes: map[string]Node{}, links: map[string]Link{}}
}
func (b *builder) addNode(n Node) bool {
	if n.ID == "" || n.Label == "" {
		return false
	}
	if _, exists := b.nodes[n.ID]; exists {
		return true
	}
	if len(b.nodes) >= b.max {
		b.truncated = true
		return false
	}
	n.ProjectKeys = boundedStrings(n.ProjectKeys, 20, 255)
	n.Tags = boundedStrings(n.Tags, 30, 80)
	n.Details = sanitize(n.Details)
	b.nodes[n.ID] = n
	b.order = append(b.order, n.ID)
	return true
}
func (b *builder) addLink(source, target, kind, label string, weight float64) {
	if source == "" || target == "" || source == target {
		return
	}
	id := linkID(source, target, kind)
	if _, exists := b.links[id]; exists {
		return
	}
	b.links[id] = Link{ID: id, SourceID: source, TargetID: target, Type: clean(kind, 80), Label: clean(label, 160), Weight: weight}
	b.linkOrder = append(b.linkOrder, id)
}
func (b *builder) ensureProject(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	id := "project:" + strings.ToLower(key)
	b.addNode(Node{ID: id, Kind: "project", Layer: "work", Label: key, Status: "active", Weight: 0.8, ParentID: rootID, VerificationStatus: "derived", Sensitivity: "internal", LocalOnly: true, UpdatedAt: b.generated})
	b.addLink(rootID, id, "contains", "", 1)
	return id
}
func (b *builder) warn(value string) {
	if len(b.warnings) < 12 {
		b.warnings = append(b.warnings, clean(value, 300))
	}
}
func (b *builder) snapshot() Snapshot {
	result := Snapshot{ContractVersion: ContractVersion, GeneratedAt: b.generated, RootID: rootID, LayerCounts: map[string]int{}, Truncated: b.truncated, Warnings: b.warnings, Scope: "Authenticated owner-only operational projection. Read paths grant no execution authority and exclude secret-like metadata."}
	incoming := map[string]int{}
	for _, id := range b.linkOrder {
		l := b.links[id]
		if _, a := b.nodes[l.SourceID]; !a {
			continue
		}
		if _, a := b.nodes[l.TargetID]; !a {
			continue
		}
		result.Links = append(result.Links, l)
		incoming[l.TargetID]++
	}
	for _, id := range b.order {
		n := b.nodes[id]
		result.Nodes = append(result.Nodes, n)
		result.LayerCounts[n.Layer]++
		if n.ID != rootID && incoming[n.ID] == 0 {
			result.Quality.OrphanNodes++
		}
		if n.SourceCount > 0 {
			result.Quality.SourceBackedNodes++
		}
		if n.VerificationStatus == "needs_review" || n.VerificationStatus == "unsupported" || n.VerificationStatus == "conflicting" {
			result.Quality.NeedsReviewNodes++
		}
		if n.LocalOnly {
			result.Quality.LocalOnlyNodes++
		}
		if strings.Contains(strings.ToLower(n.Status), "block") || strings.Contains(strings.ToLower(n.Status), "fail") {
			result.Quality.BlockedNodes++
		}
	}
	return result
}

func sanitize(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := map[string]string{}
	for k, v := range values {
		key := strings.ToLower(strings.TrimSpace(k))
		if isSecretKey(key) {
			continue
		}
		v = clean(v, 512)
		if v != "" {
			out[clean(k, 80)] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
func isSecretKey(key string) bool {
	for _, term := range []string{"secret", "token", "password", "credential", "privatekey", "private_key", "apikey", "api_key", "authorization", "cookie"} {
		if strings.Contains(key, term) {
			return true
		}
	}
	return false
}
func safeError(err error) string {
	if err == nil {
		return ""
	}
	value := strings.ToLower(err.Error())
	for _, term := range []string{"password", "secret", "token", "credential"} {
		if strings.Contains(value, term) {
			return "redacted dependency error"
		}
	}
	return clean(err.Error(), 200)
}
func adjacencyIndex(links []Link) map[string][]Link {
	out := map[string][]Link{}
	for _, l := range links {
		out[l.SourceID] = append(out[l.SourceID], l)
		out[l.TargetID] = append(out[l.TargetID], l)
	}
	return out
}
func nodeIndex(nodes []Node) map[string]Node {
	out := map[string]Node{}
	for _, n := range nodes {
		out[n.ID] = n
	}
	return out
}
func linkID(source, target, kind string) string {
	h := sha256.Sum256([]byte(source + "\x00" + target + "\x00" + kind))
	return "link:" + hex.EncodeToString(h[:12])
}
func envBoundedInt(name string, fallback, min, max int) int {
	v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil {
		return fallback
	}
	return bounded(v, fallback, min, max)
}
func bounded(value, fallback, min, max int) int {
	if value == 0 {
		return fallback
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
func priorityWeight(score int) float64 {
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return 0.2 + float64(score)/125
}
func minimum(values ...int) int {
	result := values[0]
	for _, v := range values[1:] {
		if v < result {
			result = v
		}
	}
	return result
}
func stricterRisk(values ...string) string {
	rank := map[string]int{"low": 1, "medium": 2, "high": 3, "critical": 4}
	result := "critical"
	best := 4
	for _, v := range values {
		v = strings.ToLower(strings.TrimSpace(v))
		if r, ok := rank[v]; ok && r < best {
			best = r
			result = v
		}
	}
	return result
}
func contains(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}
func uniqueSorted(values []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
func boundedStrings(values []string, maxItems, maxLen int) []string {
	out := []string{}
	for _, v := range values {
		v = clean(v, maxLen)
		if v != "" {
			out = append(out, v)
		}
		if len(out) == maxItems {
			break
		}
	}
	return uniqueSorted(out)
}
func clean(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) > max {
		return value[:max]
	}
	return value
}
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func validateText(name, value string, min, max int) error {
	n := len(strings.TrimSpace(value))
	if n < min {
		return fmt.Errorf("%s is required", name)
	}
	if n > max {
		return fmt.Errorf("%s exceeds %d characters", name, max)
	}
	return nil
}
func splitTags(value string) []string { return boundedStrings(strings.Split(value, ","), 30, 80) }
func boolCount(v bool) int {
	if v {
		return 1
	}
	return 0
}
func verificationForMemory(m models.ContextMemory) string {
	if strings.TrimSpace(m.SourceURI) != "" {
		return "source_supported"
	}
	return "needs_review"
}
func statusForVerification(value string) string {
	switch value {
	case "unsupported", "conflicting", "needs_review", "uncertain":
		return "needs_review"
	case "verified", "human_approved", "test_passed", "source_supported", "schema_validated":
		return "verified"
	default:
		return "active"
	}
}
func timeString(value *time.Time) string {
	if value == nil {
		return "never"
	}
	return value.UTC().Format(time.RFC3339)
}
func (s *Service) IsNotFound(err error) bool {
	return errors.Is(err, knowledgegraph.ErrNotFound) || errors.Is(err, agentregistry.ErrNotFound)
}
