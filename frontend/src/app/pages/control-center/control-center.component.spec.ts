import { CommonModule } from '@angular/common'
import { NO_ERRORS_SCHEMA } from '@angular/core'
import { fakeAsync, TestBed, tick } from '@angular/core/testing'
import { FormsModule } from '@angular/forms'
import { Router } from '@angular/router'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import { delay, Observable, of, Subject, throwError } from 'rxjs'
import { AgentCycleService } from '../../services/agent-cycle.service'
import { AgentRuntimeService } from '../../services/agent-runtime.service'
import { AmbientService } from '../../services/ambient.service'
import { AUTOMATIONS_SERVICE_TOKEN } from '../../services/automations/automations.service.token'
import { ContextMemoryService } from '../../services/context-memory/context-memory.service'
import { PursuitService } from '../../services/pursuit.service'
import { WorkflowService } from '../../services/workflow/workflow.service'
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service'
import { ControlCenterComponent } from './control-center.component'

function emptyPursuitDashboard(overrides: Record<string, unknown> = {}): any {
  return {
    counts: { active: 0 },
    decisionQueue: [], needsRobert: [], vaReady: [], systemReady: [], blocked: [], stale: [],
    reviewDue: [], planningNeeded: [], recentlyChanged: [], highRisk: [], completionCandidates: [],
    ...overrides,
  }
}

function emptyAmbientOverview(): any {
  return {
    generatedAt: '2026-01-01T00:00:00Z',
    policy: {},
    needs: [], opportunities: [], scans: [], warnings: [],
  }
}

