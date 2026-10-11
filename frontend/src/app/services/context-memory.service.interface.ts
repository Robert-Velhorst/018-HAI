import { Observable } from 'rxjs';
import {
  IContextMemory,
  IContextMemoryRequest,
  IMemoryExport,
  IMemoryRetrieveRequest,
  IMemoryRetrieveResult,
  ISemanticMemoryReindexResult,
} from '../models/context-memory.model.interface';

export interface IContextMemoryService {
  query(request: IMemoryQueryRequest): Observable<IMemoryQueryResult>;
  list(projectKey?: string, includeArchived?: boolean, limit?: number): Observable<IContextMemory[]>;
  create(request: IContextMemoryRequest): Observable<IContextMemory>;
  update(id: string, request: IContextMemoryRequest): Observable<IContextMemory>;
  archive(id: string): Observable<IContextMemory>;
  restore(id: string): Observable<IContextMemory>;
  delete(id: string): Observable<void>;
  retrieve(request: IMemoryRetrieveRequest): Observable<IMemoryRetrieveResult>;
  reindexSemantic(limit?: number): Observable<ISemanticMemoryReindexResult>;
  exportMemories(projectKey?: string): Observable<IMemoryExport>;
}

export interface IMemoryQueryRequest {
  projectKey?: string;
  includeArchived?: boolean;
  q?: string;
  kind?: string;
  tag?: string;
  sort?: 'updatedAt' | 'createdAt' | 'confidence' | 'kind' | 'relevance';
  order?: 'asc' | 'desc';
  page?: number;
  pageSize?: number;
}

export interface IMemoryQueryResult {
  items: IContextMemory[];
  total: number;
  page: number;
  pageSize: number;
  totalPages: number;
  sort: string;
  order: string;
  search?: string;
  kind?: string;
  tag?: string;
}
