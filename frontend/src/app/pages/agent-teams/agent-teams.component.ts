import { HttpErrorResponse } from '@angular/common/http'
import { ChangeDetectionStrategy, Component, OnInit, ViewChild } from '@angular/core'
import { forkJoin } from 'rxjs'
import { HaiProgressiveSectionComponent } from '../../control-room/progressive-section.component'
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service'
import {
  AgentCoordinationMessage,
  AgentTeamAttention,
  AgentTeamConsensusOutcome,
  AgentTeamContract,
  AgentTeamLifecycleEvent,
  AgentTeamMember,
  TeamMembershipStatus,
  TeamVote,
} from '../../models/agent-teams.model'
import { IAuthSession } from '../../models/auth-session.model.interface'
import { AgentTeamsService } from '../../services/agent-teams.service'
import { AuthSessionService } from '../../services/auth-session.service'
import { NzNotificationService } from 'ng-zorro-antd/notification'

@Component({
  changeDetection: ChangeDetectionStrategy.Eager,
  standalone: false,
  selector: 'app-agent-teams',
  templateUrl: './agent-teams.component.html',
  styleUrls: ['./agent-teams.component.scss'],
})
export class AgentTeamsComponent implements OnInit {
  @ViewChild('membersSection') membersSection?: HaiProgressiveSectionComponent
  @ViewChild('decisionsSection') decisionsSection?: HaiProgressiveSectionComponent
  @ViewChild('consensusSection') consensusSection?: HaiProgressiveSectionComponent

  readonly moduleId = 'agent-teams'
  teams: AgentTeamContract[] = []
  selected?: AgentTeamContract
  session?: IAuthSession
  loading = true
  detailLoading = false
  saving = false
  errorMessage = ''
  inspectorOpen = false
  createOpen = false
  memberFormOpen = false
  decisionFormOpen = false
  messages: AgentCoordinationMessage[] = []
  attention: AgentTeamAttention[] = []
  outcomes: AgentTeamConsensusOutcome[] = []
  events: AgentTeamLifecycleEvent[] = []
  loadedSections = new Set<string>()

  createForm = {
    key: '', version: '1.0.0', name: '', purpose: '', authorityCeiling: 1,
    consensusMode: 'majority' as 'unanimous' | 'majority' | 'quorum', quorum: 2,
    minimumSupport: 2, allowAbstention: true, evidenceRefs: '',
  }
  memberForm = { agentId: '', agentVersion: '1.0.0', roleId: '', evidenceRefs: '', reason: '' }
  decisionForm = {
    senderMembershipId: '', recipientMembershipId: '', correlationId: '', issue: '',
    position: 'support' as TeamVote, recommendation: '', evidenceRefs: '',
    requiresAcknowledgment: true, expiresInMinutes: 1440,
  }
  consensusForm = { correlationId: '', issue: '' }

  constructor(
    private teamsService: AgentTeamsService,
    private authService: AuthSessionService,
    private preferences: ModuleViewPreferencesService,
    private notification: NzNotificationService
  ) {}

  ngOnInit(): void {
    forkJoin({ session: this.authService.session(), teams: this.teamsService.list() }).subscribe({
      next: ({ session, teams }) => {
        this.session = session
        this.teams = teams
        this.loading = false
        if (teams.length) this.selectTeam(teams[0])
      },
      error: (error: HttpErrorResponse) => {
        this.loading = false
        this.errorMessage = this.apiError(error, 'The authoritative team registry could not be loaded.')
      },
    })
  }

