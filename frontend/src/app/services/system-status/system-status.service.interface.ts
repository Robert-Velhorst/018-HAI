import { Observable } from 'rxjs';
import {
  IA2ABridgeStatus,
  IEventDeliveryRetryResult,
  IEventDeliveryStats,
  ISystemReadiness,
} from '../../models/system-status.model.interface';

export interface ISystemStatusService {
  // readiness may resolve with an ISystemReadiness even on an HTTP 503: a
  // not_ready backend still returns the report body, and that body is exactly
  // what the operator needs to see. Only a transport failure is an error.
  readiness(): Observable<ISystemReadiness>;
  eventDelivery(): Observable<IEventDeliveryStats>;
  connectorStatus(): Observable<IA2ABridgeStatus>;
  retryEventDelivery(id: string): Observable<IEventDeliveryRetryResult>;
}
