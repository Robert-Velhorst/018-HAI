import { provideHttpClient } from '@angular/common/http';
import { provideHttpClientTesting, HttpTestingController } from '@angular/common/http/testing';
import { fakeAsync, TestBed, tick } from '@angular/core/testing';
import { HttpErrorResponse } from '@angular/common/http';
import { TimeoutError } from 'rxjs';

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

  it('preserves an explicit non-persistent login preference', () => {
    service.login('operator@example.test', 'synthetic-password', false).subscribe();
    const request = http.expectOne('/api/v1/auth/login');
    expect(request.request.method).toBe('POST');
    expect(request.request.body).toEqual({
      email: 'operator@example.test', password: 'synthetic-password', rememberMe: false,
    });
    request.flush(null);
  });

  it('cancels a capabilities request at its bounded deadline', fakeAsync(() => {
    let receivedError: unknown;
    service.getCapabilities().subscribe({ error: error => receivedError = error });
    const request = http.expectOne('/api/v1/auth/capabilities');
    tick(AuthService.capabilitiesTimeoutMs + 1);
    expect(receivedError).toEqual(jasmine.any(TimeoutError));
    expect(request.cancelled).toBeTrue();
  }));

  it('keeps a session timeout distinguishable from an unauthenticated response', fakeAsync(() => {
    let receivedError: unknown;
    const next = jasmine.createSpy('authenticated');
    service.loggedIn().subscribe({ next, error: error => receivedError = error });
    const request = http.expectOne('/api/v1/auth/is-user-authenticated');
    tick(AuthService.authenticationCheckTimeoutMs + 1);
    expect(receivedError).toEqual(jasmine.any(TimeoutError));
    expect(next).not.toHaveBeenCalled();
    expect(request.cancelled).toBeTrue();
  }));

  it('returns false only for an unauthenticated session response', () => {
    const next = jasmine.createSpy('authenticated');
    service.loggedIn().subscribe(next);
    http.expectOne('/api/v1/auth/is-user-authenticated').flush(null, {
      status: 401, statusText: 'Unauthorized',
    });
    expect(next).toHaveBeenCalledOnceWith(false);
  });

  it('propagates an unavailable session service instead of reporting a logged-out user', () => {
    const next = jasmine.createSpy('authenticated');
    let receivedError: unknown;
    service.loggedIn().subscribe({ next, error: error => receivedError = error });
    http.expectOne('/api/v1/auth/is-user-authenticated').flush(null, {
      status: 503, statusText: 'Service unavailable',
    });
    expect(next).not.toHaveBeenCalled();
    expect(receivedError).toEqual(jasmine.any(HttpErrorResponse));
    expect((receivedError as HttpErrorResponse).status).toBe(503);
  });

  it('sends the reset bearer in the body rather than the URL', () => {
    service.confirmPasswordReset('synthetic-reset-bearer', 'synthetic-new-password').subscribe();
    const request = http.expectOne('/api/v1/auth/confirm-password-reset');
    expect(request.request.method).toBe('POST');
    expect(request.request.body).toEqual({
      token: 'synthetic-reset-bearer', newPassword: 'synthetic-new-password',
    });
    expect(request.request.urlWithParams).not.toContain('synthetic-reset-bearer');
    request.flush(null);
  });

  it('uses a state-changing POST rather than GET for logout', () => {
    service.logout().subscribe();
    const request = http.expectOne('/api/v1/auth/logout');
    expect(request.request.method).toBe('POST');
    expect(request.request.body).toEqual({});
    request.flush(null);
  });
});
