import { HttpClient, HttpParams } from '@angular/common/http'
import { Injectable } from '@angular/core'
import { Observable } from 'rxjs'
import {
  AgentBootContext,
  OperationalGraphSearch,
  OperationalGraphSnapshot,
  OperationalNeighborhood,
} from '../models/operational-graph.model'

@Injectable({ providedIn: 'root' })
export class OperationalGraphService {
  private readonly apiUrl = '/api/v1/operational-graph'

  constructor(private http: HttpClient) {}

  snapshot(): Observable<OperationalGraphSnapshot> {
    return this.http.get<OperationalGraphSnapshot>(`${this.apiUrl}/snapshot`)
  }

  search(query = '', layer = '', status = '', limit = 40): Observable<OperationalGraphSearch> {
    let params = new HttpParams().set('limit', limit)
    if (query.trim()) params = params.set('q', query.trim())
    if (layer) params = params.set('layer', layer)
    if (status) params = params.set('status', status)
    return this.http.get<OperationalGraphSearch>(`${this.apiUrl}/search`, { params })
  }

  neighborhood(id: string, depth = 1, limit = 100): Observable<OperationalNeighborhood> {
    const params = new HttpParams().set('depth', depth).set('limit', limit)
    return this.http.get<OperationalNeighborhood>(
      `${this.apiUrl}/nodes/${encodeURIComponent(id)}/neighborhood`,
      { params },
    )
  }

  agentBoot(id: string): Observable<AgentBootContext> {
    return this.http.get<AgentBootContext>(`${this.apiUrl}/agents/${encodeURIComponent(id)}/boot`)
  }
}
