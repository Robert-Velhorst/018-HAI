import { HttpClient } from '@angular/common/http'
import { Injectable } from '@angular/core'
import { Observable } from 'rxjs'
import {
  IBackgroundStatus,
  IEmergencyStopState,
  IEmergencyStopVerification,
  IReadiness,
  IRecoveryReport,
} from '../models/runtime-control.model.interface'

export interface IControlAuthorization {
  idempotencyKey: string
  taskId: string
  approvalSourceId: string
  approvalBindingDigest: string
}

interface IModeApprovalPreparation {
  authorizationRequired: boolean
  authorization?: IControlAuthorization
}

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

  prepareResume(): Observable<IControlAuthorization> {
    return this.http.post<IControlAuthorization>(`${this.apiUrl}/background/resume/approval`, {})
  }

  resume(authorization: IControlAuthorization): Observable<{ emergencyStop: IEmergencyStopState }> {
    return this.http.post<{ emergencyStop: IEmergencyStopState }>(`${this.apiUrl}/background/resume`, authorization)
  }

  prepareModeChange(mode: string): Observable<IModeApprovalPreparation> {
    return this.http.post<IModeApprovalPreparation>(`${this.apiUrl}/background/mode/approval`, { mode })
  }

  setMode(mode: string, authorization?: IControlAuthorization): Observable<{ mode: string }> {
    return this.http.patch<{ mode: string }>(`${this.apiUrl}/background/mode`, { mode, ...authorization })
  }

  verifyEmergencyStop(): Observable<IEmergencyStopVerification> {
    return this.http.post<IEmergencyStopVerification>(`${this.apiUrl}/windows-runtime/emergency-stop/verify`, {})
  }

  recover(): Observable<IRecoveryReport> {
    return this.http.post<IRecoveryReport>(`${this.apiUrl}/windows-runtime/recovery`, {})
  }
}
