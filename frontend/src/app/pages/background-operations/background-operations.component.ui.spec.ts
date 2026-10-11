import { CommonModule } from '@angular/common'
import { HttpErrorResponse } from '@angular/common/http'
import { ComponentFixture, TestBed } from '@angular/core/testing'
import { ActivatedRoute, Router } from '@angular/router'
import { NoopAnimationsModule } from '@angular/platform-browser/animations'
import { By } from '@angular/platform-browser'
import { NzButtonModule } from 'ng-zorro-antd/button'
import { NzDrawerModule } from 'ng-zorro-antd/drawer'
import { NzEmptyModule } from 'ng-zorro-antd/empty'
import { NzIconModule } from 'ng-zorro-antd/icon'
import { NzModalModule } from 'ng-zorro-antd/modal'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import { NzTableModule } from 'ng-zorro-antd/table'
import { NzTagModule } from 'ng-zorro-antd/tag'
import { NzTimelineModule } from 'ng-zorro-antd/timeline'
import { of, Subject, throwError } from 'rxjs'
import { ControlRoomModule } from '../../control-room/control-room.module'
import { HaiProgressiveSectionComponent } from '../../control-room/progressive-section.component'
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service'
import {
  IAccountFeed,
  IBackgroundRunReport,
  IOperation,
  IOperationsDashboard,
  IOperationsOverview,
} from '../../models/background-operations.model.interface'
import { BackgroundOperationsService } from '../../services/background-operations.service'
import { BackgroundOperationsComponent } from './background-operations.component'

