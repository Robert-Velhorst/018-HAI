import { HttpErrorResponse } from '@angular/common/http'
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing'
import { CommonModule } from '@angular/common'
import { provideHttpClient, withInterceptorsFromDi } from '@angular/common/http'
import { ComponentFixture, TestBed } from '@angular/core/testing'
import { FormsModule } from '@angular/forms'
import { BrowserAnimationsModule } from '@angular/platform-browser/animations'
import { ActivatedRoute } from '@angular/router'
import {
  AuditOutline,
  BranchesOutline,
  CheckCircleOutline,
  CloseOutline,
  DownOutline,
  LinkOutline,
  NodeIndexOutline,
  PauseCircleOutline,
  ProfileOutline,
  ReloadOutline,
  RightOutline,
  SafetyCertificateOutline,
  UpOutline,
  WarningOutline,
} from '@ant-design/icons-angular/icons'
import { NzButtonModule } from 'ng-zorro-antd/button'
import { NzDrawerModule } from 'ng-zorro-antd/drawer'
import { NZ_ICONS, NzIconModule } from 'ng-zorro-antd/icon'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import { NzSpinModule } from 'ng-zorro-antd/spin'
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service'
import { of, throwError } from 'rxjs'
import { ControlRoomModule } from '../../control-room/control-room.module'
import { IPlanGraph, IPlanNode } from '../../models/plan-graph.model.interface'
import { PlanGraphService } from '../../services/plan-graph.service'
import { PlanCoordinationComponent } from './plan-coordination.component'

