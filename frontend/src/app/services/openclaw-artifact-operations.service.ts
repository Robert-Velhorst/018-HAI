import { Injectable } from '@angular/core'
import { HttpClient, HttpResponse } from '@angular/common/http'
import { BehaviorSubject, Observable, finalize, shareReplay, tap, throwError, timeout } from 'rxjs'

const artifactDigestPattern = /^[a-f0-9]{64}$/
const artifactContentLimit = 8 * 1024 * 1024

export interface RetainedArtifactCopy {
  artifactDigest: string
  contentSHA256: string
  sizeBytes: number
  retainedAt: string
  verification: 'unverified'
}

export type ArtifactMutationAction = 'retain' | 'remove'
export type ArtifactMutationFailureCode = 'retained_copy_unreadable'
export interface ArtifactMutationState {
  eventId: string
  action: ArtifactMutationAction
  digest: string
  status: 'running' | 'succeeded' | 'failed'
  httpStatus?: number
  failureCode?: ArtifactMutationFailureCode
  updatedAt: number
}

interface ArtifactMutationChannel {
  subject: BehaviorSubject<ArtifactMutationState | null>
  subscribers: number
  replayUntil?: number
}

@Injectable({ providedIn: 'root' })
export class OpenclawArtifactOperationsService {
  private readonly terminalReplayWindowMs = 24 * 60 * 60 * 1000
  private readonly maxRetainedStates = 128
  private readonly states = new Map<string, ArtifactMutationChannel>()
  private readonly inFlight = new Map<string, Observable<unknown>>()
  private readonly activeByEvent = new Map<string, number>()

  constructor(private http: HttpClient) {}

  watch(eventId: string): Observable<ArtifactMutationState | null> {
    return new Observable(observer => {
      const channel = this.stateFor(eventId)
      channel.subscribers++
      const subscription = channel.subject.subscribe(observer)
      return () => {
        subscription.unsubscribe()
        channel.subscribers--
        this.pruneState(eventId, channel)
      }
    })
  }

  retain(eventId: string, digest: string): Observable<RetainedArtifactCopy> {
    if (!this.validReference(eventId, digest)) return this.invalidReference()
    const path = this.basePath(eventId)
    return this.run(eventId, 'retain', digest, '', this.http.post<RetainedArtifactCopy>(`${path}/${digest}/retain`, {}), 60000)
  }

  forget(eventId: string, digest: string, contentSHA256: string): Observable<HttpResponse<unknown>> {
    if (!this.validReference(eventId, digest) || !artifactDigestPattern.test(contentSHA256)) return this.invalidReference()
    const path = this.basePath(eventId)
    return this.run(eventId, 'remove', digest, contentSHA256, this.http.delete(`${path}/${digest}/retained`, {
      headers: { 'If-Match': `"${contentSHA256}"` }, observe: 'response',
    }), 30000)
  }

  private run<T>(eventId: string, action: ArtifactMutationAction, digest: string, version: string, request: Observable<T>, timeoutMs: number): Observable<T> {
    const key = `${eventId}\u0000${action}\u0000${digest}\u0000${version}`
    const existing = this.inFlight.get(key)
    if (existing) return existing as Observable<T>

    this.activeByEvent.set(eventId, (this.activeByEvent.get(eventId) || 0) + 1)
    this.publish({ eventId, action, digest, status: 'running', updatedAt: Date.now() })
    const shared = request.pipe(
      timeout(timeoutMs),
      tap({
        next: result => {
          const retained = result as Partial<RetainedArtifactCopy>
          const validRetain = action !== 'retain' || (retained.artifactDigest === digest && artifactDigestPattern.test(retained.contentSHA256 || '') && Number.isSafeInteger(retained.sizeBytes) && Number(retained.sizeBytes) >= 0 && Number(retained.sizeBytes) <= artifactContentLimit && retained.verification === 'unverified' && typeof retained.retainedAt === 'string' && Number.isFinite(Date.parse(retained.retainedAt)))
          const validRemoval = action !== 'remove' || (result as HttpResponse<unknown>).status === 204
          this.publish({ eventId, action, digest, status: validRetain && validRemoval ? 'succeeded' : 'failed', httpStatus: validRetain && validRemoval ? undefined : 0, updatedAt: Date.now() })
        },
        error: error => this.publish({
          eventId, action, digest, status: 'failed',
          httpStatus: Number.isInteger(error?.status) ? error.status : 0,
          failureCode: error?.error?.code === 'retained_copy_unreadable' ? 'retained_copy_unreadable' : undefined,
          updatedAt: Date.now(),
        }),
      }),
      finalize(() => {
        this.inFlight.delete(key)
        const remaining = (this.activeByEvent.get(eventId) || 1) - 1
        if (remaining > 0) this.activeByEvent.set(eventId, remaining)
        else this.activeByEvent.delete(eventId)
        const channel = this.states.get(eventId)
        if (channel) this.pruneState(eventId, channel)
      }),
      shareReplay({ bufferSize: 1, refCount: false }),
    )
    this.inFlight.set(key, shared)
    shared.subscribe({ error: () => {} })
    return shared
  }

  private stateFor(eventId: string): ArtifactMutationChannel {
    let channel = this.states.get(eventId)
    if (channel && !this.activeByEvent.has(eventId) && channel.replayUntil !== undefined && channel.replayUntil <= Date.now()) {
      channel.replayUntil = undefined
      channel.subject.next(null)
    }
    if (!channel) {
      channel = { subject: new BehaviorSubject<ArtifactMutationState | null>(null), subscribers: 0 }
    }
    this.states.delete(eventId)
    this.states.set(eventId, channel)
    return channel
  }

  private publish(state: ArtifactMutationState): void {
    const channel = this.stateFor(state.eventId)
    channel.replayUntil = state.status === 'running' ? undefined : Date.now() + this.terminalReplayWindowMs
    channel.subject.next(state)
  }

  private pruneState(eventId: string, channel: ArtifactMutationChannel): void {
    if (channel.subscribers !== 0 || this.activeByEvent.has(eventId) || this.states.get(eventId) !== channel) return
    if (!channel.subject.value) {
      this.deleteState(eventId, channel)
      return
    }
    if (channel.replayUntil === undefined) channel.replayUntil = Date.now() + this.terminalReplayWindowMs
    if (channel.replayUntil <= Date.now()) this.deleteState(eventId, channel)
    this.enforceStateLimit()
  }

  private enforceStateLimit(): void {
    while (this.states.size > this.maxRetainedStates) {
      let removed = false
      for (const [eventId, channel] of this.states) {
        if (channel.subscribers !== 0 || this.activeByEvent.has(eventId) || !channel.subject.value || channel.subject.value.status === 'running') continue
        this.deleteState(eventId, channel)
        removed = true
        break
      }
      if (!removed) return
    }
  }

  private deleteState(eventId: string, channel: ArtifactMutationChannel): void {
    if (this.states.get(eventId) !== channel) return
    this.states.delete(eventId)
    channel.subject.complete()
  }

  private basePath(eventId: string): string {
    return `/api/v1/openclaw-artifacts/${encodeURIComponent(eventId)}`
  }

  private validReference(eventId: string, digest: string): boolean {
    return typeof eventId === 'string' && eventId.trim().length > 0 && artifactDigestPattern.test(digest)
  }

  private invalidReference<T>(): Observable<T> {
    return throwError(() => new Error('Invalid OpenClaw artifact reference'))
  }
}
