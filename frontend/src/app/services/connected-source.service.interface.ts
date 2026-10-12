import { Observable } from 'rxjs';

// References to an existing durable approval, not client-granted authority.
export interface ISourceDestructiveAuthorization {
  taskId: string;
  approvalSourceId: string;
  approvalBindingDigest: string;
  idempotencyKey: string;
}
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
  ISourceSyncResult,
  IKnowledgeGraphResult,
  IScheduledSyncRun,
  ISourceExtractionCorrectionPatch,
  ISourceExtractionCorrectionView,
} from '../models/connected-source.model.interface';

export interface IConnectedSourceService {
  connectors(): Observable<ISourceConnector[]>;
  sources(includeDisabled: boolean): Observable<IConnectedSource[]>;
  connectionHealth(sourceId: string): Observable<ISourceConnectionHealth>;
  connectionHealths(): Observable<ISourceConnectionHealth[]>;
  syncJobs(sourceId?: string): Observable<ISourceSyncJob[]>;
  submitManualSync(sourceId: string, request: { projectKey?: string }, idempotencyKey: string): Observable<ISourceManualSyncJob>;
  manualSyncJob(id: string): Observable<ISourceManualSyncJob>;
  createSource(request: ICreateSourceRequest): Observable<IConnectedSource>;
  startGoogleOAuth(sourceId: string): Observable<{ authorizeUrl: string }>;
  sync(sourceId: string, request: IImportRequest): Observable<ISourceSyncResult>;
  transcribe(sourceId: string): Observable<ISourceSyncResult>;
  extractDocuments(sourceId: string): Observable<ISourceSyncResult>;
  runDueScheduledSyncs(): Observable<IScheduledSyncRun>;
  reindex(sourceId: string): Observable<ISourceSyncResult>;
  pause(sourceId: string): Observable<IConnectedSource>;
  resume(sourceId: string): Observable<IConnectedSource>;
  revoke(sourceId: string, authorization: ISourceDestructiveAuthorization): Observable<IConnectedSource>;
  search(request: ISourceSearchRequest): Observable<ISourceSearchResult>;
  knowledgeGraph(projectKey: string, includeArchived: boolean, includeSensitive: boolean): Observable<IKnowledgeGraphResult>;
  extractions(projectKey: string, includeArchived: boolean, limit?: number): Observable<ISourceExtractionPage>;
  submitExtractionCorrection(
    extractionId: string,
    patch: ISourceExtractionCorrectionPatch,
    ifMatchRevision: string,
    idempotencyKey: string,
  ): Observable<ISourceExtractionCorrectionView>;
  extractionCorrection(correctionId: string): Observable<ISourceExtractionCorrectionView>;
  archiveExtraction(id: string): Observable<ISourceExtraction>;
  deleteExtraction(id: string, authorization: ISourceDestructiveAuthorization): Observable<void>;
  auditLogs(sourceId?: string): Observable<ISourceAuditLog[]>;
}
