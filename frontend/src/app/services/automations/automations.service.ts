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
import {Observable} from "rxjs";
import { HttpClient } from "@angular/common/http";

@Injectable({
  providedIn: 'root'
})
export class AutomationsService implements IAutomationsService {
  private apiUrl = '/api/v1/automation';

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
    return this.http.post<IAutomationLaunchResult>(`${this.apiUrl}/${id}/launch`, {});
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

}
