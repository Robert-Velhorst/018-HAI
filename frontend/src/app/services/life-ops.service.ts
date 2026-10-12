import { HttpClient, HttpErrorResponse, HttpParams } from '@angular/common/http'
import { Injectable } from '@angular/core'
import { Observable, catchError, map, of, throwError } from 'rxjs'
import {
  CapacitySnapshot,
  CreateGoalRequest,
  EntityDomainLink,
  GoalNode,
  GoalTreeNode,
  LifeDomain,
  LifeDomainId,
  LifeOpsOverview,
  LinkEntityRequest,
  NeedObservation,
  PriorityAssessment,
  PriorityAssessmentRequest,
  RecordCapacityRequest,
  RecordNeedRequest,
  UpdateGoalRequest,
} from '../models/life-ops.model'

@Injectable({ providedIn: 'root' })
export class LifeOpsService {
  private readonly apiUrl = '/api/v1/life'

  constructor(private http: HttpClient) {}

  domains(): Observable<LifeDomain[]> {
    return this.http.get<{ domains: LifeDomain[] }>(`${this.apiUrl}/domains`)
      .pipe(map((response) => response.domains ?? []))
  }

  overview(): Observable<LifeOpsOverview> {
    return this.http.get<LifeOpsOverview>(`${this.apiUrl}/overview`)
  }

  linkEntity(request: LinkEntityRequest): Observable<EntityDomainLink> {
    return this.http.post<EntityDomainLink>(
      `${this.apiUrl}/entities/link`,
      this.entityLinkPayload(request)
    )
  }

  entityDomains(entityType: string, entityId: string): Observable<EntityDomainLink[]> {
    return this.http.get<{ links: EntityDomainLink[] }>(
      `${this.apiUrl}/entities/${encodeURIComponent(entityType)}/${encodeURIComponent(entityId)}/domains`
    ).pipe(map((response) => response.links ?? []))
  }

  recordNeed(request: RecordNeedRequest): Observable<NeedObservation> {
    return this.http.post<NeedObservation>(
      `${this.apiUrl}/needs`,
      this.needPayload(request)
    )
  }

  needs(domainId?: LifeDomainId, limit = 100): Observable<NeedObservation[]> {
    let params = new HttpParams().set('limit', this.boundedLimit(limit))
    if (domainId) params = params.set('domainId', domainId)
    return this.http.get<{ observations: NeedObservation[] }>(
      `${this.apiUrl}/needs`,
      { params }
    ).pipe(map((response) => response.observations ?? []))
  }

  recordCapacity(request: RecordCapacityRequest): Observable<CapacitySnapshot> {
    return this.http.post<CapacitySnapshot>(
      `${this.apiUrl}/capacity`,
      this.capacityPayload(request)
    )
  }

  capacityHistory(limit = 25): Observable<CapacitySnapshot[]> {
    return this.http.get<{ snapshots: CapacitySnapshot[] }>(
      `${this.apiUrl}/capacity`,
      { params: new HttpParams().set('limit', this.boundedLimit(limit)) }
    ).pipe(map((response) => response.snapshots ?? []))
  }

  latestCapacity(): Observable<CapacitySnapshot | null> {
    return this.http.get<CapacitySnapshot>(`${this.apiUrl}/capacity/latest`).pipe(
      catchError((error: HttpErrorResponse) => {
        if (error.status === 404) return of(null)
        return throwError(() => error)
      })
    )
  }

  createGoal(request: CreateGoalRequest): Observable<GoalNode> {
    return this.http.post<GoalNode>(`${this.apiUrl}/goals`, this.goalPayload(request))
  }

  goals(): Observable<GoalNode[]> {
    return this.http.get<{ goals: GoalNode[] }>(`${this.apiUrl}/goals`)
      .pipe(map((response) => response.goals ?? []))
  }

  goalForest(): Observable<GoalTreeNode[]> {
    return this.http.get<{ forest: GoalTreeNode[] }>(`${this.apiUrl}/goals/forest`)
      .pipe(map((response) => response.forest ?? []))
  }

  goal(id: string): Observable<GoalNode> {
    return this.http.get<GoalNode>(`${this.apiUrl}/goals/${encodeURIComponent(id)}`)
  }

  updateGoal(id: string, request: UpdateGoalRequest): Observable<GoalNode> {
    return this.http.patch<GoalNode>(
      `${this.apiUrl}/goals/${encodeURIComponent(id)}`,
      this.updateGoalPayload(request)
    )
  }

  assessPriority(request: PriorityAssessmentRequest): Observable<PriorityAssessment> {
    return this.http.post<PriorityAssessment>(
      `${this.apiUrl}/priority/assess`,
      this.priorityPayload(request)
    )
  }

