import { ChangeDetectionStrategy, Component, Inject, OnInit } from "@angular/core";
import { FormBuilder, FormGroup, Validators } from "@angular/forms";
import { NzNotificationService } from "ng-zorro-antd/notification";
import { AUTH_SERVICE_TOKEN } from "../../services/auth/auth.service.token";
import { IAuthService } from "../../services/auth.service.interface";
import { ActivatedRoute, Router } from "@angular/router";
import { TimeoutError, timeout } from 'rxjs';
import { safePostOnboardingDestination } from '../../services/auth/guards/safe-return-url';

@Component({
    changeDetection: ChangeDetectionStrategy.Eager,
    selector: "app-login",
    templateUrl: "./login.component.html",
    styleUrls: ["./login.component.scss"],
    standalone: false
})
export class LoginComponent implements OnInit {
  readonly minimumRegistrationPasswordLength = 12;
  readonly maximumRegistrationPasswordBytes = 72;
  hidePassword: boolean = true;
  registrationMode = false;
  recoveryMode = false;
  recoveryStep: 'request' | 'confirm' = 'request';
  registering = false;
  recovering = false;
  signingIn = false;
  signInErrorMessage = '';
  authenticationUnavailable = false;
  authenticationRetrying = false;
  private fieldValidationMessages: Record<string, string> = {};
  readonly signInTimeoutMs = 15000;
  readonly passwordRecoveryTimeoutMs = 45000;
  authCapabilitiesLoaded = false;
  authCapabilitiesLoading = false;
  authCapabilitiesUnavailable = false;
  googleLoginEnabled = false;
  passwordRecoveryEmailEnabled = false;
  localPreviewEnabled = false;
  openingLocalPreview = false;
  validateForm: FormGroup = this.fb.group({});

  constructor(
    private fb: FormBuilder,
    private notification: NzNotificationService,
    private router: Router,
    private route: ActivatedRoute,
    @Inject(AUTH_SERVICE_TOKEN) private authService: IAuthService
  ) {}

  ngOnInit(): void {
    this.onInitForm();
    const authError = this.route.snapshot.queryParamMap.get('error');
    this.signInErrorMessage = this.authenticationErrorMessage(authError);
    this.authenticationUnavailable = authError === 'service_unavailable';
    this.loadAuthCapabilities();
  }

  retryAuthentication(): void {
    if (this.authenticationRetrying || this.signingIn || this.registering || this.recovering) {
      return;
    }
    this.authenticationRetrying = true;
    this.authService.loggedIn().pipe(timeout(this.signInTimeoutMs)).subscribe({
      next: (authenticated) => {
        this.authenticationRetrying = false;
        if (!authenticated) {
          this.authenticationUnavailable = false;
          this.signInErrorMessage = 'The authentication service is responding. Sign in with your account to continue.';
          return;
        }

        this.authenticationUnavailable = false;
        this.signInErrorMessage = '';
        this.router.navigateByUrl(this.authenticationDestination()).catch(() => {
          this.signInErrorMessage = 'Authentication was confirmed, but HAI could not open the requested page. Retry the connection or sign in again.';
        });
      },
      error: () => {
        this.authenticationRetrying = false;
        this.authenticationUnavailable = true;
        this.signInErrorMessage = this.authenticationErrorMessage('service_unavailable');
      },
    });
  }

  private authenticationErrorMessage(code: string | null): string {
    switch (code) {
      case 'service_unavailable':
        return 'HAI could not reach its local authentication service. This is a service connection problem, not a password error. Start the HAI services and retry.';
      case 'google_unavailable':
        return 'Google sign-in is unavailable on this HAI installation. Use your email and password, or contact the HAI administrator.';
      case 'google_denied':
        return 'Google sign-in was cancelled or not approved. Try again or use password sign-in.';
      case 'google_failed':
        return 'Google sign-in could not be completed. Try password sign-in or contact the HAI administrator.';
      default:
        return '';
    }
  }