describe('BackgroundOperationsComponent rendered states', () => {
  const moduleId = 'background-operations'
  const activeFeed: IAccountFeed = {
    name: 'Connected board',
    provider: 'trello',
    accountLabel: 'Owner account',
    sourceType: 'trello',
    enabled: true,
  }

  let fixture: ComponentFixture<BackgroundOperationsComponent>
  let service: jasmine.SpyObj<BackgroundOperationsService>
  let notification: jasmine.SpyObj<NzNotificationService>

  beforeEach(async () => {
    service = jasmine.createSpyObj<BackgroundOperationsService>('BackgroundOperationsService', [
      'overview', 'feeds', 'events', 'get', 'approvalPreview', 'approve', 'run', 'runBackground',
    ])
    service.overview.and.returnValue(of({ dashboard: dashboard(), operations: [] }))
    service.feeds.and.returnValue(of({ feeds: [activeFeed] }))
    service.events.and.returnValue(of({ events: [] }))
    service.get.and.returnValue(of(operation('selected', 'ready')))
    service.approve.and.returnValue(of(operation('approval', 'approved')))
    service.approvalPreview.and.returnValue(of({
      operation: operation('source-approval', 'awaiting_approval'),
      revision: { operationId: 'source-approval', version: 3, revisionDigest: 'a'.repeat(64) },
    }))
    service.run.and.returnValue(of({ operation: operation('worker', 'completed'), verified: true, failed: false }))
    service.runBackground.and.returnValue(of(report()))
    notification = jasmine.createSpyObj<NzNotificationService>('NzNotificationService', ['success', 'info', 'warning', 'error'])

    await TestBed.configureTestingModule({
      declarations: [BackgroundOperationsComponent],
      imports: [
        CommonModule,
        NoopAnimationsModule,
        NzButtonModule,
        NzDrawerModule,
        NzEmptyModule,
        NzIconModule,
        NzModalModule,
        NzTableModule,
        NzTagModule,
        NzTimelineModule,
        ControlRoomModule,
      ],
      providers: [
        { provide: BackgroundOperationsService, useValue: service },
        { provide: NzNotificationService, useValue: notification },
        { provide: ActivatedRoute, useValue: { snapshot: { queryParamMap: { get: () => null } } } },
        { provide: Router, useValue: jasmine.createSpyObj('Router', ['navigate']) },
      ],
    }).compileComponents()

    TestBed.inject(ModuleViewPreferencesService).reset(moduleId)
  })

  afterEach(() => {
    fixture?.destroy()
    TestBed.inject(ModuleViewPreferencesService).reset(moduleId)
    document.body.classList.remove('hai-view-advanced')
  })

  function createFixture(): ComponentFixture<BackgroundOperationsComponent> {
    fixture = TestBed.createComponent(BackgroundOperationsComponent)
    fixture.detectChanges()
    return fixture
  }

  it('renders retained 409 evidence in Basic and withholds Run after stale refresh', () => {
    const item = operation('partial', 'ready')
    service.overview.and.returnValue(of({ dashboard: dashboard(), operations: [item] }))
    service.run.and.returnValue(throwError(() => new HttpErrorResponse({ status: 409, error: {
      operationId: item.id, operation: { id: item.id, status: 'running', verificationStatus: 'pending' },
      verified: false, failed: false, reconciliationRequired: true, outcomeRecorded: false,
      receipt: { output: { artifactHash: 'receipt-hash', boundedOutput: 'api_key=synthetic-secret' } },
    } })))
    const rendered = createFixture()
    rendered.componentInstance.run(item)
    rendered.detectChanges()
    const root = rendered.nativeElement as HTMLElement
    expect(root.textContent).toContain('receipt-hash')
    expect(root.textContent).toContain('Reconciliation required')
    expect(root.textContent).not.toContain('synthetic-secret')
    expect(Array.from(root.querySelectorAll('button')).some((button) => /Run safe work|Retry safely/.test(button.textContent || ''))).toBeFalse()
    expect(Array.from(root.querySelectorAll('button')).some((button) => button.textContent?.includes('Refresh status'))).toBeTrue()
  })

  it('shows a real loading state without rendering an empty result prematurely', () => {
    service.overview.and.returnValue(new Subject<IOperationsOverview>())
    service.feeds.and.returnValue(new Subject<{ feeds: IAccountFeed[] }>())

    const rendered = createFixture()

    expect(rendered.componentInstance.loading).toBeTrue()
    expect(rendered.nativeElement.querySelector('[role="status"]')?.textContent).toContain('Loading current operations')
    expect(rendered.nativeElement.querySelector('.bg-ops__empty')).toBeNull()
  })

  it('shows the honest empty state and runs the single primary pass action', () => {
    const rendered = createFixture()
    const button = rendered.nativeElement.querySelector('.bg-ops__run-button') as HTMLButtonElement

    expect(rendered.nativeElement.textContent).toContain('Nothing needs attention right now')
    expect(button.disabled).toBeFalse()
    expect(rendered.nativeElement.querySelectorAll('.bg-ops__run-button').length).toBe(1)

    button.click()
    rendered.detectChanges()

    expect(service.runBackground).toHaveBeenCalledTimes(1)
    expect(rendered.nativeElement.textContent).toContain('Pass complete')
    expect(rendered.nativeElement.textContent).toContain('1 feeds checked')
  })

  it('keeps read failure distinct from empty and offers an inline retry without a generic toast', () => {
    service.overview.and.returnValues(
      throwError(() => new HttpErrorResponse({ status: 503 })),
      of({ dashboard: dashboard(), operations: [] }),
    )

    const rendered = createFixture()

    expect(rendered.nativeElement.querySelector('.bg-ops__load-error')?.textContent).toContain('unavailable')
    expect(rendered.nativeElement.querySelector('.bg-ops__empty')).toBeNull()
    expect(notification.error).not.toHaveBeenCalled()

    const retry = rendered.nativeElement.querySelector('.bg-ops__load-error button') as HTMLButtonElement
    retry.click()
    rendered.detectChanges()

    expect(service.overview).toHaveBeenCalledTimes(2)
    expect(rendered.componentInstance.loadError).toBe('')
    expect(rendered.nativeElement.textContent).toContain('Nothing needs attention right now')
  })

  it('renders operations and approval action, then reflects the refreshed status', () => {
    const awaitingApproval = operation('approval', 'awaiting_approval')
    const approved = operation('approval', 'approved')
    service.overview.and.returnValues(
      of({ dashboard: dashboard({ needsRobert: 1 }), operations: [awaitingApproval] }),
      of({ dashboard: dashboard(), operations: [approved] }),
    )
    service.approve.and.returnValue(of(approved))

    const rendered = createFixture()

    expect(rendered.nativeElement.textContent).toContain('Waiting for your approval before work can continue.')
    const buttons = rendered.nativeElement.querySelectorAll('button') as NodeListOf<HTMLButtonElement>
    const approveButton = Array.from(buttons)
      .find((button) => button.textContent?.trim() === 'Approve') as HTMLButtonElement
    expect(approveButton).toBeDefined()
    approveButton.click()
    rendered.detectChanges()

    expect(service.approve).toHaveBeenCalledOnceWith('approval')
    expect(rendered.nativeElement.textContent).toContain('Approved')
    expect(rendered.nativeElement.querySelectorAll('button').length).toBeGreaterThan(0)
  })

  it('shows the fetched source operation and evidence before separate approval confirmation', () => {
    const source = {
      ...operation('source-approval', 'awaiting_approval'),
      version: 3,
      sourceIdentityHash: 'identity-hash-value',
      sourceRevisionHash: 'source-revision-value',
      description: 'Prepare a formal reply from this message.',
      recommendedAction: 'Draft a reply for owner review',
      evidence: JSON.stringify({ sender: 'lawyer@example.test', subject: 'Hearing documents' }),
    } as IOperation
    const preview = {
      operation: source,
      revision: { operationId: source.id, version: 3, revisionDigest: 'b'.repeat(64) },
    }
    service.overview.and.returnValue(of({ dashboard: dashboard(), operations: [source] }))
    service.approvalPreview.and.returnValue(of(preview))
    const rendered = createFixture()
    const approveButton = Array.from(rendered.nativeElement.querySelectorAll('button') as NodeListOf<HTMLButtonElement>)
      .find((button) => button.textContent?.trim() === 'Approve') as HTMLButtonElement

    approveButton.click()
    rendered.detectChanges()

    expect(service.approvalPreview).toHaveBeenCalledOnceWith(source.id)
    expect(service.approve).not.toHaveBeenCalled()
    expect(document.body.textContent).toContain('Review source-derived operation')
    expect(document.body.textContent).toContain('Draft a reply for owner review')
    expect(document.body.textContent).toContain('Prepare a formal reply from this message.')
    expect(document.body.textContent).toContain('identity-hash-value')
    expect(document.body.textContent).toContain('lawyer@example.test')

    const confirmButton = Array.from(document.body.querySelectorAll('button') as NodeListOf<HTMLButtonElement>)
      .find((button) => button.textContent?.trim() === 'Confirm approval') as HTMLButtonElement
    expect(confirmButton).toBeDefined()
    confirmButton.click()
    rendered.detectChanges()

    expect(service.approvalPreview).toHaveBeenCalledTimes(2)
    expect(service.approve).toHaveBeenCalledOnceWith(source.id, {
      expectedVersion: 3,
      revisionDigest: 'b'.repeat(64),
    })
  })

  it('opens the operation inspector from the queue and requests its audit events', () => {
    service.overview.and.returnValue(of({ dashboard: dashboard(), operations: [operation('inspect-me', 'blocked')] }))
    const rendered = createFixture()
    const inspect = rendered.nativeElement.querySelector('[aria-label="Inspect inspect-me"]') as HTMLButtonElement

    inspect.click()
    rendered.detectChanges()

    expect(rendered.componentInstance.detailVisible).toBeTrue()
    expect(rendered.componentInstance.selected?.id).toBe('inspect-me')
    expect(service.events).toHaveBeenCalledOnceWith('inspect-me')
  })

  it('uses the shared per-module Advanced disclosure and lists only enabled feeds', () => {
    service.feeds.and.returnValue(of({
      feeds: [activeFeed, { ...activeFeed, name: 'Paused feed', enabled: false }],
    }))
    const rendered = createFixture()
    const preferences = TestBed.inject(ModuleViewPreferencesService)
    const section = rendered.debugElement.query(By.directive(HaiProgressiveSectionComponent))
      .componentInstance as HaiProgressiveSectionComponent

    expect(section.visible).toBeFalse()
    preferences.setMode(moduleId, 'advanced')
    rendered.detectChanges()

    expect(section.visible).toBeTrue()
    expect(preferences.get(moduleId).mode).toBe('advanced')
    const advancedButton = rendered.nativeElement.querySelector('.hai-progressive-section__summary') as HTMLButtonElement
    advancedButton.click()
    rendered.detectChanges()

    const feeds = rendered.nativeElement.querySelector('.bg-ops__feed-line')?.textContent ?? ''
    expect(feeds).toContain('Connected board')
    expect(feeds).not.toContain('Paused feed')
    expect(preferences.get(moduleId).openSections['operation-ledger']).toBeTrue()
  })

  it('keeps a narrow operation ledger within the page width', () => {
    service.overview.and.returnValue(of({ dashboard: dashboard(), operations: [operation('a-long-operation-title-that-wraps-on-a-narrow-screen', 'blocked')] }))
    const rendered = createFixture()
    const root = rendered.nativeElement.querySelector('.bg-ops') as HTMLElement
    root.style.width = '375px'
    TestBed.inject(ModuleViewPreferencesService).setMode(moduleId, 'advanced')
    rendered.detectChanges()
    const disclosure = rendered.nativeElement.querySelector('.hai-progressive-section__summary') as HTMLButtonElement
    disclosure.click()
    rendered.detectChanges()

    expect(root.scrollWidth).toBeLessThanOrEqual(root.clientWidth)
  })
})

