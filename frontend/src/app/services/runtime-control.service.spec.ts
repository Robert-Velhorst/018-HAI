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
    service.prepareResume().subscribe()

    const request = http.expectOne('/api/v1/background/resume/approval')
    expect(request.request.method).toBe('POST')
    expect(request.request.body).toEqual({})
    request.flush(authorization)
  })

  it('requests approval for a mode escalation without granting it locally', () => {
    service.prepareModeChange('autonomous_safe').subscribe()

    const request = http.expectOne('/api/v1/background/mode/approval')
    expect(request.request.method).toBe('POST')
    expect(request.request.body).toEqual({ mode: 'autonomous_safe' })
    request.flush({ authorizationRequired: true, authorization })
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
