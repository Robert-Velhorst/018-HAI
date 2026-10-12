import { provideHttpClient } from '@angular/common/http';
import { provideHttpClientTesting, HttpTestingController } from '@angular/common/http/testing';
import { fakeAsync, TestBed, tick } from '@angular/core/testing';

import { HttpTimeoutPolicy } from '../../shared/http-timeout-policy';
import { AuthService } from './auth.service';

describe('AuthService', () => {
  let service: AuthService;
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting(), AuthService],
    });
    service = TestBed.inject(AuthService);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => http.verify());

  it('uses the shared bounded read timeout for session checks', () => {
    expect(AuthService.authenticationCheckTimeoutMs).toBe(HttpTimeoutPolicy.readMs);
  });

  it('accepts the empty successful response returned by password login', () => {
    let completed = false;
    service.login('operator@example.test', 'synthetic-password').subscribe(() => completed = true);

    const request = http.expectOne('/api/v1/auth/login');
    expect(request.request.method).toBe('POST');
    expect(request.request.body).toEqual({
      email: 'operator@example.test',
      password: 'synthetic-password',
      rememberMe: true,
    });
    request.flush(null);

    expect(completed).toBeTrue();
  });

  it('accepts a valid session response that arrives after the former 2.5 second cutoff', fakeAsync(() => {
    let authenticated: boolean | undefined;
    service.loggedIn().subscribe((value) => authenticated = value);

    const request = http.expectOne('/api/v1/auth/is-user-authenticated');
    tick(3_000);
    request.flush({ authenticated: true });

    expect(authenticated).toBeTrue();
  }));
});
