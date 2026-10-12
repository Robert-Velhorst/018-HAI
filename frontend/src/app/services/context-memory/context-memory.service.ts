import { Injectable } from '@angular/core';
import { HttpClient, HttpParams } from '@angular/common/http';
import { Observable } from 'rxjs';
import { map } from 'rxjs/operators';
import { IContextMemoryService, IMemoryQueryRequest, IMemoryQueryResult } from '../context-memory.service.interface';
import {
  IContextMemory,
  IContextMemoryRequest,
  IMemoryExport,
  IMemoryRetrieveRequest,
  IMemoryRetrieveResult,
  ISemanticMemoryReindexResult,
} from '../../models/context-memory.model.interface';

@Injectable({
  providedIn: 'root',
})
export class ContextMemoryService implements IContextMemoryService {
  private apiUrl = '/api/v1/memory';

  constructor(private http: HttpClient) {}

  query(request: IMemoryQueryRequest): Observable<IMemoryQueryResult> {
    let params = new HttpParams()
      .set('includeArchived', String(request.includeArchived === true))
      .set('page', String(this.boundedInteger(request.page, 1, Number.MAX_SAFE_INTEGER)))
      .set('pageSize', String(this.boundedInteger(request.pageSize, 20, 100)));
    // Owner identity is supplied only by the authenticated backend boundary.
    for (const key of ['projectKey', 'q', 'kind', 'tag', 'sort', 'order'] as const) {
      const value = request[key]?.trim();
      if (value) params = params.set(key, value);
    }
    return this.http.get<IMemoryQueryResult>(`${this.apiUrl}/query`, { params });
  }

  list(projectKey?: string, includeArchived: boolean = false, limit?: number): Observable<IContextMemory[]> {
    let params = new HttpParams().set('includeArchived', String(includeArchived));
    if (projectKey) {
      params = params.set('projectKey', projectKey);
    }
    if (limit !== undefined) {
      params = params.set('limit', String(this.boundedInteger(limit, 20, 100)));
    }
    return this.http.get<IContextMemory[]>(`${this.apiUrl}/`, { params });
  }

  create(request: IContextMemoryRequest): Observable<IContextMemory> {
    return this.http.post<IContextMemory>(`${this.apiUrl}/`, request);
  }

  update(id: string, request: IContextMemoryRequest): Observable<IContextMemory> {
    return this.http.patch<IContextMemory>(`${this.apiUrl}/${encodeURIComponent(id)}`, request);
  }

  archive(id: string): Observable<IContextMemory> {
    return this.http.post<IContextMemory>(`${this.apiUrl}/${encodeURIComponent(id)}/archive`, {});
  }

  restore(id: string): Observable<IContextMemory> {
    return this.http.post<IContextMemory>(`${this.apiUrl}/${encodeURIComponent(id)}/restore`, {});
  }

  delete(id: string): Observable<void> {
    return this.http.delete<void>(`${this.apiUrl}/${encodeURIComponent(id)}`);
  }

  retrieve(request: IMemoryRetrieveRequest): Observable<IMemoryRetrieveResult> {
    return this.http.post<IMemoryRetrieveResult>(`${this.apiUrl}/retrieve`, request).pipe(
      map((result) => ({ ...result, usedContext: result.usedContext ?? [] }))
    );
  }

  reindexSemantic(limit: number = 100): Observable<ISemanticMemoryReindexResult> {
    const params = new HttpParams().set('limit', String(this.boundedInteger(limit, 100, 100)));
    return this.http.post<ISemanticMemoryReindexResult>(`${this.apiUrl}/semantic/reindex`, {}, { params });
  }

  exportMemories(projectKey?: string): Observable<IMemoryExport> {
    let params = new HttpParams();
    if (projectKey) {
      params = params.set('projectKey', projectKey);
    }
    return this.http.get<IMemoryExport>(`${this.apiUrl}/export`, { params });
  }

  private boundedInteger(value: number | undefined, fallback: number, max: number): number {
    return value !== undefined && Number.isFinite(value)
      ? Math.min(Math.max(Math.trunc(value), 1), max)
      : fallback;
  }
}
