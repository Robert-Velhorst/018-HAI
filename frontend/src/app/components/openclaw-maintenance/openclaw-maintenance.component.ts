import { ChangeDetectionStrategy, ChangeDetectorRef, Component, DestroyRef, inject, OnInit } from '@angular/core'
import { CommonModule } from '@angular/common'
import { HttpClient, HttpErrorResponse } from '@angular/common/http'
import { takeUntilDestroyed } from '@angular/core/rxjs-interop'
import { finalize, interval } from 'rxjs'
import { NzButtonModule } from 'ng-zorro-antd/button'
import { NzIconModule } from 'ng-zorro-antd/icon'
import { NzPopconfirmModule } from 'ng-zorro-antd/popconfirm'

interface MaintenanceTarget {
  id: string
  policy: string
  installed: string
  available: string
  state: string
  reason: string
  checkedAt: string | null
  nextCheck?: string | null
  verifiedAt: string | null
  receiptId: string
  reviewRequired: boolean
  pendingKind: string
  pendingStatus?: string
  pendingStartedAt?: string | null
  installBlockedReason?: string
  installStatus?: string
}

interface WorkerContact {
  state: 'disabled' | 'unknown' | 'recent_contact' | 'stale_contact' | string
  lastSeenAt?: string
  freshFor: string
}

interface InstallationCapability {
  available: boolean
  reason?: string
}

interface MaintenanceOverview {
  enabled: boolean
  worker?: WorkerContact
  installation?: InstallationCapability
  targets: MaintenanceTarget[]
}

type MaintenanceAction = 'check' | 'apply' | 'policy' | 'review'

const stateLabels: Record<string, string> = {
  disabled: 'Disabled',
  unknown: 'Not checked',
  current: 'No newer release reported',
  update_available: 'Update available',
  installing: 'Installation in progress',
  verified: 'Installation verified',
  check_failed: 'Check failed',
  failed: 'Failed',
  needs_review: 'Needs review',
}

const checkFreshnessWindowMs = 24 * 60 * 60 * 1000
const jobStatusPollIntervalMs = 5_000
const maxJobStatusPollAttempts = 24

type SubmittedJobKind = 'check' | 'apply'

interface SubmittedMaintenanceJob {
  kind: SubmittedJobKind
  attempts: number
}

