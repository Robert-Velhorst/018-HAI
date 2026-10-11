import { fakeAsync, tick } from '@angular/core/testing'
import { Subject, throwError } from 'rxjs'
import { ControlCenterComponent } from './control-center.component'

describe('ControlCenterComponent', () => {
  function createComponent(
    agentCycle: { run: jasmine.Spy },
    automations: Record<string, jasmine.Spy> = {}
  ) {
    const notifications = {
      error: jasmine.createSpy('error'),
      success: jasmine.createSpy('success'),
      warning: jasmine.createSpy('warning'),
    }

    const component = new ControlCenterComponent(
      automations as any,
      {} as any,
      {} as any,
      agentCycle as any,
      {} as any,
      {} as any,
      {} as any,
      {} as any,
      notifications as any,
      {} as any
    )

    return { component, notifications }
  }

  it('does not start overlapping operating-brief refreshes', () => {
    const run = jasmine.createSpy('run')
    const response = new Subject<any>()
    run.and.returnValue(response.asObservable())
    const { component } = createComponent({ run })

    component.runScan()
    component.runScan()

    expect(run).toHaveBeenCalledTimes(1)
    expect(component.scanning).toBeTrue()

    response.error({ error: { error: 'backend unavailable' } })
    expect(component.scanning).toBeFalse()
  })

  it('releases the running state when the operating-brief request times out', fakeAsync(() => {
    const run = jasmine.createSpy('run')
    run.and.returnValue(new Subject<any>().asObservable())
    const { component, notifications } = createComponent({ run })

    component.runScan()
    tick(30000)

    expect(component.scanning).toBeFalse()
    expect(notifications.error).toHaveBeenCalledWith(
      'Agent cycle failed',
      'The operational cycle could not complete.'
    )
  }))

  it('does not duplicate an automation health check while the first request is pending', () => {
    const run = jasmine.createSpy('run')
    run.and.returnValue(new Subject<any>().asObservable())
    const healthCheck = jasmine.createSpy('runHealthCheck')
    const response = new Subject<any>()
    healthCheck.and.returnValue(response.asObservable())
    const { component } = createComponent({ run }, { runHealthCheck: healthCheck })
    const automation = { id: 'automation-1', name: 'Daily check' } as any

    component.runHealthCheck(automation)
    component.runHealthCheck(automation)

    expect(healthCheck).toHaveBeenCalledTimes(1)
    expect(component.isChecking(automation)).toBeTrue()

    response.error({ error: 'timeout' })
    expect(component.isChecking(automation)).toBeFalse()
  })

  it('does not present an unloaded automation registry as zero registered automations', () => {
    const run = jasmine.createSpy('run').and.returnValue(new Subject<any>().asObservable())
    const { component } = createComponent({ run })

    const beforeLoading = (component as any).buildCommandActions()
      .find((action: any) => action.id === 'automation')
    expect(beforeLoading.primaryMetric).toBe('Open registry')

    component.diagnosticsLoaded = true
    component.automations = [{ id: 'automation-1' } as any]
    const afterLoading = (component as any).buildCommandActions()
      .find((action: any) => action.id === 'automation')
    expect(afterLoading.primaryMetric).toBe('1 registered')
  })

  it('does not present failed dashboard data as an empty workload', () => {
    const run = jasmine.createSpy('run').and.returnValue(new Subject<any>().asObservable())
    const { component } = createComponent({ run })
    ;(component as any).workflowService = {
      dashboard: () => throwError(() => new Error('workflow unavailable')),
    }
    ;(component as any).ambientService = {
      overview: () => throwError(() => new Error('ambient unavailable')),
    }
    ;(component as any).pursuitService = {
      dashboard: () => throwError(() => new Error('pursuit unavailable')),
    }

    component.refresh()

    expect(component.dashboardLoadErrors).toEqual([
      'Workflow status',
      'Ambient scan',
      'Pursuit status',
    ])
    expect(component.hasDashboardLoadError()).toBeTrue()
    expect(component.hasLiveWork()).toBeTrue()
  })

  it('does not present failed diagnostics as an empty automation registry', () => {
    const run = jasmine.createSpy('run').and.returnValue(new Subject<any>().asObservable())
    const { component } = createComponent({ run })
    ;(component as any).automationsService = {
      getAutomations: () => throwError(() => new Error('automations unavailable')),
      getHealthSummary: () => throwError(() => new Error('health unavailable')),
    }
    ;(component as any).agentRuntimeService = {
      overview: () => throwError(() => new Error('runtimes unavailable')),
    }

    component.loadDiagnosticsData()

    expect(component.diagnosticsLoadErrors).toEqual([
      'Automation registry',
      'Automation health',
      'Runtime overview',
    ])
    expect(component.hasDiagnosticsLoadError()).toBeTrue()
    const automation = (component as any).buildCommandActions()
      .find((action: any) => action.id === 'automation')
    expect(automation.primaryMetric).toBe('Unavailable')
  })
})