  loadAuthCapabilities(): void {
    if (this.authCapabilitiesLoading) {
      return;
    }
    this.authCapabilitiesLoading = true;
    this.authCapabilitiesLoaded = false;
    this.authService.getCapabilities().subscribe({
      next: (capabilities) => {
        this.googleLoginEnabled = Boolean(capabilities.googleLoginEnabled);
        this.passwordRecoveryEmailEnabled = Boolean(capabilities.passwordRecoveryEmailEnabled);
        this.localPreviewEnabled = Boolean(capabilities.localPreviewEnabled);
        this.authCapabilitiesLoaded = true;
        this.authCapabilitiesUnavailable = false;
        this.authCapabilitiesLoading = false;
      },
      error: () => {
        this.authCapabilitiesUnavailable = true;
        this.authCapabilitiesLoading = false;
      },
    });
  }

  onInitForm() {
    this.validateForm = this.fb.group({
      userName: [
        "",
        {
          validators: [Validators.required, Validators.email],
        },
      ],
      password: ["", { updateOn: "submit", validators: [Validators.required] }],
      confirmPassword: [""],
      resetToken: [""],
      remember: [true],
    });
  }

  // Full-page redirect into the IDP's Google OAuth flow. It is not an XHR: the
  // browser must follow Google's redirects and land back on the app with the
  // session cookies set by the callback.
  loginWithGoogle(): void {
    if (this.registering || this.recovering || this.signingIn || this.openingLocalPreview) {
      return;
    }
    window.location.href = this.googleLoginURL();
  }

  private googleLoginURL(): string {
    const rememberMe = Boolean(this.validateForm.value.remember);
    return `/api/v1/auth/google/login?returnUrl=${encodeURIComponent(this.authenticationDestination())}&rememberMe=${rememberMe}`;
  }

