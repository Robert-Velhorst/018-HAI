import { HttpClient } from '@angular/common/http';
import { Injectable } from '@angular/core';
import { Observable, throwError } from 'rxjs';
import {
  IAmbientNeed,
  IAmbientNeedUpdate,
  IAmbientOpportunity,
  IAmbientOverview,
  IAmbientScan,
} from '../models/ambient.model.interface';

@Injectable({ providedIn: 'root' })
export class AmbientService {
  private readonly apiUrl = '/api/v1/ambient';

  constructor(private http: HttpClient) {}

  overview(): Observable<IAmbientOverview> {
    return this.http.get<IAmbientOverview>(`${this.apiUrl}/overview`);
  }

  scan(): Observable<IAmbientScan> {
    return this.http.post<IAmbientScan>(`${this.apiUrl}/scan`, {});
  }

  updateNeed(key: string, request: IAmbientNeedUpdate): Observable<IAmbientNeed> {
    if (typeof key !== 'string' || !/^[a-z][a-z0-9_-]{0,79}$/.test(key) || !request ||
        ![request.currentLevel, request.targetLevel, request.priorityWeight].every(value => Number.isInteger(value) && value >= 0 && value <= 100) ||
        typeof request.enabled !== 'boolean' || (request.notes !== undefined && typeof request.notes !== 'string')) {
      return throwError(() => new Error('Invalid priority update. Refresh the profile and check its fields.'));
    }
    const body: IAmbientNeedUpdate = {
      currentLevel: request.currentLevel,
      targetLevel: request.targetLevel,
      priorityWeight: request.priorityWeight,
      enabled: request.enabled,
      ...(request.notes !== undefined ? { notes: request.notes } : {}),
    };
    return this.http.patch<IAmbientNeed>(`${this.apiUrl}/needs/${key}`, body);
  }

  accept(id: string): Observable<IAmbientOpportunity> {
    if (!this.validOpportunityId(id)) return throwError(() => new Error('Invalid proposal reference. Refresh before deciding.'));
    return this.http.post<IAmbientOpportunity>(
      `${this.apiUrl}/opportunities/${id}/accept`,
      {}
    );
  }

  dismiss(id: string): Observable<IAmbientOpportunity> {
    if (!this.validOpportunityId(id)) return throwError(() => new Error('Invalid proposal reference. Refresh before deciding.'));
    return this.http.post<IAmbientOpportunity>(
      `${this.apiUrl}/opportunities/${id}/dismiss`,
      {}
    );
  }

  private validOpportunityId(id: string): boolean {
    return typeof id === 'string' && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(id) &&
      id !== '00000000-0000-0000-0000-000000000000';
  }
}
