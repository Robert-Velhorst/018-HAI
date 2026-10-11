import { HttpClient } from '@angular/common/http'
import { Injectable } from '@angular/core'
import { Observable } from 'rxjs'
import {
  ControlApprovalAction,
  ControlApprovalDecision,
  IBackgroundStatus,
  IControlAuthorization,
  IDecidedControlApproval,
  IEmergencyStopState,
  IEmergencyStopVerification,
  IPreparedControlApproval,
  IReadiness,
  IRecoveryReport,
} from '../models/runtime-control.model.interface'

@Injectable({ providedIn: 'root' })
export class RuntimeControlService {
  private readonly apiUrl = '/api/v1'

  constructor(private http: HttpClient) {}

  status(): Observable<IBackgroundStatus> {
    return this.http.get<IBackgroundStatus>(`${this.apiUrl}/background/status`)
  }

  readiness(): Observable<IReadiness> {
    return this.http.get<IReadiness>(`${this.apiUrl}/windows-runtime/readiness`)
  }

  pause(reason: string): Observable<{ emergencyStop: IEmergencyStopState }> {
    return this.http.post<{ emergencyStop: IEmergencyStopState }>(`${this.apiUrl}/background/pause`, { reason })
  }

  prepareControlApproval(
    action: ControlApprovalAction,
    targetMode?: string
  ): Observable<IPreparedControlApproval> {
    return this.http.post<IPreparedControlApproval>(
      `${this.apiUrl}/background/control-approvals`,
      targetMode ? { action, targetMode } : { action }
    )
  }

  decideControlApproval(
    requestId: string,
    decision: ControlApprovalDecision,
    reason: string
  ): Observable<IDecidedControlApproval> {
    return this.http.post<IDecidedControlApproval>(
      `${this.apiUrl}/background/control-approvals/${encodeURIComponent(requestId)}/decision`,
      { decision, reason }
    )
  }

  resume(
    authorization: IControlAuthorization
  ): Observable<{ emergencyStop: IEmergencyStopState }> {
    return this.http.post<{ emergencyStop: IEmergencyStopState }>(
      `${this.apiUrl}/background/resume`,
      authorization
    )
  }

  setMode(
    mode: string,
    authorization?: IControlAuthorization
  ): Observable<{ mode: string }> {
    return this.http.patch<{ mode: string }>(
      `${this.apiUrl}/background/mode`,
      { mode, ...(authorization ?? {}) }
    )
  }

  verifyEmergencyStop(): Observable<IEmergencyStopVerification> {
    return this.http.post<IEmergencyStopVerification>(`${this.apiUrl}/windows-runtime/emergency-stop/verify`, {})
  }

  recover(): Observable<IRecoveryReport> {
    return this.http.post<IRecoveryReport>(`${this.apiUrl}/windows-runtime/recovery`, {})
  }
}
