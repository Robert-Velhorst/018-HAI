import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { IWorkflowFrameworkSelectionDecision } from '../../models/workflow.model.interface';
import { WorkflowService } from './workflow.service';
import { provideHttpClient, withInterceptorsFromDi } from '@angular/common/http';

describe('WorkflowService framework provenance', () => {
  let service: WorkflowService;
  let http: HttpTestingController;

  const selection = {
    id: 'c595d075-5412-4e7f-bff4-1a9df360451a',
    selected: [],
  } as unknown as IWorkflowFrameworkSelectionDecision;

  beforeEach(() => {
    TestBed.configureTestingModule({ imports: [], providers: [provideHttpClient(withInterceptorsFromDi()), provideHttpClientTesting()] });
    service = TestBed.inject(WorkflowService);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => http.verify());

  it('finds an exact owner-scoped framework selection without inventing data', () => {
    let result: IWorkflowFrameworkSelectionDecision | undefined;
    service.frameworkSelection(` ${selection.id} `).subscribe((value) => (result = value));

    const request = http.expectOne((candidate) =>
      candidate.url === '/api/v1/framework-registry/selections' &&
      candidate.params.get('limit') === '200'
    );
    expect(request.request.method).toBe('GET');
    request.flush({ selections: [{ id: 'other-selection' }, selection] });

    expect(result).toBe(selection);
  });

  it('returns undefined when the exact selection is outside available registry history', () => {
    let result: IWorkflowFrameworkSelectionDecision | undefined = selection;
    service.frameworkSelection(selection.id).subscribe((value) => (result = value));

    const request = http.expectOne((candidate) =>
      candidate.url === '/api/v1/framework-registry/selections' &&
      candidate.params.get('limit') === '200'
    );
    request.flush({ selections: [] });

    expect(result).toBeUndefined();
  });

  it('rejects selector-v5 history with a missing or incompatible risk contract', () => {
    let result: IWorkflowFrameworkSelectionDecision | undefined = selection;
    service.frameworkSelection(selection.id).subscribe((value) => (result = value));

    const request = http.expectOne((candidate) =>
      candidate.url === '/api/v1/framework-registry/selections' &&
      candidate.params.get('limit') === '200'
    );
    request.flush({
      selections: [{
        ...selection,
        selectorAlgorithmVersion: 'selector-v5',
        taskRiskLevel: 'high',
        effectiveRiskCeiling: 'medium',
        selected: [{ riskCeiling: 'medium' }],
      }],
    });

    expect(result).toBeUndefined();
  });

  it('rejects selector-v5 history without a bounded autonomy and approval contract', () => {
    let result: IWorkflowFrameworkSelectionDecision | undefined = selection;
    service.frameworkSelection(selection.id).subscribe((value) => (result = value));

    const request = http.expectOne((candidate) =>
      candidate.url === '/api/v1/framework-registry/selections'
    );
    request.flush({
      selections: [{
        ...selection,
        selectorAlgorithmVersion: 'selector-v5',
        taskRiskLevel: 'low',
        effectiveRiskCeiling: 'high',
        maximumAutonomyLevel: 8,
        selected: [{ riskCeiling: 'high', maximumAutonomyLevel: 7 }],
      }],
    });

    expect(result).toBeUndefined();
  });

  it('runs only the explicitly selected workflow', () => {
    const id = '11111111-1111-4111-8111-111111111111';
    service.runOne(id).subscribe();

    const request = http.expectOne(`/api/v1/workflow/${id}/run`);
    expect(request.request.method).toBe('POST');
    expect(request.request.body).toEqual({});
    request.flush({ workflowId: id, status: 'completed', state: 'completed', attempts: 1 });
  });

  it('loads bounded read-only reminder proposals', () => {
    service.reminderProposals(48, 25).subscribe();

    const request = http.expectOne((candidate) =>
      candidate.url === '/api/v1/workflow/reminder-proposals' &&
      candidate.params.get('horizonHours') === '48' &&
      candidate.params.get('limit') === '25'
    );
    expect(request.request.method).toBe('GET');
    request.flush({
      items: [],
      due: 0,
      upcoming: 0,
      authority: 'reminder_proposal_only',
      canExecute: false,
      freshness: {
        status: 'current_internal_reminder_snapshot',
        revalidationRequired: true,
        checkedAt: '2026-08-04T10:00:00Z',
        reason: 'Revalidate before any effect.',
      },
    });
  });

  it('prepares a reminder through the internal-only activation route', () => {
    const body = {
      expectedReminderDigest: 'a'.repeat(64),
      idempotencyKey: 'ui:internal-reminder:item-1:key',
      activationKind: 'internal_notification' as const,
      confirmation: 'PREPARE INTERNAL REMINDER ONLY' as const,
    };
    const id = '11111111-1111-4111-8111-111111111111';
    service.prepareReminderActivation(id, body).subscribe();

    const request = http.expectOne(`/api/v1/workflow/reminder-proposals/${id}/activation-requests`);
    expect(request.request.method).toBe('POST');
    expect(request.request.body).toEqual(body);
    request.flush({ authority: 'reminder_activation_request_only', canExecute: false });
  });

  it('loads bounded owner-scoped activation history', () => {
    service.reminderActivationHistory(25).subscribe();

    const request = http.expectOne((candidate) =>
      candidate.url === '/api/v1/workflow/reminder-activation-requests' &&
      candidate.params.get('limit') === '25'
    );
    expect(request.request.method).toBe('GET');
    request.flush({ items: [], authority: 'reminder_activation_history_only', canExecute: false });
  });

  it('records and reads reminder decisions without an execution endpoint', () => {
    const body = {
      decision: 'approved' as const,
      reason: 'Owner reviewed the internal preparation.',
      confirmation: 'APPROVE INTERNAL REMINDER PREPARATION' as const,
      expectedActivationRequestDigest: 'b'.repeat(64),
    };
    const id = '11111111-1111-4111-8111-111111111111';
    service.decideReminderActivation(id, body).subscribe();
    const decision = http.expectOne(`/api/v1/workflow/reminder-activation-requests/${id}/decisions`);
    expect(decision.request.method).toBe('POST');
    expect(decision.request.body).toEqual(body);
    decision.flush({ authority: 'reminder_activation_decision_only', canExecute: false });

    service.reminderActivationDecisionHistory(id, 10).subscribe();
    const history = http.expectOne((candidate) =>
      candidate.url === `/api/v1/workflow/reminder-activation-requests/${id}/decisions` &&
      candidate.params.get('limit') === '10'
    );
    expect(history.request.method).toBe('GET');
    history.flush({ decisions: [], authority: 'reminder_activation_decision_only', canExecute: false });
  });

  it('authorizes one internal delivery and exposes its bounded receipt ledger', () => {
    const body = {
      expectedActivationRequestDigest: 'a'.repeat(64),
      expectedActivationDecisionDigest: 'b'.repeat(64),
      expectedReminderDigest: 'c'.repeat(64),
      idempotencyKey: 'ui:delivery:request-1',
      channel: 'in_app' as const,
      confirmation: 'AUTHORIZE ONE INTERNAL HAI REMINDER' as const,
    };
    const id = '11111111-1111-4111-8111-111111111111';
    service.authorizeReminderDelivery(id, body).subscribe();
    const authorization = http.expectOne(`/api/v1/workflow/reminder-activation-requests/${id}/delivery-authorizations`);
    expect(authorization.request.method).toBe('POST');
    expect(authorization.request.body).toEqual(body);
    authorization.flush({ authority: 'internal_reminder_delivery_authorization', deliveryAuthorized: true, canExecute: false });

    service.reminderDeliveryHistory(25).subscribe();
    const history = http.expectOne((candidate) =>
      candidate.url === '/api/v1/workflow/reminder-deliveries' && candidate.params.get('limit') === '25'
    );
    expect(history.request.method).toBe('GET');
    history.flush({ authorizations: [], attempts: [], authority: 'internal_reminder_delivery_receipt', canExecute: false });

    service.runDueReminderDeliveries({ limit: 5 }).subscribe();
    const worker = http.expectOne('/api/v1/workflow/reminder-deliveries/run-due');
    expect(worker.request.method).toBe('POST');
    expect(worker.request.body).toEqual({ limit: 5 });
    worker.flush({ checked: 0, delivered: 0, retried: 0, suppressed: 0, deadLettered: 0, results: [] });
  });
});
