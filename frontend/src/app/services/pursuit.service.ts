import { Injectable } from '@angular/core';
import { HttpClient, HttpParams } from '@angular/common/http';
import { map, Observable } from 'rxjs';
import {
  IPursuit,
  IPursuitActivity,
  IPursuitAction,
  IPursuitApprovalOverview,
  IPursuitBrief,
  IPursuitBlocker,
  IPursuitCreateRequest,
  IPursuitDashboard,
  IPursuitDashboardDecision,
  IPursuitDelegationPackage,
  IPursuitDecisionResolutionRequest,
  IPursuitDetail,
  IPursuitEvidenceResolution,
  IPursuitIntakeRequest,
  IPursuitLink,
  IPursuitLinkRequest,
  IPursuitMatchCandidate,
  IPursuitMatchRequest,
  IPursuitPlanRequest,
  IPursuitPortfolioPlanningRequest,
  IPursuitPortfolioPlanningResult,
  IPursuitPortfolioAllocationAcceptanceRequest,
  IPursuitPortfolioAllocationAcceptanceResult,
  IPursuitPortfolioExecutionProposalRequest,
  IPursuitPortfolioExecutionProposalResult,
  IPursuitPortfolioExecutionProposalDecisionRequest,
  IPursuitPortfolioExecutionProposalDecisionResult,
  IPursuitPortfolioExecutionProposalDecisionHistoryResult,
  IPursuitPortfolioCoordinationResult,
  IPursuitPortfolioDispatchRequest,
  IPursuitPortfolioDispatchResult,
  IPursuitPortfolioWorkflowEffectAuthorizationRequest,
  IPursuitPortfolioWorkflowEffectAuthorizationResult,
  IPursuitPortfolioWorkflowEffectExecutionRequest,
  IPursuitPortfolioWorkflowEffectExecutionResult,
  IPursuitPortfolioWorkflowSettlementRequest,
  IPursuitPortfolioWorkflowSettlementResult,
  IPursuitReviewRequest,
  IPursuitResourceEvent,
  IPursuitResourceEventRequest,
  IPursuitResourceUsage,
  IPursuitRoutedIntakeResult,
  IPursuitUpdateRequest,
} from '../models/pursuit.model.interface';

@Injectable({
  providedIn: 'root',
})
export class PursuitService {
  private apiUrl = '/api/v1/pursuits';

  constructor(private http: HttpClient) {}

  list(
    includeArchived: boolean = false,
    view: 'summary' | 'full' = 'summary'
  ): Observable<IPursuit[]> {
    return this.http.get<IPursuit[]>(`${this.apiUrl}/`, {
      params: new HttpParams().set('includeArchived', includeArchived).set('view', view),
    }).pipe(map((records) => (records || []).map((record) => this.normalizePursuit(record))));
  }

  dashboard(view: 'summary' | 'counts' = 'summary'): Observable<IPursuitDashboard> {
    return this.http.get<IPursuitDashboard>(`${this.apiUrl}/dashboard`, {
      params: new HttpParams().set('view', view),
    }).pipe(
      map((dashboard) => this.normalizeDashboard(dashboard)),
    );
  }

  planPortfolio(request: IPursuitPortfolioPlanningRequest): Observable<IPursuitPortfolioPlanningResult> {
    return this.http.post<IPursuitPortfolioPlanningResult>(`${this.apiUrl}/portfolio-plan`, request);
  }

  acceptPortfolioAllocation(
    request: IPursuitPortfolioAllocationAcceptanceRequest,
  ): Observable<IPursuitPortfolioAllocationAcceptanceResult> {
    return this.http.post<IPursuitPortfolioAllocationAcceptanceResult>(`${this.apiUrl}/portfolio-plan/accept`, request);
  }

  portfolioAllocations(limit: number = 20): Observable<IPursuitPortfolioAllocationAcceptanceResult[]> {
    const boundedLimit = Math.max(1, Math.min(100, Math.trunc(limit || 20)));
    return this.http.get<IPursuitPortfolioAllocationAcceptanceResult[]>(`${this.apiUrl}/portfolio-allocations`, {
      params: new HttpParams().set('limit', boundedLimit),
    });
  }

  portfolioExecutionProposals(
    allocationIds: string[],
  ): Observable<IPursuitPortfolioExecutionProposalResult[]> {
    return this.http.get<IPursuitPortfolioExecutionProposalResult[]>(
      `${this.apiUrl}/portfolio-execution-proposals`,
      { params: new HttpParams().set('allocationIds', allocationIds.join(',')) },
    );
  }

