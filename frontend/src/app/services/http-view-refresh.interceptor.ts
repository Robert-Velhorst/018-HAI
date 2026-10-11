import { ChangeDetectorRef, inject, Injectable } from '@angular/core';
import { HttpEventType, HttpInterceptorFn } from '@angular/common/http';
import { tap } from 'rxjs';

/**
 * Batches view refreshes for legacy pages that still mutate view models from
 * direct HttpClient subscriptions. Explicit root-view detection keeps HAI's
 * ambient screens current even when the browser has no foreground frame loop.
 */
@Injectable({ providedIn: 'root' })
export class HttpViewRefreshScheduler {
  private refreshPending = false;
  private shellChangeDetector?: ChangeDetectorRef;

  registerShell(changeDetector: ChangeDetectorRef): void {
    this.shellChangeDetector = changeDetector;
  }

  unregisterShell(changeDetector: ChangeDetectorRef): void {
    if (this.shellChangeDetector === changeDetector) {
      this.shellChangeDetector = undefined;
    }
  }

  schedule(): void {
    if (this.refreshPending) {
      return;
    }
    this.refreshPending = true;
    queueMicrotask(() => {
      this.refreshPending = false;
      this.shellChangeDetector?.detectChanges();
    });
  }
}

export const httpViewRefreshInterceptor: HttpInterceptorFn = (request, next) => {
  const scheduler = inject(HttpViewRefreshScheduler);
  return next(request).pipe(
    tap({
      next: (event) => {
        if (event.type === HttpEventType.Response) {
          scheduler.schedule();
        }
      },
      error: () => scheduler.schedule(),
    })
  );
};
