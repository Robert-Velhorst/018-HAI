import { FormBuilder } from '@angular/forms'
import { convertToParamMap } from '@angular/router'
import { fakeAsync, tick } from '@angular/core/testing'
import { EMPTY, of, Subject, throwError } from 'rxjs'
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service'
import { GroundedAnswersComponent } from './grounded-answers.component'

afterEach(() => localStorage.removeItem('hai.module-view.v1.grounded-answers'));

describe('GroundedAnswersComponent RAGFlow evidence boundary', () => {
  function createComponent() {
    const verificationService = jasmine.createSpyObj('IVerificationService', ['answer', 'runs', 'runDetails'])
    const researchService = jasmine.createSpyObj('ResearchService', ['status', 'probe', 'search'])
    const ragflowService = jasmine.createSpyObj('RAGFlowService', ['status', 'probe', 'retrieve'])
    const anythingLLMService = jasmine.createSpyObj('AnythingLLMService', ['status', 'retrieve'])
    const notification = jasmine.createSpyObj('NzNotificationService', ['success', 'error', 'warning', 'info'])
    const route = { snapshot: { queryParamMap: convertToParamMap({}) } }
    const router = jasmine.createSpyObj('Router', ['navigate'])
    const preferences = new ModuleViewPreferencesService()
    preferences.reset('grounded-answers')
    verificationService.runs.and.returnValue(of([]))
    verificationService.answer.and.returnValue(of({ run: { id: 'run-1', status: 'source_supported' }, claims: [], evidence: [], unsupportedClaims: [], researchQuestions: [], logs: [] }))
    researchService.status.and.returnValue(of({ configured: false, provider: 'SearXNG', scope: 'disabled' }))
    researchService.probe.and.returnValue(of({ reachable: true, checkedAt: '2026-07-20T12:00:00Z', scope: 'endpoint only' }))
    ragflowService.status.and.returnValue(of({ enabled: true, configured: true, provider: 'RAGFlow', datasetCount: 1, capabilities: [], restrictions: [], scope: 'candidate evidence only' }))
    ragflowService.probe.and.returnValue(of({ reachable: true, checkedAt: '2026-07-20T12:00:00Z', scope: 'health endpoint only' }))
    anythingLLMService.status.and.returnValue(of({ enabled: true, configured: true, provider: 'AnythingLLM', workspaceCount: 1, workspaceSlugs: ['legal-workspace'], localEmbeddingsConfirmed: true, capabilities: [], restrictions: [], scope: 'candidate evidence only' }))

    return {
      component: new GroundedAnswersComponent(new FormBuilder(), verificationService, researchService, ragflowService, anythingLLMService, notification, route as any, router, preferences),
      verificationService,
      researchService,
      ragflowService,
      anythingLLMService,
      notification,
      preferences,
      router,
    }
  }

  it('keeps Basic lightweight and loads provider status only when source discovery opens', () => {
    const { component, verificationService, researchService, ragflowService, anythingLLMService } = createComponent()

    component.ngOnInit()

    expect(component.isAdvancedView).toBeFalse()
    expect(verificationService.runs).not.toHaveBeenCalled()
    expect(researchService.status).not.toHaveBeenCalled()
    expect(ragflowService.status).not.toHaveBeenCalled()
    expect(anythingLLMService.status).not.toHaveBeenCalled()

    component.onSourceDiscoveryOpen(true)

    expect(researchService.status).toHaveBeenCalledTimes(1)
    expect(ragflowService.status).toHaveBeenCalledTimes(1)
    expect(anythingLLMService.status).toHaveBeenCalledTimes(1)
    expect(ragflowService.retrieve).not.toHaveBeenCalled()
    expect(component.ragflowStatus?.configured).toBeTrue()
  })

  it('loads verification history only when its persisted Advanced section opens', () => {
    const { component, verificationService, preferences } = createComponent()

    component.ngOnInit()

    expect(verificationService.runs).not.toHaveBeenCalled()
    component.onVerificationHistoryOpen(true)

    expect(verificationService.runs).toHaveBeenCalledTimes(1)
    expect(component.runsLoaded).toBeTrue()
    preferences.setMode('grounded-answers', 'advanced')
    preferences.setSection('grounded-answers', 'verification-history', true)
    expect(preferences.get('memory').mode).toBe('basic')
  })

  it('does not attach illustrative evidence when the operator supplied none', () => {
    const { component, verificationService } = createComponent()

    component.answer()

    expect(component.answerForm.value.evidenceSnippet).toBe('')
    expect(verificationService.answer).toHaveBeenCalledWith(jasmine.objectContaining({ externalEvidence: [] }))
  })

  it('preserves audit warnings on the answer shown to the operator', () => {
    const { component, verificationService } = createComponent()
    verificationService.answer.and.returnValue(of({
      run: { id: 'run-2', status: 'source_supported', answer: 'The record supports this.' },
      claims: [], evidence: [], unsupportedClaims: [], researchQuestions: [], logs: [],
      auditWarnings: ['verification.memory_promoted'],
    }))

    component.answer()

    expect(component.result?.auditWarnings).toEqual(['verification.memory_promoted'])
  })

  it('opens source details in the module-specific Advanced view', () => {
    const { component, preferences, router } = createComponent()

    component.openAdvancedSection('verification-details')

    expect(preferences.get('grounded-answers').mode).toBe('advanced')
    expect(preferences.get('grounded-answers').openSections['verification-details']).toBeTrue()
    expect(preferences.get('memory').mode).toBe('basic')
    expect(router.navigate).toHaveBeenCalledWith(['/grounded-answers'], jasmine.objectContaining({ fragment: 'verification-details' }))
  })

  it('opens an already-rendered Advanced section when an inspect link targets it', () => {
    const { component, preferences } = createComponent()
    const section = jasmine.createSpyObj('HaiProgressiveSectionComponent', ['setOpen'])
    section.sectionId = 'verification-details'
    component.progressiveSections = { find: (predicate: (value: typeof section) => boolean) => predicate(section) ? section : undefined } as any
    preferences.setMode('grounded-answers', 'advanced')

    component.openAdvancedSection('verification-details')

    expect(section.setOpen).toHaveBeenCalledWith(true)
  })

  it('reads RAGFlow configuration without retrieving evidence when its Advanced panel opens', () => {
    const { component, ragflowService } = createComponent()

    component.onSourceDiscoveryOpen(true)

    expect(ragflowService.status).toHaveBeenCalled()
    expect(ragflowService.retrieve).not.toHaveBeenCalled()
    expect(component.ragflowStatus?.configured).toBeTrue()
  })

  it('probes the local RAGFlow endpoint without retrieving evidence', () => {
    const { component, ragflowService, notification } = createComponent()
    component.ragflowStatus = { enabled: true, configured: true, provider: 'RAGFlow', datasetCount: 1, capabilities: [], restrictions: [], scope: 'candidate evidence only' }

    component.probeRAGFlow()

    expect(ragflowService.probe).toHaveBeenCalled()
    expect(ragflowService.retrieve).not.toHaveBeenCalled()
    expect(component.ragflowProbe?.reachable).toBeTrue()
    expect(notification.success).toHaveBeenCalled()
  })

  it('attaches a selected RAGFlow chunk as unverified candidate evidence', () => {
    const { component, verificationService } = createComponent()
    component.useRAGFlowResult({
      chunkId: 'chunk 1',
      datasetId: 'legal-files',
      documentId: 'doc 1',
      documentName: 'Letter.pdf',
      content: 'The hearing is scheduled for 9 September.',
    })

    component.answer()

    expect(verificationService.answer).toHaveBeenCalledWith(jasmine.objectContaining({
      externalEvidence: [jasmine.objectContaining({
        sourceType: 'ragflow_candidate_evidence',
        sourceUri: 'ragflow://dataset/legal-files/document/doc%201/chunk/chunk%201',
        snippet: 'The hearing is scheduled for 9 September.',
        official: false,
        primary: false,
      })],
    }))
  })

  it('attaches a selected AnythingLLM chunk as unverified candidate evidence', () => {
    const { component, verificationService } = createComponent()
    component.useAnythingLLMResult({
      chunkId: 'chunk 1',
      workspaceSlug: 'legal-workspace',
      title: 'Letter.pdf',
      content: 'The hearing is scheduled for 9 September.',
    })

    component.answer()

    expect(verificationService.answer).toHaveBeenCalledWith(jasmine.objectContaining({
      externalEvidence: [jasmine.objectContaining({
        sourceType: 'anythingllm_candidate_evidence',
        sourceUri: 'anythingllm://workspace/legal-workspace/chunk/chunk%201',
        snippet: 'The hearing is scheduled for 9 September.',
        official: false,
        primary: false,
      })],
    }))
  })

  it('does not retain a RAGFlow selection after an explicit public-source selection', () => {
    const { component, verificationService } = createComponent()
    component.useRAGFlowResult({ chunkId: 'chunk-1', datasetId: 'documents', content: 'Local candidate.' })
    component.useResearchResult({ title: 'Official result', sourceUri: 'https://example.test/evidence', snippet: 'Public candidate.' })

    component.answer()

    expect(verificationService.answer).toHaveBeenCalledWith(jasmine.objectContaining({
      externalEvidence: [jasmine.objectContaining({ sourceType: 'local_research' })],
    }))
  })

  it('reads AnythingLLM configuration without retrieving evidence when its Advanced panel opens', () => {
    const { component, anythingLLMService } = createComponent()

    component.onSourceDiscoveryOpen(true)

    expect(anythingLLMService.status).toHaveBeenCalled()
    expect(anythingLLMService.retrieve).not.toHaveBeenCalled()
    expect(component.anythingLLMStatus?.configured).toBeTrue()
  })

  it('probes the configured local SearXNG endpoint without searching for evidence', () => {
    const { component, researchService } = createComponent()
    component.researchStatus = { enabled: true, configured: true, provider: 'SearXNG', scope: 'local discovery only' }

    component.probeResearch()

    expect(researchService.probe).toHaveBeenCalled()
    expect(researchService.search).not.toHaveBeenCalled()
    expect(component.researchProbe?.reachable).toBeTrue()
  })

  it('preserves verification history when its refresh fails', () => {
    const { component, verificationService } = createComponent()
    component.runs = [{ id: 'run-1' } as any]
    verificationService.runs.and.returnValue(throwError(() => new Error('history unavailable')))

    component.loadRuns()

    expect(component.runs.map((run) => run.id)).toEqual(['run-1'])
    expect(component.runsUnavailable).toBeTrue()
  })

  const requests = [
    ['verificationService', 'answer', 'answer'], ['verificationService', 'runs', 'loadRuns'],
    ['verificationService', 'runDetails', 'loadDetails'],
    ['researchService', 'status', 'loadResearchStatus'], ['researchService', 'probe', 'probeResearch'],
    ['researchService', 'search', 'searchResearch'], ['ragflowService', 'status', 'loadRAGFlowStatus'],
    ['ragflowService', 'probe', 'probeRAGFlow'], ['ragflowService', 'retrieve', 'searchRAGFlow'],
    ['anythingLLMService', 'status', 'loadAnythingLLMStatus'], ['anythingLLMService', 'retrieve', 'searchAnythingLLM'],
  ] as const

  for (const [serviceName, operation, action] of requests) {
    it(`contains synchronous ${serviceName}.${operation} construction failures`, () => {
      const setup = createComponent()
      const component = setup.component
      component.researchStatus = { configured: true } as any
      component.ragflowStatus = { configured: true } as any
      setup[serviceName][operation].and.callFake(() => { throw new Error('Synthetic request failure') })
      expect(() => (component[action] as Function).call(component, action === 'loadDetails' ? { id: 'run-1' } : 'reviewed-workspace')).not.toThrow()
      expect(component.loading || component.runsLoading || component.researchLoading || component.researchProbeLoading || component.researchStatusLoading || component.ragflowLoading || component.ragflowProbeLoading || component.ragflowStatusLoading || component.anythingLLMLoading || component.anythingLLMStatusLoading).toBeFalse()
      expect(setup.notification.success).not.toHaveBeenCalled()
    })
  }

  it('does not submit an answer twice while verification is pending', () => {
    const { component, verificationService } = createComponent()
    verificationService.answer.and.returnValue(new Subject())
    component.answer()
    component.answer()
    expect(verificationService.answer).toHaveBeenCalledTimes(1)
    ;(component as any).ngOnDestroy?.()
  })

  for (const [serviceName, action] of [['researchService', 'probeResearch'], ['ragflowService', 'probeRAGFlow']] as const) {
    it(`rejects a negative ${serviceName} health result without a success notification`, () => {
      const setup = createComponent()
      setup.component.researchStatus = { configured: true } as any
      setup.component.ragflowStatus = { configured: true } as any
      setup[serviceName].probe.and.returnValue(of({ reachable: false }))
      setup.component[action]()
      expect(setup.notification.success).not.toHaveBeenCalled()
      expect(setup.notification.error.calls.count() + setup.notification.warning.calls.count()).toBe(1)
    })
  }

  it('does not replace a newer detail with a late obsolete response', () => {
    const { component, verificationService } = createComponent()
    const obsolete = new Subject<any>()
    verificationService.runDetails.and.returnValues(obsolete, of({ run: { id: 'new-run' }, evidence: [] }))
    component.loadDetails({ id: 'old-run' } as any)
    component.loadDetails({ id: 'new-run' } as any)
    obsolete.next({ run: { id: 'old-run' }, evidence: [] })
    expect(component.result?.run.id).toBe('new-run')
    ;(component as any).ngOnDestroy?.()
  })

  it('bounds silent provider status reads without treating them as loaded', fakeAsync(() => {
    const { component, researchService } = createComponent()
    researchService.status.and.returnValue(new Subject())
    component.loadResearchStatus()
    tick(6001)
    expect(component.researchStatusLoading).toBeFalse()
    expect(component.researchStatusLoaded).toBeFalse()
    ;(component as any).ngOnDestroy?.()
  }))

  it('treats an empty history response as unavailable rather than successful', () => {
    const { component, verificationService } = createComponent()
    verificationService.runs.and.returnValue(EMPTY)
    component.loadRuns()
    expect(component.runsLoading).toBeFalse()
    expect(component.runsUnavailable).toBeTrue()
    expect(component.runsLoaded).toBeFalse()
  })

  it('cancels pending observations and does not dispatch after teardown', () => {
    const { component, verificationService, researchService } = createComponent()
    const pending = new Subject<any>()
    verificationService.answer.and.returnValue(pending)
    component.answer()
    ;(component as any).ngOnDestroy?.()
    pending.next({ run: { id: 'late' } })
    component.answer()
    component.loadRuns()
    component.loadResearchStatus()
    expect(component.result).toBeUndefined()
    expect(verificationService.answer).toHaveBeenCalledTimes(1)
    expect(verificationService.runs).not.toHaveBeenCalled()
    expect(researchService.status).not.toHaveBeenCalled()
  })

  it('preserves the question and reports uncertainty when answer creation times out', fakeAsync(() => {
    const { component, verificationService, notification } = createComponent()
    verificationService.answer.and.returnValue(new Subject())
    component.answerForm.patchValue({ question: 'Retained question', allowMemoryUpdate: true })
    component.answer()
    tick(30001)
    expect(component.loading).toBeFalse()
    expect(component.answerForm.value.question).toBe('Retained question')
    expect(component.answerError).toContain('may already have been saved')
    expect(notification.success).not.toHaveBeenCalled()
    component.ngOnDestroy()
  }))

  it('rejects empty answer completion without clearing the operator question', () => {
    const { component, verificationService, notification } = createComponent()
    verificationService.answer.and.returnValue(EMPTY)
    const question = component.answerForm.value.question
    component.answer()
    expect(component.result).toBeUndefined()
    expect(component.loading).toBeFalse()
    expect(component.answerError).toBeTruthy()
    expect(component.answerForm.value.question).toBe(question)
    expect(notification.success).not.toHaveBeenCalled()
  })

  it('observes one answer result rather than replacing it on later emissions', () => {
    const { component, verificationService } = createComponent()
    const response = new Subject<any>()
    verificationService.answer.and.returnValue(response)
    component.answer()
    response.next({ run: { id: 'first' } })
    response.next({ run: { id: 'unexpected-second' } })
    expect(component.result?.run.id).toBe('first')
    expect(component.loading).toBeFalse()
    component.ngOnDestroy()
  })

  it('does not let an older detail read replace a newly requested answer', () => {
    const { component, verificationService } = createComponent()
    const detail = new Subject<any>()
    verificationService.runDetails.and.returnValue(detail)
    component.loadDetails({ id: 'older-run' } as any)
    component.answer()
    detail.next({ run: { id: 'older-run' } })
    expect(component.result?.run.id).toBe('run-1')
    component.ngOnDestroy()
  })
})
