import { CommonModule } from '@angular/common';
import { HttpClient, HttpErrorResponse } from '@angular/common/http';
import { CUSTOM_ELEMENTS_SCHEMA } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { RouterTestingModule } from '@angular/router/testing';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzCardModule } from 'ng-zorro-antd/card';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { of, Subject, throwError, TimeoutError } from 'rxjs';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import { ControlRoomModule } from '../../control-room/control-room.module';
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service';
import { MCPPreflightService } from '../../services/mcp-preflight.service';
import { RuntimeLabService } from '../../services/runtime-lab.service';
import { RuntimeLabComponent } from './runtime-lab.component';
import { IRuntimeAttempt, IRuntimeSummary } from '../../models/runtime-lab.model.interface';

describe('RuntimeLabComponent MCP readiness', () => {
  beforeEach(() => {
    new ModuleViewPreferencesService(document).reset('runtime-lab')
    document.body.classList.remove('hai-view-advanced')
  })

  function make() {
    const http = jasmine.createSpyObj<HttpClient>('HttpClient', ['get']);
    const runtimeService = jasmine.createSpyObj('RuntimeLabService', ['overview', 'featureParity', 'capabilities', 'probe', 'selfTest']);
    const mcpService = jasmine.createSpyObj('MCPPreflightService', ['overview', 'run']);
    const notification = jasmine.createSpyObj('NzNotificationService', ['success', 'warning', 'error', 'info']);
    const router = jasmine.createSpyObj('Router', ['navigate']);
    http.get.and.returnValue(of([]));
    runtimeService.overview.and.returnValue(of({ runtimes: [] }));
    runtimeService.featureParity.and.returnValue(of({
      requiredCoverageAreas: ['agent_runtimes'],
      inventories: [{
        runtimeId: 'openclaw',
        project: 'OpenClaw',
        repositoryUrl: 'https://github.com/openclaw/openclaw',
        defaultBranch: 'main',
        reviewedRevision: 'abc',
        reviewedAt: '2026-08-08T00:00:00Z',
        license: 'MIT',
        licensePolicy: 'Remote protocol only.',
        readinessCeiling: 'declared',
        canonicalAuthority: 'HAI',
        features: [],
      }],
      dispositionCounts: {},
      implementationCounts: {},
      generatedAt: '2026-08-08T00:00:00Z',
    }));
    runtimeService.capabilities.and.returnValue(of({
      cards: [{
        id: 'openclaw.gateway.discovery',
        runtimeId: 'openclaw',
        name: 'Inspect OpenClaw Gateway readiness',
        purpose: 'Read metadata only.',
        inputSchema: { type: 'object' },
        outputSchema: { type: 'object' },
        authenticationState: 'not_configured',
        availability: 'not_configured',
        runtimeLocation: 'operator_managed_local_service',
        requiredAuthority: ['authenticated_owner', 'runtime.read'],
        riskLevel: 'low',
        expectedCostEurMax: 0,
        costPolicy: 'No paid calls.',
        contextCost: 'none',
        timeoutSeconds: 5,
        retryBehaviour: 'manual',
        reversibility: 'read_only',
        approvalRequirements: ['owner managed'],
        verificationMethod: 'schema check',
        evidenceReturned: ['identity'],
        readinessLevel: 'declared',
        readinessReason: 'Contract only.',
        canInvoke: false,
        canExecuteExternalEffect: false,
        sourceFeatureIds: ['openclaw-gateway'],
      }],
      counts: { declared: 1 },
      authority: 'contract_only',
      safetyNote: 'No permission is granted.',
    }));
    mcpService.overview.and.returnValue(of({ enabled: true, scope: 'Read-only MCP preflight.', servers: [{ id: 'github', catalogName: 'GitHub MCP Server', configured: true }] }));
    return { component: new RuntimeLabComponent(http, runtimeService, mcpService, notification, router, new ModuleViewPreferencesService(document)), runtimeService, mcpService, notification, http };
  }

  it('loads MCP readiness alongside runtime summaries', () => {
    const { component, mcpService } = make();
    component.refresh();

    expect(mcpService.overview).toHaveBeenCalled();
    expect(component.mcpOverview?.servers[0].catalogName).toBe('GitHub MCP Server');
  });

  it('retains redacted legacy blocker diagnostics without treating them as execution evidence', () => {
    const { component, runtimeService, notification } = make();
    runtimeService.overview.and.returnValue(of({ runtimes: [{
      info: { id: 'hermes', displayName: 'Hermes', kind: 'agent', description: 'External runtime.' },
      status: 'blocked', claimLevel: 'declared', canExecute: false, capabilities: [],
      lastAttempt: { status: 'blocked', detail: 'Missing api_key=synthetic-secret.' },
    }] }));
    component.refresh();
    expect(component.runtimeAttentionDetail(component.runtimes[0])).toBe('Missing api_key=[redacted]');
    expect(component.runtimes[0].lastAttempt).toBeUndefined();
    expect(notification.success).not.toHaveBeenCalled();
    runtimeService.overview.and.returnValue(of({ runtimes: [{
      info: component.runtimes[0].info, status: 'blocked', claimLevel: 'declared',
      canExecute: false, capabilities: [],
    }] }));
    component.refresh();
    expect(component.runtimeAttentionDetail(component.runtimes[0])).toBe('External runtime.');
  });

  it('loads the source-reviewed runtime parity inventory without changing readiness', () => {
    const { component } = make();
    component.refresh();

    expect(component.parityOverview?.inventories[0].runtimeId).toBe('openclaw');
    expect(component.parityOverview?.inventories[0].readinessCeiling).toBe('declared');
    expect(component.parityLoading).toBeFalse();
  });

  it('loads non-executable HAI capability cards separately from runtime claims', () => {
    const { component } = make();
    component.refresh();

    const cards = component.cardsForRuntime('openclaw');
    expect(cards.length).toBe(1);
    expect(cards[0].readinessLevel).toBe('declared');
    expect(cards[0].canInvoke).toBeFalse();
    expect(cards[0].canExecuteExternalEffect).toBeFalse();
  });

  it('distinguishes capability loading, unavailable, empty, and available states', () => {
    const { component, runtimeService } = make()
    component.capabilityLoading = true
    expect(component.capabilityState('openclaw')).toBe('loading')

    component.capabilityLoading = false
    component.capabilityOverview = undefined
    expect(component.capabilityState('openclaw')).toBe('unavailable')

    component.capabilityOverview = { cards: [] } as any
    expect(component.capabilityState('openclaw')).toBe('empty')

    runtimeService.capabilities.and.returnValue(throwError(() => new Error('offline')))
    component.refreshCapabilities()
    expect(component.capabilityState('openclaw')).toBe('unavailable')

    component.capabilityOverview = { cards: [{ runtimeId: 'openclaw' }] } as any
    expect(component.capabilityState('openclaw')).toBe('available')
  });

  it('tracks probe and self-test activity independently for the same runtime', () => {
    const { component, runtimeService } = make()
    const probe = new Subject<any>()
    const selfTest = new Subject<any>()
    runtimeService.probe.and.returnValue(probe.asObservable())
    runtimeService.selfTest.and.returnValue(selfTest.asObservable())
    const runtime = { info: { id: 'openclaw', displayName: 'OpenClaw' } } as any

    component.probe(runtime)
    component.probe(runtime)
    expect(runtimeService.probe).toHaveBeenCalledTimes(1)
    expect(component.probeBusy['openclaw']).toBeTrue()
    expect(component.selfTestBusy['openclaw']).toBeUndefined()

    component.selfTest(runtime)
    expect(component.probeBusy['openclaw']).toBeTrue()
    expect(component.selfTestBusy['openclaw']).toBeTrue()

    probe.next({ protocolValid: true, discoveryState: 'verified', readinessLevel: 'declared' })
    expect(component.probeBusy['openclaw']).toBeFalse()
    expect(component.selfTestBusy['openclaw']).toBeTrue()

    selfTest.error(new Error('test failed'))
    expect(component.selfTestBusy['openclaw']).toBeFalse()
  });

  it('clears each busy state when its request completes without a response', () => {
    const { component, runtimeService } = make()
    const probe = new Subject<any>()
    const selfTest = new Subject<any>()
    runtimeService.probe.and.returnValue(probe.asObservable())
    runtimeService.selfTest.and.returnValue(selfTest.asObservable())
    const runtime = { info: { id: 'openclaw', displayName: 'OpenClaw' } } as any

    component.probe(runtime)
    component.selfTest(runtime)
    probe.complete()
    selfTest.complete()

    expect(component.probeBusy['openclaw']).toBeFalse()
    expect(component.selfTestBusy['openclaw']).toBeFalse()
  });

  function safeRuntime(lastAttempt?: IRuntimeAttempt): IRuntimeSummary {
    return {
      info: { id: 'local-safe-worker', displayName: 'Local safe worker', kind: 'safe-worker', description: 'Bounded local work.' },
      status: 'ready', claimLevel: 'exercised', canExecute: true, capabilities: [], lastAttempt,
    }
  }

  function completedAttempt(overrides: Partial<IRuntimeAttempt> = {}): IRuntimeAttempt {
    return {
      id: 'attempt', runtimeId: 'local-safe-worker', operationId: 'self-test-operation',
      operationStatus: 'completed', status: 'succeeded', detail: 'Recorded ledger completion.',
      verificationPassed: true, interrupted: false, reconciliationRequired: false, outcomeRecorded: true,
      createdAt: '2026-10-01T10:00:00Z', ...overrides,
    }
  }

  it('retains an uncertain self-test response even if overview fails and later returns stale success', () => {
    const { component, runtimeService, notification } = make()
    const runtime = safeRuntime(completedAttempt())
    const uncertain = completedAttempt({
      status: 'inconclusive', operationStatus: 'running', verificationPassed: false,
      reconciliationRequired: true, outcomeRecorded: false,
      receipt: { output: { artifactHash: 'partial-hash', boundedOutput: 'password=synthetic-secret' } },
    })
    runtimeService.selfTest.and.returnValue(of(uncertain))
    runtimeService.overview.and.returnValue(throwError(() => new HttpErrorResponse({ status: 503 })))
    component.runtimes = [runtime]
    component.selfTest(runtime)
    expect(component.selfTestOutcomeSummary(runtime)).toContain('partial-hash')
    expect(component.selfTestOutcomeSummary(runtime)).not.toContain('synthetic-secret')
    expect(component.canSelfTest(runtime)).toBeFalse()
    expect(notification.success).not.toHaveBeenCalled()
    runtimeService.overview.and.returnValue(of({ runtimes: [safeRuntime(completedAttempt())] }))
    component.refresh()
    component.selfTest(component.runtimes[0])
    expect(runtimeService.selfTest).toHaveBeenCalledTimes(1)
    expect(component.selfTestAttemptLabel(component.lastSelfTestAttempt(component.runtimes[0])!)).toContain('reconciliation')
    expect(component.runtimeReadinessSummary).toBe('0 of 1 runtimes ready')
  })

  for (const conflict of [
    { verificationPassed: false }, { interrupted: true }, { reconciliationRequired: true },
    { outcomeRecorded: false }, { operationStatus: 'verifying' }, { operationStatus: undefined },
  ]) {
    it(`withholds verified notifications and green status for conflicting self-test evidence (${JSON.stringify(conflict)})`, () => {
      const { component, runtimeService, notification } = make()
      const attempt = completedAttempt(conflict)
      const runtime = safeRuntime()
      runtimeService.selfTest.and.returnValue(of(attempt))
      component.selfTest(runtime)
      expect(notification.success).not.toHaveBeenCalled()
      expect(component.selfTestAttemptColor(attempt)).toBe('gold')
      expect(component.selfTestAttemptLabel(attempt)).toContain('Completion not verified')
      expect(component.canSelfTest(runtime)).toBeFalse()
    })
  }

  it('shows verified completion only for an uncontradicted completed/passed attempt', () => {
    const { component, runtimeService, notification } = make()
    const runtime = safeRuntime()
    const attempt = completedAttempt()
    runtimeService.selfTest.and.returnValue(of(attempt))
    component.selfTest(runtime)
    expect(notification.success).toHaveBeenCalledOnceWith('Self-test passed', 'Local safe worker verified through the ledger.')
    expect(component.selfTestAttemptLabel(attempt)).toBe('Verified completion')
    expect(component.canSelfTest(runtime)).toBeTrue()
  })

  for (const error of [new TimeoutError(), new HttpErrorResponse({ status: 0 }), new HttpErrorResponse({ status: 503 })]) {
    it(`blocks another self-test after an uncertain transport outcome (${error instanceof TimeoutError ? 'timeout' : error.status})`, () => {
      const { component, runtimeService, notification } = make()
      const runtime = safeRuntime()
      runtimeService.selfTest.and.returnValue(throwError(() => error))
      component.selfTest(runtime)
      component.refresh()
      component.selfTest(runtime)
      expect(runtimeService.selfTest).toHaveBeenCalledTimes(1)
      expect(component.canSelfTest(runtime)).toBeFalse()
      expect(notification.error).not.toHaveBeenCalled()
      expect(notification.warning).toHaveBeenCalled()
    })
  }

  it('preserves receipt-free authorization refusals and setup-required records without claiming uncertain effects', () => {
    const { component, runtimeService, notification } = make()
    const runtime = safeRuntime()
    runtimeService.selfTest.and.returnValue(throwError(() => new HttpErrorResponse({ status: 403 })))
    component.selfTest(runtime)
    expect(component.canSelfTest(runtime)).toBeTrue()
    expect(component.selfTestNeedsReconciliation(runtime)).toBeFalse()
    expect(notification.warning).not.toHaveBeenCalled()
    runtimeService.selfTest.and.returnValue(of(completedAttempt({
      status: 'setup_required', verificationPassed: false, operationStatus: undefined,
      outcomeRecorded: false, operationId: undefined,
    })))
    component.selfTest(runtime)
    expect(component.canSelfTest(runtime)).toBeTrue()
  })

  it('does not classify an external health-only inconclusive record as uncertain execution', () => {
    const { component, runtimeService } = make()
    const runtime = { ...safeRuntime(), info: { ...safeRuntime().info, id: 'openclaw' } }
    runtimeService.selfTest.and.returnValue(of(completedAttempt({
      runtimeId: 'openclaw', status: 'inconclusive', verificationPassed: false,
      operationStatus: undefined, operationId: undefined, outcomeRecorded: false,
    })))
    component.selfTest(runtime)
    expect(component.selfTestNeedsReconciliation(runtime)).toBeFalse()
    expect(component.canSelfTest(runtime)).toBeTrue()
  })

  it('labels recovered read-only discovery without claiming execution verification or blocking health checks', () => {
    const { component } = make()
    const discovery: IRuntimeAttempt = {
      id: 'discovery', runtimeId: 'openclaw', status: 'succeeded', detail: 'Read-only discovery recovered.',
      discoveryRecovered: true, verificationPassed: false, createdAt: '2026-10-01T00:00:00Z',
    }
    const runtime = { ...safeRuntime(discovery), info: { ...safeRuntime().info, id: 'openclaw' } }
    expect(component.selfTestAttemptLabel(discovery)).toContain('execution not verified')
    expect(component.selfTestAttemptColor(discovery)).toBe('blue')
    expect(component.canSelfTest(runtime)).toBeTrue()
  })

  it('keeps recovered interrupted and nonterminal attempts fenced without requiring a new test', () => {
    const { component, runtimeService } = make()
    runtimeService.overview.and.returnValue(of({ runtimes: [safeRuntime(completedAttempt({
      status: 'inconclusive', operationStatus: 'interrupted', interrupted: true,
      verificationPassed: false, reconciliationRequired: true,
    }))] }))
    component.refresh()
    component.selfTest(component.runtimes[0])
    expect(runtimeService.selfTest).not.toHaveBeenCalled()
    expect(component.selfTestOutcomeSummary(component.runtimes[0])).toContain('self-test-operation')
    expect(component.canSelfTest(safeRuntime(completedAttempt({ status: 'running', operationStatus: 'verifying' })))).toBeFalse()
  })

  it('renders unresolved self-test evidence in Basic and disables the disclosed Self-test control', async () => {
    const { runtimeService, mcpService, notification, http } = make()
    runtimeService.overview.and.returnValue(of({ runtimes: [safeRuntime(completedAttempt({
      status: 'inconclusive', verificationPassed: false, reconciliationRequired: true,
      receipt: { output: { artifactHash: 'visible-hash', boundedOutput: 'api_key=synthetic-secret' } },
    }))] }))
    await TestBed.configureTestingModule({
      declarations: [RuntimeLabComponent],
      imports: [CommonModule, ControlRoomModule, RouterTestingModule, NzButtonModule, NzCardModule, NzIconModule, NzTagModule],
      schemas: [CUSTOM_ELEMENTS_SCHEMA],
      providers: [
        { provide: HttpClient, useValue: http }, { provide: RuntimeLabService, useValue: runtimeService },
        { provide: MCPPreflightService, useValue: mcpService }, { provide: NzNotificationService, useValue: notification },
      ],
    }).compileComponents()
    const fixture = TestBed.createComponent(RuntimeLabComponent)
    fixture.detectChanges()
    const root = fixture.nativeElement as HTMLElement
    expect(root.querySelector('.rl__basic-status')?.textContent).toContain('visible-hash')
    expect(root.textContent).not.toContain('synthetic-secret')
    expect(root.querySelector('#runtime-inventory')).toBeNull()
    TestBed.inject(ModuleViewPreferencesService).setMode('runtime-lab', 'advanced')
    fixture.detectChanges()
    Array.from(root.querySelectorAll<HTMLButtonElement>('.hai-progressive-section__summary'))
      .find((button) => button.textContent?.includes('Runtime inventory and controls'))?.click()
    fixture.detectChanges()
    const selfTest = Array.from(root.querySelectorAll<HTMLButtonElement>('button'))
      .find((button) => button.textContent?.trim() === 'Self-test')
    expect(selfTest?.disabled).toBeTrue()
    expect(root.textContent).toContain('Completion not verified')
    fixture.destroy()
  })

  it('keeps advanced details closed by default and names the Back control accessibly', async () => {
    const { runtimeService, mcpService, notification, http } = make()
    await TestBed.configureTestingModule({
      declarations: [RuntimeLabComponent],
      imports: [CommonModule, ControlRoomModule, RouterTestingModule, NzButtonModule, NzCardModule, NzIconModule, NzTagModule],
      schemas: [CUSTOM_ELEMENTS_SCHEMA],
      providers: [
        { provide: HttpClient, useValue: http },
        { provide: RuntimeLabService, useValue: runtimeService },
        { provide: MCPPreflightService, useValue: mcpService },
        { provide: NzNotificationService, useValue: notification },
      ],
    }).compileComponents()
    const fixture: ComponentFixture<RuntimeLabComponent> = TestBed.createComponent(RuntimeLabComponent)
    fixture.detectChanges()
    const root = fixture.nativeElement as HTMLElement
    const back = root.querySelector<HTMLButtonElement>('button[aria-label="Back to Control Center"]')

    expect(back?.title).toBe('Back to Control Center')
    expect(root.querySelector('#runtime-feature-parity')).toBeNull()
    expect(root.querySelector('#runtime-mcp-readiness')).toBeNull()
    TestBed.inject(ModuleViewPreferencesService).setMode('runtime-lab', 'advanced')
    fixture.detectChanges()
    const parity = root.querySelector<HTMLElement>('#runtime-feature-parity')
    const mcp = root.querySelector<HTMLElement>('#runtime-mcp-readiness')
    expect(parity?.classList.contains('hai-progressive-section--advanced')).toBeTrue()
    expect(parity?.querySelector('.hai-progressive-section__summary')?.getAttribute('aria-expanded')).toBe('false')
    expect(root.querySelector('#runtime-inventory-openclaw')).toBeNull()
    expect(mcp?.classList.contains('hai-progressive-section--advanced')).toBeTrue()
    expect(mcp?.querySelector('.hai-progressive-section__summary')?.getAttribute('aria-expanded')).toBe('false')
    fixture.destroy()
  });

  it('keeps blocked runtime status, reason, and safe probe action visible in Basic', async () => {
    const { runtimeService, mcpService, notification, http } = make()
    runtimeService.overview.and.returnValue(of({ runtimes: [{
      info: { id: 'hermes', displayName: 'Hermes', kind: 'agent', description: 'External runtime.' },
      status: 'blocked', claimLevel: 'declared', canExecute: false, capabilities: [],
      lastAttempt: { status: 'blocked', detail: 'Runtime credentials are not configured.' },
    }] }))
    await TestBed.configureTestingModule({
      declarations: [RuntimeLabComponent],
      imports: [CommonModule, ControlRoomModule, RouterTestingModule, NzButtonModule, NzCardModule, NzIconModule, NzTagModule],
      schemas: [CUSTOM_ELEMENTS_SCHEMA],
      providers: [
        { provide: HttpClient, useValue: http },
        { provide: RuntimeLabService, useValue: runtimeService },
        { provide: MCPPreflightService, useValue: mcpService },
        { provide: NzNotificationService, useValue: notification },
      ],
    }).compileComponents()
    const fixture = TestBed.createComponent(RuntimeLabComponent)
    fixture.detectChanges()
    const root = fixture.nativeElement as HTMLElement

    expect(root.querySelector('.rl__basic-status')?.textContent).toContain('Hermes')
    expect(root.querySelector('.rl__basic-status')?.textContent).toContain('Runtime credentials are not configured.')
    expect(Array.from(root.querySelectorAll('button')).some((button) => button.textContent?.includes('Check runtime'))).toBeTrue()
    expect(root.querySelector('#runtime-inventory')).toBeNull()
    fixture.destroy()
  });

  it('shows the authoritative DeepSeek sandbox block and offers no enable or start control', async () => {
    const { runtimeService, mcpService, notification, http } = make()
    const reason = 'DeepSeek Harness execution is blocked: HAI has no verified OS-enforced sandbox for this runtime'
    runtimeService.overview.and.returnValue(of({ runtimes: [{
      info: { id: 'deepseek-harness', displayName: 'DeepSeek Harness', kind: 'agent', description: 'Governed runtime.' },
      status: 'blocked', claimLevel: 'configured', canExecute: false, capabilities: [],
      setupRequirements: [{ step: 'Do not show this generic instruction', detail: 'Set OPENCLAW_AGENT_ENABLED=true.' }],
    }] }))
    http.get.and.returnValue(of([{
      id: 'deepseek-harness',
      name: 'DeepSeek Harness',
      type: 'deepseek_harness',
      enabled: true,
      configured: false,
      executionEnabled: false,
      requiresApproval: true,
      readOnlyDefault: true,
      capabilities: [],
      missingConfiguration: [reason],
    }]))
    await TestBed.configureTestingModule({
      declarations: [RuntimeLabComponent],
      imports: [CommonModule, ControlRoomModule, RouterTestingModule, NzButtonModule, NzCardModule, NzIconModule, NzTagModule],
      schemas: [CUSTOM_ELEMENTS_SCHEMA],
      providers: [
        { provide: HttpClient, useValue: http },
        { provide: RuntimeLabService, useValue: runtimeService },
        { provide: MCPPreflightService, useValue: mcpService },
        { provide: NzNotificationService, useValue: notification },
      ],
    }).compileComponents()
    const fixture = TestBed.createComponent(RuntimeLabComponent)
    fixture.detectChanges()
    const root = fixture.nativeElement as HTMLElement
    const basicStatus = root.querySelector('.rl__basic-status')
    expect(root.querySelector('#runtime-inventory')).toBeNull()
    TestBed.inject(ModuleViewPreferencesService).setMode('runtime-lab', 'advanced')
    fixture.detectChanges()
    const inventoryToggle = Array.from(root.querySelectorAll<HTMLButtonElement>('.hai-progressive-section__summary'))
      .find((button) => button.textContent?.includes('Runtime inventory and controls'))
    inventoryToggle?.click()
    fixture.detectChanges()
    const deepSeekCard = Array.from(root.querySelectorAll<HTMLElement>('.rl__card'))
      .find((card) => card.textContent?.includes('DeepSeek Harness'))

    expect(basicStatus?.textContent).toContain('DeepSeek Harness')
    expect(basicStatus?.textContent).toContain('blocked')
    expect(basicStatus?.textContent).toContain('Configured: No')
    expect(basicStatus?.textContent).toContain(reason)
    expect(basicStatus?.textContent).toContain('safety-policy block')
    expect(http.get).toHaveBeenCalledWith('/api/v1/agent-runtimes/')
    expect(deepSeekCard?.textContent).toContain('Configured')
    expect(deepSeekCard?.textContent).toContain('No')
    expect(deepSeekCard?.textContent).toContain(reason)
    expect(deepSeekCard?.textContent).not.toContain('Set OPENCLAW_AGENT_ENABLED=true')
    expect(Array.from(root.querySelectorAll('button')).some((button) => /enable|start|self-test/i.test(button.textContent || ''))).toBeFalse()
    expect(Array.from(basicStatus?.querySelectorAll('button') ?? []).some((button) => button.textContent?.includes('Check runtime'))).toBeFalse()
    fixture.destroy()
  });

  it('renders only aggregate prepared-model availability for a discovered OpenClaw gateway', () => {
    const { component } = make();

    expect(component.preparedModelCatalogLabel({
      gatewayPreparedModelCatalog: {
        sampledModels: 4,
        availableModels: 2,
        unavailableModels: 1,
        unknownAvailabilityModels: 1,
      },
    } as any)).toBe('2 available · 1 unavailable · 1 unknown');
  });

  it('renders OpenClaw task and capability discovery without identifiers or payloads', () => {
    const { component } = make();
    const discovery: any = {
      gatewayTaskLedger: {
        sampledTasks: 3,
        statusCounts: { running: 2, completed: 1 },
        truncated: false,
      },
      gatewayCapabilityCatalog: {
        sampledSkills: 4,
        eligibleSkills: 3,
        sampledCommands: 2,
        toolCountsBySource: { core: 5, plugin: 1 },
      },
    };

    expect(component.taskLedgerLabel(discovery)).toBe('3 sampled · complete sample');
    expect(component.capabilityCatalogLabel(discovery)).toBe('3 of 4 skills eligible · 6 tools · 2 commands');
  });

  it('renders the OpenClaw agent roster as aggregate availability only', () => {
    const { component } = make();

    expect(component.agentRosterLabel({
      gatewayAgentRoster: {
        sampledAgents: 4,
        agentCount: 2,
        systemCount: 1,
        unknownKindCount: 1,
      },
    } as any)).toBe('2 agents · 1 system · 1 legacy/unknown');
  });

  it('labels and counts runtime dispositions for progressive inspection', () => {
    const { component } = make();
    const inventory: any = {
      features: [
        { implementationStatus: 'implemented', disposition: 'already_present' },
        { implementationStatus: 'not_implemented', disposition: 'deferred' },
        { implementationStatus: 'not_implemented', disposition: 'blocked_external' },
      ],
    };

    expect(component.implementationCount(inventory, 'implemented')).toBe(1);
    expect(component.backlogCount(inventory)).toBe(2);
    expect(component.dispositionColor('constrained_unsafe')).toBe('red');
    expect(component.dispositionLabel('adapted_for_hai')).toBe('adapted for hai');
  });

  it('runs only the selected configured MCP preflight', () => {
    const { component, mcpService, notification } = make();
    mcpService.run.and.returnValue(of({ status: 'ready', toolCount: 3, detail: 'No tool was called.' }));
    component.runMCPPreflight({ id: 'github', catalogName: 'GitHub MCP Server', configured: true });

    expect(mcpService.run).toHaveBeenCalledWith('github');
    expect(notification.success).toHaveBeenCalledWith('MCP server ready', 'GitHub MCP Server: 3 declared tool(s) inspected. No tool was called.');
  });

  it('does not run an unconfigured MCP server', () => {
    const { component, mcpService } = make();
    component.runMCPPreflight({ id: 'github', configured: false });

    expect(mcpService.run).not.toHaveBeenCalled();
  });
});
