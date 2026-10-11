import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing'
import { TestBed } from '@angular/core/testing'
import { AgentTeamContract } from '../models/agent-teams.model'
import { AgentTeamsService } from './agent-teams.service'
import { provideHttpClient, withInterceptorsFromDi } from '@angular/common/http';

describe('AgentTeamsService', () => {
  let service: AgentTeamsService
  let http: HttpTestingController

  beforeEach(() => {
    TestBed.configureTestingModule({ imports: [], providers: [provideHttpClient(withInterceptorsFromDi()), provideHttpClientTesting()] })
    service = TestBed.inject(AgentTeamsService)
    http = TestBed.inject(HttpTestingController)
  })

  afterEach(() => http.verify())

  it('lists only server-declared advisory team contracts', () => {
    let result: AgentTeamContract[] = []
    service.list().subscribe((teams) => result = teams)
    http.expectOne('/api/v1/framework-registry/teams').flush({ teams: [team()] })
    expect(result.length).toBe(1)
    expect(result[0].advisoryOnly).toBeTrue()
  })

  it('rejects a response that claims execution authority', () => {
    let error: Error | undefined
    service.list().subscribe({ error: (value) => error = value })
    http.expectOne('/api/v1/framework-registry/teams').flush({ teams: [{ ...team(), grantsExecutionAuthority: true }] })
    expect(error?.message).toContain('unsafe')
  })

  it('uses guided server-owned decision and acknowledgment routes', () => {
    service.createDecision('team/a', '1.0.0', {
      senderMembershipId: 'sender', recipientMembershipId: 'recipient',
      correlationId: '00000000-0000-4000-8000-000000000001', idempotencyKey: '00000000-0000-4000-8000-000000000002',
      issue: 'Issue', position: 'support', recommendation: 'Proceed to review', evidenceRefs: ['source:1'],
      requiresAcknowledgment: true, expiresInMinutes: 60,
    }).subscribe()
    const decision = http.expectOne('/api/v1/framework-registry/teams/team%2Fa/versions/1.0.0/decision-messages')
    expect(decision.request.method).toBe('POST')
    decision.flush({})

    service.acknowledge('team/a', '1.0.0', 'message/a', {
      status: 'accepted', reason: 'Reviewed', retryAfterMinutes: 0,
      idempotencyKey: '00000000-0000-4000-8000-000000000003',
    }).subscribe()
    const acknowledgment = http.expectOne('/api/v1/framework-registry/teams/team%2Fa/versions/1.0.0/messages/message%2Fa/acknowledgments/guided')
    expect(acknowledgment.request.method).toBe('POST')
    acknowledgment.flush({})
  })

  it('loads a combined decision overview without an unsafe attention contract', () => {
    let result: any
    service.decisionOverview('team/a', '1.0.0').subscribe((overview) => result = overview)
    const request = http.expectOne('/api/v1/framework-registry/teams/team%2Fa/versions/1.0.0/decision-overview')
    expect(request.request.method).toBe('GET')
    request.flush({
      generatedAt: '2026-08-11T00:00:00Z', messages: [{ id: 'message-1' }],
      attention: [{ messageId: 'message-1', advisoryOnly: true, grantsExecutionAuthority: false, executionAuthorizationRequired: true }],
    })
    expect(result.messages.length).toBe(1)
    expect(result.attention.length).toBe(1)
  })

  function team(): AgentTeamContract {
    return {
      id: 'team-1', key: 'review', version: '1.0.0', revision: 1, status: 'draft', name: 'Review team', purpose: 'Review evidence',
      authorityCeiling: 1, riskCeiling: 'low', maximumDelegatedAuthority: 1, maximumDelegatedRisk: 'low',
      advisoryOnly: true, grantsExecutionAuthority: false, executionAuthorizationRequired: true,
      roles: [], capabilities: [], members: [],
      consensus: { mode: 'majority', decisionPayloadSchema: 'hai.agent-team.decision.v1', quorum: 2, minimumSupport: 2, allowAbstention: true, requireEvidence: true, conflictEscalationRequired: true, tieOutcome: 'escalated' },
      evidenceRefs: ['source:1'], provenance: { source: 'operator', authoredBy: 'owner', registeredBy: 'owner', registeredAt: '2026-08-09T00:00:00Z', evidenceDigest: 'digest' },
      contractDigest: 'contract', createdAt: '2026-08-09T00:00:00Z', updatedAt: '2026-08-09T00:00:00Z',
    }
  }
})