@Component({
  selector: 'hai-openclaw-maintenance',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [CommonModule, NzButtonModule, NzIconModule, NzPopconfirmModule],
  template: `
    <section aria-labelledby="openclaw-maintenance-heading">
      <header>
        <h2 id="openclaw-maintenance-heading">OpenClaw updates</h2>
        <button nz-button nzType="text" aria-label="Refresh OpenClaw update status" title="Refresh stored update status" (click)="load()" [disabled]="loading">
          <span nz-icon nzType="reload"></span>
        </button>
      </header>

      <p *ngIf="loading" role="status">Loading stored maintenance status...</p>
      <p *ngIf="error" role="alert">{{ error }}</p>
      <p *ngIf="pollingError" role="alert">{{ pollingError }}</p>
      <p *ngIf="pollingNotice" role="status">{{ pollingNotice }}</p>
      <p *ngIf="actionError" role="alert">{{ actionError }}</p>
      <p *ngIf="!loading && !error && !pollingError && !enabled" role="status">
        OpenClaw maintenance is disabled on this HAI server. The stored target information below is informational; no checks or updates can be requested.
      </p>
      <p *ngIf="!loading && enabled" role="status" class="worker-contact">{{ workerContactLabel() }}</p>
      <p *ngIf="!loading && enabled && installation && !installation.available" role="status" class="install-capability">
        {{ installation.reason }} Read-only version checks remain available; no update will be launched.
      </p>
      <p *ngIf="!loading && !error && enabled && targets.length === 0" role="status">HAI returned no OpenClaw maintenance targets.</p>

      <article *ngFor="let target of targets">
        <div class="identity">
          <h3>{{ target.id === 'companion' ? 'Windows Companion' : 'Gateway / core' }}</h3>
          <span>{{ target.installed || 'Installed version unknown' }}</span>
          <span class="state">{{ targetStateLabel(target) }}</span>
          <span class="policy">Policy: {{ policyLabel(target.policy) }}</span>
        </div>

        <div class="freshness" [class.freshness-current]="observationFreshness(target) === 'current'" [class.freshness-stale]="observationFreshness(target) === 'stale'" role="status">
          <strong>{{ observationFreshnessLabel(target) }}</strong>
          <span>Last successful check: {{ timestampLabel(target.checkedAt) }}</span>
          <span>Next scheduled check: {{ timestampLabel(target.nextCheck) }}</span>
          <p>{{ observationFreshnessSummary(target) }}</p>
        </div>

        <p *ngIf="target.reason" [attr.role]="target.reviewRequired || target.state === 'needs_review' ? 'alert' : 'status'">{{ target.reason }}</p>
        <p *ngIf="target.installBlockedReason" role="alert">{{ target.installBlockedReason }}</p>
        <p *ngIf="target.pendingKind" role="status">{{ pendingLabel(target) }}</p>
        <p *ngIf="busy[target.id]" role="status">{{ actionPendingLabel(busy[target.id]) }}</p>
        <p *ngIf="target.reviewRequired" role="alert">
          An earlier update has an uncertain outcome. Before acknowledging recovery, inspect the installation and complete a successful read-only check after that update. HAI must also confirm runtime health. Recovery leaves the update policy in Observe only.
        </p>
        <p *ngIf="target.state === 'update_available'">The last stored check reported a newer release; an installation has not been confirmed by this status.</p>
        <p *ngIf="target.available && target.available !== target.installed">Available: {{ target.available }}</p>

        <div class="actions">
          <button nz-button [disabled]="!enabled || loading || !!busy[target.id] || !!target.pendingKind" (click)="act(target, 'check')">
            <span nz-icon nzType="reload"></span> Check for updates
          </button>
          <button nz-button nzType="primary" *ngIf="target.state === 'update_available' && target.policy === 'auto_install_verified'" [disabled]="!enabled || loading || !!busy[target.id] || !!target.pendingKind || target.reviewRequired || observationFreshness(target) !== 'current' || target.installStatus === 'blocked' || !!target.installBlockedReason || installation?.available === false" (click)="act(target, 'apply')">
            Request installation of {{ target.available }}
          </button>
          <button nz-button *ngIf="target.reviewRequired" [disabled]="!enabled || loading || !!busy[target.id] || !!target.pendingKind" nz-popconfirm nzPopconfirmTitle="Have you inspected the installation and completed a successful read-only check since the interrupted update? HAI must also confirm runtime health." (nzOnConfirm)="act(target, 'review')">
            Acknowledge recovery
          </button>
        </div>

        <details>
          <summary>Update settings and history</summary>
          <label>
            Automatic verified updates
            <input type="checkbox" [checked]="target.policy === 'auto_install_verified'" [disabled]="!enabled || loading || !!busy[target.id]" (change)="policy(target, $event)" />
          </label>
          <p>Policy changes do not confirm release safety or an installation result. HAI checks release evidence and active OpenClaw work before an installation can start.</p>
          <dl>
            <dt>Last successful check</dt><dd>{{ target.checkedAt ? (target.checkedAt | date:'medium') : 'Not checked' }}</dd>
            <dt>Last verified update</dt><dd>{{ target.verifiedAt ? (target.verifiedAt | date:'medium') : 'None reported' }}</dd>
            <dt>Receipt</dt><dd>{{ target.receiptId || 'None' }}</dd>
          </dl>
        </details>
      </article>

      <p role="status" aria-live="polite" *ngIf="message">{{ message }}</p>
    </section>`,
  styles: [`
    :host { display:block; margin-block:24px; color:inherit; }
    section { border-block:1px solid var(--hai-border, #697382); padding-block:16px; }
    header,.identity,.actions { display:flex; align-items:center; flex-wrap:wrap; gap:12px; }
    header { justify-content:space-between; }
    .worker-contact,.install-capability { font-size:13px; }
    .install-capability { border-inline-start:3px solid #d89614; padding-inline:10px; }
    h2 { font-size:18px; margin:0; color:inherit; }
    h3 { font-size:15px; margin:0; color:inherit; }
    article { padding-block:16px; }
    article+article { border-top:1px solid var(--hai-border, #697382); }
    .state,.policy { font-size:13px; }
    .freshness { display:grid; gap:4px; margin-block:12px; border-inline-start:3px solid #8a94a6; padding:8px 12px; font-size:13px; }
    .freshness-current { border-color:#2f9e73; }
    .freshness-stale { border-color:#d89614; }
    .freshness p { margin:2px 0 0; }
    details { margin-top:12px; }
    summary { cursor:pointer; padding-block:8px; }
    label { display:flex; align-items:center; gap:12px; padding-block:8px; }
    input { width:20px; height:20px; }
    dl { display:grid; grid-template-columns:minmax(100px,auto) minmax(0,1fr); gap:8px 16px; }
    dd { margin:0; overflow-wrap:anywhere; }
    p { margin-block:10px; }
    button { min-height:36px; }
  `],
})
export class OpenclawMaintenanceComponent implements OnInit {
  private http = inject(HttpClient)
  private destroyRef = inject(DestroyRef)
  private changeDetector = inject(ChangeDetectorRef)
  targets: MaintenanceTarget[] = []
  worker: WorkerContact | null = null
  installation: InstallationCapability | null = null
  enabled = false
  loading = false
  error = ''
  pollingError = ''
  pollingNotice = ''
  actionError = ''
  message = ''
  busy: Record<string, string> = {}
  private readonly submittedJobs = new Map<string, SubmittedMaintenanceJob>()
  private statusPollTimer: ReturnType<typeof setTimeout> | null = null

