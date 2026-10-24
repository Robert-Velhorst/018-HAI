import { fakeAsync, tick } from '@angular/core/testing'
import { EMPTY, of, Subject } from 'rxjs'
import { BrainCatalogComponent } from './brain-catalog.component'

describe('Catalog request observation lifecycle', () => {
  let component: BrainCatalogComponent
  let service: any
  let pursuit: any
  let providers: any[]
  let migration: any
  const entry = { id: 'synthetic-candidate', name: 'Synthetic', status: 'candidate', requiresApproval: true } as any
  const discovery = { repository: 'synthetic/repository' } as any
  const statusNames = ['RAGFlow', 'AnythingLLM', 'Presidio', 'Langfuse', 'Serena', 'OpenLIT', 'MLflow', 'MiniSWE', 'Gitleaks', 'Gosec', 'Trivy', 'Grype', 'Syft', 'BrowserVerification']
  const cases = [
    ['overview', 'loading', () => component.refresh(), () => service.overview],
    ['skill inventory', 'loadingSkillInventory', () => component.loadSkillInventory(), () => service.skillInventory],
    ['roadmap', 'loadingAdoptionPlan', () => component.loadAdoptionPlan(), () => service.adoptionPlan],
    ['upstream', 'revalidatingId', () => component.revalidate(entry), () => service.revalidate],
    ['collections', 'checkingOSSInsight', () => component.revalidateOSSInsightCollections(), () => service.revalidateOSSInsightCollections],
    ['maintenance', 'runningCatalogRevalidation', () => component.runDueCatalogRevalidations(), () => service.runDueRevalidations],
    ['collection history', 'collectionHistoryLoading', () => (component as any).loadCollectionMaintenanceHistory(), () => service.collectionRevalidationHistory],
    ['repository history', 'repositoryHistoryLoading', () => (component as any).loadRepositoryDiscoveryMaintenanceHistory(), () => service.repositoryDiscoveryRevalidationHistory],
    ['discovery', 'discoveringOSSInsight', () => component.discoverOSSInsightRepositories(), () => service.discoverOSSInsightRepositories],
    ['reviewable discovery', 'discoveringOSSInsight', () => component.discoverReviewableOSSInsightRepositories(), () => service.discoverReviewableOSSInsightRepositories],
    ['verify discovery', 'verifyingDiscoveryRepository', () => component.verifyDiscovery(discovery), () => service.revalidateOSSInsightDiscovery],
    ['recommendations', 'recommendingCapabilities', () => component.recommendCapabilities('Read-only evidence'), () => service.recommendCapabilities],
    ['migration', 'planningFrameworkMigration', () => component.prepareFrameworkMigration('{"workloadId":"synthetic","events":[{}]}'), () => migration.microsoftAgentFrameworkMigrationPlan],
    ['probe', 'probingLangfuse', () => component.probeLangfuse(), () => providers[3].probe],
    ['Langfuse export', 'exportingLangfuse', () => component.exportLangfuseSnapshot(), () => providers[3].exportOperationalSnapshot],
    ['OpenLIT export', 'exportingOpenLIT', () => component.exportOpenLITSnapshot(), () => providers[5].exportOperationalSnapshot],
    ['adapter review', 'reviewingCandidateId', () => component.startAdapterReview(entry), () => pursuit.create],
    ['discovery review', 'reviewingDiscoveryRepository', () => component.queueDiscoveryReview(discovery), () => pursuit.create],
    ...statusNames.map((name, index) => [name + ' status', 'loading' + name + 'Status', () => (component as any)['load' + name + 'Status'](), () => providers[index].status]),
  ] as Array<[string, string, () => void, () => jasmine.Spy]>

  beforeEach(() => {
    service = jasmine.createSpyObj('catalog', ['overview', 'skillInventory', 'adoptionPlan', 'revalidate', 'revalidateOSSInsightCollections', 'runDueRevalidations', 'collectionRevalidationHistory', 'repositoryDiscoveryRevalidationHistory', 'discoverOSSInsightRepositories', 'discoverReviewableOSSInsightRepositories', 'revalidateOSSInsightDiscovery', 'recommendCapabilities'])
    pursuit = jasmine.createSpyObj('pursuit', ['create'])
    providers = statusNames.map(() => jasmine.createSpyObj('profile', ['status', 'probe', 'exportOperationalSnapshot']))
    migration = jasmine.createSpyObj('migration', ['microsoftAgentFrameworkMigrationPlan'])
    const notifications = jasmine.createSpyObj('notifications', ['success', 'error', 'info'])
    const router = jasmine.createSpyObj('router', ['navigate'])
    component = Reflect.construct(BrainCatalogComponent, [service, pursuit, ...providers, migration, notifications, router, { get: () => ({ openSections: {} }) }]) as BrainCatalogComponent
    component.discoveryReviews[discovery.repository] = { available: true, archived: false } as any
  })
  afterEach(() => (component as any).ngOnDestroy?.())

  for (const [name, flag, invoke, getSpy] of cases) {
    it(`contains synchronous ${name} setup errors and releases its busy flag`, () => {
      getSpy().and.callFake(() => { throw new Error('PRIVATE_SETUP_DETAIL') })
      expect(invoke).not.toThrow()
      expect(Boolean((component as any)[flag])).toBeFalse()
    })
    it(`rejects empty ${name} completion without retaining a busy state`, () => {
      getSpy().and.returnValue(EMPTY)
      invoke()
      expect(Boolean((component as any)[flag])).toBeFalse()
    })
    it(`releases pending ${name} observations on destruction`, () => {
      const pending = new Subject<never>()
      getSpy().and.returnValue(pending)
      invoke()
      expect(pending.observers.length).toBe(1)
      ;(component as any).ngOnDestroy?.()
      expect(pending.observers.length).toBe(0)
      expect(Boolean((component as any)[flag])).toBeFalse()
    })
    it(`does not dispatch ${name} after destruction`, () => {
      component.ngOnDestroy()
      invoke()
      expect(getSpy()).not.toHaveBeenCalled()
      expect(Boolean((component as any)[flag])).toBeFalse()
    })
    it(`bounds silent ${name} observation without automatic retry`, fakeAsync(() => {
      const pending = new Subject<never>()
      getSpy().and.returnValue(pending)
      invoke()
      tick(30001)
      expect(pending.observers.length).toBe(0)
      expect(Boolean((component as any)[flag])).toBeFalse()
      expect(getSpy()).toHaveBeenCalledTimes(1)
    }))
  }

  for (const [index, name] of statusNames.entries()) {
    it(`accepts only the first ${name} status response`, () => {
      const pending = new Subject<any>()
      providers[index].status.and.returnValue(pending)
      ;(component as any)['load' + name + 'Status']()
      pending.next({ configured: false })
      expect(pending.observers.length).toBe(0)
      expect((component as any)['loading' + name + 'Status']).toBeFalse()
    })
  }

  for (const [name, , invoke, getSpy] of cases.filter(([name]) => ['Langfuse export', 'OpenLIT export', 'migration'].includes(name))) {
    it(`does not clear the selected profile while ${name} is pending`, () => {
      const pending = new Subject<never>()
      getSpy().and.returnValue(pending)
      component.select(entry)
      invoke()
      component.select({ ...entry, id: 'other' })
      expect(component.selected).toBe(entry)
      expect(pending.observers.length).toBe(1)
      expect(getSpy()).toHaveBeenCalledTimes(1)
    })
  }

  it('supersedes an old profile observation even after selecting away and back', () => {
    const old = new Subject<any>(); const fresh = new Subject<any>()
    providers[0].status.and.returnValues(old, fresh)
    component.select({ ...entry, id: 'ragflow' })
    component.loadRAGFlowStatus()
    component.select({ ...entry, id: 'other' })
    component.select({ ...entry, id: 'ragflow' })
    component.loadRAGFlowStatus()
    expect(old.observers.length).toBe(0)
    expect(fresh.observers.length).toBe(1)
    old.next({ configured: true })
    expect(component.ragflowStatus).toBeUndefined()
    const current = { configured: false }
    fresh.next(current)
    expect(component.ragflowStatus).toBe(current as any)
    expect(fresh.observers.length).toBe(0)
  })

  it('bounds a silent profile read and permits a deliberate retry', fakeAsync(() => {
    const pending = new Subject<any>()
    providers[0].status.and.returnValue(pending)
    component.loadRAGFlowStatus()
    tick(6001)
    expect(pending.observers.length).toBe(0)
    expect(component.loadingRAGFlowStatus).toBeFalse()
    providers[0].status.and.returnValue(of({ configured: false }))
    component.loadRAGFlowStatus()
    expect(component.ragflowStatus?.configured).toBeFalse()
    expect(component.ragflowStatusUnavailable).toBeFalse()
  }))
})
