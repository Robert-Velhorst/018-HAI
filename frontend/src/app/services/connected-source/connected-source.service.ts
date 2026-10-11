import { Injectable } from '@angular/core';
import { HttpClient, HttpParams } from '@angular/common/http';
import { Observable } from 'rxjs';
import { IConnectedSourceService } from '../connected-source.service.interface';
import {
  IConnectedSource,
  ICreateSourceRequest,
  IImportRequest,
  ISourceAuditLog,
  ISourceConnector,
  ISourceConnectionHealth,
  ISourceOverview,
  ISourceExtraction,
  ISourceSearchRequest,
  ISourceSearchResult,
  ISourceSyncJob,
  ISourceSyncResult,
  IKnowledgeGraphResult,
  IScheduledSyncRun,
} from '../../models/connected-source.model.interface';

@Injectable({
  providedIn: 'root',
})
export class ConnectedSourceService implements IConnectedSourceService {
  private apiUrl = '/api/v1/sources';

  constructor(private http: HttpClient) {}

  connectors(): Observable<ISourceConnector[]> {
    return this.http.get<ISourceConnector[]>(`${this.apiUrl}/connectors`);
  }

  sources(includeDisabled: boolean): Observable<IConnectedSource[]> {
    return this.http.get<IConnectedSource[]>(this.apiUrl + '/', {
      params: new HttpParams().set('includeDisabled', includeDisabled),
    });
  }

  connectionHealth(sourceId: string): Observable<ISourceConnectionHealth> {
    return this.http.get<ISourceConnectionHealth>(`${this.apiUrl}/${sourceId}/health`);
  }

  syncJobs(sourceId?: string, limit?: number): Observable<ISourceSyncJob[]> {
    let params = new HttpParams();
    if (sourceId) {
      params = params.set('sourceId', sourceId);
    }
    if (limit) {
      params = params.set('limit', limit);
    }
    return this.http.get<ISourceSyncJob[]>(`${this.apiUrl}/sync-jobs`, { params });
  }

  createSource(request: ICreateSourceRequest): Observable<IConnectedSource> {
    return this.http.post<IConnectedSource>(this.apiUrl + '/', request);
  }

  // Returns the Google consent URL to open so the user authorizes a Google source
  // in their own browser. The backend issues a signed state tying it to sourceId.
  startGoogleOAuth(sourceId: string): Observable<{ authorizeUrl: string }> {
    return this.http.get<{ authorizeUrl: string }>(
      `${this.apiUrl}/oauth/google/start`,
      { params: { sourceId } }
    );
  }

  sync(sourceId: string, request: IImportRequest): Observable<ISourceSyncResult> {
    return this.http.post<ISourceSyncResult>(`${this.apiUrl}/${sourceId}/sync`, request);
  }

  transcribe(sourceId: string): Observable<ISourceSyncResult> {
    return this.http.post<ISourceSyncResult>(`${this.apiUrl}/${sourceId}/transcribe`, null);
  }

  extractDocuments(sourceId: string): Observable<ISourceSyncResult> {
    return this.http.post<ISourceSyncResult>(`${this.apiUrl}/${sourceId}/extract-documents`, null);
  }

  runDueScheduledSyncs(): Observable<IScheduledSyncRun> {
    return this.http.post<IScheduledSyncRun>(`${this.apiUrl}/sync-due`, {});
  }

  reindex(sourceId: string): Observable<ISourceSyncResult> {
    return this.http.post<ISourceSyncResult>(`${this.apiUrl}/${sourceId}/reindex`, {});
  }

  pause(sourceId: string): Observable<IConnectedSource> {
    return this.http.post<IConnectedSource>(`${this.apiUrl}/${sourceId}/pause`, {});
  }

  resume(sourceId: string): Observable<IConnectedSource> {
    return this.http.post<IConnectedSource>(`${this.apiUrl}/${sourceId}/resume`, {});
  }

  revoke(sourceId: string): Observable<IConnectedSource> {
    return this.http.post<IConnectedSource>(`${this.apiUrl}/${sourceId}/revoke`, {});
  }

  search(request: ISourceSearchRequest): Observable<ISourceSearchResult> {
    return this.http.post<ISourceSearchResult>(`${this.apiUrl}/search`, request);
  }

  knowledgeGraph(
    projectKey: string,
    includeArchived: boolean,
    includeSensitive: boolean
  ): Observable<IKnowledgeGraphResult> {
    return this.http.get<IKnowledgeGraphResult>(`${this.apiUrl}/knowledge-graph`, {
      params: new HttpParams()
        .set('projectKey', projectKey || '')
        .set('includeArchived', includeArchived)
        .set('includeSensitive', includeSensitive),
    });
  }

  extractions(projectKey: string, includeArchived: boolean, limit?: number): Observable<ISourceExtraction[]> {
    let params = new HttpParams()
      .set('projectKey', projectKey || '')
      .set('includeArchived', includeArchived);
    if (limit) {
      params = params.set('limit', limit);
    }
    return this.http.get<ISourceExtraction[]>(`${this.apiUrl}/extractions`, { params });
  }

  updateExtraction(id: string, extraction: Partial<ISourceExtraction>): Observable<ISourceExtraction> {
    return this.http.patch<ISourceExtraction>(`${this.apiUrl}/extractions/${id}`, extraction);
  }

  archiveExtraction(id: string): Observable<ISourceExtraction> {
    return this.http.post<ISourceExtraction>(`${this.apiUrl}/extractions/${id}/archive`, {});
  }

  deleteExtraction(id: string): Observable<void> {
    return this.http.delete<void>(`${this.apiUrl}/extractions/${id}`);
  }

  auditLogs(sourceId?: string, limit?: number): Observable<ISourceAuditLog[]> {
    let params = new HttpParams();
    if (sourceId) {
      params = params.set('sourceId', sourceId);
    }
    if (limit) {
      params = params.set('limit', limit);
    }
    return this.http.get<ISourceAuditLog[]>(`${this.apiUrl}/audit-logs`, { params });
  }

  overview(projectKey: string, includeArchived: boolean): Observable<ISourceOverview> {
    return this.http.get<ISourceOverview>(`${this.apiUrl}/overview`, {
      params: new HttpParams()
        .set('projectKey', projectKey || '')
        .set('includeArchived', includeArchived),
    });
  }
}
