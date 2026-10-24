import { FormBuilder } from '@angular/forms';
import { fakeAsync, flushMicrotasks, tick } from '@angular/core/testing';
import { ActivatedRoute, Router } from '@angular/router';
import { EMPTY, of, Subject } from 'rxjs';
import { LoginComponent } from './login.component';

describe('Login observation reconciliation', () => {
  let component: LoginComponent;
  let auth: jasmine.SpyObj<any>;
  let notification: jasmine.SpyObj<any>;
  let router: jasmine.SpyObj<Router>;
  const methods = ['getCapabilities', 'loggedIn', 'openLocalPreview', 'login', 'register', 'requestPasswordReset', 'confirmPasswordReset'];
  beforeEach(() => {
    auth = jasmine.createSpyObj('auth', methods);
    for (const method of methods) auth[method].and.returnValue(of(undefined));
    auth.getCapabilities.and.returnValue(of({ googleLoginEnabled: true, passwordRecoveryEmailEnabled: true, localPreviewEnabled: true }));
    auth.loggedIn.and.returnValue(of(false));
    notification = jasmine.createSpyObj('notifications', ['success', 'error', 'warning', 'info', 'create']);
    router = jasmine.createSpyObj<Router>('router', ['navigateByUrl']);
    router.navigateByUrl.and.returnValue(Promise.resolve(true));
    component = new LoginComponent(new FormBuilder(), notification, router,
      { snapshot: { queryParamMap: { get: () => null } } } as unknown as ActivatedRoute, auth);
    component.ngOnInit();
  });
  afterEach(() => component.ngOnDestroy());
  const actions = ['getCapabilities', 'loggedIn', 'openLocalPreview', 'login', 'register', 'requestPasswordReset', 'confirmPasswordReset'] as const;
  function dispatch(method: typeof actions[number]): void {
    component.validateForm.patchValue({ userName: 'operator@example.com', password: 'synthetic-passphrase', confirmPassword: 'synthetic-passphrase', resetToken: 'synthetic-code' });
    switch (method) {
      case 'getCapabilities': component.loadAuthCapabilities(); break;
      case 'loggedIn': component.retryAuthentication(); break;
      case 'openLocalPreview': component.openLocalPreview(); break;
      case 'login': component.submitForm(); break;
      case 'register': component.toggleRegistration(); component.validateForm.patchValue({ password: 'synthetic-passphrase', confirmPassword: 'synthetic-passphrase' }); component.submitForm(); break;
      case 'requestPasswordReset': component.showPasswordHelp(); component.submitForm(); break;
      case 'confirmPasswordReset': component.showPasswordHelp(); component.showResetConfirmation(); component.validateForm.patchValue({ password: 'synthetic-passphrase', confirmPassword: 'synthetic-passphrase', resetToken: 'synthetic-code' }); component.submitForm(); break;
    }
  }
  function expectIdle(): void {
    expect(component.authCapabilitiesLoading).toBeFalse();
    expect(component.authenticationRetrying).toBeFalse();
    expect(component.openingLocalPreview).toBeFalse();
    expect(component.signingIn).toBeFalse();
    expect(component.registering).toBeFalse();
    expect(component.recovering).toBeFalse();
  }
  for (const method of actions) {
    it(`contains synchronous ${method} request setup failures`, () => {
      auth[method].and.callFake(() => { throw new Error('PRIVATE_SETUP_DETAIL'); });
      expect(() => dispatch(method)).not.toThrow();
      expectIdle();
      const feedback = ['success', 'error', 'warning', 'info', 'create'].map(name => notification[name].calls.allArgs());
      expect(JSON.stringify(feedback)).not.toContain('PRIVATE_SETUP_DETAIL');
    });
    it(`does not leave ${method} busy after an empty response`, () => {
      auth[method].and.returnValue(EMPTY);
      dispatch(method);
      expectIdle();
      expect(router.navigateByUrl).not.toHaveBeenCalled();
      expect(notification.success).not.toHaveBeenCalled();
      if (method === 'getCapabilities') expect(component.authCapabilitiesUnavailable).toBeTrue();
      if (method === 'loggedIn') expect(component.authenticationUnavailable).toBeTrue();
    });
    it(`discards pending ${method} observations when the login view is destroyed`, () => {
      const pending = new Subject<never>();
      auth[method].and.returnValue(pending);
      dispatch(method);
      expect(pending.observers.length).toBe(1);
      (component as any).ngOnDestroy();
      expect(pending.observers.length).toBe(0);
      pending.next(undefined as never);
      expect(router.navigateByUrl).not.toHaveBeenCalled();
      expect(notification.success).not.toHaveBeenCalled();
      expectIdle();
    });
  }
  it('does not dispatch new auth calls after destruction', () => {
    (component as any).ngOnDestroy();
    for (const method of methods) auth[method].calls.reset();
    for (const method of actions) dispatch(method);
    for (const method of methods) expect(auth[method]).not.toHaveBeenCalled();
  });
  it('does not coerce string flags into enabled authentication capabilities', () => {
    auth.getCapabilities.and.returnValue(of({ googleLoginEnabled: 'false', passwordRecoveryEmailEnabled: 'false', localPreviewEnabled: 'false' }));
    component.loadAuthCapabilities();
    expect(component.authCapabilitiesLoaded).toBeFalse();
    expect(component.authCapabilitiesUnavailable).toBeTrue();
    expect(component.googleLoginEnabled).toBeFalse();
    expect(component.passwordRecoveryEmailEnabled).toBeFalse();
    expect(component.localPreviewEnabled).toBeFalse();
  });
  it('accepts only one login acknowledgement and stops observing the stream', fakeAsync(() => {
    const pending = new Subject<void>();
    auth.login.and.returnValue(pending);
    dispatch('login');
    pending.next(); pending.next();
    flushMicrotasks();
    expect(pending.observers.length).toBe(0);
    expect(router.navigateByUrl).toHaveBeenCalledTimes(1);
    expectIdle();
  }));
  it('reports a cancelled authentication retry navigation', fakeAsync(() => {
    auth.loggedIn.and.returnValue(of(true));
    router.navigateByUrl.and.returnValue(Promise.resolve(false));
    component.retryAuthentication();
    flushMicrotasks();
    expect(component.signInErrorMessage).toContain('could not open');
    expectIdle();
  }));
  it('bounds a silent capability service without requiring the concrete HTTP implementation', fakeAsync(() => {
    const pending = new Subject<never>();
    auth.getCapabilities.and.returnValue(pending);
    component.loadAuthCapabilities();
    tick(5001);
    expect(pending.observers.length).toBe(0);
    expect(component.authCapabilitiesUnavailable).toBeTrue();
    expectIdle();
  }));
  it('does not treat a truthy non-boolean session response as authentication', () => {
    auth.loggedIn.and.returnValue(of('true'));
    component.retryAuthentication();
    expect(router.navigateByUrl).not.toHaveBeenCalled();
    expect(component.authenticationUnavailable).toBeTrue();
    expectIdle();
  });
  it('does not overlap session retry with another authentication action', () => {
    const pending = new Subject<boolean>();
    auth.loggedIn.and.returnValue(pending);
    component.retryAuthentication();
    dispatch('login');
    component.openLocalPreview();
    component.toggleRegistration();
    component.retryAuthentication();
    expect(auth.loggedIn).toHaveBeenCalledTimes(1);
    expect(auth.login).not.toHaveBeenCalled();
    expect(auth.openLocalPreview).not.toHaveBeenCalled();
    expect(component.registrationMode).toBeFalse();
  });
  for (const method of ['login', 'openLocalPreview'] as const) {
    it(`ignores late ${method} navigation feedback after destruction`, fakeAsync(() => {
      let finish!: (value: boolean) => void;
      router.navigateByUrl.and.returnValue(new Promise<boolean>(resolve => { finish = resolve; }));
      dispatch(method);
      component.ngOnDestroy();
      finish(false);
      flushMicrotasks();
      expect(notification.error).not.toHaveBeenCalled();
      expect(notification.create).not.toHaveBeenCalled();
      expectIdle();
    }));
  }
});
