import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { LLMPolicyService } from './llm-policy.service';
import { provideHttpClient, withInterceptorsFromDi } from '@angular/common/http';

describe('LLMPolicyService', () => {
  let service: LLMPolicyService;
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({ imports: [], providers: [provideHttpClient(withInterceptorsFromDi()), provideHttpClientTesting()] });
    service = TestBed.inject(LLMPolicyService);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => http.verify());

  it('loads the redacted generation and validation history with a bounded limit', () => {
    service.getGenerationHistory(15).subscribe((history) => {
      expect(history.length).toBe(1);
      expect(history[0].validationStatus).toBe('test_passed');
      expect(history[0].telemetryId).toBe('llm-generation:generation-1');
    });

    const request = http.expectOne(
      (candidate) => candidate.url === '/api/v1/llm/generations' && candidate.params.get('limit') === '15'
    );
    expect(request.request.method).toBe('GET');
    request.flush([
      {
        generationId: 'generation-1',
        telemetryId: 'llm-generation:generation-1',
        providerId: 'ollama',
        modelId: 'qwen-coder',
        validationStatus: 'test_passed',
        validationMethod: 'task_success_criteria_v1',
      },
    ]);
  });
});
