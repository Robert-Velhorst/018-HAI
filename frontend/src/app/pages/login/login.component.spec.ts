import { FormBuilder } from '@angular/forms';
import { ComponentFixture, fakeAsync, flushMicrotasks, TestBed, tick } from '@angular/core/testing';
import { ActivatedRoute, Router } from '@angular/router';
import { NEVER, of, Subject, throwError } from 'rxjs';
import { timeout } from 'rxjs/operators';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { AuthService } from '../../services/auth/auth.service';
import { AUTH_SERVICE_TOKEN } from '../../services/auth/auth.service.token';
import { LoginComponent } from './login.component';
import { LoginModule } from './login.module';
import { NoopAnimationsModule } from '@angular/platform-browser/animations';

describe('LoginComponent registration', () => {
  function createComponent(returnUrl: string | null = null, authError: string | null = null): { component: LoginComponent; auth: jasmine.SpyObj<any>; notification: jasmine.SpyObj<any>; router: jasmine.SpyObj<Router> } {
    const auth = jasmine.createSpyObj('AuthService', ['getCapabilities', 'loggedIn', 'openLocalPreview', 'login', 'register', 'requestPasswordReset', 'confirmPasswordReset']);
    auth.loggedIn.and.returnValue(of(false));
    auth.getCapabilities.and.returnValue(of({ googleLoginEnabled: false, passwordRecoveryEmailEnabled: false, localPreviewEnabled: false }));
    const notification = jasmine.createSpyObj('NzNotificationService', ['success', 'error', 'warning', 'info', 'create']);
    const router = jasmine.createSpyObj<Router>('Router', ['navigate', 'navigateByUrl']);
    router.navigateByUrl.and.returnValue(Promise.resolve(true));
    const route = { snapshot: { queryParamMap: { get: (key: string) => key === 'returnUrl' ? returnUrl : key === 'error' ? authError : null } } } as unknown as ActivatedRoute;
    const component = new LoginComponent(new FormBuilder(), notification as NzNotificationService, router, route, auth);
    component.ngOnInit();
    return { component, auth, notification, router };
  }

  it('creates an operator account and returns to login without authenticating', () => {
    const { component, auth, notification } = createComponent();
    auth.register.and.returnValue(of({ id: 'new-user', email: 'operator@example.com' }));
    component.toggleRegistration();
    component.validateForm.patchValue({
      userName: 'operator@example.com',
      password: 'local-passphrase-2026',
      confirmPassword: 'local-passphrase-2026',
    });

    component.submitForm();

    expect(auth.register).toHaveBeenCalledWith('operator@example.com', 'local-passphrase-2026');
    expect(auth.login).not.toHaveBeenCalled();
    expect(component.registrationMode).toBeFalse();
    expect(notification.success).toHaveBeenCalledWith('Account created', 'Your local operator account is ready. Sign in to continue.');
  });

  it('shows safe explanations for Google OAuth callback errors', () => {
    const cases = [
      {
        code: 'service_unavailable',
        expected: 'HAI could not reach its local authentication service. This is a service connection problem, not a password error. Start the HAI services and retry.',
      },
      {
        code: 'google_unavailable',
        expected: 'Google sign-in is unavailable on this HAI installation. Use your email and password, or contact the HAI administrator.',
      },
      {
        code: 'google_denied',
        expected: 'Google sign-in was cancelled or not approved. Try again or use password sign-in.',
      },
      {
        code: 'google_failed',
        expected: 'Google sign-in could not be completed. Try password sign-in or contact the HAI administrator.',
      },
    ];

    for (const testCase of cases) {
      const { component } = createComponent(null, testCase.code);
      expect(component.signInErrorMessage).toBe(testCase.expected);
    }
  });

  it('ignores unknown authentication error query values', () => {
    const { component } = createComponent(null, 'untrusted-error-text');
    expect(component.signInErrorMessage).toBe('');
  });

  it('offers an auth retry and preserves the requested page when the service recovers', fakeAsync(() => {
    const { component, auth, router } = createComponent('/control-center?tab=attention', 'service_unavailable');
    auth.loggedIn.and.returnValue(of(true));

    expect(component.authenticationUnavailable).toBeTrue();
    component.retryAuthentication();
    flushMicrotasks();

    expect(auth.loggedIn).toHaveBeenCalled();
    expect(router.navigateByUrl).toHaveBeenCalledWith('/control-center?tab=attention');
    expect(component.authenticationUnavailable).toBeFalse();
  }));

  it('keeps sign-in required when auth retry confirms there is no session', () => {
    const { component, auth, router } = createComponent('/control-center', 'service_unavailable');
    auth.loggedIn.and.returnValue(of(false));

    component.retryAuthentication();

    expect(component.authenticationUnavailable).toBeFalse();
    expect(component.signInErrorMessage).toContain('Sign in with your account');
    expect(router.navigateByUrl).not.toHaveBeenCalled();
  });

  it('keeps the retry action visible when authentication remains unavailable', () => {
    const { component, auth } = createComponent('/control-center', 'service_unavailable');
    auth.loggedIn.and.returnValue(throwError(() => new Error('connection refused')));

    component.retryAuthentication();

    expect(component.authenticationUnavailable).toBeTrue();
    expect(component.signInErrorMessage).toContain('could not reach its local authentication service');
    expect(component.authenticationRetrying).toBeFalse();
  });

  it('flags the confirmation field when it does not match before calling the IDP', () => {
    const { component, auth, notification } = createComponent();
    component.toggleRegistration();
    component.validateForm.patchValue({
      userName: 'operator@example.com',
      password: 'local-passphrase-2026',
      confirmPassword: 'different-passphrase-2026',
    });

    component.submitForm();

    expect(auth.register).not.toHaveBeenCalled();
    expect(component.isFieldInvalid('confirmPassword')).toBeTrue();
    expect(notification.error).toHaveBeenCalledWith('Check your sign-up details', 'The confirmation password does not match.');
  });

  it('explains the password length requirement before calling the IDP', () => {
    const { component, auth, notification } = createComponent();
    component.toggleRegistration();
    component.validateForm.patchValue({
      userName: 'operator@example.com',
      password: 'short-pass',
      confirmPassword: 'short-pass',
    });

    component.submitForm();

    expect(auth.register).not.toHaveBeenCalled();
    expect(component.isFieldInvalid('password')).toBeTrue();
    expect(notification.error).toHaveBeenCalledWith('Check your sign-up details', 'Use at least 12 characters.');
  });

  it('counts Unicode code points for sign-up password minimums', () => {
    const { component, auth, notification } = createComponent();
    const sixAstralCharacters = String.fromCodePoint(0x1f512).repeat(6);
    auth.register.and.returnValue(of({ id: 'unicode-user', email: 'operator@example.com' }));
    component.toggleRegistration();
    component.validateForm.patchValue({
      userName: 'operator@example.com',
      password: sixAstralCharacters,
      confirmPassword: sixAstralCharacters,
    });

    component.submitForm();

    expect(auth.register).not.toHaveBeenCalled();
    expect(component.isFieldInvalid('password')).toBeTrue();
    expect(notification.error).toHaveBeenCalledWith(
      'Check your sign-up details',
      'Use at least 12 characters.'
    );
  });

  it('rejects sign-up passwords above the IDP 72-byte limit before sending them', () => {
    const { component, auth, notification } = createComponent();
    const oversizedPassword = 'a'.repeat(73);
    component.toggleRegistration();
    component.validateForm.patchValue({
      userName: 'operator@example.com',
      password: oversizedPassword,
      confirmPassword: oversizedPassword,
    });

    component.submitForm();

    expect(auth.register).not.toHaveBeenCalled();
    expect(component.isFieldInvalid('password')).toBeTrue();
    expect(notification.error).toHaveBeenCalledWith(
      'Check your sign-up details',
      'Use no more than 72 UTF-8 bytes.'
    );
  });

  it('accepts a valid multibyte sign-up password exactly at the IDP byte limit', () => {
    const { component, auth } = createComponent();
    const exactLimitPassword = String.fromCodePoint(0x1f512).repeat(18);
    auth.register.and.returnValue(of({ id: 'unicode-user', email: 'operator@example.com' }));
    component.toggleRegistration();
    component.validateForm.patchValue({
      userName: 'operator@example.com',
      password: exactLimitPassword,
      confirmPassword: exactLimitPassword,
    });

    component.submitForm();

    expect(auth.register).toHaveBeenCalledOnceWith('operator@example.com', exactLimitPassword);
  });

  it('gives a safe actionable message when the IDP rejects account details', () => {
    const { component, auth, notification } = createComponent();
    auth.register.and.returnValue(throwError(() => ({
      status: 400,
      error: { message: 'private backend detail' },
    })));
    component.toggleRegistration();
    component.validateForm.patchValue({
      userName: 'operator@example.com',
      password: 'local-passphrase-2026',
      confirmPassword: 'local-passphrase-2026',
    });

    component.submitForm();

    expect(notification.error).toHaveBeenCalledWith(
      'Sign-up details were rejected',
      'Check that the email is valid and the password is between 12 characters and 72 UTF-8 bytes.'
    );
    expect(JSON.stringify(notification.error.calls.allArgs())).not.toContain('private backend detail');
  });

  it('marks an email as already registered when the IDP returns conflict', () => {
    const { component, auth, notification } = createComponent();
    auth.register.and.returnValue(throwError(() => ({ status: 409 })));
    component.toggleRegistration();
    component.validateForm.patchValue({
      userName: 'operator@example.com',
      password: 'local-passphrase-2026',
      confirmPassword: 'local-passphrase-2026',
    });

    component.submitForm();

    expect(component.isFieldInvalid('userName')).toBeTrue();
    expect(component.emailErrorTip()).toBe('This email is already registered. Log in instead.');
    expect(notification.error).toHaveBeenCalledWith('Account already exists', 'This email is already registered. Log in instead or use password recovery.');
  });

  it('does not display backend registration errors that may echo the password', () => {
    const { component, auth, notification } = createComponent();
    const password = 'local-passphrase-2026';
    auth.register.and.returnValue(throwError(() => ({
      status: 500,
      error: { message: `Registration failed for password ${password}` },
    })));
    component.toggleRegistration();
    component.validateForm.patchValue({
      userName: 'operator@example.com',
      password,
      confirmPassword: password,
    });

    component.submitForm();

    expect(notification.error).toHaveBeenCalledWith(
      'Sign-up failed',
      'HAI could not create this account. Check the connection and try again later.'
    );
    expect(JSON.stringify(notification.error.calls.allArgs())).not.toContain(password);
  });

  it('continues from local account creation to sign-in and the originally requested page', fakeAsync(() => {
    const { component, auth, router } = createComponent('/workflow-engine?view=advanced');
    auth.register.and.returnValue(of({ id: 'new-user', email: 'operator@example.com' }));
    auth.login.and.returnValue(of(void 0));
    component.toggleRegistration();
    component.validateForm.patchValue({
      userName: 'operator@example.com',
      password: 'local-passphrase-2026',
      confirmPassword: 'local-passphrase-2026',
    });

    component.submitForm();
    expect(component.registrationMode).toBeFalse();
    expect(component.validateForm.controls['password'].value).toBe('');

    component.validateForm.controls['password'].setValue('local-passphrase-2026');
    component.submitForm();
    flushMicrotasks();

    expect(auth.login).toHaveBeenCalledWith('operator@example.com', 'local-passphrase-2026', true);
    expect(router.navigateByUrl).toHaveBeenCalledWith('/workflow-engine?view=advanced');
    expect(component.signingIn).toBeFalse();
  }));

  it('does not abandon or duplicate an account creation while its request is pending', fakeAsync(() => {
    const { component, auth, notification } = createComponent();
    auth.register.and.returnValue(NEVER);
    component.toggleRegistration();
    component.validateForm.patchValue({
      userName: 'operator@example.com',
      password: 'local-passphrase-2026',
      confirmPassword: 'local-passphrase-2026',
    });

    component.submitForm();
    component.toggleRegistration();
    component.submitForm();

    expect(component.registrationMode).toBeTrue();
    expect(component.registering).toBeTrue();
    expect(auth.register).toHaveBeenCalledTimes(1);

    tick(component.signInTimeoutMs + 1);

    expect(component.registering).toBeFalse();
    expect(notification.error).toHaveBeenCalledWith(
      'Sign-up status unknown',
      'HAI did not confirm whether the account was created. Try signing in with this email before submitting sign-up again.'
    );
    component.toggleRegistration();
    expect(component.registrationMode).toBeFalse();
  }));

  it('allows the existing account to log in after a registration conflict', () => {
    const { component, auth } = createComponent();
    auth.register.and.returnValue(throwError(() => ({ status: 409 })));
    auth.login.and.returnValue(of(void 0));
    component.toggleRegistration();
    component.validateForm.patchValue({
      userName: 'operator@example.com',
      password: 'local-passphrase-2026',
      confirmPassword: 'local-passphrase-2026',
    });

    component.submitForm();
    expect(component.isFieldInvalid('userName')).toBeTrue();
    expect(component.emailErrorTip()).toBe('This email is already registered. Log in instead.');

    component.toggleRegistration();
    component.validateForm.controls['password'].setValue('local-passphrase-2026');
    component.submitForm();

    expect(auth.login).toHaveBeenCalledWith('operator@example.com', 'local-passphrase-2026', true);
  });

  it('requests a password reset without showing developer recovery instructions', () => {
    const { component, auth, notification } = createComponent();
    auth.getCapabilities.and.returnValue(of({ googleLoginEnabled: false, passwordRecoveryEmailEnabled: true, localPreviewEnabled: false }));
    component.ngOnInit();
    auth.requestPasswordReset.and.returnValue(of(void 0));
    component.validateForm.patchValue({ userName: 'operator@example.com' });

    component.showPasswordHelp();
    component.submitForm();

    expect(component.recoveryMode).toBeTrue();
    expect(component.recoveryStep).toBe('confirm');
    expect(auth.requestPasswordReset).toHaveBeenCalledWith('operator@example.com');
    expect(notification.info).toHaveBeenCalledWith(
      'Recovery request received',
      'For privacy, HAI does not confirm whether an account matches or whether email was delivered. The settings check does not test provider connectivity. If a code does not arrive, check spam and ask the HAI operator to inspect the redacted SMTP stage in IDP logs and the provider delivery status.'
    );
    expect(JSON.stringify(notification.info.calls.allArgs())).not.toContain('has been sent');
  });

  it('does not request a reset code when SMTP capability cannot be confirmed', () => {
    const { component, auth, notification } = createComponent();
    auth.getCapabilities.and.returnValue(NEVER);
    component.loadAuthCapabilities();
    component.showPasswordHelp();
    component.validateForm.patchValue({ userName: 'operator@example.com' });

    component.submitForm();

    expect(auth.requestPasswordReset).not.toHaveBeenCalled();
    expect(notification.warning).toHaveBeenCalledWith(
      'Recovery status unavailable',
      'Retry the sign-in service check before requesting a reset code. Password sign-in remains available.'
    );
  });

  it('keeps a failed recovery request on the retryable request step', () => {
    const { component, auth, notification } = createComponent();
    auth.getCapabilities.and.returnValue(of({ googleLoginEnabled: false, passwordRecoveryEmailEnabled: true, localPreviewEnabled: false }));
    component.loadAuthCapabilities();
    auth.requestPasswordReset.and.returnValues(
      throwError(() => new Error('recovery service offline')),
      of(void 0)
    );
    component.validateForm.patchValue({ userName: 'operator@example.com' });
    component.showPasswordHelp();

    component.submitForm();

    expect(component.recoveryStep).toBe('request');
    expect(component.recovering).toBeFalse();
    expect(auth.requestPasswordReset).toHaveBeenCalledWith('operator@example.com');
    expect(notification.success).not.toHaveBeenCalled();
    expect(notification.error).toHaveBeenCalledWith(
      'Recovery request failed',
      'HAI could not contact the recovery service. Please try again. This does not confirm whether the address has an account.'
    );

    component.submitForm();

    expect(auth.requestPasswordReset).toHaveBeenCalledTimes(2);
    expect(component.recoveryStep).toBe('confirm');
  });

  it('explains recovery rate limits using the server retry delay without revealing account status', () => {
    const { component, auth, notification } = createComponent();
    auth.getCapabilities.and.returnValue(of({ googleLoginEnabled: false, passwordRecoveryEmailEnabled: true, localPreviewEnabled: false }));
    component.loadAuthCapabilities();
    auth.requestPasswordReset.and.returnValue(throwError(() => ({
      status: 429,
      headers: { get: (name: string) => name === 'Retry-After' ? '90' : null },
      error: { message: 'internal limiter detail' },
    })));
    component.validateForm.patchValue({ userName: 'operator@example.com' });
    component.showPasswordHelp();

    component.submitForm();

    expect(component.recoveryStep).toBe('request');
    expect(component.recovering).toBeFalse();
    expect(notification.warning).toHaveBeenCalledOnceWith(
      'Too many recovery requests',
      'Wait about 2 minutes before trying again. This does not indicate whether the address has an account.'
    );
    expect(JSON.stringify(notification.warning.calls.allArgs())).not.toContain('internal limiter detail');
  });

  it('releases both recovery steps after a timeout and permits a safe retry', fakeAsync(() => {
    const { component, auth, notification } = createComponent();
    auth.getCapabilities.and.returnValue(of({ googleLoginEnabled: false, passwordRecoveryEmailEnabled: true, localPreviewEnabled: false }));
    component.loadAuthCapabilities();
    auth.requestPasswordReset.and.returnValues(NEVER, of(void 0));
    component.validateForm.patchValue({ userName: 'operator@example.com' });
    component.showPasswordHelp();

    component.submitForm();
    expect(component.recovering).toBeTrue();
    tick(component.passwordRecoveryTimeoutMs + 1);

    expect(component.recovering).toBeFalse();
    expect(component.recoveryStep).toBe('request');
    expect(notification.error).toHaveBeenCalledWith(
      'Recovery request timed out',
      'HAI did not receive confirmation from the recovery service in time. Try again later; this does not confirm whether the address has an account.'
    );

    component.submitForm();
    expect(component.recoveryStep).toBe('confirm');
    auth.confirmPasswordReset.and.returnValue(NEVER);
    component.validateForm.patchValue({
      resetToken: 'test-reset-code',
      password: 'local-passphrase-2026',
      confirmPassword: 'local-passphrase-2026',
    });
    component.submitForm();
    tick(component.passwordRecoveryTimeoutMs + 1);

    expect(component.recovering).toBeFalse();
    expect(component.recoveryStep).toBe('confirm');
    expect(notification.error).toHaveBeenCalledWith(
      'Reset confirmation timed out',
      'HAI did not confirm the password change in time. Try signing in with the new password before requesting another reset.'
    );
  }));

  it('distinguishes an auth-capability outage and retries without blocking password login', () => {
    const { component, auth, router } = createComponent();
    const retryResult = new Subject<any>();
    auth.getCapabilities.and.returnValues(
      throwError(() => new Error('capability service offline')),
      retryResult.asObservable()
    );

    component.loadAuthCapabilities();

    expect(component.authCapabilitiesLoaded).toBeFalse();
    expect(component.authCapabilitiesUnavailable).toBeTrue();
    auth.login.and.returnValue(of(void 0));
    component.validateForm.patchValue({ userName: 'operator@example.com', password: 'local-passphrase-2026' });
    component.submitForm();
    expect(auth.login).toHaveBeenCalledWith('operator@example.com', 'local-passphrase-2026', true);

    component.loadAuthCapabilities();

    expect(component.authCapabilitiesLoaded).toBeFalse();
    expect(component.authCapabilitiesUnavailable).toBeTrue();
    expect(component.authCapabilitiesLoading).toBeTrue();
    retryResult.next({ googleLoginEnabled: true, passwordRecoveryEmailEnabled: true, localPreviewEnabled: false });
    retryResult.complete();

    expect(component.authCapabilitiesLoaded).toBeTrue();
    expect(component.authCapabilitiesUnavailable).toBeFalse();
    expect(component.googleLoginEnabled).toBeTrue();
    expect(auth.getCapabilities).toHaveBeenCalledTimes(3);
    expect(router.navigateByUrl).toHaveBeenCalledWith('/control-center');
  });

  it('marks optional services unavailable after a hanging probe and keeps password login usable', fakeAsync(() => {
    const { component, auth, router } = createComponent();
    auth.getCapabilities.and.returnValue(NEVER.pipe(timeout(AuthService.capabilitiesTimeoutMs)));

    component.loadAuthCapabilities();
    expect(component.authCapabilitiesLoading).toBeTrue();

    tick(AuthService.capabilitiesTimeoutMs + 1);

    expect(component.authCapabilitiesLoading).toBeFalse();
    expect(component.authCapabilitiesLoaded).toBeFalse();
    expect(component.authCapabilitiesUnavailable).toBeTrue();

    auth.login.and.returnValue(of(void 0));
    component.validateForm.patchValue({ userName: 'operator@example.com', password: 'local-passphrase-2026' });
    component.submitForm();

    expect(auth.login).toHaveBeenCalledWith('operator@example.com', 'local-passphrase-2026', true);
    expect(router.navigateByUrl).toHaveBeenCalledWith('/control-center');

    auth.getCapabilities.and.returnValue(of({ googleLoginEnabled: true, passwordRecoveryEmailEnabled: false, localPreviewEnabled: false }));
    component.loadAuthCapabilities();

    expect(component.authCapabilitiesLoading).toBeFalse();
    expect(component.authCapabilitiesLoaded).toBeTrue();
    expect(component.authCapabilitiesUnavailable).toBeFalse();
  }));

  it('opens the dashboard through the explicit local preview session', () => {
    const { component, auth, router } = createComponent('/connected-sources');
    auth.openLocalPreview.and.returnValue(of(void 0));

    component.openLocalPreview();

    expect(auth.openLocalPreview).toHaveBeenCalled();
    expect(router.navigateByUrl).toHaveBeenCalledWith('/connected-sources');
  });

  it('releases the login page when local preview access never responds', fakeAsync(() => {
    const { component, auth, notification } = createComponent();
    auth.openLocalPreview.and.returnValue(NEVER);

    component.openLocalPreview();
    expect(component.openingLocalPreview).toBeTrue();
    tick(component.signInTimeoutMs + 1);

    expect(component.openingLocalPreview).toBeFalse();
    expect(notification.error).toHaveBeenCalledWith(
      'Local preview unavailable',
      'HAI did not receive confirmation of local access in time. Check the connection or retry sign-in.'
    );
  }));

  it('returns to the requested internal route after password login', () => {
    const { component, auth, router } = createComponent('/workflow-engine?view=advanced');
    auth.login.and.returnValue(of(void 0));
    component.validateForm.patchValue({ userName: 'operator@example.com', password: 'local-passphrase-2026' });

    component.submitForm();

    expect(auth.login).toHaveBeenCalledWith('operator@example.com', 'local-passphrase-2026', true);
    expect(router.navigateByUrl).toHaveBeenCalledWith('/workflow-engine?view=advanced');
  });

  it('shows progress and prevents duplicate requests while sign-in is pending', () => {
    const { component, auth } = createComponent();
    const pendingLogin = new Subject<void>();
    auth.login.and.returnValue(pendingLogin.asObservable());
    component.validateForm.patchValue({ userName: 'operator@example.com', password: 'local-passphrase-2026' });

    component.submitForm();
    component.submitForm();

    expect(component.signingIn).toBeTrue();
    expect(auth.login).toHaveBeenCalledTimes(1);
    pendingLogin.complete();
  });

  it('releases the login screen with a retryable message when sign-in times out', fakeAsync(() => {
    const { component, auth, notification } = createComponent();
    auth.login.and.returnValue(NEVER);
    component.validateForm.patchValue({ userName: 'operator@example.com', password: 'local-passphrase-2026' });

    component.submitForm();
    expect(component.signingIn).toBeTrue();
    tick(component.signInTimeoutMs + 1);

    expect(component.signingIn).toBeFalse();
    expect(component.signInErrorMessage).toBe('HAI did not receive a sign-in response in time. Check the connection and try again.');
    expect(notification.create).toHaveBeenCalledWith('error', 'Sign-in timed out', component.signInErrorMessage);
  }));

  it('shows an inline credential error and remains retryable after a rejected sign-in', () => {
    const { component, auth, notification } = createComponent();
    auth.login.and.returnValue(throwError(() => ({ status: 401 })));
    component.validateForm.patchValue({ userName: 'operator@example.com', password: 'wrong-password' });

    component.submitForm();

    expect(component.signingIn).toBeFalse();
    expect(component.signInErrorMessage).toBe('The email or password is incorrect. Check both fields and try again.');
    expect(notification.create).toHaveBeenCalledWith('error', 'Login failed', component.signInErrorMessage);

    component.validateForm.controls['password'].setValue('corrected-password');
    component.clearSignInError();
    expect(component.signInErrorMessage).toBe('');
  });

  it('explains when the login endpoint succeeds but routing returns to the login page', fakeAsync(() => {
    const { component, auth, router } = createComponent('/control-center');
    auth.login.and.returnValue(of(void 0));
    (router as any).url = '/login?returnUrl=%2Fcontrol-center';
    component.validateForm.patchValue({ userName: 'operator@example.com', password: 'local-passphrase-2026' });

    component.submitForm();
    flushMicrotasks();

    expect(component.signingIn).toBeFalse();
    expect(component.signInErrorMessage).toBe('Your sign-in was accepted, but HAI could not confirm your session. Check the connection and try again.');
  }));

  it('clears the pending sign-in state when the requested-route navigation is cancelled', fakeAsync(() => {
    const { component, auth, router } = createComponent('/control-center');
    auth.login.and.returnValue(of(void 0));
    router.navigateByUrl.and.returnValue(Promise.resolve(false));
    component.validateForm.patchValue({ userName: 'operator@example.com', password: 'local-passphrase-2026' });

    component.submitForm();
    flushMicrotasks();

    expect(component.signingIn).toBeFalse();
    expect(component.signInErrorMessage).toBe('Your sign-in was accepted, but HAI could not confirm your session. Check the connection and try again.');
  }));

  it('sends an unchecked remember-me choice on password login', () => {
    const { component, auth } = createComponent();
    auth.login.and.returnValue(of(void 0));
    component.validateForm.patchValue({
      userName: 'operator@example.com',
      password: 'local-passphrase-2026',
      remember: false,
    });

    component.submitForm();

    expect(auth.login).toHaveBeenCalledWith('operator@example.com', 'local-passphrase-2026', false);
  });

  it('rejects an unsafe login return path', () => {
    const { component, auth, router } = createComponent('//untrusted.example');
    auth.login.and.returnValue(of(void 0));
    component.validateForm.patchValue({ userName: 'operator@example.com', password: 'local-passphrase-2026' });

    component.submitForm();

    expect(router.navigateByUrl).toHaveBeenCalledWith('/control-center');
  });

  it('rejects authentication-loop destinations and malformed return paths', () => {
    for (const returnUrl of ['/login', '/onboarding', '/%2f%2fevil.example', '/%E0%A4%A']) {
      const { component } = createComponent(returnUrl);

      expect((component as any).authenticationDestination())
        .withContext(returnUrl)
        .toBe('/control-center');
    }

    const { component } = createComponent('/connected-sources?source=gmail');
    expect((component as any).authenticationDestination())
      .toBe('/connected-sources?source=gmail');
  });

  it('passes the requested internal route to Google sign-in', () => {
    const { component } = createComponent('/connected-sources?source=gmail');

    expect((component as any).googleLoginURL()).toBe('/api/v1/auth/google/login?returnUrl=%2Fconnected-sources%3Fsource%3Dgmail&rememberMe=true');
  });

  it('passes an unchecked remember-me choice to Google sign-in', () => {
    const { component } = createComponent('/control-center');
    component.validateForm.controls['remember'].setValue(false);

    expect((component as any).googleLoginURL()).toBe('/api/v1/auth/google/login?returnUrl=%2Fcontrol-center&rememberMe=false');
  });

  it('does not request a reset code when email recovery is unavailable', () => {
    const { component, auth, notification } = createComponent();
    component.validateForm.patchValue({ userName: 'operator@example.com' });

    component.showPasswordHelp();
    component.submitForm();

    expect(auth.requestPasswordReset).not.toHaveBeenCalled();
    expect(notification.warning).toHaveBeenCalledWith(
      'Email recovery is unavailable',
      'Email password recovery is turned off for this HAI installation. No reset email was requested. Use password sign-in or contact the HAI administrator.'
    );
  });

  it('does not confirm a reset when the new passwords differ', () => {
    const { component, auth, notification } = createComponent();
    component.showPasswordHelp();
    component.showResetConfirmation();
    component.validateForm.patchValue({
      userName: 'operator@example.com',
      resetToken: 'reset-token',
      password: 'local-passphrase-2026',
      confirmPassword: 'different-passphrase-2026',
    });

    component.submitForm();

    expect(auth.confirmPasswordReset).not.toHaveBeenCalled();
    expect(component.isFieldInvalid('confirmPassword')).toBeTrue();
    expect(notification.error).toHaveBeenCalledWith('Check your reset details', 'The confirmation password does not match.');
  });

  it('counts Unicode code points for password-reset minimums', () => {
    const { component, auth, notification } = createComponent();
    const sixAstralCharacters = String.fromCodePoint(0x1f512).repeat(6);
    auth.confirmPasswordReset.and.returnValue(of(void 0));
    component.showPasswordHelp();
    component.showResetConfirmation();
    component.validateForm.patchValue({
      userName: 'operator@example.com',
      resetToken: 'reset-token',
      password: sixAstralCharacters,
      confirmPassword: sixAstralCharacters,
    });

    component.submitForm();

    expect(auth.confirmPasswordReset).not.toHaveBeenCalled();
    expect(component.isFieldInvalid('password')).toBeTrue();
    expect(notification.error).toHaveBeenCalledWith(
      'Check your reset details',
      'Use at least 12 characters.'
    );
  });

  it('rejects reset passwords above the IDP 72-byte limit before calling the reset endpoint', () => {
    const { component, auth, notification } = createComponent();
    const oversizedPassword = 'a'.repeat(73);
    component.showPasswordHelp();
    component.showResetConfirmation();
    component.validateForm.patchValue({
      userName: 'operator@example.com',
      resetToken: 'one-time-reset-code',
      password: oversizedPassword,
      confirmPassword: oversizedPassword,
    });

    component.submitForm();

    expect(auth.confirmPasswordReset).not.toHaveBeenCalled();
    expect(component.isFieldInvalid('password')).toBeTrue();
    expect(notification.error).toHaveBeenCalledWith(
      'Check your reset details',
      'Use no more than 72 UTF-8 bytes.'
    );
  });

  it('sends the reset code and new password to the reset service, then clears them on success', () => {
    const { component, auth } = createComponent();
    auth.confirmPasswordReset.and.returnValue(of(void 0));
    component.showPasswordHelp();
    component.showResetConfirmation();
    component.validateForm.patchValue({
      userName: 'operator@example.com',
      resetToken: 'one-time-reset-secret',
      password: 'new-local-passphrase-2026',
      confirmPassword: 'new-local-passphrase-2026',
    });

    component.submitForm();

    expect(auth.confirmPasswordReset).toHaveBeenCalledOnceWith(
      'one-time-reset-secret',
      'new-local-passphrase-2026'
    );
    expect(component.validateForm.controls['resetToken'].value).toBe('');
    expect(component.validateForm.controls['password'].value).toBe('');
    expect(component.validateForm.controls['confirmPassword'].value).toBe('');
  });

  it('does not display server reset errors that may echo the reset code or password', () => {
    const { component, auth, notification } = createComponent();
    const resetToken = 'one-time-reset-secret';
    const password = 'new-local-passphrase-2026';
    auth.confirmPasswordReset.and.returnValue(throwError(() => ({
      status: 400,
      error: { message: `Rejected ${resetToken} and ${password}` },
    })));
    component.showPasswordHelp();
    component.showResetConfirmation();
    component.validateForm.patchValue({
      userName: 'operator@example.com',
      resetToken,
      password,
      confirmPassword: password,
    });

    component.submitForm();

    expect(notification.error).toHaveBeenCalledWith(
      'Reset could not be completed',
      'The reset code was not accepted, or the password is outside the 12-character to 72-byte limit. Check the details or request a new code.'
    );
    const displayedErrors = JSON.stringify(notification.error.calls.allArgs());
    expect(displayedErrors).not.toContain(resetToken);
    expect(displayedErrors).not.toContain(password);
  });
});

