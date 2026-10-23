import { HttpErrorResponse } from '@angular/common/http'
import { ChangeDetectionStrategy, Component, OnDestroy, OnInit } from '@angular/core'
import { ActivatedRoute } from '@angular/router'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import { defer, finalize, Subject, Subscription, take, takeUntil, throwIfEmpty, timeout } from 'rxjs'
import {
  IPlanBinding,
  IPlanConstraint,
  IPlanGraph,
  IPlanNode,
  IPlanPreviewRequest,
  IPlanResourceEstimate,
  IPlanTransportEdge,
  IPlanTransportNode,
} from '../../models/plan-graph.model.interface'
import { PlanGraphService } from '../../services/plan-graph.service'

interface PreviewForm {
  title: string
  objective: string
  successCriteria: string
  deadlineAt: string
  pursuitId: string
  workflowId: string
  constraints: string
  estimatedMinutes?: number
  estimatedCostEur?: number
}

@Component({
    changeDetection: ChangeDetectionStrategy.Eager,
    selector: 'app-plan-coordination',
    templateUrl: './plan-coordination.component.html',
    styleUrls: ['./plan-coordination.component.scss'],
    standalone: false
})
export class PlanCoordinationComponent implements OnInit, OnDestroy {
  readonly moduleId = 'plan-coordination'

  loading = true
  saving = false
  detailLoading = false
  errorMessage = ''
  plans: IPlanGraph[] = []
  selectedPlan?: IPlanGraph
  inspectorOpen = false
  replanOpen = false
  replanReason = ''
  replanConstraints = ''
  replanDeadlineAt = ''
  private requestedPlanId = ''
  private readonly destroyed$ = new Subject<void>()
  private destroyed = false
  private listSubscription?: Subscription
  private detailSubscription?: Subscription
  private readonly readTimeoutMs = 6000
  private readonly writeTimeoutMs = 30000

  previewForm: PreviewForm = this.emptyPreviewForm()

  constructor(
    private readonly planService: PlanGraphService,
    private readonly notification: NzNotificationService,
    private readonly route: ActivatedRoute,
  ) {}

  ngOnInit(): void {
	this.requestedPlanId = (this.route.snapshot.queryParamMap.get('planId') || '').trim()
    this.refresh()
  }

  ngOnDestroy(): void {
    this.destroyed = true
    this.cancelReads()
    this.destroyed$.next()
    this.destroyed$.complete()
  }

  private cancelReads(): void {
    this.listSubscription?.unsubscribe()
    this.detailSubscription?.unsubscribe()
  }

  refresh(preserveSelection = true): void {
    if (this.destroyed || this.saving) return
    const selectedId = preserveSelection ? (this.selectedPlan?.id || this.requestedPlanId) : this.requestedPlanId
    this.cancelReads()
    this.loading = true
    this.errorMessage = ''
    this.listSubscription = defer(() => this.planService.list()).pipe(
      timeout(this.readTimeoutMs), take(1), throwIfEmpty(), takeUntil(this.destroyed$),
      finalize(() => this.loading = false),
    ).subscribe({
      next: (plans) => {
        this.plans = this.sortPlans(plans)
        this.selectedPlan = this.plans.find((plan) => plan.id === selectedId) || this.plans[0]
        if (this.selectedPlan?.id === this.requestedPlanId) this.inspectorOpen = true
        this.loading = false
      },
      error: (error: HttpErrorResponse) => {
        this.loading = false
        this.errorMessage = this.apiError(error, 'Plan records are unavailable. HAI cannot infer that there is no work to coordinate.')
      },
    })
  }

  createPreview(): void {
    if (this.destroyed || this.saving) return
    const title = this.previewForm.title.trim()
    const objective = this.previewForm.objective.trim()
    const successCriteria = this.lines(this.previewForm.successCriteria)
    if (!title || !objective || !successCriteria.length) {
      this.notification.error('Plan needs more detail', 'Add a title, objective, and at least one success criterion.')
      return
    }

    const request: IPlanPreviewRequest = {
      idempotencyKey: this.idempotencyKey('preview'),
      title,
      nodes: this.previewNodes(objective, successCriteria),
      edges: this.previewEdges(successCriteria.length),
    }

    this.cancelReads()
    this.saving = true
    defer(() => this.planService.preview(request)).pipe(
      timeout(this.writeTimeoutMs), take(1), throwIfEmpty(), takeUntil(this.destroyed$),
      finalize(() => this.saving = false),
    ).subscribe({
      next: (plan) => {
        this.saving = false
        this.upsert(plan)
        this.selectedPlan = plan
        this.previewForm = this.emptyPreviewForm()
        this.inspectorOpen = true
        this.notification.success('Plan preview created', 'Review its dependencies, evidence, and approval state before accepting it.')
      },
      error: (error: HttpErrorResponse) => {
        this.saving = false
        this.notification.error('Preview failed', this.apiError(error, 'Preview creation could not be confirmed. Your draft is retained; refresh plans before retrying.'))
      },
    })
  }

