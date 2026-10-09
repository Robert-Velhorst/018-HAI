import {Injectable} from '@angular/core';
import {IAutomationsService} from '../automations.service.interface';
import {
  IAutomationDiagnostics,
  IAutomationHealthResult,
  IAutomationHealthSummary,
  IAutomationLaunchResult,
  IAutomationModel,
} from "../../models/automation.model.interface";
import { IAgentRuntimeStopResult } from "../../models/agent-runtime.model.interface";
import {defer, Observable, tap} from "rxjs";
import { HttpClient } from "@angular/common/http";
import { isConfirmedLaunchResult } from '../../control-room/launch-recovery';

export class AutomationLaunchSafetyError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'AutomationLaunchSafetyError';
  }
}

@Injectable({
  providedIn: 'root'
})
export class AutomationsService implements IAutomationsService {
  private apiUrl = '/api/v1/automation';
  private readonly launchKeyPrefix = 'hai.automation-launch.idempotency.v1.';

  constructor(private http: HttpClient) {
  }

  getAutomations(): Observable<IAutomationModel[]> {
    return this.http.get<IAutomationModel[]>(`${this.apiUrl}/`);
  }

  addAutomation(automation: IAutomationModel): Observable<IAutomationModel> {
    const formData = new FormData();

    formData.append('name', automation.name);
    formData.append('host', automation.host);
    formData.append('port', automation.port.toString());
    formData.append('position', automation.position.toString());
    formData.append('removeImage', automation.removeImage.toString());
    this.appendIfSet(formData, 'launchType', automation.launchType);
    this.appendIfSet(formData, 'launchTarget', automation.launchTarget);
    this.appendIfSet(formData, 'runtimeType', automation.runtimeType);
    this.appendIfSet(formData, 'runtimeModel', automation.runtimeModel);
    this.appendIfSet(formData, 'serviceName', automation.serviceName);
    this.appendIfSet(formData, 'routePath', automation.routePath);
    this.appendIfSet(formData, 'publicUrl', automation.publicUrl);
    this.appendIfSet(formData, 'localUrl', automation.localUrl);
    this.appendIfSet(formData, 'dependencyNotes', automation.dependencyNotes);
    this.appendIfSet(formData, 'healthCheckType', automation.healthCheckType);
    this.appendIfSet(formData, 'healthCheckUrl', automation.healthCheckUrl);
    this.appendIfSet(formData, 'healthCheckIntervalSeconds', automation.healthCheckIntervalSeconds);
    this.appendIfSet(formData, 'expectedHttpStatus', automation.expectedHttpStatus);

    if (automation.id) {
      formData.append('id', automation.id);
    }

    if (automation.imageFile) {
      formData.append('imageFile', automation.imageFile, automation.imageFile.name);
    }

    return this.http.post<IAutomationModel>(`${this.apiUrl}/`, formData);
  }


  deleteAutomation(id: string): Observable<void> {
    return this.http.delete<void>(`${this.apiUrl}/${id}`);
  }

  getAutomation(id: string): Observable<IAutomationModel> {
    return this.http.get<IAutomationModel>(`${this.apiUrl}/${id}`);
  }

  updateAutomation(automation: IAutomationModel): Observable<IAutomationModel> {
    return this.http.patch<IAutomationModel>(`${this.apiUrl}/`, automation);
  }

  swapAutomations(automation_id1: string, automation_id2: string): Observable<void> {
    return this.http.patch<void>(`${this.apiUrl}/swap/${automation_id1}/${automation_id2}`, {});
  }

  getHealthSummary(): Observable<IAutomationHealthSummary> {
    return this.http.get<IAutomationHealthSummary>(`${this.apiUrl}/health-summary`);
  }

  launchAutomation(id: string): Observable<IAutomationLaunchResult> {
    return defer(() => {
      const idempotencyKey = this.getOrCreateLaunchKey(id);
      return this.http.post<IAutomationLaunchResult>(
        `${this.apiUrl}/${id}/launch`,
        {},
        { headers: { 'Idempotency-Key': idempotencyKey } }
      ).pipe(tap(result => {
        if (isConfirmedLaunchResult(result, id)) {
          this.clearLaunchKey(id, idempotencyKey);
        }
      }));
    });
  }

  stopRuntimeTask(id: string): Observable<IAgentRuntimeStopResult> {
    return this.http.post<IAgentRuntimeStopResult>(`${this.apiUrl}/${id}/stop-runtime`, {});
  }

  runHealthCheck(id: string): Observable<IAutomationHealthResult> {
    return this.http.post<IAutomationHealthResult>(`${this.apiUrl}/${id}/health-check`, {});
  }

  getDiagnostics(id: string): Observable<IAutomationDiagnostics> {
    return this.http.get<IAutomationDiagnostics>(`${this.apiUrl}/${id}/diagnostics`);
  }

  private appendIfSet(formData: FormData, key: string, value: unknown): void {
    if (value !== undefined && value !== null && value !== '') {
      formData.append(key, String(value));
    }
  }

  private getOrCreateLaunchKey(automationId: string): string {
    const storageKey = this.launchStorageKey(automationId);
    try {
      const existing = window.localStorage.getItem(storageKey);
      if (existing) {
        if (!this.isUuid(existing)) {
          throw new AutomationLaunchSafetyError(
            'A saved launch request key is invalid. HAI did not send another start request.'
          );
        }
        return existing;
      }

      const generated = globalThis.crypto?.randomUUID?.();
      if (!generated || !this.isUuid(generated)) {
        throw new AutomationLaunchSafetyError(
          'This browser cannot create a secure launch request key. HAI did not send the start request.'
        );
      }

      window.localStorage.setItem(storageKey, generated);
      if (window.localStorage.getItem(storageKey) !== generated) {
        throw new AutomationLaunchSafetyError(
          'This browser could not persist a launch request key. HAI did not send the start request.'
        );
      }
      return generated;
    } catch (error) {
      if (error instanceof AutomationLaunchSafetyError) throw error;
      throw new AutomationLaunchSafetyError(
        'This browser could not safely save a launch request key. HAI did not send the start request.'
      );
    }
  }

  private clearLaunchKey(automationId: string, expectedKey: string): void {
    try {
      const storageKey = this.launchStorageKey(automationId);
      if (window.localStorage.getItem(storageKey) === expectedKey) {
        window.localStorage.removeItem(storageKey);
      }
    } catch {
      // Keeping a completed key is safe: a later start will replay its result.
    }
  }

  private launchStorageKey(automationId: string): string {
    return `${this.launchKeyPrefix}${encodeURIComponent(automationId)}`;
  }

  private isUuid(value: string): boolean {
    return /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(value);
  }

}
