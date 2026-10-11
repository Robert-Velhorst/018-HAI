import { ChangeDetectorRef } from '@angular/core';
import { HttpViewRefreshScheduler } from './http-view-refresh.interceptor';

describe('HttpViewRefreshScheduler', () => {
  function scheduler(): {
    scheduler: HttpViewRefreshScheduler;
    changeDetector: jasmine.SpyObj<ChangeDetectorRef>;
  } {
    const changeDetector = jasmine.createSpyObj<ChangeDetectorRef>('ChangeDetectorRef', [
      'detectChanges',
      'markForCheck',
      'detach',
      'reattach',
    ]);
    const scheduler = new HttpViewRefreshScheduler();
    scheduler.registerShell(changeDetector);
    return {
      scheduler,
      changeDetector,
    };
  }

  it('batches synchronous response completions into one root-view refresh', async () => {
    const test = scheduler();

    test.scheduler.schedule();
    test.scheduler.schedule();
    expect(test.changeDetector.detectChanges).not.toHaveBeenCalled();

    await Promise.resolve();
    expect(test.changeDetector.detectChanges).toHaveBeenCalledTimes(1);
  });

  it('allows a later response batch to refresh again', async () => {
    const test = scheduler();

    test.scheduler.schedule();
    await Promise.resolve();
    test.scheduler.schedule();
    await Promise.resolve();

    expect(test.changeDetector.detectChanges).toHaveBeenCalledTimes(2);
  });

  it('stops refreshing after the authenticated shell is destroyed', async () => {
    const test = scheduler();
    test.scheduler.unregisterShell(test.changeDetector);

    test.scheduler.schedule();
    await Promise.resolve();

    expect(test.changeDetector.detectChanges).not.toHaveBeenCalled();
  });
});
