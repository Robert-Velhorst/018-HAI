import { Injectable } from '@angular/core';
import { HttpEvent, HttpHandler, HttpInterceptor, HttpRequest } from '@angular/common/http';
import { Observable, timeout } from 'rxjs';

@Injectable()
export class RequestTimeoutInterceptor implements HttpInterceptor {
  static readonly readTimeoutMs = 8_000;
  static readonly operationTimeoutMs = 30_000;

  intercept(request: HttpRequest<unknown>, next: HttpHandler): Observable<HttpEvent<unknown>> {
    const duration = request.method === 'GET'
      ? RequestTimeoutInterceptor.readTimeoutMs
      : RequestTimeoutInterceptor.operationTimeoutMs;
    return next.handle(request).pipe(timeout(duration));
  }
}
