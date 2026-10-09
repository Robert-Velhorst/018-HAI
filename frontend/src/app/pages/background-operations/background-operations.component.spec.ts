import { HttpErrorResponse, HttpHeaders } from '@angular/common/http'
import { Observable, of, Subject, throwError, TimeoutError } from 'rxjs'
import { IBackgroundRunReport, IOperation, IOperationApprovalPreview, IOperationEvent } from '../../models/background-operations.model.interface'
import { BackgroundOperationsComponent } from './background-operations.component'

describe('BackgroundOperationsComponent', () => {
  function make(
    overrides: Record<string, unknown> = {},
    notification = jasmine.createSpyObj('notification', ['error', 'success', 'warning', 'info']),
  ): BackgroundOperationsComponent {
    const service = {
      overview: () => of({ dashboard: {}, operations: [] }),
      feeds: () => of({ feeds: [] }),
      events: () => of({ events: [] }),
      get: () => of({} as IOperation),
      approvalPreview: () => of({} as IOperationApprovalPreview),
      runBackground: () => of({
        feedsRead: 1,
        itemsIngested: 0,
        operationsCreated: 0,
        classified: 0,
        autoExecuted: 0,
        verified: 0,
        failed: 0,
        awaitingApproval: 0,
        blocked: 0,
        drafted: 0,
        observed: 0,
      }),
      approve: () => of({} as IOperation),
      run: () => of({ operation: {} as IOperation, verified: true, failed: false }),
      ...overrides,
    }
    const router = jasmine.createSpyObj('router', ['navigate'])
    const route = { snapshot: { queryParamMap: { get: () => null } } }
    return new BackgroundOperationsComponent(service as any, notification, router, route as any)
  }

  it('does not start a pass until an account feed is enabled', () => {
    const component = make()

    component.runBackground()

    expect(component.lastRunNotice).toContain('No enabled account feed')
    expect(component.lastRunError).toBe('')
    expect(component.running).toBeFalse()
  })

  it('reports whether a configured feed can be scanned', () => {
    const component = make()
    component.feeds = [{ name: 'Trello', provider: 'trello', accountLabel: 'Robert', sourceType: 'trello', enabled: true }]

    expect(component.hasEnabledFeeds()).toBeTrue()
  })

  it('keeps unknown risk distinct from verified low risk', () => {
    const component = make()

    expect(component.riskColor('low')).toBe('green')
    expect(component.riskColor('unknown')).toBe('default')
    expect(component.riskLabel('')).toBe('Risk unknown')
    expect(component.riskColor('critical')).toBe('red')
  })

  it('cancels an obsolete refresh before starting another one', () => {
    let cancellations = 0
    const pending = () => new Observable(() => () => cancellations++)
    const component = make({
      overview: pending,
      feeds: pending,
      events: () => of({ events: [] }),
    })

    component.refresh()
    component.refresh()

    expect(cancellations).toBe(2)
  })

  it('gives an owner-permission recovery path for authorization failures', () => {
    const component = make()

    const message = component.backgroundRunError(new HttpErrorResponse({ status: 403 }))

    expect(message).toContain('owner account')
  })

  it('treats an uncertain background-pass outcome as requiring reconciliation before retry', () => {
    const component = make()

    const message = component.backgroundRunError(new HttpErrorResponse({ status: 503 }))

    expect(message).toContain('could not confirm')
    expect(message).toContain('may have reached the worker')
  })

  it('shows server status and Retry-After without inventing a retry count', () => {
    const retryAfter = new HttpHeaders({ 'Retry-After': '25' })
    const runBackground = jasmine.createSpy().and.returnValue(
      throwError(() => new HttpErrorResponse({ status: 429, headers: retryAfter })),
    )
    const component = make({ runBackground })
    component.feeds = [{ name: 'Trello', provider: 'trello', accountLabel: 'Owner', sourceType: 'trello', enabled: true }]

    component.runBackground()

    expect(component.lastRunFailure?.statusLabel).toBe('HTTP 429')
    expect(component.lastRunFailure?.retryAfter).toContain('25 seconds')
    expect(component.lastRunFailure?.recovery).toContain('Follow any server retry guidance')
    expect(component.lastRunNeedsReconciliation).toBeTrue()
  })

  it('blocks another background pass until a successful status refresh follows an uncertain result', () => {
    const enabledFeed = { name: 'Trello', provider: 'trello', accountLabel: 'Owner', sourceType: 'trello', enabled: true }
    const runBackground = jasmine.createSpy().and.returnValue(
      throwError(() => new HttpErrorResponse({ status: 503 })),
    )
    const component = make({ runBackground, feeds: () => of({ feeds: [enabledFeed] }) })
    component.feeds = [enabledFeed]

    component.runBackground()
    component.runBackground()

    expect(runBackground).toHaveBeenCalledTimes(1)
    expect(component.canRunBackground()).toBeFalse()
    expect(component.backgroundRunUnavailableReason).toContain('Refresh status')

    component.refresh()

    expect(component.lastRunNeedsReconciliation).toBeFalse()
    expect(component.lastRunStatusRefreshed).toBeTrue()
    expect(component.canRunBackground()).toBeTrue()
  })

  it('keeps read-only refresh errors distinct from uncertain background-pass outcomes', () => {
    const component = make()

    const message = component.backgroundLoadError(new HttpErrorResponse({ status: 503 }))

    expect(message).toContain('did not start background work')
    expect(message).not.toContain('may have reached the worker')
  })

  it('shows an unavailable state instead of an empty queue when refresh fails', () => {
    const notification = jasmine.createSpyObj('notification', ['error', 'success', 'warning', 'info'])
    const component = make({
      overview: () => throwError(() => new HttpErrorResponse({ status: 503 })),
      feeds: () => of({ feeds: [] }),
      events: () => of({ events: [] }),
    }, notification)

    component.refresh()

    expect(component.loading).toBeFalse()
    expect(component.loadError).toContain('temporarily unavailable')
    expect(component.dashboard).toBeUndefined()
    expect(component.operations).toEqual([])
    expect(notification.error).not.toHaveBeenCalled()
  })

  it('preserves the last successful snapshot when a later refresh fails', () => {
    const item = operation('existing', 'blocked')
    const overview = jasmine.createSpy().and.returnValues(
      of({ dashboard: { blocked: 1 }, operations: [item] }),
      throwError(() => new HttpErrorResponse({ status: 503 })),
    )
    const component = make({ overview })

    component.refresh()
    component.refresh()

    expect(component.dashboard?.blocked).toBe(1)
    expect(component.operations.map((entry) => entry.id)).toEqual(['existing'])
    expect(component.loadError).toContain('temporarily unavailable')
  })

  it('blocks a background pass while feed status is refreshing or stale after refresh failure', () => {
    const enabledFeed = { name: 'Trello', provider: 'trello', accountLabel: 'Robert', sourceType: 'trello', enabled: true }
    const pendingFeeds = new Subject<{ feeds: typeof enabledFeed[] }>()
    const feeds = jasmine.createSpy().and.returnValues(
      of({ feeds: [enabledFeed] }),
      pendingFeeds,
    )
    const runBackground = jasmine.createSpy().and.returnValue(of({} as IBackgroundRunReport))
    const component = make({ feeds, runBackground })

    component.refresh()
    expect(component.canRunBackground()).toBeTrue()

    component.refresh()
    expect(component.loading).toBeTrue()
    expect(component.canRunBackground()).toBeFalse()
    component.runBackground()
    expect(runBackground).not.toHaveBeenCalled()

    pendingFeeds.error(new HttpErrorResponse({ status: 503 }))
    expect(component.loading).toBeFalse()
    expect(component.loadError).toContain('temporarily unavailable')
    expect(component.feeds).toEqual([enabledFeed])
    expect(component.canRunBackground()).toBeFalse()

    component.runBackground()
    expect(runBackground).not.toHaveBeenCalled()
    expect(component.lastRunNotice).toContain('Feed status is not current')
  })

  it('shows no more than three Basic operations, prioritizing decisions and blockers', () => {
    const component = make()
    component.operations = [
      operation('done', 'completed'),
      operation('needs-review', 'awaiting_approval'),
      operation('failed', 'failed'),
      operation('blocked', 'blocked'),
      operation('ready', 'ready'),
    ]

    expect(component.visibleOperations.map((item) => item.id)).toEqual(['needs-review', 'blocked', 'failed'])
  })

  it('does not surface completed work in the default attention list', () => {
    const component = make()
    component.operations = [operation('done', 'completed')]

    expect(component.visibleOperations).toEqual([])
    component.statusFilter = 'completed'
    expect(component.visibleOperations.map((item) => item.id)).toEqual(['done'])
  })

  it('explains blocked, failed, and approved states without implying approval executed work', () => {
    const component = make()
    const blocked = { ...operation('blocked', 'blocked'), lastError: 'Waiting for a source document.' }
    const failed = { ...operation('failed', 'failed'), lastError: 'Worker verification failed.' }
    const approved = operation('approved', 'approved')

    expect(component.operationStatusContext(blocked)).toBe('Waiting for a source document.')
    expect(component.operationStatusContext(failed)).toBe('Worker verification failed.')
    expect(component.operationStatusContext(approved)).toContain('does not execute it')
  })

  it('does not present a task description as the reason an operation is blocked', () => {
    const component = make()
    const blocked = { ...operation('blocked', 'blocked'), description: 'Prepare the client handoff.' }

    expect(component.operationStatusContext(blocked)).not.toContain('Prepare the client handoff')
    expect(component.operationStatusContext(blocked)).toContain('no blocker detail is recorded')
    expect(component.operationRecoveryContext(blocked)).toContain('Retry is unavailable')
  })

  it('explains retry availability only when the recorded safe-worker decision permits it', () => {
    const component = make()
    const permitted = { ...operation('retryable', 'failed'), currentDecision: 'run_safe_local_worker' }
    const denied = { ...operation('not-retryable', 'failed'), currentDecision: 'review' }

    expect(component.operationRecoveryContext(permitted)).toContain('retry is currently permitted')
    expect(component.operationRecoveryContext(denied)).toContain('Retry is unavailable')
    expect(component.canRun(permitted)).toBeTrue()
    expect(component.canRun(denied)).toBeFalse()
  })

  it('redacts credential-shaped values from server-provided diagnostic details', () => {
    const component = make()

    expect(component.safeDiagnosticText('Authorization: Bearer abc.def and api_key=private-value'))
      .toBe('Authorization=[redacted] and api_key=[redacted]')
  })

  it('keeps failed safe-worker outcomes distinct from unverified results', () => {
    const failedOperation = {
      ...operation('worker-failed', 'failed'),
      currentDecision: 'run_safe_local_worker',
      lastError: 'Local worker permission was denied.',
    }
    const component = make({ run: () => of({ operation: failedOperation, verified: false, failed: true }) })

    component.run({ ...operation('worker-failed', 'ready'), currentDecision: 'run_safe_local_worker' })

    expect(component.operationActionError(failedOperation)).toBe('Local worker permission was denied.')
    expect(component.canRun(failedOperation)).toBeTrue()
    expect(component.canRun({ ...failedOperation, currentDecision: 'review' })).toBeFalse()
  })

  it('records action conflicts and refreshes the current operation state', () => {
    const overview = jasmine.createSpy().and.returnValue(
      of({ dashboard: {}, operations: [operation('approval', 'approved')] }),
    )
    const component = make({
      overview,
      approve: () => throwError(() => new HttpErrorResponse({ status: 409, error: { error: 'Approval is no longer pending.' } })),
    })

    component.approve(operation('approval', 'awaiting_approval'))

    expect(component.operationActionError(operation('approval', 'awaiting_approval'))).toBe('Approval is no longer pending.')
    expect(component.operations[0].status).toBe('approved')
  })

  it('shows the first source preview and requires a second fresh exact-revision confirmation', () => {
    const item = sourceOperation('source-approved', 4)
    const digest = 'c'.repeat(64)
    const approvalPreview = jasmine.createSpy().and.returnValue(of({
      operation: item,
      revision: { operationId: item.id, version: 4, revisionDigest: digest },
    }))
    const approve = jasmine.createSpy().and.returnValue(of({ ...item, status: 'approved' }))
    const component = make({ approvalPreview, approve })

    component.approve(item)

    expect(approvalPreview).toHaveBeenCalledOnceWith(item.id)
    expect(component.sourceApprovalReviewVisible).toBeTrue()
    expect(component.sourceApprovalReview?.revision.revisionDigest).toBe(digest)
    expect(approve).not.toHaveBeenCalled()

    component.confirmSourceApprovalReview()

    expect(approvalPreview).toHaveBeenCalledTimes(2)
    expect(approve).toHaveBeenCalledOnceWith(item.id, { expectedVersion: 4, revisionDigest: digest })
    expect(component.operationActionError(item)).toBe('')
    expect(component.sourceApprovalReviewVisible).toBeFalse()
  })

  it('does not approve a source operation when the preview revision changed and refreshes status', () => {
    const item = sourceOperation('source-stale', 4)
    const refreshed = sourceOperation('source-stale', 5)
    const approve = jasmine.createSpy().and.returnValue(of(refreshed))
    const overview = jasmine.createSpy().and.returnValue(of({ dashboard: {}, operations: [refreshed] }))
    const component = make({
      approvalPreview: () => of({
        operation: refreshed,
        revision: { operationId: item.id, version: 5, revisionDigest: 'd'.repeat(64) },
      }),
      approve,
      overview,
    })

    component.approve(item)

    expect(approve).not.toHaveBeenCalled()
    expect(overview).toHaveBeenCalled()
    expect(component.operationActionError(refreshed)).toContain('changed since it was displayed')
    expect(component.operationActionRecovery(refreshed)).toContain('inspect the updated operation')
  })

  it('fails closed on a malformed source preview and does not fall back to legacy approval', () => {
    const item = sourceOperation('source-malformed', 2)
    const approve = jasmine.createSpy().and.returnValue(of(item))
    const component = make({
      approvalPreview: () => throwError(() => new Error('invalid approval preview')),
      approve,
    })

    component.approve(item)

    expect(approve).not.toHaveBeenCalled()
    expect(component.operationActionError(item)).toContain('could not load a valid source approval preview')
    expect(component.operationActionRecovery(item)).toContain('starting a new review')
  })

  it('fails closed on source preview request errors without submitting approval', () => {
    const item = sourceOperation('source-preview-error', 3)
    const approve = jasmine.createSpy().and.returnValue(of(item))
    const component = make({
      approvalPreview: () => throwError(() => new HttpErrorResponse({ status: 503 })),
      approve,
    })

    component.approve(item)

    expect(approve).not.toHaveBeenCalled()
    expect(component.operationActionError(item)).toContain('could not load a valid source approval preview')
  })

  it('does not submit when the fresh confirmation preview has a different digest', () => {
    const item = sourceOperation('source-digest-changed', 4)
    const displayed = {
      operation: item,
      revision: { operationId: item.id, version: 4, revisionDigest: 'a'.repeat(64) },
    }
    const changed = {
      operation: item,
      revision: { operationId: item.id, version: 4, revisionDigest: 'b'.repeat(64) },
    }
    const approvalPreview = jasmine.createSpy().and.returnValues(of(displayed), of(changed))
    const approve = jasmine.createSpy().and.returnValue(of(item))
    const component = make({ approvalPreview, approve })

    component.approve(item)
    component.confirmSourceApprovalReview()
    component.confirmSourceApprovalReview()

    expect(approvalPreview).toHaveBeenCalledTimes(2)
    expect(approve).not.toHaveBeenCalled()
    expect(component.sourceApprovalReviewError).toContain('changed after review')
  })

  it('does not submit when the operation version changed after the review was displayed', () => {
    const item = sourceOperation('source-version-changed', 4)
    const changed = sourceOperation('source-version-changed', 5)
    const approvalPreview = jasmine.createSpy().and.returnValues(
      of({ operation: item, revision: { operationId: item.id, version: 4, revisionDigest: 'a'.repeat(64) } }),
      of({ operation: changed, revision: { operationId: item.id, version: 5, revisionDigest: 'b'.repeat(64) } }),
    )
    const approve = jasmine.createSpy().and.returnValue(of(changed))
    const component = make({ approvalPreview, approve })

    component.approve(item)
    component.confirmSourceApprovalReview()

    expect(approve).not.toHaveBeenCalled()
    expect(component.sourceApprovalReviewError).toContain('changed after review')
  })

  it('does not submit when the fresh confirmation preview request fails', () => {
    const item = sourceOperation('source-confirm-preview-error', 4)
    const preview = {
      operation: item,
      revision: { operationId: item.id, version: 4, revisionDigest: 'c'.repeat(64) },
    }
    const approvalPreview = jasmine.createSpy().and.returnValues(
      of(preview),
      throwError(() => new HttpErrorResponse({ status: 503 })),
    )
    const approve = jasmine.createSpy().and.returnValue(of(item))
    const component = make({ approvalPreview, approve })

    component.approve(item)
    component.confirmSourceApprovalReview()

    expect(approve).not.toHaveBeenCalled()
    expect(component.sourceApprovalReviewError).toContain('No approval was submitted')
    expect(component.sourceApprovalReview).toEqual(preview)
  })

  it('refreshes the selected operation record as well as the queue', () => {
    const updated = operation('selected', 'completed')
    const get = jasmine.createSpy().and.returnValue(of(updated))
    const component = make({ get })
    component.detailVisible = true
    component.selected = operation('selected', 'ready')

    component.refresh()

    expect(get).toHaveBeenCalledOnceWith('selected')
    expect(component.selected?.status).toBe('completed')
  })

  it('prevents duplicate background passes while one request is in flight', () => {
    const response = new Subject<IBackgroundRunReport>()
    const runBackground = jasmine.createSpy().and.returnValue(response)
    const component = make({ runBackground })
    component.feeds = [{ name: 'Trello', provider: 'trello', accountLabel: 'Robert', sourceType: 'trello', enabled: true }]

    component.runBackground()
    component.runBackground()

    expect(runBackground).toHaveBeenCalledTimes(1)
    expect(component.running).toBeTrue()
    response.next({
      feedsRead: 1,
      itemsIngested: 0,
      operationsCreated: 0,
      classified: 0,
      autoExecuted: 0,
      verified: 0,
      failed: 0,
      awaitingApproval: 0,
      blocked: 0,
      drafted: 0,
      observed: 0,
      errors: [],
    })
    response.complete()
    expect(component.running).toBeFalse()
  })

  it('prevents duplicate approvals for the same operation until the request completes', () => {
    const response = new Subject<IOperation>()
    const approve = jasmine.createSpy().and.returnValue(response)
    const component = make({ approve })
    const item = operation('approval', 'awaiting_approval')

    component.approve(item)
    component.approve(item)

    expect(approve).toHaveBeenCalledOnceWith('approval')
    expect(component.isPending(item)).toBeTrue()
    response.next(item)
    response.complete()
    expect(component.isPending(item)).toBeFalse()
  })

  it('prevents duplicate worker runs for the same operation until the request completes', () => {
    const response = new Subject<{ operation: IOperation; verified: boolean; failed: boolean }>()
    const run = jasmine.createSpy().and.returnValue(response)
    const component = make({ run })
    const item = { ...operation('safe-run', 'ready'), currentDecision: 'run_safe_local_worker' }

    component.run(item)
    component.run(item)

    expect(run).toHaveBeenCalledOnceWith('safe-run')
    expect(component.isPending(item)).toBeTrue()
    response.next({ operation: item, verified: true, failed: false })
    response.complete()
    expect(component.isPending(item)).toBeFalse()
  })

  it('opens the full operation ledger in Advanced mode', () => {
    const router = jasmine.createSpyObj('router', ['navigate'])
    const route = { snapshot: { queryParamMap: { get: () => null } } }
    const component = new BackgroundOperationsComponent({} as any, jasmine.createSpyObj('notification', ['error', 'success', 'warning', 'info']), router, route as any)
    component.statusFilter = 'failed'

    component.openFullLedger()

    expect(router.navigate).toHaveBeenCalledOnceWith(['/background-operations'], {
      queryParams: { mode: 'advanced', status: 'failed' },
      fragment: 'operation-ledger',
    })
  })

  it('retains a structured 409 receipt and blocks another run despite stale ready reads', () => {
    const item = { ...operation('partial', 'ready'), currentDecision: 'run_safe_local_worker' }
    const run = jasmine.createSpy().and.returnValue(throwError(() => new HttpErrorResponse({
      status: 409, error: {
        operationId: item.id, operation: { id: item.id, status: 'running', verificationStatus: 'pending' },
        verified: false, failed: false, interrupted: false, reconciliationRequired: true, outcomeRecorded: false,
        receipt: { output: { artifactHash: 'retained-hash', boundedOutput: 'token=synthetic-secret retained' } },
        error: 'Safe execution requires reconciliation.',
      },
    })))
    const component = make({ run, overview: () => of({ dashboard: {}, operations: [item] }), get: () => of(item) })
    component.selected = item
    component.detailVisible = true

    component.run(item)
    component.refresh()
    component.setFilter('failed')
    component.closeDetail()
    component.openDetail(item)
    component.run(item)

    expect(run).toHaveBeenCalledTimes(1)
    expect(component.isPending(item)).toBeFalse()
    expect(component.canRun(item)).toBeFalse()
    expect(component.operationActionStatus(item)).toBe('Reconciliation required')
    expect(component.operationOutcomeSummary(item)).toContain('retained-hash')
    expect(component.operationOutcomeSummary(item)).toContain('token=[redacted]')
    expect(component.operationOutcomeSummary(item)).not.toContain('synthetic-secret')
    expect(component.operationOutcomeSummary(item)).toContain('not confirmed recorded')
    expect(component.operationVerificationLabel({ ...item, status: 'completed', verificationStatus: 'passed' })).toContain('reconciliation')
    expect(component.canRun({ ...item, id: 'unrelated' })).toBeTrue()
  })

  for (const error of [new TimeoutError(), new HttpErrorResponse({ status: 0 }), new HttpErrorResponse({ status: 503 })]) {
    it(`keeps an unknown run fenced after ${error instanceof TimeoutError ? 'timeout' : error.status} and successful stale refresh`, () => {
      const item = { ...operation('unknown', 'ready'), currentDecision: 'run_safe_local_worker' }
      const run = jasmine.createSpy().and.returnValue(throwError(() => error))
      const component = make({ run, overview: () => of({ dashboard: {}, operations: [item] }) })
      component.run(item)
      component.refresh()
      component.run(item)
      expect(run).toHaveBeenCalledTimes(1)
      expect(component.canRun(item)).toBeFalse()
    })
  }

  for (const status of [401, 403, 409]) {
    it(`does not invent uncertain effects for a receipt-free pre-effect refusal (${status})`, () => {
      const item = { ...operation('refused', 'ready'), currentDecision: 'run_safe_local_worker' }
      const run = jasmine.createSpy().and.returnValue(throwError(() => new HttpErrorResponse({
        status, error: { error: 'Execution is not authorized by the current policy.' },
      })))
      const component = make({ run })
      component.run(item)
      expect(component.canRun(item)).toBeTrue()
      expect(component.operationActionStatus(item)).toBe(`HTTP ${status}`)
      expect(component.operationActionError(item)).not.toContain('produced effects')
    })
  }

  for (const conflict of [
    { interrupted: true }, { reconciliationRequired: true }, { outcomeRecorded: false },
    { operation: { id: 'contradictory', status: 'verifying', verificationStatus: 'passed' } },
    { operation: null }, { operationId: 'wrong-operation' },
  ]) {
    it(`does not promote a verified flag with conflicting evidence (${JSON.stringify(conflict)})`, () => {
      const notification = jasmine.createSpyObj('notification', ['error', 'success', 'warning', 'info'])
      const item = { ...operation('contradictory', 'ready'), currentDecision: 'run_safe_local_worker' }
      const component = make({ run: () => of({
        operation: { ...item, status: 'completed', verificationStatus: 'passed' },
        verified: true, failed: false, ...conflict,
      }) }, notification)
      component.run(item)
      expect(notification.success).not.toHaveBeenCalled()
      expect(component.canRun(item)).toBeFalse()
    })
  }

  it('accepts only correlated completed/passed results and never uses worker receipt verification as completion', () => {
    const notification = jasmine.createSpyObj('notification', ['error', 'success', 'warning', 'info'])
    const item = { ...operation('verified', 'ready'), currentDecision: 'run_safe_local_worker' }
    const component = make({ run: () => of({
      operation: { ...item, status: 'completed', verificationStatus: 'passed' },
      verified: true, failed: false, interrupted: false, reconciliationRequired: false, outcomeRecorded: true,
    }) }, notification)
    component.run(item)
    expect(notification.success).toHaveBeenCalledOnceWith('Verified', 'verified executed and verified.')
  })

  it('retains reconciliation warnings after the operation status changes', () => {
    const item = { ...operation('changed', 'ready'), currentDecision: 'run_safe_local_worker' }
    const component = make({ run: () => of({
      operation: { ...item, status: 'interrupted' }, verified: false, failed: false, interrupted: true,
      reconciliationRequired: true, outcomeRecorded: true,
    }) })
    component.run(item)
    expect(component.operationActionIssue({ ...item, status: 'interrupted' })?.tone).toBe('warning')
    expect(component.operationOutcomeSummary(item)).toContain('durability is not established')
  })

  it('prioritizes interrupted and retained uncertain work in Basic and labels it for review', () => {
    const item = { ...operation('uncertain', 'ready'), currentDecision: 'run_safe_local_worker' }
    const component = make({ run: () => throwError(() => new HttpErrorResponse({ status: 409, error: {
      verified: false, failed: false, reconciliationRequired: true,
    } })) })
    component.run(item)
    component.operations = [operation('blocked', 'blocked'), operation('failed', 'failed'), operation('approval', 'awaiting_approval')]
    expect(component.visibleOperations[0].id).toBe(item.id)
    expect(component.operationDisplayStatus(item)).toBe('interrupted')
    expect(component.statusLabel('interrupted')).toBe('Reconciliation required')
    expect(component.statusColor('interrupted')).toBe('gold')
    expect(component.statusFilters.some((filter) => filter.value === 'interrupted')).toBeTrue()
    expect(component.canRun({ ...operation('known-failed', 'failed'), currentDecision: 'run_safe_local_worker' })).toBeTrue()
    component.operations = [operation('recorded-interruption', 'interrupted'), operation('blocked', 'blocked')]
    expect(component.visibleOperations.map((entry) => entry.id)).toContain('recorded-interruption')
  })

  it('does not hide additional interrupted work behind the normal three-item Basic limit', () => {
    const component = make()
    component.operations = [1, 2, 3, 4].map((id) => operation(`interrupted-${id}`, 'interrupted'))
    expect(component.visibleOperations.length).toBe(4)
    expect(component.visibleOperations.every((entry) => entry.status === 'interrupted')).toBeTrue()
  })

  it('routes source management to the existing connected-sources page', () => {
    const router = jasmine.createSpyObj('router', ['navigate'])
    const route = { snapshot: { queryParamMap: { get: () => null } } }
    const component = new BackgroundOperationsComponent(
      {} as any,
      jasmine.createSpyObj('notification', ['error', 'success', 'warning', 'info']),
      router,
      route as any,
    )

    component.openSources()

    expect(router.navigate).toHaveBeenCalledOnceWith(['/connected-sources'])
  })

  it('restores supported filters from a direct full-ledger URL', () => {
    const overview = jasmine.createSpy().and.returnValue(of({ dashboard: {}, operations: [] }))
    const service = { overview, feeds: () => of({ feeds: [] }) }
    const route = { snapshot: { queryParamMap: { get: () => 'blocked' } } }
    const component = new BackgroundOperationsComponent(
      service as any,
      jasmine.createSpyObj('notification', ['error', 'success', 'warning', 'info']),
      jasmine.createSpyObj('router', ['navigate']),
      route as any,
    )

    component.ngOnInit()

    expect(component.statusFilter).toBe('blocked')
    expect(overview).toHaveBeenCalledOnceWith({ status: 'blocked' })
  })

  it('ignores stale audit responses when the selected operation changes', () => {
    const first = new Subject<{ events: IOperationEvent[] }>()
    const second = new Subject<{ events: IOperationEvent[] }>()
    const events = jasmine.createSpy().and.returnValues(first, second)
    const component = make({ events })

    component.openDetail(operation('first', 'blocked'))
    component.openDetail(operation('second', 'failed'))
    first.next({ events: [{ ...event('first-event'), operationId: 'first' }] })
    second.next({ events: [{ ...event('second-event'), operationId: 'second' }] })
    second.complete()

    expect(component.selected?.id).toBe('second')
    expect(component.selectedEvents.map((item) => item.id)).toEqual(['second-event'])
    expect(component.eventsLoading).toBeFalse()
  })

  function operation(id: string, status: string): IOperation {
    return {
      id,
      version: 1,
      ownerUserId: 'owner',
      workspaceId: 'workspace',
      title: id,
      sourceType: 'manual',
      operationType: 'review',
      status,
      riskLevel: 'low',
      autonomyLevel: 'draft_only',
      ownerType: 'robert',
      currentDecision: 'review',
      requiresApproval: status === 'awaiting_approval',
      verificationStatus: 'pending',
      createdAt: '2026-09-24T00:00:00Z',
      updatedAt: '2026-09-24T00:00:00Z',
    }
  }

  function sourceOperation(id: string, version: number): IOperation {
    return { ...operation(id, 'awaiting_approval'), version, sourceIdentityHash: 'source-identity' }
  }

  function event(id: string): IOperationEvent {
    return {
      id,
      operationId: 'first',
      eventType: 'created',
      actorType: 'system',
      createdAt: '2026-09-24T00:00:00Z',
    }
  }
})
