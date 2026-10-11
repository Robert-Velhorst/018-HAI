import { ChangeDetectionStrategy, Component, OnInit } from '@angular/core'
import { Router } from '@angular/router'
import { catchError, finalize, forkJoin, of, switchMap, throwError } from 'rxjs'
import { NzModalService } from 'ng-zorro-antd/modal'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import {
  AuthActorRole,
  IAuthSession,
} from '../../models/auth-session.model.interface'
import {
  ControlApprovalAction,
  IBackgroundStatus,
  IControlAuthorization,
  IDecidedControlApproval,
  IReadiness,
} from '../../models/runtime-control.model.interface'
import { AuthSessionService } from '../../services/auth-session.service'
import { RuntimeControlService } from '../../services/runtime-control.service'

@Component({
  changeDetection: ChangeDetectionStrategy.Eager,
  standalone: false,
  selector: 'app-runtime-control',
  templateUrl: './runtime-control.component.html',
  styleUrls: ['./runtime-control.component.scss'],
})
export class RuntimeControlComponent implements OnInit {
  status?: IBackgroundStatus
  readiness?: IReadiness
  authSession?: IAuthSession
  loading = false
  busy = false
  sessionUnavailable = false
  runtimeLoadError = ''

  readonly modes = ['autonomous_safe', 'approval_required', 'draft_only', 'read_only', 'paused']

  constructor(
    private service: RuntimeControlService,
    private authSessionService: AuthSessionService,
    private modal: NzModalService,
    private notification: NzNotificationService,
    private router: Router
  ) {}

  ngOnInit(): void {
    this.refresh()
  }

  refresh(): void {
    this.loading = true
    this.sessionUnavailable = false
    this.runtimeLoadError = ''
    this.authSession = undefined
    this.status = undefined
    this.readiness = undefined

    this.authSessionService.session().pipe(
      catchError(() => {
        this.sessionUnavailable = true
        return of(undefined)
      }),
      switchMap((session) => {
        this.authSession = session
        if (!this.canReadRuntime) {
          return of({ status: undefined, readiness: undefined })
        }

        return forkJoin({
          status: this.service.status(),
          readiness: this.service.readiness(),
        }).pipe(
          catchError(() => {
            this.runtimeLoadError =
              'Runtime state could not be loaded. No controls were executed.'
            return of({ status: undefined, readiness: undefined })
          })
        )
      }),
      finalize(() => (this.loading = false))
    ).subscribe(({ status, readiness }) => {
      this.status = status
      this.readiness = readiness
    })
  }

  pause(): void {
    if (!this.requirePermission(
      this.canApproveRuntimeActions,
      'Emergency stop',
      this.approvalControlExplanation
    )) {
      return
    }
    this.busy = true
    this.service.pause('operator engaged emergency stop').subscribe({
      next: () => {
        this.busy = false
        this.notification.warning('Emergency stop engaged', 'Background processing is halted.')
        this.refresh()
      },
      error: () => {
        this.busy = false
        this.notification.error('Error', 'Pause failed.')
      },
    })
  }

  resume(): void {
    if (!this.requirePermission(
      this.canAdministerRuntime,
      'Resume',
      this.ownerControlExplanation
    )) {
      return
    }
    this.modal.confirm({
      nzTitle: 'Resume background processing?',
      nzContent:
        'HAI will create a five-minute approval for the current emergency-stop revision, record your owner decision, and consume it once. A changed or replayed request will be rejected.',
      nzOkText: 'Approve and resume',
      nzCancelText: 'Keep stopped',
      nzOnOk: () => this.executeApprovedControlChange('resume'),
    })
  }

