import { DefaultUrlSerializer, PRIMARY_OUTLET } from '@angular/router'

export type HaiModuleGroup = 'work' | 'intelligence' | 'system'

export interface HaiAdvancedSectionIdPrefix {
  prefix: string
  parentId?: string
}

export interface HaiProgressiveSectionResolution {
  sectionId: string
  /** Enclosing section IDs from the outermost wrapper to the immediate parent. */
  ancestorIds: string[]
  /** Advanced is required when the target or any ancestor is an Advanced section. */
  requiredMode: 'basic' | 'advanced'
}

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
  /** The page resolves its primary action from current data, state, or permissions. */
  primaryActionOwner?: 'module'
  advancedSectionIds?: readonly string[]
  basicSectionIds?: readonly string[]
  advancedSectionIdPrefixes?: readonly HaiAdvancedSectionIdPrefix[]
  progressiveSectionParents?: Readonly<Record<string, string>>
  /** Access-only routes share shell services without appearing in operational navigation. */
  showInNavigation?: boolean
}

export const HAI_MODULES: HaiModuleDefinition[] = [
  { id: 'control-center', route: '/control-center', group: 'work', title: 'Command Center', description: 'Your next useful action.', icon: 'appstore', primaryActionOwner: 'module', advancedSectionIds: ['action-catalog', 'more-controls', 'operational-controls', 'context-audit', 'diagnostics'] },
  { id: 'automations', route: '/home', group: 'work', title: 'Automations', description: 'Manage approved automation work.', icon: 'setting', primaryActionOwner: 'module', advancedSectionIds: ['automation-management'] },
  { id: 'background-operations', route: '/background-operations', group: 'work', title: 'Background operations', description: 'Safe work running while you are away.', icon: 'thunderbolt', primaryActionOwner: 'module', advancedSectionIds: ['operation-ledger'] },
  {
    id: 'pursuits',
    route: '/pursuits',
    group: 'work',
    title: 'Pursuits',
    description: 'Long-running goals and their next moves.',
    icon: 'flag',
    primaryActionOwner: 'module',
    advancedSectionIds: [
      'pursuit-health', 'life-domain-index', 'portfolio-planning', 'route-input', 'new-pursuit-contract',
      'pursuit-summary-metrics', 'resource-ledger', 'generated-actions', 'operational-timeline',
      'pursuit-intake', 'linked-record-counts', 'manage-pursuit-links', 'record-audit-trails',
      'workflow-approval-trail', 'workflow-quality-gates', 'decision-audit-history',
      'workflow-transition-source-audit', 'task-execution-trail', 'automation-runtime-trail',
      'verification-runs-claims', 'evidence-memory-source-context', 'proactive-opportunities',
      'pursuit-activity-log',
    ],
    progressiveSectionParents: {
      'workflow-approval-trail': 'record-audit-trails',
      'workflow-quality-gates': 'record-audit-trails',
      'decision-audit-history': 'record-audit-trails',
      'workflow-transition-source-audit': 'record-audit-trails',
      'task-execution-trail': 'record-audit-trails',
      'automation-runtime-trail': 'record-audit-trails',
      'verification-runs-claims': 'record-audit-trails',
      'evidence-memory-source-context': 'record-audit-trails',
      'proactive-opportunities': 'record-audit-trails',
      'pursuit-activity-log': 'record-audit-trails',
    },
  },
  {
    id: 'plan-coordination',
    route: '/plans',
    group: 'work',
    title: 'Plan and coordination',
    description: 'Immutable dependencies, owners, resources, and plan revisions.',
    icon: 'branches',
    primaryAction: { label: 'Create plan preview', capability: 'plans:write' },
    advancedSectionIds: ['dependency-plan', 'governance-bindings', 'revision-history', 'schedule-resources'],
    basicSectionIds: ['create-preview'],
  },
  { id: 'workflow-engine', route: '/workflow-engine', group: 'work', title: 'Workflows', description: 'Review and move controlled workflows forward.', icon: 'unordered-list', primaryActionOwner: 'module', advancedSectionIds: ['queue-filters', 'intake-provenance', 'inbox-search', 'last-operation-details', 'framework-provenance', 'workflow-controls', 'checklist', 'evidence-proposals', 'timeline-audit', 'technical-status', 'workflow-header-controls', 'reminder-evidence', 'intake-outcome-criteria'] },
  { id: 'quick-capture', route: '/quick-capture', group: 'work', title: 'Quick capture', description: 'Turn a thought into controlled work.', icon: 'plus-square', primaryActionOwner: 'module', advancedSectionIds: ['capture-details'] },
  { id: 'exceptions', route: '/exceptions', group: 'work', title: 'Exceptions', description: 'Resolve work that needs intervention.', icon: 'warning', primaryActionOwner: 'module', advancedSectionIds: ['filters', 'diagnostics'] },
  { id: 'task-blueprint', route: '/task-blueprint', group: 'intelligence', title: 'Task planning', description: 'Talk to HAI and inspect its plan.', icon: 'partition', primaryActionOwner: 'module', advancedSectionIds: ['task-context', 'task-inspector'] },
  {
    id: 'agent-teams',
    route: '/agent-teams',
    group: 'intelligence',
    title: 'Agent teams',
    description: 'Govern advisory teams, decisions, and consensus.',
    icon: 'team',
    primaryAction: { label: 'Create advisory team', capability: 'agent-teams:govern' },
    advancedSectionIds: ['members', 'decisions', 'consensus', 'history', 'charter-record'],
  },
  { id: 'connected-sources', route: '/connected-sources', group: 'intelligence', title: 'Sources', description: 'Connected accounts, files, and sync health.', icon: 'cluster', primaryActionOwner: 'module', advancedSectionIds: ['source-records', 'source-activity', 'source-timeline-cooccurrence', 'source-permissions-filters', 'source-danger-zone'] },
  { id: 'account-bridges', route: '/account-bridges', group: 'intelligence', title: 'Account bridges', description: 'Connection and permission health.', icon: 'link', primaryActionOwner: 'module', advancedSectionIds: ['registered-feeds', 'bridge-contracts'] },
  { id: 'memory', route: '/memory', group: 'intelligence', title: 'Memory', description: 'Review useful, source-linked context.', icon: 'database', primaryActionOwner: 'module', advancedSectionIds: ['memory-overview', 'memory-maintenance-actions', 'memory-records', 'memory-store-metadata', 'memory-retrieval-options', 'memory-import-metadata', 'memory-record-detail', 'memory-semantic-index', 'memory-record-content', 'memory-record-danger-zone'], progressiveSectionParents: { 'memory-record-content': 'memory-record-detail', 'memory-record-danger-zone': 'memory-record-detail' } },
  {
    id: 'knowledge-claims',
    route: '/knowledge-claims',
    group: 'intelligence',
    title: 'Truth review',
    description: 'Resolve conflicting, unsupported, or outdated claims.',
    icon: 'audit',
    primaryAction: { label: 'Review claim exceptions', capability: 'knowledge-claims:review' },
    advancedSectionIds: ['workspace-boundary', 'claim-register', 'claim-evidence'],
  },
  { id: 'grounded-answers', route: '/grounded-answers', group: 'intelligence', title: 'Verified answers', description: 'Evidence, claims, and verification.', icon: 'safety-certificate', primaryActionOwner: 'module', advancedSectionIds: ['verification-controls', 'source-discovery', 'verification-details', 'verification-history'] },
  { id: 'ambient-brain', route: '/ambient-brain', group: 'intelligence', title: 'Brain settings', description: 'Priorities, safeguards, and proactive work.', icon: 'compass', primaryActionOwner: 'module', advancedSectionIds: ['engine-controls', 'opportunity-history'] },
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
  { id: 'brain-catalog', route: '/brain-catalog', group: 'intelligence', title: 'Brain catalog', description: 'Reviewed external capabilities and activation gates.', icon: 'book', primaryActionOwner: 'module', advancedSectionIds: ['catalog-inventory', 'capability-planes', 'implementation-roadmap', 'profile-inventory', 'candidates', 'capability-match', 'held', 'oss-insight-screening', 'agent-skills', 'discovery-verification-record'] },
  { id: 'skills', route: '/skills', group: 'intelligence', title: 'Skills', description: 'Source-pinned guidance available to HAI.', icon: 'read', primaryActionOwner: 'module', advancedSectionIds: ['inventory-provenance'] },
  {
    id: 'hai-os',
    route: '/hai-os',
    group: 'intelligence',
    title: 'HAI OS',
    description: 'Operating-system architecture and readiness.',
    icon: 'deployment-unit',
    primaryActionOwner: 'module',
    advancedSectionIds: [
      'pursuit-queues',
      'pursuit-context',
      'product-stack',
      'system-metrics',
      'operating-planes',
      'real-world-readiness',
      'governance',
    ],
    basicSectionIds: ['pursuit-spotlight'],
  },
  { id: 'llm-policy', route: '/llm-policy', group: 'system', title: 'Models', description: 'Local-first routing, providers, and budget.', icon: 'deployment-unit', primaryActionOwner: 'module', advancedSectionIds: ['route-decision-evidence', 'model-catalog', 'provider-inventory', 'routing-audit'], advancedSectionIdPrefixes: [{ prefix: 'provider-detail-', parentId: 'provider-inventory' }] },
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
      'family-taxonomy',
      'framework-catalog',
      'constitution-history',
      'selection-history',
      'constitution-governance',
      'framework-preference-history',
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
  },
  { id: 'runtime-control', route: '/runtime-control', group: 'system', title: 'Runtime control', description: 'Controlled execution and safety gates.', icon: 'poweroff', primaryActionOwner: 'module', advancedSectionIds: ['runtime-autonomy-policy', 'runtime-readiness-evidence'] },
  { id: 'runtime-lab', route: '/runtime-lab', group: 'system', title: 'Runtime lab', description: 'Runtime adapters and capability checks.', icon: 'api', primaryActionOwner: 'module', advancedSectionIds: ['runtime-inventory', 'runtime-feature-parity', 'runtime-mcp-readiness'], advancedSectionIdPrefixes: [{ prefix: 'runtime-inventory-', parentId: 'runtime-feature-parity' }] },
  { id: 'system-status', route: '/system-status', group: 'system', title: 'System status', description: 'Services, dependencies, and health.', icon: 'heart', primaryActionOwner: 'module', advancedSectionIds: ['system-check-details'] },
  { id: 'command-dashboard', route: '/command-dashboard', group: 'system', title: 'Legacy dashboard', description: 'Detailed operating records.', icon: 'dashboard', primaryActionOwner: 'module', advancedSectionIds: ['runtime-inventory', 'memory-inspection', 'command-history'] },
  { id: 'onboarding', route: '/onboarding', group: 'system', title: 'Getting started', description: 'A short orientation to HAI.', icon: 'read', primaryActionOwner: 'module', showInNavigation: false },
]

