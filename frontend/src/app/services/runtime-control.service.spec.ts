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

  it('prepares the exact current emergency-stop recovery', () => {
    service.prepareControlApproval('resume').subscribe()

    const request = http.expectOne('/api/v1/background/control-approvals')
    expect(request.request.method).toBe('POST')
    expect(request.request.body).toEqual({ action: 'resume' })
    request.flush({})
  })

  it('records the owner decision separately from execution', () => {
    service.decideControlApproval(
      '11111111-1111-4111-8111-111111111111',
      'approved',
      'Owner approved the exact recovery.'
    ).subscribe()

    const request = http.expectOne(
      '/api/v1/background/control-approvals/11111111-1111-4111-8111-111111111111/decision'
    )
    expect(request.request.method).toBe('POST')
    expect(request.request.body).toEqual({
      decision: 'approved',
      reason: 'Owner approved the exact recovery.',
    })
    request.flush({})
  })

  it('submits server-issued references to resume processing', () => {
    service.resume(authorization).subscribe()

    const request = http.expectOne('/api/v1/background/resume')
    expect(request.request.method).toBe('POST')
    expect(request.request.body).toEqual(authorization)
    request.flush({})
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
