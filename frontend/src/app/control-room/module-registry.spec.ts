import { HAI_MODULES, HAI_MODULE_GROUPS, moduleDocumentTitle, moduleForUrl } from './module-registry';

describe('HAI module registry', () => {
  it('maps every registered route to a unique module', () => {
    const routes = HAI_MODULES.map((module) => module.route);

    expect(new Set(routes).size).toBe(routes.length);
    expect(HAI_MODULES.every((module) => moduleForUrl(module.route)?.id === module.id)).toBeTrue();
  });

  it('keeps navigation groups limited to operational intent', () => {
    expect(HAI_MODULE_GROUPS.map((group) => group.id)).toEqual(['work', 'intelligence', 'system']);
    expect(HAI_MODULES.every((module) => HAI_MODULE_GROUPS.some((group) => group.id === module.group))).toBeTrue();
  });

  it('builds a product-qualified browser title for every module', () => {
    expect(HAI_MODULES.every((module) => moduleDocumentTitle(module) === `${module.title} | HAI Automation Hub`))
      .toBeTrue();
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
          'selection-history',
          'constitution-history',
          'constitution-governance',
        ],
      })
    );
    expect(moduleForUrl('/framework-registry?mode=advanced#constitution').id)
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
        'execution-receipts',
        'standing-mandates',
        'controlled-learning',
        'agent-registry',
        'domain-pack-catalog',
      ],
    }));
    expect(moduleForUrl('/governance-control?mode=advanced#agent-registry').id)
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
      advancedSectionIds: ['workspace-boundary', 'claim-register'],
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
        'schedule-resources',
        'governance-bindings',
        'revision-history',
        'create-preview',
      ],
    }));
    expect(moduleForUrl('/plans?mode=advanced#dependency-plan').id)
      .toBe('plan-coordination');
  });
});
