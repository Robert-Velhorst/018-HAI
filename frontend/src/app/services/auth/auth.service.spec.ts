import { TestBed } from '@angular/core/testing';
import { HttpTestingController } from '@angular/common/http/testing';
import { provideHttpClientTesting } from '@angular/common/http/testing';

import { AuthService } from './auth.service';
import { provideHttpClient, withInterceptorsFromDi } from '@angular/common/http';

describe('AuthService', () => {
  let service: AuthService;
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({ imports: [], providers: [provideHttpClient(withInterceptorsFromDi()), provideHttpClientTesting()] });
    service = TestBed.inject(AuthService);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => http.verify());

  it('should be created', () => {
    expect(service).toBeTruthy();
  });

  it('uses the non-error session status for route-guard authentication', () => {
    let authenticated: boolean | undefined;
    service.loggedIn().subscribe((value) => authenticated = value);

    const request = http.expectOne('/api/v1/auth/session');
    expect(request.request.method).toBe('GET');
    request.flush({ authenticated: false });

    expect(authenticated).toBeFalse();
  });
});
