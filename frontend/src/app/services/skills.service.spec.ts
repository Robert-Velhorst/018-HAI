import { provideHttpClient, withInterceptorsFromDi } from '@angular/common/http'
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing'
import { TestBed } from '@angular/core/testing'
import { SkillsService } from './skills.service'
import { ISkillGuidancePreview } from '../models/skills.model.interface'

describe('SkillsService', () => {
  const sourceCommit = 'a'.repeat(40)
  const validInventory = {
    sourceRepository: 'anthropics/skills',
    sourceCommit,
    sourceCommitDate: '2026-09-23T00:00:00Z',
    catalogFingerprint: 'f'.repeat(64),
    skills: [{
      id: 'skill-a', name: 'Skill A', description: 'Description', license: 'Apache-2.0',
      licenseURL: 'https://example.test/license', licensePath: 'skills/skill-a/LICENSE.txt',
      licenseSHA256: 'c'.repeat(64), sourcePath: 'skills/skill-a/SKILL.md',
      sourceURL: 'https://example.test/source', sourceSHA256: 'b'.repeat(64),
      guidanceSHA256: 'd'.repeat(64), scope: 'advisory', status: 'available',
      enabled: false, needsReapproval: false,
    }],
  }
  let service: SkillsService
  let http: HttpTestingController

  beforeEach(() => {
    TestBed.configureTestingModule({ providers: [provideHttpClient(withInterceptorsFromDi()), provideHttpClientTesting()] })
    service = TestBed.inject(SkillsService)
    http = TestBed.inject(HttpTestingController)
  })

  afterEach(() => http.verify())

  it('reads only the server catalog metadata', () => {
    let received: unknown
    service.inventory().subscribe((value) => { received = value })

    const request = http.expectOne('/api/v1/brain-skills/')
    expect(request.request.method).toBe('GET')
    expect(request.request.body).toBeNull()
    request.flush(validInventory)
    expect(received).toEqual(validInventory)
  })

  it('rejects malformed inventory before it reaches the Skills page', () => {
    let receivedError: unknown
    service.inventory().subscribe({ error: (error) => { receivedError = error } })

    const request = http.expectOne('/api/v1/brain-skills/')
    request.flush({ ...validInventory, skills: null })

    expect(receivedError).toEqual(jasmine.any(Error))
    expect((receivedError as Error).message).toBe('Skills inventory response did not match the expected schema.')
  })

  it('rejects duplicate catalog IDs before they can alias selection controls', () => {
    let receivedError: unknown
    const duplicate = { ...validInventory.skills[0] }
    service.inventory().subscribe({ error: (error) => { receivedError = error } })

    http.expectOne('/api/v1/brain-skills/').flush({
      ...validInventory,
      skills: [validInventory.skills[0], duplicate],
    })

    expect(receivedError).toEqual(jasmine.any(Error))
    expect((receivedError as Error).message).toBe('Skills inventory response did not match the expected schema.')
  })

  it('accepts a missing optional license hash without claiming it was verified', () => {
    const inventoryWithoutLicenseHash = {
      ...validInventory,
      skills: [{ ...validInventory.skills[0], licenseSHA256: '' }],
    }
    let received: unknown
    service.inventory().subscribe((value) => { received = value })

    http.expectOne('/api/v1/brain-skills/').flush(inventoryWithoutLicenseHash)

    expect(received).toEqual(inventoryWithoutLicenseHash)
  })

  it('rejects invalid upstream pins and content hashes', () => {
    const invalidInventories = [
      { ...validInventory, sourceRepository: 'untrusted/skills' },
      { ...validInventory, sourceCommit: 'abc123' },
      { ...validInventory, sourceCommitDate: 'not-a-date' },
      { ...validInventory, skills: [{ ...validInventory.skills[0], id: 'invalid/skill-id' }] },
      { ...validInventory, skills: [{ ...validInventory.skills[0], sourceSHA256: 'not-a-hash' }] },
      { ...validInventory, skills: [{ ...validInventory.skills[0], guidanceSHA256: 'not-a-hash' }] },
      { ...validInventory, skills: [{ ...validInventory.skills[0], licenseSHA256: 'not-a-hash' }] },
    ]

    for (const value of invalidInventories) {
      let receivedError: unknown
      service.inventory().subscribe({ error: (error) => { receivedError = error } })
      http.expectOne('/api/v1/brain-skills/').flush(value)
      expect(receivedError).toEqual(jasmine.any(Error))
      expect((receivedError as Error).message).toBe('Skills inventory response did not match the expected schema.')
    }
  })

  it('reads the bounded approval-only guidance preview through its dedicated route', () => {
    const preview: ISkillGuidancePreview = {
      skillId: 'skill/a',
      currentGuidance: 'Exact HAI-authored prompt guidance.',
      currentGuidanceSHA256: 'current-guidance-hash',
      catalogFingerprint: 'f'.repeat(64),
      previousApprovedGuidanceSHA256: 'previous-guidance-hash',
      previousGuidanceTextStored: false,
      previousGuidanceTextLimitation: 'Earlier text is not stored.',
    }
    let received: unknown
    service.guidancePreview('skill/a').subscribe((value) => { received = value })

    const request = http.expectOne('/api/v1/brain-skills/skill%2Fa/guidance-preview')
    expect(request.request.method).toBe('GET')
    expect(request.request.body).toBeNull()
    request.flush(preview)
    expect(received).toEqual(preview)
  })

  it('sends only explicit owner selection and returns the updated state', () => {
    let received: unknown
    const reviewedHash = 'a'.repeat(64)
    const catalogFingerprint = 'f'.repeat(64)
    service.setEnabled('skill/a', true, reviewedHash, catalogFingerprint).subscribe((state) => { received = state })

    const request = http.expectOne('/api/v1/brain-skills/skill%2Fa/selection')
    expect(request.request.method).toBe('PUT')
    expect(request.request.body).toEqual({ enabled: true, reviewedGuidanceSHA256: reviewedHash, reviewedCatalogFingerprint: catalogFingerprint })
    request.flush({ enabled: true, needsReapproval: false, supersededByConcurrentDecision: false })
    expect(received).toEqual({ enabled: true, needsReapproval: false, supersededByConcurrentDecision: false })
  })

  it('surfaces owner selection failures to the page', () => {
    let failed = false
    service.setEnabled('skill-a', false).subscribe({ error: () => { failed = true } })
    const request = http.expectOne('/api/v1/brain-skills/skill-a/selection')
    expect(request.request.body).toEqual({ enabled: false })
    request.flush({ error: 'unavailable' }, { status: 503, statusText: 'Service Unavailable' })
    expect(failed).toBeTrue()
  })

  it('does not send an enable request without both reviewed fingerprints', () => {
    let failed = false
    service.setEnabled('skill-a', true).subscribe({ error: () => { failed = true } })

    expect(failed).toBeTrue()
    http.expectNone('/api/v1/brain-skills/skill-a/selection')
  })

  it('rejects malformed reviewed hashes without sending a mutation', () => {
    let failed = false
    service.setEnabled('skill-a', true, 'A'.repeat(64), 'f'.repeat(64)).subscribe({ error: () => { failed = true } })

    expect(failed).toBeTrue()
    http.expectNone('/api/v1/brain-skills/skill-a/selection')
  })

  it('rejects a malformed catalog fingerprint without sending a mutation', () => {
    let failed = false
    service.setEnabled('skill-a', true, 'a'.repeat(64), 'not-a-fingerprint').subscribe({ error: () => { failed = true } })

    expect(failed).toBeTrue()
    http.expectNone('/api/v1/brain-skills/skill-a/selection')
  })
})
