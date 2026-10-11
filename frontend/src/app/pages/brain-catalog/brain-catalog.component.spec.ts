import { of, throwError } from 'rxjs'
import { BrainCatalogComponent } from './brain-catalog.component'

describe('BrainCatalogComponent adapter reviews', () => {
  const candidate = {
    id: 'cline',
    name: 'Cline',
    upstreamUrl: 'https://github.com/cline/cline',
    sourceCatalogUrl: 'https://ossinsight.io/collections/llm-devtools',
    status: 'candidate',
    category: 'interactive coding agent',
    integrationMode: 'local bridge',
    capabilities: [],
    recommendedFor: [],
    requiresApproval: true,
    localFirstCompatible: true,
    activation: 'review first',
    rationale: 'Tool-mediated workspace access needs a boundary.',
    verifiedAt: '2026-07-19',
    verificationNote: 'reviewed',
  }

  function createComponent() {
    const catalogService = { adoptionPlan: jasmine.createSpy('adoptionPlan'), revalidate: jasmine.createSpy('revalidate'), revalidateOSSInsightCollections: jasmine.createSpy('revalidateOSSInsightCollections'), collectionRevalidationHistory: jasmine.createSpy('collectionRevalidationHistory'), repositoryDiscoveryRevalidationHistory: jasmine.createSpy('repositoryDiscoveryRevalidationHistory'), discoverOSSInsightRepositories: jasmine.createSpy('discoverOSSInsightRepositories'), discoverReviewableOSSInsightRepositories: jasmine.createSpy('discoverReviewableOSSInsightRepositories'), revalidateOSSInsightDiscovery: jasmine.createSpy('revalidateOSSInsightDiscovery'), recommendCapabilities: jasmine.createSpy('recommendCapabilities') }
    const pursuitService = { create: jasmine.createSpy('create') }
    const ragflowService = { status: jasmine.createSpy('status') }
    const anythingLLMService = { status: jasmine.createSpy('status') }
    const presidioService = { status: jasmine.createSpy('status') }
		const langfuseService = { status: jasmine.createSpy('status'), probe: jasmine.createSpy('probe'), exportOperationalSnapshot: jasmine.createSpy('exportOperationalSnapshot') }
    const serenaService = { status: jasmine.createSpy('status') }
    const openLITService = { status: jasmine.createSpy('status'), exportOTLP: jasmine.createSpy('exportOTLP') }
    const mlflowService = { status: jasmine.createSpy('status') }
    const miniSWEService = { status: jasmine.createSpy('status') }
    const gitleaksService = { status: jasmine.createSpy('status') }
    const gosecService = { status: jasmine.createSpy('status') }
    const trivyService = { status: jasmine.createSpy('status') }
    const grypeService = { status: jasmine.createSpy('status') }
    const syftService = { status: jasmine.createSpy('status') }
    const browserVerificationService = { status: jasmine.createSpy('status') }
    const autoGenCompatibilityService = { migration: jasmine.createSpy('migration') }
    const notification = jasmine.createSpyObj('NzNotificationService', ['success', 'error'])
    const router = { navigate: jasmine.createSpy('navigate') }
    return {
      component: new BrainCatalogComponent(
        catalogService as any,
        pursuitService as any,
        ragflowService as any,
        anythingLLMService as any,
        presidioService as any,
        langfuseService as any,
        serenaService as any,
        openLITService as any,
        mlflowService as any,
        miniSWEService as any,
        gitleaksService as any,
        gosecService as any,
        trivyService as any,
        grypeService as any,
        syftService as any,
        browserVerificationService as any,
        autoGenCompatibilityService as any,
        notification,
        router as any,
      ),
      pursuitService,
      ragflowService,
      anythingLLMService,
      presidioService,
			langfuseService,
      serenaService,
      notification,
      router,
    }
  }

  it('creates a review pursuit without claiming activation', () => {
    const { component, pursuitService, notification, router } = createComponent()
    pursuitService.create.and.returnValue(of({ id: 'pursuit-1' }))

    component.startAdapterReview(candidate as any)

    expect(pursuitService.create).toHaveBeenCalledWith(jasmine.objectContaining({
      title: 'Review Cline adapter boundary',
      status: 'waiting',
      autonomyLevel: 'manual',
      riskLevel: 'high',
      sourceOfCreation: 'brain_catalog:cline',
    }))
    expect(notification.success).toHaveBeenCalledWith('Adapter review created', 'Cline remains disabled. HAI created a review record instead of activating the project.')
    expect(router.navigate).toHaveBeenCalledWith(['/pursuits'], { queryParams: { selected: 'pursuit-1' } })
  })

  it('does not create a review for held catalog entries', () => {
    const { component, pursuitService } = createComponent()
    component.startAdapterReview({ ...candidate, status: 'excluded' } as any)
    expect(pursuitService.create).not.toHaveBeenCalled()
  })

  it('loads the read-only implementation roadmap without activating a project', () => {
    const { component, notification } = createComponent()
    const catalogService = (component as any).service
    catalogService.adoptionPlan.and.returnValue(of({ items: [{ id: 'cloudquery', name: 'CloudQuery', priority: 88 }], message: 'does not install or execute' }))

    component.loadAdoptionPlan()

    expect(catalogService.adoptionPlan).toHaveBeenCalled()
    expect(component.adoptionPlan?.items[0].id).toBe('cloudquery')
    expect(notification.error).not.toHaveBeenCalled()
  })

  it('routes a roadmap candidate through the existing manual adapter review flow', () => {
    const { component, pursuitService } = createComponent()
    component.catalog = { entries: [candidate] } as any
    pursuitService.create.and.returnValue(of({ id: 'pursuit-roadmap-1' }))

    component.startRoadmapReview('cline')

    expect(pursuitService.create).toHaveBeenCalledWith(jasmine.objectContaining({ sourceOfCreation: 'brain_catalog:cline' }))
  })

  it('keeps the candidate disabled after a create failure', () => {
    const { component, pursuitService, notification } = createComponent()
    pursuitService.create.and.returnValue(throwError(() => new Error('offline')))

    component.startAdapterReview(candidate as any)

    expect(component.reviewingCandidateId).toBe('')
    expect(notification.error).toHaveBeenCalledWith('Could not create adapter review', 'No project was installed, configured, or activated. Try again after checking the local pursuit service.')
  })

  it('retains confirmed maintenance history when a later history read is unavailable', () => {
    const { component } = createComponent()
    const catalogService = (component as any).service
    component.collectionMaintenanceHistory = [{ checkedAt: '2026-08-23T10:00:00Z' }] as any
    catalogService.collectionRevalidationHistory.and.returnValue(throwError(() => new Error('offline')))

    ;(component as any).loadCollectionMaintenanceHistory()

    expect(component.collectionMaintenanceHistory.length).toBe(1)
    expect(component.collectionMaintenanceHistoryUnavailable).toBeTrue()
  })

  it('retains confirmed repository gap-review history when a later read is unavailable', () => {
    const { component } = createComponent()
    const catalogService = (component as any).service
    component.repositoryDiscoveryMaintenanceHistory = [{ checkedAt: '2026-08-23T10:00:00Z' }] as any
    catalogService.repositoryDiscoveryRevalidationHistory.and.returnValue(throwError(() => new Error('offline')))

    ;(component as any).loadRepositoryDiscoveryMaintenanceHistory()

    expect(component.repositoryDiscoveryMaintenanceHistory.length).toBe(1)
    expect(component.repositoryDiscoveryMaintenanceHistoryUnavailable).toBeTrue()
  })

  it('shows an upstream recheck without changing the catalog entry', () => {
    const { component, notification } = createComponent()
    const catalogService = (component as any).service
    catalogService.revalidate.and.returnValue(of({
      id: 'cline',
      available: true,
      archived: false,
      license: 'Apache-2.0',
      message: 'metadata only',
    }))

    component.revalidate(candidate as any)

    expect(catalogService.revalidate).toHaveBeenCalledWith('cline')
    expect(component.upstreamReview?.license).toBe('Apache-2.0')
    expect(notification.success).toHaveBeenCalledWith('Upstream rechecked', 'Cline was checked without changing its HAI activation state.')
  })

	it('reports collection drift without changing catalog or runtime state', () => {
	  const { component, notification } = createComponent()
	  const catalogService = (component as any).service
	  catalogService.revalidateOSSInsightCollections.and.returnValue(of({
	    available: true,
	    expectedTotal: 138,
	    currentTotal: 139,
	    newCollections: ['Future capability'],
	    missingExpected: [],
	    message: 'drift only',
	  }))

	  component.revalidateOSSInsightCollections()

	  expect(catalogService.revalidateOSSInsightCollections).toHaveBeenCalled()
	  expect(component.ossInsightReview?.newCollections).toEqual(['Future capability'])
	  expect(notification.success).toHaveBeenCalledWith('OSS Insight checked', 'Collection drift needs catalog review. No project was installed or activated.')
	})

  it('creates a manual discovery review without adding a catalog profile', () => {
    const { component, pursuitService, notification, router } = createComponent()
    pursuitService.create.and.returnValue(of({ id: 'pursuit-discovery-1' }))
    component.discoveryReviews['owner/new-mcp'] = {
      id: 'ossinsight-owner-new-mcp',
      name: 'owner/new-mcp',
      upstreamUrl: 'https://github.com/owner/new-mcp',
      available: true,
      archived: false,
      repositoryMoved: false,
      license: 'MIT',
      message: 'metadata only',
      disposition: 'candidate',
      readiness: 'review_now',
      readinessReason: 'review safely',
      checkedAt: '2026-07-30T18:00:00Z',
    }

    component.queueDiscoveryReview({ collection: 'MCP Servers', disposition: 'review_candidate', repository: 'owner/new-mcp', sourceUrl: 'https://api.ossinsight.io/example', rationale: 'Review first.', reviewTrack: 'controlled execution', priority: 72, risk: 'high', reviewReason: 'Review locally.' })

    expect(pursuitService.create).toHaveBeenCalledWith(jasmine.objectContaining({
      title: 'Screen owner/new-mcp for a HAI adapter',
      status: 'waiting',
      autonomyLevel: 'manual',
      riskLevel: 'high',
      sourceOfCreation: 'ossinsight_discovery:owner/new-mcp',
    }))
    expect(notification.success).toHaveBeenCalledWith('Discovery review created', 'owner/new-mcp remains unconfigured. HAI created a manual review record.')
    expect(router.navigate).toHaveBeenCalledWith(['/pursuits'], { queryParams: { selected: 'pursuit-discovery-1' } })
  })

  it('verifies source-discovered repository metadata before a manual review', () => {
    const { component, notification } = createComponent()
    const catalogService = (component as any).service
    catalogService.revalidateOSSInsightDiscovery.and.returnValue(of({ id: 'ossinsight-owner-new-mcp', name: 'owner/new-mcp', upstreamUrl: 'https://github.com/owner/new-mcp', available: true, archived: false, license: 'MIT', message: 'metadata only', disposition: 'candidate', readiness: 'review_now', readinessReason: 'review safely' }))

    component.verifyDiscovery({ collection: 'MCP Servers', disposition: 'review_candidate', repository: 'owner/new-mcp', sourceUrl: 'https://api.ossinsight.io/example', rationale: 'Review first.', reviewTrack: 'controlled execution', priority: 72, risk: 'high', reviewReason: 'Review locally.' })

    expect(catalogService.revalidateOSSInsightDiscovery).toHaveBeenCalledWith('owner/new-mcp', 'candidate')
    expect(component.discoveryReviews['owner/new-mcp'].license).toBe('MIT')
    expect(notification.success).toHaveBeenCalled()
  })

  it('matches a need against the reviewed catalog without activating a candidate', () => {
    const { component } = createComponent()
    const catalogService = (component as any).service
    catalogService.recommendCapabilities.and.returnValue(of({ need: 'local model evaluation', message: 'planning only', recommendations: [{ id: 'lm-eval-harness', name: 'LM Evaluation Harness', status: 'candidate', role: 'offline model evaluation', rationale: 'test', requiresApproval: true, activation: 'review first', score: 14, reasons: ['matches capability'], nextStep: 'Create a manual adapter review.' }] }))

    component.recommendCapabilities('local model evaluation')

    expect(catalogService.recommendCapabilities).toHaveBeenCalledWith('local model evaluation')
    expect(component.capabilityRecommendation?.recommendations[0].id).toBe('lm-eval-harness')
  })

  it('reads RAGFlow bridge state only when the RAGFlow candidate is selected', () => {
    const { component, ragflowService } = createComponent()
    ragflowService.status.and.returnValue(of({ enabled: false, configured: false, provider: 'RAGFlow', datasetCount: 0, capabilities: [], restrictions: ['no ingestion'], scope: 'candidate evidence only' }))

    component.select({ ...candidate, id: 'ragflow', name: 'RAGFlow' } as any)

    expect(ragflowService.status).toHaveBeenCalled()
    expect(component.ragflowStatus?.configured).toBeFalse()
  })

  it('reads AnythingLLM bridge state only when the AnythingLLM profile is selected', () => {
    const { component, anythingLLMService } = createComponent()
    anythingLLMService.status.and.returnValue(of({ enabled: false, configured: false, provider: 'AnythingLLM', workspaceCount: 0, workspaceSlugs: [], localEmbeddingsConfirmed: false, capabilities: [], restrictions: ['no chat'], scope: 'candidate evidence only' }))

    component.select({ ...candidate, id: 'anythingllm', name: 'AnythingLLM', status: 'integrated_profile' } as any)

    expect(anythingLLMService.status).toHaveBeenCalled()
    expect(component.anythingLLMStatus?.configured).toBeFalse()
  })

  it('reads Presidio bridge state only when the Presidio candidate is selected', () => {
    const { component, presidioService } = createComponent()
    presidioService.status.and.returnValue(of({ enabled: false, configured: false, provider: 'Presidio Analyzer', language: '', entityTypes: [], capabilities: [], restrictions: ['no persistence'], scope: 'review metadata only' }))

    component.select({ ...candidate, id: 'presidio', name: 'Presidio' } as any)

    expect(presidioService.status).toHaveBeenCalled()
    expect(component.presidioStatus?.configured).toBeFalse()
  })

  it('reads Langfuse bridge state only when the Langfuse profile is selected', () => {
    const { component, langfuseService } = createComponent()
    langfuseService.status.and.returnValue(of({ enabled: false, configured: false, provider: 'Langfuse self-hosted observability', capabilities: [], restrictions: ['no prompt export'], scope: 'aggregate-only local trace evidence' }))

    component.select({ ...candidate, id: 'langfuse', name: 'Langfuse', status: 'integrated_profile' } as any)

    expect(langfuseService.status).toHaveBeenCalled()
    expect(component.langfuseStatus?.configured).toBeFalse()
  })

  it('reads Serena bridge state only when the Serena profile is selected', () => {
    const { component, serenaService } = createComponent()
    serenaService.status.and.returnValue(of({ enabled: false, configured: false, provider: 'Serena semantic code context', capabilities: [], restrictions: ['no edit'], scope: 'read-only metadata only' }))

    component.select({ ...candidate, id: 'serena', name: 'Serena', status: 'integrated_profile' } as any)

    expect(serenaService.status).toHaveBeenCalled()
    expect(component.serenaStatus?.configured).toBeFalse()
  })

  it('probes Langfuse without exporting a trace', () => {
    const { component, langfuseService, notification } = createComponent()
    langfuseService.probe.and.returnValue(of({ healthy: true, ready: true, checkedAt: '2026-07-20T00:00:00Z', scope: 'health only' }))

    component.probeLangfuse()

    expect(langfuseService.probe).toHaveBeenCalled()
    expect(component.langfuseProbe?.ready).toBeTrue()
    expect(notification.success).toHaveBeenCalledWith('Langfuse ready', 'HAI verified the configured local health and readiness endpoints. No trace was exported.')
  })

  it('exports only through the explicit Langfuse snapshot action', () => {
    const { component, langfuseService, notification } = createComponent()
    langfuseService.exportOperationalSnapshot.and.returnValue(of({ traceId: 'a'.repeat(32), spanId: 'b'.repeat(16), exportedAt: '2026-07-20T00:00:00Z', scope: 'aggregate-only' }))

    component.exportLangfuseSnapshot()

    expect(langfuseService.exportOperationalSnapshot).toHaveBeenCalled()
    expect(component.langfuseExport?.traceId).toBe('a'.repeat(32))
    expect(notification.success).toHaveBeenCalledWith('Aggregate trace exported', "Langfuse accepted HAI's fixed aggregate operational snapshot. No prompts, sources, or workflow records were exported.")
  })

  it('shows discovery results without changing runtime state', () => {
    const { component, notification } = createComponent()
    const catalogService = (component as any).service
    catalogService.discoverOSSInsightRepositories.and.returnValue(of({
      available: true,
      cached: false,
      collectionsScreened: 138,
      candidateCollections: 12,
      collectionsChecked: 12,
      repositoriesChecked: 50,
      duplicateSourceHits: 0,
      maximumDiscoveries: 800,
      knownProfileHits: 8,
      discoveries: [{ collection: 'MCP Servers', disposition: 'review_candidate', repository: 'owner/new-mcp', sourceUrl: 'https://api.ossinsight.io/example', rationale: 'Review first.', reviewTrack: 'controlled execution', priority: 72, risk: 'high', reviewReason: 'Review locally.', relatedCollections: ['MCP Servers'], relatedSourceUrls: ['https://api.ossinsight.io/example'] }],
      discoveriesTruncated: false,
      message: 'did not add catalog entries',
    }))

    component.discoverOSSInsightRepositories()

    expect(catalogService.discoverOSSInsightRepositories).toHaveBeenCalled()
    expect(component.ossInsightDiscovery?.discoveries?.[0].repository).toBe('owner/new-mcp')
    expect(notification.success).toHaveBeenCalledWith('Candidate discovery complete', '1 unreviewed repositories were found. No catalog entry, credential, or runtime state changed.')
  })

  it('can scan represented capability categories without activating an upstream', () => {
    const { component, notification } = createComponent()
    const catalogService = (component as any).service
    catalogService.discoverReviewableOSSInsightRepositories.and.returnValue(of({
      available: true,
      cached: false,
      scope: 'reviewable',
      collectionsScreened: 138,
      candidateCollections: 12,
      reviewableCollections: 25,
      eligibleCollections: 25,
      collectionsChecked: 25,
      repositoriesChecked: 90,
      duplicateSourceHits: 0,
      maximumDiscoveries: 800,
      knownProfileHits: 8,
      discoveries: [{ collection: 'LLM Inference Engines', disposition: 'represented_in_catalog', repository: 'owner/new-inference', sourceUrl: 'https://api.ossinsight.io/example', rationale: 'Review first.', reviewTrack: 'local inference', priority: 80, risk: 'medium', reviewReason: 'Review loopback limits.', relatedCollections: ['LLM Inference Engines'], relatedSourceUrls: ['https://api.ossinsight.io/example'] }],
      discoveriesTruncated: false,
      message: 'did not add catalog entries',
    }))

    component.discoverReviewableOSSInsightRepositories()

    expect(catalogService.discoverReviewableOSSInsightRepositories).toHaveBeenCalled()
    expect(component.ossInsightDiscovery?.scope).toBe('reviewable')
    expect(notification.success).toHaveBeenCalledWith('Relevant discovery complete', '1 unreviewed repositories were found across candidate and represented categories. No catalog entry, credential, or runtime state changed.')
  })
})
