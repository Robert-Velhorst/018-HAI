import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { AuthSessionService } from './auth-session.service';
import { provideHttpClient, withInterceptorsFromDi } from '@angular/common/http';

describe('AuthSessionService', () => {
  let service: AuthSessionService;
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({ imports: [], providers: [provideHttpClient(withInterceptorsFromDi()), provideHttpClientTesting()] });
    service = TestBed.inject(AuthSessionService);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => http.verify());

  it('loads and preserves signed session capabilities', () => {
    service.session().subscribe((session) => {
      expect(session).toEqual({
        authenticated: true,
        subject: 'owner-1',
        role: 'owner',
        permissions: {
          canRead: true,
          canOperate: true,
          canApprove: true,
          canAdminister: true,
        },
      });
    });

    const request = http.expectOne('/api/v1/auth/session');
    expect(request.request.method).toBe('GET');
    request.flush({
      authenticated: true,
      subject: ' owner-1 ',
      role: 'owner',
      permissions: {
        canRead: true,
        canOperate: true,
        canApprove: true,
        canAdminister: true,
      },
    });
  });

  it('fails closed for an incomplete or unauthenticated response', () => {
    service.session().subscribe((session) => {
      expect(session.authenticated).toBeFalse();
      expect(session.role).toBe('unknown');
      expect(session.permissions).toEqual({
        canRead: false,
        canOperate: false,
        canApprove: false,
        canAdminister: false,
      });
    });

    const request = http.expectOne('/api/v1/auth/session');
    request.flush({
      authenticated: true,
      subject: '',
      role: 'administrator',
      permissions: {
        canRead: true,
        canOperate: true,
        canApprove: true,
        canAdminister: true,
      },
    });
  });
});
