import { HttpClient, HttpParams } from '@angular/common/http'
import { Injectable } from '@angular/core'
import { Observable } from 'rxjs'
import {
  MonitorCompositionAttemptListResponse,
  MonitorCompositionListResponse,
  MonitorRunListResponse,
  MonitorTargetListResponse,
  MonitorTargetWriteResponse,
  ObservationRecordListResponse,
  ProcessDueResult,
  RecoverAmbientMonitorsResponse,
  RegisterMonitorTargetRequest,
  RunDueMonitorsRequest,
  SetMonitorEnabledRequest,
} from '../models/ambient-monitor.model.interface'

@Injectable({ providedIn: 'root' })
export class AmbientMonitorService {
  private readonly apiUrl = '/api/v1/outcome-evaluations/workspaces'

  constructor(private http: HttpClient) {}

  getMonitor(workspaceId: string, outcomeId: string): Observable<MonitorTargetListResponse> {
    return this.http.get<MonitorTargetListResponse>(this.monitorUrl(workspaceId, outcomeId))
  }

  registerTarget(
    workspaceId: string,
    outcomeId: string,
    request: RegisterMonitorTargetRequest
  ): Observable<MonitorTargetWriteResponse> {
    return this.http.put<MonitorTargetWriteResponse>(this.monitorUrl(workspaceId, outcomeId), {
      idempotencyKey: request.idempotencyKey,
      targetId: request.targetId,
      indicatorId: request.indicatorId,
      sourceKind: request.sourceKind,
      enabled: request.enabled,
      cadenceSeconds: Math.trunc(request.cadenceSeconds),
      firstRunAt: request.firstRunAt,
    })
  }

  setEnabled(
    workspaceId: string,
    outcomeId: string,
    targetId: string,
    request: SetMonitorEnabledRequest
  ): Observable<MonitorTargetWriteResponse> {
    return this.http.patch<MonitorTargetWriteResponse>(
      `${this.targetUrl(workspaceId, outcomeId, targetId)}/enabled`,
      { idempotencyKey: request.idempotencyKey, enabled: request.enabled }
    )
  }

  listObservations(
    workspaceId: string,
    outcomeId: string,
    targetId: string,
    limit = 50
  ): Observable<ObservationRecordListResponse> {
    return this.http.get<ObservationRecordListResponse>(
      `${this.targetUrl(workspaceId, outcomeId, targetId)}/observations`,
      { params: this.limitParams(limit) }
    )
  }

  listRuns(
    workspaceId: string,
    outcomeId: string,
    targetId: string,
    limit = 50
  ): Observable<MonitorRunListResponse> {
    return this.http.get<MonitorRunListResponse>(
      `${this.targetUrl(workspaceId, outcomeId, targetId)}/runs`,
      { params: this.limitParams(limit) }
    )
  }

  listCompositions(
    workspaceId: string,
    outcomeId: string,
    targetId: string,
    limit = 25
  ): Observable<MonitorCompositionListResponse> {
    return this.http.get<MonitorCompositionListResponse>(
      `${this.targetUrl(workspaceId, outcomeId, targetId)}/compositions`,
      { params: this.limitParams(limit) }
    )
  }

  listCompositionAttempts(
    workspaceId: string,
    outcomeId: string,
    targetId: string,
    deliveryId: string,
    limit = 25
  ): Observable<MonitorCompositionAttemptListResponse> {
    return this.http.get<MonitorCompositionAttemptListResponse>(
      `${this.targetUrl(workspaceId, outcomeId, targetId)}/compositions/${encodeURIComponent(deliveryId)}/attempts`,
      { params: this.limitParams(limit) }
    )
  }

  runDue(workspaceId: string, request: RunDueMonitorsRequest): Observable<ProcessDueResult> {
    return this.http.post<ProcessDueResult>(
      `${this.workspaceUrl(workspaceId)}/monitors/run-due`,
      {
        workerId: request.workerId,
        asOf: request.asOf,
        leaseSeconds: Math.trunc(request.leaseSeconds),
        limit: Math.trunc(request.limit),
      }
    )
  }

  recover(workspaceId: string, asOf: string): Observable<RecoverAmbientMonitorsResponse> {
    return this.http.post<RecoverAmbientMonitorsResponse>(
      `${this.workspaceUrl(workspaceId)}/monitors/recover`,
      { asOf }
    )
  }

  private workspaceUrl(workspaceId: string): string {
    return `${this.apiUrl}/${encodeURIComponent(workspaceId)}`
  }

  private monitorUrl(workspaceId: string, outcomeId: string): string {
    return `${this.workspaceUrl(workspaceId)}/outcomes/${encodeURIComponent(outcomeId)}/monitor`
  }

  private targetUrl(workspaceId: string, outcomeId: string, targetId: string): string {
    return `${this.monitorUrl(workspaceId, outcomeId)}/${encodeURIComponent(targetId)}`
  }

  private limitParams(limit: number): HttpParams {
    return new HttpParams().set('limit', String(Math.min(Math.max(Math.trunc(limit), 1), 500)))
  }
}