function dashboard(overrides: Partial<IOperationsDashboard> = {}): IOperationsDashboard {
  return {
    countsByStatus: {},
    countsByRisk: {},
    needsRobert: 0,
    doneWhileAway: 0,
    blocked: 0,
    running: 0,
    failed: 0,
    recent: [],
    ...overrides,
  }
}

function operation(id: string, status: string): IOperation {
  return {
    id,
    ownerUserId: 'owner',
    workspaceId: 'workspace',
    title: id,
    sourceType: 'manual',
    operationType: 'review',
    status,
    riskLevel: 'low',
    autonomyLevel: 'draft_only',
    ownerType: 'robert',
    currentDecision: status === 'awaiting_approval' ? 'review' : 'run_safe_local_worker',
    requiresApproval: status === 'awaiting_approval',
    verificationStatus: 'pending',
    createdAt: '2026-09-26T00:00:00Z',
    updatedAt: '2026-09-26T00:00:00Z',
    ...(status === 'blocked' ? { lastError: 'Waiting for a source response.' } : {}),
  }
}

function report(): IBackgroundRunReport {
  return {
    feedsRead: 1,
    itemsIngested: 1,
    operationsCreated: 1,
    classified: 1,
    autoExecuted: 0,
    verified: 0,
    failed: 0,
    awaitingApproval: 1,
    blocked: 0,
    drafted: 0,
    observed: 0,
  }
}