  selectPlan(plan: IPlanGraph, openInspector = false): void {
    if (this.destroyed || this.saving) return
    this.cancelReads()
    this.selectedPlan = plan
    this.replanOpen = false
    if (openInspector) this.inspectorOpen = true
    this.detailLoading = true
    this.detailSubscription = defer(() => this.planService.get(plan.id)).pipe(
      timeout(this.readTimeoutMs), take(1), throwIfEmpty(), takeUntil(this.destroyed$),
      finalize(() => this.detailLoading = false),
    ).subscribe({
      next: (detail) => {
        this.detailLoading = false
        this.selectedPlan = detail
        this.upsert(detail)
      },
      error: (error: HttpErrorResponse) => {
        this.detailLoading = false
        this.notification.error('Plan detail unavailable', this.apiError(error, 'The selected summary remains visible, but its full record could not be loaded.'))
      },
    })
  }

  openInspector(): void {
    if (!this.selectedPlan) return
    this.inspectorOpen = true
    this.selectPlan(this.selectedPlan)
  }

  closeInspector(): void {
    this.inspectorOpen = false
  }

  acceptSelected(): void {
    const plan = this.selectedPlan
    if (this.destroyed || !plan || !this.canAccept(plan) || this.saving) return
    this.cancelReads()
    this.saving = true
    defer(() => this.planService.accept(plan.id, {
      expectedRevision: plan.revision,
      expectedDigest: plan.planDigest,
    })).pipe(
      timeout(this.writeTimeoutMs), take(1), throwIfEmpty(), takeUntil(this.destroyed$),
      finalize(() => this.saving = false),
    ).subscribe({
      next: (accepted) => {
        this.saving = false
        this.selectedPlan = accepted
        this.upsert(accepted)
        this.notification.success('Plan accepted', 'The accepted revision is now the authoritative coordination plan.')
      },
      error: (error: HttpErrorResponse) => {
        this.saving = false
        this.notification.error('Plan was not accepted', this.apiError(error, 'Refresh the plan before making another decision.'))
      },
    })
  }

  beginReplan(): void {
    if (this.destroyed || this.saving) return
    this.replanOpen = true
    this.replanReason = ''
    this.replanConstraints = ''
    this.replanDeadlineAt = this.toLocalDateTime(this.selectedPlan?.deadlineAt)
  }

  cancelReplan(): void {
    if (this.destroyed || this.saving) return
    this.replanOpen = false
    this.replanReason = ''
    this.replanConstraints = ''
  }

  replanSelected(): void {
    if (this.destroyed || this.saving) return
    const plan = this.selectedPlan
    const reason = this.replanReason.trim()
    if (!plan || !reason || this.saving) {
      if (!reason) this.notification.error('Reason required', 'Explain what changed so the repair remains auditable.')
      return
    }
    this.cancelReads()
    this.saving = true
    defer(() => this.planService.replan(plan.id, {
      expectedRevision: plan.revision,
      expectedDigest: plan.planDigest,
      idempotencyKey: this.idempotencyKey('replan'),
      title: plan.title,
      nodes: this.replannedNodes(plan),
      edges: plan.edges.map((edge) => ({
        id: edge.id,
        from: edge.fromNodeId,
        to: edge.toNodeId,
        type: edge.type === 'other' ? 'finish_to_start' : edge.type,
        lagMinutes: edge.lagMinutes,
      })),
      reason,
      trigger: 'owner_requested',
    })).pipe(
      timeout(this.writeTimeoutMs), take(1), throwIfEmpty(), takeUntil(this.destroyed$),
      finalize(() => this.saving = false),
    ).subscribe({
      next: (replanned) => {
        this.saving = false
        this.replanOpen = false
        this.selectedPlan = replanned
        this.upsert(replanned)
        this.notification.success('New plan revision prepared', 'Review the repaired dependencies and evidence before accepting the revision.')
      },
      error: (error: HttpErrorResponse) => {
        this.saving = false
        this.notification.error('Replan failed', this.apiError(error, 'Replanning could not be confirmed. Refresh before retrying; a new revision may already exist.'))
      },
    })
  }