  preparePortfolioExecutionProposals(
    allocationId: string,
    request: IPursuitPortfolioExecutionProposalRequest,
  ): Observable<IPursuitPortfolioExecutionProposalResult> {
    return this.http.post<IPursuitPortfolioExecutionProposalResult>(
      `${this.apiUrl}/portfolio-allocations/${encodeURIComponent(allocationId)}/execution-proposals`,
      request,
    );
  }

  portfolioDispatchCoordination(proposalId: string): Observable<IPursuitPortfolioCoordinationResult> {
    return this.http.get<IPursuitPortfolioCoordinationResult>(
      `${this.apiUrl}/portfolio-execution-proposals/${encodeURIComponent(proposalId)}/coordination`,
    );
  }

  portfolioDispatchCoordinations(proposalIds: string[]): Observable<IPursuitPortfolioCoordinationResult[]> {
    return this.http.get<IPursuitPortfolioCoordinationResult[]>(
      `${this.apiUrl}/portfolio-execution-proposals/coordination`,
      { params: new HttpParams().set('proposalIds', proposalIds.join(',')) },
    );
  }

  dispatchPortfolioWorkflows(
    proposalId: string,
    request: IPursuitPortfolioDispatchRequest,
  ): Observable<IPursuitPortfolioDispatchResult> {
    return this.http.post<IPursuitPortfolioDispatchResult>(
      `${this.apiUrl}/portfolio-execution-proposals/${encodeURIComponent(proposalId)}/dispatch`,
      request,
    );
  }

  decidePortfolioExecutionProposalItem(
    itemId: string,
    request: IPursuitPortfolioExecutionProposalDecisionRequest,
  ): Observable<IPursuitPortfolioExecutionProposalDecisionResult> {
    return this.http.post<IPursuitPortfolioExecutionProposalDecisionResult>(
      `${this.apiUrl}/portfolio-execution-proposal-items/${encodeURIComponent(itemId)}/decisions`,
      request,
    );
  }

  portfolioExecutionProposalDecisionHistory(
    itemId: string,
    limit: number = 50,
  ): Observable<IPursuitPortfolioExecutionProposalDecisionHistoryResult> {
    const boundedLimit = Math.max(1, Math.min(100, Math.trunc(limit || 50)));
    return this.http.get<IPursuitPortfolioExecutionProposalDecisionHistoryResult>(
      `${this.apiUrl}/portfolio-execution-proposal-items/${encodeURIComponent(itemId)}/decisions`,
      { params: new HttpParams().set('limit', boundedLimit) },
    );
  }

  authorizePortfolioWorkflowEffect(
    itemId: string,
    request: IPursuitPortfolioWorkflowEffectAuthorizationRequest,
  ): Observable<IPursuitPortfolioWorkflowEffectAuthorizationResult> {
    return this.http.post<IPursuitPortfolioWorkflowEffectAuthorizationResult>(
      `${this.apiUrl}/portfolio-execution-proposal-items/${encodeURIComponent(itemId)}/authorize-workflow`,
      request,
    );
  }

  executePortfolioWorkflowEffect(
    itemId: string,
    request: IPursuitPortfolioWorkflowEffectExecutionRequest,
  ): Observable<IPursuitPortfolioWorkflowEffectExecutionResult> {
    return this.http.post<IPursuitPortfolioWorkflowEffectExecutionResult>(
      `${this.apiUrl}/portfolio-execution-proposal-items/${encodeURIComponent(itemId)}/execute-workflow`,
      request,
    );
  }

  settlePortfolioWorkflow(
    itemId: string,
    request: IPursuitPortfolioWorkflowSettlementRequest,
  ): Observable<IPursuitPortfolioWorkflowSettlementResult> {
    return this.http.post<IPursuitPortfolioWorkflowSettlementResult>(
      `${this.apiUrl}/portfolio-execution-proposal-items/${encodeURIComponent(itemId)}/settle-workflow`,
      request,
    );
  }

  brief(): Observable<IPursuitBrief> {
    return this.http.get<IPursuitBrief>(`${this.apiUrl}/brief`);
  }

  decisions(): Observable<IPursuitDashboardDecision[]> {
    return this.http.get<IPursuitDashboardDecision[]>(`${this.apiUrl}/decisions`);
  }

  create(request: IPursuitCreateRequest): Observable<IPursuit> {
    return this.http.post<IPursuit>(`${this.apiUrl}/`, request);
  }

  get(id: string): Observable<IPursuitDetail> {
    return this.http.get<IPursuitDetail>(`${this.apiUrl}/${id}`).pipe(map((detail) => this.normalizeDetail(detail)));
  }

  resourceUsage(id: string): Observable<IPursuitResourceUsage> {
    return this.http.get<IPursuitResourceUsage>(`${this.apiUrl}/${id}/resources`);
  }

