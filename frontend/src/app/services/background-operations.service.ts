import { HttpClient, HttpParams } from '@angular/common/http'
import { Injectable } from '@angular/core'
import { Observable } from 'rxjs'
import { map } from 'rxjs/operators'
import {
  IAccountFeed,
  IBackgroundRunReport,
  IOperation,
  IOperationApprovalBinding,
  IOperationApprovalPreview,
  IOperationEvent,
  IOperationRunResult,
  IOperationsDashboard,
  IOperationsOverview,
} from '../models/background-operations.model.interface'

@Injectable({ providedIn: 'root' })
export class BackgroundOperationsService {
  private readonly apiUrl = '/api/v1'

  constructor(private http: HttpClient) {}

  list(filters?: { status?: string; risk?: string; limit?: number }): Observable<{ operations: IOperation[] }> {
    let params = new HttpParams()
    if (filters?.status) params = params.set('status', filters.status)
    if (filters?.risk) params = params.set('risk', filters.risk)
    if (filters?.limit) params = params.set('limit', String(filters.limit))
    return this.http.get<{ operations: IOperation[] }>(`${this.apiUrl}/operations`, { params })
  }

  dashboard(): Observable<IOperationsDashboard> {
    return this.http.get<IOperationsDashboard>(`${this.apiUrl}/operations/dashboard`)
  }

  overview(filters?: { status?: string; risk?: string; limit?: number }): Observable<IOperationsOverview> {
    let params = new HttpParams()
    if (filters?.status) params = params.set('status', filters.status)
    if (filters?.risk) params = params.set('risk', filters.risk)
    if (filters?.limit) params = params.set('limit', String(filters.limit))
    return this.http.get<IOperationsOverview>(`${this.apiUrl}/operations/overview`, { params })
  }

  get(id: string): Observable<IOperation> {
    return this.http.get<IOperation>(`${this.apiUrl}/operations/${id}`)
  }

  events(id: string): Observable<{ events: IOperationEvent[] }> {
    return this.http.get<{ events: IOperationEvent[] }>(`${this.apiUrl}/operations/${id}/events`)
  }

  approvalPreview(id: string): Observable<IOperationApprovalPreview> {
    return this.http.get<unknown>(`${this.apiUrl}/operations/${id}/approval-preview`).pipe(
      map((value) => readOperationApprovalPreview(value, id)),
    )
  }

  approve(id: string, binding?: IOperationApprovalBinding): Observable<IOperation> {
    return this.http.post<IOperation>(`${this.apiUrl}/operations/${id}/approve`, binding ?? {})
  }

  run(id: string): Observable<IOperationRunResult> {
    return this.http.post<IOperationRunResult>(
      `${this.apiUrl}/operations/${id}/run`,
      {}
    )
  }

  runBackground(): Observable<IBackgroundRunReport> {
    return this.http.post<IBackgroundRunReport>(`${this.apiUrl}/background/run`, {})
  }

  feeds(): Observable<{ feeds: IAccountFeed[] }> {
    return this.http.get<{ feeds: IAccountFeed[] }>(`${this.apiUrl}/account-feeds`)
  }
}

export function isSourceDerivedOperation(operation: IOperation): boolean {
  return !!operation.sourceIdentityHash?.trim() || !!operation.sourceId || !!operation.accountFeedId ||
    !!operation.sourceObservationId || !!operation.sourceRevisionHash?.trim()
}

export function readOperationApprovalPreview(value: unknown, expectedOperationId: string): IOperationApprovalPreview {
  const body = asRecord(value)
  const operation = body && asRecord(body['operation'])
  const revision = body && asRecord(body['revision'])
  const operationId = operation && operation['id']
  const operationVersion = operation && operation['version']
  const revisionOperationId = revision && revision['operationId']
  const revisionVersion = revision && revision['version']
  const revisionDigest = revision && revision['revisionDigest']
  const validVersion = (version: unknown): version is number => Number.isSafeInteger(version) && (version as number) > 0

  if (!body || !operation || !revision || operationId !== expectedOperationId ||
    revisionOperationId !== expectedOperationId || !validVersion(operationVersion) ||
    !validVersion(revisionVersion) || revisionVersion !== operationVersion ||
    typeof operation['status'] !== 'string' || operation['status'] !== 'awaiting_approval' ||
    operation['requiresApproval'] !== true || typeof operation['updatedAt'] !== 'string' ||
    !Number.isFinite(Date.parse(operation['updatedAt'])) ||
    typeof revisionDigest !== 'string' || !/^[a-f0-9]{64}$/i.test(revisionDigest)) {
    throw new Error('The source approval preview was invalid or did not match this operation.')
  }

  const parsedOperation = operation as unknown as IOperation
  if (!isSourceDerivedOperation(parsedOperation)) {
    throw new Error('The source approval preview is not bound to a source-derived operation.')
  }

  return {
    operation: parsedOperation,
    revision: { operationId: revisionOperationId, version: revisionVersion, revisionDigest },
  }
}

function asRecord(value: unknown): Record<string, unknown> | undefined {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
    ? value as Record<string, unknown>
    : undefined
}
