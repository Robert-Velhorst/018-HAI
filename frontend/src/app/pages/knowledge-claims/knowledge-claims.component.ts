import { ChangeDetectionStrategy, Component, OnInit } from '@angular/core'
import { ActivatedRoute, Router } from '@angular/router'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import { catchError, forkJoin, of } from 'rxjs'
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
  standalone: false,
  selector: 'app-knowledge-claims',
  templateUrl: './knowledge-claims.component.html',
  styleUrls: ['./knowledge-claims.component.scss'],
})
export class KnowledgeClaimsComponent implements OnInit {
  workspaceId = '018-HAI'
  queue?: IClaimReviewQueue
  session?: IAuthSession
  selected?: IClaimReviewItem
  lifecycle?: IClaimLifecycle
  loading = true
  detailLoading = false
  saving = false
  errorMessage = ''
  filter: ClaimFilter = 'attention'
  inspectorOpen = false
  correctionOpen = false
  correctedObject = ''
  correctionReason = ''
  correctionEffectiveFrom = ''
  correctionConfirmed = false

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

  get visibleItems(): IClaimReviewItem[] {
    const items = this.queue?.items || []
    if (this.filter === 'all') return items
    if (this.filter === 'attention') return this.attentionItems
    return items.filter((item) => item.assessment.status === this.filter)
  }

  get canApprove(): boolean {
    return this.session?.permissions.canApprove === true
  }

  count(status: ClaimAssessmentStatus): number {
    return this.queue?.counts?.[status] || 0
  }

  load(selectClaimId = ''): void {
    const workspace = this.workspaceId.trim()
    if (!workspace) {
      this.errorMessage = 'Enter a workspace before loading claim records.'
      return
    }
    this.loading = true
    this.errorMessage = ''
    forkJoin({
      queue: this.claims.reviewQueue(workspace),
      session: this.authSession.session().pipe(catchError(() => of(undefined))),
    }).subscribe({
      next: ({ queue, session }) => {
        this.queue = queue
        this.session = session
        this.loading = false
        const selected = queue.items.find((item) => item.claim.id === selectClaimId)
        if (selected) this.openInspector(selected)
      },
      error: (error) => {
        this.loading = false
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
    this.correctionOpen = false
    this.router.navigate([], {
      relativeTo: this.route,
      queryParams: { workspaceId: this.workspaceId, claimId: item.claim.id },
      queryParamsHandling: 'merge',
      replaceUrl: true,
    })
    this.claims.lifecycle(this.workspaceId, item.claim.id).subscribe({
      next: (lifecycle) => {
        this.lifecycle = lifecycle
        this.detailLoading = false
      },
      error: (error) => {
        this.detailLoading = false
        this.notification.error('Claim detail unavailable', this.describeError(error, 'The lifecycle could not be loaded.'))
      },
    })
  }

  closeInspector(): void {
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
    this.saving = true
    const requestId = this.newRequestId()
    this.claims.correct(this.selected.claim.id, {
      workspaceId: this.workspaceId,
      requestId,
      correctedObject,
      reason,
      ...(this.correctionEffectiveFrom
        ? { effectiveFrom: new Date(this.correctionEffectiveFrom).toISOString() }
        : {}),
    }).subscribe({
      next: (claim) => {
        this.saving = false
        this.notification.success('Correction recorded', 'The original claim remains in history and the approved successor is now active for review.')
        this.closeInspector()
        this.load(claim.id)
      },
      error: (error) => {
        this.saving = false
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

  private describeError(error: any, fallback: string): string {
    return String(error?.error?.error || error?.error?.message || error?.message || fallback)
  }
}
