package braincatalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxOSSInsightRepositoryBytes = 128 << 10
	// OSS Insight currently returns 20 repositories for each eligible
	// collection. 800 permits a complete scan of the fixed 138-category
	// screening snapshot without silently dropping valid review candidates.
	maxOSSInsightDiscoveries    = 800
	ossInsightDiscoveryCacheTTL = 5 * time.Minute
)

// OSSInsightDiscoveryScope controls the already-screened collections from
// which repository names can be read. It is not an installation scope.
type OSSInsightDiscoveryScope string

const (
	OSSInsightCandidateScope  OSSInsightDiscoveryScope = "candidate"
	OSSInsightReviewableScope OSSInsightDiscoveryScope = "reviewable"
)

// OSSInsightRepositoryScout reads repository names only from collections HAI
// has already classified as candidates or represented operational capability.
// It cannot install, run, or add a project to HAI's catalog.
type OSSInsightRepositoryScout interface {
	DiscoverRepositories() (OSSInsightRepositoryDiscoveryReport, error)
	DiscoverReviewableRepositories() (OSSInsightRepositoryDiscoveryReport, error)
	DiscoverRepositoriesFor(OSSInsightDiscoveryScope) (OSSInsightRepositoryDiscoveryReport, error)
}

type OSSInsightRepositoryContextScout interface {
	DiscoverRepositoriesContext(context.Context) (OSSInsightRepositoryDiscoveryReport, error)
	DiscoverReviewableRepositoriesContext(context.Context) (OSSInsightRepositoryDiscoveryReport, error)
	DiscoverRepositoriesForContext(context.Context, OSSInsightDiscoveryScope) (OSSInsightRepositoryDiscoveryReport, error)
}

func discoverRepositoriesFor(ctx context.Context, scout OSSInsightRepositoryScout, scope OSSInsightDiscoveryScope) (OSSInsightRepositoryDiscoveryReport, error) {
	if contextual, ok := scout.(OSSInsightRepositoryContextScout); ok {
		return contextual.DiscoverRepositoriesForContext(ctx, scope)
	}
	return scout.DiscoverRepositoriesFor(scope)
}

type OSSInsightRepositoryDiscovery struct {
	Collection         string                `json:"collection"`
	Disposition        CollectionDisposition `json:"disposition"`
	Repository         string                `json:"repository"`
	SourceURL          string                `json:"sourceUrl"`
	Rationale          string                `json:"rationale"`
	ReviewTrack        string                `json:"reviewTrack"`
	Priority           int                   `json:"priority"`
	Risk               string                `json:"risk"`
	ReviewReason       string                `json:"reviewReason"`
	Triage             string                `json:"triage"`
	TriageReason       string                `json:"triageReason"`
	ReviewAllowed      bool                  `json:"reviewAllowed"`
	RelatedCollections []string              `json:"relatedCollections,omitempty"`
	RelatedSourceURLs  []string              `json:"relatedSourceUrls,omitempty"`
}

const (
	discoveryTriageManualReview = "manual_review"
	discoveryTriageReference    = "reference_only"
	discoveryTriageDeferred     = "deferred"
)

type discoveryTriage struct {
	status string
	reason string
}

