import { OnboardingComponent } from './onboarding.component';

describe('OnboardingComponent', () => {
  const router = { navigateByUrl: jasmine.createSpy('navigateByUrl') } as any;
  let requestedReturnUrl: string | null;
  const route = {
    snapshot: {
      queryParamMap: {
        get: (key: string) => key === 'returnUrl' ? requestedReturnUrl : null,
      },
    },
  } as any;

  beforeEach(() => {
    router.navigateByUrl.calls.reset();
    requestedReturnUrl = null;
    localStorage.removeItem('hai_onboarded');
  });

  it('starts at the first step and clamps at both ends', () => {
    const c = new OnboardingComponent(router, route);
    expect(c.current).toBe(0);
    c.prev();
    expect(c.current).toBe(0);
    c.next();
    expect(c.current).toBe(1);
  });

  it('discloses enabled external services instead of promising unconditional local data isolation', () => {
    const component = new OnboardingComponent(router, route);
    expect(component.steps[0].description).toContain('external');
    expect(component.steps[0].description).toContain('data');
    expect(component.steps[0].description).not.toContain('Your data stays on your machine');
  });

  it('describes configured approval policy rather than promising approval of every side effect', () => {
    const component = new OnboardingComponent(router, route);
    expect(component.steps[2].description).toContain('configured');
    expect(component.steps[2].description).toContain('approval policy');
    expect(component.steps[2].description).not.toContain('Nothing with real side effects');
  });

  it('recognizes the last step', () => {
    const c = new OnboardingComponent(router, route);
    c.current = c.steps.length - 1;
    expect(c.isLastStep).toBeTrue();
    c.next();
    expect(c.current).toBe(c.steps.length - 1);
  });

  it('finish marks onboarded and returns to the originally requested internal route', () => {
    requestedReturnUrl = '/connected-sources?source=gmail';
    const c = new OnboardingComponent(router, route);
    c.finish();
    expect(localStorage.getItem('hai_onboarded')).toBe('true');
    expect(router.navigateByUrl).toHaveBeenCalledWith('/connected-sources?source=gmail');
    expect(OnboardingComponent.isOnboarded()).toBeTrue();
  });

  it('uses the control center when no return destination was requested', () => {
    const c = new OnboardingComponent(router, route);
    c.finish();
    expect(router.navigateByUrl).toHaveBeenCalledWith('/control-center');
  });

  it('rejects external return destinations and uses the control center', () => {
    requestedReturnUrl = 'https://example.com/redirect';
    const c = new OnboardingComponent(router, route);
    c.finish();
    expect(router.navigateByUrl).toHaveBeenCalledWith('/control-center');
  });

  it('rejects protocol-relative, backslash, and auth-loop destinations', () => {
    for (const unsafe of ['//example.com', '/\\\\example.com', '/%2f%2fevil.test', '/onboarding']) {
      requestedReturnUrl = unsafe;
      router.navigateByUrl.calls.reset();
      new OnboardingComponent(router, route).finish();
      expect(router.navigateByUrl).toHaveBeenCalledWith('/control-center');
    }
  });

  it('uses the shared theme token for its explanatory text', () => {
    const styles = (OnboardingComponent as any).ɵcmp.styles.join('\n');
    // Angular 22 namespaces CSS custom properties in compiled component styles.
    expect(styles).toMatch(/color:\s*var\(--(?:%NS%)?hai-muted\)/);
  });
});