export const HAI_MODULE_GROUPS: Array<{ id: HaiModuleGroup; label: string; icon: string }> = [
  { id: 'work', label: 'Work', icon: 'appstore' },
  { id: 'intelligence', label: 'Intelligence', icon: 'bulb' },
  { id: 'system', label: 'System', icon: 'setting' },
]

const DYNAMIC_SECTION_SUFFIX = /^[A-Za-z0-9](?:[A-Za-z0-9._~-]*[A-Za-z0-9_-])?(?![\s\S])/u

function sectionIdMatchesPrefix(sectionId: string, prefix: HaiAdvancedSectionIdPrefix): boolean {
  if (!sectionId.startsWith(prefix.prefix)) return false

  const suffix = sectionId.slice(prefix.prefix.length)
  return suffix.length <= 64 && DYNAMIC_SECTION_SUFFIX.test(suffix) && !suffix.includes('..')
}

export function isAdvancedSectionRegistered(module: HaiModuleDefinition, sectionId: string): boolean {
  if (module.advancedSectionIds?.includes(sectionId)) return true

  return module.advancedSectionIdPrefixes?.some((prefix) => sectionIdMatchesPrefix(sectionId, prefix)) ?? false
}

export function isProgressiveSectionRegistered(module: HaiModuleDefinition, sectionId: string): boolean {
  return isAdvancedSectionRegistered(module, sectionId)
    || module.basicSectionIds?.includes(sectionId) === true
    || Object.prototype.hasOwnProperty.call(module.progressiveSectionParents ?? {}, sectionId)
}

