import {
  HAI_MODULES,
  HAI_MODULE_GROUPS,
  isAdvancedSectionRegistered,
  isProgressiveSectionRegistered,
  moduleForUrl,
  progressiveSectionParentFor,
  resolveProgressiveSection,
} from './module-registry';
import { AUTHENTICATED_PAGE_PATHS } from '../app-routing.module';

// Audited shared disclosure IDs plus stable Control Center IDs reserved for the owner-led migration.
const STATIC_PROGRESSIVE_SECTION_IDS_BY_ROUTE: Readonly<Record<string, readonly string[]>> = {
  '/onboarding': [],
  '/control-center': ['action-catalog', 'more-controls', 'operational-controls', 'context-audit', 'diagnostics'],
  '/home': ['automation-management'],
  '/background-operations': ['operation-ledger'],
  '/pursuits': ['pursuit-health', 'life-domain-index', 'portfolio-planning', 'route-input', 'new-pursuit-contract', 'pursuit-summary-metrics', 'resource-ledger', 'generated-actions', 'operational-timeline', 'pursuit-intake', 'linked-record-counts', 'manage-pursuit-links', 'record-audit-trails', 'workflow-approval-trail', 'workflow-quality-gates', 'decision-audit-history', 'workflow-transition-source-audit', 'task-execution-trail', 'automation-runtime-trail', 'verification-runs-claims', 'evidence-memory-source-context', 'proactive-opportunities', 'pursuit-activity-log'],
  '/plans': ['dependency-plan', 'governance-bindings', 'revision-history', 'schedule-resources', 'create-preview'],
  '/workflow-engine': ['queue-filters', 'intake-provenance', 'inbox-search', 'last-operation-details', 'framework-provenance', 'workflow-controls', 'checklist', 'evidence-proposals', 'timeline-audit', 'technical-status', 'workflow-header-controls', 'reminder-evidence', 'intake-outcome-criteria'],
  '/quick-capture': ['capture-details'],
  '/exceptions': ['filters', 'diagnostics'],
  '/task-blueprint': ['task-context', 'task-inspector'],
  '/agent-teams': ['members', 'decisions', 'consensus', 'history', 'charter-record'],
  '/connected-sources': ['source-records', 'source-activity', 'source-timeline-cooccurrence', 'source-permissions-filters', 'source-danger-zone'],
  '/account-bridges': ['registered-feeds', 'bridge-contracts'],
  '/memory': ['memory-overview', 'memory-maintenance-actions', 'memory-records', 'memory-store-metadata', 'memory-retrieval-options', 'memory-import-metadata', 'memory-record-detail', 'memory-semantic-index', 'memory-record-content', 'memory-record-danger-zone'],
  '/knowledge-claims': ['workspace-boundary', 'claim-register', 'claim-evidence'],
  '/grounded-answers': ['verification-controls', 'source-discovery', 'verification-details', 'verification-history'],
  '/ambient-brain': ['engine-controls', 'opportunity-history'],
  '/life-ops': ['need-observations', 'capacity-record', 'entity-domains', 'goal-hierarchy', 'priority-assessment', 'provenance'],
  '/brain-catalog': ['catalog-inventory', 'capability-planes', 'implementation-roadmap', 'profile-inventory', 'candidates', 'capability-match', 'held', 'oss-insight-screening', 'agent-skills', 'discovery-verification-record'],
  '/skills': ['inventory-provenance'],
  '/hai-os': ['pursuit-queues', 'pursuit-context', 'product-stack', 'system-metrics', 'operating-planes', 'real-world-readiness', 'governance', 'pursuit-spotlight'],
  '/llm-policy': ['route-decision-evidence', 'model-catalog', 'provider-inventory', 'routing-audit'],
  '/model-intelligence': ['providers', 'model-calibration', 'model-profiles', 'runtime-budget'],
  '/framework-registry': ['selection-context', 'family-taxonomy', 'framework-catalog', 'constitution-history', 'selection-history', 'constitution-governance', 'framework-preference-history'],
  '/governance-control': ['advisory-engines', 'outcome-monitor-advanced', 'outcome-monitor-composition', 'execution-receipts', 'life-ledger-records', 'standing-mandates', 'controlled-learning', 'agent-registry', 'domain-pack-catalog', 'agent-teams', 'whole-life-context', 'life-ledger-overview', 'proactivity-policy', 'outcome-evaluations', 'resilience-status'],
  '/runtime-control': ['runtime-autonomy-policy', 'runtime-readiness-evidence'],
  '/runtime-lab': ['runtime-inventory', 'runtime-feature-parity', 'runtime-mcp-readiness'],
  '/system-status': ['system-check-details'],
  '/command-dashboard': ['runtime-inventory', 'memory-inspection', 'command-history'],
};

