import {Injectable} from '@angular/core';
import { HttpClient, HttpHeaders, HttpParams } from '@angular/common/http';
import {catchError, map, timeout} from 'rxjs/operators';
import {IAuthCapabilities, IAuthService} from "../auth.service.interface";
import {Observable, of, throwError} from "rxjs";
import {HttpErrorResponse} from '@angular/common/http';
import {IUserModel} from "../../models/user.model.interface";
import {HttpTimeoutPolicy} from '../../shared/http-timeout-policy';


@Injectable({
    providedIn: 'root'
})
export class AuthService implements IAuthService {
    // Use the same bounded read budget as other authenticated API checks. A
    // shorter guard-only timeout can reject a valid session before the shared
    // HTTP read deadline has elapsed.
    static readonly authenticationCheckTimeoutMs = HttpTimeoutPolicy.readMs;
    // Capabilities only control optional sign-in providers. Bound the request
    // so an unavailable gateway cannot leave their status and retry action pending.
    static readonly capabilitiesTimeoutMs = 5000;
    private apiUrl = '/api/v1/auth';

    constructor(private http: HttpClient) {
    }

    getCapabilities(): Observable<IAuthCapabilities> {
        return this.http.get<IAuthCapabilities>(`${this.apiUrl}/capabilities`).pipe(
            timeout(AuthService.capabilitiesTimeoutMs)
        );
    }

    openLocalPreview(): Observable<void> {
        return this.http.post<void>(`${this.apiUrl}/local-preview`, {});
    }

    login(email: string, password: string, rememberMe = true): Observable<void> {
        return this.http.post<void>(`${this.apiUrl}/login`, {email, password, rememberMe});
    }

    register(email: string, password: string): Observable<IUserModel> {
        return this.http.post<IUserModel>(`${this.apiUrl}/register`, {email, password});
    }

    requestPasswordReset(email: string): Observable<void> {
        return this.http.post<void>(
            `${this.apiUrl}/request-password-reset`,
            new HttpParams().set('email', email).toString(),
            {headers: new HttpHeaders({'Content-Type': 'application/x-www-form-urlencoded'})}
        );
    }

    confirmPasswordReset(token: string, newPassword: string): Observable<void> {
        return this.http.post<void>(
            `${this.apiUrl}/confirm-password-reset`,
            {token, newPassword}
        );
    }


    logout() {
        return this.http.post<void>(`${this.apiUrl}/logout`, {});
    }

    loggedIn() {
        return this.http.get(`${this.apiUrl}/is-user-authenticated`).pipe(
            timeout(AuthService.authenticationCheckTimeoutMs),
            map(() => true),
            catchError((error: unknown) => {
                if (error instanceof HttpErrorResponse && error.status === 401) {
                    return of(false);
                }
                return throwError(() => error);
            })
        );
    }
}