// repositoryDiscoveryTriages captures repository-specific decisions made in
// the live OSS Insight reconciliation. It prevents a broad category match
// from presenting known overlapping control planes or high-risk execution
// surfaces as equally suitable implementation candidates.
var repositoryDiscoveryTriages = map[string]discoveryTriage{
	"hkuds/rag-anything":                          {discoveryTriageReference, "Overlaps HAI's bounded RAGFlow bridge and source-linked retrieval; require a measured retrieval gap before reconsideration."},
	"osu-nlp-group/hipporag":                      {discoveryTriageReference, "Overlaps HAI's source-linked graph and RAGFlow retrieval boundaries; do not create a second memory or graph authority."},
	"flowiseai/flowise":                           {discoveryTriageReference, "A broad visual agent and workflow platform would duplicate HAI's routing, approval, audit, and execution control plane."},
	"langgenius/dify":                             {discoveryTriageReference, "A broad agent and RAG platform would duplicate HAI's source, memory, routing, and workflow authorities."},
	"falkordb/falkordb":                           {discoveryTriageReference, "HAI retains Postgres and its candidate graph rather than adding another graph database without a measured scale need."},
	"apache/airflow":                              {discoveryTriageReference, "Airflow's scheduler and worker model overlaps HAI's bounded Temporal workflow layer."},
	"netflix/metaflow":                            {discoveryTriageReference, "Metaflow's orchestration and remote-compute model is outside HAI's local workflow authority."},
	"ray-project/ray":                             {discoveryTriageDeferred, "Distributed compute is not justified for the current local-first control plane and requires a separate resource and security design."},
	"zenml-io/zenml":                              {discoveryTriageReference, "MLOps pipelines and metadata would duplicate HAI's model policy, evaluation, provenance, and audit paths."},
	"e2b-dev/open-computer-use":                   {discoveryTriageDeferred, "A general computer-use surface must not create a second host-execution path outside HAI's controlled runtime and approvals."},
	"lavague-ai/lavague":                          {discoveryTriageDeferred, "Browser agent execution remains behind HAI's allowlisted verification and approval flow."},
	"web-infra-dev/midscene":                      {discoveryTriageDeferred, "Browser automation remains behind HAI's allowlisted verification and approval flow."},
	"nanobrowser/nanobrowser":                     {discoveryTriageDeferred, "Browser automation remains behind HAI's allowlisted verification and approval flow."},
	"aorwall/moatless-tools":                      {discoveryTriageDeferred, "Code-agent execution remains behind HAI's workspace-constrained mini-SWE and verification path."},
	"qodo-ai/qodo-cover":                          {discoveryTriageDeferred, "Automated test generation needs an approved repository scope, local model, and test sandbox before it can be reconsidered."},
	"awslabs/mcp":                                 {discoveryTriageDeferred, "Cloud-provider MCP tooling must not inherit account, credential, network, or spend authority from a catalog discovery."},
	"stripe/agent-toolkit":                        {discoveryTriageDeferred, "Financial account and action tooling requires a separate regulated-use and approval design."},
	"supabase-community/supabase-mcp":             {discoveryTriageDeferred, "Database MCP tooling requires a named local adapter and explicit data-scope review before consideration."},
	"tavily-ai/tavily-mcp":                        {discoveryTriageReference, "HAI's local SearXNG discovery path remains the preferred web-research boundary."},
	"google/adk-python":                           {discoveryTriageReference, "A general agent framework would create a parallel orchestration and tool-authority plane."},
	"google/adk-java":                             {discoveryTriageReference, "A general agent framework would create a parallel orchestration and tool-authority plane."},
	"agent-network-protocol/agentnetworkprotocol": {discoveryTriageReference, "HAI retains its bounded A2A interoperability profile instead of adopting another agent-network control plane."},
	"coral-protocol/anemoi":                       {discoveryTriageReference, "HAI retains its bounded A2A interoperability profile instead of adopting another agent-network control plane."},
	"coqui-ai/tts":                                {discoveryTriageDeferred, "Voice output needs explicit user intent, model provenance, retention, and delivery controls before adoption."},
	"suno-ai/bark":                                {discoveryTriageDeferred, "Voice output needs explicit user intent, model provenance, retention, and delivery controls before adoption."},
	"myshell-ai/openvoice":                        {discoveryTriageDeferred, "Voice output needs explicit user intent, model provenance, retention, and delivery controls before adoption."},
	"agenta-ai/agenta":                            {discoveryTriageReference, "Evaluation and prompt-management surfaces overlap HAI's bounded local evaluation and audit profiles."},
	"pydantic/logfire":                            {discoveryTriageReference, "Observability is covered by HAI audit plus opt-in Prometheus, Langfuse, OpenLIT, and MLflow profiles."},
	"uptrain-ai/uptrain":                          {discoveryTriageReference, "Evaluation is covered by HAI's bounded Promptfoo, DeepEval, Evidently, Garak, and LM Evaluation Harness profiles."},
	"microsoft/responsible-ai-toolbox":            {discoveryTriageReference, "Responsible-AI analysis requires a concrete measured fairness or model-risk use case before a separate toolbox is introduced."},
	"fairlearn/fairlearn":                         {discoveryTriageReference, "Fairness analysis requires a concrete measured fairness or model-risk use case before a separate toolbox is introduced."},
	"trusted-ai/aif360":                           {discoveryTriageReference, "Fairness analysis requires a concrete measured fairness or model-risk use case before a separate toolbox is introduced."},
	"trusted-ai/aix360":                           {discoveryTriageReference, "Explainability analysis requires a concrete measured model-risk use case before a separate toolbox is introduced."},
}