  get canGovern(): boolean { return this.session?.permissions.canAdminister === true }
  get canOperate(): boolean { return this.canGovern || this.session?.permissions.canOperate === true }
  get activeCount(): number { return this.teams.filter((team) => team.status === 'active').length }
  get draftCount(): number { return this.teams.filter((team) => team.status === 'draft').length }
  get reviewCount(): number { return this.attention.filter((item) => item.humanReviewRequired).length }
  get actionableAcknowledgments(): AgentTeamAttention[] { return this.attention.filter((item) => this.canAcknowledge(item)) }
  get activeMembers(): AgentTeamMember[] { return (this.selected?.members || []).filter((member) => member.status === 'active') }
  get votingMembers(): AgentTeamMember[] {
    const team = this.selected
    if (!team) return []
    const votingRoles = new Set(team.roles.filter((role) => role.mayVote).map((role) => role.id))
    return this.activeMembers.filter((member) => member.roleIds.some((role) => votingRoles.has(role)))
  }
  get canActivateSelected(): boolean {
    const team = this.selected
    return !!team && team.status === 'draft' && this.activeMembers.length >= team.consensus.quorum
  }
  get nextAction(): { title: string; summary: string; label: string; action: string } {
    const team = this.selected
    if (!team) return { title: 'Create the first advisory team', summary: 'Define a bounded charter before agents deliberate.', label: 'Create advisory team', action: 'create' }
    if (team.status === 'draft' && this.activeMembers.length < team.consensus.quorum) return { title: 'Add governed members', summary: `${team.consensus.quorum - this.activeMembers.length} more active member(s) required before activation.`, label: 'Add member', action: 'member' }
    if (team.status === 'draft') return { title: 'Activate the charter', summary: 'The quorum is present. Activation enables advisory coordination only.', label: 'Activate team', action: 'activate' }
    if (team.status === 'suspended') return { title: 'Review and reactivate', summary: 'Confirm the charter and evidence before resuming coordination.', label: 'Reactivate', action: 'activate' }
    if (this.reviewCount) return { title: 'Resolve waiting decisions', summary: `${this.reviewCount} coordination message(s) require human review.`, label: 'Review messages', action: 'decisions' }
    if (this.actionableAcknowledgments.length) return { title: 'Acknowledge the decision', summary: `${this.actionableAcknowledgments.length} message(s) await explicit receipt.`, label: 'Review acknowledgment', action: 'decisions' }
    if (this.consensusReady) return { title: 'Evaluate advisory consensus', summary: 'The recorded vote set meets the charter threshold.', label: 'Evaluate consensus', action: 'consensus' }
    return { title: 'Record a deliberation', summary: 'Capture evidence-backed votes, then calculate an advisory consensus.', label: 'Record decision', action: 'decision' }
  }

  get consensusReady(): boolean {
    const team = this.selected
    const correlationId = this.consensusForm.correlationId.trim()
    if (!team || !correlationId || this.outcomes.some((outcome) => outcome.correlationId === correlationId)) return false
    const distinctVoters = new Set(
      this.messages
        .filter((message) => message.correlationId === correlationId)
        .map((message) => message.sender.id)
    )
    return distinctVoters.size >= team.consensus.minimumSupport
  }

  refresh(): void {
    this.loading = true
    this.errorMessage = ''
    this.teamsService.list().subscribe({
      next: (teams) => {
        this.teams = teams
        this.loading = false
        const current = this.selected && teams.find((team) => team.id === this.selected?.id && team.version === this.selected?.version)
        if (current) this.selectTeam(current)
        else if (teams.length) this.selectTeam(teams[0])
        else this.selected = undefined
      },
      error: (error: HttpErrorResponse) => { this.loading = false; this.errorMessage = this.apiError(error, 'Refresh failed.') },
    })
  }

  selectTeam(team: AgentTeamContract): void {
    this.selected = team
    this.loadedSections.clear()
    this.messages = []
    this.attention = []
    this.outcomes = []
    this.events = []
    const open = this.preferences.get(this.moduleId).openSections
    Object.keys(open).filter((section) => open[section]).forEach((section) => this.loadSection(section, true))
  }

  inspect(): void { if (this.selected) this.inspectorOpen = true }
  runNextAction(): void {
    switch (this.nextAction.action) {
      case 'create': this.openCreate(); break
      case 'member': this.openMembers(); break
      case 'activate': this.transition('activate'); break
      case 'decisions': this.openDecisions(false); break
      case 'consensus': this.openConsensus(); break
      default: this.openDecisions(true)
    }
  }

