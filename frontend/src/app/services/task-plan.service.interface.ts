import { Observable } from 'rxjs';
import {
  IApprovalDecision,
	IApprovedReviewReconciliationRequest,
	IApprovedReviewReconciliationResult,
  ICompletionPlan,
  ITaskPlanHistoryItem,
  IReviewQueueItem,
  IReviewResolutionResult,
  ITaskPlanRequest,
} from '../models/task-plan.model.interface';

export interface ITaskPlanService {
  plan(request: ITaskPlanRequest): Observable<ICompletionPlan>;
  run(request: ITaskPlanRequest): Observable<ICompletionPlan>;
  logs(limit?: number): Observable<ITaskPlanHistoryItem[]>;
  reviewQueue(): Observable<IReviewQueueItem[]>;
  resolveReviewItem(id: string, decision: IApprovalDecision): Observable<IReviewResolutionResult>;
	reconcileApprovedReviews(request: IApprovedReviewReconciliationRequest): Observable<IApprovedReviewReconciliationResult>;
}
