import { Injectable } from '@angular/core';
import { HttpClient, HttpHeaders, HttpParams } from '@angular/common/http';
import { Observable } from 'rxjs';
import { map } from 'rxjs/operators';
import { IConnectedSourceService, ISourceDestructiveAuthorization } from '../connected-source.service.interface';
import {
  IConnectedSource,
  ICreateSourceRequest,
  IImportRequest,
  ISourceAuditLog,
  ISourceConnector,
  ISourceConnectionHealth,
  ISourceExtraction,
  ISourceExtractionPage,
  ISourceSearchRequest,
  ISourceSearchResult,
  ISourceSyncJob,
  ISourceManualSyncJob,
  ISourceExtractionCorrectionPatch,
  ISourceExtractionCorrectionView,
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

  connectionHealths(): Observable<ISourceConnectionHealth[]> {
    return this.http.get<ISourceConnectionHealth[]>(`${this.apiUrl}/connection-health`);
  }

  syncJobs(sourceId?: string): Observable<ISourceSyncJob[]> {
    let params = new HttpParams();
    if (sourceId) {
      params = params.set('sourceId', sourceId);
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

  revoke(sourceId: string, authorization: ISourceDestructiveAuthorization): Observable<IConnectedSource> {
    return this.http.post<IConnectedSource>(`${this.apiUrl}/${encodeURIComponent(sourceId)}/revoke`, {}, {
      headers: this.destructiveHeaders(authorization),
    });
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

  extractions(projectKey: string, includeArchived: boolean, limit = 100): Observable<ISourceExtractionPage> {
    return this.http.get<ISourceExtraction[]>(`${this.apiUrl}/extractions`, {
      params: new HttpParams()
        .set('projectKey', projectKey || '')
        .set('includeArchived', includeArchived)
        .set('limit', limit),
      observe: 'response',
    }).pipe(map((response) => {
      const items = response.body || [];
      const totalHeader = response.headers.get('X-Total-Count');
      const total = totalHeader?.trim() ? Number(totalHeader) : NaN;
      const responseLimit = Number(response.headers.get('X-Result-Limit'));
      return {
        items,
        totalCount: Number.isFinite(total) && total >= 0 ? total : items.length,
        limit: Number.isFinite(responseLimit) && responseLimit > 0 ? responseLimit : limit,
      };
    }));
  }

  submitManualSync(sourceId: string, request: { projectKey?: string }, idempotencyKey: string): Observable<ISourceManualSyncJob> {
    return this.http.post<ISourceManualSyncJob>(
      `${this.apiUrl}/${sourceId}/sync-jobs`,
      request,
      { headers: new HttpHeaders({ 'Idempotency-Key': idempotencyKey }) }
    );
  }

  manualSyncJob(id: string): Observable<ISourceManualSyncJob> {
    return this.http.get<ISourceManualSyncJob>(`${this.apiUrl}/sync-jobs/${id}`, {
      headers: new HttpHeaders({ 'Cache-Control': 'no-cache' }),
    });
  }

  submitExtractionCorrection(
    extractionId: string,
    patch: ISourceExtractionCorrectionPatch,
    ifMatchRevision: string,
    idempotencyKey: string,
  ): Observable<ISourceExtractionCorrectionView> {
    return this.http.patch<ISourceExtractionCorrectionView>(
      `${this.apiUrl}/extractions/${encodeURIComponent(extractionId)}`,
      patch,
      { headers: new HttpHeaders({ 'If-Match': ifMatchRevision, 'Idempotency-Key': idempotencyKey }) },
    );
  }

  extractionCorrection(correctionId: string): Observable<ISourceExtractionCorrectionView> {
    return this.http.get<ISourceExtractionCorrectionView>(
      `${this.apiUrl}/extraction-corrections/${encodeURIComponent(correctionId)}`,
      { headers: new HttpHeaders({ 'Cache-Control': 'no-cache' }) },
    );
  }

  archiveExtraction(id: string): Observable<ISourceExtraction> {
    return this.http.post<ISourceExtraction>(`${this.apiUrl}/extractions/${id}/archive`, {});
  }

  deleteExtraction(id: string, authorization: ISourceDestructiveAuthorization): Observable<void> {
    return this.http.delete<void>(`${this.apiUrl}/extractions/${encodeURIComponent(id)}`, {
      headers: this.destructiveHeaders(authorization),
    });
  }

  private destructiveHeaders(authorization: ISourceDestructiveAuthorization): HttpHeaders {
    return new HttpHeaders({
      'X-HAI-Task-ID': authorization.taskId,
      'X-HAI-Approval-Source-ID': authorization.approvalSourceId,
      'X-HAI-Approval-Binding-Digest': authorization.approvalBindingDigest,
      'X-HAI-Idempotency-Key': authorization.idempotencyKey,
    });
  }

  auditLogs(sourceId?: string): Observable<ISourceAuditLog[]> {
    let params = new HttpParams();
    if (sourceId) {
      params = params.set('sourceId', sourceId);
    }
    return this.http.get<ISourceAuditLog[]>(`${this.apiUrl}/audit-logs`, { params });
  }
}
