import { FormBuilder } from '@angular/forms'
import { Router } from '@angular/router'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service'
import { of, throwError } from 'rxjs'
import { ILLMPolicyService } from '../../services/llm-policy.service.interface'
import { ThemeService } from '../../services/theme.service'
import { LLMPolicyComponent } from './llm-policy.component'

describe('LLMPolicyComponent', () => {
  function createComponent(viewPreferences = new ModuleViewPreferencesService(document)): LLMPolicyComponent {
    const themeService = jasmine.createSpyObj<ThemeService>(
      'ThemeService',
      ['mode', 'toggle', 'label', 'icon']
    )
    themeService.mode.and.returnValue('dark')
    themeService.icon.and.returnValue('star')
    const notification = jasmine.createSpyObj<NzNotificationService>(
      'NzNotificationService',
      ['success', 'warning', 'error']
    )

    return new LLMPolicyComponent(
      new FormBuilder(),
      {} as ILLMPolicyService,
      notification,
      {} as Router,
      themeService,
      viewPreferences
    )
  }

  it('persists Advanced state and disclosures only for the LLM policy module', () => {
    localStorage.removeItem('hai.module-view.v1.llm-policy')
    const preferences = new ModuleViewPreferencesService(document)
    const component = createComponent(preferences)
    preferences.setMode('llm-policy', 'advanced')
    preferences.setSection('llm-policy', 'model-catalog', true)

    expect(component.isAdvanced).toBeTrue()
    expect(new ModuleViewPreferencesService(document).get('llm-policy').openSections['model-catalog']).toBeTrue()
    expect(preferences.get('system-status').mode).toBe('basic')
    localStorage.removeItem('hai.module-view.v1.llm-policy')
  })

  it('summarizes provider health from policy and the latest persisted probe', () => {
    const component = createComponent()
    const policy = {
      requireRecentLiveProviderProbe: true,
      providerProbeMaxAgeSeconds: 300,
      providers: [
        { id: 'local', name: 'Local runtime', enabled: true, configured: true, local: true, paid: false, models: [] },
        { id: 'cloud', name: 'Cloud runtime', enabled: true, configured: true, local: false, paid: false, models: [] },
        { id: 'off', name: 'Disabled', enabled: false, configured: false, local: false, paid: false, models: [] },
      ],
    } as any
    component.policy = policy
    component.probes = [{
      providerId: 'local', providerName: 'Local runtime', status: 'ready', reason: '',
      modelsSeen: 1, durationMs: 4, live: true, requiresReview: false, checkedAt: new Date().toISOString(),
    }]

    expect(component.providerHealthSummary(policy)).toBe('2 enabled · 2 configured · 1 live probes')
    expect(component.providerProbeStatus('local')).toBe('live')
    expect(component.providersNeedingAttention.map((provider) => provider.id)).toEqual(['cloud'])
    expect(component.providerAttentionReason(policy.providers[1])).toContain('has not been recorded')
  })

  it('uses the centrally registered theme icon', () => {
    const component = createComponent()

    expect(component.themeIcon()).toBe('star')
  })

  it('explains conservative validator evidence for a routed model', () => {
    const component = createComponent()

    expect(component.calibrationSummary({
      calibration: {
        lane: 'recursive_deep_review',
        evaluatedRuns: 10,
        acceptedOutputs: 8,
        acceptanceRate: 0.8,
        wilsonLowerBound: 0.49,
        confidence: 'medium',
      },
    } as any)).toBe(
      '8/10 validator-accepted outputs, 49.0% conservative lower bound, medium confidence.'
    )
  })

  it('keeps trusted, review, failed, and unevaluated outcomes visually distinct', () => {
    const component = createComponent()

    expect(component.validationLabel('source_supported')).toBe('source supported')
    expect(component.validationColor('test_passed')).toBe('green')
    expect(component.validationColor('needs_review')).toBe('gold')
    expect(component.validationColor('failed')).toBe('red')
    expect(component.validationColor()).toBe('default')
  })

  it('labels daily usage provenance and does not present failed durable totals as zero', () => {
    const component = createComponent()
    const policy = {
      dailyBudgetUsedEur: 0,
      dailyPaidBudgetEur: 25,
      inputTokensUsed: 0,
      outputTokensUsed: 0,
      usageAccountingStatus: 'unavailable',
    } as any

    expect(component.policyBudgetText(policy)).toBe('Usage unavailable / max EUR 25')
    expect(component.usageAccountingLabel('durable')).toBe('Stored generation history')
    expect(component.usageAccountingLabel('process_only')).toContain('Current process only')
    expect(component.usageAccountingLabel('unavailable')).toContain('could not be read')
    expect(component.policyBudgetTooltip(policy)).toContain('Stored totals could not be read')
  })

  it('does not claim a maintenance check is scheduled when the due time is missing or invalid', () => {
    const component = createComponent()

    expect(component.maintenanceScheduleText({} as any)).toBe(
      'Next check time was not provided by the maintenance service.'
    )
    expect(component.maintenanceScheduleText({ nextCheckDueAt: 'not-a-date' } as any)).toBe(
      'Next check time was not provided in a valid format.'
    )
  })

  it('identifies an overdue model maintenance check instead of presenting it as upcoming', () => {
    const component = createComponent()
    const overdue = new Date(Date.now() - 60_000)

    expect(component.maintenanceScheduleText({ nextCheckDueAt: overdue.toISOString() } as any)).toBe(
      `Next check is overdue (due ${overdue.toLocaleString()}).`
    )
  })

  it('reports a cancelled maintenance sweep as stopped instead of successful', () => {
    const component = createComponent()
    const service = (component as any).llmPolicyService
    const notification = (component as any).notification
    service.runDueModelMaintenance = jasmine.createSpy('runDueModelMaintenance').and.returnValue(of({
      eligible: 1,
      checked: 1,
      providerManaged: 0,
      healthOnly: 0,
      reused: 0,
      updated: 0,
      inProgress: 0,
      failed: 1,
      cancelled: true,
      results: [],
      runAt: new Date().toISOString(),
    }))
    service.getModelMaintenanceHistory = jasmine.createSpy('getModelMaintenanceHistory').and.returnValue(of([]))

    component.runDueModelMaintenance()

    expect(notification.warning).toHaveBeenCalledWith(
      'Model maintenance stopped',
      jasmine.stringMatching(/interrupted update remains blocked/)
    )
    expect(notification.success).not.toHaveBeenCalled()
  })

  it('distinguishes cloud catalog checks from health-only runtime checks', () => {
    const component = createComponent()
    const service = (component as any).llmPolicyService
    const notification = (component as any).notification
    service.runDueModelMaintenance = jasmine.createSpy('runDueModelMaintenance').and.returnValue(of({
      eligible: 2,
      checked: 1,
      providerManaged: 1,
      healthOnly: 1,
      reused: 0,
      updated: 0,
      inProgress: 0,
      failed: 0,
      cancelled: false,
      results: [],
      runAt: new Date().toISOString(),
    }))
    service.getModelMaintenanceHistory = jasmine.createSpy('getModelMaintenanceHistory').and.returnValue(of([]))

    component.runDueModelMaintenance()

    expect(notification.success).toHaveBeenCalledWith(
      'Daily model checks complete',
      jasmine.stringMatching(/1 checked from 2 eligible models; cloud catalog: 1; health-only runtime checks: 1/)
    )
  })

  it('does not report a distributed maintenance lease as a successful completed sweep', () => {
    const component = createComponent()
    const service = (component as any).llmPolicyService
    const notification = (component as any).notification
    service.runDueModelMaintenance = jasmine.createSpy('runDueModelMaintenance').and.returnValue(of({
      eligible: 1,
      checked: 0,
      providerManaged: 0,
      healthOnly: 0,
      reused: 0,
      updated: 0,
      inProgress: 1,
      failed: 0,
      cancelled: false,
      results: [],
      runAt: new Date().toISOString(),
    }))
    service.getModelMaintenanceHistory = jasmine.createSpy('getModelMaintenanceHistory').and.returnValue(of([]))

    component.runDueModelMaintenance()

    expect(notification.warning).toHaveBeenCalledWith(
      'Model maintenance needs review',
      jasmine.stringMatching(/1 already in progress/)
    )
    expect(notification.success).not.toHaveBeenCalled()
  })

  it('retains confirmed routing history when the next audit read is unavailable', () => {
    const component = createComponent()
    const service = (component as any).llmPolicyService
    const history = [{ id: 'route-1', selectedModelName: 'local-model' }]

    component.logs = history as any
    service.getLogs = jasmine.createSpy('getLogs').and.returnValue(
      throwError(() => new Error('routing history unavailable'))
    )

    component.loadLogs()

    expect(component.logs).toEqual(history as any)
    expect((component as any).logsUnavailable).toBeTrue()
  })
})
