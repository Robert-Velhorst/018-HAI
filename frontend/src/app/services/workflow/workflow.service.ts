import { Injectable } from '@angular/core';
import { HttpClient, HttpParams } from '@angular/common/http';
import { Observable, map, throwError } from 'rxjs';
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

type WorkflowFrameworkSelectionListResponse =
  | IWorkflowFrameworkSelectionDecision[]
  | { selections: IWorkflowFrameworkSelectionDecision[] };

@Injectable({
  providedIn: 'root',
})
export class WorkflowService implements IWorkflowService {
  private apiUrl = '/api/v1/workflow';

  constructor(private http: HttpClient) {}

  private validReference(value: unknown): value is string {
    return typeof value === 'string' && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(value) &&
      value !== '00000000-0000-0000-0000-000000000000';
  }

  private invalidRequest<T>(): Observable<T> {
    return throwError(() => new Error('Invalid workflow request. Refresh the record and check the supported fields.'));
  }

  private validReminderDigest(value: unknown): value is string {
    return typeof value === 'string' && /^[0-9a-f]{64}$/.test(value);
  }

  private validReminderKey(value: unknown): value is string {
    return typeof value === 'string' && /^[A-Za-z0-9._:-]{1,160}$/.test(value);
  }

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
    if (!this.validReference(id)) return this.invalidRequest();
    return this.http.get<IWorkflowRecord>(`${this.apiUrl}/${id}`);
  }

  frameworkSelection(
    selectionDecisionId: string
  ): Observable<IWorkflowFrameworkSelectionDecision | undefined> {
    const expectedId = typeof selectionDecisionId === 'string' ? selectionDecisionId.trim() : '';
    if (!this.validReference(expectedId)) return this.invalidRequest();
    return this.http.get<WorkflowFrameworkSelectionListResponse>(
      '/api/v1/framework-registry/selections',
      { params: new HttpParams().set('limit', 200) }
    ).pipe(
      map((response) => {
        const selections = Array.isArray(response) ? response : response.selections ?? [];
        const selection = selections.find((item) => item.id === expectedId);
        return selection && this.hasValidFrameworkRiskContract(selection)
          ? selection
          : undefined;
      })
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
    if (!this.validReference(itemId) || !request || Array.isArray(request) ||
        !this.validReminderDigest(request.expectedReminderDigest) || !this.validReminderKey(request.idempotencyKey) ||
        request.activationKind !== 'internal_notification' || request.confirmation !== 'PREPARE INTERNAL REMINDER ONLY') return this.invalidRequest();
    return this.http.post<IWorkflowReminderActivationRequestResult>(
      `${this.apiUrl}/reminder-proposals/${itemId}/activation-requests`,
      { expectedReminderDigest: request.expectedReminderDigest, idempotencyKey: request.idempotencyKey,
        activationKind: request.activationKind, confirmation: request.confirmation }
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
    const confirmations = { approved: 'APPROVE INTERNAL REMINDER PREPARATION', rejected: 'REJECT INTERNAL REMINDER PREPARATION',
      needs_clarification: 'REQUEST REMINDER CLARIFICATION', revoked: 'REVOKE INTERNAL REMINDER PREPARATION' };
    if (!this.validReference(requestId) || !request || Array.isArray(request) ||
        !Object.prototype.hasOwnProperty.call(confirmations, request.decision) || request.confirmation !== confirmations[request.decision] ||
        typeof request.reason !== 'string' || !request.reason.trim() || Array.from(request.reason.trim()).length > 2000 ||
        !this.validReminderDigest(request.expectedActivationRequestDigest) ||
        (request.expectedPreviousDecisionId !== undefined && !this.validReference(request.expectedPreviousDecisionId))) return this.invalidRequest();
    const body: IWorkflowReminderActivationDecisionRequest = { decision: request.decision, reason: request.reason,
      confirmation: request.confirmation, expectedActivationRequestDigest: request.expectedActivationRequestDigest };
    if (request.expectedPreviousDecisionId !== undefined) body.expectedPreviousDecisionId = request.expectedPreviousDecisionId;
    return this.http.post<IWorkflowReminderActivationDecisionResult>(
      `${this.apiUrl}/reminder-activation-requests/${requestId}/decisions`,
      body
    );
  }

  reminderActivationDecisionHistory(
    requestId: string,
    limit = 50
  ): Observable<IWorkflowReminderActivationDecisionHistory> {
    if (!this.validReference(requestId)) return this.invalidRequest();
    return this.http.get<IWorkflowReminderActivationDecisionHistory>(
      `${this.apiUrl}/reminder-activation-requests/${requestId}/decisions`,
      { params: new HttpParams().set('limit', limit) }
    );
  }

  authorizeReminderDelivery(
    requestId: string,
    request: IWorkflowReminderDeliveryAuthorizeRequest
  ): Observable<IWorkflowReminderDeliveryAuthorizationResult> {
    if (!this.validReference(requestId) || !request || Array.isArray(request) ||
        ![request.expectedActivationRequestDigest, request.expectedActivationDecisionDigest, request.expectedReminderDigest]
          .every(value => this.validReminderDigest(value)) || !this.validReminderKey(request.idempotencyKey) ||
        request.channel !== 'in_app' || request.confirmation !== 'AUTHORIZE ONE INTERNAL HAI REMINDER') return this.invalidRequest();
    return this.http.post<IWorkflowReminderDeliveryAuthorizationResult>(
      `${this.apiUrl}/reminder-activation-requests/${requestId}/delivery-authorizations`,
      { expectedActivationRequestDigest: request.expectedActivationRequestDigest, expectedActivationDecisionDigest: request.expectedActivationDecisionDigest,
        expectedReminderDigest: request.expectedReminderDigest, idempotencyKey: request.idempotencyKey,
        channel: request.channel, confirmation: request.confirmation }
    );
  }

  reminderDeliveryHistory(limit = 50): Observable<IWorkflowReminderDeliveryHistory> {
    return this.http.get<IWorkflowReminderDeliveryHistory>(`${this.apiUrl}/reminder-deliveries`, {
      params: new HttpParams().set('limit', limit),
    });
  }

  runDueReminderDeliveries(request: IWorkflowRunDueRequest): Observable<IWorkflowReminderDeliveryRunSummary> {
    const body = this.batchRequest(request, 100);
    if (!body) return this.invalidRequest();
    return this.http.post<IWorkflowReminderDeliveryRunSummary>(`${this.apiUrl}/reminder-deliveries/run-due`, body);
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
    if (!this.validReference(id) || !request || typeof request.targetState !== 'string' ||
        !/^[a-z][a-z0-9_]{0,79}$/.test(request.targetState) ||
        (request.message !== undefined && typeof request.message !== 'string') ||
        (request.actor !== undefined && typeof request.actor !== 'string')) return this.invalidRequest();
    const body: IWorkflowTransitionRequest = { targetState: request.targetState };
    if (request.message !== undefined) body.message = request.message;
    if (request.actor !== undefined) body.actor = request.actor;
    return this.http.post<IWorkflowRecord>(`${this.apiUrl}/${id}/transition`, body);
  }

  resolveApproval(id: string, request: IWorkflowApprovalResolutionRequest): Observable<IWorkflowRecord> {
    if (!this.validReference(id) || !request || typeof request.approved !== 'boolean' ||
        (request.note !== undefined && typeof request.note !== 'string') ||
        (request.actor !== undefined && typeof request.actor !== 'string')) return this.invalidRequest();
    const body: IWorkflowApprovalResolutionRequest = { approved: request.approved };
    if (request.note !== undefined) body.note = request.note;
    if (request.actor !== undefined) body.actor = request.actor;
    return this.http.post<IWorkflowRecord>(`${this.apiUrl}/${id}/approval`, body);
  }

  resolveInterruptedExecution(
    id: string,
    request: IWorkflowInterruptedExecutionResolutionRequest
  ): Observable<IWorkflowRecord> {
    if (!this.validReference(id) || !request || !['retry', 'confirm_completed', 'keep_blocked'].includes(request.decision) ||
        typeof request.note !== 'string' || !request.note.trim() ||
        (request.evidenceUri !== undefined && typeof request.evidenceUri !== 'string') ||
        (request.evidenceLabel !== undefined && typeof request.evidenceLabel !== 'string') ||
        (request.actor !== undefined && typeof request.actor !== 'string') ||
        (request.priorExecutionReconciled !== undefined && typeof request.priorExecutionReconciled !== 'boolean') ||
        (request.decision !== 'keep_blocked' && (request.priorExecutionReconciled !== true || !request.evidenceUri?.trim()))) return this.invalidRequest();
    const body: IWorkflowInterruptedExecutionResolutionRequest = { decision: request.decision, note: request.note };
    if (request.evidenceUri !== undefined) body.evidenceUri = request.evidenceUri;
    if (request.evidenceLabel !== undefined) body.evidenceLabel = request.evidenceLabel;
    if (request.actor !== undefined) body.actor = request.actor;
    if (request.priorExecutionReconciled !== undefined) body.priorExecutionReconciled = request.priorExecutionReconciled;
    return this.http.post<IWorkflowRecord>(`${this.apiUrl}/${id}/interruption/resolve`, body);
  }

  resolveProposal(
    id: string,
    proposalId: string,
    request: IWorkflowProposalResolutionRequest
  ): Observable<IWorkflowRecord> {
    if (!this.validReference(id) || !this.validReference(proposalId) || !request ||
        (request.status === undefined && typeof request.approved !== 'boolean') ||
        (request.status !== undefined && !['approved', 'rejected', 'changes_requested'].includes(request.status)) ||
        (request.approved !== undefined && typeof request.approved !== 'boolean') ||
        (request.status !== undefined && request.approved !== undefined && request.approved !== (request.status === 'approved')) ||
        (request.selectedOption !== undefined && typeof request.selectedOption !== 'string') ||
        (request.note !== undefined && typeof request.note !== 'string') ||
        (request.actor !== undefined && typeof request.actor !== 'string')) return this.invalidRequest();
    const body: IWorkflowProposalResolutionRequest = {};
    if (request.status !== undefined) body.status = request.status;
    if (request.approved !== undefined) body.approved = request.approved;
    if (request.selectedOption !== undefined) body.selectedOption = request.selectedOption;
    if (request.note !== undefined) body.note = request.note;
    if (request.actor !== undefined) body.actor = request.actor;
    return this.http.post<IWorkflowRecord>(`${this.apiUrl}/${id}/proposals/${proposalId}/resolve`, body);
  }

  updateChecklistItem(
    id: string,
    itemId: string,
    request: IWorkflowChecklistUpdateRequest
  ): Observable<IWorkflowRecord> {
    if (!this.validReference(id) || !this.validReference(itemId) || !request ||
        !['open', 'done', 'blocked'].includes(request.status) ||
        (request.note !== undefined && typeof request.note !== 'string') ||
        (request.actor !== undefined && typeof request.actor !== 'string')) return this.invalidRequest();
    const body: IWorkflowChecklistUpdateRequest = { status: request.status };
    if (request.note !== undefined) body.note = request.note;
    if (request.actor !== undefined) body.actor = request.actor;
    return this.http.patch<IWorkflowRecord>(`${this.apiUrl}/${id}/checklist/${itemId}`, body);
  }

  recoverStaleClaims(request: IWorkflowRunDueRequest): Observable<IWorkflowClaimRecoverySummary> {
    const body = this.batchRequest(request);
    if (!body) return this.invalidRequest();
    return this.http.post<IWorkflowClaimRecoverySummary>(`${this.apiUrl}/recover-stale`, body);
  }

  runDue(request: IWorkflowRunDueRequest): Observable<IWorkflowRunSummary> {
    const body = this.batchRequest(request);
    if (!body) return this.invalidRequest();
    return this.http.post<IWorkflowRunSummary>(`${this.apiUrl}/run-due`, body);
  }

  runOne(id: string): Observable<IWorkflowRunResult> {
    if (!this.validReference(id)) return this.invalidRequest();
    return this.http.post<IWorkflowRunResult>(`${this.apiUrl}/${id}/run`, {});
  }

  runDueOpenLoops(request: IWorkflowRunDueRequest): Observable<IWorkflowOpenLoopRunSummary> {
    const body = this.batchRequest(request);
    if (!body) return this.invalidRequest();
    return this.http.post<IWorkflowOpenLoopRunSummary>(`${this.apiUrl}/open-loops/run-due`, body);
  }

  private batchRequest(request: IWorkflowRunDueRequest, maximum = 50): IWorkflowRunDueRequest | undefined {
    if (!request || typeof request !== 'object' || Array.isArray(request)) return undefined;
    const limit = request.limit === undefined ? 10 : request.limit;
    if (!Number.isSafeInteger(limit) || limit < 1 || limit > maximum) return undefined;
    return { limit };
  }
}
