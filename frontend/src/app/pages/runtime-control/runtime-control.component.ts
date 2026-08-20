import { ChangeDetectionStrategy, Component, HostListener, OnInit } from '@angular/core'
import { Router } from '@angular/router'
import {
  Observable,
  catchError,
  finalize,
  forkJoin,
  map,
  of,
  switchMap,
  throwError,
} from 'rxjs'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service'
import {
  AuthActorRole,
  IAuthSession,
} from '../../models/auth-session.model.interface'
import {
  IBackgroundStatus,
  IControlAuthorization,
  IEmergencyStopState,
  IRecoveryReport,
  IReadiness,
  IResumeApprovalRequest,
} from '../../models/runtime-control.model.interface'
import { AuthSessionService } from '../../services/auth-session.service'
import { RuntimeControlService } from '../../services/runtime-control.service'

const MODULE_ID = 'runtime-control'
const RESUME_CONFIRMATION = 'RESUME BACKGROUND PROCESSING'

interface IRuntimeStateLoad {
  session?: IAuthSession
  sessionUnavailable: boolean
  runtimeLoadError: string
  status?: IBackgroundStatus
  readiness?: IReadiness
}

interface IEmergencyStopFanoutSummary {
  status?: string
  message?: string
}

interface IPauseResponse {
  emergencyStop?: IEmergencyStopState
  openClawCancellation?: IEmergencyStopFanoutSummary
}

@Component({
    changeDetection: ChangeDetectionStrategy.Eager,
    selector: 'app-runtime-control',
    templateUrl: './runtime-control.component.html',
    styleUrls: ['./runtime-control.component.scss'],
    standalone: false
})
export class RuntimeControlComponent implements OnInit {
  status?: IBackgroundStatus
  readiness?: IReadiness
  authSession?: IAuthSession
  loading = false
  busy = false
  sessionUnavailable = false
  runtimeLoadError = ''
  resumeApproval?: IResumeApprovalRequest
  resumeConfirmation = ''
  private refreshRequestId = 0

  readonly modes = ['autonomous_safe', 'approval_required', 'draft_only', 'read_only', 'paused']

  constructor(
    private service: RuntimeControlService,
    private authSessionService: AuthSessionService,
    private notification: NzNotificationService,
    private router: Router,
    private viewPreferences: ModuleViewPreferencesService
  ) {}

  ngOnInit(): void {
    this.refresh()
  }

  refresh(): void {
    if (this.loading || this.busy) {
      return
    }

    const requestId = ++this.refreshRequestId
    this.loading = true
    this.sessionUnavailable = false
    this.runtimeLoadError = ''
    this.authSession = undefined
    this.status = undefined
    this.readiness = undefined
    this.resumeApproval = undefined
    this.resumeConfirmation = ''

    this.authSessionService.session().pipe(
      catchError(() => of(undefined)),
      switchMap((session): Observable<IRuntimeStateLoad> => {
        if (!session) {
          return of({
            sessionUnavailable: true,
            runtimeLoadError: '',
          } as IRuntimeStateLoad)
        }
        if (!session.authenticated || !session.permissions.canRead) {
          return of({
            session,
            sessionUnavailable: false,
            runtimeLoadError: '',
          } as IRuntimeStateLoad)
        }

        return forkJoin({
          status: this.service.status(),
          readiness: this.service.readiness(),
        }).pipe(
          map(({ status, readiness }) => ({
            session,
            sessionUnavailable: false,
            runtimeLoadError: '',
            status,
            readiness,
          })),
          catchError(() => of({
            session,
            sessionUnavailable: false,
            runtimeLoadError: 'Runtime state could not be loaded. No controls were executed.',
          } as IRuntimeStateLoad))
        )
      }),
      finalize(() => {
        if (requestId === this.refreshRequestId) {
          this.loading = false
        }
      })
    ).subscribe((snapshot) => {
      if (requestId !== this.refreshRequestId) {
        return
      }
      this.authSession = snapshot.session
      this.sessionUnavailable = snapshot.sessionUnavailable
      this.runtimeLoadError = snapshot.runtimeLoadError
      this.status = snapshot.status
      this.readiness = snapshot.readiness
    })
  }

  @HostListener('window:focus')
  refreshWhenWindowFocused(): void {
    this.refresh()
  }

