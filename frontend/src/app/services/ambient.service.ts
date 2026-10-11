import { HttpClient, HttpParams } from '@angular/common/http';
import { Injectable } from '@angular/core';
import { Observable } from 'rxjs';
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

  overview(view: 'full' | 'summary' = 'full'): Observable<IAmbientOverview> {
    return this.http.get<IAmbientOverview>(`${this.apiUrl}/overview`, {
      params: new HttpParams().set('view', view),
    });
  }

  scan(): Observable<IAmbientScan> {
    return this.http.post<IAmbientScan>(`${this.apiUrl}/scan`, {});
  }

  updateNeed(key: string, request: IAmbientNeedUpdate): Observable<IAmbientNeed> {
    return this.http.patch<IAmbientNeed>(`${this.apiUrl}/needs/${key}`, request);
  }

  accept(id: string): Observable<IAmbientOpportunity> {
    return this.http.post<IAmbientOpportunity>(
      `${this.apiUrl}/opportunities/${id}/accept`,
      {}
    );
  }

  dismiss(id: string): Observable<IAmbientOpportunity> {
    return this.http.post<IAmbientOpportunity>(
      `${this.apiUrl}/opportunities/${id}/dismiss`,
      {}
    );
  }
}
