import { TestBed } from '@angular/core/testing'
import { provideHttpClient } from '@angular/common/http'
import { provideHttpClientTesting, HttpTestingController } from '@angular/common/http/testing'
import { OpenclawArtifactOperationsService } from './openclaw-artifact-operations.service'

describe('OpenclawArtifactOperationsService', () => {
  beforeEach(() => TestBed.configureTestingModule({ providers: [provideHttpClient(), provideHttpClientTesting()] }))
  afterEach(() => TestBed.inject(HttpTestingController).verify())

  it('releases idle event channels when the last watcher unsubscribes', () => {
    const operations = TestBed.inject(OpenclawArtifactOperationsService)
    for (let index = 0; index < 200; index++) {
      const subscription = operations.watch(`event-${index}`).subscribe()
      subscription.unsubscribe()
    }
    expect((operations as any).states.size).toBe(0)
  })

  it('rejects invalid artifact references and content fingerprints before making a request', () => {
    const operations = TestBed.inject(OpenclawArtifactOperationsService)
    const http = TestBed.inject(HttpTestingController)
    let retainFailed = false
    let forgetFailed = false

    operations.retain('event-1', '../not-a-digest').subscribe({ error: () => retainFailed = true })
    operations.forget('event-1', 'a'.repeat(64), 'not-a-fingerprint').subscribe({ error: () => forgetFailed = true })

    expect(retainFailed).toBeTrue()
    expect(forgetFailed).toBeTrue()
    http.expectNone(request => request.url.includes('/openclaw-artifacts/'))
  })

  it('does not publish an oversized retention response as a successful operation', () => {
    const operations = TestBed.inject(OpenclawArtifactOperationsService)
    const http = TestBed.inject(HttpTestingController)
    const digest = 'a'.repeat(64)
    const states: Array<string | undefined> = []
    const watcher = operations.watch('event-1').subscribe(state => states.push(state?.status))
    operations.retain('event-1', digest).subscribe()
    http.expectOne(`/api/v1/openclaw-artifacts/event-1/${digest}/retain`).flush({
      artifactDigest: digest,
      contentSHA256: 'b'.repeat(64),
      sizeBytes: 8 * 1024 * 1024 + 1,
      retainedAt: '2026-09-24T00:00:00Z',
      verification: 'unverified',
    })

    expect(states).toEqual([undefined, 'running', 'failed'])
    watcher.unsubscribe()
  })

  it('replays only the allow-listed retained-copy integrity failure code', () => {
    const operations = TestBed.inject(OpenclawArtifactOperationsService)
    const http = TestBed.inject(HttpTestingController)
    const states: Array<{ httpStatus?: number; failureCode?: string } | null> = []
    const watcher = operations.watch('event-1').subscribe(state => states.push(state && { httpStatus: state.httpStatus, failureCode: state.failureCode }))
    operations.forget('event-1', 'a'.repeat(64), 'b'.repeat(64)).subscribe({ error: () => {} })
    http.expectOne('/api/v1/openclaw-artifacts/event-1/' + 'a'.repeat(64) + '/retained').flush(
      { code: 'retained_copy_unreadable', error: 'private backend detail' }, { status: 503, statusText: 'Unavailable' },
    )

    expect(states[states.length - 1]).toEqual({ httpStatus: 503, failureCode: 'retained_copy_unreadable' })
    watcher.unsubscribe()
  })

  it('retains bounded terminal state after unsubscription so a reopened view can reconcile it', () => {
    const operations = TestBed.inject(OpenclawArtifactOperationsService)
    const http = TestBed.inject(HttpTestingController)
    const digest = 'a'.repeat(64)
    const sha = 'b'.repeat(64)
    const observed: Array<string | null> = []
    const watcher = operations.watch('event-1').subscribe(state => observed.push(state?.status || null))
    const operation = operations.retain('event-1', digest).subscribe()
    const request = http.expectOne(`/api/v1/openclaw-artifacts/event-1/${digest}/retain`)

    expect(observed).toEqual([null, 'running'])
    watcher.unsubscribe()
    expect((operations as any).states.size).toBe(1)
    request.flush({ artifactDigest: digest, contentSHA256: sha, sizeBytes: 2, retainedAt: '2026-09-24T00:00:00Z', verification: 'unverified' })
    operation.unsubscribe()
    expect((operations as any).states.size).toBe(1)

    let latest: string | null | undefined
    const reopened = operations.watch('event-1').subscribe(state => latest = state?.status || null)
    expect(latest).toBe('succeeded')
    reopened.unsubscribe()
  })

  it('caps terminal operation history instead of growing with every execution', () => {
    const operations = TestBed.inject(OpenclawArtifactOperationsService)
    const http = TestBed.inject(HttpTestingController)
    const digest = 'a'.repeat(64)
    const sha = 'b'.repeat(64)
    for (let index = 0; index < 140; index++) {
      operations.retain(`event-${index}`, digest).subscribe()
      http.expectOne(`/api/v1/openclaw-artifacts/event-${index}/${digest}/retain`).flush({
        artifactDigest: digest,
        contentSHA256: sha,
        sizeBytes: 2,
        retainedAt: '2026-09-24T00:00:00Z',
        verification: 'unverified',
      })
    }
    expect((operations as any).states.size).toBe(128)
  })

  it('expires terminal state for existing watchers before replaying it to a new watcher', () => {
    const operations = TestBed.inject(OpenclawArtifactOperationsService)
    const http = TestBed.inject(HttpTestingController)
    const digest = 'a'.repeat(64)
    const sha = 'b'.repeat(64)
    const original: Array<string | null> = []
    const activeWatcher = operations.watch('event-1').subscribe(state => original.push(state?.status || null))
    operations.retain('event-1', digest).subscribe()
    http.expectOne(`/api/v1/openclaw-artifacts/event-1/${digest}/retain`).flush({
      artifactDigest: digest,
      contentSHA256: sha,
      sizeBytes: 2,
      retainedAt: '2026-09-24T00:00:00Z',
      verification: 'unverified',
    })
    expect(original).toEqual([null, 'running', 'succeeded'])

    ;(operations as any).states.get('event-1').replayUntil = Date.now() - 1
    let latest: string | null | undefined
    const reopened = operations.watch('event-1').subscribe(state => latest = state?.status || null)
    expect(original[original.length - 1]).toBeNull()
    expect(latest).toBeNull()
    reopened.unsubscribe()
    activeWatcher.unsubscribe()
  })
})
