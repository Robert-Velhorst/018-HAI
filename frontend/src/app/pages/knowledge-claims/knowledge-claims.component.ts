import { ChangeDetectionStrategy, Component, OnDestroy, OnInit, ViewChild } from '@angular/core'
import { ActivatedRoute, Router } from '@angular/router'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import { catchError, forkJoin, of, Subscription } from 'rxjs'
import { HaiProgressiveSectionComponent } from '../../control-room/progressive-section.component'
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service'
import {
  ClaimAssessmentStatus,
  IClaimLifecycle,
  IClaimReviewItem,
  IClaimReviewQueue,
} from '../../models/knowledge-claim.model.interface'
import { IAuthSession } from '../../models/auth-session.model.interface'
import { AuthSessionService } from '../../services/auth-session.service'
import { KnowledgeClaimService } from '../../services/knowledge-claim.service'

type ClaimFilter = 'attention' | 'all' | ClaimAssessmentStatus

@Component({
    changeDetection: ChangeDetectionStrategy.Eager,
    selector: 'app-knowledge-claims',
    templateUrl: './knowledge-claims.component.html',
    styleUrls: ['./knowledge-claims.component.scss'],
    standalone: false
})
export class KnowledgeClaimsComponent implements OnInit, OnDestroy {
  @ViewChild('claimRegisterSection') claimRegisterSection?: HaiProgressiveSectionComponent

  readonly moduleId = 'knowledge-claims'
  workspaceId = '018-HAI'
  queue?: IClaimReviewQueue
  session?: IAuthSession
  selected?: IClaimReviewItem
  lifecycle?: IClaimLifecycle
  loading = true
  detailLoading = false
  saving = false
  errorMessage = ''
  authorityError = ''
  lifecycleError = ''
  filter: ClaimFilter = 'attention'
  inspectorOpen = false
  correctionOpen = false
  correctedObject = ''
  correctionReason = ''
  correctionEffectiveFrom = ''
  correctionConfirmed = false
  private loadSubscription?: Subscription
  private lifecycleSubscription?: Subscription
  private correctionSubscription?: Subscription

  readonly filters: Array<{ value: ClaimFilter; label: string }> = [
    { value: 'attention', label: 'Needs attention' },
    { value: 'all', label: 'All claims' },
    { value: 'conflicting', label: 'Conflicts' },
    { value: 'needs_review', label: 'Needs review' },
    { value: 'supported', label: 'Supported' },
    { value: 'corroborated', label: 'Corroborated' },
    { value: 'superseded', label: 'Superseded' },
  ]

  constructor(
    private claims: KnowledgeClaimService,
    private authSession: AuthSessionService,
    private notification: NzNotificationService,
    private route: ActivatedRoute,
    private router: Router,
    private viewPreferences: ModuleViewPreferencesService,
  ) {}

  ngOnInit(): void {
    this.workspaceId = this.route.snapshot.queryParamMap.get('workspaceId') || '018-HAI'
    this.load(this.route.snapshot.queryParamMap.get('claimId') || '')
  }

  get attentionItems(): IClaimReviewItem[] {
    return (this.queue?.items || []).filter((item) =>
      item.assessment.status === 'conflicting' ||
      item.assessment.status === 'needs_review' ||
      item.assessment.truncated,
    )
  }

  ngOnDestroy(): void {
    this.loadSubscription?.unsubscribe()
    this.lifecycleSubscription?.unsubscribe()
    this.correctionSubscription?.unsubscribe()
    this.session = undefined
  }

  get basicAttentionItems(): IClaimReviewItem[] {
    return this.attentionItems.slice(0, 3)
  }

  get visibleItems(): IClaimReviewItem[] {
    const items = this.queue?.items || []
    if (this.filter === 'all') return items
    if (this.filter === 'attention') return this.attentionItems
    return items.filter((item) => item.assessment.status === this.filter)
  }

  get canApprove(): boolean {
    return !this.loading && !this.authorityError
      && this.session?.authenticated === true
      && !!this.session.subject?.trim()
      && this.session.permissions?.canApprove === true
  }

  count(status: ClaimAssessmentStatus): number {
    return this.queue?.counts?.[status] || 0
  }

  load(selectClaimId = ''): void {
    this.loadSubscription?.unsubscribe()
    this.session = undefined
    this.authorityError = ''
    this.correctionConfirmed = false
    const workspace = this.workspaceId.trim()
    if (!workspace) {
      this.loading = false
      this.errorMessage = 'Enter a workspace before loading claim records.'
      return
    }
    this.loading = true
    this.errorMessage = ''
    this.loadSubscription = forkJoin({
      queue: this.claims.reviewQueue(workspace),
      session: this.authSession.session().pipe(catchError((error) => {
        this.revokeAuthority(error)
        return of(undefined)
      })),
    }).subscribe({
      next: ({ queue, session }) => {
        this.queue = queue
        this.session = session
        this.loading = false
        if (!this.authorityError && (!session?.authenticated || !session.subject?.trim())) this.revokeAuthority({ status: 401 })
        const selected = queue.items.find((item) => item.claim.id === selectClaimId)
        if (selected) this.openInspector(selected)
      },
      error: (error) => {
        this.loading = false
        this.revokeAuthority(error)
        this.errorMessage = this.describeError(error, 'Claim review is unavailable.')
      },
    })
  }

