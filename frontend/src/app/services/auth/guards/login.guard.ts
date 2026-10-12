import {CanActivateFn, Router} from '@angular/router';
import {inject} from '@angular/core';
import {IAuthService} from "../../auth.service.interface";
import {AUTH_SERVICE_TOKEN} from "../auth.service.token";
import {catchError, map} from "rxjs/operators";
import {of} from 'rxjs';
import {isOnboardingComplete} from '../../../pages/onboarding/onboarding-state';
import {safePostOnboardingDestination} from './safe-return-url';

export const RedirectIfLoggedGuard: CanActivateFn = (route, state) => {
    const authService: IAuthService = inject(AUTH_SERVICE_TOKEN);
    const router = inject(Router);
    // This marker is set only after an earlier authentication probe failed.
    // Render the retryable login state immediately instead of making the user
    // wait through a second full timeout on the redirected URL.
    if (route.queryParamMap.get('error') === 'service_unavailable') {
        return true;
    }

    return authService.loggedIn().pipe(
        map(authenticated => {
            if (authenticated) {
                const returnUrl = safePostOnboardingDestination(
                    route.queryParamMap.get('returnUrl'),
                );
                if (!isOnboardingComplete()) {
                    return router.createUrlTree(['/onboarding'], {
                        queryParams: returnUrl ? { returnUrl } : {},
                    });
                }
                return router.parseUrl(returnUrl ?? '/control-center');
            }
            return true;
        }),
        catchError(() => {
            const returnUrl = safePostOnboardingDestination(
                route.queryParamMap.get('returnUrl'),
            );
            return of(router.createUrlTree(['/login'], {
                queryParams: {
                    ...(returnUrl ? { returnUrl } : {}),
                    error: 'service_unavailable',
                },
            }));
        })
    );
};