describe('LoginComponent rendered forms', () => {
  let fixture: ComponentFixture<LoginComponent>;
  let component: LoginComponent;
  let auth: jasmine.SpyObj<any>;

  beforeEach(async () => {
    auth = jasmine.createSpyObj('AuthService', [
      'getCapabilities', 'openLocalPreview', 'login', 'register',
      'requestPasswordReset', 'confirmPasswordReset',
    ]);
    auth.getCapabilities.and.returnValue(of({
      googleLoginEnabled: false,
      passwordRecoveryEmailEnabled: true,
      localPreviewEnabled: false,
    }));
    const notification = jasmine.createSpyObj('NzNotificationService', [
      'success', 'error', 'warning', 'info', 'create',
    ]);
    const router = jasmine.createSpyObj<Router>('Router', ['navigate', 'navigateByUrl']);
    router.navigateByUrl.and.returnValue(Promise.resolve(true));
    Object.defineProperty(router, 'url', { value: '/login', configurable: true });
    const route = {
      snapshot: { queryParamMap: { get: () => null } },
    } as unknown as ActivatedRoute;

    await TestBed.configureTestingModule({
      imports: [LoginModule, NoopAnimationsModule],
      providers: [
        { provide: AUTH_SERVICE_TOKEN, useValue: auth },
        { provide: NzNotificationService, useValue: notification },
        { provide: Router, useValue: router },
        { provide: ActivatedRoute, useValue: route },
      ],
    }).compileComponents();

    fixture = TestBed.createComponent(LoginComponent);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  function expectLinkedError(inputId: string, errorId: string): void {
    const input = fixture.nativeElement.querySelector(`#${inputId}`) as HTMLInputElement;
    const controlNameByInputId: Record<string, string> = {
      'login-email': 'userName',
      'login-password': 'password',
      'login-reset-code': 'resetToken',
      'login-confirm-password': 'confirmPassword',
    };
    const controlName = controlNameByInputId[inputId];
    if (!controlName) {
      throw new Error(`No form control is mapped for input ${inputId}`);
    }
    const control = component.validateForm.controls[controlName];
    const state = `${control.status} ${JSON.stringify(control.errors)} dirty=${control.dirty} touched=${control.touched}`;
    expect(input.getAttribute('aria-invalid')).withContext(state).toBe('true');
    expect(input.getAttribute('aria-describedby')?.split(/\s+/)).withContext(state).toContain(errorId);
    expect(fixture.nativeElement.querySelector(`#${errorId}`)?.textContent.trim()).toBeTruthy();
  }

  it('links login validation errors to their labelled inputs for assistive technology', () => {
    component.submitForm();
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('label[for="login-email"]')?.textContent).toContain('Email');
    expect(fixture.nativeElement.querySelector('label[for="login-password"]')?.textContent).toContain('Password');
    expectLinkedError('login-email', 'login-email-error');
    expectLinkedError('login-password', 'login-password-error');
  });

  it('links operator-account and reset validation errors to the correct inputs', () => {
    component.toggleRegistration();
    fixture.detectChanges();
    component.validateForm.patchValue({
      userName: 'operator@example.com',
      password: 'local-passphrase-2026',
      confirmPassword: 'different-passphrase-2026',
    });
    (fixture.nativeElement.querySelector('form') as HTMLFormElement).dispatchEvent(
      new Event('submit', { bubbles: true, cancelable: true }),
    );
    expect(component.confirmationErrorTip()).toBe('Passwords do not match.');
    fixture.detectChanges();

    expectLinkedError('login-confirm-password', 'login-confirm-password-error');
    const confirmation = fixture.nativeElement.querySelector('#login-confirm-password') as HTMLInputElement;
    confirmation.value = 'local-passphrase-2026';
    confirmation.dispatchEvent(new Event('input', { bubbles: true }));
    fixture.detectChanges();
    expect(component.isFieldInvalid('confirmPassword')).toBeFalse();

    component.showPasswordHelp();
    fixture.detectChanges();
    component.showResetConfirmation();
    fixture.detectChanges();
    component.validateForm.patchValue({
      resetToken: '',
      password: 'short-pass',
      confirmPassword: '',
    });
    (fixture.nativeElement.querySelector('form') as HTMLFormElement).dispatchEvent(
      new Event('submit', { bubbles: true, cancelable: true }),
    );
    fixture.detectChanges();

    expectLinkedError('login-reset-code', 'login-reset-code-error');
    expectLinkedError('login-password', 'login-password-error');
    expectLinkedError('login-confirm-password', 'login-confirm-password-error');
  });

  it('keeps the confirmation secret masked and resets visibility when switching forms', () => {
    component.toggleRegistration();
    fixture.detectChanges();
    const password = fixture.nativeElement.querySelector('#login-password') as HTMLInputElement;
    const confirmation = fixture.nativeElement.querySelector('#login-confirm-password') as HTMLInputElement;

    expect(password.type).toBe('password');
    expect(confirmation.type).toBe('password');
    (fixture.nativeElement.querySelector('.password-visibility-toggle') as HTMLButtonElement).click();
    fixture.detectChanges();
    expect(password.type).toBe('text');
    expect(confirmation.type).toBe('password');

    component.showPasswordHelp();
    component.showResetConfirmation();
    fixture.detectChanges();
    expect((fixture.nativeElement.querySelector('#login-password') as HTMLInputElement).type).toBe('password');
    expect((fixture.nativeElement.querySelector('#login-confirm-password') as HTMLInputElement).type).toBe('password');
  });

  it('explains disabled Google and email recovery while keeping password sign-in available', () => {
    auth.getCapabilities.and.returnValue(of({
      googleLoginEnabled: false,
      passwordRecoveryEmailEnabled: false,
      localPreviewEnabled: false,
    }));
    component.loadAuthCapabilities();
    fixture.detectChanges();

    const notice = fixture.nativeElement.querySelector('[data-testid="login-capability-note"]') as HTMLElement;
    const loginButton = fixture.nativeElement.querySelector('[data-testid="login-submit"]') as HTMLButtonElement;
    expect(notice.textContent).toContain('Password sign-in remains available.');
    expect(notice.textContent).toContain('Google sign-in is turned off');
    expect(notice.textContent).toContain('Email password recovery is turned off');
    expect(fixture.nativeElement.querySelector('[data-testid="login-google"]')).toBeNull();
    expect(loginButton.disabled).toBeFalse();
    expect(notice.textContent).not.toMatch(/SMTP_|GOOGLE_OAUTH|CLIENT_SECRET/);

    (fixture.nativeElement.querySelector('[data-testid="login-password-help"]') as HTMLButtonElement).click();
    fixture.detectChanges();
    const recoveryHint = fixture.nativeElement.querySelector('.login-form-hint') as HTMLElement;
    const recoveryButton = fixture.nativeElement.querySelector('[data-testid="login-submit"]') as HTMLButtonElement;
    expect(recoveryHint.textContent).toContain('No reset email can be requested here');
    expect(recoveryHint.textContent).toContain('contact the HAI administrator');
    expect(recoveryHint.textContent).not.toMatch(/SMTP_|PASSWORD=|secret/i);
    expect(recoveryButton.disabled).toBeTrue();
  });

  it('shows Google and email recovery controls only when capabilities are enabled and does not promise delivery', () => {
    auth.getCapabilities.and.returnValue(of({
      googleLoginEnabled: true,
      passwordRecoveryEmailEnabled: true,
      localPreviewEnabled: false,
    }));
    component.loadAuthCapabilities();
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('[data-testid="login-capability-note"]')).toBeNull();
    const googleButton = fixture.nativeElement.querySelector('[data-testid="login-google"]') as HTMLButtonElement;
    expect(googleButton).not.toBeNull();
    expect(googleButton.disabled).toBeFalse();

    (fixture.nativeElement.querySelector('[data-testid="login-password-help"]') as HTMLButtonElement).click();
    fixture.detectChanges();
    const recoveryHint = fixture.nativeElement.querySelector('.login-form-hint') as HTMLElement;
    const recoveryButton = fixture.nativeElement.querySelector('[data-testid="login-submit"]') as HTMLButtonElement;
    expect(recoveryHint.textContent).toContain('Email recovery is enabled for this installation');
    expect(recoveryHint.textContent).toContain('cannot confirm whether a reset request will be delivered');
    expect(recoveryButton.disabled).toBeFalse();
  });

  it('uses non-technical Google-unavailable copy without displaying configuration names', () => {
    const message = (component as any).authenticationErrorMessage('google_unavailable') as string;
    expect(message).toContain('Google sign-in is unavailable');
    expect(message).toContain('Use your email and password');
    expect(message).not.toMatch(/GOOGLE_OAUTH|CLIENT_SECRET|REDIRECT_URL/);
  });

  it('shows a disabled loading state while signing in, then presents a safe retryable error', () => {
    const pendingLogin = new Subject<void>();
    auth.login.and.returnValue(pendingLogin.asObservable());
    component.validateForm.patchValue({ userName: 'operator@example.com', password: 'wrong-password' });

    component.submitForm();
    fixture.detectChanges();

    const submit = fixture.nativeElement.querySelector('[data-testid="login-submit"]') as HTMLButtonElement;
    expect(submit.disabled).toBeTrue();
    expect(submit.classList.contains('ant-btn-loading')).toBeTrue();

    pendingLogin.error({ status: 401 });
    fixture.detectChanges();

    expect(submit.disabled).toBeFalse();
    expect(fixture.nativeElement.querySelector('.login-inline-error')?.textContent).toContain('email or password is incorrect');
  });
});
