import { TestBed } from '@angular/core/testing';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { HttpClient, provideHttpClient, withInterceptorsFromDi } from '@angular/common/http';

import { AutomationsService } from './automations.service';
import { IAutomationLaunchResult } from '../../models/automation.model.interface';

describe('AutomationsService', () => {
  let service: AutomationsService;
  let http: HttpTestingController;

  beforeEach(() => {
    window.localStorage.clear();
    TestBed.configureTestingModule({ imports: [], providers: [provideHttpClient(withInterceptorsFromDi()), provideHttpClientTesting()] });
    service = TestBed.inject(AutomationsService);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => http.verify());

  it('should be created', () => {
    expect(service).toBeTruthy();
  });

  it('sends the OpenClaw model selection when registering an automation', () => {
    const automation = { name: 'Draft', host: 'localhost', port: 80, position: 0, image: '',
      removeImage: false, runtimeType: 'openclaw', launchType: 'agent_runtime',
      runtimeModel: 'ollama/qwen3:14b' };
    service.addAutomation(automation).subscribe();
    const request = http.expectOne('/api/v1/automation/');
    expect((request.request.body as FormData).get('runtimeModel')).toBe('ollama/qwen3:14b');
    request.flush(automation);
  });

  it('uses PATCH to reorder shared automation configuration', () => {
    service.swapAutomations('first', 'second').subscribe();

    const request = http.expectOne('/api/v1/automation/swap/first/second');
    expect(request.request.method).toBe('PATCH');
    expect(request.request.body).toEqual({});
    request.flush(null);
  });

  it('persists a launch key, sends it to the backend, and clears it after a definitive result', () => {
    service.launchAutomation('automation-1').subscribe();

    const first = http.expectOne('/api/v1/automation/automation-1/launch');
    const firstKey = first.request.headers.get('Idempotency-Key');
    expect(firstKey).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i);
    expect(window.localStorage.getItem('hai.automation-launch.idempotency.v1.automation-1')).toBe(firstKey);
    first.flush(launchResult('completed', 'automation-1'));
    expect(window.localStorage.getItem('hai.automation-launch.idempotency.v1.automation-1')).toBeNull();

    service.launchAutomation('automation-1').subscribe();
    const second = http.expectOne('/api/v1/automation/automation-1/launch');
    expect(second.request.headers.get('Idempotency-Key')).not.toBe(firstKey);
    second.flush(launchResult('completed', 'automation-1'));
  });

  it('retains and reuses the key after an indeterminate result, including across service instances', () => {
    service.launchAutomation('automation-2').subscribe();
    const first = http.expectOne('/api/v1/automation/automation-2/launch');
    const key = first.request.headers.get('Idempotency-Key');
    first.flush(launchResult('indeterminate', 'automation-2'));
    expect(window.localStorage.getItem('hai.automation-launch.idempotency.v1.automation-2')).toBe(key);

    const restartedService = new AutomationsService(TestBed.inject(HttpClient));
    restartedService.launchAutomation('automation-2').subscribe();
    const retry = http.expectOne('/api/v1/automation/automation-2/launch');
    expect(retry.request.headers.get('Idempotency-Key')).toBe(key);
    retry.flush(launchResult('completed', 'automation-2'));
    expect(window.localStorage.getItem('hai.automation-launch.idempotency.v1.automation-2')).toBeNull();
  });

  it('retains the same launch key after a transport error', () => {
    service.launchAutomation('automation-3').subscribe({ error: () => undefined });
    const first = http.expectOne('/api/v1/automation/automation-3/launch');
    const key = first.request.headers.get('Idempotency-Key');
    first.error(new ProgressEvent('network error'));

    service.launchAutomation('automation-3').subscribe();
    const retry = http.expectOne('/api/v1/automation/automation-3/launch');
    expect(retry.request.headers.get('Idempotency-Key')).toBe(key);
    retry.flush(launchResult('completed', 'automation-3'));
  });

  it('does not send a launch if the browser cannot persist its idempotency key', () => {
    spyOn(window.localStorage, 'setItem').and.throwError('storage unavailable');
    let launchError: Error | undefined;

    service.launchAutomation('automation-4').subscribe({ error: error => launchError = error });

    expect(launchError?.name).toBe('AutomationLaunchSafetyError');
    expect(http.match('/api/v1/automation/automation-4/launch')).toHaveSize(0);
  });
});

function launchResult(status: string, automationId: string): IAutomationLaunchResult {
  return {
    automationId,
    launchEventId: '22222222-2222-4222-8222-222222222222',
    launchType: 'agent_runtime',
    target: 'default',
    status,
    exitCode: 0,
    durationMs: 1,
    requiresApproval: false,
    auditEvents: [],
    launchedAt: new Date().toISOString(),
  };
}
