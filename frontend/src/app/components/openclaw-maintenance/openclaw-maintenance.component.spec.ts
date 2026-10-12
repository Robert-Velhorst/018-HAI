import { ComponentFixture, fakeAsync, TestBed, tick } from '@angular/core/testing'
import { provideHttpClient } from '@angular/common/http'
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing'
import { OpenclawMaintenanceComponent } from './openclaw-maintenance.component'

describe('OpenClaw maintenance controls', () => {
  let fixture: ComponentFixture<OpenclawMaintenanceComponent>
  let component: OpenclawMaintenanceComponent
  let http: HttpTestingController

  interface TargetFixture {
    id: string
    policy: string
    installed: string
    available: string
    state: string
    reason: string
    checkedAt: string | null
    nextCheck: string | null
    verifiedAt: string | null
    receiptId: string
    reviewRequired: boolean
    pendingKind: string
    pendingStatus?: string
    pendingStartedAt?: string | null
    installBlockedReason?: string
    installStatus?: string
  }

  const target: TargetFixture = {
    id: 'gateway_core',
    policy: 'observe',
    installed: '2026.6.10',
    available: '2026.9.1',
    state: 'update_available',
    reason: '',
    checkedAt: null,
    nextCheck: null,
    verifiedAt: null,
    receiptId: 'check-1',
    reviewRequired: false,
    pendingKind: '',
  }

  beforeEach(() => {
    TestBed.configureTestingModule({ providers: [provideHttpClient(), provideHttpClientTesting()] })
    fixture = TestBed.createComponent(OpenclawMaintenanceComponent)
    component = fixture.componentInstance
    http = TestBed.inject(HttpTestingController)
  })

  afterEach(() => { http.verify(); TestBed.resetTestingModule() })

  function initialize(
    enabled = true,
    targets: TargetFixture[] = [target],
    details: { worker?: { state: string; lastSeenAt: string; freshFor: string }; installation?: { available: boolean; reason: string } } = {},
  ): void {
    fixture.detectChanges()
    http.expectOne('assets/outline/reload.svg').flush('<svg></svg>')
    http.expectOne('/api/v1/openclaw-maintenance').flush({ enabled, targets, ...details })
    fixture.detectChanges()
  }

  function freshTarget(overrides: Partial<TargetFixture> = {}): TargetFixture {
    const now = Date.now()
    return {
      ...target,
      checkedAt: new Date(now - 60_000).toISOString(),
      nextCheck: new Date(now + 23 * 60 * 60 * 1000).toISOString(),
      ...overrides,
    }
  }

  function acceptCheckAndReportPending(): void {
    component.act(component.targets[0], 'check')
    http.expectOne('/api/v1/openclaw-maintenance/gateway_core').flush(null, { status: 202, statusText: 'Accepted' })
    http.expectOne('/api/v1/openclaw-maintenance').flush({
      enabled: true,
      targets: [{ ...component.targets[0], pendingKind: 'check', pendingStatus: 'pending' }],
    })
    fixture.detectChanges()
  }

  it('loads stored status without scheduling a check', () => {
    fixture.detectChanges()
    expect(fixture.nativeElement.textContent).toContain('Loading stored maintenance status...')
    http.expectOne('assets/outline/reload.svg').flush('<svg></svg>')
    http.expectOne('/api/v1/openclaw-maintenance').flush({ enabled: true, targets: [target] })
    fixture.detectChanges()

    expect(component.targets[0].installed).toBe('2026.6.10')
    expect(fixture.nativeElement.textContent).toContain('Update available')
    http.expectNone('/api/v1/openclaw-maintenance/gateway_core')
  })

  it('does not poll a pending job that was not submitted by this component', fakeAsync(() => {
    initialize(true, [{ ...target, pendingKind: 'check', pendingStatus: 'pending' }])

    tick(15_000)

    http.expectNone('/api/v1/openclaw-maintenance')
    expect(component.targets[0].pendingKind).toBe('check')
  }))

  it('polls an accepted pending check and stops when the backend reports a terminal state', fakeAsync(() => {
    initialize()
    acceptCheckAndReportPending()

    tick(4_999)
    http.expectNone('/api/v1/openclaw-maintenance')
    tick(1)
    const poll = http.expectOne('/api/v1/openclaw-maintenance')
    expect(poll.request.method).toBe('GET')
    poll.flush({
      enabled: true,
      targets: [{ ...target, state: 'check_failed', reason: 'The read-only check failed.', pendingKind: '', pendingStatus: 'failed' }],
    })
    fixture.detectChanges()

    expect(component.targets[0].state).toBe('check_failed')
    expect(component.pollingError).toBe('')
    expect(fixture.nativeElement.textContent).toContain('The read-only check failed.')
    expect(fixture.nativeElement.textContent).not.toContain('Installation verified')

    tick(30_000)
    http.expectNone('/api/v1/openclaw-maintenance')
  }))

  it('follows a verified automatic apply queued by the submitted check without resubmitting an action', fakeAsync(() => {
    const automaticTarget = freshTarget({ policy: 'auto_install_verified' })
    initialize(true, [automaticTarget])
    acceptCheckAndReportPending()

    tick(5_000)
    http.expectOne('/api/v1/openclaw-maintenance').flush({
      enabled: true,
      targets: [{ ...automaticTarget, state: 'installing', pendingKind: 'apply', pendingStatus: 'leased' }],
    })
    tick(5_000)
    http.expectOne('/api/v1/openclaw-maintenance').flush({
      enabled: true,
      targets: [{ ...automaticTarget, state: 'verified', pendingKind: '', pendingStatus: 'completed' }],
    })
    fixture.detectChanges()

    expect(component.targets[0].state).toBe('verified')
    expect(fixture.nativeElement.textContent).toContain('Installation verified')
    http.expectNone('/api/v1/openclaw-maintenance/gateway_core')
    tick(10_000)
    http.expectNone('/api/v1/openclaw-maintenance')
  }))

  it('serializes polling requests while a status response is unresolved', fakeAsync(() => {
    initialize()
    acceptCheckAndReportPending()

    tick(5_000)
    const firstPoll = http.expectOne('/api/v1/openclaw-maintenance')
    tick(20_000)
    http.expectNone('/api/v1/openclaw-maintenance')
    expect(component.targets[0].pendingKind).toBe('check')

    firstPoll.flush({ enabled: true, targets: [{ ...target, pendingKind: 'check', pendingStatus: 'leased' }] })
    tick(5_000)
    http.expectOne('/api/v1/openclaw-maintenance').flush({
      enabled: true,
      targets: [{ ...target, state: 'current', pendingKind: '', pendingStatus: 'completed' }],
    })
    tick(10_000)
    http.expectNone('/api/v1/openclaw-maintenance')
  }))

  it('surfaces polling errors as unknown status and resumes within the retry bound', fakeAsync(() => {
    initialize()
    acceptCheckAndReportPending()

    tick(5_000)
    http.expectOne('/api/v1/openclaw-maintenance').flush(
      { error: 'temporary failure' },
      { status: 503, statusText: 'Unavailable' },
    )
    fixture.detectChanges()

    expect(component.pollingError).toContain('no completion is confirmed')
    expect(fixture.nativeElement.querySelector('[role="alert"]')?.textContent).toContain('no completion is confirmed')
    expect(fixture.nativeElement.textContent).not.toContain('Installation verified')

    tick(5_000)
    http.expectOne('/api/v1/openclaw-maintenance').flush({
      enabled: true,
      targets: [{ ...target, state: 'current', pendingKind: '', pendingStatus: 'completed' }],
    })
    fixture.detectChanges()

    expect(component.pollingError).toBe('')
    expect(component.targets[0].state).toBe('current')
  }))

  it('stops at the polling bound without presenting an unconfirmed job as complete', fakeAsync(() => {
    initialize()
    acceptCheckAndReportPending()

    for (let attempt = 0; attempt < 24; attempt += 1) {
      tick(5_000)
      http.expectOne('/api/v1/openclaw-maintenance').flush({
        enabled: true,
        targets: [{ ...target, pendingKind: 'check', pendingStatus: 'leased' }],
      })
    }
    fixture.detectChanges()

    expect(component.pollingNotice).toContain('No terminal status is confirmed')
    expect(fixture.nativeElement.textContent).toContain('refresh manually to check again')
    expect(fixture.nativeElement.textContent).not.toContain('Check completed')
    tick(30_000)
    http.expectNone('/api/v1/openclaw-maintenance')
  }))

  it('cancels scheduled status polling when the component is destroyed', fakeAsync(() => {
    initialize()
    acceptCheckAndReportPending()

    fixture.destroy()
    tick(10_000)

    http.expectNone('/api/v1/openclaw-maintenance')
    expect(component.pollingError).toBe('')
  }))

  it('shows current no-update evidence and both schedule timestamps in the main view', () => {
    const current = freshTarget({ state: 'current', available: '2026.6.10' })
    initialize(true, [current])

    expect(component.targets[0].nextCheck).toBe(current.nextCheck)
    expect(component.observationFreshness(component.targets[0])).toBe('current')
    expect(component.targetStateLabel(component.targets[0])).toBe('Current: no newer release reported')
    expect(fixture.nativeElement.textContent).toContain('Current observation: no newer release reported')
    expect(fixture.nativeElement.textContent).toContain('Last successful check:')
    expect(fixture.nativeElement.textContent).toContain('Next scheduled check:')
    expect(fixture.nativeElement.textContent).toContain('A recent successful check reported no newer release.')
  })

  it('recomputes freshness locally when the next check becomes due', fakeAsync(() => {
    const current = freshTarget({
      state: 'current',
      available: '2026.6.10',
      nextCheck: new Date(Date.now() + 30_000).toISOString(),
    })
    initialize(true, [current])
    expect(fixture.nativeElement.textContent).toContain('Current observation: no newer release reported')

    tick(60_000)
    fixture.detectChanges()

    expect(component.observationFreshness(component.targets[0])).toBe('stale')
    expect(fixture.nativeElement.textContent).toContain('Stale observation')
    expect(fixture.nativeElement.textContent).toContain('that observation is stale and is not a current finding')
    http.expectNone('/api/v1/openclaw-maintenance')
  }))

  it('marks a stored no-update result stale after its scheduled check is due', () => {
    const now = Date.now()
    const stale = {
      ...target,
      state: 'current',
      checkedAt: new Date(now - 25 * 60 * 60 * 1000).toISOString(),
      nextCheck: new Date(now - 60 * 60 * 1000).toISOString(),
    }
    initialize(true, [stale])

    expect(component.observationFreshness(component.targets[0])).toBe('stale')
    expect(component.targetStateLabel(component.targets[0])).toBe('Stale: last check found no newer release')
    expect(fixture.nativeElement.textContent).toContain('Stale observation')
    expect(fixture.nativeElement.textContent).toContain('that observation is stale and is not a current finding')
  })

  it('marks missing or invalid check timestamps unknown rather than current', () => {
    const unknown = { ...target, state: 'current', checkedAt: null, nextCheck: new Date(Date.now() + 60_000).toISOString() }
    initialize(true, [unknown])

    expect(component.observationFreshness(component.targets[0])).toBe('unknown')
    expect(component.targetStateLabel(component.targets[0])).toBe('Freshness unknown')
    expect(fixture.nativeElement.textContent).toContain('Freshness unknown')
    expect(fixture.nativeElement.textContent).toContain('no valid successful-check time to confirm')
    expect(component.timestampLabel('not-a-date')).toBe('Not reported')
  })

  it('does not offer or submit installation from a stale update observation', () => {
    const now = Date.now()
    const stale = {
      ...freshTarget({ policy: 'auto_install_verified' }),
      checkedAt: new Date(now - 25 * 60 * 60 * 1000).toISOString(),
      nextCheck: new Date(now - 60 * 60 * 1000).toISOString(),
    }
    initialize(true, [stale])

    const apply = Array.from(fixture.nativeElement.querySelectorAll('.actions button')) as HTMLButtonElement[]
    expect(apply.find(button => button.textContent?.includes('Request installation'))?.disabled).toBeTrue()
    component.act(component.targets[0], 'apply')
    http.expectNone('/api/v1/openclaw-maintenance/gateway_core')
  })

  it('distinguishes disabled maintenance from an offline worker', () => {
    initialize(false, [{ ...target, policy: 'auto_install_verified' }])

    expect(fixture.nativeElement.textContent).toContain('maintenance is disabled on this HAI server')
    expect(fixture.nativeElement.textContent).not.toContain('Connect the Windows maintenance worker')
    const buttons = Array.from(fixture.nativeElement.querySelectorAll('.actions button')) as HTMLButtonElement[]
    expect(buttons.every(button => button.disabled)).toBeTrue()
    expect((fixture.nativeElement.querySelector('input[type="checkbox"]') as HTMLInputElement).disabled).toBeTrue()
  })

  it('reports observed worker contact and blocks installs without blocking checks', () => {
    const installation = { available: false, reason: 'Process-tree termination cannot be guaranteed.' }
    const worker = { state: 'stale_contact', lastSeenAt: '2026-09-24T10:00:00Z', freshFor: '3m0s' }
    initialize(true, [{
      ...target,
      policy: 'auto_install_verified',
      installStatus: 'blocked',
      installBlockedReason: 'Companion update is blocked: HAI_OPENCLAW_PUBLISHER_THUMBPRINT is not configured on the Windows worker.',
    }], { installation, worker })

    expect(component.installation).toEqual(installation)
    expect(component.worker).toEqual(worker)
    expect(component.targetStateLabel(component.targets[0])).toBe('Update blocked')
    expect(fixture.nativeElement.textContent).toContain('Process-tree termination cannot be guaranteed')
    expect(fixture.nativeElement.textContent).toContain('THUMBPRINT is not configured')
    expect(fixture.nativeElement.textContent).toContain('This does not prove whether the process has stopped')
    const apply = Array.from(fixture.nativeElement.querySelectorAll('.actions button')) as HTMLButtonElement[]
    expect(apply.find(button => button.textContent?.includes('Request installation'))?.disabled).toBeTrue()

    component.act(component.targets[0], 'apply')
    component.act(component.targets[0], 'check')
    http.expectOne('/api/v1/openclaw-maintenance/gateway_core').flush(null, { status: 202, statusText: 'Accepted' })
    http.expectOne('/api/v1/openclaw-maintenance').flush({
      enabled: true,
      installation,
      worker,
      targets: component.targets,
    })
    http.expectNone('/api/v1/openclaw-maintenance/gateway_core')
  })

  it('maps backend target states without treating a failed check as an update failure', () => {
    const labels: Record<string, string> = {
      disabled: 'Disabled',
      unknown: 'Not checked',
      current: 'No newer release reported',
      update_available: 'Update available',
      installing: 'Installation in progress',
      verified: 'Installation verified',
      check_failed: 'Check failed',
      failed: 'Failed',
      needs_review: 'Needs review',
    }

    for (const [state, label] of Object.entries(labels)) expect(component.stateLabel(state)).toBe(label)
    expect(component.stateLabel('future_state')).toBe('Unrecognized status (future_state)')
    expect(component.policyLabel('future_policy')).toBe('Unknown policy (future_policy)')
    initialize(true, [{ ...target, state: 'check_failed', reason: 'Read-only check failed.' }])
    expect(fixture.nativeElement.querySelector('.state').textContent).toContain('Check failed')
    expect(fixture.nativeElement.textContent).not.toContain('Installation failed')
  })

  it('shows queued or in-progress work and disables conflicting actions', () => {
    initialize(true, [{ ...target, policy: 'auto_install_verified', pendingKind: 'apply' }])

    expect(fixture.nativeElement.textContent).toContain('An update is queued; no installation result is confirmed yet')
    const buttons = Array.from(fixture.nativeElement.querySelectorAll('.actions button')) as HTMLButtonElement[]
    expect(buttons.every(button => button.disabled)).toBeTrue()
  })

  it('requires the backend recovery review path and keeps policy in observe mode', () => {
    initialize(true, [{
      ...target,
      policy: 'observe',
      state: 'needs_review',
      reason: 'Inspect the installation before resuming.',
      reviewRequired: true,
      pendingKind: 'check',
    }])

    expect(fixture.nativeElement.querySelector('.state').textContent).toContain('Needs review')
    expect(fixture.nativeElement.textContent).toContain('successful read-only check after that update')
    expect(fixture.nativeElement.textContent).toContain('HAI must also confirm runtime health')
    expect(fixture.nativeElement.textContent).toContain('Policy: Observe only')
    const review = Array.from(fixture.nativeElement.querySelectorAll('.actions button')) as HTMLButtonElement[]
    expect(review.find(button => button.textContent?.includes('Acknowledge recovery'))?.disabled).toBeTrue()
    http.expectNone('/api/v1/openclaw-maintenance/gateway_core')
  })

  it('records an accepted recovery acknowledgement without turning automatic updates back on', () => {
    initialize(true, [{ ...target, policy: 'observe', state: 'needs_review', reviewRequired: true }])
    component.act(component.targets[0], 'review')
    const request = http.expectOne('/api/v1/openclaw-maintenance/gateway_core')
    expect(request.request.body).toEqual({ action: 'review', version: '', policy: '' })
    request.flush(null, { status: 202, statusText: 'Accepted' })
    http.expectOne('/api/v1/openclaw-maintenance').flush({
      enabled: true,
      targets: [{ ...target, policy: 'observe', state: 'current', reviewRequired: false }],
    })
    fixture.detectChanges()

    expect(fixture.nativeElement.textContent).toContain('HAI recorded the recovery acknowledgement')
    expect(fixture.nativeElement.textContent).toContain('Policy: Observe only')
    expect(fixture.nativeElement.textContent).not.toContain('Policy: Automatic verified updates')
  })

  it('shows the action being submitted, then only reports acceptance and backend queue evidence', () => {
    const automaticTarget = freshTarget({ policy: 'auto_install_verified' })
    initialize(true, [automaticTarget])

    component.act(automaticTarget, 'check')
    component.act(automaticTarget, 'check')
    fixture.detectChanges()
    expect(component.busy[automaticTarget.id]).toBe('check')
    expect(fixture.nativeElement.textContent).toContain('Submitting read-only check request...')
    const request = http.expectOne('/api/v1/openclaw-maintenance/gateway_core')
    expect(request.request.method).toBe('POST')
    expect(request.request.body).toEqual({ action: 'check', version: '', policy: '' })

    request.flush(null, { status: 202, statusText: 'Accepted' })
    fixture.detectChanges()
    expect(component.message).toContain('HAI accepted the check request')
    expect(fixture.nativeElement.textContent).toContain('HAI accepted the check request')
    expect(fixture.nativeElement.textContent).toContain('response has no check result')
    const statusRequest = http.expectOne('/api/v1/openclaw-maintenance')
    statusRequest.flush({ enabled: true, targets: [{ ...automaticTarget, pendingKind: 'check' }] })
    fixture.detectChanges()
    expect(fixture.nativeElement.textContent).toContain('A read-only check is queued; no completion is confirmed yet.')
    expect(fixture.nativeElement.textContent).not.toContain('Check pending or running')
    const buttons = Array.from(fixture.nativeElement.querySelectorAll('.actions button')) as HTMLButtonElement[]
    expect(buttons.every(button => button.disabled)).toBeTrue()
  })

  it('does not present an accepted installation request as a completed update', () => {
    const automaticTarget = freshTarget({ policy: 'auto_install_verified' })
    initialize(true, [automaticTarget])
    component.act(automaticTarget, 'apply')
    const request = http.expectOne('/api/v1/openclaw-maintenance/gateway_core')
    expect(request.request.body).toEqual({ action: 'apply', version: '2026.9.1', policy: '' })
    request.flush(null, { status: 202, statusText: 'Accepted' })
    http.expectOne('/api/v1/openclaw-maintenance').flush({
      enabled: true,
      targets: [{ ...automaticTarget, pendingKind: 'apply' }],
    })
    fixture.detectChanges()

    expect(fixture.nativeElement.textContent).toContain('HAI accepted the installation request')
    expect(fixture.nativeElement.textContent).toContain('Acceptance is not completion evidence')
    expect(fixture.nativeElement.textContent).toContain('An update is queued; no installation result is confirmed yet.')
    expect(fixture.nativeElement.querySelector('.state').textContent).toContain('Update available')
    expect(fixture.nativeElement.textContent).not.toContain('Installation verified')
  })

  it('surfaces action rejection separately and preserves the last backend state', () => {
    initialize()
    component.act(freshTarget({ policy: 'auto_install_verified' }), 'apply')
    const request = http.expectOne('/api/v1/openclaw-maintenance/gateway_core')
    request.flush({ error: 'Action unavailable: check policy and active sessions' }, { status: 409, statusText: 'Conflict' })
    fixture.detectChanges()

    expect(component.actionError).toContain('Action unavailable: check policy and active sessions')
    expect(component.error).toBe('')
    expect(fixture.nativeElement.querySelector('[role="alert"]')?.textContent).toContain('No update outcome has been confirmed')
    expect(fixture.nativeElement.textContent).toContain('No update outcome has been confirmed')
    expect(component.targets[0].state).toBe('update_available')
    http.expectNone('/api/v1/openclaw-maintenance')
  })

  it('does not invite a retry when HAI cannot confirm whether a request was accepted', () => {
    initialize()
    component.act(target, 'check')
    const request = http.expectOne('/api/v1/openclaw-maintenance/gateway_core')
    request.error(new ProgressEvent('error'))
    fixture.detectChanges()

    expect(component.actionError).toContain('did not confirm whether it accepted this request')
    expect(component.actionError).toContain('Refresh status before retrying')
    http.expectNone('/api/v1/openclaw-maintenance')
  })

  it('allows policy revocation while an update is pending', () => {
    initialize(true, [{ ...target, policy: 'auto_install_verified', pendingKind: 'apply' }])
    const toggle = fixture.nativeElement.querySelector('input[type="checkbox"]') as HTMLInputElement
    expect(toggle.checked).toBeTrue()
    toggle.checked = false
    component.policy(component.targets[0], { target: toggle } as unknown as Event)
    expect(toggle.checked).toBeTrue()
    const request = http.expectOne('/api/v1/openclaw-maintenance/gateway_core')
    expect(request.request.body.policy).toBe('observe')
    request.flush(null, { status: 202, statusText: 'Accepted' })
    http.expectOne('/api/v1/openclaw-maintenance').flush({ enabled: true, targets: [{ ...target, policy: 'observe', pendingKind: 'apply' }] })
    fixture.detectChanges()
    expect(component.targets[0].policy).toBe('observe')
    expect(fixture.nativeElement.textContent).toContain('Policy: Observe only')
    expect((fixture.nativeElement.querySelector('input[type="checkbox"]') as HTMLInputElement).checked).toBeFalse()
  })

  it('does not send actions while a status refresh is in flight', () => {
    initialize()
    component.load()
    component.act(target, 'check')
    http.expectNone('/api/v1/openclaw-maintenance/gateway_core')
    http.expectOne('/api/v1/openclaw-maintenance').flush({ enabled: true, targets: [target] })
    expect(component.loading).toBeFalse()
  })

  it('keeps controls disabled when owner access is denied', () => {
    fixture.detectChanges()
    http.expectOne('assets/outline/reload.svg').flush('<svg></svg>')
    http.expectOne('/api/v1/openclaw-maintenance').flush({}, { status: 403, statusText: 'Forbidden' })
    fixture.detectChanges()

    expect(component.error).toContain('owner')
    expect(component.enabled).toBeFalse()
    expect(fixture.nativeElement.querySelector('[role="alert"]').textContent).toContain('owner')
  })
})
