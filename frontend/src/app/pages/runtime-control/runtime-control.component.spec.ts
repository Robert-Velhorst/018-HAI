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
import { NzNotificationService } from 'ng-zorro-antd/notification'
import { IAuthSession } from '../../models/auth-session.model.interface'
import {
  IBackgroundStatus,
  IReadiness,
} from '../../models/runtime-control.model.interface'
import { AuthSessionService } from '../../services/auth-session.service'
import {
  IControlAuthorization,
  RuntimeControlService,
} from '../../services/runtime-control.service'
import { RuntimeControlComponent } from './runtime-control.component'
import { RuntimeControlModule } from './runtime-control.module'

describe('RuntimeControlComponent role boundaries', () => {
  let fixture: ComponentFixture<RuntimeControlComponent>
  let component: RuntimeControlComponent
  let runtimeService: jasmine.SpyObj<RuntimeControlService>
  let authSessionService: jasmine.SpyObj<AuthSessionService>
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
      canApprove: false,
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
      'prepareResume',
      'resume',
      'prepareModeChange',
      'setMode',
      'verifyEmergencyStop',
      'recover',
    ])
    authSessionService = jasmine.createSpyObj<AuthSessionService>('AuthSessionService', [
      'session',
    ])
    notification = jasmine.createSpyObj<NzNotificationService>('NzNotificationService', [
      'success',
      'warning',
      'error',
    ])

    runtimeService.status.and.returnValue(of(activeStatus))
    runtimeService.readiness.and.returnValue(of(readiness))
    runtimeService.pause.and.returnValue(of({
      emergencyStop: {
        ...activeStatus.emergencyStop,
        engaged: true,
      },
      openClawCancellation: {
        status: 'complete',
        message: 'No unresolved OpenClaw run required cancellation.',
      },
    }))
    runtimeService.prepareResume.and.returnValue(of({
      idempotencyKey: 'resume-1',
      taskId: 'opscontrol-emergency-stop-1',
      approvalSourceId: 'opscontrol-owner:approval',
      approvalBindingDigest: 'a'.repeat(64),
    }))
    runtimeService.resume.and.returnValue(of({ emergencyStop: activeStatus.emergencyStop }))
    runtimeService.prepareModeChange.and.returnValue(of({ authorizationRequired: false }))
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
    component.setMode('read_only')

    expect(runtimeService.prepareResume).toHaveBeenCalled()
    expect(runtimeService.resume).toHaveBeenCalledWith({
      idempotencyKey: 'resume-1',
      taskId: 'opscontrol-emergency-stop-1',
      approvalSourceId: 'opscontrol-owner:approval',
      approvalBindingDigest: 'a'.repeat(64),
    })
    expect(runtimeService.prepareModeChange).toHaveBeenCalledWith('read_only')
    expect(runtimeService.setMode).toHaveBeenCalledWith('read_only')
    expect(component.canAdministerRuntime).toBeTrue()
  })

  it('keeps emergency stop, recovery, and verification available to an operator', () => {
    authSessionService.session.and.returnValue(of(operatorSession))
    createComponent()

    expect(text()).toContain('Operator authority')
    expect(component.canOperateRuntime).toBeTrue()
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

  it('does not present a fail-closed fallback as a confirmed stored autonomy mode', () => {
    authSessionService.session.and.returnValue(of(ownerSession))
    runtimeService.status.and.returnValue(of({
      ...activeStatus,
      mode: 'paused',
      storedMode: 'paused',
      modeStateError: 'persisted autonomy mode is unavailable',
    }))
    createComponent()

    expect(text()).toContain('Paused; stored mode unavailable')
    expect(text()).not.toContain('Stored: paused')
  })

  it('refreshes backend state when returning to the runtime-control window', () => {
    authSessionService.session.and.returnValue(of(ownerSession))
    createComponent()
    const initialStatusCalls = runtimeService.status.calls.count()

    window.dispatchEvent(new Event('focus'))
    fixture.detectChanges()

    expect(runtimeService.status.calls.count()).toBe(initialStatusCalls + 1)
  })

  it('reports partial emergency-stop fan-out without presenting it as complete', () => {
    authSessionService.session.and.returnValue(of(operatorSession))
    runtimeService.pause.and.returnValue(of({
      emergencyStop: {
        ...activeStatus.emergencyStop,
        engaged: true,
      },
      openClawCancellation: {
        status: 'partial',
        message: 'One exact run remains unresolved.',
      },
    }))
    createComponent()

    button('Engage emergency stop').click()

    expect(notification.warning).toHaveBeenCalledWith(
      'Emergency stop active; remote cancellation incomplete',
      'One exact run remains unresolved.'
    )
    expect(notification.success).not.toHaveBeenCalled()
  })

  it('fails closed when the mode-approval response omits its required proof', () => {
    authSessionService.session.and.returnValue(of(ownerSession))
    runtimeService.prepareModeChange.and.returnValue(of({ authorizationRequired: true }))
    createComponent()

    component.setMode('autonomous_safe')

    expect(runtimeService.prepareModeChange).toHaveBeenCalledWith('autonomous_safe')
    expect(runtimeService.setMode).not.toHaveBeenCalled()
    expect(notification.error).toHaveBeenCalledWith(
      'Mode change not confirmed',
      jasmine.stringContaining('incomplete approval proof')
    )
    expect(component.status?.storedMode).toBe(activeStatus.storedMode)
  })

  it('does not call resume when its owner-approval preparation is incomplete', () => {
    authSessionService.session.and.returnValue(of(ownerSession))
    runtimeService.status.and.returnValue(of({
      ...activeStatus,
      emergencyStop: {
        ...activeStatus.emergencyStop,
        engaged: true,
      },
      backgroundProcessingActive: false,
    }))
    runtimeService.prepareResume.and.returnValue(of({} as IControlAuthorization))
    createComponent()

    component.resume()

    expect(runtimeService.prepareResume).toHaveBeenCalled()
    expect(runtimeService.resume).not.toHaveBeenCalled()
    expect(notification.success).not.toHaveBeenCalled()
    expect(notification.error).toHaveBeenCalledWith(
      'Resume not confirmed',
      jasmine.stringContaining('complete owner-approval proof')
    )
  })

  it('reconciles a stale resume request when the backend rejects its old emergency-stop state', () => {
    authSessionService.session.and.returnValue(of(ownerSession))
    runtimeService.prepareResume.and.returnValue(throwError(() => ({
      error: { error: 'safety-control state changed; refresh and retry' },
    })))
    createComponent()
    component.status = {
      ...activeStatus,
      emergencyStop: {
        ...activeStatus.emergencyStop,
        engaged: true,
      },
      backgroundProcessingActive: false,
    }
    component.resume()

    expect(runtimeService.prepareResume).toHaveBeenCalled()
    expect(runtimeService.resume).not.toHaveBeenCalled()
    expect(notification.success).not.toHaveBeenCalled()
    expect(component.status?.emergencyStop.engaged).toBeFalse()
    expect(notification.error).toHaveBeenCalledWith(
      'Resume not confirmed',
      'safety-control state changed; refresh and retry'
    )
  })

  it('blocks resume when one backend snapshot says both stop-engaged and processing-enabled', () => {
    authSessionService.session.and.returnValue(of(ownerSession))
    runtimeService.status.and.returnValue(of({
      ...activeStatus,
      emergencyStop: {
        ...activeStatus.emergencyStop,
        engaged: true,
      },
      backgroundProcessingActive: true,
    }))
    createComponent()

    expect(component.emergencyStopStatusInconsistent).toBeTrue()
    expect(button('Resume').disabled).toBeTrue()
    component.resume()

    expect(runtimeService.prepareResume).not.toHaveBeenCalled()
    expect(notification.error).toHaveBeenCalledWith(
      'Resume blocked: runtime state conflicts',
      'Refresh the backend-confirmed state before attempting to clear the emergency stop.'
    )
  })

  it('requires the verification result to confirm both halt and prior-state restoration', () => {
    authSessionService.session.and.returnValue(of(operatorSession))
    runtimeService.verifyEmergencyStop.and.returnValue(of({
      engagedDuringTest: true,
      operationsProcessedDuringStop: 0,
      halted: true,
      restoredEngagedState: false,
      detail: 'The prior stop state was not restored.',
    }))
    createComponent()

    button('Verify emergency stop').click()

    expect(notification.success).not.toHaveBeenCalled()
    expect(notification.error).toHaveBeenCalledWith(
      'Emergency stop verification not confirmed',
      'The prior stop state was not restored.'
    )
  })

  it('does not call a recovery report complete when it has no scan-error proof', () => {
    authSessionService.session.and.returnValue(of(operatorSession))
    runtimeService.recover.and.returnValue(of({
      scannedRunning: 0,
      scannedVerifying: 0,
      recovered: 0,
      ranAt: '2026-09-24T12:00:00Z',
    }))
    createComponent()

    button('Run recovery').click()

    expect(notification.success).not.toHaveBeenCalled()
    expect(notification.warning).toHaveBeenCalledWith(
      'Recovery report received',
      'HAI reported 0 recovered of 0 scanned operations. Review the operation ledger for the resulting states.'
    )
  })

  it('keeps emergency stop, approval count, and readiness remediation visible in Basic', () => {
    authSessionService.session.and.returnValue(of(ownerSession))
    runtimeService.readiness.and.returnValue(of({
      ...readiness,
      overallReady: false,
      gates: [{
        name: 'windows.startup',
        status: 'fail',
        evidence: 'Startup task is not registered.',
        remediation: 'Register the signed startup task for this Windows user.',
      }],
    }))
    createComponent()

    const root = fixture.nativeElement as HTMLElement
    expect(button('Engage emergency stop').hidden).toBeFalse()
    expect(text()).toContain('Awaiting approval')
    expect(text()).toContain('2')
    expect(text()).toContain('Register the signed startup task for this Windows user.')
    expect(root.querySelector('.hai-progressive-section__content')).toBeNull()
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

  it('names the icon-only back control for assistive technology', () => {
    authSessionService.session.and.returnValue(of(ownerSession))
    createComponent()

    const back = fixture.nativeElement.querySelector('button[aria-label="Back to previous page"]') as HTMLButtonElement | null
    expect(back).not.toBeNull()
    expect(back?.title).toBe('Back to previous page')
    expect(back?.querySelector('[nz-icon]')?.getAttribute('aria-hidden')).toBe('true')
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
      const available = Array.from(
        (fixture.nativeElement as HTMLElement).querySelectorAll('button')
      ).map((candidate) => candidate.textContent?.trim()).filter(Boolean)
      throw new Error(`Button not found: ${label}; available buttons: ${available.join(', ')}`)
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