  resourceEvents(id: string, limit: number = 100): Observable<IPursuitResourceEvent[]> {
    const boundedLimit = Math.max(1, Math.min(500, Math.trunc(limit || 100)));
    return this.http.get<{ events?: IPursuitResourceEvent[] }>(`${this.apiUrl}/${id}/resource-events`, {
      params: new HttpParams().set('limit', boundedLimit),
    }).pipe(map((response) => response?.events || []));
  }

  appendResourceEvent(id: string, request: IPursuitResourceEventRequest): Observable<IPursuitResourceEvent> {
    return this.http.post<IPursuitResourceEvent>(`${this.apiUrl}/${id}/resource-events`, request);
  }

  releaseResourceReservation(id: string, reservationId: string, reason: string): Observable<IPursuitResourceUsage> {
    return this.http.post<IPursuitResourceUsage>(
      `${this.apiUrl}/${id}/resource-reservations/${reservationId}/release`,
      { confirmedOrphan: true, reason },
    );
  }

  resolveEvidence(id: string, uri: string): Observable<IPursuitEvidenceResolution> {
    return this.http.get<IPursuitEvidenceResolution>(`${this.apiUrl}/${id}/evidence`, {
      params: new HttpParams().set('uri', uri),
    });
  }

  update(id: string, request: IPursuitUpdateRequest): Observable<IPursuit> {
    return this.http.patch<IPursuit>(`${this.apiUrl}/${id}`, request);
  }

  archive(id: string, archived: boolean = true, actor: string = 'robert'): Observable<IPursuit> {
    return this.http.post<IPursuit>(`${this.apiUrl}/${id}/archive`, { archived, actor });
  }

  reopen(id: string, note: string = ''): Observable<IPursuit> {
    return this.http.post<IPursuit>(`${this.apiUrl}/${id}/reopen`, { note });
  }

  refreshSummary(id: string, actor: string = 'hai'): Observable<IPursuitDetail> {
    return this.http.post<IPursuitDetail>(`${this.apiUrl}/${id}/summary`, { actor }).pipe(map((detail) => this.normalizeDetail(detail)));
  }

  review(id: string, request: IPursuitReviewRequest): Observable<IPursuitDetail> {
    return this.http.post<IPursuitDetail>(`${this.apiUrl}/${id}/review`, request).pipe(map((detail) => this.normalizeDetail(detail)));
  }

  intake(id: string, request: IPursuitIntakeRequest): Observable<IPursuitDetail> {
    return this.http.post<IPursuitDetail>(`${this.apiUrl}/${id}/intake`, request).pipe(map((detail) => this.normalizeDetail(detail)));
  }

  routeIntake(request: IPursuitIntakeRequest): Observable<IPursuitRoutedIntakeResult> {
    return this.http.post<IPursuitRoutedIntakeResult>(`${this.apiUrl}/intake`, request).pipe(
      map((result) => {
        const source = result || ({} as IPursuitRoutedIntakeResult);
        return {
          ...source,
          matches: source.matches || [],
          detail: source.detail ? this.normalizeDetail(source.detail) : undefined,
        };
      }),
    );
  }

  plan(id: string, request: IPursuitPlanRequest = {}): Observable<IPursuitDetail> {
    return this.http.post<IPursuitDetail>(`${this.apiUrl}/${id}/plan`, request).pipe(map((detail) => this.normalizeDetail(detail)));
  }

  acceptCandidate(id: string, request: IPursuitPlanRequest = {}): Observable<IPursuitDetail> {
    return this.http.post<IPursuitDetail>(`${this.apiUrl}/${id}/candidate/accept`, request).pipe(map((detail) => this.normalizeDetail(detail)));
  }

  resolveDecision(id: string, request: IPursuitDecisionResolutionRequest): Observable<IPursuitDetail> {
    return this.http.post<IPursuitDetail>(`${this.apiUrl}/${id}/decisions/resolve`, request).pipe(map((detail) => this.normalizeDetail(detail)));
  }

  link(id: string, request: IPursuitLinkRequest): Observable<IPursuitLink> {
    return this.http.post<IPursuitLink>(`${this.apiUrl}/${id}/links`, request);
  }

  deleteLink(id: string, linkId: string): Observable<void> {
    return this.http.delete<void>(`${this.apiUrl}/${id}/links/${linkId}`);
  }

  match(request: IPursuitMatchRequest): Observable<IPursuitMatchCandidate[]> {
    return this.http.post<IPursuitMatchCandidate[]>(`${this.apiUrl}/match`, request);
  }

  activity(id: string): Observable<IPursuitActivity[]> {
    return this.http.get<IPursuitActivity[]>(`${this.apiUrl}/${id}/activity`);
  }