  setMode(mode: string): void {
    if (!this.requirePermission(
      this.canAdministerRuntime,
      'Autonomy mode change',
      this.ownerControlExplanation
    )) {
      return
    }
    const currentMode = this.status?.storedMode
    if (!currentMode || currentMode === mode) {
      return
    }
    const currentRank = this.modeAuthorityRank(currentMode)
    const targetRank = this.modeAuthorityRank(mode)
    if (currentRank < 0 || targetRank < 0) {
      this.notification.error(
        'Mode change failed',
        'HAI returned an unsupported autonomy mode; no control was changed.'
      )
      return
    }
    if (targetRank > currentRank) {
      this.modal.confirm({
        nzTitle: `Increase autonomy to ${mode}?`,
        nzContent:
          `This weakens the current ${currentMode} safety boundary. HAI will create a short-lived approval for this exact transition and consume it once.`,
        nzOkText: 'Approve mode increase',
        nzCancelText: 'Keep current mode',
        nzOnOk: () => this.executeApprovedControlChange('set_mode', mode),
      })
      return
    }
    this.busy = true
    this.service.setMode(mode).pipe(
      finalize(() => (this.busy = false))
    ).subscribe({
      next: () => {
        this.notification.success('Mode restricted', `Autonomy mode set to ${mode}.`)
        this.refresh()
      },
      error: (err) => this.notification.error(
        'Mode change failed',
        this.serverError(err, 'The safer mode could not be persisted.')
      ),
    })
  }

  verify(): void {
    if (!this.requirePermission(
      this.canApproveRuntimeActions,
      'Emergency-stop verification',
      this.approvalControlExplanation
    )) {
      return
    }
    this.busy = true
    this.service.verifyEmergencyStop().subscribe({
      next: (v) => {
        this.busy = false
        if (v.halted) {
          this.notification.success('Emergency stop verified', v.detail)
        } else {
          this.notification.error('EMERGENCY STOP FAILED', v.detail)
        }
        this.refresh()
      },
      error: () => {
        this.busy = false
        this.notification.error('Error', 'Verification failed.')
      },
    })
  }

  recover(): void {
    if (!this.requirePermission(
      this.canApproveRuntimeActions,
      'Runtime recovery',
      this.approvalControlExplanation
    )) {
      return
    }
    this.busy = true
    this.service.recover().subscribe({
      next: (r) => {
        this.busy = false
        this.notification.success('Recovery complete', `${r.recovered} operations recovered.`)
        this.refresh()
      },
      error: () => {
        this.busy = false
        this.notification.error('Error', 'Recovery failed.')
      },
    })
  }

  get actorRole(): AuthActorRole {
    return this.authSession?.role ?? 'unknown'
  }

  get canReadRuntime(): boolean {
    return Boolean(
      this.authSession?.authenticated &&
      this.authSession.permissions.canRead
    )
  }

  get canApproveRuntimeActions(): boolean {
    return Boolean(
      this.authSession?.authenticated &&
      this.authSession.permissions.canApprove
    )
  }

  get canAdministerRuntime(): boolean {
    return Boolean(
      this.authSession?.authenticated &&
      this.authSession.permissions.canAdminister
    )
  }

  get authorityTitle(): string {
    switch (this.actorRole) {
      case 'owner':
        return 'Owner authority'
      case 'operator':
        return 'Operator authority'
      case 'viewer':
        return 'Read-only authority'
      default:
        return 'Authority unavailable'
    }
  }

  get authorityDescription(): string {
    switch (this.actorRole) {
      case 'owner':
        return 'Emergency stop, recovery, verification, resume, and autonomy controls are available.'
      case 'operator':
        return 'Emergency stop, recovery, and verification are available. Resume and autonomy changes require the owner.'
      case 'viewer':
        return 'Runtime state is read-only. Operational and owner controls are disabled.'
      default:
        return 'HAI could not verify signed session permissions. All runtime changes are disabled.'
    }
  }

  get authorityTagColor(): string {
    switch (this.actorRole) {
      case 'owner':
        return 'blue'
      case 'operator':
        return 'green'
      case 'viewer':
        return 'default'
      default:
        return 'gold'
    }
  }

