import { ComponentFixture, TestBed } from '@angular/core/testing'
import { RouterTestingModule } from '@angular/router/testing'
import { NoopAnimationsModule } from '@angular/platform-browser/animations'
import {
  ArrowLeftOutline,
  CheckCircleOutline,
  ReloadOutline,
  StopOutline,
} from '@ant-design/icons-angular/icons'
import { of, throwError } from 'rxjs'
import { NZ_ICONS } from 'ng-zorro-antd/icon'
import { NzModalService } from 'ng-zorro-antd/modal'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import { IAuthSession } from '../../models/auth-session.model.interface'
import {
  IBackgroundStatus,
  IReadiness,
} from '../../models/runtime-control.model.interface'
import { AuthSessionService } from '../../services/auth-session.service'
import { RuntimeControlService } from '../../services/runtime-control.service'
import { RuntimeControlComponent } from './runtime-control.component'
import { RuntimeControlModule } from './runtime-control.module'

describe('RuntimeControlComponent role boundaries', () => {
  let fixture: ComponentFixture<RuntimeControlComponent>
  let component: RuntimeControlComponent
  let runtimeService: jasmine.SpyObj<RuntimeControlService>
  let authSessionService: jasmine.SpyObj<AuthSessionService>
  let modal: jasmine.SpyObj<NzModalService>
  let notification: jasmine.SpyObj<NzNotificationService>

  const ownerSession: IAuthSession = {
    authenticated: true,
    subject: 'owner-1',
    role: 'owner',
    permissions: {
      canRead: true,
      canOperate: true,
      canApprove: true,
      canAdminister: true,
    },
  }

  const operatorSession: IAuthSession = {
    authenticated: true,
    subject: 'operator-1',
    role: 'operator',
    permissions: {
      canRead: true,
      canOperate: true,
      canApprove: true,
      canAdminister: false,
    },
  }

  const viewerSession: IAuthSession = {
    authenticated: true,
    subject: 'viewer-1',
    role: 'viewer',
    permissions: {
      canRead: true,
      canOperate: false,
      canApprove: false,
      canAdminister: false,
    },
  }

  const activeStatus: IBackgroundStatus = {
    mode: 'approval_required',
    storedMode: 'approval_required',
    emergencyStop: {
      engaged: false,
      updatedAt: '2026-07-30T12:00:00Z',
    },
    backgroundProcessingActive: true,
    docker: {
      cliAvailable: true,
      daemonRunning: true,
      required: false,
      detail: 'available',
    },
    completedOperations: 4,
    awaitingApproval: 2,
  }

  const readiness: IReadiness = {
    operatingSystem: 'windows',
    isWindows: true,
    overallReady: true,
    targetMachineVerificationPending: false,
    backgroundMode: 'approval_required',
    emergencyStop: activeStatus.emergencyStop,
    docker: activeStatus.docker,
    gates: [],
  }

  beforeEach(async () => {
    runtimeService = jasmine.createSpyObj<RuntimeControlService>('RuntimeControlService', [
      'status',
      'readiness',
      'pause',
      'prepareControlApproval',
      'decideControlApproval',
      'resume',
      'setMode',
      'verifyEmergencyStop',
      'recover',
    ])
    authSessionService = jasmine.createSpyObj<AuthSessionService>('AuthSessionService', [
      'session',
    ])
    modal = jasmine.createSpyObj<NzModalService>('NzModalService', ['confirm'])
    notification = jasmine.createSpyObj<NzNotificationService>('NzNotificationService', [
      'success',
      'warning',
      'error',
    ])

    runtimeService.status.and.returnValue(of(activeStatus))
    runtimeService.readiness.and.returnValue(of(readiness))
    runtimeService.pause.and.returnValue(of({ emergencyStop: activeStatus.emergencyStop }))
    runtimeService.prepareControlApproval.and.returnValue(of({
      requestId: '11111111-1111-4111-8111-111111111111',
      action: 'opscontrol.emergency-stop.clear',
      resourceType: 'opscontrol-emergency-stop',
      resourceId: 'emergency-stop:revision-1',
      target: 'disengaged',
      bindingDigest: 'a'.repeat(64),
      expiresAt: '2099-07-30T12:05:00Z',
    }))
    runtimeService.decideControlApproval.and.returnValue(of({
      requestId: '11111111-1111-4111-8111-111111111111',
      decisionId: '22222222-2222-4222-8222-222222222222',
      decision: 'approved',
      idempotencyKey: 'opscontrol:11111111-1111-4111-8111-111111111111',
      taskId: 'opscontrol:11111111-1111-4111-8111-111111111111',
      approvalSourceId: 'control-decision:22222222-2222-4222-8222-222222222222',
      approvalBindingDigest: 'a'.repeat(64),
      expiresAt: '2099-07-30T12:05:00Z',
    }))
    runtimeService.resume.and.returnValue(of({ emergencyStop: activeStatus.emergencyStop }))
    runtimeService.setMode.and.returnValue(of({ mode: 'read_only' }))
    runtimeService.verifyEmergencyStop.and.returnValue(of({
      engagedDuringTest: true,
      operationsProcessedDuringStop: 0,
      halted: true,
      restoredEngagedState: true,
      detail: 'Emergency stop halted processing.',
    }))
    runtimeService.recover.and.returnValue(of({
      scannedRunning: 1,
      scannedVerifying: 0,
      recovered: 1,
      ranAt: '2026-07-30T12:00:00Z',
    }))

    await TestBed.configureTestingModule({
      imports: [
        RuntimeControlModule,
        RouterTestingModule,
        NoopAnimationsModule,
      ],
      providers: [
        { provide: RuntimeControlService, useValue: runtimeService },
        { provide: AuthSessionService, useValue: authSessionService },
        { provide: NzModalService, useValue: modal },
        { provide: NzNotificationService, useValue: notification },
        {
          provide: NZ_ICONS,
          useValue: [
            ArrowLeftOutline,
            CheckCircleOutline,
            ReloadOutline,
            StopOutline,
          ],
        },
      ],
    }).compileComponents()
  })

  it('renders owner controls and permits resume and autonomy changes', () => {
    authSessionService.session.and.returnValue(of(ownerSession))
    runtimeService.status.and.returnValue(of({
      ...activeStatus,
      emergencyStop: {
        ...activeStatus.emergencyStop,
        engaged: true,
      },
      backgroundProcessingActive: false,
    }))
    createComponent()

    expect(text()).toContain('Owner authority')
    expect(button('Resume').disabled).toBeFalse()

    button('Resume').click()
    expect(runtimeService.resume).not.toHaveBeenCalled()
    expect(modal.confirm).toHaveBeenCalled()
    ;(modal.confirm.calls.mostRecent().args[0]!.nzOnOk as () => void)()
    component.setMode('read_only')

    expect(runtimeService.prepareControlApproval).toHaveBeenCalledWith('resume', undefined)
    expect(runtimeService.decideControlApproval).toHaveBeenCalledWith(
      '11111111-1111-4111-8111-111111111111',
      'approved',
      'Owner approved this exact emergency-stop recovery.'
    )
    expect(runtimeService.resume).toHaveBeenCalledWith(jasmine.objectContaining({
      approvalSourceId: 'control-decision:22222222-2222-4222-8222-222222222222',
      approvalBindingDigest: 'a'.repeat(64),
    }))
    expect(runtimeService.setMode).toHaveBeenCalledWith('read_only')
    expect(component.canAdministerRuntime).toBeTrue()
  })

  it('requires and consumes an exact approval before increasing autonomy', () => {
    authSessionService.session.and.returnValue(of(ownerSession))
    runtimeService.status.and.returnValue(of({
      ...activeStatus,
      mode: 'read_only',
      storedMode: 'read_only',
    }))
    createComponent()

    component.setMode('autonomous_safe')

    expect(runtimeService.setMode).not.toHaveBeenCalled()
    expect(modal.confirm).toHaveBeenCalled()
    ;(modal.confirm.calls.mostRecent().args[0]!.nzOnOk as () => void)()
    expect(runtimeService.prepareControlApproval).toHaveBeenCalledWith(
      'set_mode',
      'autonomous_safe'
    )
    expect(runtimeService.setMode).toHaveBeenCalledWith(
      'autonomous_safe',
      jasmine.objectContaining({
        approvalSourceId: 'control-decision:22222222-2222-4222-8222-222222222222',
      })
    )
  })

  it('does not weaken a control when approval evidence is incomplete', () => {
    authSessionService.session.and.returnValue(of(ownerSession))
    runtimeService.status.and.returnValue(of({
      ...activeStatus,
      emergencyStop: { ...activeStatus.emergencyStop, engaged: true },
      backgroundProcessingActive: false,
    }))
    runtimeService.decideControlApproval.and.returnValue(of({
      requestId: '11111111-1111-4111-8111-111111111111',
      decisionId: '22222222-2222-4222-8222-222222222222',
      decision: 'approved',
      expiresAt: '2099-07-30T12:05:00Z',
    }))
    createComponent()

    component.resume()
    ;(modal.confirm.calls.mostRecent().args[0]!.nzOnOk as () => void)()

    expect(runtimeService.resume).not.toHaveBeenCalled()
    expect(notification.error).toHaveBeenCalledWith(
      'Resume failed',
      'HAI returned incomplete or expired control-approval evidence.'
    )
    expect(component.busy).toBeFalse()
  })

  it('keeps emergency stop, recovery, and verification available to an operator', () => {
    authSessionService.session.and.returnValue(of(operatorSession))
    createComponent()

    expect(text()).toContain('Operator authority')
    expect(button('Engage emergency stop').disabled).toBeFalse()
    expect(button('Verify emergency stop').disabled).toBeFalse()
    expect(button('Run recovery').disabled).toBeFalse()
    expect(radioInputs().every((input) => input.disabled)).toBeTrue()

    button('Engage emergency stop').click()
    component.verify()
    component.recover()
    component.setMode('read_only')

    expect(runtimeService.pause).toHaveBeenCalled()
    expect(runtimeService.verifyEmergencyStop).toHaveBeenCalled()
    expect(runtimeService.recover).toHaveBeenCalled()
    expect(runtimeService.setMode).not.toHaveBeenCalled()

    component.status = {
      ...activeStatus,
      emergencyStop: {
        ...activeStatus.emergencyStop,
        engaged: true,
      },
    }
    fixture.detectChanges()

    expect(button('Resume').disabled).toBeTrue()
    component.resume()
    expect(runtimeService.resume).not.toHaveBeenCalled()
    expect(text()).toContain('Only the owner can resume processing')
  })

  it('renders a viewer as read-only and blocks every protected interaction', () => {
    authSessionService.session.and.returnValue(of(viewerSession))
    createComponent()

    expect(text()).toContain('Read-only authority')
    expect(button('Engage emergency stop').disabled).toBeTrue()
    expect(button('Verify emergency stop').disabled).toBeTrue()
    expect(button('Run recovery').disabled).toBeTrue()
    expect(radioInputs().every((input) => input.disabled)).toBeTrue()

    component.pause()
    component.resume()
    component.verify()
    component.recover()
    component.setMode('paused')

    expect(runtimeService.pause).not.toHaveBeenCalled()
    expect(runtimeService.resume).not.toHaveBeenCalled()
    expect(runtimeService.verifyEmergencyStop).not.toHaveBeenCalled()
    expect(runtimeService.recover).not.toHaveBeenCalled()
    expect(runtimeService.setMode).not.toHaveBeenCalled()
    expect(notification.warning).toHaveBeenCalled()
  })

  it('fails closed when the signed session is unavailable', () => {
    authSessionService.session.and.returnValue(
      throwError(() => new Error('session unavailable'))
    )
    createComponent()

    expect(text()).toContain('Authority unavailable')
    expect(text()).toContain('Runtime controls locked')
    expect(runtimeService.status).not.toHaveBeenCalled()
    expect(runtimeService.readiness).not.toHaveBeenCalled()
    expect(component.canReadRuntime).toBeFalse()
    expect(fixture.nativeElement.querySelector('.rc__control')).toBeNull()
  })

  function createComponent(): void {
    fixture = TestBed.createComponent(RuntimeControlComponent)
    component = fixture.componentInstance
    fixture.detectChanges()
  }

  function text(): string {
    return (fixture.nativeElement as HTMLElement).textContent ?? ''
  }

  function button(label: string): HTMLButtonElement {
    const match = Array.from(
      (fixture.nativeElement as HTMLElement).querySelectorAll('button')
    ).find((candidate) => candidate.textContent?.includes(label))
    if (!match) {
      throw new Error(`Button not found: ${label}`)
    }
    return match
  }

  function radioInputs(): HTMLInputElement[] {
    return Array.from(
      (fixture.nativeElement as HTMLElement).querySelectorAll<HTMLInputElement>(
        'input[type="radio"]'
      )
    )
  }
})
