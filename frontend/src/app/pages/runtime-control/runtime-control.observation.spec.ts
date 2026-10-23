import { fakeAsync, tick } from '@angular/core/testing'
import { Router } from '@angular/router'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import { EMPTY, of, Subject, throwError } from 'rxjs'
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service'
import { IAuthSession } from '../../models/auth-session.model.interface'
import { IBackgroundStatus, IReadiness } from '../../models/runtime-control.model.interface'
import { AuthSessionService } from '../../services/auth-session.service'
import { RuntimeControlService } from '../../services/runtime-control.service'
import { RuntimeControlComponent } from './runtime-control.component'

describe('Runtime control observation reconciliation', () => {
  let component: RuntimeControlComponent
  let service: jasmine.SpyObj<RuntimeControlService>
  let session: jasmine.SpyObj<AuthSessionService>
  let notifications: jasmine.SpyObj<NzNotificationService>
  const owner: IAuthSession = { authenticated: true, subject: 'synthetic-owner', role: 'owner', permissions: { canRead: true, canOperate: true, canApprove: true, canAdminister: true } }
  const status: IBackgroundStatus = { mode: 'paused', storedMode: 'paused', emergencyStop: { engaged: true, updatedAt: '2026-10-23T08:00:00Z' }, backgroundProcessingActive: false, docker: { cliAvailable: false, daemonRunning: false, required: false, detail: 'Not required' }, completedOperations: 0, awaitingApproval: 0 }
  const approval = { reviewItemId: '11111111-1111-4111-8111-111111111111', approvalSourceId: 'opscontrol-review:11111111-1111-4111-8111-111111111111', approvalBindingDigest: 'a'.repeat(64) }

  beforeEach(() => {
    service = jasmine.createSpyObj<RuntimeControlService>('runtime', ['status', 'readiness', 'pause', 'requestResumeApproval', 'approveAndResume', 'prepareModeChange', 'setMode', 'verifyEmergencyStop', 'recover'])
    service.status.and.returnValue(of(status))
    service.readiness.and.returnValue(of({ gates: [] } as unknown as IReadiness))
    service.pause.and.returnValue(of({ emergencyStop: status.emergencyStop }))
    service.requestResumeApproval.and.returnValue(of(approval))
    service.approveAndResume.and.returnValue(of({ emergencyStop: { ...status.emergencyStop, engaged: false } }))
    service.prepareModeChange.and.returnValue(of({ authorizationRequired: false }))
    service.setMode.and.returnValue(of({ mode: 'read_only' }))
    service.verifyEmergencyStop.and.returnValue(of({ engagedDuringTest: true, halted: true, operationsProcessedDuringStop: 0, restoredEngagedState: true, detail: 'Synthetic check' }))
    service.recover.and.returnValue(of({ scannedRunning: 0, scannedVerifying: 0, recovered: 0, ranAt: '2026-10-23T08:00:00Z' }))
    session = jasmine.createSpyObj<AuthSessionService>('session', ['session'])
    session.session.and.returnValue(of(owner))
    notifications = jasmine.createSpyObj<NzNotificationService>('notifications', ['success', 'warning', 'error', 'info'])
    component = new RuntimeControlComponent(service, session, notifications, jasmine.createSpyObj<Router>('router', ['navigate']), {} as ModuleViewPreferencesService)
    component.ngOnInit()
  })

  afterEach(() => component.ngOnDestroy())

  it('locks controls when the session completes without evidence', () => {
    session.session.and.returnValue(EMPTY)
    component.refresh()
    expect(component.loading).toBeFalse()
    expect(component.sessionUnavailable).toBeTrue()
    expect(component.canAdministerRuntime).toBeFalse()
  })

  it('takes a runtime snapshot without waiting for response stream completion', () => {
    const pending = new Subject<IBackgroundStatus>()
    service.status.and.returnValue(pending)
    component.refresh()
    pending.next(status)
    expect(component.loading).toBeFalse()
    expect(component.status).toBe(status)
    expect(pending.observers.length).toBe(0)
  })

  it('contains a synchronous session setup failure without retaining authority', () => {
    session.session.and.callFake(() => { throw new Error('PRIVATE_SETUP_DETAIL') })
    expect(() => component.refresh()).not.toThrow()
    expect(component.loading).toBeFalse()
    expect(component.sessionUnavailable).toBeTrue()
    expect(component.canOperateRuntime).toBeFalse()
  })

  it('does not dispatch any protected control after destruction', () => {
    component.resumeApproval = approval
    component.resumeConfirmation = 'RESUME BACKGROUND PROCESSING'
    component.ngOnDestroy()
    component.pause()
    component.resume()
    component.approveAndResume()
    component.verify()
    component.recover()
    component.setMode('read_only')
    expect(service.pause).not.toHaveBeenCalled()
    expect(service.requestResumeApproval).not.toHaveBeenCalled()
    expect(service.approveAndResume).not.toHaveBeenCalled()
    expect(service.verifyEmergencyStop).not.toHaveBeenCalled()
    expect(service.recover).not.toHaveBeenCalled()
    expect(service.prepareModeChange).not.toHaveBeenCalled()
  })

  it('cancels pending mode preparation before a late response can change policy', () => {
    const pending = new Subject<{ authorizationRequired: boolean }>()
    service.prepareModeChange.and.returnValue(pending)
    component.setMode('read_only')
    component.ngOnDestroy()
    expect(pending.observers.length).toBe(0)
    pending.next({ authorizationRequired: false })
    expect(service.setMode).not.toHaveBeenCalled()
  })

  for (const action of ['pause', 'resume', 'approveAndResume', 'verify', 'recover', 'mode'] as const) {
    it(`treats empty ${action} evidence as unconfirmed and releases the local busy state`, () => {
      switch (action) {
        case 'pause': service.pause.and.returnValue(EMPTY); component.pause(); break
        case 'resume': service.requestResumeApproval.and.returnValue(EMPTY); component.resume(); break
        case 'approveAndResume':
          component.resumeApproval = approval
          component.resumeConfirmation = 'RESUME BACKGROUND PROCESSING'
          service.approveAndResume.and.returnValue(EMPTY)
          component.approveAndResume()
          break
        case 'verify': service.verifyEmergencyStop.and.returnValue(EMPTY); component.verify(); break
        case 'recover': service.recover.and.returnValue(EMPTY); component.recover(); break
        case 'mode': service.prepareModeChange.and.returnValue(EMPTY); component.setMode('read_only'); break
      }
      expect(component.busy).toBeFalse()
      expect(notifications.error).toHaveBeenCalled()
      expect(notifications.success).not.toHaveBeenCalled()
      if (action === 'mode') expect(service.setMode).not.toHaveBeenCalled()
    })
  }

  it('keeps policy change busy until its single final acknowledgement', () => {
    const prepare = new Subject<{ authorizationRequired: boolean }>()
    const save = new Subject<{ mode: string }>()
    service.prepareModeChange.and.returnValue(prepare)
    service.setMode.and.returnValue(save)
    component.setMode('read_only')
    prepare.next({ authorizationRequired: false })
    expect(prepare.observers.length).toBe(0)
    expect(component.busy).toBeTrue()
    prepare.next({ authorizationRequired: false })
    expect(service.setMode).toHaveBeenCalledTimes(1)
    save.next({ mode: 'read_only' })
    expect(save.observers.length).toBe(0)
    expect(component.busy).toBeFalse()
    expect(notifications.success).toHaveBeenCalledTimes(1)
  })

  it('does not display arbitrary backend diagnostics in resume and mode errors', () => {
    service.requestResumeApproval.and.returnValue(throwError(() => ({ error: { error: 'PRIVATE_RESPONSE_DETAIL' } })))
    component.resume()
    expect(JSON.stringify(notifications.error.calls.allArgs())).not.toContain('PRIVATE_RESPONSE_DETAIL')
    service.prepareModeChange.and.returnValue(throwError(() => new Error('PRIVATE_RESPONSE_DETAIL')))
    component.setMode('read_only')
    expect(JSON.stringify(notifications.error.calls.allArgs())).not.toContain('PRIVATE_RESPONSE_DETAIL')
  })

  it('bounds a stalled preparation without retry or a policy mutation', fakeAsync(() => {
    const pending = new Subject<{ authorizationRequired: boolean }>()
    service.prepareModeChange.and.returnValue(pending)
    component.setMode('read_only')
    tick(30001)
    expect(component.busy).toBeFalse()
    expect(pending.observers.length).toBe(0)
    expect(service.prepareModeChange).toHaveBeenCalledTimes(1)
    expect(service.setMode).not.toHaveBeenCalled()
    expect(notifications.success).not.toHaveBeenCalled()
  }))

  for (const action of ['pause', 'resume', 'approveAndResume', 'verify', 'recover', 'mode'] as const) {
    it(`discards a pending ${action} observation on destruction without late feedback`, () => {
      const pending = new Subject<never>()
      switch (action) {
        case 'pause': service.pause.and.returnValue(pending); component.pause(); break
        case 'resume': service.requestResumeApproval.and.returnValue(pending); component.resume(); break
        case 'approveAndResume':
          component.resumeApproval = approval
          component.resumeConfirmation = 'RESUME BACKGROUND PROCESSING'
          service.approveAndResume.and.returnValue(pending)
          component.approveAndResume()
          break
        case 'verify': service.verifyEmergencyStop.and.returnValue(pending); component.verify(); break
        case 'recover': service.recover.and.returnValue(pending); component.recover(); break
        case 'mode': service.prepareModeChange.and.returnValue(pending); component.setMode('read_only'); break
      }
      expect(pending.observers.length).toBe(1)
      session.session.calls.reset()
      component.ngOnDestroy()
      expect(pending.observers.length).toBe(0)
      pending.next(undefined as never)
      expect(session.session).not.toHaveBeenCalled()
      expect(notifications.success).not.toHaveBeenCalled()
      expect(notifications.error).not.toHaveBeenCalled()
      expect(notifications.warning).not.toHaveBeenCalled()
      expect(notifications.info).not.toHaveBeenCalled()
    })
  }

  it('bounds a pending final policy write without replaying preparation or mutation', fakeAsync(() => {
    const pending = new Subject<{ mode: string }>()
    service.setMode.and.returnValue(pending)
    component.setMode('read_only')
    expect(component.busy).toBeTrue()
    tick(30001)
    expect(pending.observers.length).toBe(0)
    expect(component.busy).toBeFalse()
    expect(service.prepareModeChange).toHaveBeenCalledTimes(1)
    expect(service.setMode).toHaveBeenCalledTimes(1)
    expect(notifications.success).not.toHaveBeenCalled()
    expect(component.status?.storedMode).toBe('paused')
  }))

  it('contains synchronous emergency-stop request setup errors without leaking details', () => {
    service.pause.and.callFake(() => { throw new Error('PRIVATE_SETUP_DETAIL') })
    expect(() => component.pause()).not.toThrow()
    expect(component.busy).toBeFalse()
    expect(notifications.error).toHaveBeenCalled()
    expect(JSON.stringify(notifications.error.calls.allArgs())).not.toContain('PRIVATE_SETUP_DETAIL')
    expect(notifications.success).not.toHaveBeenCalled()
  })
})