  openCreate(): void {
    if (!this.canGovern) return
    this.createOpen = true
  }

  private openMembers(): void {
    this.membersSection?.setOpen(true)
    this.memberFormOpen = true
  }

  private openDecisions(recordDecision: boolean): void {
    this.decisionsSection?.setOpen(true)
    this.loadSection('decisions', true)
    if (recordDecision) {
      this.decisionFormOpen = true
      this.prepareDecision()
    }
  }

  private openConsensus(): void {
    this.consensusSection?.setOpen(true)
    this.loadSection('consensus', true)
  }

  createTeam(): void {
    if (!this.canGovern || this.saving) return
    const name = this.createForm.name.trim()
    const purpose = this.createForm.purpose.trim()
    if (!name || !purpose || !this.createForm.key.trim()) {
      this.notification.error('Team charter incomplete', 'Name, key, and purpose are required.')
      return
    }
    this.saving = true
    this.teamsService.createGuided({
      key: this.createForm.key.trim(), version: this.createForm.version.trim(), name, purpose,
      authorityCeiling: this.createForm.authorityCeiling, riskCeiling: 'low',
      maximumDelegatedAuthority: Math.min(1, this.createForm.authorityCeiling), maximumDelegatedRisk: 'low',
      consensusMode: this.createForm.consensusMode, quorum: this.createForm.quorum,
      minimumSupport: this.createForm.minimumSupport, allowAbstention: this.createForm.allowAbstention,
      evidenceRefs: this.lines(this.createForm.evidenceRefs), actor: this.actor,
    }).subscribe({
      next: (team) => {
        this.saving = false; this.createOpen = false; this.teams = [team, ...this.teams]; this.selectTeam(team)
        this.notification.success('Advisory team created', 'The charter is a draft and grants no execution authority.')
      },
      error: (error: HttpErrorResponse) => { this.saving = false; this.notification.error('Team creation failed', this.apiError(error, 'No team was created.')) },
    })
  }

  addMember(): void {
    const team = this.selected
    if (!team || !this.canGovern || this.saving) return
    const role = team.roles.find((item) => item.id === this.memberForm.roleId)
    if (!role || !this.memberForm.agentId.trim() || !this.memberForm.reason.trim()) {
      this.notification.error('Member record incomplete', 'Agent, role, and auditable reason are required.')
      return
    }
    this.saving = true
    this.teamsService.addMember(team.id, team.version, {
      expectedRevision: team.revision, actor: this.actor, reason: this.memberForm.reason.trim(),
      member: {
        agentId: this.memberForm.agentId.trim(), agentVersion: this.memberForm.agentVersion.trim(),
        roleIds: [role.id], capabilityIds: [...role.capabilityIds], status: 'active',
        authorityCeiling: Math.min(role.authorityCeiling, team.authorityCeiling),
        riskCeiling: role.riskCeiling, evidenceRefs: this.lines(this.memberForm.evidenceRefs),
      },
    }).subscribe({
      next: (updated) => {
        this.saving = false; this.memberFormOpen = false; this.replaceTeam(updated)
        this.memberForm = { agentId: '', agentVersion: '1.0.0', roleId: '', evidenceRefs: '', reason: '' }
        this.notification.success('Member added', 'The member is bounded by the team charter and remains advisory.')
      },
      error: (error: HttpErrorResponse) => { this.saving = false; this.notification.error('Member not added', this.apiError(error, 'The charter remains unchanged.')) },
    })
  }

  changeMembership(member: AgentTeamMember, status: TeamMembershipStatus): void {
    const team = this.selected
    if (!team || !this.canGovern || this.saving) return
    const reason = window.prompt(`Reason for changing ${member.agentId} to ${status}`)?.trim()
    if (!reason) return
    this.saving = true
    this.teamsService.changeMembership(team.id, team.version, member.id, {
      expectedRevision: team.revision, actor: this.actor, status, reason, evidenceRefs: member.evidenceRefs || [],
    }).subscribe({
      next: (updated) => { this.saving = false; this.replaceTeam(updated); this.notification.success('Membership updated', 'The lifecycle event was recorded.') },
      error: (error: HttpErrorResponse) => { this.saving = false; this.notification.error('Membership unchanged', this.apiError(error, 'The transition was rejected.')) },
    })
  }