  applyWorkspace(): void {
    this.closeInspector()
    this.router.navigate([], {
      relativeTo: this.route,
      queryParams: { workspaceId: this.workspaceId.trim() || null, claimId: null },
      queryParamsHandling: 'merge',
      replaceUrl: true,
    })
    this.load()
  }

  openInspector(item: IClaimReviewItem): void {
    this.selected = item
    this.inspectorOpen = true
    this.detailLoading = true
    this.lifecycle = undefined
    this.lifecycleError = ''
    this.correctionOpen = false
    this.router.navigate([], {
      relativeTo: this.route,
      queryParams: { workspaceId: this.workspaceId, claimId: item.claim.id },
      queryParamsHandling: 'merge',
      replaceUrl: true,
    })
    this.loadLifecycle(item.claim.id)
  }

  retryLifecycle(): void {
    if (this.selected && !this.detailLoading) this.loadLifecycle(this.selected.claim.id)
  }

  openClaimRegister(): void {
    this.viewPreferences.setMode(this.moduleId, 'advanced')
    this.claimRegisterSection?.setOpen(true)
    this.router.navigate([], {
      relativeTo: this.route,
      queryParams: { mode: 'advanced' },
      queryParamsHandling: 'merge',
    })
  }

  private loadLifecycle(claimId: string): void {
    this.lifecycleSubscription?.unsubscribe()
    this.detailLoading = true
    this.lifecycleError = ''
    this.lifecycleSubscription = this.claims.lifecycle(this.workspaceId, claimId).subscribe({
      next: (lifecycle) => {
        this.lifecycle = lifecycle
        this.detailLoading = false
      },
      error: (error) => {
        this.detailLoading = false
        if (error?.status === 401 || error?.status === 403) this.revokeAuthority(error)
        this.lifecycleError = this.describeError(error, 'The lifecycle could not be loaded.')
        this.notification.error('Claim detail unavailable', this.lifecycleError)
      },
    })
  }

  closeInspector(): void {
    this.lifecycleSubscription?.unsubscribe()
    this.detailLoading = false
    this.inspectorOpen = false
    this.selected = undefined
    this.lifecycle = undefined
    this.correctionOpen = false
    this.router.navigate([], {
      relativeTo: this.route,
      queryParams: { claimId: null },
      queryParamsHandling: 'merge',
      replaceUrl: true,
    })
  }

  beginCorrection(): void {
    if (!this.selected || !this.canApprove) return
    this.correctedObject = this.selected.claim.object
    this.correctionReason = ''
    this.correctionEffectiveFrom = ''
    this.correctionConfirmed = false
    this.correctionOpen = true
  }

  submitCorrection(): void {
    if (!this.selected || !this.canApprove || !this.correctionConfirmed || this.saving) return
    const correctedObject = this.correctedObject.trim()
    const reason = this.correctionReason.trim()
    if (!correctedObject || correctedObject === this.selected.claim.object || !reason) {
      this.notification.warning('Correction incomplete', 'Change the claim value and record why it is being corrected.')
      return
    }
    const effectiveFrom = this.correctionEffectiveFrom ? new Date(this.correctionEffectiveFrom) : undefined
    if (effectiveFrom && !Number.isFinite(effectiveFrom.getTime())) {
      this.notification.warning('Invalid effective date', 'Enter a valid date or leave the effective date empty.')
      return
    }
    this.saving = true
    const requestId = this.newRequestId()
    this.correctionSubscription = this.claims.correct(this.selected.claim.id, {
      workspaceId: this.workspaceId,
      requestId,
      correctedObject,
      reason,
      ...(effectiveFrom ? { effectiveFrom: effectiveFrom.toISOString() } : {}),
    }).subscribe({
      next: (claim) => {
        this.saving = false
        this.notification.success('Correction recorded', 'The original claim remains in history and the approved successor is now active for review.')
        this.closeInspector()
        this.load(claim.id)
      },
      error: (error) => {
        this.saving = false
        if (error?.status === 401 || error?.status === 403) {
          this.loadSubscription?.unsubscribe()
          this.loading = false
          this.revokeAuthority(error)
        }
        this.notification.error('Correction rejected', this.describeError(error, 'The correction was not stored.'))
      },
    })
  }

  statusLabel(status: string): string {
    return status.replace(/_/g, ' ')
  }

  statusTone(status: ClaimAssessmentStatus): string {
    switch (status) {
      case 'corroborated':
      case 'supported': return 'good'
      case 'conflicting': return 'danger'
      case 'needs_review': return 'review'
      default: return 'muted'
    }
  }

  trackByClaim(_: number, item: IClaimReviewItem): string {
    return item.claim.id
  }

  private newRequestId(): string {
    const random = globalThis.crypto?.randomUUID?.()
    return random || `correction-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`
  }

  private revokeAuthority(error: { status?: number }): void {
    this.session = undefined
    this.correctionConfirmed = false
    this.authorityError = error?.status === 401
      ? 'Your session expired. Sign in again, then refresh access before confirming this correction.'
      : error?.status === 403
        ? 'Your current account cannot approve corrections. Refresh access or sign in with an authorized account.'
        : 'Approval access could not be verified. Refresh access before confirming this correction.'
  }

  private describeError(error: any, fallback: string): string {
    return String(error?.error?.error || error?.error?.message || error?.message || fallback)
  }
}
