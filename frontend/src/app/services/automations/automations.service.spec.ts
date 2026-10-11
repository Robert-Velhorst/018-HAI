import { TestBed } from '@angular/core/testing';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';

import { AutomationsService } from './automations.service';
import { provideHttpClient, withInterceptorsFromDi } from '@angular/common/http';

describe('AutomationsService', () => {
  let service: AutomationsService;
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({ imports: [], providers: [provideHttpClient(withInterceptorsFromDi()), provideHttpClientTesting()] });
    service = TestBed.inject(AutomationsService);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => http.verify());

  it('should be created', () => {
    expect(service).toBeTruthy();
  });

  it('uses PATCH to reorder shared automation configuration', () => {
    service.swapAutomations('first', 'second').subscribe();

    const request = http.expectOne('/api/v1/automation/swap/first/second');
    expect(request.request.method).toBe('PATCH');
    expect(request.request.body).toEqual({});
    request.flush(null);
  });
});