  transition(action: 'activate' | 'suspend' | 'retire' | 'revoke'): void {
    const team = this.selected
    if (!team || !this.canGovern || this.saving) return
    const reason = window.prompt(`Reason to ${action} ${team.name}`)?.trim()
    if (!reason) return
    this.saving = true
    this.teamsService.transition(team.id, team.version, action, {
      expectedRevision: team.revision, actor: this.actor, reason, evidenceRefs: team.evidenceRefs || [],
    }).subscribe({
      next: (updated) => { this.saving = false; this.replaceTeam(updated); this.notification.success('Team lifecycle updated', `${updated.name} is now ${updated.status}.`) },
      error: (error: HttpErrorResponse) => { this.saving = false; this.notification.error('Transition rejected', this.apiError(error, 'The team state is unchanged.')) },
    })
  }

  prepareDecision(): void {
    if (!this.decisionForm.correlationId) this.decisionForm.correlationId = this.uuid()
    if (!this.decisionForm.senderMembershipId && this.votingMembers.length) this.decisionForm.senderMembershipId = this.votingMembers[0].id
    if (!this.decisionForm.recipientMembershipId && this.votingMembers.length > 1) this.decisionForm.recipientMembershipId = this.votingMembers[1].id
  }

  createDecision(): void {
    const team = this.selected
    if (!team || !this.canOperate || this.saving) return
    if (!this.decisionForm.senderMembershipId || !this.decisionForm.recipientMembershipId || !this.decisionForm.issue.trim() || !this.decisionForm.recommendation.trim() || !this.lines(this.decisionForm.evidenceRefs).length) {
      this.notification.error('Decision evidence incomplete', 'Sender, recipient, issue, recommendation, and evidence are required.')
      return
    }
    this.saving = true
    this.teamsService.createDecision(team.id, team.version, {
      ...this.decisionForm, issue: this.decisionForm.issue.trim(), recommendation: this.decisionForm.recommendation.trim(),
      evidenceRefs: this.lines(this.decisionForm.evidenceRefs), idempotencyKey: this.uuid(),
    }).subscribe({
      next: (message) => {
        this.saving = false; this.decisionFormOpen = false; this.messages = [message, ...this.messages]; this.loadedSections.add('decisions')
        this.consensusForm = { correlationId: message.correlationId, issue: this.decisionForm.issue.trim() }
        this.decisionForm.recommendation = ''; this.decisionForm.evidenceRefs = ''
        this.loadAttention(); this.notification.success('Decision recorded', 'The server created the canonical advisory message envelope.')
      },
      error: (error: HttpErrorResponse) => { this.saving = false; this.notification.error('Decision not recorded', this.apiError(error, 'No message was stored.')) },
    })
  }

  acknowledge(item: AgentTeamAttention, status: 'accepted' | 'rejected' | 'deferred'): void {
    const team = this.selected
    if (!team || !this.canOperate || this.saving) return
    const reason = status === 'accepted' ? 'Reviewed in HAI operator workspace.' : window.prompt(`Reason for ${status}`)?.trim()
    if (!reason) return
    this.saving = true
    this.teamsService.acknowledge(team.id, team.version, item.messageId, {
      status, reason, retryAfterMinutes: status === 'deferred' ? 60 : 0, idempotencyKey: this.uuid(),
    }).subscribe({
      next: () => { this.saving = false; this.loadAttention(); this.notification.success('Acknowledgment recorded', 'The response is bound to the persisted message and recipient.') },
      error: (error: HttpErrorResponse) => { this.saving = false; this.notification.error('Acknowledgment rejected', this.apiError(error, 'No acknowledgment was stored.')) },
    })
  }

