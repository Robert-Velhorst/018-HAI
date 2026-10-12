import { HttpClient, HttpParams } from '@angular/common/http'
import { Injectable } from '@angular/core'
import { Observable } from 'rxjs'
import {
  IAccountFeed,
  IBackgroundOperationsOverview,
  IBackgroundRunReport,
  IOperation,
  IOperationEvent,
  IOperationsDashboard,
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

  overview(filters?: { status?: string; risk?: string; limit?: number }): Observable<IBackgroundOperationsOverview> {
    let params = new HttpParams()
    if (filters?.status) params = params.set('status', filters.status)
    if (filters?.risk) params = params.set('risk', filters.risk)
    if (filters?.limit) params = params.set('limit', String(filters.limit))
    return this.http.get<IBackgroundOperationsOverview>(`${this.apiUrl}/background/overview`, { params })
  }

  get(id: string): Observable<IOperation> {
    return this.http.get<IOperation>(`${this.apiUrl}/operations/${id}`)
  }

  events(id: string): Observable<{ events: IOperationEvent[] }> {
    return this.http.get<{ events: IOperationEvent[] }>(`${this.apiUrl}/operations/${id}/events`)
  }

  approve(id: string): Observable<IOperation> {
    return this.http.post<IOperation>(`${this.apiUrl}/operations/${id}/approve`, {})
  }

  run(id: string): Observable<{ operation: IOperation; verified: boolean; failed: boolean }> {
    return this.http.post<{ operation: IOperation; verified: boolean; failed: boolean }>(
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