describe('PlanCoordinationComponent', () => {
  let service: jasmine.SpyObj<PlanGraphService>
  let notification: jasmine.SpyObj<any>
  let component: PlanCoordinationComponent

  const node = (overrides: Partial<IPlanNode> = {}): IPlanNode => ({
    id: 'node-1',
    sequence: 1,
    title: 'Collect evidence',
    status: 'ready',
    ownerType: 'hai',
    risk: 'low',
    approvalRequired: false,
    approvalStatus: 'not_required',
    dependencyIds: [],
    constraints: [],
    resourceEstimates: [],
    bindings: [],
    transport: {
      id: 'node-1',
      type: 'objective',
      title: 'Collect evidence',
      owner: 'hai',
      status: 'ready',
      estimatedMinutes: 0,
      estimatedCostEur: 0,
      risk: 'low',
      approvalState: 'not_required',
      bindings: {},
    },
    ...overrides,
  })

  const plan = (overrides: Partial<IPlanGraph> = {}): IPlanGraph => ({
    id: 'plan-1',
    title: 'Evidence response',
    objective: 'Prepare a verified response',
    status: 'draft',
    risk: 'medium',
    revision: 1,
    planDigest: 'digest-1',
    successCriteria: ['Sources attached'],
    nodes: [node()],
    edges: [],
    criticalPathNodeIds: ['node-1'],
    constraints: [],
    resourceEstimates: [],
    bindings: [],
    approval: { required: true, status: 'pending', reason: 'External communication' },
    createdAt: '2026-08-05T08:00:00Z',
    updatedAt: '2026-08-05T08:00:00Z',
    revisions: [],
    repairHistory: [],
    canExecute: false,
    ...overrides,
  })

  beforeEach(() => {
    service = jasmine.createSpyObj<PlanGraphService>('PlanGraphService', ['list', 'get', 'preview', 'accept', 'replan'])
    notification = jasmine.createSpyObj('NzNotificationService', ['success', 'error'])
    service.list.and.returnValue(of([plan()]))
    service.get.and.callFake(() => of(plan()))
    component = new PlanCoordinationComponent(service, notification, {
      snapshot: { queryParamMap: { get: () => null } },
    } as any)
  })

  it('loads the immutable plan list and exposes the next coordinated node', () => {
    component.ngOnInit()

    expect(component.loading).toBeFalse()
    expect(component.selectedPlan?.id).toBe('plan-1')
    expect(component.nextCoordinatedNode?.id).toBe('node-1')
    expect(component.planRequiresDecision).toBeTrue()
  })

  it('prefers a ready node over an earlier planned node for the next action', () => {
    component.selectedPlan = plan({ nodes: [
      node({ id: 'planned-first', sequence: 1, status: 'planned' }),
      node({ id: 'ready-next', sequence: 2, status: 'ready' }),
    ] })

    expect(component.nextCoordinatedNode?.id).toBe('ready-next')
  })

  it('selects and opens a plan requested by a workflow deep link', () => {
    service.list.and.returnValue(of([plan({ id: 'other-plan' }), plan({ id: 'workflow-plan' })]))
    component = new PlanCoordinationComponent(service, notification, {
      snapshot: { queryParamMap: { get: (key: string) => key === 'planId' ? 'workflow-plan' : null } },
    } as any)

    component.ngOnInit()

    expect(component.selectedPlan?.id).toBe('workflow-plan')
    expect(component.inspectorOpen).toBeTrue()
  })

  it('creates a trimmed preview with deduplicated criteria and constraints', () => {
    const preview = plan({ id: 'preview-1' })
    service.preview.and.returnValue(of(preview))
    component.previewForm = {
      title: '  Evidence response ',
      objective: ' Prepare a verified response ',
      successCriteria: 'Sources attached\nSources attached\nTests pass',
      deadlineAt: '',
      pursuitId: '',
      workflowId: '',
      constraints: 'Draft only\nDraft only',
      estimatedMinutes: 45,
      estimatedCostEur: 0,
    }

    component.createPreview()

    expect(service.preview).toHaveBeenCalledWith(jasmine.objectContaining({
      title: 'Evidence response',
      nodes: jasmine.arrayContaining([
        jasmine.objectContaining({ type: 'objective', title: 'Prepare a verified response', estimatedMinutes: 45 }),
        jasmine.objectContaining({ type: 'success_criterion', title: 'Sources attached' }),
        jasmine.objectContaining({ type: 'constraint', title: 'Draft only' }),
      ]),
    }))
    expect(component.selectedPlan?.id).toBe('preview-1')
    expect(component.inspectorOpen).toBeTrue()
  })

  it('keeps preview submission recoverable after an API error', () => {
    service.preview.and.returnValue(throwError(() => new HttpErrorResponse({
      status: 503,
      error: { error: 'Preview service unavailable' },
    })))
    component.previewForm = {
      title: 'Evidence response',
      objective: 'Prepare a verified response',
      successCriteria: 'Sources attached',
      deadlineAt: '',
      pursuitId: '',
      workflowId: '',
      constraints: '',
    }

    component.createPreview()

    expect(component.saving).toBeFalse()
    expect(component.selectedPlan).toBeUndefined()
    expect(notification.error).toHaveBeenCalledWith('Preview failed', 'Preview service unavailable')
  })

  it('accepts only the currently loaded revision and digest', () => {
    const accepted = plan({ status: 'accepted', revision: 1 })
    service.accept.and.returnValue(of(accepted))
    component.selectedPlan = plan()

    component.acceptSelected()

    expect(service.accept).toHaveBeenCalledWith('plan-1', {
      expectedRevision: 1,
      expectedDigest: 'digest-1',
    })
    expect(component.selectedPlan?.status).toBe('accepted')
  })

  it('keeps a draft unchanged when revision acceptance fails', () => {
    service.accept.and.returnValue(throwError(() => new HttpErrorResponse({
      status: 409,
      error: { error: 'Plan revision changed; refresh before accepting.' },
    })))
    component.selectedPlan = plan()

    component.acceptSelected()

    expect(component.saving).toBeFalse()
    expect(component.selectedPlan?.status).toBe('draft')
    expect(notification.error).toHaveBeenCalledWith('Plan was not accepted', 'Plan revision changed; refresh before accepting.')
  })

  it('creates a new revision when an accepted plan is replanned', () => {
    const accepted = plan({ status: 'accepted', revision: 2, planDigest: 'digest-2' })
    const repaired = plan({ status: 'draft', revision: 3, planDigest: 'digest-3' })
    service.replan.and.returnValue(of(repaired))
    component.selectedPlan = accepted
    component.replanOpen = true
    component.replanReason = 'External deadline moved'
    component.replanConstraints = 'Use the new hearing date'

    component.replanSelected()

    expect(service.replan).toHaveBeenCalledWith('plan-1', jasmine.objectContaining({
      expectedRevision: 2,
      expectedDigest: 'digest-2',
      reason: 'External deadline moved',
      trigger: 'owner_requested',
      nodes: jasmine.arrayContaining([
        jasmine.objectContaining({ type: 'constraint', title: 'Use the new hearing date' }),
      ]),
    }))
    expect(component.selectedPlan?.revision).toBe(3)
    expect(component.replanOpen).toBeFalse()
  })

  it('keeps the accepted plan and replan form available when revision creation fails', () => {
    service.replan.and.returnValue(throwError(() => new HttpErrorResponse({
      status: 503,
      error: { error: 'Revision service unavailable' },
    })))
    component.selectedPlan = plan({ status: 'accepted', revision: 2, planDigest: 'digest-2' })
    component.replanOpen = true
    component.replanReason = 'Deadline moved'

    component.replanSelected()

    expect(component.saving).toBeFalse()
    expect(component.replanOpen).toBeTrue()
    expect(component.selectedPlan?.revision).toBe(2)
    expect(notification.error).toHaveBeenCalledWith('Replan failed', 'Revision service unavailable')
  })

  it('keeps the selected plan summary when detail loading fails', () => {
    service.get.and.returnValue(throwError(() => new HttpErrorResponse({
      status: 503,
      error: { error: 'Plan detail unavailable' },
    })))

    component.selectPlan(plan())

    expect(component.detailLoading).toBeFalse()
    expect(component.selectedPlan?.title).toBe('Evidence response')
    expect(notification.error).toHaveBeenCalledWith('Plan detail unavailable', 'Plan detail unavailable')
  })

  it('keeps API failure distinct from an empty plan list', () => {
    service.list.and.returnValue(throwError(() => new HttpErrorResponse({
      status: 503,
      error: { error: 'plan graph store unavailable' },
    })))

    component.refresh()

    expect(component.loading).toBeFalse()
    expect(component.errorMessage).toBe('plan graph store unavailable')
    expect(component.plans).toEqual([])
  })
})