  ngOnInit(): void {
    this.destroyRef.onDestroy(() => this.stopStatusPolling())
    this.load()
    interval(60_000).pipe(takeUntilDestroyed(this.destroyRef)).subscribe(() => this.changeDetector.markForCheck())
  }

  stateLabel(state: string): string {
    return stateLabels[state] || (state ? `Unrecognized status (${state})` : 'Status unavailable')
  }

  policyLabel(policy: string): string {
    if (policy === 'auto_install_verified') return 'Automatic verified updates'
    if (policy === 'observe') return 'Observe only'
    return policy ? `Unknown policy (${policy})` : 'Policy not reported'
  }

  targetStateLabel(target: MaintenanceTarget): string {
    if (target.state === 'update_available' && (target.installStatus === 'blocked' || target.installBlockedReason || this.installation?.available === false)) {
      return 'Update blocked'
    }
    if (target.state === 'current') {
      const freshness = this.observationFreshness(target)
      if (freshness === 'stale') return 'Stale: last check found no newer release'
      if (freshness === 'unknown') return 'Freshness unknown'
      return 'Current: no newer release reported'
    }
    if (target.state === 'update_available' && this.observationFreshness(target) !== 'current') {
      return this.observationFreshness(target) === 'stale' ? 'Stale: update reported available' : 'Update available; freshness unknown'
    }
    return this.stateLabel(target.state)
  }

  observationFreshness(target: MaintenanceTarget, now = Date.now()): 'current' | 'stale' | 'unknown' {
    const checkedAt = Date.parse(target.checkedAt || '')
    if (!Number.isFinite(checkedAt) || checkedAt > now) return 'unknown'
    if (now - checkedAt >= checkFreshnessWindowMs) return 'stale'

    const nextCheck = Date.parse(target.nextCheck || '')
    if (Number.isFinite(nextCheck) && nextCheck <= now) return 'stale'
    return 'current'
  }

  observationFreshnessLabel(target: MaintenanceTarget): string {
    const freshness = this.observationFreshness(target)
    if (freshness === 'stale') return 'Stale observation'
    if (freshness === 'unknown') return 'Freshness unknown'
    if (target.state === 'current') return 'Current observation: no newer release reported'
    if (target.state === 'update_available') return 'Current check reported a newer release'
    return 'Last successful check is within the 24-hour freshness window'
  }

  observationFreshnessSummary(target: MaintenanceTarget): string {
    const freshness = this.observationFreshness(target)
    if (freshness === 'stale') {
      return target.state === 'current'
        ? 'The last successful check reported no newer release, but that observation is stale and is not a current finding.'
        : 'The stored maintenance status is based on a stale check and should be refreshed before relying on it.'
    }
    if (freshness === 'unknown') {
      return target.state === 'current'
        ? 'The stored status says no newer release, but HAI has no valid successful-check time to confirm that this is current.'
        : 'HAI has no valid successful-check time, so the freshness of this stored status is unknown.'
    }
    if (target.state === 'current') return 'A recent successful check reported no newer release.'
    if (target.state === 'update_available') return 'A recent successful check reported a newer release; this is not confirmation that an update was installed.'
    return 'This status comes from a successful check within the current freshness window.'
  }

  timestampLabel(value: string | null | undefined): string {
    const timestamp = Date.parse(value || '')
    return Number.isFinite(timestamp) ? new Date(timestamp).toLocaleString() : 'Not reported'
  }

