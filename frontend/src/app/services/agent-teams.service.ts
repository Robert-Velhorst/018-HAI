import { HttpClient, HttpParams } from '@angular/common/http'
import { Injectable } from '@angular/core'
import { Observable, map } from 'rxjs'
import {
  AddTeamMemberRequest,
  AgentCoordinationMessage,

  AgentTeamDecisionOverview,
  AgentTeamAcknowledgment,
  AgentTeamAttention,
  AgentTeamConsensusOutcome,
  AgentTeamContract,
  AgentTeamLifecycleEvent,
  ChangeTeamMembershipRequest,
  CreateGuidedAgentTeamRequest,
  CreateTeamAcknowledgmentRequest,
  CreateTeamDecisionMessageRequest,
  RecordTeamConsensusRequest,
  TeamTransitionRequest,
} from '../models/agent-teams.model'

@Injectable({ providedIn: 'root' })
export class AgentTeamsService {
  private readonly baseUrl = '/api/v1/framework-registry/teams'

  constructor(private http: HttpClient) {}

  list(): Observable<AgentTeamContract[]> {
    return this.http.get<{ teams?: AgentTeamContract[] }>(this.baseUrl).pipe(
      map((response) => (response.teams || []).map((team) => this.assertAdvisory(team)))
    )
  }

  get(id: string, version: string): Observable<AgentTeamContract> {
    return this.http.get<AgentTeamContract>(this.teamUrl(id, version)).pipe(map((team) => this.assertAdvisory(team)))
  }

  createGuided(request: CreateGuidedAgentTeamRequest): Observable<AgentTeamContract> {
    return this.http.post<AgentTeamContract>(`${this.baseUrl}/guided`, request).pipe(map((team) => this.assertAdvisory(team)))
  }

  transition(id: string, version: string, action: 'activate' | 'suspend' | 'retire' | 'revoke', request: TeamTransitionRequest): Observable<AgentTeamContract> {
    return this.http.post<AgentTeamContract>(`${this.teamUrl(id, version)}/${action}`, request).pipe(map((team) => this.assertAdvisory(team)))
  }

  addMember(id: string, version: string, request: AddTeamMemberRequest): Observable<AgentTeamContract> {
    return this.http.post<AgentTeamContract>(`${this.teamUrl(id, version)}/members`, request).pipe(map((team) => this.assertAdvisory(team)))
  }

  changeMembership(id: string, version: string, memberId: string, request: ChangeTeamMembershipRequest): Observable<AgentTeamContract> {
    return this.http.post<AgentTeamContract>(`${this.teamUrl(id, version)}/members/${encodeURIComponent(memberId)}/status`, request).pipe(map((team) => this.assertAdvisory(team)))
  }

  messages(id: string, version: string, correlationId = ''): Observable<AgentCoordinationMessage[]> {
    const params = correlationId ? new HttpParams().set('correlationId', correlationId) : undefined
    return this.http.get<{ messages?: AgentCoordinationMessage[] }>(`${this.teamUrl(id, version)}/messages`, { params }).pipe(map((response) => response.messages || []))
  }

  createDecision(id: string, version: string, request: CreateTeamDecisionMessageRequest): Observable<AgentCoordinationMessage> {
    return this.http.post<AgentCoordinationMessage>(`${this.teamUrl(id, version)}/decision-messages`, request)
  }

  attention(id: string, version: string): Observable<AgentTeamAttention[]> {
    return this.http.get<{ messages?: AgentTeamAttention[] }>(`${this.teamUrl(id, version)}/message-attention`).pipe(
      map((response) => (response.messages || []).map((item) => this.assertAdvisory(item)))
    )
  }

  decisionOverview(id: string, version: string): Observable<AgentTeamDecisionOverview> {
    return this.http.get<AgentTeamDecisionOverview>(`${this.teamUrl(id, version)}/decision-overview`).pipe(
      map((overview) => ({
        ...overview,
        messages: overview.messages || [],
        attention: (overview.attention || []).map((item) => this.assertAdvisory(item)),
      }))
    )
  }

  acknowledge(id: string, version: string, messageId: string, request: CreateTeamAcknowledgmentRequest): Observable<AgentTeamAcknowledgment> {
    return this.http.post<AgentTeamAcknowledgment>(`${this.teamUrl(id, version)}/messages/${encodeURIComponent(messageId)}/acknowledgments/guided`, request)
  }

  outcomes(id: string, version: string): Observable<AgentTeamConsensusOutcome[]> {
    return this.http.get<{ outcomes?: AgentTeamConsensusOutcome[] }>(`${this.teamUrl(id, version)}/consensus`).pipe(
      map((response) => (response.outcomes || []).map((outcome) => this.assertAdvisory(outcome)))
    )
  }

  recordConsensus(id: string, version: string, request: RecordTeamConsensusRequest): Observable<AgentTeamConsensusOutcome> {
    return this.http.post<AgentTeamConsensusOutcome>(`${this.teamUrl(id, version)}/consensus`, request).pipe(map((outcome) => this.assertAdvisory(outcome)))
  }

  events(id: string, version: string): Observable<AgentTeamLifecycleEvent[]> {
    return this.http.get<{ events?: AgentTeamLifecycleEvent[] }>(`${this.teamUrl(id, version)}/events`).pipe(map((response) => response.events || []))
  }

  private teamUrl(id: string, version: string): string {
    return `${this.baseUrl}/${encodeURIComponent(id)}/versions/${encodeURIComponent(version)}`
  }

  private assertAdvisory<T extends { advisoryOnly: boolean; grantsExecutionAuthority: boolean; executionAuthorizationRequired: boolean }>(value: T): T {
    if (!value || value.advisoryOnly !== true || value.grantsExecutionAuthority !== false || value.executionAuthorizationRequired !== true) {
      throw new Error('Agent team authority contract is unsafe or incomplete')
    }
    return value
  }
}
