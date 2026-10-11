import { HTTP_INTERCEPTORS, HttpClient, provideHttpClient, withInterceptorsFromDi } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed, fakeAsync, tick } from '@angular/core/testing';
import { TimeoutError } from 'rxjs';
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
});