describe('PlanCoordinationComponent rendering', () => {
  let fixture: ComponentFixture<PlanCoordinationComponent>
  let renderedService: jasmine.SpyObj<PlanGraphService>

  beforeEach(async () => {
    renderedService = jasmine.createSpyObj<PlanGraphService>('PlanGraphService', ['list', 'get', 'preview', 'accept', 'replan'])
    renderedService.list.and.returnValue(of([]))
    await TestBed.configureTestingModule({
      declarations: [PlanCoordinationComponent],
      imports: [
        CommonModule,
        FormsModule,
        BrowserAnimationsModule,
        ControlRoomModule,
        NzButtonModule,
        NzDrawerModule,
        NzIconModule,
        NzSpinModule,
      ],
      providers: [
        {
          provide: NZ_ICONS,
          useValue: [
            AuditOutline,
            BranchesOutline,
            CheckCircleOutline,
            CloseOutline,
            DownOutline,
            LinkOutline,
            NodeIndexOutline,
            PauseCircleOutline,
            ProfileOutline,
            ReloadOutline,
            RightOutline,
            SafetyCertificateOutline,
            UpOutline,
            WarningOutline,
          ],
        },
        { provide: PlanGraphService, useValue: renderedService },
        { provide: ActivatedRoute, useValue: { snapshot: { queryParamMap: { get: () => null } } } },
        { provide: NzNotificationService, useValue: jasmine.createSpyObj('NzNotificationService', ['success', 'error']) },
      ],
    }).compileComponents()
    fixture = TestBed.createComponent(PlanCoordinationComponent)
  })

  it('renders a calm immutable empty state and preview form', () => {
    fixture.detectChanges()

    const text = (fixture.nativeElement as HTMLElement).textContent || ''
    expect(text).toContain('No coordination plan exists yet')
    expect(text).toContain('Create plan preview')
    expect(renderedService.list).toHaveBeenCalledWith()
  })

  it('keeps schedule controls in the shared per-module disclosure', () => {
    fixture.detectChanges()
    const root = fixture.nativeElement as HTMLElement
    const preferences = TestBed.inject(ModuleViewPreferencesService)

    expect(root.querySelector('#schedule-resources.hai-progressive-section')).toBeNull()
    preferences.setMode('plan-coordination', 'advanced')
    fixture.detectChanges()
    const section = root.querySelector('#schedule-resources.hai-progressive-section')
    expect(section?.classList.contains('hai-progressive-section--advanced')).toBeTrue()
    expect(root.querySelectorAll('.preview-form label').length).toBe(3)
    expect(root.querySelector('.preview-options')).toBeNull()

    section?.querySelector<HTMLButtonElement>('.hai-progressive-section__summary')?.click()

    expect(preferences.get('plan-coordination').openSections['schedule-resources']).toBeTrue()
    expect(preferences.get('control-center').openSections['schedule-resources']).toBeUndefined()
    preferences.reset('plan-coordination')
  })
})

