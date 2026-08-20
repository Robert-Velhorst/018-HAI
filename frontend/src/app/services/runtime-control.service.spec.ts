import {
  HttpClientTestingModule,
  HttpTestingController,
} from '@angular/common/http/testing'
import { TestBed } from '@angular/core/testing'
import { IControlAuthorization } from '../models/runtime-control.model.interface'
import { RuntimeControlService } from './runtime-control.service'

describe('RuntimeControlService approval transport', () => {
  let service: RuntimeControlService
  let http: HttpTestingController

  const authorization: IControlAuthorization = {
    idempotencyKey: 'opscontrol:request-1',
    taskId: 'opscontrol:request-1',
    approvalSourceId: 'control-decision:decision-1',
    approvalBindingDigest: 'a'.repeat(64),
  }

  beforeEach(() => {
    TestBed.configureTestingModule({ imports: [HttpClientTestingModule] })
    service = TestBed.inject(RuntimeControlService)
    http = TestBed.inject(HttpTestingController)
  })

  afterEach(() => http.verify())

  it('creates a durable owner review without resuming processing', () => {
    service.requestResumeApproval().subscribe()

    const request = http.expectOne('/api/v1/background/resume-approval')
    expect(request.request.method).toBe('POST')
    expect(request.request.body).toEqual({})
    request.flush({
      reviewItemId: 'review-1',
      approvalSourceId: 'opscontrol-review:review-1',
      approvalBindingDigest: 'a'.repeat(64),
    })
  })

  it('submits the exact owner confirmation for a specific durable review', () => {
    const reviewID = '11111111-1111-4111-8111-111111111111'
    service.approveAndResume(reviewID, 'RESUME BACKGROUND PROCESSING').subscribe()

    const request = http.expectOne(`/api/v1/background/resume-approval/${reviewID}/approve-and-resume`)
    expect(request.request.method).toBe('POST')
    expect(request.request.body).toEqual({ confirmation: 'RESUME BACKGROUND PROCESSING' })
    request.flush({})
  })

  it('requests approval for a mode escalation without granting it locally', () => {
    service.prepareModeChange('autonomous_safe').subscribe()

    const request = http.expectOne('/api/v1/background/mode/approval')
    expect(request.request.method).toBe('POST')
    expect(request.request.body).toEqual({ mode: 'autonomous_safe' })
    request.flush({ authorizationRequired: true, authorization })
  })

  it('includes authorization only for an approved mode escalation', () => {
    service.setMode('autonomous_safe', authorization).subscribe()

    const request = http.expectOne('/api/v1/background/mode')
    expect(request.request.method).toBe('PATCH')
    expect(request.request.body).toEqual({
      mode: 'autonomous_safe',
      ...authorization,
    })
    request.flush({})
  })

  it('keeps restrictive mode changes free of fabricated approval fields', () => {
    service.setMode('read_only').subscribe()

    const request = http.expectOne('/api/v1/background/mode')
    expect(request.request.body).toEqual({ mode: 'read_only' })
    request.flush({})
  })
})