  get ownerControlExplanation(): string {
    if (this.actorRole === 'operator') {
      return 'Only the owner can resume processing or change the autonomy mode.'
    }
    if (this.actorRole === 'viewer') {
      return 'Viewer sessions are read-only. Only the owner can change runtime policy.'
    }
    return 'Owner controls are disabled because HAI could not verify owner authority.'
  }

  get approvalControlExplanation(): string {
    if (this.actorRole === 'viewer') {
      return 'Viewer sessions are read-only. An operator or owner is required.'
    }
    return 'This control is disabled because HAI could not verify approval authority.'
  }

  gateColor(status: string): string {
    switch (status) {
      case 'pass':
        return 'green'
      case 'warn':
        return 'gold'
      case 'fail':
        return 'red'
      case 'pending':
        return 'blue'
      case 'not_applicable':
        return 'default'
      default:
        return 'default'
    }
  }

  goBack(): void {
    this.router.navigate(['/control-center'])
  }

  private requirePermission(
    permitted: boolean,
    action: string,
    explanation: string
  ): boolean {
    if (permitted && !this.busy) {
      return true
    }
    if (!permitted) {
      this.notification.warning(`${action} unavailable`, explanation)
    }
    return false
  }

  private executeApprovedControlChange(
    action: ControlApprovalAction,
    targetMode?: string
  ): void {
    if (this.busy) {
      return
    }
    this.busy = true
    this.service.prepareControlApproval(action, targetMode).pipe(
      switchMap((prepared) => this.service.decideControlApproval(
        prepared.requestId,
        'approved',
        action === 'resume'
          ? 'Owner approved this exact emergency-stop recovery.'
          : `Owner approved this exact autonomy transition to ${targetMode}.`
      )),
      switchMap((decision) => {
        let authorization: IControlAuthorization
        try {
          authorization = this.authorizationFromDecision(decision)
        } catch (error) {
          return throwError(() => error)
        }
        if (action === 'resume') {
          return this.service.resume(authorization)
        }
        if (!targetMode) {
          return throwError(() => new Error('Approved mode target is missing.'))
        }
        return this.service.setMode(targetMode, authorization)
      }),
      finalize(() => (this.busy = false))
    ).subscribe({
      next: () => {
        if (action === 'resume') {
          this.notification.success('Resumed', 'The exact recovery approval was consumed once.')
        } else {
          this.notification.success('Mode updated', `Autonomy mode set to ${targetMode}.`)
        }
        this.refresh()
      },
      error: (err) => this.notification.error(
        action === 'resume' ? 'Resume failed' : 'Mode change failed',
        this.serverError(err, 'The exact approval flow did not complete; no safety control was weakened.')
      ),
    })
  }

  private authorizationFromDecision(
    decision: IDecidedControlApproval
  ): IControlAuthorization {
    if (
      decision.decision !== 'approved' ||
      !decision.idempotencyKey ||
      !decision.taskId ||
      !decision.approvalSourceId ||
      !decision.approvalBindingDigest ||
      decision.approvalBindingDigest.length !== 64 ||
      !decision.expiresAt ||
      Date.parse(decision.expiresAt) <= Date.now()
    ) {
      throw new Error('HAI returned incomplete or expired control-approval evidence.')
    }
    return {
      idempotencyKey: decision.idempotencyKey,
      taskId: decision.taskId,
      approvalSourceId: decision.approvalSourceId,
      approvalBindingDigest: decision.approvalBindingDigest,
    }
  }

  private modeAuthorityRank(mode: string): number {
    switch (mode) {
      case 'paused':
        return 0
      case 'read_only':
        return 1
      case 'draft_only':
        return 2
      case 'approval_required':
        return 3
      case 'autonomous_safe':
        return 4
      default:
        return -1
    }
  }

  private serverError(error: any, fallback: string): string {
    return error?.error?.error ?? error?.message ?? fallback
  }
}
