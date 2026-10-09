import { TestBed } from '@angular/core/testing';
import { CanActivateFn, Router } from '@angular/router';
import { NEVER, of, throwError } from 'rxjs';

import { RedirectIfLoggedGuard } from './login.guard';
import { AUTH_SERVICE_TOKEN } from '../auth.service.token';

describe('RedirectIfLoggedGuard', () => {
  const executeGuard: CanActivateFn = (...guardParameters) =>
    TestBed.runInInjectionContext(() => RedirectIfLoggedGuard(...guardParameters));

  beforeEach(() => {
    localStorage.removeItem('hai_onboarded');
    TestBed.configureTestingModule({
      providers: [
        { provide: AUTH_SERVICE_TOKEN, useValue: { loggedIn: () => of(false) } },
        { provide: Router, useValue: jasmine.createSpyObj<Router>('Router', ['createUrlTree', 'parseUrl']) },
      ],
    });
  });

  it('allows an unauthenticated user to remain on login and complete local sign-in', (done) => {
    (executeGuard({ queryParamMap: { get: () => null } } as any, { url: '/login' } as any) as any).subscribe((result: unknown) => {
      expect(result).toBeTrue();
      done();
    });
  });

  it('renders the unavailable login state after an auth outage without losing the requested route', (done) => {
    TestBed.overrideProvider(AUTH_SERVICE_TOKEN, {
      useValue: { loggedIn: () => throwError(() => new Error('connection refused')) },
    });
    const router = TestBed.inject(Router) as jasmine.SpyObj<Router>;
    const unavailableLogin = {} as any;
    router.createUrlTree.and.returnValue(unavailableLogin);

    (executeGuard({ queryParamMap: { get: (key: string) => key === 'returnUrl' ? '/control-center?tab=attention' : null } } as any, { url: '/login' } as any) as any).subscribe((result: unknown) => {
      expect(result).toBe(unavailableLogin);
      expect(router.createUrlTree).toHaveBeenCalledWith(['/login'], {
        queryParams: {
          returnUrl: '/control-center?tab=attention',
          error: 'service_unavailable',
        },
      });
      done();
    });
  });

  it('renders the marked recovery route without waiting for another authentication probe', () => {
    const loggedIn = jasmine.createSpy('loggedIn').and.returnValue(NEVER);
    TestBed.overrideProvider(AUTH_SERVICE_TOKEN, { useValue: { loggedIn } });

    const result = executeGuard({ queryParamMap: { get: (key: string) => key === 'error' ? 'service_unavailable' : null } } as any, { url: '/login?error=service_unavailable' } as any);

    expect(result).toBeTrue();
    expect(loggedIn).not.toHaveBeenCalled();
    expect((TestBed.inject(Router) as jasmine.SpyObj<Router>).createUrlTree).not.toHaveBeenCalled();
  });

  it('redirects an authenticated first-run user to onboarding and preserves a safe destination', (done) => {
    TestBed.overrideProvider(AUTH_SERVICE_TOKEN, { useValue: { loggedIn: () => of(true) } });
    const router = TestBed.inject(Router) as jasmine.SpyObj<Router>;
    const onboarding = {} as any;
    router.createUrlTree.and.returnValue(onboarding);

    (executeGuard({ queryParamMap: { get: (key: string) => key === 'returnUrl' ? '/connected-sources?source=gmail' : null } } as any, { url: '/login' } as any) as any).subscribe((result: unknown) => {
      expect(result).toBe(onboarding);
      expect(router.createUrlTree).toHaveBeenCalledWith(['/onboarding'], {
        queryParams: { returnUrl: '/connected-sources?source=gmail' },
      });
      done();
    });
  });

  it('routes an already-onboarded user to the safe requested destination', (done) => {
    localStorage.setItem('hai_onboarded', 'true');
    TestBed.overrideProvider(AUTH_SERVICE_TOKEN, { useValue: { loggedIn: () => of(true) } });
    const router = TestBed.inject(Router) as jasmine.SpyObj<Router>;
    const destination = {} as any;
    router.parseUrl.and.returnValue(destination);

    (executeGuard({ queryParamMap: { get: (key: string) => key === 'returnUrl' ? '/pursuits?filter=active' : null } } as any, { url: '/login' } as any) as any).subscribe((result: unknown) => {
      expect(result).toBe(destination);
      expect(router.parseUrl).toHaveBeenCalledWith('/pursuits?filter=active');
      done();
    });
  });

  it('rejects an external or unsafe return URL and falls back to the control center after onboarding', (done) => {
    TestBed.overrideProvider(AUTH_SERVICE_TOKEN, { useValue: { loggedIn: () => of(true) } });
    const router = TestBed.inject(Router) as jasmine.SpyObj<Router>;
    const onboarding = {} as any;
    router.createUrlTree.and.returnValue(onboarding);

    (executeGuard({ queryParamMap: { get: (key: string) => key === 'returnUrl' ? '//example.com/steal' : null } } as any, { url: '/login' } as any) as any).subscribe((result: unknown) => {
      expect(result).toBe(onboarding);
      expect(router.createUrlTree).toHaveBeenCalledWith(['/onboarding'], { queryParams: {} });
      done();
    });
  });

  it('uses the control center for an authenticated user with no requested destination after onboarding', (done) => {
    localStorage.setItem('hai_onboarded', 'true');
    TestBed.overrideProvider(AUTH_SERVICE_TOKEN, { useValue: { loggedIn: () => of(true) } });
    const router = TestBed.inject(Router) as jasmine.SpyObj<Router>;
    const destination = {} as any;
    router.parseUrl.and.returnValue(destination);

    (executeGuard({ queryParamMap: { get: () => null } } as any, { url: '/login' } as any) as any).subscribe((result: unknown) => {
      expect(result).toBe(destination);
      expect(router.parseUrl).toHaveBeenCalledWith('/control-center');
      done();
    });
  });
});