// OSSInsightKnownProfileHit is a source repository HAI already represents in
// its catalog. It keeps the discovery report complete without presenting a
// reviewed profile as a new integration candidate.
type OSSInsightKnownProfileHit struct {
	Repository         string   `json:"repository"`
	CatalogEntryIDs    []string `json:"catalogEntryIds"`
	RelatedCollections []string `json:"relatedCollections,omitempty"`
	RelatedSourceURLs  []string `json:"relatedSourceUrls,omitempty"`
}

type OSSInsightRepositoryDiscoveryReport struct {
	CheckedAt               string                          `json:"checkedAt"`
	SourceURL               string                          `json:"sourceUrl"`
	Available               bool                            `json:"available"`
	Cached                  bool                            `json:"cached"`
	Scope                   OSSInsightDiscoveryScope        `json:"scope"`
	CollectionsScreened     int                             `json:"collectionsScreened"`
	CandidateCollections    int                             `json:"candidateCollections"`
	ReviewableCollections   int                             `json:"reviewableCollections"`
	EligibleCollections     int                             `json:"eligibleCollections"`
	CollectionsChecked      int                             `json:"collectionsChecked"`
	RepositoriesChecked     int                             `json:"repositoriesChecked"`
	DuplicateSourceHits     int                             `json:"duplicateSourceHits"`
	MaximumDiscoveries      int                             `json:"maximumDiscoveries"`
	SourceQueryLimit        int                             `json:"sourceQueryLimit,omitempty"`
	CollectionsAtQueryLimit int                             `json:"collectionsAtQueryLimit,omitempty"`
	KnownProfileHits        int                             `json:"knownProfileHits"`
	KnownProfiles           []OSSInsightKnownProfileHit     `json:"knownProfiles,omitempty"`
	KnownProfilesTruncated  bool                            `json:"knownProfilesTruncated"`
	Discoveries             []OSSInsightRepositoryDiscovery `json:"discoveries,omitempty"`
	MissingCollections      []string                        `json:"missingCollections,omitempty"`
	UnavailableCollections  []string                        `json:"unavailableCollections,omitempty"`
	DiscoveriesTruncated    bool                            `json:"discoveriesTruncated"`
	Message                 string                          `json:"message"`
}

type ossInsightRepositoryScout struct {
	client  *http.Client
	now     func() time.Time
	cacheMu sync.Mutex
	cache   map[OSSInsightDiscoveryScope]ossInsightDiscoveryCache
}

type ossInsightDiscoveryCache struct {
	report  OSSInsightRepositoryDiscoveryReport
	expires time.Time
}

