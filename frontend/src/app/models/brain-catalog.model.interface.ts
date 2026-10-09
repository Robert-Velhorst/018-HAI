import { ISkillInventory, ISkillInventoryEntry } from './skills.model.interface'

export type BrainCatalogStatus =
  | 'integrated_profile'
  | 'candidate'
  | 'compatibility_only'
  | 'reference_only'
  | 'license_review'
  | 'excluded'

export type IBrainSkillInventoryEntry = ISkillInventoryEntry
export type IBrainSkillInventory = ISkillInventory

export interface IBrainCatalogControlMapping {
  sourcePattern: string
  haiControl: string
  boundary: string
}

export interface IBrainCatalogImplementationBoundary {
  control: string;
  route: string;
  sourcePath: string;
  scope: string;
}

export interface IBrainCatalogEntry {
  id: string
  name: string
  upstreamUrl: string
  sourceCatalogUrl: string
  sourceCollection?: string
  status: BrainCatalogStatus
  category: string
  integrationMode: string
  capabilities: string[]
  recommendedFor: string[]
  requiresApproval: boolean
  localFirstCompatible: boolean
  activation: string
  rationale: string
  verifiedAt: string
  verificationNote: string
  controlMappings?: IBrainCatalogControlMapping[]
  implementation?: IBrainCatalogImplementationBoundary
}

export interface IBrainCatalogUpstreamReview {
  id: string
  name: string
  upstreamUrl: string
  resolvedRepository?: string
  resolvedUpstreamUrl?: string
  repositoryMoved: boolean
  checkedAt: string
  available: boolean
  archived: boolean
  license?: string
  defaultBranch?: string
  pushedAt?: string
  message: string
  disposition: BrainCatalogStatus
  readiness: string
  readinessReason: string
  requiredGates?: string[]
}

export interface IBrainCatalogRevalidationRun {
  enabled: boolean;
  eligible: number;
  checked: number;
  reused: number;
  failed: number;
  results: IBrainCatalogUpstreamReview[];
  collectionReview?: IBrainCatalogCollectionRevalidationRun;
  repositoryDiscoveryReview?: IBrainCatalogRepositoryDiscoveryRevalidationRun;
  runAt: string;
}

export interface IBrainCatalogCollectionRevalidationRun {
  enabled: boolean;
  reused: boolean;
  failed: boolean;
  review: IBrainCatalogOSSInsightReview;
  runAt: string;
}

export interface IBrainCatalogRepositoryDiscoveryRevalidationRun {
  enabled: boolean;
  reused: boolean;
  failed: boolean;
  review: IBrainCatalogRepositoryDiscoveryMaintenanceReview;
  runAt: string;
}

export interface IBrainCatalogRepositoryDiscoveryMaintenanceReview {
  checkedAt: string;
  sourceUrl: string;
  scope: 'candidate' | 'reviewable';
  available: boolean;
  collectionsScreened: number;
  eligibleCollections: number;
  collectionsChecked: number;
  repositoriesChecked: number;
  knownProfileHits: number;
  unreviewedDiscoveries: number;
  missingCollections?: string[];
  unavailableCollections?: string[];
  candidateRepositories?: string[];
  candidatesTruncated: boolean;
  message: string;
}

export interface IBrainCatalogOSSInsightReview {
  checkedAt: string
  sourceUrl: string
  available: boolean
  expectedTotal: number
  currentTotal: number
  newCollections?: string[]
  missingExpected?: string[]
  message: string
}

export interface IBrainCatalogOSSInsightDiscovery {
  collection: string
  disposition: BrainCatalogCollectionDisposition
  repository: string
  sourceUrl: string
  rationale: string
  reviewTrack: string
  priority: number
  risk: 'low' | 'medium' | 'high'
  reviewReason: string
  triage?: 'manual_review' | 'reference_only' | 'deferred' | string
  triageReason?: string
  reviewAllowed?: boolean
  relatedCollections?: string[]
  relatedSourceUrls?: string[]
}

export interface IBrainCatalogOSSInsightKnownProfileHit {
  repository: string;
  catalogEntryIds: string[];
  relatedCollections?: string[];
  relatedSourceUrls?: string[];
}

export interface IBrainCatalogOSSInsightDiscoveryReport {
  checkedAt: string
  sourceUrl: string
  available: boolean
  cached: boolean
  scope: 'candidate' | 'reviewable'
  collectionsScreened: number
  candidateCollections: number
  reviewableCollections: number
  eligibleCollections: number
  collectionsChecked: number
  repositoriesChecked: number
  duplicateSourceHits: number
  maximumDiscoveries: number
  sourceQueryLimit?: number
  collectionsAtQueryLimit?: number
  knownProfileHits: number
  knownProfiles?: IBrainCatalogOSSInsightKnownProfileHit[]
  knownProfilesTruncated: boolean
  discoveries?: IBrainCatalogOSSInsightDiscovery[]
  missingCollections?: string[]
  unavailableCollections?: string[]
  discoveriesTruncated: boolean
  message: string
}

export interface IBrainCatalogCapabilityRecommendation {
  id: string
  name: string
  status: BrainCatalogStatus
  role: string
  rationale: string
  requiresApproval: boolean
  activation: string
  upstreamUrl: string
  sourceCatalogUrl: string
  sourceCollection?: string
  verifiedAt: string
  verificationNote: string
  score: number
  matchedTerms: string[]
  reasons: string[]
  nextStep: string
  roadmapPriority: number
  roadmapReason: string
  capabilityPlanes: string[]
}

export interface IBrainCatalogCapabilityRecommendationResponse {
  need: string
  expandedTerms: string[]
  recommendations: IBrainCatalogCapabilityRecommendation[]
  message: string
}

export interface IBrainCatalogAdoptionPlanItem {
  id: string
  name: string
  status: BrainCatalogStatus
  category: string
  planes: string[]
  priority: number
  priorityReason: string
  integrationMode: string
  localFirst: boolean
  requiresApproval: boolean
  activation: string
  requiredGates: string[]
  upstreamUrl: string
  sourceCatalogUrl: string
  sourceCollection?: string
  verificationNote: string
  recommendedAction: string
}

export interface IBrainCatalogAdoptionPlan {
  items: IBrainCatalogAdoptionPlanItem[]
  message: string
}

export interface IBrainCatalogSource {
  name: string
  url: string
  scope: string
}

export type BrainCatalogCollectionDisposition =
  | 'represented_in_catalog'
  | 'review_candidate'
  | 'reference_only'
  | 'not_adopted'

export interface IBrainCatalogCollectionScreeningEntry {
  collection: string
  page: number
  disposition: BrainCatalogCollectionDisposition
  relatedEntryIds?: string[]
  rationale: string
  sourceUrl: string
}

export interface IBrainCatalogCollectionScreening {
  total: number
  represented: number
  candidates: number
  reference: number
  deferred: number
  entries: IBrainCatalogCollectionScreeningEntry[]
}

export interface IBrainCatalogPlaneEntry {
  id: string
  name: string
  status: BrainCatalogStatus
}

export interface IBrainCatalogPlaneCoverage {
  plane: string
  name: string
  description: string
  integrated: number
  candidates: number
  held: number
  entries: IBrainCatalogPlaneEntry[]
}

export interface IBrainCatalogResponse {
  sourceCatalog: string
  discoverySources: IBrainCatalogSource[]
  verifiedAt: string
  entries: IBrainCatalogEntry[]
  planeCoverage: IBrainCatalogPlaneCoverage[]
  collectionScreening: IBrainCatalogCollectionScreening
  activationPolicy: string
}
