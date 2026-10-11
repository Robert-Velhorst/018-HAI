import { HttpClientTestingModule, HttpTestingController } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { TaskPlanService } from './task-plan.service';

describe('TaskPlanService', () => {
	let service: TaskPlanService;
	let http: HttpTestingController;

	beforeEach(() => {
		TestBed.configureTestingModule({ imports: [HttpClientTestingModule] });
		service = TestBed.inject(TaskPlanService);
		http = TestBed.inject(HttpTestingController);
	});

	afterEach(() => http.verify());

	it('binds one durable operation identity to each plan request', () => {
		service.plan({ request: 'Plan the source review' }).subscribe();
		const call = http.expectOne('/api/v1/task/plan');
		expect(call.request.method).toBe('POST');
		expect(call.request.body.idempotencyKey).toMatch(/^[0-9a-f-]{36}$/);
		call.flush({});
	});

	it('preserves a caller operation identity for transport retries', () => {
		service.run({ request: 'Run the reviewed task', idempotencyKey: 'workflow:retry-1' }).subscribe();
		const call = http.expectOne('/api/v1/task/run');
		expect(call.request.body.idempotencyKey).toBe('workflow:retry-1');
		call.flush({});
	});

	it('loads a bounded recent task history by default', () => {
		service.logs().subscribe();

		const call = http.expectOne((request) =>
			request.url === '/api/v1/task/logs' && request.params.get('limit') === '10'
		);
		expect(call.request.method).toBe('GET');
		call.flush([]);
	});

	it('posts approved-task recovery to the owner-scoped reconciliation route', () => {
		const request = { apply: false, olderThanMinutes: 30, limit: 50 };
		service.reconcileApprovedReviews(request).subscribe();

		const call = http.expectOne('/api/v1/task/review-queue/reconcile');
		expect(call.request.method).toBe('POST');
		expect(call.request.body).toEqual(request);
		call.flush({
			dryRun: true,
			cutoff: '2026-08-04T00:00:00Z',
			inspected: 0,
			approvedFound: 0,
			eligible: 0,
			completed: 0,
			returnedToReview: 0,
			conflicts: 0,
			items: [],
		});
	});
});