  workerContactLabel(): string {
    if (!this.worker || this.worker.state === 'unknown') return 'Windows worker: no contact observed since this backend started.'
    if (this.worker.state === 'disabled') return 'Windows worker contact: maintenance is disabled.'
    if (this.worker.state === 'recent_contact') {
      const seen = this.worker.lastSeenAt ? new Date(this.worker.lastSeenAt).toLocaleString() : 'recently'
      return `Windows worker contacted HAI at ${seen}. This confirms API contact, not process health.`
    }
    const seen = this.worker.lastSeenAt ? new Date(this.worker.lastSeenAt).toLocaleString() : 'unknown'
    return `No worker contact for at least ${this.worker.freshFor}; last observed at ${seen}. This does not prove whether the process has stopped.`
  }

  pendingLabel(target: MaintenanceTarget): string {
    if (target.pendingStatus === 'pending') {
      return target.pendingKind === 'apply'
        ? 'Installation request is queued; the worker has not confirmed a start.'
        : 'Read-only check is queued; the worker has not confirmed a start.'
    }
    if (target.pendingStatus === 'leased' && target.pendingStartedAt) {
      return `Worker confirmed the ${target.pendingKind === 'apply' ? 'installation job' : 'read-only check lease'} at ${new Date(target.pendingStartedAt).toLocaleString()}. This does not prove the installer started or completed.`
    }
    if (target.pendingStatus === 'leased') return 'Job was leased; execution start has not yet been confirmed.'
    if (target.pendingKind === 'check') return 'A read-only check is queued; no completion is confirmed yet.'
    if (target.pendingKind === 'apply') return 'An update is queued; no installation result is confirmed yet.'
    return 'A maintenance job is queued; no completion is confirmed yet.'
  }

  actionPendingLabel(action: string): string {
    const labels: Record<string, string> = {
      check: 'Submitting read-only check request...',
      apply: 'Submitting installation request...',
      policy: 'Saving update policy...',
      review: 'Submitting recovery acknowledgement...',
    }
    return labels[action] || 'Submitting maintenance request...'
  }

  load(preserveMessage = false): void {
    if (this.loading) return
    this.loading = true
    this.error = ''
    this.actionError = ''
    if (!preserveMessage) this.message = ''
    this.changeDetector.markForCheck()
    this.http.get<MaintenanceOverview>('/api/v1/openclaw-maintenance')
      .pipe(takeUntilDestroyed(this.destroyRef), finalize(() => {
        this.loading = false
        this.changeDetector.markForCheck()
      })).subscribe({
        next: value => {
          this.targets = value.targets
          this.enabled = value.enabled
          this.worker = value.worker || null
          this.installation = value.installation || null
          this.reconcileSubmittedJobs()
          this.changeDetector.markForCheck()
        },
        error: (err: HttpErrorResponse) => {
          this.enabled = false
          const message = err.status === 403 || err.status === 401
            ? 'Sign in as the owner to manage OpenClaw updates.'
            : 'Stored maintenance status could not be loaded. Previously displayed target information may be stale.'
          if (this.submittedJobs.size > 0) {
            this.pollingError = 'HAI could not refresh the submitted job status. The last displayed state may be stale; no completion is confirmed.'
            this.scheduleStatusPoll()
          } else {
            this.error = message
          }
          this.changeDetector.markForCheck()
        },
      })
  }

  policy(target: MaintenanceTarget, event: Event): void {
    const input = event.target as HTMLInputElement
    const selected = input.checked
    input.checked = target.policy === 'auto_install_verified'
    this.act(target, 'policy', selected ? 'auto_install_verified' : 'observe')
  }

  act(target: MaintenanceTarget, action: MaintenanceAction, policy?: string): void {
    if (this.busy[target.id] || this.loading || !this.enabled) return
    if (action !== 'policy' && target.pendingKind) return
    if (action === 'apply' && (target.state !== 'update_available' || target.policy !== 'auto_install_verified' || target.reviewRequired || this.observationFreshness(target) !== 'current' || target.installStatus === 'blocked' || this.installation?.available === false)) return
    if (action === 'review' && !target.reviewRequired) return
    if (action === 'policy' && policy !== 'observe' && policy !== 'auto_install_verified') return

    this.busy[target.id] = action
    this.actionError = ''
    this.message = ''
    this.changeDetector.markForCheck()
    this.http.post(`/api/v1/openclaw-maintenance/${target.id}`, { action, version: action === 'apply' ? target.available : '', policy: policy || '' })
      .pipe(takeUntilDestroyed(this.destroyRef), finalize(() => {
        this.busy[target.id] = ''
        this.changeDetector.markForCheck()
      })).subscribe({
        next: () => {
          this.message = this.actionResultMessage(action, policy)
          if (action === 'check' || action === 'apply') this.trackSubmittedJob(target.id, action)
          this.changeDetector.markForCheck()
          this.load(true)
        },
        error: (err: HttpErrorResponse) => {
          this.actionError = this.actionErrorMessage(err)
          this.changeDetector.markForCheck()
        },
      })
  }