  nextActions(id: string): Observable<IPursuitAction[]> {
    return this.http.get<IPursuitAction[]>(`${this.apiUrl}/${id}/next-actions`);
  }

  blockers(id: string): Observable<IPursuitBlocker[]> {
    return this.http.get<IPursuitBlocker[]>(`${this.apiUrl}/${id}/blockers`);
  }

  approvals(id: string): Observable<IPursuitApprovalOverview> {
    return this.http.get<IPursuitApprovalOverview>(`${this.apiUrl}/${id}/approvals`);
  }

  delegationPackage(id: string): Observable<IPursuitDelegationPackage> {
    return this.http.get<IPursuitDelegationPackage>(`${this.apiUrl}/${id}/delegation`);
  }

  private normalizeDashboard(dashboard: IPursuitDashboard | null | undefined): IPursuitDashboard {
    const source = dashboard || ({} as IPursuitDashboard);
    return {
      ...source,
      counts: source.counts || {},
      decisionQueue: source.decisionQueue || [],
      needsRobert: source.needsRobert || [],
      vaReady: source.vaReady || [],
      systemReady: source.systemReady || [],
      blocked: source.blocked || [],
      stale: source.stale || [],
      reviewDue: source.reviewDue || [],
      planningNeeded: source.planningNeeded || [],
      recentlyChanged: source.recentlyChanged || [],
      highRisk: source.highRisk || [],
      completionCandidates: source.completionCandidates || [],
    };
  }

  private normalizeDetail(detail: IPursuitDetail | null | undefined): IPursuitDetail {
    const source = detail || ({} as IPursuitDetail);
    const unavailableUsage: IPursuitResourceUsage = {
      state: 'unavailable',
      available: false,
      limitsConfigured: false,
      effortRecordedHours: 0,
      effortReservedHours: 0,
      effortCommittedHours: 0,
      effortLimitHours: 0,
      effortRemainingHours: 0,
      effortExhausted: false,
      effortExceeded: false,
      spendIncurredEur: 0,
      spendRefundedEur: 0,
      spendNetEur: 0,
      spendReservedEur: 0,
      spendCommittedEur: 0,
      spendLimitEur: 0,
      spendRemainingEur: 0,
      spendExhausted: false,
      spendExceeded: false,
      eventCount: 0,
      activeReservations: 0,
      reservations: [],
      blockingReason: 'Resource usage is not available from this backend response.',
    };
    const resourceUsage = source.resourceUsage
      ? {
          ...unavailableUsage,
          ...(source.resourceUsage as Partial<IPursuitResourceUsage>),
          blockingReason: source.resourceUsage.blockingReason || '',
          reservations: source.resourceUsage.reservations || [],
        }
      : unavailableUsage;
    return {
      ...source,
      pursuit: this.normalizePursuit(source.pursuit),
      links: source.links || [],
      activity: source.activity || [],
      workflows: source.workflows || [],
      checklistItems: source.checklistItems || [],
      openLoops: source.openLoops || [],
      proposals: source.proposals || [],
      qualityGates: source.qualityGates || [],
      decisions: source.decisions || [],
      decisionQueue: source.decisionQueue || [],
      transitions: source.transitions || [],
      sourceLinks: source.sourceLinks || [],
      events: source.events || [],
      timeline: source.timeline || [],
      evidence: source.evidence || [],
      memories: source.memories || [],
      conversations: source.conversations || [],
      ambientOpportunities: source.ambientOpportunities || [],
      taskRuns: source.taskRuns || [],
      taskAttempts: source.taskAttempts || [],
      verificationRuns: source.verificationRuns || [],
      verificationClaims: source.verificationClaims || [],
      verificationEvidence: source.verificationEvidence || [],
      automations: source.automations || [],
      runtimeAttempts: source.runtimeAttempts || [],
      sourceItems: source.sourceItems || [],
      sourceExtractions: source.sourceExtractions || [],
      nextActions: source.nextActions || [],
      blockers: source.blockers || [],
      approvalItems: source.approvalItems || [],
      actionQueues: {
        needsRobert: source.actionQueues?.needsRobert || [],
        vaReady: source.actionQueues?.vaReady || [],
        systemReady: source.actionQueues?.systemReady || [],
        waiting: source.actionQueues?.waiting || [],
      },
      resourceUsage,
    };
  }

  private normalizePursuit(pursuit: IPursuit): IPursuit {
    const source = pursuit || ({} as IPursuit);
    return {
      ...source,
      successCriteria: source.successCriteria || [],
      stopConditions: source.stopConditions || [],
      dependencies: source.dependencies || [],
      resourceLimits: source.resourceLimits || {},
      reviewCadenceDays: source.reviewCadenceDays || 0,
    };
  }
}
