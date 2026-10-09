import { HttpClient } from '@angular/common/http'
import { Injectable } from '@angular/core'
import { Observable } from 'rxjs'
import {
  IBrainCatalogAdoptionPlan,
  IBrainCatalogCapabilityRecommendationResponse,
  IBrainCatalogOSSInsightDiscoveryReport,
  IBrainCatalogOSSInsightReview,
  IBrainCatalogRepositoryDiscoveryMaintenanceReview,
  IBrainCatalogRevalidationRun,
  IBrainCatalogResponse,
  IBrainCatalogUpstreamReview,
  IBrainSkillInventory,
} from '../models/brain-catalog.model.interface'

@Injectable({ providedIn: 'root' })
export class BrainCatalogService {
  constructor(private http: HttpClient) {}

  overview(): Observable<IBrainCatalogResponse> {
    return this.http.get<IBrainCatalogResponse>('/api/v1/brain-catalog/')
  }

  skillInventory(): Observable<IBrainSkillInventory> {
    return this.http.get<IBrainSkillInventory>('/api/v1/brain-skills/')
  }

  adoptionPlan(): Observable<IBrainCatalogAdoptionPlan> {
    return this.http.get<IBrainCatalogAdoptionPlan>('/api/v1/brain-catalog/adoption-plan')
  }

  revalidate(id: string): Observable<IBrainCatalogUpstreamReview> {
    return this.http.post<IBrainCatalogUpstreamReview>(`/api/v1/brain-catalog/${encodeURIComponent(id)}/revalidate`, {})
  }

  revalidationHistory(limit: number = 30): Observable<IBrainCatalogUpstreamReview[]> {
    return this.http.get<IBrainCatalogUpstreamReview[]>('/api/v1/brain-catalog/revalidation-history', {
      params: { limit: String(limit) },
    })
  }

  runDueRevalidations(): Observable<IBrainCatalogRevalidationRun> {
    return this.http.post<IBrainCatalogRevalidationRun>('/api/v1/brain-catalog/revalidation/run', {})
  }

  collectionRevalidationHistory(limit: number = 5): Observable<IBrainCatalogOSSInsightReview[]> {
    return this.http.get<IBrainCatalogOSSInsightReview[]>('/api/v1/brain-catalog/collection-revalidation-history', {
      params: { limit: String(limit) },
    })
  }

  repositoryDiscoveryRevalidationHistory(limit: number = 5): Observable<IBrainCatalogRepositoryDiscoveryMaintenanceReview[]> {
    return this.http.get<IBrainCatalogRepositoryDiscoveryMaintenanceReview[]>(
      '/api/v1/brain-catalog/repository-discovery-revalidation-history',
      { params: { limit: String(limit) } }
    )
  }

  revalidateOSSInsightCollections(): Observable<IBrainCatalogOSSInsightReview> {
    return this.http.post<IBrainCatalogOSSInsightReview>('/api/v1/brain-catalog/ossinsight/revalidate', {})
  }

  discoverOSSInsightRepositories(): Observable<IBrainCatalogOSSInsightDiscoveryReport> {
    return this.http.post<IBrainCatalogOSSInsightDiscoveryReport>('/api/v1/brain-catalog/ossinsight/discover', {})
  }

  discoverReviewableOSSInsightRepositories(): Observable<IBrainCatalogOSSInsightDiscoveryReport> {
    return this.http.post<IBrainCatalogOSSInsightDiscoveryReport>('/api/v1/brain-catalog/ossinsight/discover/reviewable', {})
  }

  revalidateOSSInsightDiscovery(repository: string, scope: 'candidate' | 'reviewable' = 'candidate'): Observable<IBrainCatalogUpstreamReview> {
    return this.http.post<IBrainCatalogUpstreamReview>('/api/v1/brain-catalog/ossinsight/discoveries/revalidate', { repository, scope })
  }

  recommendCapabilities(need: string): Observable<IBrainCatalogCapabilityRecommendationResponse> {
    return this.http.post<IBrainCatalogCapabilityRecommendationResponse>('/api/v1/brain-catalog/recommend', { need })
  }
}
