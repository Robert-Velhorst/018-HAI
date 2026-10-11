export interface ISourceConnector {
  id: string;
  connectorKey: string;
  name: string;
  category: string;
  supportedModes: string;
  requiredScopes: string;
  localOnlyCapable: boolean;
  enabled: boolean;
  adapterStatus?: string;
  statusReason?: string;
}

export interface IConnectedSource {
  id: string;
  connectorKey: string;
  name: string;
  category: string;
  enabled: boolean;
  localOnly: boolean;
  syncFrequency: string;
  syncTarget?: string;
  defaultProjectKey?: string;
  ingestionModes: string;
  permissions: string;
  excludePatterns: string;
  cursor?: string;
  status: string;
  lastSyncedAt?: string;
  revokedAt?: string;
}

export interface ISourceConnectionHealth {
  sourceId: string;
  connectorKey: string;
  status: string;
  reason: string;
  configured: boolean;
  authorized: boolean;
  requiresReconnect: boolean;
  cursorPhase?: string;
  tokenExpiry?: string;
  lastSyncedAt?: string;
}

export interface ISourceOverview {
  extractionCount: number;
  sensitiveExtractionCount: number;
  uncertainExtractionCount: number;
  failedJobs: number;
  pendingJobs: number;
  extractionCountsBySource: Record<string, number>;
  latestJobStatusBySource: Record<string, string>;
}

export interface ICreateSourceRequest {
  connectorKey: string;
  name: string;
  category?: string;
  enabled: boolean;
  localOnly: boolean;
  syncFrequency?: string;
  syncTarget?: string;
  defaultProjectKey?: string;
  ingestionModes?: string[];
  permissions?: string[];
  excludePatterns?: string[];
}

export interface IImportItem {
  externalId: string;
  title: string;
  content: string;
  sourceUri?: string;
  itemType?: string;
  projectKey?: string;
  metadata?: string;
}

export interface IImportRequest {
  mode?: string;
  items: IImportItem[];
  folderPath?: string;
  projectKey?: string;
  limit?: number;
  maxBytes?: number;
}

export interface ISourceSyncJob {
  id: string;
  sourceId: string;
  mode: string;
  status: string;
  itemsSeen: number;
  itemsAdded: number;
  itemsUpdated: number;
  itemsFailed: number;
  message?: string;
  startedAt: string;
  completedAt?: string;
}

export interface ISourceExtraction {
  id: string;
  sourceId: string;
  rawItemId: string;
  projectKey?: string;
  contentType: string;
  text: string;
  summary?: string;
  entities?: string;
  dates?: string;
  tasks?: string;
  decisions?: string;
  followUps?: string;
  sourceUri?: string;
  sourceLabel?: string;
  sensitive: boolean;
  uncertain: boolean;
  archived: boolean;
  updatedAt: string;
}

export interface ISourceSyncResult {
  job: ISourceSyncJob;
  extractions: ISourceExtraction[];
  pursuitOutcomes?: ISourcePursuitRoutingOutcome[];
  lifeGraphProjections?: ISourceLifeGraphProjectionOutcome[];
  message: string;
  errors?: string[];
  warnings?: string[];
}

export interface ISourceLifeGraphProjectionOutcome {
  extractionId: string;
  documentId: string;
  linkedEntityIds?: string[];
  relationIds?: string[];
  alreadyExisted: boolean;
  advisoryOnly: boolean;
  canExecute: boolean;
  grantsAuthority: boolean;
}

export interface ISourcePursuitRoutingOutcome {
  extractionId?: string;
  workflowId?: string;
  pursuitId?: string;
  status: string;
  message: string;
}

export interface IScheduledSyncRun {
  checked: number;
  due: number;
  completed: number;
  failed: number;
  skipped: number;
  messages: string[];
}

export interface ISourceSearchRequest {
  query: string;
  projectKey?: string;
  limit?: number;
  includeSensitive?: boolean;
}

export interface IRankedExtraction {
  extraction: ISourceExtraction;
  score: number;
  explanation: string;
}

export interface ISourceSearchResult {
  query: string;
  projectKey?: string;
  usedContext: IRankedExtraction[];
  explanation: string;
}

export interface IKnowledgeGraphSourceRef {
  extractionId: string;
  sourceUri?: string;
  sourceLabel?: string;
}

export interface IKnowledgeGraphEntity {
  id: string;
  name: string;
  kind: string;
  status: 'candidate';
  mentionCount: number;
  sourceRefs: IKnowledgeGraphSourceRef[];
}

export interface IKnowledgeGraphRelationship {
  id: string;
  fromEntityId: string;
  toEntityId: string;
  relationship: 'co_occurs_in_source';
  status: 'candidate';
  supportCount: number;
  sourceRefs: IKnowledgeGraphSourceRef[];
}

export interface IKnowledgeGraphTimelineEvent {
  id: string;
  dateHint: string;
  parsedAt?: string;
  status: 'candidate';
  sourceRefs: IKnowledgeGraphSourceRef[];
}

export interface IKnowledgeGraphResult {
  projectKey?: string;
  status: 'candidate_only';
  extractionCount: number;
  sensitiveExcluded: number;
  entities: IKnowledgeGraphEntity[];
  relationships: IKnowledgeGraphRelationship[];
  timeline: IKnowledgeGraphTimelineEvent[];
  warnings: string[];
}

export interface ISourceAuditLog {
  id: string;
  sourceId?: string;
  action: string;
  message: string;
  createdAt: string;
}
