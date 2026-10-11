import { Observable } from 'rxjs';
import {
  IContextMemory,
  IContextMemoryRequest,
  IMemoryExport,
  IMemoryPageResult,
  IMemoryQueryParams,
  IMemoryRetrieveRequest,
  IMemoryRetrieveResult,
  ISemanticMemoryReindexResult,
} from '../models/context-memory.model.interface';

export interface IContextMemoryService {
  list(projectKey?: string, includeArchived?: boolean): Observable<IContextMemory[]>;
  query(params: IMemoryQueryParams): Observable<IMemoryPageResult>;
  create(request: IContextMemoryRequest): Observable<IContextMemory>;
  update(id: string, request: IContextMemoryRequest): Observable<IContextMemory>;
  archive(id: string): Observable<IContextMemory>;
  restore(id: string): Observable<IContextMemory>;
  delete(id: string): Observable<void>;
  retrieve(request: IMemoryRetrieveRequest): Observable<IMemoryRetrieveResult>;
  reindexSemantic(limit?: number): Observable<ISemanticMemoryReindexResult>;
  exportMemories(projectKey?: string): Observable<IMemoryExport>;
}