export function progressiveSectionParentFor(module: HaiModuleDefinition, sectionId: string): string | undefined {
  const parents = module.progressiveSectionParents
  const staticParent = parents && Object.prototype.hasOwnProperty.call(parents, sectionId)
    ? parents[sectionId]
    : undefined
  if (staticParent) return staticParent

  return module.advancedSectionIdPrefixes
    ?.find((prefix) => prefix.parentId && sectionIdMatchesPrefix(sectionId, prefix))
    ?.parentId
}

export function resolveProgressiveSection(
  module: HaiModuleDefinition,
  sectionFragment: string,
): HaiProgressiveSectionResolution | undefined {
  if (!isProgressiveSectionRegistered(module, sectionFragment)) return undefined

  const ancestors: string[] = []
  const seen = new Set([sectionFragment])
  let parentId = progressiveSectionParentFor(module, sectionFragment)

  while (parentId) {
    if (
      seen.has(parentId)
      || (!isProgressiveSectionRegistered(module, parentId)
        && !Object.prototype.hasOwnProperty.call(module.progressiveSectionParents ?? {}, parentId))
    ) {
      return undefined
    }

    seen.add(parentId)
    ancestors.unshift(parentId)
    parentId = progressiveSectionParentFor(module, parentId)
  }

  const requiresAdvanced = isAdvancedSectionRegistered(module, sectionFragment)
    || ancestors.some((ancestorId) => isAdvancedSectionRegistered(module, ancestorId))

  return {
    sectionId: sectionFragment,
    ancestorIds: ancestors,
    requiredMode: requiresAdvanced ? 'advanced' : 'basic',
  }
}

export function moduleForUrl(url: string): HaiModuleDefinition {
  const fallback = HAI_MODULES.find((module) => module.id === 'control-center')!
  try {
    const segments = new DefaultUrlSerializer().parse(url).root.children[PRIMARY_OUTLET]?.segments || []
    // An encoded slash inside a segment must not become a route boundary.
    return HAI_MODULES.find((module) => {
      const parts = module.route.split('/').filter(Boolean)
      return parts.length <= segments.length && parts.every((part, index) => segments[index].path === part)
    }) || fallback
  } catch {
    return fallback
  }
}