describe('HAI module registry', () => {
  it('preserves module identity with matrix parameters and encoded segment boundaries', () => {
    expect(moduleForUrl('/memory;view=records/entry;id=1?mode=advanced#memory-records').id).toBe('memory');
    expect(moduleForUrl('/knowledge-claims;workspace=hai').id).toBe('knowledge-claims');
    expect(moduleForUrl('/memoryx;view=records').id).toBe('control-center');
    expect(moduleForUrl('/memory%2Fentry').id).toBe('control-center');
    expect(moduleForUrl('/memory%ZZ').id).toBe('control-center');
  });
  it('maps every registered route to a unique module', () => {
    const routes = HAI_MODULES.map((module) => module.route);

    expect(new Set(routes).size).toBe(routes.length);
    expect(HAI_MODULES.every((module) => moduleForUrl(module.route)?.id === module.id)).toBeTrue();
  });

  it('keeps navigation groups limited to operational intent', () => {
    expect(HAI_MODULE_GROUPS.map((group) => group.id)).toEqual(['work', 'intelligence', 'system']);
    expect(HAI_MODULES.every((module) => HAI_MODULE_GROUPS.some((group) => group.id === module.group))).toBeTrue();
  });

  it('registers the guarded Framework Registry route with its shell contract', () => {
    const frameworkRegistry = HAI_MODULES.find(
      (module) => module.route === '/framework-registry'
    );

    expect(frameworkRegistry).toEqual(
      jasmine.objectContaining({
        id: 'framework-registry',
        group: 'system',
        title: 'Framework registry',
        icon: 'apartment',
        description: 'Governed framework selection, owner preferences, and Constitution.',
        primaryAction: {
          label: 'Select frameworks',
          capability: 'framework-registry:select',
        },
        advancedSectionIds: [
          'selection-context',
          'family-taxonomy',
          'framework-catalog',
          'constitution-history',
          'selection-history',
          'constitution-governance',
        ],
      })
    );
    expect(moduleForUrl('/framework-registry?mode=advanced#constitution-history').id)
      .toBe('framework-registry');
  });

  it('registers Life Ops with owner-context progressive sections', () => {
    const lifeOps = HAI_MODULES.find((module) => module.route === '/life-ops');

    expect(lifeOps).toEqual(jasmine.objectContaining({
      id: 'life-ops',
      group: 'intelligence',
      title: 'Life Ops',
      primaryAction: {
        label: 'Record current context',
        capability: 'life-ops:write',
      },
      advancedSectionIds: [
        'need-observations',
        'capacity-record',
        'entity-domains',
        'goal-hierarchy',
        'priority-assessment',
        'provenance',
      ],
    }));
    expect(moduleForUrl('/life-ops?mode=advanced#priority-assessment').id)
      .toBe('life-ops');
  });

  it('keeps authenticated onboarding inside the shared shell but out of work navigation', () => {
    const onboarding = HAI_MODULES.find((module) => module.route === '/onboarding');

    expect(AUTHENTICATED_PAGE_PATHS).toContain('/onboarding');
    expect(onboarding?.showInNavigation).toBeFalse();
    expect(moduleForUrl('/onboarding').id).toBe('onboarding');
  });

  it('lists every guarded operational route in the control-room registry', () => {
    const registeredPaths = new Set(HAI_MODULES.map((module) => module.route));

    expect(AUTHENTICATED_PAGE_PATHS.length).toBe(registeredPaths.size);
    expect(AUTHENTICATED_PAGE_PATHS.every((path) => registeredPaths.has(path))).toBeTrue();
  });

  it('declares the owner of a primary action for every module', () => {
    for (const module of HAI_MODULES) {
      expect(Boolean(module.primaryAction) || module.primaryActionOwner === 'module')
        .withContext(`${module.route} must register a capability-backed action or delegate resolution to its page`)
        .toBeTrue();
    }
  });

  it('registers Governance Control with its progressive governance surfaces', () => {
    const governance = HAI_MODULES.find(
      (module) => module.route === '/governance-control'
    );

    expect(governance).toEqual(jasmine.objectContaining({
      id: 'governance-control',
      group: 'system',
      title: 'Governance control',
      primaryAction: {
        label: 'Review governance',
        capability: 'governance:review',
      },
      advancedSectionIds: [
        'advisory-engines',
        'outcome-monitor-advanced',
        'outcome-monitor-composition',
        'execution-receipts',
        'life-ledger-records',
        'standing-mandates',
        'controlled-learning',
        'agent-registry',
        'domain-pack-catalog',
      ],
      basicSectionIds: [
        'agent-teams',
        'whole-life-context',
        'life-ledger-overview',
        'proactivity-policy',
        'outcome-evaluations',
        'resilience-status',
      ],
      progressiveSectionParents: {
        'agent-teams': 'advisory-engines',
        'whole-life-context': 'advisory-engines',
        'life-ledger-overview': 'advisory-engines',
        'proactivity-policy': 'advisory-engines',
        'outcome-evaluations': 'advisory-engines',
        'outcome-monitor-advanced': 'outcome-evaluations',
        'outcome-monitor-composition': 'outcome-evaluations',
        'resilience-status': 'advisory-engines',
      },
    }));
    expect(moduleForUrl('/governance-control?mode=advanced#outcome-monitor-composition').id)
      .toBe('governance-control');
  });

  it('registers Truth review as a progressive intelligence module', () => {
    const truthReview = HAI_MODULES.find((module) => module.route === '/knowledge-claims');

    expect(truthReview).toEqual(jasmine.objectContaining({
      id: 'knowledge-claims',
      group: 'intelligence',
      title: 'Truth review',
      primaryAction: {
        label: 'Review claim exceptions',
        capability: 'knowledge-claims:review',
      },
      advancedSectionIds: ['workspace-boundary', 'claim-register', 'claim-evidence'],
    }));
  });

  it('registers Model intelligence with outcome-first progressive sections', () => {
    const modelIntelligence = HAI_MODULES.find((module) => module.route === '/model-intelligence');

    expect(modelIntelligence).toEqual(jasmine.objectContaining({
      id: 'model-intelligence',
      group: 'system',
      title: 'Model intelligence',
      description: 'Validated outcomes, provider health, and model efficiency.',
      primaryAction: {
        label: 'Review model outcomes',
        capability: 'model-intelligence:review',
      },
      advancedSectionIds: ['providers', 'model-calibration', 'model-profiles', 'runtime-budget'],
    }));
  });

  it('registers Plan and coordination as a progressive work module', () => {
    const plans = HAI_MODULES.find((module) => module.route === '/plans');

    expect(plans).toEqual(jasmine.objectContaining({
      id: 'plan-coordination',
      group: 'work',
      title: 'Plan and coordination',
      primaryAction: {
        label: 'Create plan preview',
        capability: 'plans:write',
      },
      advancedSectionIds: [
        'dependency-plan',
        'governance-bindings',
        'revision-history',
        'schedule-resources',
      ],
      basicSectionIds: ['create-preview'],
    }));
    expect(moduleForUrl('/plans?mode=advanced#dependency-plan').id)
      .toBe('plan-coordination');
  });

  it('registers every HAI OS Advanced section for deep links from the shared shell', () => {
    const haiOs = HAI_MODULES.find((module) => module.route === '/hai-os');

    expect(haiOs?.advancedSectionIds).toEqual([
      'pursuit-queues',
      'pursuit-context',
      'product-stack',
      'system-metrics',
      'operating-planes',
      'real-world-readiness',
      'governance',
    ]);
    expect(haiOs?.basicSectionIds).toEqual(['pursuit-spotlight']);
    expect(moduleForUrl('/hai-os?mode=advanced#system-metrics').id).toBe('hai-os');
  });

  it('registers the Background Operations ledger as an Advanced deep-link target', () => {
    const backgroundOperations = HAI_MODULES.find((module) => module.route === '/background-operations');

    expect(backgroundOperations?.advancedSectionIds).toEqual(['operation-ledger']);
    expect(moduleForUrl('/background-operations?mode=advanced#operation-ledger').id)
      .toBe('background-operations');
  });

  it('registers the audited static progressive section inventory for every route', () => {
    expect(Object.keys(STATIC_PROGRESSIVE_SECTION_IDS_BY_ROUTE).sort())
      .toEqual(HAI_MODULES.map((module) => module.route).sort());

    for (const [route, sectionIds] of Object.entries(STATIC_PROGRESSIVE_SECTION_IDS_BY_ROUTE)) {
      const module = HAI_MODULES.find((candidate) => candidate.route === route);
      expect([...(module?.advancedSectionIds ?? []), ...(module?.basicSectionIds ?? [])])
        .withContext(route)
        .toEqual(sectionIds);
      for (const sectionId of sectionIds) {
        expect(isProgressiveSectionRegistered(module!, sectionId))
          .withContext(`${route}#${sectionId}`)
          .toBeTrue();
      }
      for (const sectionId of Object.keys(module?.progressiveSectionParents ?? {})) {
        expect(sectionIds).withContext(`${route}#${sectionId} parent-map inventory`).toContain(sectionId);
        expect(resolveProgressiveSection(module!, sectionId))
          .withContext(`${route}#${sectionId} parent-map target`)
          .toBeDefined();
      }
    }
  });

  it('recognizes only the two declared dynamic section prefixes and safe identifiers', () => {
    const llmPolicy = HAI_MODULES.find((module) => module.route === '/llm-policy')!;
    const runtimeLab = HAI_MODULES.find((module) => module.route === '/runtime-lab')!;

    expect(llmPolicy.advancedSectionIdPrefixes).toEqual([
      { prefix: 'provider-detail-', parentId: 'provider-inventory' },
    ]);
    expect(runtimeLab.advancedSectionIdPrefixes).toEqual([
      { prefix: 'runtime-inventory-', parentId: 'runtime-feature-parity' },
    ]);
    expect(isAdvancedSectionRegistered(llmPolicy, 'provider-detail-ollama')).toBeTrue();
    expect(isAdvancedSectionRegistered(runtimeLab, 'runtime-inventory-openclaw')).toBeTrue();
    expect(isAdvancedSectionRegistered(llmPolicy, 'provider-detail-0123456789abcdef01234567')).toBeTrue();
    expect(isAdvancedSectionRegistered(runtimeLab, 'runtime-inventory-0123456789abcdef01234567')).toBeTrue();
    expect(isAdvancedSectionRegistered(runtimeLab, `runtime-inventory-${'a'.repeat(64)}`)).toBeTrue();
    expect(isAdvancedSectionRegistered(llmPolicy, 'runtime-inventory-openclaw')).toBeFalse();
    expect(isAdvancedSectionRegistered(runtimeLab, 'provider-detail-ollama')).toBeFalse();
    expect(isAdvancedSectionRegistered(llmPolicy, 'provider-detail-')).toBeFalse();
    expect(isAdvancedSectionRegistered(runtimeLab, 'runtime-inventory-../openclaw')).toBeFalse();
    expect(isAdvancedSectionRegistered(runtimeLab, 'runtime-inventory-alpha\\beta')).toBeFalse();
    expect(isAdvancedSectionRegistered(runtimeLab, 'runtime-inventory-%2e%2e')).toBeFalse();
    expect(isAdvancedSectionRegistered(runtimeLab, 'runtime-inventory-alpha..beta')).toBeFalse();
    expect(isAdvancedSectionRegistered(runtimeLab, 'runtime-inventory-alpha?beta')).toBeFalse();
    expect(isAdvancedSectionRegistered(runtimeLab, 'runtime-inventory-alpha#beta')).toBeFalse();
    expect(isAdvancedSectionRegistered(runtimeLab, `runtime-inventory-${'a'.repeat(65)}`)).toBeFalse();
    expect(isAdvancedSectionRegistered(llmPolicy, 'unregistered-provider-hash')).toBeFalse();
  });

  it('resolves direct parents for nested static and dynamic progressive sections', () => {
    const governance = HAI_MODULES.find((module) => module.route === '/governance-control')!;
    const llmPolicy = HAI_MODULES.find((module) => module.route === '/llm-policy')!;
    const runtimeLab = HAI_MODULES.find((module) => module.route === '/runtime-lab')!;

    expect(progressiveSectionParentFor(governance, 'outcome-monitor-advanced'))
      .toBe('outcome-evaluations');
    expect(progressiveSectionParentFor(governance, 'outcome-evaluations'))
      .toBe('advisory-engines');
    expect(progressiveSectionParentFor(governance, 'resilience-status'))
      .toBe('advisory-engines');
    expect(progressiveSectionParentFor(llmPolicy, 'provider-detail-ollama'))
      .toBe('provider-inventory');
    expect(progressiveSectionParentFor(runtimeLab, 'runtime-inventory-openclaw'))
      .toBe('runtime-feature-parity');
    expect(progressiveSectionParentFor(runtimeLab, 'runtime-inventory-../openclaw'))
      .toBeUndefined();
  });

  it('resolves exact fragments to their outer-to-inner ancestor chain', () => {
    const governance = HAI_MODULES.find((module) => module.route === '/governance-control')!;
    const llmPolicy = HAI_MODULES.find((module) => module.route === '/llm-policy')!;
    const runtimeLab = HAI_MODULES.find((module) => module.route === '/runtime-lab')!;

    expect(resolveProgressiveSection(governance, 'outcome-monitor-advanced')).toEqual({
      sectionId: 'outcome-monitor-advanced',
      ancestorIds: ['advisory-engines', 'outcome-evaluations'],
      requiredMode: 'advanced',
    });
    expect(resolveProgressiveSection(governance, 'advisory-engines')).toEqual({
      sectionId: 'advisory-engines',
      ancestorIds: [],
      requiredMode: 'advanced',
    });
    expect(resolveProgressiveSection(governance, 'whole-life-context')).toEqual({
      sectionId: 'whole-life-context',
      ancestorIds: ['advisory-engines'],
      requiredMode: 'advanced',
    });
    expect(resolveProgressiveSection(governance, 'outcome-evaluations')).toEqual({
      sectionId: 'outcome-evaluations',
      ancestorIds: ['advisory-engines'],
      requiredMode: 'advanced',
    });
    expect(resolveProgressiveSection(llmPolicy, 'provider-detail-ollama')).toEqual({
      sectionId: 'provider-detail-ollama',
      ancestorIds: ['provider-inventory'],
      requiredMode: 'advanced',
    });
    expect(resolveProgressiveSection(runtimeLab, 'runtime-inventory-openclaw')).toEqual({
      sectionId: 'runtime-inventory-openclaw',
      ancestorIds: ['runtime-feature-parity'],
      requiredMode: 'advanced',
    });
    expect(resolveProgressiveSection(runtimeLab, 'runtime-inventory-0123456789abcdef01234567')).toEqual({
      sectionId: 'runtime-inventory-0123456789abcdef01234567',
      ancestorIds: ['runtime-feature-parity'],
      requiredMode: 'advanced',
    });
    expect(resolveProgressiveSection(runtimeLab, '#runtime-inventory-openclaw')).toBeUndefined();
    expect(resolveProgressiveSection(runtimeLab, 'runtime-inventory-../openclaw')).toBeUndefined();
    expect(resolveProgressiveSection(runtimeLab, 'unknown-section')).toBeUndefined();
  });

  it('registers nested native detail panels as direct Advanced targets', () => {
    const pursuits = HAI_MODULES.find((module) => module.route === '/pursuits')!;
    const memory = HAI_MODULES.find((module) => module.route === '/memory')!;
    const connectedSources = HAI_MODULES.find((module) => module.route === '/connected-sources')!;
    const workflowEngine = HAI_MODULES.find((module) => module.route === '/workflow-engine')!;

    expect(resolveProgressiveSection(pursuits, 'workflow-approval-trail')).toEqual({
      sectionId: 'workflow-approval-trail',
      ancestorIds: ['record-audit-trails'],
      requiredMode: 'advanced',
    });
    expect(resolveProgressiveSection(memory, 'memory-record-content')).toEqual({
      sectionId: 'memory-record-content',
      ancestorIds: ['memory-record-detail'],
      requiredMode: 'advanced',
    });
    expect(resolveProgressiveSection(connectedSources, 'source-danger-zone')).toEqual({
      sectionId: 'source-danger-zone',
      ancestorIds: [],
      requiredMode: 'advanced',
    });
    expect(resolveProgressiveSection(workflowEngine, 'workflow-header-controls')).toEqual({
      sectionId: 'workflow-header-controls',
      ancestorIds: [],
      requiredMode: 'advanced',
    });
  });

  it('keeps the Basic-only plan preview out of Advanced targets', () => {
    const plans = HAI_MODULES.find((module) => module.route === '/plans')!;

    expect(plans.advancedSectionIds).not.toContain('create-preview');
    expect(plans.basicSectionIds).toContain('create-preview');
    expect(isAdvancedSectionRegistered(plans, 'create-preview')).toBeFalse();
    expect(resolveProgressiveSection(plans, 'create-preview')).toEqual({
      sectionId: 'create-preview',
      ancestorIds: [],
      requiredMode: 'basic',
    });
  });

  it('resolves parent-map keys as nested targets even when not separately registered', () => {
    const governance = HAI_MODULES.find((module) => module.route === '/governance-control')!;
    const parentMapOnly = {
      ...governance,
      basicSectionIds: governance.basicSectionIds?.filter((sectionId) => sectionId !== 'whole-life-context'),
    };

    expect(isProgressiveSectionRegistered(parentMapOnly, 'whole-life-context')).toBeTrue();
    expect(resolveProgressiveSection(parentMapOnly, 'whole-life-context')).toEqual({
      sectionId: 'whole-life-context',
      ancestorIds: ['advisory-engines'],
      requiredMode: 'advanced',
    });
  });

  it('keeps a Basic target in Basic mode when it has no Advanced ancestor', () => {
    const haiOs = HAI_MODULES.find((module) => module.route === '/hai-os')!;

    expect(resolveProgressiveSection(haiOs, 'pursuit-spotlight')).toEqual({
      sectionId: 'pursuit-spotlight',
      ancestorIds: [],
      requiredMode: 'basic',
    });
  });
});
