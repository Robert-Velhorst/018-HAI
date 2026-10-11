import { Injectable } from '@angular/core';
import { HttpClient, HttpParams } from '@angular/common/http';
import { Observable } from 'rxjs';
import { IContextMemoryService } from '../context-memory.service.interface';
import {
  IContextMemory,
  IContextMemoryRequest,
  IMemoryExport,
  IMemoryPageResult,
  IMemoryQueryParams,
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

  list(projectKey?: string, includeArchived: boolean = false): Observable<IContextMemory[]> {
    let params = new HttpParams().set('includeArchived', String(includeArchived));
    if (projectKey) {
      params = params.set('projectKey', projectKey);
    }
    return this.http.get<IContextMemory[]>(`${this.apiUrl}/`, { params });
  }

  query(query: IMemoryQueryParams): Observable<IMemoryPageResult> {
    let params = new HttpParams()
      .set('includeArchived', String(Boolean(query.includeArchived)))
      .set('page', String(Math.max(1, Math.trunc(query.page || 1))))
      .set('pageSize', String(Math.min(100, Math.max(1, Math.trunc(query.pageSize || 20)))));

    const optionalParams: Array<[string, string | undefined]> = [
      ['projectKey', query.projectKey],
      ['q', query.q],
      ['kind', query.kind],
      ['tag', query.tag],
      ['sort', query.sort],
      ['order', query.order],
    ];
    optionalParams.forEach(([key, value]) => {
      const normalized = String(value || '').trim();
      if (normalized) {
        params = params.set(key, normalized);
      }
    });

    return this.http.get<IMemoryPageResult>(`${this.apiUrl}/query`, { params });
  }

  create(request: IContextMemoryRequest): Observable<IContextMemory> {
    return this.http.post<IContextMemory>(`${this.apiUrl}/`, request);
  }

  update(id: string, request: IContextMemoryRequest): Observable<IContextMemory> {
    return this.http.patch<IContextMemory>(`${this.apiUrl}/${id}`, request);
  }

  archive(id: string): Observable<IContextMemory> {
    return this.http.post<IContextMemory>(`${this.apiUrl}/${id}/archive`, {});
  }

  restore(id: string): Observable<IContextMemory> {
    return this.http.post<IContextMemory>(`${this.apiUrl}/${id}/restore`, {});
  }

  delete(id: string): Observable<void> {
    return this.http.delete<void>(`${this.apiUrl}/${id}`);
  }

  retrieve(request: IMemoryRetrieveRequest): Observable<IMemoryRetrieveResult> {
    return this.http.post<IMemoryRetrieveResult>(`${this.apiUrl}/retrieve`, request);
  }

  reindexSemantic(limit: number = 100): Observable<ISemanticMemoryReindexResult> {
    const params = new HttpParams().set('limit', String(Math.min(Math.max(limit, 1), 100)));
    return this.http.post<ISemanticMemoryReindexResult>(`${this.apiUrl}/semantic/reindex`, {}, { params });
  }

  exportMemories(projectKey?: string): Observable<IMemoryExport> {
    let params = new HttpParams();
    if (projectKey) {
      params = params.set('projectKey', projectKey);
    }
    return this.http.get<IMemoryExport>(`${this.apiUrl}/export`, { params });
  }
}