  get nextCoordinatedNode(): IPlanNode | undefined {
    const nodes = [...(this.selectedPlan?.nodes || [])]
      .sort((left, right) => left.sequence - right.sequence)
    return nodes.find((node) => node.status === 'ready')
      || nodes.find((node) => node.status === 'planned')
  }

  get blockedNodes(): IPlanNode[] {
    return (this.selectedPlan?.nodes || []).filter((node) => node.status === 'blocked' || !!node.blockedReason)
  }

  get orderedNodes(): IPlanNode[] {
    return [...(this.selectedPlan?.nodes || [])].sort((left, right) => left.sequence - right.sequence)
  }

  get planRequiresDecision(): boolean {
    const plan = this.selectedPlan
    return !!plan && this.canAccept(plan)
  }

  canAccept(plan: IPlanGraph): boolean {
    return plan.status === 'draft'
  }

  canReplan(plan: IPlanGraph): boolean {
    return plan.status === 'accepted'
  }

  isCritical(node: IPlanNode): boolean {
    return (this.selectedPlan?.criticalPathNodeIds || []).includes(node.id)
  }

  dependencyTitles(node: IPlanNode): string {
    if (!node.dependencyIds.length) return 'No prerequisites'
    const nodes = this.selectedPlan?.nodes || []
    return node.dependencyIds.map((id) => nodes.find((candidate) => candidate.id === id)?.title || id).join(', ')
  }

  statusLabel(value: string | undefined): string {
    return (value || 'unknown').replace(/_/g, ' ')
  }

  ownerLabel(node: IPlanNode | undefined): string {
    if (!node) return 'Unassigned'
    return node.ownerLabel || this.statusLabel(node.ownerType)
  }

  shortDigest(value: string | undefined): string {
    if (!value) return 'Not recorded'
    return value.length > 18 ? `${value.slice(0, 10)}...${value.slice(-6)}` : value
  }

  bindingLabel(binding: IPlanBinding): string {
    return binding.label || `${this.statusLabel(binding.type)} ${binding.id}`
  }

  constraintState(constraint: IPlanConstraint): string {
    if (constraint.satisfied === true) return 'satisfied'
    if (constraint.satisfied === false) return 'unmet'
    return constraint.hard ? 'required' : 'advisory'
  }

  resourceAvailability(resource: IPlanResourceEstimate): string {
    if (resource.availableAmount === undefined) return `${resource.amount} ${resource.unit}`
    return `${resource.amount} of ${resource.availableAmount} ${resource.unit}`
  }

  trackById(_: number, item: { id: string }): string {
    return item.id
  }

  private upsert(plan: IPlanGraph): void {
    const remaining = this.plans.filter((candidate) => candidate.id !== plan.id)
    this.plans = this.sortPlans([plan, ...remaining])
  }

  private sortPlans(plans: IPlanGraph[]): IPlanGraph[] {
    return [...plans].sort((left, right) => {
      const leftDecision = this.canAccept(left) ? 1 : 0
      const rightDecision = this.canAccept(right) ? 1 : 0
      if (leftDecision !== rightDecision) return rightDecision - leftDecision
      return Date.parse(right.updatedAt || right.createdAt) - Date.parse(left.updatedAt || left.createdAt)
    })
  }

  private emptyPreviewForm(): PreviewForm {
    return {
      title: '',
      objective: '',
      successCriteria: '',
      deadlineAt: '',
      pursuitId: '',
      workflowId: '',
      constraints: '',
      estimatedMinutes: undefined,
      estimatedCostEur: undefined,
    }
  }

  private lines(value: string): string[] {
    return [...new Set((value || '').split(/\r?\n/).map((line) => line.trim()).filter(Boolean))]
  }

  private optional(value: string | undefined): string | undefined {
    const clean = (value || '').trim()
    return clean || undefined
  }