func NewOSSInsightRepositoryScout(client *http.Client) OSSInsightRepositoryScout {
	if client == nil {
		client = &http.Client{
			Timeout: 8 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return &ossInsightRepositoryScout{client: client, now: time.Now, cache: map[OSSInsightDiscoveryScope]ossInsightDiscoveryCache{}}
}

func (s *ossInsightRepositoryScout) DiscoverRepositories() (OSSInsightRepositoryDiscoveryReport, error) {
	return s.DiscoverRepositoriesContext(context.Background())
}

func (s *ossInsightRepositoryScout) DiscoverReviewableRepositories() (OSSInsightRepositoryDiscoveryReport, error) {
	return s.DiscoverReviewableRepositoriesContext(context.Background())
}

func (s *ossInsightRepositoryScout) DiscoverRepositoriesFor(scope OSSInsightDiscoveryScope) (OSSInsightRepositoryDiscoveryReport, error) {
	return s.DiscoverRepositoriesForContext(context.Background(), scope)
}

func (s *ossInsightRepositoryScout) DiscoverRepositoriesContext(ctx context.Context) (OSSInsightRepositoryDiscoveryReport, error) {
	return s.DiscoverRepositoriesForContext(ctx, OSSInsightCandidateScope)
}

func (s *ossInsightRepositoryScout) DiscoverReviewableRepositoriesContext(ctx context.Context) (OSSInsightRepositoryDiscoveryReport, error) {
	return s.DiscoverRepositoriesForContext(ctx, OSSInsightReviewableScope)
}

func (s *ossInsightRepositoryScout) DiscoverRepositoriesForContext(parent context.Context, scope OSSInsightDiscoveryScope) (OSSInsightRepositoryDiscoveryReport, error) {
	scope, err := normalizedDiscoveryScope(scope)
	if err != nil {
		return OSSInsightRepositoryDiscoveryReport{}, err
	}
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if cached, ok := s.cache[scope]; ok && cached.expires.After(s.now()) {
		report := cached.report
		report.Cached = true
		return report, nil
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, ossInsightRequestTimeout)
	defer cancel()
	report, err := s.discoverRepositories(ctx, scope)
	if err == nil {
		s.cache[scope] = ossInsightDiscoveryCache{report: report, expires: s.now().Add(ossInsightDiscoveryCacheTTL)}
	}
	return report, err
}

func (s *ossInsightRepositoryScout) discoverRepositories(ctx context.Context, scope OSSInsightDiscoveryScope) (OSSInsightRepositoryDiscoveryReport, error) {
	report := OSSInsightRepositoryDiscoveryReport{
		CheckedAt:           s.now().UTC().Format(time.RFC3339),
		SourceURL:           ossInsightCollectionsURL,
		Scope:               scope,
		CollectionsScreened: len(CollectionScreenings()),
		MaximumDiscoveries:  maxOSSInsightDiscoveries,
	}
	live, err := s.liveCollections(ctx)
	if err != nil {
		return report, err
	}
	candidates := candidateCollectionDecisions()
	reviewable := reviewableCollectionDecisions()
	report.CandidateCollections = len(candidates)
	report.ReviewableCollections = len(reviewable)
	decisions := repositoryDiscoveryDecisions(scope)
	report.EligibleCollections = len(decisions)
	known := catalogRepositoryEntryIDs()
	liveNames := map[string]bool{}
	discoveryIndex := map[string]int{}
	knownProfileIndex := map[string]int{}

	for _, collection := range live {
		liveNames[collection.Name] = true
		decision, ok := decisions[collection.Name]
		if !ok {
			continue
		}
		if !isDecimalID(collection.ID) {
			report.UnavailableCollections = append(report.UnavailableCollections, collection.Name)
			continue
		}
		repositoryResponse, err := s.collectionRepositories(ctx, collection.ID)
		if err != nil {
			report.UnavailableCollections = append(report.UnavailableCollections, collection.Name)
			continue
		}
		report.CollectionsChecked++
		report.RepositoriesChecked += len(repositoryResponse.Repositories)
		if repositoryResponse.QueryLimit > report.SourceQueryLimit {
			report.SourceQueryLimit = repositoryResponse.QueryLimit
		}
		if repositoryResponse.QueryLimit > 0 && len(repositoryResponse.Repositories) >= repositoryResponse.QueryLimit {
			report.CollectionsAtQueryLimit++
		}
		for _, repository := range repositoryResponse.Repositories {
			repository = strings.TrimSpace(repository)
			if repository == "" {
				continue
			}
			if entryIDs, found := known[strings.ToLower(repository)]; found {
				report.KnownProfileHits++
				key := strings.ToLower(repository)
				knownProfile := OSSInsightKnownProfileHit{
					Repository: repository, CatalogEntryIDs: append([]string(nil), entryIDs...),
					RelatedCollections: []string{collection.Name}, RelatedSourceURLs: []string{ossInsightCollectionRepositoriesURL(collection.ID)},
				}
				if existingIndex, found := knownProfileIndex[key]; found {
					report.KnownProfiles[existingIndex] = mergeKnownProfile(report.KnownProfiles[existingIndex], knownProfile)
				} else if len(report.KnownProfiles) < maxOSSInsightDiscoveries {
					knownProfileIndex[key] = len(report.KnownProfiles)
					report.KnownProfiles = append(report.KnownProfiles, knownProfile)
				} else {
					report.KnownProfilesTruncated = true
				}
				continue
			}
			item := newDiscovery(collection, decision, repository)
			key := strings.ToLower(item.Repository)
			if existingIndex, found := discoveryIndex[key]; found {
				report.DuplicateSourceHits++
				report.Discoveries[existingIndex] = mergeDiscovery(report.Discoveries[existingIndex], item)
				continue
			}
			if len(report.Discoveries) >= maxOSSInsightDiscoveries {
				report.DiscoveriesTruncated = true
				continue
			}
			discoveryIndex[key] = len(report.Discoveries)
			report.Discoveries = append(report.Discoveries, item)
		}
	}
	for name := range decisions {
		if !liveNames[name] {
			report.MissingCollections = append(report.MissingCollections, name)
		}
	}
	sort.Strings(report.MissingCollections)
	sort.Strings(report.UnavailableCollections)
	sort.Slice(report.Discoveries, func(i, j int) bool {
		if report.Discoveries[i].Priority != report.Discoveries[j].Priority {
			return report.Discoveries[i].Priority > report.Discoveries[j].Priority
		}
		return report.Discoveries[i].Repository < report.Discoveries[j].Repository
	})
	sort.Slice(report.KnownProfiles, func(i, j int) bool {
		return report.KnownProfiles[i].Repository < report.KnownProfiles[j].Repository
	})
	report.Available = report.CollectionsChecked > 0
	if !report.Available {
		return report, fmt.Errorf("OSS Insight repository collections were unavailable")
	}
	if scope == OSSInsightReviewableScope {
		report.Message = "HAI classified the complete collection index and read every repository row returned by OSS Insight for candidate and already-represented operational categories. The source endpoint is a ranked collection response, not a complete GitHub inventory; discoveries remain unreviewed and this scan did not add catalog entries, install software, create credentials, or execute a project."
	} else {
		report.Message = "HAI classified the complete collection index and read every repository row returned by OSS Insight for pre-screened candidate categories. The source endpoint is a ranked collection response, not a complete GitHub inventory; discoveries remain unreviewed and this scan did not add catalog entries, install software, create credentials, or execute a project."
	}
	return report, nil
}

func newDiscovery(collection ossInsightCollection, decision collectionDecision, repository string) OSSInsightRepositoryDiscovery {
	track, priority, risk, reason := discoveryReviewProfile(collection.Name, decision.disposition)
	triage, triageReason, reviewAllowed := repositoryDiscoveryTriage(repository)
	sourceURL := ossInsightCollectionRepositoriesURL(collection.ID)
	return OSSInsightRepositoryDiscovery{
		Collection:         collection.Name,
		Disposition:        decision.disposition,
		Repository:         repository,
		SourceURL:          sourceURL,
		Rationale:          decision.rationale,
		ReviewTrack:        track,
		Priority:           priority,
		Risk:               risk,
		ReviewReason:       reason,
		Triage:             triage,
		TriageReason:       triageReason,
		ReviewAllowed:      reviewAllowed,
		RelatedCollections: []string{collection.Name},
		RelatedSourceURLs:  []string{sourceURL},
	}
}

func repositoryDiscoveryTriage(repository string) (status string, reason string, reviewAllowed bool) {
	if decision, found := repositoryDiscoveryTriages[normalizedRepositorySlug(repository)]; found {
		return decision.status, decision.reason, false
	}
	return discoveryTriageManualReview, "No repository-specific exclusion is recorded. Verify fixed upstream metadata, licence, local deployment boundaries, and a no-op validation plan before creating a manual adapter review.", true
}

// discoveryReviewProfile ranks a category-level review path. It is not a
// claim about a discovered project's quality, security, license, or runtime
// readiness; those require the separate fixed-upstream metadata review.
func discoveryReviewProfile(collection string, disposition CollectionDisposition) (track string, priority int, risk string, reason string) {
	switch collection {
	case "AI Safety & Alignment", "AI Evaluation & Testing", "AI Red Teaming":
		return "verification", 90, "medium", "Can improve validation and redaction, but must remain no-write and use redacted fixtures."
	case "AI Observability", "Monitoring Tool":
		return "observability", 82, "medium", "Could improve traceability or metrics, subject to local hosting, retention, and redaction review."
	case "LLM Inference Engines", "LLM Gateway & Proxy", "ai-gateways", "Edge AI":
		return "local inference", 80, "medium", "Could strengthen local-first model serving or routing, subject to a loopback and EUR 0 policy review."
	case "Data Integration", "Business Management", "RAG Frameworks", "Multimodal AI":
		return "source intake", 76, "high", "May access connected information; requires explicit scope, retention, deletion, and source-provenance controls."
	case "MCP Servers", "Model Context Protocol (MCP) Client", "AI Browser Agents", "Coding Agents", "AI Coding Assistants", "AI Code Review":
		return "controlled execution", 72, "high", "May expose tools, browser, filesystem, or repository changes; requires a named local adapter and approval boundary."
	case "AI Agent Frameworks", "AI Workflow Orchestration", "Agent Harness", "A2A Protocol":
		return "orchestration", 68, "high", "Could overlap HAI orchestration; review only a narrow fixed-schema bridge that preserves HAI policy ownership."
	default:
		if disposition == CollectionRepresented {
			return "replacement review", 60, "medium", "Could complement an existing HAI profile; compare it against the current adapter before adding infrastructure."
		}
		return "capability review", 55, "medium", "Requires fixed-upstream metadata, license, local deployment, and no-op adapter review before any adoption decision."
	}
}

func mergeDiscovery(existing, incoming OSSInsightRepositoryDiscovery) OSSInsightRepositoryDiscovery {
	existing.RelatedCollections = appendUnique(existing.RelatedCollections, incoming.RelatedCollections...)
	existing.RelatedSourceURLs = appendUnique(existing.RelatedSourceURLs, incoming.RelatedSourceURLs...)
	if incoming.Priority > existing.Priority {
		incoming.RelatedCollections = existing.RelatedCollections
		incoming.RelatedSourceURLs = existing.RelatedSourceURLs
		return incoming
	}
	return existing
}

func mergeKnownProfile(existing, incoming OSSInsightKnownProfileHit) OSSInsightKnownProfileHit {
	existing.CatalogEntryIDs = appendUnique(existing.CatalogEntryIDs, incoming.CatalogEntryIDs...)
	sort.Strings(existing.CatalogEntryIDs)
	existing.RelatedCollections = appendUnique(existing.RelatedCollections, incoming.RelatedCollections...)
	existing.RelatedSourceURLs = appendUnique(existing.RelatedSourceURLs, incoming.RelatedSourceURLs...)
	return existing
}

func appendUnique(values []string, additions ...string) []string {
	for _, addition := range additions {
		found := false
		for _, value := range values {
			if value == addition {
				found = true
				break
			}
		}
		if !found {
			values = append(values, addition)
		}
	}
	return values
}

type ossInsightCollection struct {
	ID   string
	Name string
}

func (s *ossInsightRepositoryScout) liveCollections(ctx context.Context) ([]ossInsightCollection, error) {
	resp, err := ossInsightGET(ctx, s.client, ossInsightCollectionsURL)
	if err != nil {
		return nil, fmt.Errorf("OSS Insight collection request failed: %w", err)
	}
	defer resp.Body.Close()
	var payload struct {
		Data struct {
			Rows []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"rows"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxOSSInsightCollectionsBytes)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("OSS Insight collection response was invalid")
	}
	result := make([]ossInsightCollection, 0, len(payload.Data.Rows))
	for _, row := range payload.Data.Rows {
		name := strings.TrimSpace(row.Name)
		id := strings.TrimSpace(row.ID)
		if name != "" && id != "" {
			result = append(result, ossInsightCollection{ID: id, Name: name})
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("OSS Insight collection response contained no collections")
	}
	return result, nil
}

type ossInsightCollectionRepositoryResponse struct {
	Repositories []string
	QueryLimit   int
}

func (s *ossInsightRepositoryScout) collectionRepositories(ctx context.Context, id string) (ossInsightCollectionRepositoryResponse, error) {
	resp, err := ossInsightGET(ctx, s.client, ossInsightCollectionRepositoriesURL(id))
	if err != nil {
		return ossInsightCollectionRepositoryResponse{}, fmt.Errorf("OSS Insight repository request failed: %w", err)
	}
	defer resp.Body.Close()
	var payload struct {
		Data struct {
			Rows []struct {
				Repository string `json:"repo_name"`
			} `json:"rows"`
			Result struct {
				Limit int `json:"limit"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxOSSInsightRepositoryBytes)).Decode(&payload); err != nil {
		return ossInsightCollectionRepositoryResponse{}, fmt.Errorf("OSS Insight repository response was invalid")
	}
	repositories := make([]string, 0, len(payload.Data.Rows))
	for _, row := range payload.Data.Rows {
		if name := strings.TrimSpace(row.Repository); name != "" {
			repositories = append(repositories, name)
		}
	}
	return ossInsightCollectionRepositoryResponse{Repositories: repositories, QueryLimit: payload.Data.Result.Limit}, nil
}

func candidateCollectionDecisions() map[string]collectionDecision {
	decisions := map[string]collectionDecision{}
	for _, screening := range CollectionScreenings() {
		if screening.Disposition == CollectionCandidate {
			decisions[screening.Collection] = collectionDecision{rationale: screening.Rationale}
		}
	}
	return decisions
}

func reviewableCollectionDecisions() map[string]collectionDecision {
	decisions := map[string]collectionDecision{}
	for _, screening := range CollectionScreenings() {
		if screening.Disposition == CollectionCandidate || screening.Disposition == CollectionRepresented {
			decisions[screening.Collection] = collectionDecision{disposition: screening.Disposition, rationale: screening.Rationale}
		}
	}
	return decisions
}

func repositoryDiscoveryDecisions(scope OSSInsightDiscoveryScope) map[string]collectionDecision {
	if scope == OSSInsightReviewableScope {
		return reviewableCollectionDecisions()
	}
	return candidateCollectionDecisions()
}

func normalizedDiscoveryScope(scope OSSInsightDiscoveryScope) (OSSInsightDiscoveryScope, error) {
	switch OSSInsightDiscoveryScope(strings.ToLower(strings.TrimSpace(string(scope)))) {
	case "", OSSInsightCandidateScope:
		return OSSInsightCandidateScope, nil
	case OSSInsightReviewableScope:
		return OSSInsightReviewableScope, nil
	default:
		return "", fmt.Errorf("unsupported OSS Insight discovery scope")
	}
}

func catalogRepositories() map[string]bool {
	known := map[string]bool{}
	for repository := range catalogRepositoryEntryIDs() {
		known[repository] = true
	}
	return known
}

func catalogRepositoryEntryIDs() map[string][]string {
	known := map[string][]string{}
	for _, entry := range Entries() {
		if repository := repositorySlugFromURL(entry.UpstreamURL); repository != "" {
			known[repository] = appendUnique(known[repository], entry.ID)
		}
		for _, alias := range entry.RepositoryAliases {
			if repository := normalizedRepositorySlug(alias); repository != "" {
				known[repository] = appendUnique(known[repository], entry.ID)
			}
		}
	}
	for repository := range known {
		sort.Strings(known[repository])
	}
	return known
}

func repositorySlugFromURL(value string) string {
	parts := strings.Split(strings.TrimSuffix(strings.TrimSpace(value), "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	return normalizedRepositorySlug(parts[len(parts)-2] + "/" + parts[len(parts)-1])
}

func normalizedRepositorySlug(value string) string {
	parts := strings.Split(strings.Trim(strings.ToLower(strings.TrimSpace(value)), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

func ossInsightCollectionRepositoriesURL(id string) string {
	return "https://api.ossinsight.io/v1/collections/" + id + "/repos/"
}

func isDecimalID(value string) bool {
	if value == "" {
		return false
	}
	_, err := strconv.ParseUint(value, 10, 64)
	return err == nil
}
