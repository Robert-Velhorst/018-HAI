import {Injectable} from '@angular/core';
import { HttpClient, HttpHeaders, HttpParams } from '@angular/common/http';
import {catchError, map} from 'rxjs/operators';
import {IAuthCapabilities, IAuthService} from "../auth.service.interface";
import {Observable, of} from "rxjs";
import {IUserModel} from "../../models/user.model.interface";


@Injectable({
    providedIn: 'root'
})
export class AuthService implements IAuthService {
    private apiUrl = '/api/v1/auth';

    constructor(private http: HttpClient) {
    }

    getCapabilities(): Observable<IAuthCapabilities> {
        return this.http.get<IAuthCapabilities>(`${this.apiUrl}/capabilities`);
    }

    openLocalPreview(): Observable<void> {
        return this.http.post<void>(`${this.apiUrl}/local-preview`, {});
    }

    login(email: string, password: string) {
        return this.http.post(`${this.apiUrl}/login`, {email, password});
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
            `${this.apiUrl}/confirm-password-reset/${encodeURIComponent(token)}`,
            new HttpParams().set('newPassword', newPassword).toString(),
            {headers: new HttpHeaders({'Content-Type': 'application/x-www-form-urlencoded'})}
        );
    }


    logout() {
        return this.http.get<void>(`${this.apiUrl}/logout`);
    }

    loggedIn() {
        return this.http.get<{authenticated?: boolean}>(`${this.apiUrl}/session`).pipe(
            map((session) => session.authenticated === true),
            catchError(() => of(false))
        );
    }
}
