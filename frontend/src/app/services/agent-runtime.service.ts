import { HttpClient } from '@angular/common/http';
import { Injectable } from '@angular/core';
import { Observable } from 'rxjs';
import {
  IAgentRuntimeHealth,
  IAgentRuntimeEcosystemAuthorization,
  IAgentRuntimeInfo,
  IAgentRuntimeSkill,
  IAgentRuntimeStopResult,
} from '../models/agent-runtime.model.interface';

@Injectable({
  providedIn: 'root',
})
export class AgentRuntimeService {
  private readonly apiUrl = '/api/v1/agent-runtimes';

  constructor(private http: HttpClient) {}

  overview(): Observable<{
    runtimes: IAgentRuntimeInfo[];
    health: IAgentRuntimeHealth[];
  }> {
    return this.http.get<{
      runtimes: IAgentRuntimeInfo[];
      health: IAgentRuntimeHealth[];
    }>(`${this.apiUrl}/overview`);
  }

  openClawEcosystem(): Observable<IAgentRuntimeInfo> {
    return this.http.get<IAgentRuntimeInfo>(`${this.apiUrl}/openclaw/ecosystem`);
  }

  skills(runtimeId: string): Observable<IAgentRuntimeSkill[]> {
    return this.http.get<IAgentRuntimeSkill[]>(`${this.apiUrl}/${runtimeId}/skills`);
  }

  stopTask(runtimeId: string, taskId: string): Observable<IAgentRuntimeStopResult> {
    return this.http.post<IAgentRuntimeStopResult>(`${this.apiUrl}/${runtimeId}/tasks/${taskId}/stop`, null);
  }

  prepareOpenClawEcosystemPath(
    ecosystemPath: string
  ): Observable<IAgentRuntimeEcosystemAuthorization> {
    return this.http.post<IAgentRuntimeEcosystemAuthorization>(
      `${this.apiUrl}/openclaw/ecosystem/approval/set-path`,
      { ecosystemPath }
    );
  }

  setOpenClawEcosystemPath(
    ecosystemPath: string,
    authorization: IAgentRuntimeEcosystemAuthorization
  ): Observable<IAgentRuntimeInfo> {
    return this.http.patch<IAgentRuntimeInfo>(`${this.apiUrl}/openclaw/ecosystem`, {
      ecosystemPath,
      ...authorization,
    });
  }

  prepareOpenClawEcosystemUpload(
    file: File
  ): Observable<IAgentRuntimeEcosystemAuthorization> {
    const form = new FormData();
    form.append('ecosystem', file, file.name);
    return this.http.post<IAgentRuntimeEcosystemAuthorization>(
      `${this.apiUrl}/openclaw/ecosystem/approval/upload`,
      form
    );
  }

  uploadOpenClawEcosystem(
    file: File,
    authorization: IAgentRuntimeEcosystemAuthorization
  ): Observable<IAgentRuntimeInfo> {
    const form = new FormData();
    form.append('ecosystem', file, file.name);
    form.append('idempotencyKey', authorization.idempotencyKey);
    form.append('taskId', authorization.taskId);
    form.append('approvalSourceId', authorization.approvalSourceId);
    form.append('approvalBindingDigest', authorization.approvalBindingDigest);
    return this.http.post<IAgentRuntimeInfo>(`${this.apiUrl}/openclaw/ecosystem/upload`, form);
  }

  prepareOpenClawEcosystemRefresh(): Observable<IAgentRuntimeEcosystemAuthorization> {
    return this.http.post<IAgentRuntimeEcosystemAuthorization>(
      `${this.apiUrl}/openclaw/ecosystem/approval/refresh`,
      null
    );
  }

  refreshOpenClawEcosystem(
    authorization: IAgentRuntimeEcosystemAuthorization
  ): Observable<IAgentRuntimeInfo> {
    return this.http.post<IAgentRuntimeInfo>(`${this.apiUrl}/openclaw/ecosystem/refresh`, authorization);
  }

  prepareOpenClawEcosystemRollback(): Observable<IAgentRuntimeEcosystemAuthorization> {
    return this.http.post<IAgentRuntimeEcosystemAuthorization>(
      `${this.apiUrl}/openclaw/ecosystem/approval/rollback`,
      null
    );
  }

  rollbackOpenClawEcosystem(
    authorization: IAgentRuntimeEcosystemAuthorization
  ): Observable<IAgentRuntimeInfo> {
    return this.http.post<IAgentRuntimeInfo>(
      `${this.apiUrl}/openclaw/ecosystem/rollback`,
      authorization
    );
  }
}
