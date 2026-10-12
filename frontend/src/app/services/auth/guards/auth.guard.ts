import {CanActivateFn, Router} from '@angular/router';
import {inject} from '@angular/core';
import {IAuthService} from "../../auth.service.interface";
import {AUTH_SERVICE_TOKEN} from "../auth.service.token";
import {catchError, map} from "rxjs/operators";
import {of} from 'rxjs';
import {isOnboardingComplete} from '../../../pages/onboarding/onboarding-state';
import {safeInternalReturnUrl} from './safe-return-url';

export const authGuard: CanActivateFn = (route, state) => {
    const authService: IAuthService = inject(AUTH_SERVICE_TOKEN);
    const router = inject(Router);
    return authService.loggedIn().pipe(
        map(authenticated => {
            if (authenticated) {
                if (route.routeConfig?.path === 'onboarding' || isOnboardingComplete()) {
                    return true;
                }
                const returnUrl = safeInternalReturnUrl(state.url);
                return router.createUrlTree(['/onboarding'], {
                    queryParams: returnUrl ? { returnUrl } : {},
                });
            }
            const returnUrl = safeInternalReturnUrl(state.url);
            return router.createUrlTree(['/login'], {
                queryParams: returnUrl ? { returnUrl } : {},
            });
        }),
        catchError(() => {
            const returnUrl = safeInternalReturnUrl(state.url);
            return of(router.createUrlTree(['/login'], {
                queryParams: {
                    ...(returnUrl ? { returnUrl } : {}),
                    error: 'service_unavailable',
                },
            }));
        })
    );
};
