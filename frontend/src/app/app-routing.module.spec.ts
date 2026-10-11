import { authGuard } from './services/auth/guards/auth.guard';
import { APP_ROUTES, AUTHENTICATED_ROUTES } from './app-routing.module';

describe('AppRoutingModule', () => {
  it('guards the authenticated shell once instead of every child navigation', () => {
    const shell = APP_ROUTES.find((route) => route.path === '');

    expect(shell?.canActivate).toEqual([authGuard]);
    expect(shell?.children).toContain(AUTHENTICATED_ROUTES[0]);
    expect(AUTHENTICATED_ROUTES.every((route) => !route.canActivate)).toBeTrue();
  });

  it('keeps onboarding independently protected', () => {
    const onboarding = APP_ROUTES.find((route) => route.path === 'onboarding');

    expect(onboarding?.canActivate).toEqual([authGuard]);
  });
});
