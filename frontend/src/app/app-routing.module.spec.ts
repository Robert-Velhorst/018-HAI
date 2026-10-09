import { APP_ROUTES, AUTHENTICATED_PAGE_PATHS } from './app-routing.module'
import { authGuard } from './services/auth/guards/auth.guard'
import { RedirectIfLoggedGuard } from './services/auth/guards/login.guard'

describe('App routing', () => {
  const shellRoute = APP_ROUTES.find((route) => route.path === '')
  const shellChildren = shellRoute?.children ?? []

  it('retains the root redirect and all registered authenticated URLs', () => {
    expect(shellChildren.find((route) => route.path === '')).toEqual(
      jasmine.objectContaining({ redirectTo: 'control-center', pathMatch: 'full' }),
    )

    const registeredPaths = shellChildren
      .filter((route) => route.path && route.path !== '**')
      .map((route) => `/${route.path}`)
      .sort()
    expect(registeredPaths).toEqual([...AUTHENTICATED_PAGE_PATHS].sort())

    for (const path of AUTHENTICATED_PAGE_PATHS) {
      expect(shellChildren.find((route) => route.path === path.slice(1))?.canActivate)
        .withContext(`${path} must remain guarded`)
        .toContain(authGuard)
    }

    expect(APP_ROUTES.find((route) => route.path === 'login')?.canActivate)
      .toContain(RedirectIfLoggedGuard)
  })

  it('uses the guarded wildcard for a not-found page instead of redirecting', () => {
    const wildcard = shellChildren.find((route) => route.path === '**')

    expect(shellChildren[shellChildren.length - 1]?.path).toBe('**')
    expect(wildcard?.redirectTo).toBeUndefined()
    expect(wildcard?.loadComponent).toEqual(jasmine.any(Function))
    expect(wildcard?.canActivate).toContain(authGuard)
  })
})