  private boundedLimit(limit: number): string {
    return String(Number.isInteger(limit) && limit >= 1 && limit <= 500 ? limit : 100)
  }

  private entityLinkPayload(request: LinkEntityRequest): LinkEntityRequest {
    return {
      entityType: request.entityType,
      entityId: request.entityId,
      domainId: request.domainId,
      primary: request.primary,
      confidence: request.confidence,
      sourceLabel: request.sourceLabel,
      ...(request.sourceUri ? { sourceUri: request.sourceUri } : {}),
      ...(request.evidence ? { evidence: request.evidence } : {}),
      ...(request.verificationStatus
        ? { verificationStatus: request.verificationStatus }
        : {}),
    }
  }

  private needPayload(request: RecordNeedRequest): RecordNeedRequest {
    return {
      domainId: request.domainId,
      needLevel: request.needLevel,
      state: request.state,
      currentLevel: request.currentLevel,
      targetLevel: request.targetLevel,
      priority: request.priority,
      confidence: request.confidence,
      ...(request.evidence ? { evidence: request.evidence } : {}),
      sourceLabel: request.sourceLabel,
      ...(request.sourceUri ? { sourceUri: request.sourceUri } : {}),
      observedAt: request.observedAt,
      ...(request.expiresAt ? { expiresAt: request.expiresAt } : {}),
      needsReview: request.needsReview,
    }
  }

  private capacityPayload(request: RecordCapacityRequest): RecordCapacityRequest {
    return {
      status: request.status,
      signals: {
        ...request.signals,
        availableTools: request.signals.availableTools ?? [],
        availableHelpers: request.signals.availableHelpers ?? [],
      },
      timeAvailableMinutes: request.timeAvailableMinutes,
      concurrentWorkLimit: request.concurrentWorkLimit,
      currentLoad: request.currentLoad,
      ...(request.planningStepLimit
        ? { planningStepLimit: request.planningStepLimit }
        : {}),
      ...(request.constraints ? { constraints: request.constraints } : {}),
      sourceLabel: request.sourceLabel,
      ...(request.sourceUri ? { sourceUri: request.sourceUri } : {}),
      capturedAt: request.capturedAt,
      confidence: request.confidence,
      needsReview: request.needsReview,
    }
  }

  private goalPayload(request: CreateGoalRequest): CreateGoalRequest {
    return {
      ...(request.parentId ? { parentId: request.parentId } : {}),
      level: request.level,
      domainIds: request.domainIds,
      title: request.title,
      ...(request.description ? { description: request.description } : {}),
      ...(request.successCriteria
        ? { successCriteria: request.successCriteria }
        : {}),
      ...(request.stopConditions ? { stopConditions: request.stopConditions } : {}),
      ...(request.status ? { status: request.status } : {}),
      confidence: request.confidence,
      sourceLabel: request.sourceLabel,
      ...(request.sourceUri ? { sourceUri: request.sourceUri } : {}),
      ...(request.targetAt ? { targetAt: request.targetAt } : {}),
    }
  }

  private updateGoalPayload(request: UpdateGoalRequest): UpdateGoalRequest {
    return {
      ...(request.parentId !== undefined ? { parentId: request.parentId } : {}),
      ...(request.clearParent !== undefined ? { clearParent: request.clearParent } : {}),
      ...(request.level !== undefined ? { level: request.level } : {}),
      ...(request.domainIds !== undefined ? { domainIds: request.domainIds } : {}),
      ...(request.title !== undefined ? { title: request.title } : {}),
      ...(request.description !== undefined
        ? { description: request.description }
        : {}),
      ...(request.successCriteria !== undefined
        ? { successCriteria: request.successCriteria }
        : {}),
      ...(request.stopConditions !== undefined
        ? { stopConditions: request.stopConditions }
        : {}),
      ...(request.status !== undefined ? { status: request.status } : {}),
      ...(request.confidence !== undefined
        ? { confidence: request.confidence }
        : {}),
      ...(request.sourceLabel !== undefined
        ? { sourceLabel: request.sourceLabel }
        : {}),
      ...(request.sourceUri !== undefined ? { sourceUri: request.sourceUri } : {}),
      ...(request.targetAt !== undefined ? { targetAt: request.targetAt } : {}),
      ...(request.clearTarget !== undefined ? { clearTarget: request.clearTarget } : {}),
    }
  }

  private priorityPayload(
    request: PriorityAssessmentRequest
  ): PriorityAssessmentRequest {
    return {
      entityType: request.entityType,
      entityId: request.entityId,
      title: request.title,
      ...(request.deadline ? { deadline: request.deadline } : {}),
      factors: { ...request.factors },
      ...(request.capacity ? { capacity: request.capacity } : {}),
    }
  }
}