describe('PlanCoordinationComponent real API actions', () => {
  let fixture: ComponentFixture<PlanCoordinationComponent>
  let http: HttpTestingController
  let notification: jasmine.SpyObj<NzNotificationService>

  const transportPlan = (status: 'draft' | 'accepted', revision: number, digest: string): any => ({
    id: 'plan-1',
    title: 'Evidence response',
    status,
    revision,
    digest,
    nodes: [
      {
        id: 'objective-1',
        type: 'objective',
        title: 'Prepare a verified response',
        owner: 'hai',
        status: 'ready',
        estimatedMinutes: 45,
        estimatedCostEur: 22,
        deadline: '2030-06-10T09:00:00.000Z',
        risk: 'medium',
        approvalState: 'not_required',
        bindings: { pursuitId: 'pursuit-1', workflowId: 'workflow-1' },
      },
      {
        id: 'criterion-1',
        type: 'success_criterion',
        title: 'Sources are linked',
        owner: 'hai',
        status: 'planned',
        estimatedMinutes: 0,
        estimatedCostEur: 0,
        risk: 'low',
        approvalState: 'not_required',
        bindings: {},
      },
    ],
    edges: [{ id: 'edge-1', from: 'objective-1', to: 'criterion-1', type: 'finish_to_start' }],
    createdBy: 'robert',
    createdAt: '2030-06-01T09:00:00.000Z',
    acceptedAt: status === 'accepted' ? '2030-06-02T09:00:00.000Z' : undefined,
    canExecute: false,
  })

  function findButton(text: string): HTMLButtonElement {
    const root = fixture.nativeElement as HTMLElement
    const button = [...root.querySelectorAll<HTMLButtonElement>('button')]
      .find((candidate) => candidate.textContent?.includes(text))
    if (!button) {
      const component = fixture.componentInstance
      const rendered = (fixture.nativeElement as HTMLElement).textContent?.trim().replace(/\s+/g, ' ')
      throw new Error(`Expected button containing "${text}" to be rendered (loading=${component.loading}, error="${component.errorMessage}", selected=${component.selectedPlan?.id || 'none'}, text="${rendered}")`)
    }
    return button
  }

  beforeEach(async () => {
    notification = jasmine.createSpyObj<NzNotificationService>('NzNotificationService', ['success', 'error'])
    await TestBed.configureTestingModule({
      declarations: [PlanCoordinationComponent],
      imports: [
        CommonModule,
        FormsModule,
        BrowserAnimationsModule,
        ControlRoomModule,
        NzButtonModule,
        NzDrawerModule,
        NzIconModule,
        NzSpinModule,
      ],
      providers: [
        provideHttpClient(withInterceptorsFromDi()),
        provideHttpClientTesting(),
        {
          provide: NZ_ICONS,
          useValue: [
            AuditOutline,
            BranchesOutline,
            CheckCircleOutline,
            CloseOutline,
            DownOutline,
            LinkOutline,
            NodeIndexOutline,
            PauseCircleOutline,
            ProfileOutline,
            ReloadOutline,
            RightOutline,
            SafetyCertificateOutline,
            UpOutline,
            WarningOutline,
          ],
        },
        { provide: ActivatedRoute, useValue: { snapshot: { queryParamMap: { get: () => null } } } },
        { provide: NzNotificationService, useValue: notification },
      ],
    }).compileComponents()
    http = TestBed.inject(HttpTestingController)
    fixture = TestBed.createComponent(PlanCoordinationComponent)
  })

  afterEach(() => {
    http.verify()
    document.body.classList.remove('hai-view-advanced')
    TestBed.inject(ModuleViewPreferencesService).reset('plan-coordination')
  })

  it('runs preview, approval, and immutable replan through the actual HTTP service', async () => {
    fixture.detectChanges()
    const listRequest = http.expectOne('/api/v1/plans')
    expect((fixture.nativeElement as HTMLElement).querySelector('.plan-loading')?.textContent).toContain('Loading authoritative plan records')
    listRequest.flush([])
    await fixture.whenStable()
    fixture.detectChanges()

    fixture.componentInstance.previewForm = {
      title: 'Evidence response',
      objective: 'Prepare a verified response',
      successCriteria: 'Sources are linked',
      deadlineAt: '2030-06-10T09:00',
      pursuitId: 'pursuit-1',
      workflowId: 'workflow-1',
      constraints: 'Draft only',
      estimatedMinutes: 45,
      estimatedCostEur: 22,
    }
    fixture.detectChanges()
    const previewButton = findButton('Create preview')
    previewButton.click()
    fixture.detectChanges()

    expect(fixture.componentInstance.saving).toBeTrue()
    expect(previewButton.disabled).toBeTrue()
    const previewRequest = http.expectOne('/api/v1/plans/preview')
    expect(previewRequest.request.method).toBe('POST')
    expect(previewRequest.request.body.title).toBe('Evidence response')
    expect(previewRequest.request.body.nodes[0]).toEqual(jasmine.objectContaining({
      estimatedMinutes: 45,
      estimatedCostEur: 22,
      bindings: { pursuitId: 'pursuit-1', workflowId: 'workflow-1' },
    }))
    expect(previewRequest.request.body.nodes[0].deadline).toBe(new Date('2030-06-10T09:00').toISOString())
    previewRequest.flush(transportPlan('draft', 1, 'digest-1'))
    await fixture.whenStable()
    fixture.detectChanges()
    expect(fixture.componentInstance.selectedPlan?.status).toBe('draft')

    const acceptButton = findButton('Accept revision 1')
    acceptButton.click()
    fixture.detectChanges()
    expect(fixture.componentInstance.saving).toBeTrue()
    expect(acceptButton.disabled).toBeTrue()
    const acceptRequest = http.expectOne('/api/v1/plans/plan-1/accept')
    expect(acceptRequest.request.body).toEqual({ expectedRevision: 1, expectedDigest: 'digest-1' })
    acceptRequest.flush(transportPlan('accepted', 1, 'digest-1'))
    await fixture.whenStable()
    fixture.detectChanges()
    expect(fixture.componentInstance.selectedPlan?.status).toBe('accepted')

    findButton('Replan').click()
    fixture.componentInstance.replanReason = 'The external deadline moved'
    fixture.componentInstance.replanDeadlineAt = '2030-06-12T09:00'
    fixture.componentInstance.replanConstraints = 'Use the revised evidence date'
    fixture.detectChanges()
    const reviseButton = findButton('Create revised plan')
    reviseButton.click()
    fixture.detectChanges()
    expect(fixture.componentInstance.saving).toBeTrue()
    expect(reviseButton.disabled).toBeTrue()
    const replanRequest = http.expectOne('/api/v1/plans/plan-1/replan')
    expect(replanRequest.request.body).toEqual(jasmine.objectContaining({
      expectedRevision: 1,
      expectedDigest: 'digest-1',
      reason: 'The external deadline moved',
      trigger: 'owner_requested',
    }))
    expect(replanRequest.request.body.nodes).toEqual(jasmine.arrayContaining([
      jasmine.objectContaining({ type: 'constraint', title: 'Use the revised evidence date', deadline: new Date('2030-06-12T09:00').toISOString() }),
    ]))
    replanRequest.flush(transportPlan('draft', 2, 'digest-2'))
    await fixture.whenStable()
    fixture.detectChanges()
    expect(fixture.componentInstance.saving).toBeFalse()
    expect(fixture.componentInstance.selectedPlan?.revision).toBe(2)
    expect(fixture.componentInstance.replanOpen).toBeFalse()
  })

  it('shows list loading and recovery after the plan API fails, then retries the real request', async () => {
    fixture.detectChanges()
    const listRequest = http.expectOne('/api/v1/plans')
    expect((fixture.nativeElement as HTMLElement).querySelector('[role="status"]')).not.toBeNull()
    listRequest.flush({ error: 'Plan store unavailable' }, { status: 503, statusText: 'Service Unavailable' })
    await fixture.whenStable()
    fixture.detectChanges()
    expect(fixture.componentInstance.errorMessage).toBe('Plan store unavailable')
    expect((fixture.nativeElement as HTMLElement).querySelector('.source-error')?.textContent).toContain('Plan store unavailable')

    findButton('Retry').click()
    fixture.detectChanges()
    const retryRequest = http.expectOne('/api/v1/plans')
    expect(fixture.componentInstance.loading).toBeTrue()
    retryRequest.flush([])
    await fixture.whenStable()
    fixture.detectChanges()
    expect(fixture.componentInstance.loading).toBeFalse()
    expect(fixture.componentInstance.errorMessage).toBe('')
  })

  it('shows preview API failure and re-enables the actual form for recovery', async () => {
    fixture.detectChanges()
    http.expectOne('/api/v1/plans').flush([])
    await fixture.whenStable()
    fixture.detectChanges()
    fixture.componentInstance.previewForm = {
      title: 'Evidence response',
      objective: 'Prepare a verified response',
      successCriteria: 'Sources are linked',
      deadlineAt: '',
      pursuitId: '',
      workflowId: '',
      constraints: '',
    }
    fixture.detectChanges()
    const submit = findButton('Create preview')
    submit.click()
    fixture.detectChanges()
    const request = http.expectOne('/api/v1/plans/preview')
    expect(submit.disabled).toBeTrue()
    request.flush({ error: 'Preview engine unavailable' }, { status: 503, statusText: 'Service Unavailable' })
    await fixture.whenStable()
    fixture.detectChanges()

    expect(fixture.componentInstance.saving).toBeFalse()
    expect(findButton('Create preview').disabled).toBeFalse()
    expect(notification.error).toHaveBeenCalledWith('Preview failed', 'Preview engine unavailable')
    expect((fixture.nativeElement as HTMLElement).querySelector('.empty-layout')).not.toBeNull()
  })
})
