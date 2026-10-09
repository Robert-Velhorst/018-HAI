import { HTTP_INTERCEPTORS, HttpClient, provideHttpClient, withInterceptorsFromDi } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed, fakeAsync, tick } from '@angular/core/testing';
import { TimeoutError } from 'rxjs';
import { HttpTimeoutPolicy } from '../shared/http-timeout-policy';
import { RequestTimeoutInterceptor } from './request-timeout.interceptor';

describe('RequestTimeoutInterceptor', () => {
  let http: any;
  let controller: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(withInterceptorsFromDi()),
        provideHttpClientTesting(),
        { provide: HTTP_INTERCEPTORS, useClass: RequestTimeoutInterceptor, multi: true },
      ],
    });
    http = TestBed.inject(HttpClient);
    controller = TestBed.inject(HttpTestingController);
  });

  afterEach(() => controller.verify());

  it('bounds a hanging read request', fakeAsync(() => {
    let receivedError: unknown;
    http.get('/api/v1/slow').subscribe({ error: (error: unknown) => (receivedError = error) });
    controller.expectOne('/api/v1/slow');

    tick(RequestTimeoutInterceptor.readTimeoutMs + 1);

    expect(receivedError instanceof TimeoutError).toBeTrue();
  }));

  it('keeps the reviewed archive upload aligned with the gateway timeout', fakeAsync(() => {
    let receivedError: unknown;
    http.post('/api/v1/agent-runtimes/openclaw/ecosystem/upload', new FormData())
      .subscribe({ error: (error: unknown) => (receivedError = error) });
    const request = controller.expectOne('/api/v1/agent-runtimes/openclaw/ecosystem/upload');

    tick(RequestTimeoutInterceptor.operationTimeoutMs + 1);

    expect(receivedError).toBeUndefined();
    request.flush({ id: 'openclaw' });
  }));

  it('uses the long bounded timeout only for the exact source transcription route', fakeAsync(() => {
    let receivedError: unknown;
    http.post('/api/v1/sources/source-1/transcribe', null)
      .subscribe({ error: (error: unknown) => (receivedError = error) });
    const request = controller.expectOne('/api/v1/sources/source-1/transcribe');

    tick(RequestTimeoutInterceptor.operationTimeoutMs + 1);

    expect(receivedError).toBeUndefined();
    expect(request.cancelled).toBeFalse();
    request.flush({});
  }));

  it('uses the long bounded timeout only for the exact document extraction route', fakeAsync(() => {
    let receivedError: unknown;
    http.post('/api/v1/sources/source-2/extract-documents', null)
      .subscribe({ error: (error: unknown) => (receivedError = error) });
    const request = controller.expectOne('/api/v1/sources/source-2/extract-documents');

    tick(RequestTimeoutInterceptor.operationTimeoutMs + 1);

    expect(receivedError).toBeUndefined();
    expect(request.cancelled).toBeFalse();
    request.flush({});
  }));

  it('normalizes source operation paths while rejecting lookalike routes', () => {
    expect(HttpTimeoutPolicy.forRequest('POST', 'https://hai.local//api/v1/sources/source-3/transcribe/?run=1'))
      .toBe(RequestTimeoutInterceptor.sourceTranscriptionTimeoutMs);
    expect(HttpTimeoutPolicy.forRequest('POST', '/api/v1/sources/source-3/extract-documents/?run=1'))
      .toBe(RequestTimeoutInterceptor.sourceDocumentExtractionTimeoutMs);
    expect(HttpTimeoutPolicy.forRequest('POST', '/api/v1/sources/source-3/transcribe/extra'))
      .toBe(RequestTimeoutInterceptor.operationTimeoutMs);
    expect(HttpTimeoutPolicy.forRequest('POST', '/api/v1/sources/source-3/extract-documents-archive'))
      .toBe(RequestTimeoutInterceptor.operationTimeoutMs);
  });

  it('keeps unrelated mutations on the 30-second cap and does not extend non-POST methods', () => {
    expect(HttpTimeoutPolicy.forRequest('POST', '/api/v1/sources/source-3/sync'))
      .toBe(RequestTimeoutInterceptor.operationTimeoutMs);
    expect(HttpTimeoutPolicy.forRequest('PATCH', '/api/v1/sources/source-3/transcribe'))
      .toBe(RequestTimeoutInterceptor.operationTimeoutMs);
    expect(HttpTimeoutPolicy.forRequest('DELETE', '/api/v1/sources/source-3/extract-documents'))
      .toBe(RequestTimeoutInterceptor.operationTimeoutMs);
  });

  it('keeps long-running source timeouts within the backend configured maximum', () => {
    const backendMaximumMs = 10 * 60_000;
    expect(RequestTimeoutInterceptor.sourceTranscriptionTimeoutMs).toBeLessThanOrEqual(backendMaximumMs);
    expect(RequestTimeoutInterceptor.sourceDocumentExtractionTimeoutMs).toBeLessThanOrEqual(backendMaximumMs);
    expect(RequestTimeoutInterceptor.sourceTranscriptionTimeoutMs).toBeGreaterThanOrEqual(5 * 60_000);
    expect(RequestTimeoutInterceptor.sourceDocumentExtractionTimeoutMs).toBeGreaterThanOrEqual(3 * 60_000);
  });

  it('preserves the 30-second timeout and abort behavior for unrelated mutations', fakeAsync(() => {
    let receivedError: unknown;
    http.post('/api/v1/sources/source-1/sync', {}).subscribe({ error: (error: unknown) => (receivedError = error) });
    const request = controller.expectOne('/api/v1/sources/source-1/sync');

    tick(RequestTimeoutInterceptor.operationTimeoutMs + 1);

    expect(receivedError instanceof TimeoutError).toBeTrue();
    expect(request.cancelled).toBeTrue();
  }));
});
