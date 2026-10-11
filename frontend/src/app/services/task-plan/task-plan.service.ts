import { Injectable } from '@angular/core';
import { HttpClient, HttpParams } from '@angular/common/http';
import { Observable } from 'rxjs';
import { ITaskPlanService } from '../task-plan.service.interface';
import {
  IApprovalDecision,
	IApprovedReviewReconciliationRequest,
	IApprovedReviewReconciliationResult,
  ICompletionPlan,
  ITaskPlanHistoryItem,
  IReviewQueueItem,
  IReviewResolutionResult,
  ITaskPlanRequest,
} from '../../models/task-plan.model.interface';

@Injectable({
  providedIn: 'root',
})
export class TaskPlanService implements ITaskPlanService {
  private apiUrl = '/api/v1/task';

  constructor(private http: HttpClient) {}

  plan(request: ITaskPlanRequest): Observable<ICompletionPlan> {
	return this.http.post<ICompletionPlan>(`${this.apiUrl}/plan`, this.withOperationIdentity(request));
  }

  run(request: ITaskPlanRequest): Observable<ICompletionPlan> {
	return this.http.post<ICompletionPlan>(`${this.apiUrl}/run`, this.withOperationIdentity(request));
  }

	private withOperationIdentity(request: ITaskPlanRequest): ITaskPlanRequest {
		if (request.idempotencyKey?.trim()) {
			return { ...request, idempotencyKey: request.idempotencyKey.trim() };
		}
		return { ...request, idempotencyKey: crypto.randomUUID() };
	}

  logs(limit = 10): Observable<ITaskPlanHistoryItem[]> {
    const boundedLimit = Number.isInteger(limit) && limit > 0 && limit <= 50 ? limit : 10;
    return this.http.get<ITaskPlanHistoryItem[]>(`${this.apiUrl}/logs`, {
      params: new HttpParams().set('limit', boundedLimit),
    });
  }

  reviewQueue(): Observable<IReviewQueueItem[]> {
    return this.http.get<IReviewQueueItem[]>(`${this.apiUrl}/review-queue`);
  }

  resolveReviewItem(id: string, decision: IApprovalDecision): Observable<IReviewResolutionResult> {
    return this.http.post<IReviewResolutionResult>(`${this.apiUrl}/review-queue/${id}/resolve`, decision);
  }

	reconcileApprovedReviews(request: IApprovedReviewReconciliationRequest): Observable<IApprovedReviewReconciliationResult> {
		return this.http.post<IApprovedReviewReconciliationResult>(`${this.apiUrl}/review-queue/reconcile`, request);
	}
}
