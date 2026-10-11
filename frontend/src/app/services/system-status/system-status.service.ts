import { Injectable } from '@angular/core';
import { HttpClient, HttpErrorResponse } from '@angular/common/http';
import { Observable, throwError } from 'rxjs';
import { catchError } from 'rxjs/operators';
import { ISystemStatusService } from './system-status.service.interface';
import {
  IA2ABridgeStatus,
  IEventDeliveryRetryResult,
  IEventDeliveryStats,
  ISystemReadiness,
} from '../../models/system-status.model.interface';

@Injectable({
  providedIn: 'root',
})
export class SystemStatusService implements ISystemStatusService {
  // Detailed checks can reveal internal topology. The operator page uses the
  // authenticated endpoint; the public /readyz probe exposes aggregates only.
  private readonly readinessUrl = '/api/v1/system/readiness';
  private readonly eventDeliveryUrl = '/api/v1/event-delivery';
  private readonly connectorStatusUrl = '/api/v1/a2a/status';

  constructor(private http: HttpClient) {}

  readiness(): Observable<ISystemReadiness> {
    return this.http
      .get<ISystemReadiness>(this.readinessUrl, { observe: 'body' })
      .pipe(
        catchError((error: HttpErrorResponse) => {
          // A not_ready backend answers 503 with the full report. That is data,
          // not a failure: surfacing "database unreachable" is the entire point
          // of the page. Re-throw only when there is no usable body (a real
          // transport error, or the gateway bouncing us to login).
          const body = error.error as ISystemReadiness | undefined;
          if (body && typeof body === 'object' && Array.isArray(body.checks)) {
            return new Observable<ISystemReadiness>((subscriber) => {
              subscriber.next(body);
              subscriber.complete();
            });
          }
          return throwError(() => error);
        })
      );
  }

  eventDelivery(): Observable<IEventDeliveryStats> {
    return this.http.get<IEventDeliveryStats>(`${this.eventDeliveryUrl}/`);
  }

  connectorStatus(): Observable<IA2ABridgeStatus> {
    return this.http.get<IA2ABridgeStatus>(this.connectorStatusUrl);
  }

  retryEventDelivery(id: string): Observable<IEventDeliveryRetryResult> {
    return this.http.post<IEventDeliveryRetryResult>(
      `${this.eventDeliveryUrl}/${encodeURIComponent(id)}/retry`,
      {}
    );
  }
}
