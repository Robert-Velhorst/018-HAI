import { TestBed } from '@angular/core/testing';
import { CanActivateFn, Router } from '@angular/router';
import { of, throwError } from 'rxjs';

import { authGuard } from './auth.guard';
import { AUTH_SERVICE_TOKEN } from '../auth.service.token';

describe('authGuard', () => {
  const executeGuard: CanActivateFn = (...guardParameters) => 
      TestBed.runInInjectionContext(() => authGuard(...guardParameters));

  beforeEach(() => {
    localStorage.removeItem('hai_onboarded');
    TestBed.configureTestingModule({
      providers: [
        { provide: AUTH_SERVICE_TOKEN, useValue: { loggedIn: () => of(false) } },
        { provide: Router, useValue: jasmine.createSpyObj<Router>('Router', ['createUrlTree']) },
      ],
    });
  });

  it('should be created', () => {
    expect(executeGuard).toBeTruthy();
  });

  it('preserves the requested internal route when authentication is required', (done) => {
    const router = TestBed.inject(Router) as jasmine.SpyObj<Router>;
    const returnTree = {} as any;
    router.createUrlTree.and.returnValue(returnTree);

    (executeGuard({} as any, { url: '/connected-sources?source=gmail' } as any) as any).subscribe((result: unknown) => {
      expect(result).toBe(returnTree);
      expect(router.createUrlTree).toHaveBeenCalledWith(
        ['/login'],
        { queryParams: { returnUrl: '/connected-sources?source=gmail' } },
      );
      done();
    });
  });

  it('reports an unavailable authentication service and preserves the requested route', (done) => {
    TestBed.overrideProvider(AUTH_SERVICE_TOKEN, {
      useValue: { loggedIn: () => throwError(() => new Error('connection refused')) },
    });
    const router = TestBed.inject(Router) as jasmine.SpyObj<Router>;
    const errorTree = {} as any;
    router.createUrlTree.and.returnValue(errorTree);

    (executeGuard({} as any, { url: '/control-center?tab=attention' } as any) as any).subscribe((result: unknown) => {
      expect(result).toBe(errorTree);
      expect(router.createUrlTree).toHaveBeenCalledWith(['/login'], {
        queryParams: {
          returnUrl: '/control-center?tab=attention',
          error: 'service_unavailable',
        },
      });
      done();
    });
  });

  it('sends an authenticated first-run user to onboarding and preserves the requested route', (done) => {
    TestBed.overrideProvider(AUTH_SERVICE_TOKEN, { useValue: { loggedIn: () => of(true) } });
    const router = TestBed.inject(Router) as jasmine.SpyObj<Router>;
    const onboardingTree = {} as any;
    router.createUrlTree.and.returnValue(onboardingTree);

    (executeGuard({ routeConfig: { path: 'workflow-engine' } } as any, {
      url: '/workflow-engine?state=ready',
    } as any) as any).subscribe((result: unknown) => {
      expect(result).toBe(onboardingTree);
      expect(router.createUrlTree).toHaveBeenCalledWith(['/onboarding'], {
        queryParams: { returnUrl: '/workflow-engine?state=ready' },
      });
      done();
    });
  });

  it('allows an authenticated first-run user to open onboarding without a redirect loop', (done) => {
    TestBed.overrideProvider(AUTH_SERVICE_TOKEN, { useValue: { loggedIn: () => of(true) } });

    (executeGuard({ routeConfig: { path: 'onboarding' } } as any, {
      url: '/onboarding',
    } as any) as any).subscribe((result: unknown) => {
      expect(result).toBeTrue();
      done();
    });
  });

  it('allows authenticated navigation after onboarding is complete', (done) => {
    localStorage.setItem('hai_onboarded', 'true');
    TestBed.overrideProvider(AUTH_SERVICE_TOKEN, { useValue: { loggedIn: () => of(true) } });

    (executeGuard({ routeConfig: { path: 'workflow-engine' } } as any, {
      url: '/workflow-engine',
    } as any) as any).subscribe((result: unknown) => {
      expect(result).toBeTrue();
      done();
    });
  });
});