  private toIso(value: string | undefined): string | undefined {
    if (!value) return undefined
    const parsed = new Date(value)
    return Number.isNaN(parsed.getTime()) ? undefined : parsed.toISOString()
  }

  private toLocalDateTime(value: string | undefined): string {
    if (!value) return ''
    const parsed = new Date(value)
    if (Number.isNaN(parsed.getTime())) return ''
    const offset = parsed.getTimezoneOffset() * 60_000
    return new Date(parsed.getTime() - offset).toISOString().slice(0, 16)
  }

  private previewNodes(objective: string, successCriteria: string[]): IPlanTransportNode[] {
    const deadline = this.toIso(this.previewForm.deadlineAt)
    const bindings = {
      pursuitId: this.optional(this.previewForm.pursuitId),
      workflowId: this.optional(this.previewForm.workflowId),
    }
    const objectiveNode: IPlanTransportNode = {
      id: '001-objective',
      type: 'objective',
      title: objective,
      owner: 'hai',
      status: 'ready',
      estimatedMinutes: this.nonNegativeInteger(this.previewForm.estimatedMinutes),
      estimatedCostEur: this.nonNegativeNumber(this.previewForm.estimatedCostEur),
      deadline,
      risk: 'low',
      approvalState: 'not_required',
      bindings,
    }
    const criterionNodes = successCriteria.map((criterion, index): IPlanTransportNode => ({
      id: `${String(index + 2).padStart(3, '0')}-criterion`,
      type: 'success_criterion',
      title: criterion,
      owner: 'hai',
      status: 'planned',
      estimatedMinutes: 0,
      estimatedCostEur: 0,
      deadline,
      risk: 'low',
      approvalState: 'not_required',
      bindings,
    }))
    const constraintNodes = this.lines(this.previewForm.constraints).map((constraint, index): IPlanTransportNode => ({
      id: `${String(successCriteria.length + index + 2).padStart(3, '0')}-constraint`,
      type: 'constraint',
      title: constraint,
      owner: 'robert',
      status: 'planned',
      estimatedMinutes: 0,
      estimatedCostEur: 0,
      deadline,
      risk: 'low',
      approvalState: 'not_required',
      bindings,
    }))
    return [objectiveNode, ...criterionNodes, ...constraintNodes]
  }

  private previewEdges(criteriaCount: number): IPlanTransportEdge[] {
    return Array.from({ length: criteriaCount }, (_, index) => ({
      id: `edge-${String(index + 1).padStart(3, '0')}`,
      from: index === 0 ? '001-objective' : `${String(index + 1).padStart(3, '0')}-criterion`,
      to: `${String(index + 2).padStart(3, '0')}-criterion`,
      type: 'finish_to_start',
    }))
  }

  private replannedNodes(plan: IPlanGraph): IPlanTransportNode[] {
    const deadline = this.toIso(this.replanDeadlineAt)
    const nodes = plan.nodes.map((node) => ({
      ...node.transport,
      deadline: deadline || node.transport.deadline,
      status: node.transport.status === 'completed' ? 'completed' as const : 'planned' as const,
    }))
    const added = this.lines(this.replanConstraints).map((constraint, index): IPlanTransportNode => ({
      id: `repair-${plan.revision + 1}-${String(index + 1).padStart(3, '0')}`,
      type: 'constraint',
      title: constraint,
      owner: 'robert',
      status: 'planned',
      estimatedMinutes: 0,
      estimatedCostEur: 0,
      deadline,
      risk: 'low',
      approvalState: 'not_required',
      bindings: {},
    }))
    return [...nodes, ...added]
  }

  private nonNegativeInteger(value: number | undefined): number {
    return value !== undefined && Number.isFinite(value) ? Math.max(0, Math.trunc(value)) : 0
  }

  private nonNegativeNumber(value: number | undefined): number {
    return value !== undefined && Number.isFinite(value) ? Math.max(0, value) : 0
  }

  private idempotencyKey(operation: string): string {
    const id = typeof globalThis.crypto?.randomUUID === 'function'
      ? globalThis.crypto.randomUUID()
      : `${Date.now()}-${Math.random().toString(36).slice(2)}`
    return `plan-${operation}-${id}`
  }

  private apiError(error: HttpErrorResponse, fallback: string): string {
    const detail = error?.error?.error || error?.error?.message || error?.message
    return typeof detail === 'string' && detail.trim() ? detail.trim() : fallback
  }
}
