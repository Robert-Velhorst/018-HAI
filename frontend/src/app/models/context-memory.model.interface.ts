export interface IContextMemory {
  id?: string;
  projectKey?: string;
  kind: string;
  content: string;
  summary?: string;
  tags?: string;
  confidence: number;
  sourceUri?: string;
  sourceLabel?: string;
  contentHash?: string;
  archived: boolean;
  lastUsedAt?: string;
  createdAt?: string;
  updatedAt?: string;
}

export interface IContextMemoryRequest {
  projectKey?: string;
  kind: string;
  content: string;
  summary?: string;
  tags?: string[];
  confidence?: number;
  sourceUri?: string;
  sourceLabel?: string;
  archived?: boolean;
}

export type MemoryQuerySort = 'updatedAt' | 'createdAt' | 'confidence' | 'kind' | 'relevance';
export type MemoryQueryOrder = 'asc' | 'desc';

export interface IMemoryQueryParams {
  projectKey?: string;
  includeArchived?: boolean;
  q?: string;
  kind?: string;
  tag?: string;
  sort?: MemoryQuerySort;
  order?: MemoryQueryOrder;
  page?: number;
  pageSize?: number;
}

export interface IMemoryPageResult {
  items: IContextMemory[];
  total: number;
  page: number;
  pageSize: number;
  totalPages: number;
  sort: MemoryQuerySort;
  order: MemoryQueryOrder;
  search?: string;
  kind?: string;
  tag?: string;
}

export interface IMemoryRetrieveRequest {
  query: string;
  projectKey?: string;
  limit?: number;
}

export interface IRankedMemory {
  memory: IContextMemory;
  score: number;
  explanation: string;
}

export interface IMemoryRetrieveResult {
  query: string;
  projectKey?: string;
  usedContext: IRankedMemory[];
  explanation: string;
}

export interface IMemoryExport {
  format: string;
  memories: IContextMemory[];
}

export interface ISemanticMemoryReindexResult {
  enabled: boolean;
  attempted: number;
  indexed: number;
  failed: number;
  deferred: number;
  explanation: string;
}
