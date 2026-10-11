import { Injectable } from '@angular/core';
import { HttpClient, HttpParams } from '@angular/common/http';
import { Observable, catchError, map, of, throwError } from 'rxjs';
import { IWorkflowService } from '../workflow.service.interface';
import {
  IWorkflowApprovalResolutionRequest,
  IWorkflowChecklistUpdateRequest,
  IWorkflowClaimRecoverySummary,
  IWorkflowDashboard,
  IWorkflowFrameworkSelectionDecision,
  IWorkflowIntakeRequest,
  IWorkflowInterruptedExecutionResolutionRequest,
  IWorkflowItem,
  IWorkflowOpenLoopRunSummary,
  IWorkflowOverview,
  IWorkflowProposalResolutionRequest,
  IWorkflowRecord,
  IWorkflowReminderProposalSnapshot,
  IWorkflowReminderActivationPrepareRequest,
  IWorkflowReminderActivationRequestResult,
  IWorkflowReminderActivationDecisionRequest,
  IWorkflowReminderActivationDecisionResult,
  IWorkflowReminderActivationHistorySnapshot,
  IWorkflowReminderActivationDecisionHistory,
  IWorkflowReminderDeliveryAuthorizeRequest,
  IWorkflowReminderDeliveryAuthorizationResult,
  IWorkflowReminderDeliveryHistory,
  IWorkflowReminderDeliveryRunSummary,
  IWorkflowRunDueRequest,
  IWorkflowRunResult,
  IWorkflowRunSummary,
  IWorkflowTransitionRequest,
} from '../../models/workflow.model.interface';

@Injectable({
  providedIn: 'root',
})
export class WorkflowService implements IWorkflowService {
  private apiUrl = '/api/v1/workflow';

  constructor(private http: HttpClient) {}

  overview(): Observable<IWorkflowOverview> {
    return this.http.get<IWorkflowOverview>(`${this.apiUrl}/overview`);
  }

  dashboard(): Observable<IWorkflowDashboard> {
    return this.http.get<IWorkflowDashboard>(`${this.apiUrl}/dashboard`);
  }

  items(includeArchived: boolean): Observable<IWorkflowItem[]> {
    return this.http.get<IWorkflowItem[]>(`${this.apiUrl}/`, {
      params: new HttpParams().set('includeArchived', includeArchived),
    });
  }

  approvals(): Observable<IWorkflowItem[]> {
    return this.http.get<IWorkflowItem[]>(`${this.apiUrl}/approvals`);
  }

  intake(request: IWorkflowIntakeRequest): Observable<IWorkflowRecord> {
    return this.http.post<IWorkflowRecord>(`${this.apiUrl}/intake`, request);
  }

  get(id: string): Observable<IWorkflowRecord> {
    return this.http.get<IWorkflowRecord>(`${this.apiUrl}/${id}`);
  }

  frameworkSelection(
    selectionDecisionId: string
  ): Observable<IWorkflowFrameworkSelectionDecision | undefined> {
    const expectedId = selectionDecisionId.trim();
    return this.http.get<IWorkflowFrameworkSelectionDecision>(
      `/api/v1/framework-registry/selections/${encodeURIComponent(expectedId)}`
    ).pipe(
      map((selection) => {
        return selection.id === expectedId && this.hasValidFrameworkRiskContract(selection)
          ? selection
          : undefined;
      }),
      catchError((error) => error?.status === 404 ? of(undefined) : throwError(() => error))
    );
  }

  reminderProposals(horizonHours = 168, limit = 100): Observable<IWorkflowReminderProposalSnapshot> {
    return this.http.get<IWorkflowReminderProposalSnapshot>(`${this.apiUrl}/reminder-proposals`, {
      params: new HttpParams()
        .set('horizonHours', horizonHours)
        .set('limit', limit),
    });
  }

  prepareReminderActivation(
    itemId: string,
    request: IWorkflowReminderActivationPrepareRequest
  ): Observable<IWorkflowReminderActivationRequestResult> {
    return this.http.post<IWorkflowReminderActivationRequestResult>(
      `${this.apiUrl}/reminder-proposals/${itemId}/activation-requests`,
      request
    );
  }

  reminderActivationHistory(limit = 50): Observable<IWorkflowReminderActivationHistorySnapshot> {
    return this.http.get<IWorkflowReminderActivationHistorySnapshot>(
      `${this.apiUrl}/reminder-activation-requests`,
      { params: new HttpParams().set('limit', limit) }
    );
  }

  decideReminderActivation(
    requestId: string,
    request: IWorkflowReminderActivationDecisionRequest
  ): Observable<IWorkflowReminderActivationDecisionResult> {
    return this.http.post<IWorkflowReminderActivationDecisionResult>(
      `${this.apiUrl}/reminder-activation-requests/${requestId}/decisions`,
      request
    );
  }

