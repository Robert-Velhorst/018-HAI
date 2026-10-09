import { of, Subject, throwError } from 'rxjs'
import { TestBed } from '@angular/core/testing'
import { ActivatedRoute, Router } from '@angular/router'
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service'
import { IClaimReviewQueue } from '../../models/knowledge-claim.model.interface'
import { AuthSessionService } from '../../services/auth-session.service'
import { KnowledgeClaimService } from '../../services/knowledge-claim.service'
import { KnowledgeClaimsComponent } from './knowledge-claims.component'
import { KnowledgeClaimsModule } from './knowledge-claims.module'

describe('KnowledgeClaimsComponent', () => {
  const viewPreferenceKey = 'hai.module-view.v1.knowledge-claims'
  let claims: jasmine.SpyObj<KnowledgeClaimService>
  let auth: jasmine.SpyObj<AuthSessionService>
  let notification: jasmine.SpyObj<any>
  let router: jasmine.SpyObj<any>
  let viewPreferences: jasmine.SpyObj<ModuleViewPreferencesService>
  let component: KnowledgeClaimsComponent

  beforeEach(() => window.localStorage.removeItem(viewPreferenceKey))
  afterEach(() => window.localStorage.removeItem(viewPreferenceKey))

  const queue: IClaimReviewQueue = {
    effectiveAt: '2026-08-04T10:00:00Z',
    observedBy: '2026-08-04T10:00:00Z',
    truncated: false,
    counts: { conflicting: 1 },
    items: [{
      claim: {
        id: 'claim-1', ownerIdentity: 'robert', workspaceId: '018-HAI',
        subject: 'HAI', predicate: 'status', object: 'ready',
        effectiveFrom: '2026-08-04T09:00:00Z', observedAt: '2026-08-04T09:00:00Z',
        verificationStatus: 'source_supported', provenance: [{
          referenceId: 'source-1', contentDigest: 'a'.repeat(64),
          capturedAt: '2026-08-04T09:00:00Z', localOnly: true,
        }],
        provenanceDigest: 'b'.repeat(64), sensitivity: 'internal', localOnly: true,
        claimDigest: 'c'.repeat(64), createdAt: '2026-08-04T09:00:00Z',
      },
      assessment: {
        claimId: 'claim-1', subject: 'HAI', predicate: 'status', object: 'ready',
        status: 'conflicting', effectiveAt: '2026-08-04T10:00:00Z', observedBy: '2026-08-04T10:00:00Z',
        reasons: ['another active claim has a different value'], evidenceIds: [], supportingClaimIds: [],
        conflictingClaimIds: ['claim-2'], supersedingClaimIds: [], truncated: false,
      },
    }],
  }

  beforeEach(() => {
    claims = jasmine.createSpyObj<KnowledgeClaimService>('KnowledgeClaimService', ['reviewQueue', 'lifecycle', 'correct'])
    auth = jasmine.createSpyObj<AuthSessionService>('AuthSessionService', ['session'])
    notification = jasmine.createSpyObj('NzNotificationService', ['success', 'warning', 'error'])
    router = jasmine.createSpyObj('Router', ['navigate'])
    viewPreferences = jasmine.createSpyObj<ModuleViewPreferencesService>('ModuleViewPreferencesService', ['setMode'])
    claims.reviewQueue.and.returnValue(of(queue))
    claims.lifecycle.and.returnValue(of({ claim: queue.items[0].claim, supersedes: [], supersededBy: [], conflicts: [], truncated: false }))
    auth.session.and.returnValue(of({
      authenticated: true, subject: 'robert', role: 'owner',
      permissions: { canRead: true, canOperate: true, canApprove: true, canAdminister: true },
    }))
    const route: any = {
      snapshot: { queryParamMap: { get: () => null } },
    }
    component = new KnowledgeClaimsComponent(claims, auth, notification, route, router, viewPreferences)
  })

  it('loads conflicts as the first operator attention queue', () => {
    component.ngOnInit()

    expect(component.loading).toBeFalse()
    expect(component.attentionItems.length).toBe(1)
    expect(component.visibleItems[0].claim.id).toBe('claim-1')
    expect(component.canApprove).toBeTrue()
  })

  it('revokes approval after a denied correction without losing the draft', () => {
    component.ngOnInit()
    component.openInspector(queue.items[0])
    component.beginCorrection()
    component.correctedObject = 'not ready'
    component.correctionReason = 'The source changed.'
    component.correctionConfirmed = true
    claims.correct.and.returnValue(throwError(() => ({ status: 403 })))
    component.submitCorrection()
    expect(component.canApprove).toBeFalse()
    expect(component.correctionConfirmed).toBeFalse()
    expect(component.correctedObject).toBe('not ready')
    expect(component.correctionOpen).toBeTrue()
    component.submitCorrection()
    expect(claims.correct).toHaveBeenCalledTimes(1)
    component.load()
    expect(component.canApprove).toBeTrue()
    expect(component.correctionConfirmed).toBeFalse()
  })

  it('cancels older refreshes rather than restoring a previous actor', () => {
    const oldQueue = new Subject<IClaimReviewQueue>()
    claims.reviewQueue.and.returnValue(oldQueue)
    component.load()
    expect(component.canApprove).toBeFalse()
    claims.reviewQueue.and.returnValue(of(queue))
    auth.session.and.returnValue(of({
      authenticated: true, subject: 'viewer', role: 'viewer',
      permissions: { canRead: true, canOperate: false, canApprove: false, canAdminister: false },
    }))
    component.load()
    oldQueue.next(queue)
    oldQueue.complete()
    expect(component.session?.subject).toBe('viewer')
    expect(component.canApprove).toBeFalse()
  })

  it('opens immutable lifecycle detail rather than editing a claim in place', () => {
    component.ngOnInit()
    component.openInspector(queue.items[0])

    expect(claims.lifecycle).toHaveBeenCalledWith('018-HAI', 'claim-1')
    expect(component.lifecycle?.claim.id).toBe('claim-1')
    expect(component.inspectorOpen).toBeTrue()
  })

  it('keeps the Basic approval path visible and defers register and raw evidence detail', async () => {
    await TestBed.configureTestingModule({
      imports: [KnowledgeClaimsModule],
      providers: [
        { provide: KnowledgeClaimService, useValue: claims },
        { provide: AuthSessionService, useValue: auth },
        { provide: Router, useValue: router },
        { provide: ActivatedRoute, useValue: { snapshot: { queryParamMap: { get: () => null } } } },
      ],
    }).compileComponents()
    const fixture = TestBed.createComponent(KnowledgeClaimsComponent)
    fixture.detectChanges()
    const host = fixture.nativeElement as HTMLElement

    expect(host.querySelector('.attention-summary')?.textContent).toContain('need attention')
    expect(host.querySelector('.review-panel')).toBeNull()
    expect(host.querySelector('.claim-metrics')).toBeNull()
    expect(host.querySelector('.claim-register')).toBeNull()
    expect(host.querySelector('.claim-facts')).toBeNull()

    const reviewNext = host.querySelector('.attention-summary button') as HTMLButtonElement
    expect(reviewNext?.textContent).toContain('Review next')
    reviewNext.click()
    fixture.detectChanges()
    await fixture.whenStable()
    fixture.detectChanges()
    const drawer = document.body
    expect(drawer.querySelector('.correction-panel button')?.textContent).toContain('Correct claim')
    const collapsedEvidence = Array.from(drawer.querySelectorAll('.hai-progressive-section__summary')).find((button) => button.textContent?.includes('Evidence, facts, and lifecycle'))
    expect(collapsedEvidence).toBeUndefined()
    TestBed.inject(ModuleViewPreferencesService).setMode('knowledge-claims', 'advanced')
    fixture.detectChanges()
    await fixture.whenStable()
    fixture.detectChanges()
    const advancedEvidence = Array.from(drawer.querySelectorAll('.hai-progressive-section__summary')).find((button) => button.textContent?.includes('Evidence, facts, and lifecycle'))
    expect(advancedEvidence?.getAttribute('aria-expanded')).toBe('false')
    expect(drawer.querySelector('.claim-facts')).toBeNull()

    const evidenceToggle = advancedEvidence as HTMLButtonElement
    evidenceToggle.click()
    fixture.detectChanges()
    expect(drawer.querySelector('.claim-facts')?.textContent).toContain('Verification')
    expect(drawer.querySelector('.source-record')?.textContent).toContain('source-1')
  })

  it('opens the Advanced claim register through the per-module preference and route state', () => {
    component.openClaimRegister()

    expect(viewPreferences.setMode).toHaveBeenCalledOnceWith('knowledge-claims', 'advanced')
    expect(router.navigate).toHaveBeenCalledWith([], jasmine.objectContaining({
      queryParams: { mode: 'advanced' },
      queryParamsHandling: 'merge',
    }))
  })
})
