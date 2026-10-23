import { FormBuilder } from '@angular/forms';
import { fakeAsync, tick } from '@angular/core/testing';
import { EMPTY, of, Subject } from 'rxjs';
import { CommandDashboardComponent } from './command-dashboard.component';
import { HttpTimeoutPolicy } from '../../shared/http-timeout-policy';

describe('Command Dashboard OpenClaw observation boundaries', () => {
  const authorization = {
    idempotencyKey: 'fixture-operation', taskId: 'fixture-task',
    approvalSourceId: 'fixture-owner', approvalBindingDigest: 'a'.repeat(64),
  };
  const runtime = { id: 'openclaw', ecosystemPath: 'C:\\fixture\\current.zip', ecosystemRollbackAvailable: true };
  const updated = { ...runtime, ecosystemPath: 'C:\\fixture\\updated.zip' };
  const operations = [
    ['set', 'prepareOpenClawEcosystemPath', 'setOpenClawEcosystemPath'],
    ['refresh', 'prepareOpenClawEcosystemRefresh', 'refreshOpenClawEcosystem'],
    ['upload', 'prepareOpenClawEcosystemUpload', 'uploadOpenClawEcosystem'],
    ['rollback', 'prepareOpenClawEcosystemRollback', 'rollbackOpenClawEcosystem'],
  ] as const;

  function fixture(operation: typeof operations[number]) {
    const prepare = jasmine.createSpy('prepare').and.returnValue(of(authorization));
    const execute = jasmine.createSpy('execute').and.returnValue(of(updated));
    const service = { [operation[1]]: prepare, [operation[2]]: execute };
    const notification = jasmine.createSpyObj('notification', ['error', 'success']);
    const component = new CommandDashboardComponent(new FormBuilder(), {} as any, service as any,
      {} as any, {} as any, {} as any, notification, {} as any);
    component.runtimes = [{ ...runtime } as any];
    component.openClawEcosystemPath = runtime.ecosystemPath;
    const run = () => {
      if (operation[0] === 'set') component.setOpenClawEcosystemPath(component.runtimes[0]);
      if (operation[0] === 'refresh') component.refreshOpenClawEcosystem(component.runtimes[0]);
      if (operation[0] === 'upload') component.uploadOpenClawEcosystem(new File(['fixture'], 'fixture.zip'));
      if (operation[0] === 'rollback') component.rollbackOpenClawEcosystem(component.runtimes[0]);
    };
    return { component, prepare, execute, notification, run };
  }

  for (const operation of operations) {
    it(`${operation[0]} accepts a single bounded acknowledgement`, () => {
      const f = fixture(operation);
      f.run();
      expect(f.prepare).toHaveBeenCalledTimes(1);
      expect(f.execute).toHaveBeenCalledTimes(1);
      expect(f.component.runtimes[0]).toEqual(updated as any);
      expect(f.notification.success).toHaveBeenCalledTimes(1);
      expect(f.component.isOpenClawBusy()).toBeFalse();
    });

    for (const invalid of [null, {}, { ...authorization, approvalBindingDigest: 'invalid' },
      { ...authorization, approvalBindingDigest: ['a'.repeat(64)] }]) {
      it(`${operation[0]} rejects malformed approval ${JSON.stringify(invalid)}`, () => {
        const f = fixture(operation);
        f.prepare.and.returnValue(of(invalid));
        f.run();
        expect(f.execute).not.toHaveBeenCalled();
        expect(f.notification.success).not.toHaveBeenCalled();
        expect(f.notification.error).toHaveBeenCalledTimes(1);
        expect(f.component.isOpenClawBusy()).toBeFalse();
      });
    }

    for (const phase of ['prepare', 'execute'] as const) {
      it(`${operation[0]} treats an empty ${phase} response as unconfirmed`, () => {
        const f = fixture(operation);
        f[phase].and.returnValue(EMPTY);
        f.run();
        expect(f.notification.success).not.toHaveBeenCalled();
        expect(f.notification.error).toHaveBeenCalledTimes(1);
        expect(f.component.isOpenClawBusy()).toBeFalse();
        expect(f.component.runtimes[0]).toEqual(runtime as any);
      });

      it(`${operation[0]} releases state and hides synchronous ${phase} failures`, () => {
        const f = fixture(operation);
        f[phase].and.callFake(() => { throw new Error('PRIVATE_CREDENTIAL_CANARY'); });
        expect(() => f.run()).not.toThrow();
        expect(f.notification.error).toHaveBeenCalledTimes(1);
        expect(JSON.stringify(f.notification.error.calls.allArgs())).not.toContain('PRIVATE_CREDENTIAL_CANARY');
        expect(f.component.isOpenClawBusy()).toBeFalse();
      });

      it(`${operation[0]} cancels a pending ${phase} response on destruction`, () => {
        const f = fixture(operation);
        const pending = new Subject<any>();
        f[phase].and.returnValue(pending);
        f.run();
        expect(pending.observed).toBeTrue();
        f.component.ngOnDestroy();
        expect(pending.observed).toBeFalse();
        pending.next(phase === 'prepare' ? authorization : updated);
        expect(f.notification.success).not.toHaveBeenCalled();
        expect(f.component.isOpenClawBusy()).toBeFalse();
        if (phase === 'prepare') expect(f.execute).not.toHaveBeenCalled();
      });

      it(`${operation[0]} does not expose asynchronous ${phase} server details`, () => {
        const f = fixture(operation);
        const pending = new Subject<any>();
        f[phase].and.returnValue(pending);
        f.run();
        pending.error({ error: { error: 'PRIVATE_SERVER_CANARY' } });
        expect(f.notification.error).toHaveBeenCalledTimes(1);
        expect(JSON.stringify(f.notification.error.calls.allArgs())).not.toContain('PRIVATE_SERVER_CANARY');
        expect(f.notification.error.calls.mostRecent().args[1]).toContain('Refresh runtime inventory');
        expect(f.notification.success).not.toHaveBeenCalled();
        expect(f.component.isOpenClawBusy()).toBeFalse();
      });

      it(`${operation[0]} bounds a pending ${phase} response`, fakeAsync(() => {
        const f = fixture(operation);
        const pending = new Subject<any>();
        f[phase].and.returnValue(pending);
        f.run();
        const limit = operation[0] === 'upload'
          ? HttpTimeoutPolicy.reviewedArchiveUploadMs : HttpTimeoutPolicy.operationMs;
        tick(limit);
        expect(pending.observed).toBeFalse();
        expect(f.notification.success).not.toHaveBeenCalled();
        expect(f.notification.error).toHaveBeenCalledTimes(1);
        expect(f.component.isOpenClawBusy()).toBeFalse();
      }));
    }

    it(`${operation[0]} consumes approval once rather than redispatching a second emission`, () => {
      const f = fixture(operation);
      const pending = new Subject<any>();
      f.prepare.and.returnValue(pending);
      f.run();
      pending.next(authorization);
      pending.next(authorization);
      expect(f.execute).toHaveBeenCalledTimes(1);
      expect(pending.observed).toBeFalse();
    });

    it(`${operation[0]} accepts only the first execution acknowledgement`, () => {
      const f = fixture(operation);
      const pending = new Subject<any>();
      f.execute.and.returnValue(pending);
      f.run();
      pending.next(updated);
      pending.next({ ...updated, ecosystemPath: 'C:\\fixture\\late.zip' });
      expect(pending.observed).toBeFalse();
      expect(f.component.runtimes[0]).toEqual(updated as any);
      expect(f.notification.success).toHaveBeenCalledTimes(1);
      expect(f.component.isOpenClawBusy()).toBeFalse();
    });

    for (const invalid of [null, { id: 'hermes', ecosystemPath: 'C:\\fixture\\foreign.zip' },
      { id: 'openclaw', ecosystemPath: operation[0] === 'refresh' ? 13 : '' }]) {
      it(`${operation[0]} rejects an unusable runtime acknowledgement ${JSON.stringify(invalid)}`, () => {
        const f = fixture(operation);
        f.execute.and.returnValue(of(invalid));
        f.run();
        expect(f.notification.success).not.toHaveBeenCalled();
        expect(f.component.runtimes[0]).toEqual(runtime as any);
        expect(f.notification.error).toHaveBeenCalledTimes(1);
        expect(f.component.isOpenClawBusy()).toBeFalse();
      });
    }

    it(`${operation[0]} does not start another operation after destruction`, () => {
      const f = fixture(operation);
      f.component.ngOnDestroy();
      f.run();
      expect(f.prepare).not.toHaveBeenCalled();
      expect(f.execute).not.toHaveBeenCalled();
    });
  }

  it('preserves the backend refresh contract when no archive has been selected', () => {
    const f = fixture(operations[1]);
    f.execute.and.returnValue(of({ id: 'openclaw' }));
    f.run();
    expect(f.notification.success).toHaveBeenCalledTimes(1);
    expect(f.notification.success.calls.mostRecent().args[1]).not.toContain('Re-scanned');
    expect(f.component.isOpenClawBusy()).toBeFalse();
  });

  for (const availability of ['true', 1, undefined]) {
    it(`does not grant rollback from a non-boolean availability flag ${availability}`, () => {
      const f = fixture(operations[3]);
      (f.component.runtimes[0] as any).ecosystemRollbackAvailable = availability;
      f.run();
      expect(f.prepare).not.toHaveBeenCalled();
      expect(f.execute).not.toHaveBeenCalled();
    });
  }
});