  reminderActivationDecisionHistory(
    requestId: string,
    limit = 50
  ): Observable<IWorkflowReminderActivationDecisionHistory> {
    return this.http.get<IWorkflowReminderActivationDecisionHistory>(
      `${this.apiUrl}/reminder-activation-requests/${requestId}/decisions`,
      { params: new HttpParams().set('limit', limit) }
    );
  }

  authorizeReminderDelivery(
    requestId: string,
    request: IWorkflowReminderDeliveryAuthorizeRequest
  ): Observable<IWorkflowReminderDeliveryAuthorizationResult> {
    return this.http.post<IWorkflowReminderDeliveryAuthorizationResult>(
      `${this.apiUrl}/reminder-activation-requests/${requestId}/delivery-authorizations`,
      request
    );
  }

  reminderDeliveryHistory(limit = 50): Observable<IWorkflowReminderDeliveryHistory> {
    return this.http.get<IWorkflowReminderDeliveryHistory>(`${this.apiUrl}/reminder-deliveries`, {
      params: new HttpParams().set('limit', limit),
    });
  }

  runDueReminderDeliveries(request: IWorkflowRunDueRequest): Observable<IWorkflowReminderDeliveryRunSummary> {
    return this.http.post<IWorkflowReminderDeliveryRunSummary>(`${this.apiUrl}/reminder-deliveries/run-due`, request);
  }

  private hasValidFrameworkRiskContract(
    selection: IWorkflowFrameworkSelectionDecision
  ): boolean {
    if (selection.selectorAlgorithmVersion !== 'selector-v5') {
      return true;
    }
    const rank: Record<string, number> = { low: 1, medium: 2, high: 3 };
    const taskRank = rank[selection.taskRiskLevel ?? ''];
    const ceilingRank = rank[selection.effectiveRiskCeiling ?? ''];
    if (!taskRank || !ceilingRank || taskRank > ceilingRank) {
      return false;
    }
    if (!Number.isInteger(selection.maximumAutonomyLevel) ||
        selection.maximumAutonomyLevel < 0 || selection.maximumAutonomyLevel > 10 ||
        typeof selection.requiresApproval !== 'boolean') {
      return false;
    }
    return selection.selected.length > 0 && selection.selected.every((framework) => {
      const frameworkRank = rank[framework.riskCeiling ?? ''];
      return Boolean(frameworkRank) && frameworkRank >= taskRank &&
        framework.maximumAutonomyLevel >= selection.maximumAutonomyLevel;
    });
  }

  transition(id: string, request: IWorkflowTransitionRequest): Observable<IWorkflowRecord> {
    return this.http.post<IWorkflowRecord>(`${this.apiUrl}/${id}/transition`, request);
  }

  resolveApproval(id: string, request: IWorkflowApprovalResolutionRequest): Observable<IWorkflowRecord> {
    return this.http.post<IWorkflowRecord>(`${this.apiUrl}/${id}/approval`, request);
  }

  resolveInterruptedExecution(
    id: string,
    request: IWorkflowInterruptedExecutionResolutionRequest
  ): Observable<IWorkflowRecord> {
    return this.http.post<IWorkflowRecord>(`${this.apiUrl}/${id}/interruption/resolve`, request);
  }

  resolveProposal(
    id: string,
    proposalId: string,
    request: IWorkflowProposalResolutionRequest
  ): Observable<IWorkflowRecord> {
    return this.http.post<IWorkflowRecord>(`${this.apiUrl}/${id}/proposals/${proposalId}/resolve`, request);
  }

  updateChecklistItem(
    id: string,
    itemId: string,
    request: IWorkflowChecklistUpdateRequest
  ): Observable<IWorkflowRecord> {
    return this.http.patch<IWorkflowRecord>(`${this.apiUrl}/${id}/checklist/${itemId}`, request);
  }

  recoverStaleClaims(request: IWorkflowRunDueRequest): Observable<IWorkflowClaimRecoverySummary> {
    return this.http.post<IWorkflowClaimRecoverySummary>(`${this.apiUrl}/recover-stale`, request);
  }

  runDue(request: IWorkflowRunDueRequest): Observable<IWorkflowRunSummary> {
    return this.http.post<IWorkflowRunSummary>(`${this.apiUrl}/run-due`, request);
  }

  runOne(id: string): Observable<IWorkflowRunResult> {
    return this.http.post<IWorkflowRunResult>(`${this.apiUrl}/${id}/run`, {});
  }

  runDueOpenLoops(request: IWorkflowRunDueRequest): Observable<IWorkflowOpenLoopRunSummary> {
    return this.http.post<IWorkflowOpenLoopRunSummary>(`${this.apiUrl}/open-loops/run-due`, request);
  }
}
