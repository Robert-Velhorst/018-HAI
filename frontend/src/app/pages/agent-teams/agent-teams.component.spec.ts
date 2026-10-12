import { AgentCoordinationMessage, AgentTeamAttention, AgentTeamContract } from '../../models/agent-teams.model'
import { CommonModule } from '@angular/common'
import { HttpErrorResponse } from '@angular/common/http'
import { ComponentFixture, TestBed } from '@angular/core/testing'
import { FormsModule } from '@angular/forms'
import { Router } from '@angular/router'
import { ControlRoomModule } from '../../control-room/control-room.module'
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service'
import { NzButtonModule } from 'ng-zorro-antd/button'
import { NzDrawerModule } from 'ng-zorro-antd/drawer'
import { NzIconModule } from 'ng-zorro-antd/icon'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import { NzSpinModule } from 'ng-zorro-antd/spin'
import { of, throwError } from 'rxjs'
import { AgentTeamsService } from '../../services/agent-teams.service'
import { AuthSessionService } from '../../services/auth-session.service'
import { AgentTeamsComponent } from './agent-teams.component'

describe('AgentTeamsComponent', () => {
  function dependencies() {
    const teamsService = { attention: jasmine.createSpy('attention').and.returnValue(of([])) }
    const preferences = {
      get: jasmine.createSpy('get').and.returnValue({ openSections: {} }),
      setMode: jasmine.createSpy('setMode'),
    }
    const notification = jasmine.createSpyObj('NzNotificationService', ['success', 'error'])
    const router = jasmine.createSpyObj('Router', ['navigate'])
    return { teamsService, preferences, notification, router }
  }

  function component(): AgentTeamsComponent {
    const deps = dependencies()
    return new AgentTeamsComponent(deps.teamsService as never, {} as never, deps.preferences as never, deps.notification, deps.router)
  }

  function selectedTeam(): AgentTeamContract {
    return {
      id: 'team-1', key: 'review-team', version: '1.0.0', name: 'Review team', purpose: 'Review evidence.',
      status: 'active', revision: 1, authorityCeiling: 1, riskCeiling: 'low',
      maximumDelegatedAuthority: 1, maximumDelegatedRisk: 'low', advisoryOnly: true,
      grantsExecutionAuthority: false, executionAuthorizationRequired: true,
      capabilities: [], roles: [], members: [], evidenceRefs: ['source:test'],
      consensus: {
        mode: 'majority', decisionPayloadSchema: 'hai.agent-team.decision.v1', quorum: 2,
        minimumSupport: 2, allowAbstention: true, requireEvidence: true,
        conflictEscalationRequired: true, tieOutcome: 'escalated',
      },
      provenance: {
        source: 'test', authoredBy: 'test', registeredBy: 'test', registeredAt: '', evidenceDigest: 'digest',
      },
      contractDigest: 'digest', createdAt: '', updatedAt: '',
    }
  }

  function decisionMessage(id: string, senderId: string): AgentCoordinationMessage {
    return {
      id, correlationId: 'correlation-1', idempotencyKey: id, schemaVersion: '1', type: 'decision',
      sender: { id: senderId, role: 'reviewer', authorityCeiling: 1 },
      recipient: { id: 'recipient-1', role: 'coordinator', authorityCeiling: 1 },
      confidentiality: 'internal', authorityLevel: 1,
      payload: { schema: 'hai.agent-team.decision.v1', subject: 'Accept the result?', data: { position: 'support' } },
      payloadDigest: 'digest', evidenceRefs: ['source:test'], requiresAck: true,
      createdAt: '', expiresAt: '', provenanceSummary: 'test',
    }
  }

  function attentionItem(overrides: Partial<AgentTeamAttention> = {}): AgentTeamAttention {
    return {
      messageId: 'message-1',
      correlationId: 'correlation-1',
      recipientId: 'recipient-1',
      subject: 'Review recommendation',
      requiresAcknowledgment: true,
      state: 'waiting',
      reason: 'Human review required',
      expiresAt: '2026-09-25T10:00:00Z',
      humanReviewRequired: true,
      advisoryOnly: true,
      grantsExecutionAuthority: false,
      executionAuthorizationRequired: true,
      ...overrides,
    }
  }

  it('requires distinct voting members before promoting consensus evaluation', () => {
    const fixture = component()
    fixture.selected = selectedTeam()
    fixture.consensusForm = { correlationId: 'correlation-1', issue: 'Accept the result?' }
    const message = decisionMessage('message-1', 'member-1')

    fixture.messages = [message, { ...message, id: 'message-2' }]
    expect(fixture.consensusReady).toBeFalse()

    fixture.messages = [message, decisionMessage('message-3', 'member-2')]
    expect(fixture.consensusReady).toBeTrue()
  })

  it('does not promote consensus after an outcome already exists', () => {
    const fixture = component()
    fixture.selected = selectedTeam()
    fixture.consensusForm = { correlationId: 'correlation-1', issue: 'Accept the result?' }
    fixture.messages = [
      decisionMessage('message-1', 'member-1'),
      decisionMessage('message-2', 'member-2'),
    ]
    fixture.outcomes = [{ correlationId: 'correlation-1' } as never]

    expect(fixture.consensusReady).toBeFalse()
  })

  it('reports an attention refresh failure without hiding a completed decision', () => {
    const fixture = component()
    const notification = { error: jasmine.createSpy('error') }
    fixture.selected = selectedTeam()
    ;(fixture as any).notification = notification
    ;(fixture as any).teamsService = {
      attention: () => throwError(() => new HttpErrorResponse({ error: { error: 'refresh failed' } })),
    }

    ;(fixture as any).loadAttention()

    expect(notification.error).toHaveBeenCalledWith('Attention queue unavailable', 'refresh failed')
    expect(fixture.attentionError).toBe('refresh failed')
    expect(fixture.attentionLoading).toBeFalse()
  })

  it('loads approval attention as a Basic next action without opening decision records', () => {
    const deps = dependencies()
    const fixture = new AgentTeamsComponent(deps.teamsService as never, {} as never, deps.preferences as never, deps.notification, deps.router)
    const team = selectedTeam()
    fixture.session = { permissions: { canOperate: true, canAdminister: false } } as never
    const item = attentionItem()
    deps.teamsService.attention.and.returnValue(of([item]))
    fixture.selectTeam(team)

    fixture.runNextAction()

    expect(deps.teamsService.attention).toHaveBeenCalledOnceWith(team.id, team.version)
    expect(fixture.attention).toEqual([item])
    expect(fixture.nextAction.action).toBe('decisions')
    expect(deps.preferences.setMode).not.toHaveBeenCalled()
    expect(deps.router.navigate).not.toHaveBeenCalled()
  })

  describe('rendered attention queue', () => {
    const preferenceKey = 'hai.module-view.v1.agent-teams'
    let fixture: ComponentFixture<AgentTeamsComponent>
    let teamsService: jasmine.SpyObj<AgentTeamsService>
    let preferences: ModuleViewPreferencesService
    let router: jasmine.SpyObj<Router>

    const pendingItems = Array.from({ length: 7 }, (_, index) => attentionItem({
      messageId: `message-${index + 1}`,
      subject: `Review recommendation ${index + 1}`,
    }))

    beforeEach(async () => {
      localStorage.removeItem(preferenceKey)
      teamsService = jasmine.createSpyObj<AgentTeamsService>('AgentTeamsService', ['list', 'attention', 'decisionOverview'])
      teamsService.list.and.returnValue(of([selectedTeam()]))
      teamsService.attention.and.returnValue(of(pendingItems))
      teamsService.decisionOverview.and.returnValue(of({
        generatedAt: '2026-09-24T10:00:00Z',
        messages: [],
        attention: pendingItems,
      }))
      router = jasmine.createSpyObj<Router>('Router', ['navigate'])
      router.navigate.and.returnValue(Promise.resolve(true))

      await TestBed.configureTestingModule({
        declarations: [AgentTeamsComponent],
        imports: [
          CommonModule,
          FormsModule,
          ControlRoomModule,
          NzButtonModule,
          NzDrawerModule,
          NzIconModule,
          NzSpinModule,
        ],
        providers: [
          { provide: AgentTeamsService, useValue: teamsService },
          { provide: AuthSessionService, useValue: { session: () => of({
            authenticated: true,
            subject: 'operator-test',
            role: 'operator',
            permissions: { canRead: true, canOperate: true, canApprove: true, canAdminister: false },
          }) } },
          { provide: NzNotificationService, useValue: jasmine.createSpyObj('NzNotificationService', ['success', 'error']) },
          { provide: Router, useValue: router },
          ModuleViewPreferencesService,
        ],
      }).compileComponents()

      preferences = TestBed.inject(ModuleViewPreferencesService)
      fixture = TestBed.createComponent(AgentTeamsComponent)
      fixture.detectChanges()
    })

    afterEach(() => {
      fixture?.destroy()
      localStorage.removeItem(preferenceKey)
      document.body.classList.remove('hai-view-advanced')
    })

    it('opens all pending items from Basic view and keeps their existing acknowledgment actions', () => {
      const page: HTMLElement = fixture.nativeElement
      const basicQueue = page.querySelector('#team-attention-queue')!
      const viewAll = basicQueue.querySelector('button.attention-view-all') as HTMLButtonElement

      expect(basicQueue.querySelectorAll('.attention-list article').length).toBe(5)
      expect(viewAll.textContent).toContain('View all 7 pending items')
      expect(viewAll.getAttribute('aria-label')).toContain('View all 7 pending items')
      expect(viewAll.getAttribute('aria-controls')).toBe('agent-team-decisions')
      expect(viewAll.tabIndex).toBe(0)

      viewAll.click()
      fixture.detectChanges()

      const decisions = page.querySelector('#agent-team-decisions')!
      expect(decisions.querySelector('.hai-progressive-section__summary')?.getAttribute('aria-expanded')).toBe('true')
      expect(decisions.querySelectorAll('.decision-attention-list article').length).toBe(7)
      expect(decisions.querySelectorAll('.decision-attention-list .record-actions button').length).toBe(21)
      expect(preferences.get('agent-teams').mode).toBe('advanced')
      expect(teamsService.decisionOverview).toHaveBeenCalledOnceWith('team-1', '1.0.0')
      expect(router.navigate).toHaveBeenCalledWith([], { queryParams: { mode: 'advanced' }, queryParamsHandling: 'merge' })
    })

    it('keeps the full queue visible without granting acknowledgment actions to a read-only role', () => {
      fixture.componentInstance.session = {
        authenticated: true,
        subject: 'viewer-test',
        role: 'viewer',
        permissions: { canRead: true, canOperate: false, canApprove: false, canAdminister: false },
      } as never
      fixture.detectChanges()

      const page: HTMLElement = fixture.nativeElement
      const viewAll = page.querySelector('button.attention-view-all') as HTMLButtonElement
      viewAll.click()
      fixture.detectChanges()

      const decisions = page.querySelector('#agent-team-decisions')!
      expect(decisions.querySelectorAll('.decision-attention-list article').length).toBe(7)
      expect(decisions.querySelectorAll('.decision-attention-list .record-actions button').length).toBe(0)
      expect(decisions.textContent).toContain('Your current role cannot acknowledge this message.')
    })
  })
})
