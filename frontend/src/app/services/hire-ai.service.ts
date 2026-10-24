import { HttpClient } from '@angular/common/http'
import { Injectable } from '@angular/core'
import { Observable } from 'rxjs'
import { IHireAICursor, IHireAIFilters, IHireAISearchResult, IHireAIStatus } from '../models/hire-ai.model.interface'

@Injectable({ providedIn: 'root' })
export class HireAIService {
  private readonly apiUrl = '/api/v1/hire-ai'
  private readonly pageSize = 25

  constructor(private http: HttpClient) {}

  status(): Observable<IHireAIStatus> {
    return this.http.get<IHireAIStatus>(`${this.apiUrl}/status`)
  }

  search(filters: IHireAIFilters, cursor?: IHireAICursor): Observable<IHireAISearchResult> {
    return this.http.post<IHireAISearchResult>(`${this.apiUrl}/jobs/search`, {
      filters,
      limit: this.pageSize,
      ...(cursor ? { cursor } : {}),
    })
  }
}
