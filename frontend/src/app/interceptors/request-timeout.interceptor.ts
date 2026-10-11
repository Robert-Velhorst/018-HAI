import { Injectable } from '@angular/core';
import { HttpEvent, HttpHandler, HttpInterceptor, HttpRequest } from '@angular/common/http';
import { Observable, timeout } from 'rxjs';
import { HttpTimeoutPolicy } from '../shared/http-timeout-policy';

@Injectable()
export class RequestTimeoutInterceptor implements HttpInterceptor {
  static readonly readTimeoutMs = HttpTimeoutPolicy.readMs;
  static readonly operationTimeoutMs = HttpTimeoutPolicy.operationMs;
  static readonly reviewedArchiveUploadTimeoutMs = HttpTimeoutPolicy.reviewedArchiveUploadMs;
  static readonly sourceTranscriptionTimeoutMs = HttpTimeoutPolicy.sourceTranscriptionMs;
  static readonly sourceDocumentExtractionTimeoutMs = HttpTimeoutPolicy.sourceDocumentExtractionMs;
  private static readonly reviewedArchiveUploadPath = '/api/v1/agent-runtimes/openclaw/ecosystem/upload';

  intercept(request: HttpRequest<unknown>, next: HttpHandler): Observable<HttpEvent<unknown>> {
    const duration = this.isReviewedArchiveUpload(request)
      ? RequestTimeoutInterceptor.reviewedArchiveUploadTimeoutMs
      : HttpTimeoutPolicy.forRequest(request.method, request.url);
    return next.handle(request).pipe(timeout(duration));
  }

  private isReviewedArchiveUpload(request: HttpRequest<unknown>): boolean {
    return request.method === 'POST'
      && request.url.includes(RequestTimeoutInterceptor.reviewedArchiveUploadPath);
  }
}