  canAcknowledge(item: AgentTeamAttention): boolean {
    return item.requiresAcknowledgment && ['waiting', 'deferred', 'overdue'].includes(item.state)
  }

  recordConsensus(): void {
    const team = this.selected
    if (!team || !this.canGovern || this.saving || !this.consensusForm.correlationId.trim() || !this.consensusForm.issue.trim()) return
    this.saving = true
    this.teamsService.recordConsensus(team.id, team.version, {
      correlationId: this.consensusForm.correlationId.trim(), issue: this.consensusForm.issue.trim(), idempotencyKey: this.uuid(),
    }).subscribe({
      next: (outcome) => { this.saving = false; this.outcomes = [outcome, ...this.outcomes]; this.notification.success('Consensus evaluated', 'The result is advisory and does not authorize execution.') },
      error: (error: HttpErrorResponse) => { this.saving = false; this.notification.error('Consensus not recorded', this.apiError(error, 'The vote set may be incomplete or conflicting.')) },
    })
  }

  loadSection(section: string, open: boolean): void {
    if (!open || !this.selected || this.loadedSections.has(section)) return
    const team = this.selected
    this.detailLoading = true
    const failed = (error: HttpErrorResponse) => {
      this.detailLoading = false
      this.notification.error('Team detail unavailable', this.apiError(error, 'The section could not be loaded.'))
    }
    if (section === 'decisions') {
      forkJoin({ messages: this.teamsService.messages(team.id, team.version), attention: this.teamsService.attention(team.id, team.version) }).subscribe({
        next: (result) => { this.detailLoading = false; this.loadedSections.add(section); this.messages = result.messages; this.attention = result.attention },
        error: failed,
      })
      return
    }
    if (section === 'consensus') {
      this.teamsService.outcomes(team.id, team.version).subscribe({
        next: (result) => { this.detailLoading = false; this.loadedSections.add(section); this.outcomes = result },
        error: failed,
      })
      return
    }
    if (section === 'history') {
      this.teamsService.events(team.id, team.version).subscribe({
        next: (result) => { this.detailLoading = false; this.loadedSections.add(section); this.events = result },
        error: failed,
      })
      return
    }
    this.teamsService.get(team.id, team.version).subscribe({
      next: (result) => { this.detailLoading = false; this.loadedSections.add(section); this.replaceTeam(result) },
      error: failed,
    })
  }

  trackById(_: number, item: { id?: string; messageId?: string }): string { return item.id || item.messageId || '' }
  statusLabel(value: string): string { return value.replace(/_/g, ' ') }
  roleLabel(member: AgentTeamMember): string { return member.roleIds.map((id) => this.selected?.roles.find((role) => role.id === id)?.name || id).join(', ') }
  shortDigest(value?: string): string { return value ? `${value.slice(0, 10)}...${value.slice(-6)}` : 'Not recorded' }

  private loadAttention(): void {
    const team = this.selected
    if (!team) return
    this.teamsService.attention(team.id, team.version).subscribe({ next: (items) => this.attention = items })
  }
  private replaceTeam(team: AgentTeamContract): void {
    this.selected = team
    this.teams = this.teams.map((item) => item.id === team.id && item.version === team.version ? team : item)
  }
  private get actor(): string { return this.session?.subject || 'authenticated-operator' }
  private lines(value: string): string[] { return value.split(/\r?\n/).map((item) => item.trim()).filter(Boolean) }
  private uuid(): string {
    if (typeof crypto !== 'undefined' && 'randomUUID' in crypto) {
      return crypto.randomUUID()
    }

    return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (character) => {
      const random = Math.floor(Math.random() * 16)
      const value = character === 'x' ? random : (random & 0x3) | 0x8
      return value.toString(16)
    })
  }
  private apiError(error: HttpErrorResponse, fallback: string): string {
    const message = error.error?.error || error.error?.message
    return typeof message === 'string' && message.trim() ? message : fallback
  }
}
