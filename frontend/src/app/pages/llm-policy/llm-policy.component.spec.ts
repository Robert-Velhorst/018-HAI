import { FormBuilder } from '@angular/forms'
import { Router } from '@angular/router'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import { of } from 'rxjs'
import { ILLMPolicy } from '../../models/llm-policy.model.interface'
import { ILLMPolicyService } from '../../services/llm-policy.service.interface'
import { ThemeService } from '../../services/theme.service'
import { LLMPolicyComponent } from './llm-policy.component'

describe('LLMPolicyComponent', () => {
  function createHarness(): {
    component: LLMPolicyComponent
    service: jasmine.SpyObj<ILLMPolicyService>
  } {
    const themeService = jasmine.createSpyObj<ThemeService>(
      'ThemeService',
      ['mode', 'toggle', 'label', 'icon']
    )
    themeService.mode.and.returnValue('dark')
    themeService.icon.and.returnValue('star')

    const policy: ILLMPolicy = {
      dailyPaidBudgetEur: 0,
      paidCallsAllowed: false,
      localModelsAllowed: true,
      freeCloudQuotaAllowed: true,
      localFirst: true,
      cacheRepeatedPrompts: true,
      routeSimpleTasksToSmallModels: true,
      routeComplexTasksToBestAvailableFreeModel: true,
      requireApprovalBeforePaidUsage: true,
      requireRecentLiveProviderProbe: true,
      providerProbeMaxAgeSeconds: 300,
      tierOrder: ['local', 'free', 'cheap', 'acceptable', 'high', 'premium', 'expensive'],
      dailyBudgetUsedEur: 0,
      inputTokensUsed: 0,
      outputTokensUsed: 0,
      providers: [],
      inferenceInfrastructure: {
        kvCacheLoadStrategy: 'disabled',
        disaggregatedServingVerified: false,
        dualPathInfrastructureAvailable: false,
        reason: 'Not configured.',
      },
    }
    const service = jasmine.createSpyObj<ILLMPolicyService>('ILLMPolicyService', [
      'getPolicy',
      'probeProviders',
      'getProbeHistory',
      'getModelMaintenanceHistory',
      'runDueModelMaintenance',
      'routeTask',
      'getLogs',
      'getGenerationHistory',
    ])
    service.getPolicy.and.returnValue(of(policy))
    service.getProbeHistory.and.returnValue(of([]))
    service.getModelMaintenanceHistory.and.returnValue(of([]))
    service.getLogs.and.returnValue(of([]))
    service.getGenerationHistory.and.returnValue(of([]))

    const component = new LLMPolicyComponent(
      new FormBuilder(),
      service,
      {} as NzNotificationService,
      {} as Router,
      themeService
    )
    return { component, service }
  }

  function createComponent(): LLMPolicyComponent {
    return createHarness().component
  }

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

  it('distinguishes configured endpoints from catalog-enabled integrations', () => {
    const component = createComponent()
    const policy = {
      requireRecentLiveProviderProbe: false,
      providers: [
        { id: 'configured', enabled: true, configured: true, models: [] },
        { id: 'catalog-only', enabled: true, configured: false, models: [] },
        { id: 'disabled', enabled: false, configured: false, models: [] },
      ],
    } as any

    expect(component.enabledProviderCount(policy)).toBe(2)
    expect(component.configuredProviderCount(policy)).toBe(1)
    expect(component.configuredProviderSummary(policy)).toBe('1 / 3')
    expect(component.strictProbePolicyLabel(policy)).toBe('probe optional')
  })

  it('loads only current policy in the basic view', () => {
    const { component, service } = createHarness()

    component.refresh()

    expect(service.getPolicy).toHaveBeenCalledTimes(1)
    expect(service.getProbeHistory).not.toHaveBeenCalled()
    expect(service.getModelMaintenanceHistory).not.toHaveBeenCalled()
    expect(service.getLogs).not.toHaveBeenCalled()
    expect(service.getGenerationHistory).not.toHaveBeenCalled()
  })

  it('loads all audit evidence once when routing audit opens', () => {
    const { component, service } = createHarness()

    component.onAuditToggle({ target: { open: true } } as any)
    component.onAuditToggle({ target: { open: false } } as any)
    component.onAuditToggle({ target: { open: true } } as any)

    expect(service.getProbeHistory).toHaveBeenCalledTimes(1)
    expect(service.getModelMaintenanceHistory).toHaveBeenCalledTimes(1)
    expect(service.getLogs).toHaveBeenCalledTimes(1)
    expect(service.getGenerationHistory).toHaveBeenCalledTimes(1)
  })

  it('loads only probe readiness when provider inventory opens', () => {
    const { component, service } = createHarness()

    component.onProviderInventoryToggle({ target: { open: true } } as any)

    expect(service.getProbeHistory).toHaveBeenCalledTimes(1)
    expect(service.getModelMaintenanceHistory).not.toHaveBeenCalled()
    expect(service.getLogs).not.toHaveBeenCalled()
    expect(service.getGenerationHistory).not.toHaveBeenCalled()
  })

  it('reloads visible audit evidence during an explicit refresh', () => {
    const { component, service } = createHarness()

    component.onAuditToggle({ target: { open: true } } as any)
    component.refresh()

    expect(service.getPolicy).toHaveBeenCalledTimes(1)
    expect(service.getProbeHistory).toHaveBeenCalledTimes(2)
    expect(service.getModelMaintenanceHistory).toHaveBeenCalledTimes(2)
    expect(service.getLogs).toHaveBeenCalledTimes(2)
    expect(service.getGenerationHistory).toHaveBeenCalledTimes(2)
  })
})