  openLocalPreview(): void {
    if (this.openingLocalPreview || this.registering || this.recovering || this.signingIn) {
      return;
    }
    this.openingLocalPreview = true;
    this.authService.openLocalPreview().pipe(timeout(this.signInTimeoutMs)).subscribe({
      next: () => {
        try {
          this.router.navigateByUrl(this.authenticationDestination()).then(
            (navigated) => {
              this.openingLocalPreview = false;
              const currentPath = String(this.router.url || '').split(/[?#]/, 1)[0];
              if (!navigated || currentPath === '/login') {
                this.notification.error('Local dashboard did not open', 'HAI could not confirm a local session. Refresh the app and try again.');
              }
            },
            () => {
              this.openingLocalPreview = false;
              this.notification.error('Local dashboard did not open', 'HAI could not open the requested page. Refresh the app and try again.');
            }
          );
        } catch {
          this.openingLocalPreview = false;
          this.notification.error('Local dashboard did not open', 'HAI could not open the requested page. Refresh the app and try again.');
        }
      },
      error: (error) => {
        this.openingLocalPreview = false;
        this.notification.error(
          'Local preview unavailable',
          error instanceof TimeoutError
            ? 'HAI did not receive confirmation of local access in time. Check the connection or retry sign-in.'
            : 'This device is not configured for login-free local access.'
        );
      },
    });
  }

  submitForm(): void {
    if (this.registering || this.recovering || this.signingIn || this.openingLocalPreview) {
      return;
    }
    this.clearSignInError();
    if (this.recoveryMode) {
      if (this.recoveryStep === 'confirm') {
        this.confirmPasswordReset();
      } else {
        this.requestPasswordReset();
      }
      return;
    }
    if (this.registrationMode) {
      this.registerAccount();
      return;
    }
    if (!this.validateForm.valid) {
      for (const i in this.validateForm.controls) {
        this.validateForm.controls[i].markAsDirty();
        this.validateForm.controls[i].updateValueAndValidity();
      }
      return;
    }

    this.signingIn = true;
    this.authService
      .login(
        this.validateForm.value.userName,
        this.validateForm.value.password,
        Boolean(this.validateForm.value.remember)
      )
      .pipe(timeout(this.signInTimeoutMs))
      .subscribe({
        next: () => {
          try {
            this.router.navigateByUrl(this.authenticationDestination()).then(
              (navigated) => {
                this.signingIn = false;
                const currentPath = String(this.router.url || '').split(/[?#]/, 1)[0];
                if (!navigated || currentPath === '/login') {
                  this.reportSignInFailure(
                    'Session not confirmed',
                    'Your sign-in was accepted, but HAI could not confirm your session. Check the connection and try again.'
                  );
                }
              },
              () => {
                this.signingIn = false;
                this.reportSignInFailure(
                  'Workspace did not open',
                  'Your sign-in was accepted, but HAI could not open the requested page. Refresh the app and try again.'
                );
              }
            );
          } catch {
            this.signingIn = false;
            this.reportSignInFailure(
              'Workspace did not open',
              'Your sign-in was accepted, but HAI could not open the requested page. Refresh the app and try again.'
            );
          }
        },
        error: (error) => {
          this.signingIn = false;
          if (error.status === 401) {
            this.reportSignInFailure(
              'Login failed',
              'The email or password is incorrect. Check both fields and try again.'
            );
          } else if (error instanceof TimeoutError) {
            this.reportSignInFailure(
              'Sign-in timed out',
              'HAI did not receive a sign-in response in time. Check the connection and try again.'
            );
          } else {
            this.reportSignInFailure(
              'Sign-in unavailable',
              'HAI could not reach the sign-in service. Check that the app is connected, then try again.'
            );
          }
        },
      });
  }

  clearSignInError(): void {
    this.signInErrorMessage = '';
  }

  private reportSignInFailure(title: string, message: string): void {
    this.signInErrorMessage = message;
    this.notification.create('error', title, message);
  }

  // Only route within this Angular application after authentication. The value
  // comes from the route guard, but validating it here prevents a hand-edited
  // login URL from becoming an open redirect.
  private authenticationDestination(): string {
    return safePostOnboardingDestination(
      this.route.snapshot.queryParamMap.get('returnUrl'),
    ) ?? '/control-center';
  }

  toggleRegistration(): void {
    if (this.registering || this.recovering || this.signingIn || this.openingLocalPreview) {
      return;
    }
    this.clearSignInError();
    this.fieldValidationMessages = {};
    this.hidePassword = true;
    this.recoveryMode = false;
    this.registrationMode = !this.registrationMode;
    this.clearValidationError(this.validateForm.controls['userName'], 'accountExists');
    this.validateForm.controls['password'].reset();
    this.validateForm.controls['confirmPassword'].reset();
    this.validateForm.controls['password'].markAsPristine();
    this.validateForm.controls['confirmPassword'].markAsPristine();
    this.validateForm.controls['password'].markAsUntouched();
    this.validateForm.controls['confirmPassword'].markAsUntouched();
  }

  passwordErrorTip(): string {
    if (this.fieldValidationMessages['password']) {
      return this.fieldValidationMessages['password'];
    }
    const passwordControl = this.validateForm.controls['password'];
    if ((this.registrationMode || this.recoveryStep === 'confirm') && passwordControl.hasError('minLength')) {
      return `Use at least ${this.minimumRegistrationPasswordLength} characters.`;
    }
    return 'Please input your password!';
  }

  confirmationErrorTip(): string {
    if (this.fieldValidationMessages['confirmPassword']) {
      return this.fieldValidationMessages['confirmPassword'];
    }
    const confirmationControl = this.validateForm.controls['confirmPassword'];
    if (confirmationControl.hasError('mismatch')) {
      return 'Passwords do not match.';
    }
    return 'Please confirm your password.';
  }

  emailErrorTip(): string {
    if (this.fieldValidationMessages['userName']) {
      return this.fieldValidationMessages['userName'];
    }
    if (this.registrationMode && this.validateForm.controls['userName'].hasError('accountExists')) {
      return 'This email is already registered. Log in instead.';
    }
    return 'Please input a valid email!';
  }

  isFieldInvalid(controlName: string): boolean {
    const control = this.validateForm.controls[controlName];
    return Boolean(this.fieldValidationMessages[controlName]) || Boolean(control && control.invalid && (control.dirty || control.touched));
  }

  fieldStatus(controlName: string): any {
    return this.fieldValidationMessages[controlName] ? 'error' : this.validateForm.controls[controlName];
  }

  onEmailInput(): void {
    this.clearFieldValidationMessage('userName');
    this.clearValidationError(this.validateForm.controls['userName'], 'accountExists');
    this.clearSignInError();
  }

  onPasswordInput(): void {
    this.clearFieldValidationMessage('password');
    this.clearFieldValidationMessage('confirmPassword');
    this.clearSignInError();
  }

  onConfirmationInput(): void {
    this.clearFieldValidationMessage('confirmPassword');
  }

  onResetTokenInput(): void {
    this.clearFieldValidationMessage('resetToken');
  }

  private clearFieldValidationMessage(controlName: string): void {
    delete this.fieldValidationMessages[controlName];
  }

  private passwordCharacterCount(password: string): number {
    return Array.from(password).length;
  }

  private passwordByteCount(password: string): number {
    return new TextEncoder().encode(password).length;
  }

  private requestPasswordReset(): void {
    if (!this.authCapabilitiesLoaded || this.authCapabilitiesUnavailable) {
      this.notification.warning(
        'Recovery status unavailable',
        'Retry the sign-in service check before requesting a reset code. Password sign-in remains available.'
      );
      return;
    }
    if (!this.passwordRecoveryEmailEnabled) {
      this.notification.warning(
        'Email recovery is unavailable',
        'Email password recovery is turned off for this HAI installation. No reset email was requested. Use password sign-in or contact the HAI administrator.'
      );
      return;
    }
    const emailControl = this.validateForm.controls['userName'];
    emailControl.updateValueAndValidity();
    if (emailControl.invalid) {
      emailControl.markAsDirty();
      emailControl.markAsTouched();
      return;
    }

    this.recovering = true;
    this.authService.requestPasswordReset(String(emailControl.value).trim()).pipe(timeout(this.passwordRecoveryTimeoutMs)).subscribe({
      next: () => this.finishRecoveryRequest(),
      // Keep the response identical when delivery is temporarily unavailable or the email is unknown.
      error: (error) => {
        this.recovering = false;
        if (error?.status === 429) {
          this.notification.warning(
            'Too many recovery requests',
            `${this.recoveryRateLimitDelay(error?.headers?.get?.('Retry-After'))} This does not indicate whether the address has an account.`
          );
          return;
        }
        this.notification.error(
          error instanceof TimeoutError ? 'Recovery request timed out' : 'Recovery request failed',
          error instanceof TimeoutError
            ? 'HAI did not receive confirmation from the recovery service in time. Try again later; this does not confirm whether the address has an account.'
            : 'HAI could not contact the recovery service. Please try again. This does not confirm whether the address has an account.'
        );
      },
    });
  }

  private recoveryRateLimitDelay(retryAfter: unknown): string {
    const seconds = Number(retryAfter);
    if (!Number.isFinite(seconds) || seconds <= 0) {
      return 'Wait briefly before trying again.';
    }
    const roundedSeconds = Math.ceil(seconds);
    if (roundedSeconds >= 60) {
      const minutes = Math.ceil(roundedSeconds / 60);
      return `Wait about ${minutes} minute${minutes === 1 ? '' : 's'} before trying again.`;
    }
    return `Wait ${roundedSeconds} second${roundedSeconds === 1 ? '' : 's'} before trying again.`;
  }

  private finishRecoveryRequest(): void {
    this.recovering = false;
    this.recoveryStep = 'confirm';
    this.notification.info(
      'Recovery request received',
      'For privacy, HAI does not confirm whether an account matches or whether email was delivered. The settings check does not test provider connectivity. If a code does not arrive, check spam and ask the HAI operator to inspect the redacted SMTP stage in IDP logs and the provider delivery status.'
    );
  }

  private confirmPasswordReset(): void {
    const tokenControl = this.validateForm.controls['resetToken'];
    const passwordControl = this.validateForm.controls['password'];
    const confirmationControl = this.validateForm.controls['confirmPassword'];
    const token = String(tokenControl.value || '').trim();
    const password = String(passwordControl.value || '');
    const confirmation = String(confirmationControl.value || '');

    this.fieldValidationMessages = {};
    this.clearValidationError(tokenControl, 'required');
    this.clearValidationError(passwordControl, 'minLength');
    this.clearValidationError(confirmationControl, 'mismatch');
    this.clearValidationError(confirmationControl, 'required');
    if (!token) {
      this.fieldValidationMessages['resetToken'] = 'Enter the one-time reset code.';
    }
    if (this.passwordCharacterCount(password) < this.minimumRegistrationPasswordLength) {
      this.fieldValidationMessages['password'] = `Use at least ${this.minimumRegistrationPasswordLength} characters.`;
    } else if (this.passwordByteCount(password) > this.maximumRegistrationPasswordBytes) {
      this.fieldValidationMessages['password'] = `Use no more than ${this.maximumRegistrationPasswordBytes} UTF-8 bytes.`;
    }
    if (!confirmation) {
      this.fieldValidationMessages['confirmPassword'] = 'Please confirm your new password.';
    } else if (password !== confirmation) {
      this.fieldValidationMessages['confirmPassword'] = 'Passwords do not match.';
    }

    if (Object.keys(this.fieldValidationMessages).length > 0 || passwordControl.invalid) {
      [tokenControl, passwordControl, confirmationControl].forEach((control) => {
        control.markAsDirty();
        control.markAsTouched();
      });
      this.notification.error('Check your reset details', this.resetValidationSummary());
      return;
    }

    this.recovering = true;
    this.authService.confirmPasswordReset(token, password).pipe(timeout(this.passwordRecoveryTimeoutMs)).subscribe({
      next: () => {
        this.recovering = false;
        this.backToLogin();
        this.notification.success('Password reset', 'Your password has been updated. Sign in with your new password.');
      },
      error: (error) => {
        this.recovering = false;
        this.notification.error(
          error instanceof TimeoutError ? 'Reset confirmation timed out' : 'Reset could not be completed',
          error instanceof TimeoutError
            ? 'HAI did not confirm the password change in time. Try signing in with the new password before requesting another reset.'
            : error?.status === 400
              ? `The reset code was not accepted, or the password is outside the ${this.minimumRegistrationPasswordLength}-character to ${this.maximumRegistrationPasswordBytes}-byte limit. Check the details or request a new code.`
              : 'HAI could not complete the password reset. Check the connection and try again; if you already submitted, try signing in with the new password.'
        );
      },
    });
  }

  private resetValidationSummary(): string {
    if (this.fieldValidationMessages['resetToken']) {
      return 'Enter the one-time reset code.';
    }
    if (this.fieldValidationMessages['password']) {
      return this.fieldValidationMessages['password'];
    }
    if (this.fieldValidationMessages['confirmPassword']) {
      return this.fieldValidationMessages['confirmPassword'] === 'Passwords do not match.'
        ? 'The confirmation password does not match.'
        : this.fieldValidationMessages['confirmPassword'];
    }
    return 'Confirm your new password.';
  }

  private registerAccount(): void {
    const email = String(this.validateForm.value.userName || '').trim();
    const password = String(this.validateForm.value.password || '');
    const confirmation = String(this.validateForm.value.confirmPassword || '');
    const emailControl = this.validateForm.controls['userName'];
    const passwordControl = this.validateForm.controls['password'];
    const confirmationControl = this.validateForm.controls['confirmPassword'];
    emailControl.updateValueAndValidity();

    this.fieldValidationMessages = {};
    this.clearValidationError(passwordControl, 'minLength');
    this.clearValidationError(confirmationControl, 'mismatch');
    this.clearValidationError(confirmationControl, 'required');
    if (this.passwordCharacterCount(password) < this.minimumRegistrationPasswordLength) {
      this.fieldValidationMessages['password'] = `Use at least ${this.minimumRegistrationPasswordLength} characters.`;
    } else if (this.passwordByteCount(password) > this.maximumRegistrationPasswordBytes) {
      this.fieldValidationMessages['password'] = `Use no more than ${this.maximumRegistrationPasswordBytes} UTF-8 bytes.`;
    }
    if (!confirmation) {
      this.fieldValidationMessages['confirmPassword'] = 'Please confirm your password.';
    } else if (password !== confirmation) {
      this.fieldValidationMessages['confirmPassword'] = 'Passwords do not match.';
    }

    if (!email || emailControl.invalid || passwordControl.invalid || Object.keys(this.fieldValidationMessages).length > 0) {
      [emailControl, passwordControl, confirmationControl].forEach((control) => {
        control.markAsDirty();
        control.markAsTouched();
      });
      this.notification.error('Check your sign-up details', this.signupValidationSummary());
      return;
    }
    this.registering = true;
    this.authService.register(email, password).pipe(timeout(this.signInTimeoutMs)).subscribe({
      next: () => {
        this.registering = false;
        this.registrationMode = false;
        this.hidePassword = true;
        this.validateForm.patchValue({ userName: email, password: '', confirmPassword: '' });
        this.notification.success('Account created', 'Your local operator account is ready. Sign in to continue.');
      },
      error: (error) => {
        this.registering = false;
        if (error?.status === 409) {
          this.fieldValidationMessages['userName'] = 'This email is already registered. Log in instead.';
          emailControl.markAsDirty();
          emailControl.markAsTouched();
          this.notification.error('Account already exists', 'This email is already registered. Log in instead or use password recovery.');
          return;
        }
        if (error instanceof TimeoutError) {
          this.notification.error(
            'Sign-up status unknown',
            'HAI did not confirm whether the account was created. Try signing in with this email before submitting sign-up again.'
          );
          return;
        }
        if (error?.status === 400) {
          this.notification.error(
            'Sign-up details were rejected',
            `Check that the email is valid and the password is between ${this.minimumRegistrationPasswordLength} characters and ${this.maximumRegistrationPasswordBytes} UTF-8 bytes.`
          );
          return;
        }
        this.notification.error('Sign-up failed', 'HAI could not create this account. Check the connection and try again later.');
      },
    });
  }

  private clearValidationError(control: any, errorName: string): void {
    if (!control.hasError(errorName)) {
      return;
    }
    const errors = { ...control.errors };
    delete errors[errorName];
    control.setErrors(Object.keys(errors).length ? errors : null);
  }

  private signupValidationSummary(): string {
    const emailControl = this.validateForm.controls['userName'];
    if (this.fieldValidationMessages['userName']) {
      return this.fieldValidationMessages['userName'];
    }
    if (emailControl.invalid) {
      return 'Enter a valid email address.';
    }
    if (this.fieldValidationMessages['password']) {
      return this.fieldValidationMessages['password'];
    }
    if (this.fieldValidationMessages['confirmPassword']) {
      return this.fieldValidationMessages['confirmPassword'] === 'Passwords do not match.'
        ? 'The confirmation password does not match.'
        : this.fieldValidationMessages['confirmPassword'];
    }
    return 'Confirm your password to create the account.';
  }

  showPasswordHelp(): void {
    if (this.registering || this.recovering || this.signingIn || this.openingLocalPreview) {
      return;
    }
    this.fieldValidationMessages = {};
    this.registrationMode = false;
    this.recoveryMode = true;
    this.hidePassword = true;
    this.recoveryStep = 'request';
    this.validateForm.patchValue({ password: '', confirmPassword: '', resetToken: '' });
    ['password', 'confirmPassword', 'resetToken'].forEach((name) => {
      const control = this.validateForm.controls[name];
      control.setErrors(null);
      control.markAsPristine();
      control.markAsUntouched();
    });
  }

  showResetConfirmation(): void {
    if (this.recovering || this.signingIn) {
      return;
    }
    this.fieldValidationMessages = {};
    this.hidePassword = true;
    this.recoveryStep = 'confirm';
  }

  backToLogin(): void {
    if (this.registering || this.recovering || this.signingIn || this.openingLocalPreview) {
      return;
    }
    this.clearSignInError();
    this.fieldValidationMessages = {};
    this.hidePassword = true;
    this.registrationMode = false;
    this.recoveryMode = false;
    this.recoveryStep = 'request';
    this.validateForm.patchValue({ password: '', confirmPassword: '', resetToken: '' });
    ['password', 'confirmPassword', 'resetToken'].forEach((name) => {
      const control = this.validateForm.controls[name];
      control.setErrors(null);
      control.markAsPristine();
      control.markAsUntouched();
    });
  }
}
