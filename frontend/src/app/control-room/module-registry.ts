export type HaiModuleGroup = 'work' | 'intelligence' | 'system'

export interface HaiModuleDefinition {
  id: string
  route: string
  group: HaiModuleGroup
  title: string
  description: string
  icon: string
  primaryAction?: {
    label: string
    capability: string
  }
  advancedSectionIds?: readonly string[]
}

export const HAI_MODULES: HaiModuleDefinition[] = [
  { id: 'control-center', route: '/control-center', group: 'work', title: 'Command Center', description: 'Your next useful action.', icon: 'appstore' },
  { id: 'automations', route: '/home', group: 'work', title: 'Automations', description: 'Manage approved automation work.', icon: 'setting' },
  { id: 'background-operations', route: '/background-operations', group: 'work', title: 'Background operations', description: 'Safe work running while you are away.', icon: 'thunderbolt' },
  { id: 'pursuits', route: '/pursuits', group: 'work', title: 'Pursuits', description: 'Long-running goals and their next moves.', icon: 'flag' },
  {
    id: 'plan-coordination',
    route: '/plans',
    group: 'work',
    title: 'Plan and coordination',
    description: 'Immutable dependencies, owners, resources, and plan revisions.',
    icon: 'branches',
    primaryAction: { label: 'Create plan preview', capability: 'plans:write' },
    advancedSectionIds: ['dependency-plan', 'schedule-resources', 'governance-bindings', 'revision-history', 'create-preview'],
  },
  { id: 'workflow-engine', route: '/workflow-engine', group: 'work', title: 'Workflows', description: 'Review and move controlled workflows forward.', icon: 'unordered-list' },
  { id: 'quick-capture', route: '/quick-capture', group: 'work', title: 'Quick capture', description: 'Turn a thought into controlled work.', icon: 'plus-square' },
  { id: 'exceptions', route: '/exceptions', group: 'work', title: 'Exceptions', description: 'Resolve work that needs intervention.', icon: 'warning' },
  { id: 'task-blueprint', route: '/task-blueprint', group: 'intelligence', title: 'Task planning', description: 'Talk to HAI and inspect its plan.', icon: 'partition' },
  {
    id: 'agent-teams',
    route: '/agent-teams',
    group: 'intelligence',
    title: 'Agent teams',
    description: 'Govern advisory teams, decisions, and consensus.',
    icon: 'team',
    primaryAction: { label: 'Create advisory team', capability: 'agent-teams:govern' },
    advancedSectionIds: ['members', 'decisions', 'consensus', 'history'],
  },
  { id: 'connected-sources', route: '/connected-sources', group: 'intelligence', title: 'Sources', description: 'Connected accounts, files, and sync health.', icon: 'cluster' },
  { id: 'account-bridges', route: '/account-bridges', group: 'intelligence', title: 'Account bridges', description: 'Connection and permission health.', icon: 'link' },
  { id: 'memory', route: '/memory', group: 'intelligence', title: 'Memory', description: 'Review useful, source-linked context.', icon: 'database' },
  {
    id: 'operational-brain',
    route: '/operational-brain',
    group: 'intelligence',
    title: 'Operational Brain',
    description: 'See how work, knowledge, sources, memory, and agents connect.',
    icon: 'node-index',
    primaryAction: { label: 'Inspect attention map', capability: 'operational-graph:read' },
    advancedSectionIds: ['layers', 'agent-context', 'quality'],
  },
  {
    id: 'knowledge-claims',
    route: '/knowledge-claims',
    group: 'intelligence',
    title: 'Truth review',
    description: 'Resolve conflicting, unsupported, or outdated claims.',
    icon: 'audit',
    primaryAction: { label: 'Review claim exceptions', capability: 'knowledge-claims:review' },
    advancedSectionIds: ['workspace-boundary', 'claim-register'],
  },
  { id: 'grounded-answers', route: '/grounded-answers', group: 'intelligence', title: 'Verified answers', description: 'Evidence, claims, and verification.', icon: 'safety-certificate' },
  { id: 'ambient-brain', route: '/ambient-brain', group: 'intelligence', title: 'Brain settings', description: 'Priorities, safeguards, and proactive work.', icon: 'compass' },
  {
    id: 'life-ops',
    route: '/life-ops',
    group: 'intelligence',
    title: 'Life Ops',
    description: 'Needs, capacity, goals, and whole-life priority.',
    icon: 'heart',
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
  },
  { id: 'brain-catalog', route: '/brain-catalog', group: 'intelligence', title: 'Brain catalog', description: 'Reviewed external capabilities and activation gates.', icon: 'book' },
  { id: 'hai-os', route: '/hai-os', group: 'intelligence', title: 'HAI OS', description: 'Operating-system architecture and readiness.', icon: 'deployment-unit' },
  { id: 'llm-policy', route: '/llm-policy', group: 'system', title: 'Models', description: 'Local-first routing, providers, and budget.', icon: 'deployment-unit' },
  {
    id: 'model-intelligence',
    route: '/model-intelligence',
    group: 'system',
    title: 'Model intelligence',
    description: 'Validated outcomes, provider health, and model efficiency.',
    icon: 'experiment',
    primaryAction: { label: 'Review model outcomes', capability: 'model-intelligence:review' },
    advancedSectionIds: ['providers', 'model-calibration', 'model-profiles', 'runtime-budget'],
  },
  {
    id: 'framework-registry',
    route: '/framework-registry',
    group: 'system',
    title: 'Framework registry',
    description: 'Governed framework selection, owner preferences, and Constitution.',
    icon: 'apartment',
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
  },
  {
    id: 'governance-control',
    route: '/governance-control',
    group: 'system',
    title: 'Governance control',
    description: 'Authority, controlled learning, agents, and domain packs.',
    icon: 'safety-certificate',
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
  },
  { id: 'runtime-control', route: '/runtime-control', group: 'system', title: 'Runtime control', description: 'Controlled execution and safety gates.', icon: 'poweroff' },
  { id: 'runtime-lab', route: '/runtime-lab', group: 'system', title: 'Runtime lab', description: 'Runtime adapters and capability checks.', icon: 'api' },
  { id: 'system-status', route: '/system-status', group: 'system', title: 'System status', description: 'Services, dependencies, and health.', icon: 'heart' },
  { id: 'command-dashboard', route: '/command-dashboard', group: 'system', title: 'Legacy dashboard', description: 'Detailed operating records.', icon: 'dashboard' },
]

export const HAI_MODULE_GROUPS: Array<{ id: HaiModuleGroup; label: string }> = [
  { id: 'work', label: 'Work' },
  { id: 'intelligence', label: 'Intelligence' },
  { id: 'system', label: 'System' },
]

export function moduleForUrl(url: string): HaiModuleDefinition {
  const path = url.split('?')[0].split('#')[0]
  return HAI_MODULES.find((module) => path === module.route || path.startsWith(`${module.route}/`)) || HAI_MODULES[0]
}

export function moduleDocumentTitle(module: HaiModuleDefinition): string {
  return `${module.title} | HAI Automation Hub`
}
