import { FormBuilder } from '@angular/forms'
import { convertToParamMap } from '@angular/router'
import { of, throwError } from 'rxjs'
import { GroundedAnswersComponent } from './grounded-answers.component'

describe('GroundedAnswersComponent RAGFlow evidence boundary', () => {
  function createComponent() {
    const verificationService = jasmine.createSpyObj('IVerificationService', ['answer', 'runs', 'runDetails'])
    const researchService = jasmine.createSpyObj('ResearchService', ['status', 'probe', 'search'])
    const ragflowService = jasmine.createSpyObj('RAGFlowService', ['status', 'probe', 'retrieve'])
    const anythingLLMService = jasmine.createSpyObj('AnythingLLMService', ['status', 'retrieve'])
    const notification = jasmine.createSpyObj('NzNotificationService', ['success', 'error', 'warning', 'info'])
    const route = { snapshot: { queryParamMap: convertToParamMap({}) } }
    const router = jasmine.createSpyObj('Router', ['navigate'])
    verificationService.runs.and.returnValue(of([]))
    verificationService.answer.and.returnValue(of({ run: { id: 'run-1', status: 'source_supported' }, claims: [], evidence: [], unsupportedClaims: [], researchQuestions: [], logs: [] }))
    researchService.status.and.returnValue(of({ configured: false, provider: 'SearXNG', scope: 'disabled' }))
    researchService.probe.and.returnValue(of({ reachable: true, checkedAt: '2026-07-20T12:00:00Z', scope: 'endpoint only' }))
    ragflowService.status.and.returnValue(of({ enabled: true, configured: true, provider: 'RAGFlow', datasetCount: 1, capabilities: [], restrictions: [], scope: 'candidate evidence only' }))
    ragflowService.probe.and.returnValue(of({ reachable: true, checkedAt: '2026-07-20T12:00:00Z', scope: 'health endpoint only' }))
    anythingLLMService.status.and.returnValue(of({ enabled: true, configured: true, provider: 'AnythingLLM', workspaceCount: 1, workspaceSlugs: ['legal-workspace'], localEmbeddingsConfirmed: true, capabilities: [], restrictions: [], scope: 'candidate evidence only' }))

    return {
      component: new GroundedAnswersComponent(new FormBuilder(), verificationService, researchService, ragflowService, anythingLLMService, notification, route as any, router),
      verificationService,
      researchService,
      ragflowService,
      anythingLLMService,
      notification,
    }
  }

  it('reads RAGFlow configuration without retrieving evidence', () => {
    const { component, ragflowService } = createComponent()

    component.ngOnInit()

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

  it('reads AnythingLLM configuration without retrieving evidence', () => {
    const { component, anythingLLMService } = createComponent()

    component.ngOnInit()

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
})