  private actionResultMessage(action: MaintenanceAction, policy?: string): string {
    if (action === 'policy') return `HAI accepted the policy change to ${this.policyLabel(policy || '')}.`
    if (action === 'review') return 'HAI recorded the recovery acknowledgement. The update policy remains Observe only.'
    if (action === 'apply') return 'HAI accepted the installation request. Acceptance is not completion evidence; use the refreshed status below for the backend outcome.'
    return 'HAI accepted the check request. The response has no check result; use the refreshed status below for the worker outcome.'
  }

  private trackSubmittedJob(targetId: string, kind: SubmittedJobKind): void {
    this.submittedJobs.set(targetId, { kind, attempts: 0 })
    this.pollingError = ''
    this.pollingNotice = ''
  }

  private reconcileSubmittedJobs(): void {
    let allSubmittedTargetsReported = true
    for (const [targetId, job] of this.submittedJobs) {
      const target = this.targets.find(item => item.id === targetId)
      if (!target) {
        allSubmittedTargetsReported = false
        continue
      }
      if (this.isSubmittedJobPending(target, job.kind)) continue
      this.submittedJobs.delete(targetId)
    }

    if (this.submittedJobs.size === 0) {
      this.stopStatusPolling()
      this.pollingError = ''
      this.pollingNotice = ''
      return
    }

    if (allSubmittedTargetsReported) this.pollingError = ''
    this.updatePollingNotice()
    this.scheduleStatusPoll()
  }

  private isSubmittedJobPending(target: MaintenanceTarget, kind: SubmittedJobKind): boolean {
    const kindMatches = target.pendingKind === kind ||
      (kind === 'check' && target.pendingKind === 'apply')
    if (!kindMatches) return false
    return !target.pendingStatus || target.pendingStatus === 'pending' || target.pendingStatus === 'leased'
  }

  private scheduleStatusPoll(): void {
    if (this.statusPollTimer !== null || this.submittedJobs.size === 0) return
    if (![...this.submittedJobs.values()].some(job => job.attempts < maxJobStatusPollAttempts)) {
      this.updatePollingNotice()
      return
    }

    this.statusPollTimer = setTimeout(() => {
      this.statusPollTimer = null
      if (this.submittedJobs.size === 0) return
      for (const job of this.submittedJobs.values()) {
        if (job.attempts < maxJobStatusPollAttempts) job.attempts += 1
      }
      if (this.loading) {
        this.updatePollingNotice()
        this.scheduleStatusPoll()
        this.changeDetector.markForCheck()
        return
      }
      this.load(true)
    }, jobStatusPollIntervalMs)
  }

  private updatePollingNotice(): void {
    const exhausted = [...this.submittedJobs.values()].some(job => job.attempts >= maxJobStatusPollAttempts)
    this.pollingNotice = exhausted
      ? `Automatic status refresh paused after ${maxJobStatusPollAttempts} attempts for at least one submitted job. No terminal status is confirmed; refresh manually to check again.`
      : ''
  }

  private stopStatusPolling(): void {
    if (this.statusPollTimer !== null) {
      clearTimeout(this.statusPollTimer)
      this.statusPollTimer = null
    }
    this.submittedJobs.clear()
  }

  private actionErrorMessage(err: HttpErrorResponse): string {
    if (err.status === 401 || err.status === 403) return 'Sign in as the owner to manage OpenClaw updates.'
    const apiError = err.error && typeof err.error === 'object' ? err.error.error : undefined
    if (typeof apiError === 'string' && apiError.trim()) return `${apiError} No update outcome has been confirmed.`
    if (err.status === 0 || err.status >= 500) return 'HAI did not confirm whether it accepted this request. Refresh status before retrying; no update outcome is confirmed.'
    return 'HAI rejected the request. No update outcome has been confirmed.'
  }
}