describe('ControlCenterComponent', () => {
  for (const failure of ['none', 'unavailable', 'unauthorized'] as const) {
    it(`renders asynchronous dashboard ${failure} state without a user click or manual detection`, async () => {
      const queue = { counts: { approvals: 0, blocked: 0, dueOpenLoops: 0, ready: 0 },
        approvalItems: [], blockedItems: [], readyItems: [], dueOpenLoops: [] }
      const router = { navigate: jasmine.createSpy('navigate') }
      await TestBed.configureTestingModule({
        declarations: [ControlCenterComponent],
        imports: [CommonModule, FormsModule],
        schemas: [NO_ERRORS_SCHEMA],
        providers: [
          { provide: AUTOMATIONS_SERVICE_TOKEN, useValue: {} },
          { provide: WorkflowService, useValue: { dashboard: () => of(queue).pipe(delay(1)) } },
          { provide: PursuitService, useValue: { dashboard: () => of(emptyPursuitDashboard()).pipe(delay(1)) } },
          { provide: AmbientService, useValue: { overview: () => of(emptyAmbientOverview()).pipe(delay(1)) } },
          { provide: AgentCycleService, useValue: {} },
          { provide: AgentRuntimeService, useValue: {} },
          { provide: ContextMemoryService, useValue: {} },
          { provide: NzNotificationService, useValue: {} },
          { provide: Router, useValue: router },
          { provide: ModuleViewPreferencesService, useValue: {
            get: () => ({ version: 1, mode: 'basic', openSections: {}, navigationMode: 'auto' }),
          } },
        ],
      }).compileComponents()
      const response = new Subject<any>()
      ;(TestBed.inject(WorkflowService) as any).dashboard = () => response.asObservable()
      const fixture = TestBed.createComponent(ControlCenterComponent)
      fixture.autoDetectChanges()
      expect(fixture.nativeElement.textContent).toContain('Checking the workflow queue')
      await new Promise<void>((resolve) => setTimeout(() => {
        if (failure === 'unavailable') response.error(new Error('fixture read failed'))
        else if (failure === 'unauthorized') response.error({ status: 401 })
        else { response.next(queue); response.complete() }
        resolve()
      }, 5))
      await fixture.whenStable()
      const refresh = fixture.nativeElement.querySelector('[data-testid="page-refresh"]') as HTMLButtonElement
      expect(refresh.disabled).toBeFalse()
      expect(fixture.nativeElement.textContent).not.toContain('Checking the workflow queue')
      expect(fixture.nativeElement.querySelector('[data-testid="dashboard-unavailable"]') !== null).toBe(failure !== 'none')
      const nextAction = fixture.nativeElement.querySelector('[data-testid="next-action-primary"]') as HTMLButtonElement
      expect(nextAction).not.toBeNull()
      expect(nextAction.disabled).toBeFalse()
      if (failure === 'unavailable') expect(nextAction.textContent).toContain('Retry queue check')
      if (failure === 'unauthorized') {
        expect(fixture.nativeElement.querySelector('[data-testid="dashboard-unavailable"]')?.textContent).toContain('Sign-in required')
        expect(fixture.nativeElement.textContent).toContain('Sign-in required to verify current work')
        expect(fixture.nativeElement.textContent).not.toContain('Nothing is waiting on you')
        expect(nextAction.textContent).toContain('Sign in again')
        nextAction.click()
        expect(router.navigate).toHaveBeenCalledWith(['/login'], {
          queryParams: { returnUrl: '/control-center' },
        })
      }
    })
  }

  function createComponent(
    agentCycle: { run: jasmine.Spy },
    automations: Record<string, jasmine.Spy> = {},
    initialViewMode: 'basic' | 'advanced' = 'basic'
  ) {
    const notifications = {
      error: jasmine.createSpy('error'),
      success: jasmine.createSpy('success'),
      warning: jasmine.createSpy('warning'),
    }

    let viewMode = initialViewMode
    const viewPreferences = {
      get: jasmine.createSpy('get').and.callFake(() => ({
        version: 1,
        mode: viewMode,
        openSections: {},
        navigationMode: 'auto',
      })),
    }
    const router = { navigate: jasmine.createSpy('navigate') }
    const cdr = { markForCheck: jasmine.createSpy('markForCheck') }
    const component = new ControlCenterComponent(
      automations as any,
      {} as any,
      {} as any,
      agentCycle as any,
      {} as any,
      {} as any,
      {} as any,
      notifications as any,
      router as any,
      viewPreferences as any,
      cdr as any
    )

    return {
      component,
      notifications,
      cdr,
      router,
      viewPreferences,
      setViewMode: (mode: 'basic' | 'advanced') => { viewMode = mode },
    }
  }

  it('cancels memory and diagnostics reads when the dashboard is destroyed', () => {
    let teardownCount = 0
    const pendingRead = () => new Observable<any>(() => () => { teardownCount += 1 })
    const automationService = {
      getAutomations: jasmine.createSpy('getAutomations').and.returnValue(pendingRead()),
      getHealthSummary: jasmine.createSpy('getHealthSummary').and.returnValue(pendingRead()),
    }
    const { component } = createComponent(
      { run: jasmine.createSpy('run') },
      automationService
    )
    ;(component as any).memoryService.list = jasmine.createSpy('list').and.returnValue(pendingRead())
    ;(component as any).agentRuntimeService.overview = jasmine.createSpy('overview').and.returnValue(pendingRead())

    component.loadMemories()
    component.loadDiagnosticsData()
    expect(teardownCount).toBe(0)

    component.ngOnDestroy()

    expect(teardownCount).toBe(4)
  })

  it('identifies only expired or needs-review host runtime review events', () => {
    const { component } = createComponent({ run: jasmine.createSpy('run') })

    expect(component.isHostRuntimeReviewEvent({
      launchType: 'agent_runtime_host_review', status: 'expired',
    } as any)).toBeTrue()
    expect(component.isHostRuntimeReviewEvent({
      launchType: 'agent_runtime_host_review', status: 'needs_review',
    } as any)).toBeTrue()
    expect(component.isHostRuntimeReviewEvent({
      launchType: 'agent_runtime_host_completion', status: 'failed',
    } as any)).toBeFalse()
    expect(component.isHostRuntimeReviewEvent({
      launchType: 'agent_runtime_host_review', status: 'completed',
    } as any)).toBeFalse()
  })

  it('does not use execution output as the summary for an unknown host outcome', () => {
    const { component } = createComponent({ run: jasmine.createSpy('run') })
    const review = {
      launchType: 'agent_runtime_host_review',
      status: 'needs_review',
      message: '',
      output: 'OUTPUT_CANARY_MUST_NOT_RENDER',
    } as any
    const completed = {
      launchType: 'agent_runtime_host_completion',
      status: 'completed',
      message: '',
      output: 'Completed output summary',
    } as any

    expect(component.launchEventLabel(review)).toContain('Operator review required')
    expect(component.launchEventSummary(review)).toContain('outcome is unknown')
    expect(component.launchEventSummary(review)).not.toContain(review.output)
    expect(component.launchEventSummary(completed)).toBe(completed.output)
  })

  it('notifies Angular when a deferred inspector action changes the view', fakeAsync(() => {
    const { component, cdr } = createComponent({ run: jasmine.createSpy('run') })
    const execute = jasmine.createSpy('execute')
    component.openAction({ execute } as any)
    component.runSelectedAction()
    expect(execute).not.toHaveBeenCalled()
    tick(360)
    expect(execute).toHaveBeenCalledTimes(1)
    expect(cdr.markForCheck).toHaveBeenCalled()
  }))

  it('does not start overlapping operating-brief refreshes', () => {
    const run = jasmine.createSpy('run')
    const response = new Subject<any>()
    run.and.returnValue(response.asObservable())
    const { component } = createComponent({ run })

    component.runScan()
    component.runScan()

    expect(run).toHaveBeenCalledTimes(1)
    expect(component.scanning).toBeTrue()

    response.error({ error: { error: 'backend unavailable' } })
    expect(component.scanning).toBeFalse()
    expect(component.lastAgentCycleIssue).toContain('has not confirmed')
  })

  it('locks every other approval action while one decision is pending and clears the state on failure', () => {
    const run = jasmine.createSpy('run').and.returnValue(new Subject<any>().asObservable())
    const response = new Subject<void>()
    const resolveApproval = jasmine.createSpy('resolveApproval').and.returnValue(response.asObservable())
    const { component, notifications } = createComponent({ run })
    ;(component as any).workflowService = { resolveApproval }
    const first = { id: 'workflow-1' } as any
    const second = { id: 'workflow-2' } as any
    component.workflowDashboard = {
      counts: { approvals: 2, blocked: 0, dueOpenLoops: 0 },
      approvalItems: [first, second],
    } as any
    const refresh = spyOn(component, 'refresh')

    component.resolveApproval(first, true)

    expect(component.isResolving(first, 'approve')).toBeTrue()
    expect(component.isApprovalActionDisabled()).toBeTrue()
    component.resolveApproval(second, false)
    expect(resolveApproval).toHaveBeenCalledTimes(1)

    response.error(new Error('request failed'))

    expect(component.resolvingId).toBe('')
    expect(component.resolvingAction).toBe('')
    expect(component.isApprovalActionDisabled()).toBeFalse()
    expect(notifications.warning).toHaveBeenCalledWith(
      'Decision outcome unconfirmed',
      'HAI could not confirm whether the decision was saved. Refreshing the queue to reconcile its current state before another decision.'
    )
    expect(refresh).toHaveBeenCalledTimes(1)
  })

  it('blocks approval submissions while dashboard data is refreshing', () => {
    const { component } = createComponent({ run: jasmine.createSpy('run') })
    const workflowResponse = new Subject<any>()
    const resolveApproval = jasmine.createSpy('resolveApproval')
    ;(component as any).workflowService = { dashboard: () => workflowResponse.asObservable(), resolveApproval }
    ;(component as any).ambientService = { overview: () => new Subject<any>().asObservable() }
    ;(component as any).pursuitService = { dashboard: () => new Subject<any>().asObservable() }

    component.refresh()
    const item = { id: 'stale-workflow-1' } as any
    expect(component.isApprovalActionDisabled()).toBeTrue()
    component.resolveApproval(item, true)

    expect(resolveApproval).not.toHaveBeenCalled()
  })

  for (const failure of ['unavailable', 'unauthorized'] as const) {
    it(`blocks stale approval actions after a dashboard refresh is ${failure}`, () => {
      const { component } = createComponent({ run: jasmine.createSpy('run') })
      const staleApproval = { id: 'stale-approval', title: 'Approve old item' } as any
      const resolveApproval = jasmine.createSpy('resolveApproval')
      ;(component as any).workflowService = {
        dashboard: () => throwError(() => failure === 'unauthorized' ? { status: 401 } : new Error('unavailable')),
        resolveApproval,
      }
      ;(component as any).ambientService = { overview: () => of(emptyAmbientOverview()) }
      ;(component as any).pursuitService = { dashboard: () => of(emptyPursuitDashboard()) }
      component.workflowDashboard = {
        counts: { approvals: 1, blocked: 0, dueOpenLoops: 0 },
        approvalItems: [staleApproval],
      } as any
      component.attentionItemsView = [staleApproval]

      component.refresh()
      component.resolveApproval(staleApproval, true)

      expect(component.isApprovalActionDisabled(staleApproval)).toBeTrue()
      expect(resolveApproval).not.toHaveBeenCalled()
      expect(component.dashboardAuthenticationRequired).toBe(failure === 'unauthorized')
    })
  }

  it('only enables approval for an item present in the current complete workflow response', () => {
    const { component } = createComponent({ run: jasmine.createSpy('run') })
    const currentApproval = { id: 'current-approval' } as any
    const staleApproval = { id: 'stale-approval' } as any
    component.workflowDashboard = {
      counts: { approvals: 1, blocked: 0, dueOpenLoops: 0 },
      approvalItems: [currentApproval],
    } as any

    expect(component.isApprovalActionDisabled(currentApproval)).toBeFalse()
    expect(component.isApprovalActionDisabled(staleApproval)).toBeTrue()
  })

  it('releases the running state when the operating-brief request times out', fakeAsync(() => {
    const run = jasmine.createSpy('run')
    run.and.returnValue(new Subject<any>().asObservable())
    const { component, notifications } = createComponent({ run })

    component.runScan()
    tick(30000)

    expect(component.scanning).toBeFalse()
    expect(notifications.error).toHaveBeenCalledWith(
      'Agent cycle failed',
      'The operational cycle could not complete.'
    )
  }))

  it('does not duplicate an automation health check while the first request is pending', () => {
    const run = jasmine.createSpy('run')
    run.and.returnValue(new Subject<any>().asObservable())
    const healthCheck = jasmine.createSpy('runHealthCheck')
    const response = new Subject<any>()
    healthCheck.and.returnValue(response.asObservable())
    const { component } = createComponent({ run }, { runHealthCheck: healthCheck })
    const automation = { id: 'automation-1', name: 'Daily check' } as any

    component.runHealthCheck(automation)
    component.runHealthCheck(automation)

    expect(healthCheck).toHaveBeenCalledTimes(1)
    expect(component.isChecking(automation)).toBeTrue()

    response.error({ error: 'timeout' })
    expect(component.isChecking(automation)).toBeFalse()
  })

  it('warns when a completed automation health check reports an unhealthy state', () => {
    const run = jasmine.createSpy('run')
    const healthCheck = jasmine.createSpy('runHealthCheck').and.returnValue(of({
      status: 'broken', checkedAt: '2026-10-09T10:00:00Z', failureReason: 'Connection refused',
      latencyMs: 0, consecutiveFailures: 2,
    }))
    const { component, notifications } = createComponent({ run }, { runHealthCheck: healthCheck })
    const automation = { id: 'automation-1', name: 'Mail bridge' } as any

    component.runHealthCheck(automation)

    expect(notifications.success).not.toHaveBeenCalled()
    expect(notifications.warning).toHaveBeenCalledWith(
      'Check completed with an issue',
      'Mail bridge is broken: Connection refused'
    )
  })

  it('keeps a memory load failure visible and allows an explicit retry', () => {
    const { component, notifications } = createComponent({ run: jasmine.createSpy('run') })
    const response = new Subject<any[]>()
    const list = jasmine.createSpy('list').and.returnValue(response.asObservable())
    ;(component as any).memoryService = { list }

    component.loadMemories()
    response.error(new Error('memory unavailable'))

    expect(component.memoryLoading).toBeFalse()
    expect(component.memoryLoadError).toBeTrue()
    expect(component.memoriesLoaded).toBeFalse()
    expect(notifications.error).toHaveBeenCalledWith('Memory unavailable', 'Recent memory updates could not be loaded.')

    const retry = new Subject<any[]>()
    list.and.returnValue(retry.asObservable())
    component.loadMemories(true)
    expect(component.memoryLoadError).toBeFalse()
    expect(component.memoryLoading).toBeTrue()
    retry.next([])
    retry.complete()
    expect(component.memoriesLoaded).toBeTrue()
    expect(component.memoryLoading).toBeFalse()
  })

  it('does not present an unloaded automation registry as zero registered automations', () => {
    const run = jasmine.createSpy('run').and.returnValue(new Subject<any>().asObservable())
    const { component } = createComponent({ run })

    const beforeLoading = (component as any).buildCommandActions()
      .find((action: any) => action.id === 'automation')
    expect(beforeLoading.primaryMetric).toBe('Open registry')

    component.diagnosticsLoaded = true
    component.automations = [{ id: 'automation-1' } as any]
    const afterLoading = (component as any).buildCommandActions()
      .find((action: any) => action.id === 'automation')
    expect(afterLoading.primaryMetric).toBe('1 registered')
  })

  it('labels navigation and diagnostics actions according to the operation they actually perform', () => {
    const { component } = createComponent({ run: jasmine.createSpy('run') })
    const actions = (component as any).buildCommandActions()
    const sources = actions.find((action: any) => action.id === 'sources')
    const health = actions.find((action: any) => action.id === 'health')

    expect(sources.title).toBe('Review sources')
    expect(sources.route).toBe('/connected-sources')
    expect(sources.execute).toBeUndefined()
    expect(health.title).toBe('Review health status')
    expect(health.detail).toContain('Load current')
    expect(component.actionModeLabel(health)).toBe('Loads current diagnostics')
  })

  it('labels navigation and diagnostics actions according to the operation they actually perform', () => {
    const { component } = createComponent({ run: jasmine.createSpy('run') })
    const actions = (component as any).buildCommandActions()
    const sources = actions.find((action: any) => action.id === 'sources')
    const health = actions.find((action: any) => action.id === 'health')

    expect(sources.title).toBe('Review sources')
    expect(sources.route).toBe('/connected-sources')
    expect(sources.execute).toBeUndefined()
    expect(health.title).toBe('Review health status')
    expect(health.detail).toContain('Load current')
    expect(component.actionModeLabel(health)).toBe('Loads current diagnostics')
  })

  it('does not present failed dashboard data as an empty workload', () => {
    const run = jasmine.createSpy('run').and.returnValue(new Subject<any>().asObservable())
    const { component } = createComponent({ run })
    const lastSuccessfulCheck = new Date('2026-01-01T12:00:00Z')
    component.dashboardCheckedAt = lastSuccessfulCheck
    ;(component as any).workflowService = {
      dashboard: () => throwError(() => new Error('workflow unavailable')),
    }
    ;(component as any).ambientService = {
      overview: () => throwError(() => new Error('ambient unavailable')),
    }
    ;(component as any).pursuitService = {
      dashboard: () => throwError(() => new Error('pursuit unavailable')),
    }

    component.refresh()

    expect(component.dashboardLoadErrors).toEqual([
      'Workflow status',
      'Ambient scan',
      'Pursuit status',
    ])
    expect(component.hasDashboardLoadError()).toBeTrue()
    expect(component.hasLiveWork()).toBeTrue()
    expect(component.dashboardCheckedAt).toBe(lastSuccessfulCheck)
  })

  it('reports live dashboard source states and the completed API check time', () => {
    const { component } = createComponent({ run: jasmine.createSpy('run') })
    ;(component as any).workflowService = {
      dashboard: () => of({
        counts: { approvals: 0, blocked: 0, dueOpenLoops: 0 },
        approvalItems: [], blockedItems: [], readyItems: [], dueOpenLoops: [],
      }),
    }
    ;(component as any).ambientService = { overview: () => of(emptyAmbientOverview()) }
    ;(component as any).pursuitService = { dashboard: () => of(emptyPursuitDashboard()) }

    component.refresh()

    expect(component.dashboardSourceState('workflow')).toBe('loaded')
    expect(component.dashboardSourceState('ambient')).toBe('loaded')
    expect(component.dashboardSourceState('pursuits')).toBe('loaded')
    expect(component.dashboardCheckedAt instanceof Date).toBeTrue()
  })

  it('only presents queue sections as empty when complete backend counts and item lists agree', () => {
    const { component } = createComponent({ run: jasmine.createSpy('run') })
    component.workflowDashboard = {
      counts: { approvals: 0, blocked: 0, dueOpenLoops: 0, ready: 0 },
      approvalItems: [], blockedItems: [], dueOpenLoops: [], readyItems: [],
    } as any

    expect(component.workflowQueueHasNoApprovals()).toBeTrue()
    expect(component.workflowQueueHasNoActiveItems()).toBeTrue()
    expect(component.workflowQueueHasNoBlockers()).toBeTrue()

    component.dashboardLoadErrors = ['Workflow status']
    expect(component.workflowQueueHasNoApprovals()).toBeFalse()
    expect(component.workflowQueueHasNoActiveItems()).toBeFalse()
    expect(component.workflowQueueHasNoBlockers()).toBeFalse()

    component.dashboardLoadErrors = []
    component.workflowDashboard = {
      counts: { approvals: 0, blocked: 0, dueOpenLoops: 0, ready: 0 },
      approvalItems: [{ id: 'approval-inconsistent' }], blockedItems: [], dueOpenLoops: [], readyItems: [],
    } as any
    expect(component.workflowQueueHasNoApprovals()).toBeFalse()

    component.workflowDashboard = {
      counts: { approvals: 0, blocked: 0, dueOpenLoops: 0, ready: 0 },
      blockedItems: [], dueOpenLoops: [], readyItems: [],
    } as any
    expect(component.workflowQueueHasNoApprovals()).toBeFalse()
  })

  it('does not label priorities unavailable or unconfigured without a valid ambient response', () => {
    const { component } = createComponent({ run: jasmine.createSpy('run') })
    expect(component.ambientPrioritiesAvailable()).toBeFalse()
    component.ambientOverview = { needs: [], scans: [] } as any
    expect(component.ambientPrioritiesAvailable()).toBeTrue()
    component.ambientOverview = { needs: null, scans: null } as any
    expect(component.ambientPrioritiesAvailable()).toBeTrue()
    component.dashboardLoadErrors = ['Ambient scan']
    expect(component.ambientPrioritiesAvailable()).toBeFalse()
  })

  it('passes the open-loop workflow ID to the workflow-engine route', () => {
    const { component, router } = createComponent({ run: jasmine.createSpy('run') })
    component.openWorkflowId('workflow-for-loop')
    expect(router.navigate).toHaveBeenCalledWith(['/workflow-engine'], {
      queryParams: { workflowId: 'workflow-for-loop' },
    })
  })

  it('keeps per-source failures visible without masking successful dashboard responses', () => {
    const { component } = createComponent({ run: jasmine.createSpy('run') })
    ;(component as any).workflowService = {
      dashboard: () => of({
        counts: { approvals: 0, blocked: 0, dueOpenLoops: 0 },
        approvalItems: [], blockedItems: [], readyItems: [], dueOpenLoops: [],
      }),
    }
    ;(component as any).ambientService = { overview: () => throwError(() => new Error('offline')) }
    ;(component as any).pursuitService = { dashboard: () => of(emptyPursuitDashboard()) }

    component.refresh()

    expect(component.dashboardSourceState('workflow')).toBe('loaded')
    expect(component.dashboardSourceState('ambient')).toBe('unavailable')
    expect(component.dashboardSourceState('pursuits')).toBe('loaded')
  })

  it('replaces stale primary action with a checking state while a refresh is in flight', () => {
    const { component } = createComponent({ run: jasmine.createSpy('run') })
    const workflow = new Subject<any>()
    const ambient = new Subject<any>()
    const pursuits = new Subject<any>()
    ;(component as any).workflowService = {
      dashboard: jasmine.createSpy('dashboard').and.returnValues(
        of({ counts: { approvals: 0, blocked: 0, dueOpenLoops: 0 }, approvalItems: [], blockedItems: [], readyItems: [], dueOpenLoops: [] }),
        workflow,
      ),
    }
    ;(component as any).ambientService = {
      overview: jasmine.createSpy('overview').and.returnValues(of(emptyAmbientOverview()), ambient),
    }
    ;(component as any).pursuitService = {
      dashboard: jasmine.createSpy('dashboard').and.returnValues(of(emptyPursuitDashboard()), pursuits),
    }

    component.refresh()
    expect(component.nextAction()?.kind).toBe('brief')

    component.refresh()

    expect(component.dashboardSourceState('workflow')).toBe('checking')
    expect(component.dashboardSourceState('ambient')).toBe('checking')
    expect(component.dashboardSourceState('pursuits')).toBe('checking')
    expect(component.nextAction()?.kind).toBe('loading')
    expect(component.nextActionBusy(component.nextAction()!)).toBeTrue()
  })

  it('cancels an obsolete dashboard refresh so stale data cannot replace the latest view', () => {
    const run = jasmine.createSpy('run').and.returnValue(new Subject<any>().asObservable())
    const { component } = createComponent({ run })
    const firstWorkflow = new Subject<any>()
    const firstAmbient = new Subject<any>()
    const firstPursuits = new Subject<any>()
    const secondWorkflow = new Subject<any>()
    const secondAmbient = new Subject<any>()
    const secondPursuits = new Subject<any>()
    const workflow = jasmine.createSpy('dashboard').and.returnValues(firstWorkflow, secondWorkflow)
    const ambient = jasmine.createSpy('overview').and.returnValues(firstAmbient, secondAmbient)
    const pursuits = jasmine.createSpy('dashboard').and.returnValues(firstPursuits, secondPursuits)
    ;(component as any).workflowService = { dashboard: workflow }
    ;(component as any).ambientService = { overview: ambient }
    ;(component as any).pursuitService = { dashboard: pursuits }

    component.refresh()
    component.refresh()

    secondWorkflow.next({ counts: { current: 2 } })
    secondWorkflow.complete()
    secondAmbient.next({ scans: [] })
    secondAmbient.complete()
    secondPursuits.next({ counts: { active: 2 } })
    secondPursuits.complete()

    firstWorkflow.next({ counts: { current: 99 } })
    firstWorkflow.complete()
    firstAmbient.next({ scans: [] })
    firstAmbient.complete()
    firstPursuits.next({ counts: { active: 99 } })
    firstPursuits.complete()

    expect(component.workflowDashboard?.counts?.['current']).toBe(2)
    expect(component.pursuitDashboard?.counts?.['active']).toBe(2)
  })

  it('does not present failed diagnostics as an empty automation registry', () => {
    const run = jasmine.createSpy('run').and.returnValue(new Subject<any>().asObservable())
    const { component } = createComponent({ run })
    ;(component as any).automationsService = {
      getAutomations: () => throwError(() => new Error('automations unavailable')),
      getHealthSummary: () => throwError(() => new Error('health unavailable')),
    }
    ;(component as any).agentRuntimeService = {
      overview: () => throwError(() => new Error('runtimes unavailable')),
    }

    component.loadDiagnosticsData()

    expect(component.diagnosticsLoadErrors).toEqual([
      'Automation registry',
      'Automation health',
      'Runtime overview',
    ])
    expect(component.hasDiagnosticsLoadError()).toBeTrue()
    const automation = (component as any).buildCommandActions()
      .find((action: any) => action.id === 'automation')
    expect(automation.primaryMetric).toBe('Unavailable')
  })

  it('keeps automation diagnostics bound to the most recently selected automation', () => {
    const run = jasmine.createSpy('run').and.returnValue(new Subject<any>().asObservable())
    const firstResponse = new Subject<any>()
    const secondResponse = new Subject<any>()
    const getDiagnostics = jasmine.createSpy('getDiagnostics')
      .and.returnValues(firstResponse.asObservable(), secondResponse.asObservable())
    const { component } = createComponent({ run }, { getDiagnostics })
    const firstAutomation = { id: 'automation-a', name: 'Automation A' } as any
    const secondAutomation = { id: 'automation-b', name: 'Automation B' } as any

    component.openDiagnostics(firstAutomation)
    expect(firstResponse.observed).toBeTrue()
    component.openDiagnostics(secondAutomation)
    expect(firstResponse.observed).toBeFalse()
    expect(secondResponse.observed).toBeTrue()

    firstResponse.next({ launchTarget: 'target-a' })
    expect(component.diagnostics).toBeUndefined()
    expect(component.diagnosticsName).toBe('Automation B')

    secondResponse.next({ launchTarget: 'target-b' })
    expect(component.diagnostics?.launchTarget).toBe('target-b')
    expect(component.diagnosticsName).toBe('Automation B')
  })

  it('does not apply an automation diagnostics response after its inspector is closed', () => {
    const run = jasmine.createSpy('run').and.returnValue(new Subject<any>().asObservable())
    const response = new Subject<any>()
    const { component } = createComponent({ run }, {
      getDiagnostics: jasmine.createSpy('getDiagnostics').and.returnValue(response.asObservable()),
    })

    component.openDiagnostics({ id: 'automation-a', name: 'Automation A' } as any)
    expect(response.observed).toBeTrue()
    component.closeDiagnostics()
    expect(response.observed).toBeFalse()
    response.next({ launchTarget: 'target-a' })

    expect(component.isDiagnosticsVisible).toBeFalse()
    expect(component.diagnostics).toBeUndefined()
  })

  it('loads only the bounded recent-memory slice for the overview', () => {
    const run = jasmine.createSpy('run').and.returnValue(new Subject<any>().asObservable())
    const { component } = createComponent({ run })
    const list = jasmine.createSpy('list').and.returnValue(new Subject<any[]>().asObservable())
    ;(component as any).memoryService = { list }

    component.loadMemories()

    expect(list).toHaveBeenCalledWith(undefined, false, 20)
  })

  it('tracks overlapping memory archives per row and reports success and failure', async () => {
    const responses = new Map<string, Subject<any>>()
    const archive = jasmine.createSpy('archive').and.callFake((id: string) => {
      const response = new Subject<any>()
      responses.set(id, response)
      return response.asObservable()
    })
    const notifications = {
      error: jasmine.createSpy('error'),
      success: jasmine.createSpy('success'),
      warning: jasmine.createSpy('warning'),
    }
    await TestBed.configureTestingModule({
      declarations: [ControlCenterComponent],
      imports: [CommonModule, FormsModule],
      schemas: [NO_ERRORS_SCHEMA],
      providers: [
        { provide: AUTOMATIONS_SERVICE_TOKEN, useValue: {} },
        { provide: WorkflowService, useValue: { dashboard: () => of({ counts: { approvals: 0, blocked: 0, dueOpenLoops: 0 }, approvalItems: [], blockedItems: [], readyItems: [], dueOpenLoops: [] }) } },
        { provide: PursuitService, useValue: { dashboard: () => of(emptyPursuitDashboard()) } },
        { provide: AgentCycleService, useValue: {} },
        { provide: AgentRuntimeService, useValue: {} },
        { provide: AmbientService, useValue: { overview: () => of(emptyAmbientOverview()) } },
        { provide: ContextMemoryService, useValue: { archive } },
        { provide: NzNotificationService, useValue: notifications },
        { provide: Router, useValue: { navigate: jasmine.createSpy('navigate') } },
        { provide: ModuleViewPreferencesService, useValue: {
          get: () => ({ version: 1, mode: 'advanced', openSections: {}, navigationMode: 'auto' }),
        } },
      ],
    }).compileComponents()

    const fixture = TestBed.createComponent(ControlCenterComponent)
    const component = fixture.componentInstance
    const first = { id: 'memory-1', summary: 'First lesson', kind: 'lesson', confidence: 0.8 } as any
    const second = { id: 'memory-2', summary: 'Second lesson', kind: 'lesson', confidence: 0.7 } as any
    component.memories = [first, second]
    component.memoriesLoaded = true
    ;(component as any).rebuildViewModel()
    fixture.detectChanges()

    const firstButton = fixture.nativeElement.querySelector('[data-testid="memory-archive-memory-1"]') as HTMLButtonElement
    const secondButton = fixture.nativeElement.querySelector('[data-testid="memory-archive-memory-2"]') as HTMLButtonElement
    firstButton.click()
    fixture.detectChanges()

    expect(archive).toHaveBeenCalledTimes(1)
    expect(archive).toHaveBeenCalledWith('memory-1')
    expect(firstButton.disabled).toBeTrue()
    expect(firstButton.getAttribute('aria-label')).toBe('Archiving memory')
    expect((firstButton.querySelector('i') as HTMLElement & { nzType?: string }).nzType).toBe('loading')
    expect(secondButton.disabled).toBeFalse()

    secondButton.click()
    fixture.detectChanges()

    expect(archive).toHaveBeenCalledTimes(2)
    expect(secondButton.disabled).toBeTrue()
    expect((secondButton.querySelector('i') as HTMLElement & { nzType?: string }).nzType).toBe('loading')

    const firstResponse = responses.get('memory-1')
    expect(firstResponse?.observed).toBeTrue()
    firstResponse?.next(first)
    expect(component.isArchivingMemory(first)).toBeFalse()
    firstResponse?.complete()
    fixture.detectChanges()
    expect(component.memories.some((memory) => memory.id === first.id)).toBeFalse()
    expect(component.recentMemories().some((memory) => memory.id === first.id)).toBeFalse()
    expect(notifications.success).toHaveBeenCalledWith(
      'Memory archived',
      'The item will no longer be used as active context.'
    )

    const secondResponse = responses.get('memory-2')
    expect(secondResponse?.observed).toBeTrue()
    secondResponse?.error(new Error('archive failed'))
    expect(component.isArchivingMemory(second)).toBeFalse()
    fixture.detectChanges()
    expect(component.memories.some((memory) => memory.id === second.id)).toBeTrue()
    expect(notifications.error).toHaveBeenCalledWith(
      'Archive outcome unconfirmed',
      'Reload memory updates to check whether the item was archived before trying again.'
    )
  })

  it('uses this module preference to limit Basic view and reveal Advanced records', () => {
    const { component, viewPreferences, setViewMode } = createComponent({
      run: jasmine.createSpy('run'),
    })
    component.activeItemsView = Array.from({ length: 5 }, (_, index) => ({ id: `workflow-${index}` } as any))
    component.attentionItemsView = Array.from({ length: 4 }, (_, index) => ({ id: `approval-${index}` } as any))

    expect(component.isAdvancedView).toBeFalse()
    expect(component.activeItemsForView().length).toBe(2)
    expect(component.attentionItems().length).toBe(1)
    expect(viewPreferences.get).toHaveBeenCalledWith('control-center')

    setViewMode('advanced')
    expect(component.isAdvancedView).toBeTrue()
    expect(component.activeItemsForView().length).toBe(5)
    expect(component.attentionItems().length).toBe(4)
  })

  it('renders every Advanced action card with an accessible name and live context description', async () => {
    const emptyQueue = {
      counts: { approvals: 0, blocked: 0, dueOpenLoops: 0, ready: 0 },
      approvalItems: [], blockedItems: [], readyItems: [], dueOpenLoops: [], rules: [],
    }
    await TestBed.configureTestingModule({
      declarations: [ControlCenterComponent],
      imports: [CommonModule, FormsModule],
      schemas: [NO_ERRORS_SCHEMA],
      providers: [
        { provide: AUTOMATIONS_SERVICE_TOKEN, useValue: {} },
        { provide: WorkflowService, useValue: { dashboard: () => of(emptyQueue) } },
        { provide: PursuitService, useValue: { dashboard: () => of(emptyPursuitDashboard({ needsRobert: [], blocked: [], stale: [] })) } },
        { provide: AgentCycleService, useValue: {} },
        { provide: AgentRuntimeService, useValue: {} },
        { provide: AmbientService, useValue: { overview: () => of(emptyAmbientOverview()) } },
        { provide: ContextMemoryService, useValue: {} },
        { provide: NzNotificationService, useValue: {} },
        { provide: Router, useValue: { navigate: jasmine.createSpy('navigate') } },
        { provide: ModuleViewPreferencesService, useValue: {
          get: () => ({ version: 1, mode: 'advanced', openSections: {}, navigationMode: 'auto' }),
        } },
      ],
    }).compileComponents()

    const fixture = TestBed.createComponent(ControlCenterComponent)
    fixture.nativeElement.style.setProperty('--hai-text', '#edf4ff')
    fixture.detectChanges()
    const pageTitle = fixture.nativeElement.querySelector('.page-heading h1') as HTMLElement
    const titleButton = pageTitle.querySelector('button') as HTMLButtonElement
    const diagnosticsClose = fixture.nativeElement.querySelector('[data-testid="diagnostics-close"]') as HTMLButtonElement
    expect(titleButton.textContent?.trim()).toBe('Command Center')
    expect(titleButton.hasAttribute('aria-label')).toBeFalse()
    expect(diagnosticsClose.getAttribute('aria-label')).toBe('Collapse diagnostics section')
    expect(getComputedStyle(pageTitle).color).toBe('rgb(237, 244, 255)')
    expect(getComputedStyle(titleButton).color).toBe('rgb(237, 244, 255)')
    const cards = Array.from((fixture.nativeElement as HTMLElement).querySelectorAll<HTMLButtonElement>('.command-card'))
    expect(cards.length).toBe(10)
    for (const card of cards) {
      const actionId = card.getAttribute('data-testid')?.replace('action-', '')
      const action = fixture.componentInstance.commandActions().find((candidate) => candidate.id === actionId)
      const descriptionId = card.getAttribute('aria-describedby')
      const description = descriptionId ? fixture.nativeElement.querySelector(`#${descriptionId}`) as HTMLElement : null

      expect(action).toBeDefined()
      if (!action || !description) throw new Error('An action card is missing its accessible description')
      expect(card.getAttribute('aria-label')).toBe(action.title)
      expect(description).not.toBeNull()
      expect(description.textContent).toContain(action.primaryMetric)
      expect(description.textContent).toContain(action.secondaryMetric)
      expect(description.textContent).toContain(action.context)
    }

    const workflowId = '22222222-2222-4222-8222-222222222222'
    fixture.componentInstance.workflowDashboard = {
      ...emptyQueue,
      counts: { ...emptyQueue.counts, dueOpenLoops: 1 },
      dueOpenLoops: [{ id: 'loop-1', workflowId, waitingFor: 'Lawyer reply', nextAction: 'Follow up' }],
    } as any
    ;(fixture.componentInstance as any).rebuildViewModel()
    fixture.detectChanges()
    const reviewLoop = fixture.nativeElement.querySelector('[data-testid="open-loop-review-loop-1"]') as HTMLButtonElement
    expect(reviewLoop).not.toBeNull()
    reviewLoop.click()

    expect(TestBed.inject(Router).navigate).toHaveBeenCalledWith(['/workflow-engine'], {
      queryParams: { workflowId },
    })
  })

  it('routes the primary approval action to the matching workflow without bypassing approval controls', () => {
    const { component, router } = createComponent({ run: jasmine.createSpy('run') })
    component.workflowDashboard = { counts: { approvals: 1, blocked: 0, dueOpenLoops: 0 } } as any
    component.attentionItemsView = [{ id: 'approval-1', title: 'Review evidence', description: 'Confirm the attached source.' } as any]

    expect(component.nextAction()?.kind).toBe('approval')
    component.runNextAction()

    expect(router.navigate).toHaveBeenCalledWith(['/workflow-engine'], {
      queryParams: { workflowId: 'approval-1' },
    })
  })

  it('routes to a non-empty backend queue even when the response omits item detail', () => {
    const { component, router } = createComponent({ run: jasmine.createSpy('run') })
    component.workflowDashboard = { counts: { approvals: 2, blocked: 0, dueOpenLoops: 0 } } as any

    expect(component.nextAction()?.actionLabel).toBe('Open decision queue')
    component.runNextAction()

    expect(router.navigate).toHaveBeenCalledWith(['/workflow-engine'], { queryParams: undefined })
  })

  it('shows a loading state, does not act before data arrives, and reports failed counts as unavailable', () => {
    const { component, router } = createComponent({ run: jasmine.createSpy('run') })
    component.loading = true

    expect(component.nextAction()?.kind).toBe('loading')
    component.runNextAction()
    expect(router.navigate).not.toHaveBeenCalled()

    component.loading = false
    component.dashboardLoadErrors = ['Workflow status']
    expect(component.nextAction()?.kind).toBe('queue-unavailable')
    expect(component.nextAction()?.actionLabel).toBe('Retry queue check')
    const refresh = spyOn(component, 'refresh')
    component.runNextAction()
    expect(refresh).toHaveBeenCalledTimes(1)
    expect(router.navigate).not.toHaveBeenCalled()
    expect(component.workflowCount('approvals')).toBe('Unavailable')
    expect(component.hasDashboardLoadError()).toBeTrue()
  })

  it('uses a real empty-queue response to offer a scan instead of inventing work', () => {
    const run = jasmine.createSpy('run').and.returnValue(new Subject<any>().asObservable())
    const { component } = createComponent({ run })
    component.workflowDashboard = {
      counts: { approvals: 0, blocked: 0, dueOpenLoops: 0, ready: 0 },
      approvalItems: [],
      blockedItems: [],
      readyItems: [],
      dueOpenLoops: [],
    } as any

    expect(component.workflowCount('approvals')).toBe('0')
    expect(component.nextAction()?.kind).toBe('brief')
    component.runNextAction()

    expect(run).toHaveBeenCalledTimes(1)
    expect(component.scanning).toBeTrue()
  })

  it('surfaces pursuit decisions after workflow work is clear and opens the selected pursuit', () => {
    const { component, router } = createComponent({ run: jasmine.createSpy('run') })
    component.workflowDashboard = {
      counts: { approvals: 0, blocked: 0, dueOpenLoops: 0, ready: 1 },
      approvalItems: [], blockedItems: [], readyItems: [], dueOpenLoops: [],
    } as any
    component.activeItemsView = [{ id: 'workflow-active', title: 'Background workflow' } as any]
    component.pursuitDashboard = emptyPursuitDashboard({
      needsRobert: [{ pursuit: { id: 'pursuit-42', title: 'Resolve Vivare dispute' }, nextAction: 'Review the evidence request.' }],
    })

    expect(component.nextAction()?.kind).toBe('pursuit')
    expect(component.nextAction()?.title).toBe('Resolve Vivare dispute')
    component.runNextAction()

    expect(router.navigate).toHaveBeenCalledWith(['/pursuits'], {
      queryParams: { selected: 'pursuit-42' },
    })
  })

  it('fails closed when pursuit or ambient queue payloads are incomplete', () => {
    const { component } = createComponent({ run: jasmine.createSpy('run') })
    component.workflowDashboard = {
      counts: { approvals: 0, blocked: 0, dueOpenLoops: 0, ready: 0 },
      approvalItems: [], blockedItems: [], readyItems: [], dueOpenLoops: [],
    } as any
    component.pursuitDashboard = { counts: { active: 0 } } as any

    expect(component.nextAction()?.kind).toBe('queue-unavailable')
    expect(component.nextAction()?.source).toBe('Pursuit engine')

    component.pursuitDashboard = emptyPursuitDashboard()
    component.ambientOverview = {} as any
    expect(component.nextAction()?.kind).toBe('queue-unavailable')
    expect(component.nextAction()?.source).toBe('Ambient engine')
  })

  it('surfaces malformed successful ambient and pursuit responses in the dashboard recovery state', () => {
    const { component } = createComponent({ run: jasmine.createSpy('run') })
    component.workflowDashboard = {
      counts: { approvals: 0, blocked: 0, dueOpenLoops: 0, ready: 0 },
      approvalItems: [], blockedItems: [], readyItems: [], dueOpenLoops: [],
    } as any

    component.ambientOverview = {} as any
    expect(component.hasDashboardLoadError()).toBeTrue()
    expect(component.dashboardSourceState('ambient')).toBe('unavailable')
    expect(component.dashboardLoadErrorMessage()).toContain('Ambient scan data')

    component.ambientOverview = emptyAmbientOverview()
    component.pursuitDashboard = { counts: { active: 0 } } as any
    expect(component.hasDashboardLoadError()).toBeTrue()
    expect(component.dashboardSourceState('pursuits')).toBe('unavailable')
    expect(component.dashboardLoadErrorMessage()).toContain('Pursuit status data')
  })

  it('fails closed when a successful workflow response omits a required queue count', () => {
    const { component } = createComponent({ run: jasmine.createSpy('run') })
    component.workflowDashboard = {
      counts: { approvals: 0, blocked: 0 },
      approvalItems: [],
      blockedItems: [],
      readyItems: [],
      dueOpenLoops: [],
    } as any

    expect(component.hasDashboardLoadError()).toBeTrue()
    expect(component.dashboardLoadErrorMessage()).toContain('Workflow queue counts')
    expect(component.nextAction()?.kind).toBe('queue-unavailable')
    expect(component.workflowCount('dueOpenLoops')).toBe('Unavailable')
  })

  it('refreshes the live dashboard when the queue count contract is incomplete', () => {
    const { component } = createComponent({ run: jasmine.createSpy('run') })
    const workflow = jasmine.createSpy('dashboard').and.returnValue(of({
      counts: { approvals: 0, blocked: 0, dueOpenLoops: 0 },
      approvalItems: [],
      blockedItems: [],
      readyItems: [],
      highRiskItems: [],
      itemsWithoutNextAction: [],
      dueOpenLoops: [],
      rules: [],
    }))
    const ambient = jasmine.createSpy('overview').and.returnValue(of(emptyAmbientOverview()))
    const pursuits = jasmine.createSpy('dashboard').and.returnValue(of(emptyPursuitDashboard()))
    ;(component as any).workflowService = { dashboard: workflow }
    ;(component as any).ambientService = { overview: ambient }
    ;(component as any).pursuitService = { dashboard: pursuits }
    component.workflowDashboard = { counts: { approvals: 0, blocked: 0 } } as any

    component.runNextAction()

    expect(workflow).toHaveBeenCalledTimes(1)
    expect(ambient).toHaveBeenCalledTimes(1)
    expect(pursuits).toHaveBeenCalledTimes(1)
    expect(component.nextAction()?.kind).toBe('brief')
  })

  it('shows only active queue signals in Basic and exposes zero counts in Advanced', () => {
    const { component, setViewMode } = createComponent({ run: jasmine.createSpy('run') })
    const dashboard = {
      counts: { approvals: 0, blocked: 1, dueOpenLoops: 0 },
      approvalItems: [],
      blockedItems: [{ id: 'blocked-1' }],
      readyItems: [],
      dueOpenLoops: [],
    } as any
    component.workflowDashboard = dashboard

    expect(component.workflowSummaryVisible()).toBeTrue()
    expect(component.workflowSignalVisible('blocked')).toBeTrue()
    expect(component.workflowSignalVisible('approvals')).toBeFalse()
    expect(component.nextAction()?.kind).toBe('blocked')

    dashboard.counts['blocked'] = 0
    dashboard.blockedItems = []
    expect(component.workflowSummaryVisible()).toBeFalse()

    setViewMode('advanced')
    expect(component.workflowSummaryVisible()).toBeTrue()
    expect(component.workflowCount('approvals')).toBe('0')
  })

  it('routes blocked primary actions to the specific workflow record', () => {
    const { component, router } = createComponent({ run: jasmine.createSpy('run') })
    component.workflowDashboard = { counts: { approvals: 0, blocked: 1, dueOpenLoops: 0 } } as any
    component.blockedItemsView = [{ id: 'blocked-1', title: 'Missing document' } as any]

    expect(component.nextAction()?.kind).toBe('blocked')
    component.runNextAction()

    expect(router.navigate).toHaveBeenCalledWith(['/workflow-engine'], {
      queryParams: { workflowId: 'blocked-1' },
    })
  })

  it('keeps blocker inspection available during a scan and discloses full diagnostics only in Advanced', () => {
    const { component, router, setViewMode } = createComponent({ run: jasmine.createSpy('run') })
    const diagnostic = 'No capable model was selected; ' + 'Execution evidence is missing; '.repeat(30)
    component.workflowDashboard = { counts: { approvals: 0, blocked: 1, dueOpenLoops: 0 } } as any
    component.blockedItemsView = [{ id: 'blocked-1', title: 'Readiness probe', blockedReason: diagnostic } as any]
    component.scanning = true
    const action = component.nextAction()!
    expect(component.nextActionDetail(action)).toContain('No capable model was selected.')
    expect(component.nextActionDetail(action).length).toBeLessThan(260)
    expect(component.nextActionDetail(action)).not.toContain('Execution evidence is missing')
    expect(component.nextActionBusy(action)).toBeFalse()
    component.runNextAction()
    expect(router.navigate).toHaveBeenCalledWith(['/workflow-engine'], { queryParams: { workflowId: 'blocked-1' } })
    setViewMode('advanced')
    expect(component.nextActionDetail(action)).toBe(diagnostic)
    expect(component.blockedItemsView[0].blockedReason).toBe(diagnostic)
    expect(component.nextActionBusy({ ...action, kind: 'brief' })).toBeTrue()
  })

  it('routes an inspected action to its configured destination', fakeAsync(() => {
    const { component, router } = createComponent({ run: jasmine.createSpy('run') })
    component.openAction({ route: '/connected-sources' } as any)
    component.runSelectedAction()
    tick(360)

    expect(router.navigate).toHaveBeenCalledWith(['/connected-sources'])
  }))

  it('offers an explicit retry after an unsuccessful operating brief', () => {
    const run = jasmine.createSpy('run').and.returnValue(new Subject<any>().asObservable())
    const { component } = createComponent({ run })
    component.workflowDashboard = { counts: { approvals: 0, blocked: 0, dueOpenLoops: 0 } } as any
    component.lastAgentCycleIssue = 'The last cycle did not complete.'

    expect(component.nextAction()?.kind).toBe('cycle-issue')
    component.runNextAction()
    expect(run).toHaveBeenCalledTimes(1)
  })
})