  pause(): void {
    if (!this.requirePermission(
      this.canOperateRuntime,
      'Emergency stop',
      this.operatorControlExplanation
    )) {
      return
    }
    this.busy = true
    this.service.pause('operator engaged emergency stop').subscribe({
      next: (response) => {
        this.busy = false
        const result = response as (typeof response & IPauseResponse) | null | undefined
        if (result?.emergencyStop?.engaged !== true) {
          this.notification.error(
            'Emergency stop not confirmed',
            'The server response did not confirm a persisted stop. Runtime state is being refreshed.'
          )
          this.refresh()
          return
        }

        const cancellation = result?.openClawCancellation
        if (cancellation?.status === 'complete') {
          this.notification.warning(
            'Emergency stop engaged',
            cancellation.message || 'The persisted stop is active; no unresolved OpenClaw cancellation was reported.'
          )
        } else {
          this.notification.warning(
            'Emergency stop active; remote cancellation incomplete',
            cancellation?.message || 'The persisted stop is active, but remote cancellation status was not confirmed.'
          )
        }
        this.refresh()
      },
      error: () => {
        this.busy = false
        this.notification.error(
          'Emergency stop not confirmed',
          'The request did not confirm a persisted stop. Runtime state is being refreshed.'
        )
        this.refresh()
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
        if (this.emergencyStopStatusInconsistent) {
      this.notification.error(
        'Resume blocked: runtime state conflicts',
        'Refresh the backend-confirmed state before attempting to clear the emergency stop.'
      )
      this.refresh()
      return
    }
    this.busy = true
    this.service.requestResumeApproval().subscribe({
      next: (approval) => {
        this.busy = false
        if (!this.isValidResumeReview(approval)) {
          this.resumeApproval = undefined
          this.notification.error('Resume review unavailable', 'The server returned incomplete or mismatched review evidence.')
          return
        }
        this.resumeApproval = approval
        this.resumeConfirmation = ''
        this.notification.info('Resume review prepared', 'Review the current stop revision, then explicitly confirm before processing resumes.')
      },
      error: (err) => {
        this.busy = false
        this.notification.error('Resume review unavailable', this.errorMessage(err, 'HAI could not create the durable owner review.'))
        this.refresh()
      },
    })
  }

  approveAndResume(): void {
    if (!this.isValidResumeReview(this.resumeApproval) || this.resumeConfirmation !== RESUME_CONFIRMATION) {
      return
    }
    if (!this.requirePermission(this.canAdministerRuntime, 'Resume', this.ownerControlExplanation)) {
      return
    }
    this.busy = true
    this.service.approveAndResume(this.resumeApproval.reviewItemId, this.resumeConfirmation).subscribe({
      next: (result) => {
        this.busy = false
        if (result?.emergencyStop?.engaged !== false) {
          this.notification.error('Resume not confirmed', 'The server did not confirm that the persisted emergency stop was cleared.')
          this.refresh()
          return
        }
        this.resumeApproval = undefined
        this.resumeConfirmation = ''
        this.notification.success('Emergency stop cleared', 'The owner review was consumed for the current stop revision.')
        this.refresh()
      },
      error: (err) => {
        this.busy = false
        this.notification.error('Resume blocked', this.errorMessage(err, 'The review could not be consumed. The stop remains governed by backend state.'))
        this.refresh()
      },
    })
  }

  private isValidResumeReview(approval?: IResumeApprovalRequest): approval is IResumeApprovalRequest {
    return Boolean(
      approval &&
      /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(approval.reviewItemId) &&
      approval.approvalSourceId === `opscontrol-review:${approval.reviewItemId}` &&
      /^[0-9a-f]{64}$/.test(approval.approvalBindingDigest)
    )
  }

  setMode(mode: string): void {
    if (!this.requirePermission(
      this.canAdministerRuntime,
      'Autonomy mode change',
      this.ownerControlExplanation
    )) {
      return
    }
    this.busy = true
    this.service.prepareModeChange(mode).pipe(
      switchMap((result) => {
        if (!result || typeof result.authorizationRequired !== 'boolean') {
          return throwError(() => new Error('The server did not confirm whether this mode change requires approval.'))
        }
        if (result.authorizationRequired) {
          if (!this.hasControlAuthorization(result.authorization)) {
            return throwError(() => new Error(
              'The server required owner approval but returned incomplete approval proof. The mode was not changed.'
            ))
          }
          return this.service.setMode(mode, result.authorization)
        }
        return this.service.setMode(mode)
      })
    ).subscribe({
      next: (result) => {
        this.busy = false
        if (result?.mode !== mode) {
          this.notification.error(
            'Mode change not confirmed',
            'The server response did not confirm the requested mode. Runtime state is being refreshed.'
          )
          this.refresh()
          return
        }
        this.notification.success('Mode confirmed', `The server confirmed autonomy mode: ${result.mode}.`)
        this.refresh()
      },
      error: (err) => {
        this.busy = false
        this.notification.error(
          'Mode change not confirmed',
          this.errorMessage(err, 'The displayed mode will be reconciled with backend state.')
        )
        this.refresh()
      },
    })
  }

  verify(): void {
    if (!this.requirePermission(
      this.canOperateRuntime,
      'Emergency-stop verification',
      this.operatorControlExplanation
    )) {
      return
    }
    this.busy = true
    this.service.verifyEmergencyStop().subscribe({
      next: (v) => {
        this.busy = false
        if (
          v?.engagedDuringTest === true &&
          v.halted === true &&
          v.operationsProcessedDuringStop === 0 &&
          v.restoredEngagedState === true
        ) {
          this.notification.success('Emergency stop verified', v.detail)
        } else {
          this.notification.error(
            'Emergency stop verification not confirmed',
            v?.detail || 'The stop did not pass all checks or the prior stop state was not confirmed restored.'
          )
        }
        this.refresh()
      },
      error: () => {
        this.busy = false
        this.notification.error('Emergency stop verification failed', 'The verification result could not be confirmed.')
        this.refresh()
      },
    })
  }

  recover(): void {
    if (!this.requirePermission(
      this.canOperateRuntime,
      'Runtime recovery',
      this.operatorControlExplanation
    )) {
      return
    }
    this.busy = true
    this.service.recover().subscribe({
      next: (r) => {
        this.busy = false
        if (!this.isValidRecoveryReport(r)) {
          this.notification.error(
            'Recovery report not confirmed',
            'The server returned an incomplete recovery report. Check the operation ledger after refreshing.'
          )
          this.refresh()
          return
        }
        const scanned = r.scannedRunning + r.scannedVerifying
        if (r.recovered < scanned) {
          this.notification.warning(
            'Recovery needs review',
            `HAI reported ${r.recovered} recovered of ${scanned} scanned operations. Review unresolved operations.`
          )
        } else {
          this.notification.warning(
            'Recovery report received',
            `HAI reported ${r.recovered} recovered of ${scanned} scanned operations. Review the operation ledger for the resulting states.`
          )
        }
        this.refresh()
      },
      error: () => {
        this.busy = false
        this.notification.error('Recovery not confirmed', 'The server did not return a recovery report.')
        this.refresh()
      },
    })
  }

  get actorRole(): AuthActorRole {
    return this.authSession?.role ?? 'unknown'
  }

  get isAdvanced(): boolean {
    return this.viewPreferences.get(MODULE_ID).mode === 'advanced'
  }

  get readinessIssues() {
    return (this.readiness?.gates ?? []).filter(
      (gate) => gate.status !== 'pass' && gate.status !== 'not_applicable'
    )
  }

  resetView(): void {
    this.viewPreferences.reset(MODULE_ID)
    document.body.classList.remove('hai-view-advanced')
    void this.router.navigate(['/runtime-control'], {
      queryParams: { mode: 'basic' },
      replaceUrl: true,
    })
  }

  get canReadRuntime(): boolean {
    return Boolean(
      this.authSession?.authenticated &&
      this.authSession.permissions.canRead
    )
  }

  get canOperateRuntime(): boolean {
    return Boolean(
      this.authSession?.authenticated &&
      this.authSession.permissions.canOperate
    )
  }

  get canAdministerRuntime(): boolean {
    return Boolean(
      this.authSession?.authenticated &&
      this.authSession.permissions.canAdminister
    )
  }

  get emergencyStopStatusInconsistent(): boolean {
    return Boolean(
      this.status?.emergencyStop.engaged &&
      this.status.backgroundProcessingActive
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
        return [
          this.canOperateRuntime
            ? 'Emergency stop, recovery, and verification are enabled.'
            : 'Runtime execution actions are disabled because execute permission is absent.',
          this.canAdministerRuntime
            ? 'Resume and autonomy changes require current owner approval.'
            : 'Resume and autonomy changes are disabled because admin permission is absent.',
        ].join(' ')
      case 'operator':
        return [
          this.canOperateRuntime
            ? 'Emergency stop, recovery, and verification are enabled.'
            : 'Runtime execution actions are disabled because execute permission is absent.',
          'Resume and autonomy changes require owner authority.',
        ].join(' ')
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

  get operatorControlExplanation(): string {
    if (this.actorRole === 'viewer') {
      return 'Viewer sessions are read-only. An operator or owner with runtime execution permission is required.'
    }
    if (this.actorRole === 'unknown') {
      return 'This control is disabled because HAI could not verify runtime execution permission.'
    }
    return 'The signed session does not currently grant runtime execution permission.'
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

  private hasControlAuthorization(
    authorization: IControlAuthorization | undefined
  ): authorization is IControlAuthorization {
    return Boolean(
      authorization &&
      authorization.idempotencyKey?.trim() &&
      authorization.taskId?.trim() &&
      authorization.approvalSourceId?.trim() &&
      authorization.approvalBindingDigest?.trim()
    )
  }

  private isValidRecoveryReport(report: IRecoveryReport): boolean {
    return Boolean(
      report &&
      Number.isInteger(report.scannedRunning) && report.scannedRunning >= 0 &&
      Number.isInteger(report.scannedVerifying) && report.scannedVerifying >= 0 &&
      Number.isInteger(report.recovered) && report.recovered >= 0 &&
      report.recovered <= report.scannedRunning + report.scannedVerifying &&
      typeof report.ranAt === 'string' && report.ranAt.length > 0
    )
  }

  private errorMessage(error: unknown, fallback: string): string {
    const candidate = typeof error === 'object' && error !== null
      ? error as { error?: unknown; message?: unknown }
      : undefined
    const body = typeof candidate?.error === 'object' && candidate.error !== null
      ? candidate.error as { error?: unknown }
      : undefined
    const serverMessage = body?.error
    if (typeof serverMessage === 'string' && serverMessage.trim()) {
      return serverMessage
    }
    if (typeof candidate?.message === 'string' && candidate.message.trim()) {
      return candidate.message
    }
    return fallback
  }
}
