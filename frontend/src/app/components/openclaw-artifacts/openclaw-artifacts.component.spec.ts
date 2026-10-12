import { fakeAsync, TestBed, tick } from '@angular/core/testing'
import { provideHttpClient } from '@angular/common/http'
import { provideHttpClientTesting, HttpTestingController } from '@angular/common/http/testing'
import { OpenclawArtifactsComponent } from './openclaw-artifacts.component'
import { NzModalService } from 'ng-zorro-antd/modal'
import { OpenclawArtifactOperationsService } from '../../services/openclaw-artifact-operations.service'

describe('OpenclawArtifactsComponent', () => {
  const digest = 'a'.repeat(64)
  const item = { artifactDigest: digest, artifactType: 'file', mimeType: 'text/plain', sizeBytes: null, createdAt: '2026-09-06T10:00:00Z' }
  const sha = '8f434346648f6b96df89dda901c5176b10a6d83961dd3c1ac88b59b2dc327aa4'
  const base = '/api/v1/openclaw-artifacts/event-1'
  const nextBase = '/api/v1/openclaw-artifacts/event-2'
  beforeEach(() => TestBed.configureTestingModule({ imports: [OpenclawArtifactsComponent], providers: [provideHttpClient(), provideHttpClientTesting()] }))
  afterEach(() => TestBed.inject(HttpTestingController).verify())

  function setup() {
    const fixture = TestBed.createComponent(OpenclawArtifactsComponent)
    fixture.componentRef.setInput('eventId', 'event-1')
    fixture.detectChanges()
    return { fixture, component: fixture.componentInstance, http: TestBed.inject(HttpTestingController) }
  }

  async function waitForDownload(component: OpenclawArtifactsComponent) {
    // Web Crypto work is not tracked by Angular's fixture stability.
    const deadline = Date.now() + 4000
    while (component.downloading) {
      if (Date.now() > deadline) throw new Error('Download verification did not settle')
      await new Promise(resolve => setTimeout(resolve, 10))
    }
  }

  it('requires confirmation before deleting and binds it to the reviewed version without exposing fingerprints', () => {
    const { fixture, component, http } = setup()
    const confirm = spyOn(fixture.debugElement.injector.get(NzModalService), 'confirm').and.returnValue({ destroy: () => {} } as any)
    const saved = { artifactDigest: digest, contentSHA256: sha, sizeBytes: 2, retainedAt: item.createdAt, verification: 'unverified' }
    component.toggle()
    http.expectOne(base).flush({ state: 'captured', items: [item], retentionAvailable: true, retained: [saved] })
    component.confirmForget(item)
    http.expectNone(`${base}/${digest}/retained`)
    const options = confirm.calls.mostRecent().args[0]!
    expect(options.nzAutofocus).toBe('cancel')
    expect(options.nzContent).toContain('Only this account’s encrypted HAI copy')
    expect(options.nzContent).not.toContain(digest)
    expect(options.nzContent).not.toContain(sha)
    const onConfirm = options.nzOnOk as Function
    onConfirm()
    onConfirm()
    const requests = http.match(`${base}/${digest}/retained`)
    expect(requests.length).toBe(1)
    const request = requests[0]
    expect(request.request.method).toBe('DELETE')
    expect(request.request.headers.get('If-Match')).toBe(`"${sha}"`)
    request.flush(null, { status: 204, statusText: 'No Content' })
    http.expectOne(base).flush({ state: 'captured', items: [item], retentionAvailable: true, retained: [], storageUsage: { bytesUsed: 0, filesUsed: 0, bytesLimit: 67108864, filesLimit: 128 } })
    fixture.detectChanges()
    expect(component.view?.items.length).toBe(1)
    expect(component.retained(item)).toBeUndefined()
    expect(fixture.nativeElement.textContent).toContain('0 of 128 files')
    fixture.destroy()
  })

  it('renders a destructive confirmation and sends the bound version only after the user confirms', fakeAsync(() => {
    const { fixture, component, http } = setup()
    component.toggle()
    http.expectOne(base).flush({ state: 'captured', items: [item], retentionAvailable: true, retained: [{ artifactDigest: digest, contentSHA256: sha, sizeBytes: 2, retainedAt: item.createdAt, verification: 'unverified' }] })
    component.confirmForget(item)
    fixture.detectChanges()
    tick(250)

    const modal = document.body.querySelector('.ant-modal-confirm')
    expect(modal?.textContent).toContain('Only this account’s encrypted HAI copy')
    expect(modal?.textContent).toContain('Only this account’s encrypted HAI copy')
    expect(modal?.textContent).not.toContain(digest)
    expect(modal?.textContent).not.toContain(sha)
    const dialog = document.body.querySelector('[role="dialog"]') as HTMLElement | null
    expect(dialog?.getAttribute('aria-modal')).toBe('true')
    const titleId = dialog?.getAttribute('aria-labelledby')
    expect(titleId).toBeTruthy()
    expect(titleId && document.getElementById(titleId)?.textContent).toContain('Remove the retained copy from HAI?')
    const confirm = Array.from(modal?.querySelectorAll('button') || []).find(button => button.textContent?.includes('Remove HAI copy'))
    expect(confirm).toBeDefined()
    http.expectNone(`${base}/${digest}/retained`)

    confirm!.click()
    fixture.detectChanges()
    const request = http.expectOne(`${base}/${digest}/retained`)
    expect(request.request.method).toBe('DELETE')
    expect(request.request.headers.get('If-Match')).toBe(`"${sha}"`)
    request.flush(null, { status: 204, statusText: 'No Content' })
    http.expectOne(base).flush({ state: 'captured', items: [item], retentionAvailable: true, retained: [], storageUsage: { bytesUsed: 0, filesUsed: 0, bytesLimit: 67108864, filesLimit: 128 } })
    tick(250)
    fixture.detectChanges()
    expect(component.retained(item)).toBeUndefined()
    expect(fixture.nativeElement.textContent).toContain('0 of 128 files')
    fixture.destroy()
  }))

  it('disables file actions and rejects calls while metadata is refreshing', () => {
    const { fixture, component, http } = setup()
    component.toggle()
    http.expectOne(base).flush({ state: 'captured', items: [item], retentionAvailable: true, retained: [] })
    component.load()
    fixture.detectChanges()

    const download = fixture.nativeElement.querySelector('button[aria-label="Download file 1"]') as HTMLButtonElement
    const retain = fixture.nativeElement.querySelector('button[aria-label="Retain file 1 in HAI"]') as HTMLButtonElement
    expect(fixture.nativeElement.querySelector('section[aria-busy="true"]')).not.toBeNull()
    expect(download.disabled).toBeTrue()
    expect(retain.disabled).toBeTrue()
    component.download(item)
    component.retain(item)
    http.expectNone(`${base}/${digest}/download`)
    http.expectNone(`${base}/${digest}/retain`)

    http.expectOne(base).flush({ state: 'captured', items: [item], retentionAvailable: true, retained: [] })
    fixture.detectChanges()
    expect(download.disabled).toBeFalse()
    expect(retain.disabled).toBeFalse()
    fixture.destroy()
  })

  it('keeps metadata reads owner-scoped and fetches bytes only after an explicit file action', () => {
    const { fixture, component, http } = setup()
    component.toggle()
    http.expectOne(base).flush({ state: 'captured', items: [item], retentionAvailable: true, retentionStatus: 'available', retained: [] })
    fixture.detectChanges()

    expect(fixture.nativeElement.textContent).toContain('authenticated account that owns this execution')
    expect(fixture.nativeElement.textContent).toContain('loads metadata only')
    expect(fixture.nativeElement.textContent).toContain('fetches file contents only after you choose Download or Retain')
    const toggle = fixture.nativeElement.querySelector('button[aria-expanded="true"]') as HTMLButtonElement
    const controlled = fixture.nativeElement.querySelector(`[id="${component.panelId}"]`) as HTMLElement
    expect(toggle.getAttribute('aria-controls')).toBe(controlled.id)
    expect(controlled.hidden).toBeFalse()
    http.expectNone(`${base}/${digest}/download`)
    http.expectNone(`${base}/${digest}/retain`)

    component.download(item)
    expect(http.expectOne(`${base}/${digest}/download`).request.method).toBe('GET')
    fixture.destroy()
  })

  it('keeps the aria-controls target present while the files panel is collapsed', () => {
    const { fixture, component, http } = setup()
    const toggle = fixture.nativeElement.querySelector('button[aria-expanded="false"]') as HTMLButtonElement
    const panelId = toggle.getAttribute('aria-controls')
    const controlled = fixture.nativeElement.querySelector(`[id="${panelId}"]`) as HTMLElement | null

    expect(panelId).toBe(component.panelId)
    expect(controlled).not.toBeNull()
    expect(controlled?.hidden).toBeTrue()
    expect(controlled?.querySelector('section[aria-label="OpenClaw files"]')).toBeNull()

    component.toggle()
    http.expectOne(base).flush({ state: 'captured', items: [item], retentionAvailable: true, retained: [] })
    fixture.detectChanges()
    expect(toggle.getAttribute('aria-expanded')).toBe('true')
    expect(controlled?.hidden).toBeFalse()
    expect(controlled?.querySelector('section[aria-label="OpenClaw files"]')).not.toBeNull()
    fixture.destroy()
  })

  it('allows removing existing owner copies with a weak key while keeping new retention disabled', () => {
    const { fixture, component, http } = setup()
    const confirm = spyOn(fixture.debugElement.injector.get(NzModalService), 'confirm').and.returnValue({ destroy: () => {} } as any)
    const saved = { artifactDigest: digest, contentSHA256: sha, sizeBytes: 2, retainedAt: item.createdAt, verification: 'unverified' }
    component.toggle()
    http.expectOne(base).flush({ state: 'captured', items: [item], retentionAvailable: false, retentionStatus: 'weak_key', retained: [saved] })
    fixture.detectChanges()

    expect(component.canForget(item)).toBeTrue()
    expect(fixture.nativeElement.textContent).toContain('Existing copies can still be downloaded or removed')
    expect(fixture.nativeElement.querySelector('button[aria-label="Retain file 1 in HAI"]')).toBeNull()
    expect((fixture.nativeElement.querySelector('button[aria-label="Remove HAI copy for file 1"]') as HTMLButtonElement).disabled).toBeFalse()

    component.confirmForget(item)
    ;(confirm.calls.mostRecent().args[0]!.nzOnOk as Function)()
    const request = http.expectOne(`${base}/${digest}/retained`)
    expect(request.request.headers.get('If-Match')).toBe(`"${sha}"`)
    request.flush(null, { status: 204, statusText: 'No Content' })
    http.expectOne(base).flush({ state: 'captured', items: [item], retentionAvailable: false, retentionStatus: 'weak_key', retained: [] })
    fixture.destroy()
  })

  it('does not try to read or remove a retained copy while its encryption key is unavailable', () => {
    const { fixture, component, http } = setup()
    const confirm = spyOn(fixture.debugElement.injector.get(NzModalService), 'confirm')
    component.toggle()
    http.expectOne(base).flush({ state: 'captured', items: [item], retentionAvailable: false, retentionStatus: 'key_unavailable', retained: [{ artifactDigest: digest, contentSHA256: sha, sizeBytes: 2, retainedAt: item.createdAt, verification: 'unverified' }] })
    fixture.detectChanges()

    expect(fixture.nativeElement.textContent).toContain('encryption key is unavailable')
    expect(component.canDownload(item)).toBeFalse()
    expect(component.canForget(item)).toBeFalse()
    expect((fixture.nativeElement.querySelector('button[aria-label="Download file 1"]') as HTMLButtonElement).disabled).toBeTrue()
    expect((fixture.nativeElement.querySelector('button[aria-label="Remove HAI copy for file 1"]') as HTMLButtonElement).disabled).toBeTrue()
    component.download(item)
    component.confirmForget(item)
    http.expectNone(`${base}/${digest}/download`)
    http.expectNone(`${base}/${digest}/retained`)
    expect(confirm).not.toHaveBeenCalled()
    fixture.destroy()
  })

  it('rejects contradictory retention capability data instead of enabling a write', () => {
    const { fixture, component, http } = setup()
    component.toggle()
    http.expectOne(base).flush({ state: 'captured', items: [item], retentionAvailable: 'false', retentionStatus: 'mystery' })
    fixture.detectChanges()
    expect(component.view).toBeUndefined()
    expect(component.error).toContain('Invalid file metadata')
    expect(fixture.nativeElement.querySelector('button[aria-label="Retain file 1 in HAI"]')).toBeNull()
    fixture.destroy()
  })

  it('does not delete if the retained version changes while confirmation is open', () => {
    const { fixture, component, http } = setup()
    const confirm = spyOn(fixture.debugElement.injector.get(NzModalService), 'confirm').and.returnValue({ destroy: () => {} } as any)
    component.toggle()
    http.expectOne(base).flush({ state: 'captured', items: [item], retentionAvailable: true, retained: [{ artifactDigest: digest, contentSHA256: sha, sizeBytes: 2, retainedAt: item.createdAt, verification: 'unverified' }] })
    component.confirmForget(item)
    const onConfirm = confirm.calls.mostRecent().args[0]!.nzOnOk as Function
    const newerSHA = 'b'.repeat(64)
    component.view = { ...component.view!, retained: [{ artifactDigest: digest, contentSHA256: newerSHA, sizeBytes: 2, retainedAt: item.createdAt, verification: 'unverified' }] }

    onConfirm()

    http.expectNone(`${base}/${digest}/retained`)
    expect(component.downloading).toBe('')
    fixture.destroy()
  })

  it('invalidates a removal confirmation on collapse or record change', () => {
    const { fixture, component, http } = setup()
    const destroy = jasmine.createSpy('destroy')
    const confirm = spyOn(fixture.debugElement.injector.get(NzModalService), 'confirm').and.returnValue({ destroy } as any)
    component.toggle()
    http.expectOne(base).flush({ state: 'captured', items: [item], retentionAvailable: true, retained: [{ artifactDigest: digest, contentSHA256: sha, sizeBytes: 2, retainedAt: item.createdAt, verification: 'unverified' }] })
    component.confirmForget(item)
    const options = confirm.calls.mostRecent().args[0]!
    component.toggle()
    expect(destroy).toHaveBeenCalled()
    ;(options.nzOnOk as Function)()
    http.expectNone(`${base}/${digest}/retained`)
    fixture.destroy()
  })

  it('keeps the saved record on stale-version failure without exposing server details', () => {
    const { fixture, component, http } = setup()
    const confirm = spyOn(fixture.debugElement.injector.get(NzModalService), 'confirm').and.returnValue({ destroy: () => {} } as any)
    component.toggle()
    http.expectOne(base).flush({ state: 'captured', items: [item], retentionAvailable: true, retained: [{ artifactDigest: digest, contentSHA256: sha, sizeBytes: 2, retainedAt: item.createdAt, verification: 'unverified' }] })
    component.confirmForget(item)
    ;(confirm.calls.mostRecent().args[0]!.nzOnOk as Function)()
    http.expectOne(`${base}/${digest}/retained`).flush({ error: 'PRIVATE_SENTINEL' }, { status: 412, statusText: 'Precondition Failed' })
    const latestSHA = 'b'.repeat(64)
    http.expectOne(base).flush({ state: 'captured', items: [item], retentionAvailable: true, retained: [{ artifactDigest: digest, contentSHA256: latestSHA, sizeBytes: 2, retainedAt: item.createdAt, verification: 'unverified' }] })
    expect(component.retained(item)?.contentSHA256).toBe(latestSHA)
    expect(component.downloadError).toContain('changed')
    expect(component.downloadError).toContain('HAI refreshed the file list')
    expect(component.downloadError).not.toContain('PRIVATE_SENTINEL')
    fixture.detectChanges()
    expect(fixture.nativeElement.querySelector('[role="alert"]')?.textContent).toContain('changed')
    fixture.destroy()
  })

  it('lets removal finish when collapsed and keeps the result visible outside the section', () => {
    const { fixture, component, http } = setup()
    const confirm = spyOn(fixture.debugElement.injector.get(NzModalService), 'confirm').and.returnValue({ destroy: () => {} } as any)
    component.toggle()
    http.expectOne(base).flush({ state: 'captured', items: [item], retentionAvailable: true, retained: [{ artifactDigest: digest, contentSHA256: sha, sizeBytes: 2, retainedAt: item.createdAt, verification: 'unverified' }] })
    component.confirmForget(item)
    ;(confirm.calls.mostRecent().args[0]!.nzOnOk as Function)()
    const request = http.expectOne(`${base}/${digest}/retained`)
    component.toggle()
    fixture.detectChanges()
    expect(request.cancelled).toBeFalse()
    expect(component.expanded).toBeFalse()
    expect(fixture.nativeElement.querySelector('.download-status[aria-busy="true"]')).not.toBeNull()
    request.flush(null, { status: 204, statusText: 'No Content' })
    http.expectOne(base).flush({ state: 'captured', items: [item], retentionAvailable: true, retained: [] })
    expect(component.retained(item)).toBeUndefined()
    expect(component.downloading).toBe('')
    fixture.detectChanges()
    expect(fixture.nativeElement.querySelector('section')).toBeNull()
    expect(fixture.nativeElement.querySelector('.download-feedback[role="status"]')?.textContent).toContain('HAI copy removed')
    fixture.destroy()
  })

  it('keeps a removal alive across execution changes and never applies old data to the new execution', () => {
    const { fixture, component, http } = setup()
    const confirm = spyOn(fixture.debugElement.injector.get(NzModalService), 'confirm').and.returnValue({ destroy: () => {} } as any)
    component.toggle()
    http.expectOne(base).flush({ state: 'captured', items: [item], retentionAvailable: true, retained: [{ artifactDigest: digest, contentSHA256: sha, sizeBytes: 2, retainedAt: item.createdAt, verification: 'unverified' }] })
    component.confirmForget(item)
    ;(confirm.calls.mostRecent().args[0]!.nzOnOk as Function)()
    const request = http.expectOne(`${base}/${digest}/retained`)

    fixture.componentRef.setInput('eventId', 'event-2')
    fixture.detectChanges()
    expect(request.cancelled).toBeFalse()
    component.toggle()
    expect(component.expanded).toBeTrue()
    http.expectNone(nextBase)

    request.flush(null, { status: 204, statusText: 'No Content' })
    const nextDigest = 'c'.repeat(64)
    http.expectOne(nextBase).flush({ state: 'captured', items: [{ ...item, artifactDigest: nextDigest }], retentionAvailable: true, retained: [] })
    fixture.detectChanges()
    expect(component.view?.items[0].artifactDigest).toBe(nextDigest)
    expect(component.message).toContain('previous execution')
    expect(fixture.nativeElement.textContent).toContain('previous execution')
    fixture.destroy()
  })

  it('keeps a retention alive across execution changes without attaching it to the new execution', () => {
    const { fixture, component, http } = setup()
    component.toggle()
    http.expectOne(base).flush({ state: 'captured', items: [item], retentionAvailable: true, retained: [] })
    component.retain(item)
    const request = http.expectOne(`${base}/${digest}/retain`)

    fixture.componentRef.setInput('eventId', 'event-2')
    fixture.detectChanges()
    expect(request.cancelled).toBeFalse()
    component.toggle()
    request.flush({ artifactDigest: digest, contentSHA256: sha, sizeBytes: 2, retainedAt: item.createdAt, verification: 'unverified' })
    const nextDigest = 'c'.repeat(64)
    http.expectOne(nextBase).flush({ state: 'captured', items: [{ ...item, artifactDigest: nextDigest }], retentionAvailable: true, retained: [] })
    fixture.detectChanges()
    expect(component.view?.items[0].artifactDigest).toBe(nextDigest)
    expect(component.view?.retained).toEqual([])
    expect(component.message).toContain('retained for a previous execution')
    fixture.destroy()
  })

  it('continues a confirmed retention after the detail component closes and restores its result when reopened', () => {
    const { fixture, component, http } = setup()
    component.toggle()
    http.expectOne(base).flush({ state: 'captured', items: [item], retentionAvailable: true, retained: [] })
    component.retain(item)
    const request = http.expectOne(`${base}/${digest}/retain`)

    fixture.destroy()
    expect(request.cancelled).toBeFalse()

    const reopened = TestBed.createComponent(OpenclawArtifactsComponent)
    reopened.componentRef.setInput('eventId', 'event-1')
    reopened.detectChanges()
    const reopenedComponent = reopened.componentInstance
    expect(reopenedComponent.downloading).toBe(digest)
    reopenedComponent.toggle()
    http.expectNone(base)
    request.flush({ artifactDigest: digest, contentSHA256: sha, sizeBytes: 2, retainedAt: item.createdAt, verification: 'unverified' })
    http.expectOne(base).flush({ state: 'captured', items: [item], retentionAvailable: true, retained: [{ artifactDigest: digest, contentSHA256: sha, sizeBytes: 2, retainedAt: item.createdAt, verification: 'unverified' }] })
    reopened.detectChanges()
    expect(reopenedComponent.retained(item)?.contentSHA256).toBe(sha)
    expect(reopenedComponent.message).toContain('HAI confirms the encrypted copy is retained')
    expect(reopened.nativeElement.textContent).toContain('deliverable correctness has not been verified')
    reopened.destroy()
  })

  it('reconciles a replayed terminal success with the latest server state', () => {
    const http = TestBed.inject(HttpTestingController)
    const keepStateObservable = TestBed.inject(OpenclawArtifactOperationsService).watch('event-1').subscribe()
    const first = setup()
    const confirm = spyOn(first.fixture.debugElement.injector.get(NzModalService), 'confirm').and.returnValue({ destroy: () => {} } as any)
    const saved = { artifactDigest: digest, contentSHA256: sha, sizeBytes: 2, retainedAt: item.createdAt, verification: 'unverified' }
    first.component.toggle()
    http.expectOne(base).flush({ state: 'captured', items: [item], retentionAvailable: true, retained: [saved] })
    first.component.confirmForget(item)
    ;(confirm.calls.mostRecent().args[0]!.nzOnOk as Function)()
    http.expectOne(`${base}/${digest}/retained`).flush(null, { status: 204, statusText: 'No Content' })
    http.expectOne(base).flush({ state: 'captured', items: [item], retentionAvailable: true, retained: [saved] })
    first.fixture.destroy()

    const reopened = TestBed.createComponent(OpenclawArtifactsComponent)
    reopened.componentRef.setInput('eventId', 'event-1')
    reopened.detectChanges()
    const reopenedComponent = reopened.componentInstance
    expect(reopenedComponent.message).toContain('Checking the latest HAI file state')
    reopenedComponent.toggle()
    http.expectOne(base).flush({ state: 'captured', items: [item], retentionAvailable: true, retained: [saved] })
    expect(reopenedComponent.downloadError).toContain('still shows the retained copy')
    expect(reopenedComponent.message).not.toContain('HAI copy removed')

    keepStateObservable.unsubscribe()
    reopened.destroy()
  })

  it('reconciles an ambiguous removal response against refreshed server state', () => {
    const { fixture, component, http } = setup()
    const confirm = spyOn(fixture.debugElement.injector.get(NzModalService), 'confirm').and.returnValue({ destroy: () => {} } as any)
    component.toggle()
    http.expectOne(base).flush({ state: 'captured', items: [item], retentionAvailable: true, retained: [{ artifactDigest: digest, contentSHA256: sha, sizeBytes: 2, retainedAt: item.createdAt, verification: 'unverified' }] })
    component.confirmForget(item)
    ;(confirm.calls.mostRecent().args[0]!.nzOnOk as Function)()
    http.expectOne(`${base}/${digest}/retained`).flush({ error: 'PRIVATE_SENTINEL' }, { status: 503, statusText: 'Unavailable' })
    expect(component.downloadError).toContain('Checking the current')
    http.expectOne(base).flush({ state: 'captured', items: [item], retentionAvailable: true, retained: [], storageUsage: { bytesUsed: 0, filesUsed: 0, bytesLimit: 67108864, filesLimit: 128 } })
    expect(component.downloadError).toBe('')
    expect(component.message).toContain('refreshed list confirms HAI no longer holds')
    fixture.destroy()
  })

  it('explains that a retained copy was preserved when its encryption key cannot authenticate it', () => {
    const { fixture, component, http } = setup()
    const confirm = spyOn(fixture.debugElement.injector.get(NzModalService), 'confirm').and.returnValue({ destroy: () => {} } as any)
    const saved = { artifactDigest: digest, contentSHA256: sha, sizeBytes: 2, retainedAt: item.createdAt, verification: 'unverified' as const }
    component.toggle()
    http.expectOne(base).flush({ state: 'captured', items: [item], retentionAvailable: true, retained: [saved] })
    component.confirmForget(item)
    ;(confirm.calls.mostRecent().args[0]!.nzOnOk as Function)()
    http.expectOne(`${base}/${digest}/retained`).flush(
      { code: 'retained_copy_unreadable', error: 'private backend detail' }, { status: 503, statusText: 'Unavailable' },
    )

    expect(component.downloadError).toContain('copy was preserved')
    expect(component.downloadError).not.toContain('private backend detail')
    http.expectOne(base).flush({ state: 'captured', items: [item], retentionAvailable: true, retained: [saved] })
    expect(component.retained(item)).toEqual(saved)
    expect(component.downloadError).toContain('restore the correct key')
    fixture.destroy()
  })

  it('retains only after explicit action and displays persisted unverified status', () => {
    const { fixture, component, http } = setup()
    component.toggle()
    http.expectOne(base).flush({ state: 'captured', attempts: 1, items: [item], retentionAvailable: true, retained: [] })
    http.expectNone(`${base}/${digest}/retain`)
    component.retain(item)
    component.retain(item)
    const request = http.expectOne(`${base}/${digest}/retain`)
    expect(request.request.method).toBe('POST')
    request.flush({ artifactDigest: digest, contentSHA256: sha, sizeBytes: 2, retainedAt: '2026-09-06T10:00:00Z', verification: 'unverified' })
    http.expectOne(base).flush({ state: 'captured', attempts: 1, items: [item], retentionAvailable: true, retained: [{ artifactDigest: digest, contentSHA256: sha, sizeBytes: 2, retainedAt: '2026-09-06T10:00:00Z', verification: 'unverified' }], storageUsage: { bytesUsed: 2, filesUsed: 1, bytesLimit: 67108864, filesLimit: 128 } })
    fixture.detectChanges()
    expect(fixture.nativeElement.textContent).toContain('Retained in HAI')
    expect(fixture.nativeElement.textContent).toContain('not a verified deliverable')
    expect(fixture.nativeElement.textContent).toContain('1 of 128 files')
    const usage = fixture.nativeElement.querySelector('.storage-usage') as HTMLElement
    expect(usage.textContent).toContain('retained file content quota')
    expect(usage.textContent).toContain('bytes of retained content')
    expect(usage.getAttribute('title')).toContain('not physical database storage')
    component.retain(item)
    http.expectNone(`${base}/${digest}/retain`)
    fixture.destroy()
  })

  it('does not retain when disabled and does not claim success on quota failure', () => {
    const { fixture, component, http } = setup()
    component.toggle()
    http.expectOne(base).flush({ state: 'captured', attempts: 1, items: [item] })
    component.retain(item)
    http.expectNone(`${base}/${digest}/retain`)
    component.load()
    http.expectOne(base).flush({ state: 'captured', attempts: 1, items: [item], retentionAvailable: true })
    component.retain(item)
    http.expectOne(`${base}/${digest}/retain`).flush({ error: 'PRIVATE_SENTINEL' }, { status: 409, statusText: 'Conflict' })
    http.expectOne(base).flush({ state: 'captured', attempts: 1, items: [item], retentionAvailable: true, retained: [], storageUsage: { bytesUsed: 0, filesUsed: 0, bytesLimit: 67108864, filesLimit: 128 } })
    fixture.detectChanges()
    expect(fixture.nativeElement.textContent).not.toContain('Retained in HAI')
    expect(fixture.nativeElement.textContent).not.toContain('PRIVATE_SENTINEL')
    expect(component.downloadError).toContain('limit')
    fixture.destroy()
  })

  it('reports write authorization failure without exposing server response details', () => {
    const { fixture, component, http } = setup()
    component.toggle()
    http.expectOne(base).flush({ state: 'captured', attempts: 1, items: [item], retentionAvailable: true, retentionStatus: 'available', retained: [] })
    component.retain(item)
    http.expectOne(`${base}/${digest}/retain`).flush({ error: 'PRIVATE_SENTINEL' }, { status: 403, statusText: 'Forbidden' })
    http.expectOne(base).flush({ state: 'captured', attempts: 1, items: [item], retentionAvailable: true, retentionStatus: 'available', retained: [] })
    fixture.detectChanges()

    expect(component.downloadError).toContain('not authorized to save a copy')
    expect(fixture.nativeElement.textContent).not.toContain('PRIVATE_SENTINEL')
    expect(component.retained(item)).toBeUndefined()
    fixture.destroy()
  })

  it('reconciles an ambiguous retention response against refreshed server state', () => {
    const { fixture, component, http } = setup()
    component.toggle()
    http.expectOne(base).flush({ state: 'captured', attempts: 1, items: [item], retentionAvailable: true, retained: [] })
    component.retain(item)
    http.expectOne(`${base}/${digest}/retain`).flush({ error: 'PRIVATE_SENTINEL' }, { status: 503, statusText: 'Unavailable' })
    expect(component.downloadError).toContain('Checking the current')
    http.expectOne(base).flush({ state: 'captured', attempts: 1, items: [item], retentionAvailable: true, retained: [{ artifactDigest: digest, contentSHA256: sha, sizeBytes: 2, retainedAt: item.createdAt, verification: 'unverified' }], storageUsage: { bytesUsed: 2, filesUsed: 1, bytesLimit: 67108864, filesLimit: 128 } })
    expect(component.downloadError).toBe('')
    expect(component.message).toContain('HAI confirms the encrypted copy is retained')
    fixture.destroy()
  })

  it('loads on expansion only, keeps unknown size distinct and cancels obsolete reads', () => {
    const { fixture, component, http } = setup()
    http.expectNone(base)
    component.toggle()
    component.load()
    http.expectOne(base).flush({ state: 'captured', attempts: 1, items: [item] })
    fixture.detectChanges()
    expect(fixture.nativeElement.textContent).toContain('Size unknown')
    component.toggle()
    component.toggle()
    http.expectNone(base)
    component.load()
    const request = http.expectOne(base)
    fixture.componentRef.setInput('eventId', 'event-2')
    fixture.detectChanges()
    expect(request.cancelled).toBeTrue()
    expect(component.view).toBeUndefined()
    fixture.destroy()
  })

  it('downloads only a listed reference on demand, verifies bytes, and prevents duplicate requests', async () => {
    const { fixture, component, http } = setup()
    const save = spyOn(component, 'saveBlob')
    component.toggle()
    http.expectOne(base).flush({ state: 'captured', attempts: 1, items: [item] })
    http.expectNone(`${base}/${digest}/download`)
    component.download({ ...item, artifactDigest: 'b'.repeat(64) })
    http.expectNone(`${base}/${'b'.repeat(64)}/download`)
    component.download(item)
    component.download(item)
    const request = http.expectOne(`${base}/${digest}/download`)
    expect(request.request.responseType).toBe('blob')
    request.flush(new Blob(['hi']), { headers: { 'X-Content-SHA256': sha } })
    await waitForDownload(component)
    expect(save).toHaveBeenCalledOnceWith(jasmine.any(Blob), `openclaw-${digest.slice(0, 12)}.bin`)
    expect(component.message).toContain('Download requested')
    fixture.destroy()
  })

  it('refuses an invalid checksum and never renders raw server errors', async () => {
    const { fixture, component, http } = setup()
    const save = spyOn(component, 'saveBlob')
    component.toggle()
    http.expectOne(base).flush({ state: 'captured', attempts: 1, items: [item] })
    component.download(item)
    http.expectOne(`${base}/${digest}/download`).flush(new Blob(['changed']), { headers: { 'X-Content-SHA256': sha } })
    await waitForDownload(component)
    expect(save).not.toHaveBeenCalled()
    expect(component.downloadError).toContain('integrity')
    component.download(item)
    http.expectOne(`${base}/${digest}/download`).flush(new Blob(['private token']), { status: 422, statusText: 'Unsupported' })
    fixture.detectChanges()
    expect(fixture.nativeElement.textContent).not.toContain('private token')
    expect(component.downloadError).toContain('8 MiB')
    fixture.destroy()
  })

  it('cancels downloads on record change and on explicit cancellation', () => {
    const { fixture, component, http } = setup()
    component.toggle()
    http.expectOne(base).flush({ state: 'captured', attempts: 1, items: [item] })
    component.download(item)
    const first = http.expectOne(`${base}/${digest}/download`)
    component.cancelDownload()
    expect(first.cancelled).toBeTrue()
    expect(component.downloading).toBe('')
    component.download(item)
    const second = http.expectOne(`${base}/${digest}/download`)
    fixture.componentRef.setInput('eventId', 'event-2')
    fixture.detectChanges()
    expect(second.cancelled).toBeTrue()
    fixture.destroy()
  })

  it('does not turn failed collection into an empty success or fetch known oversized content', () => {
    const { fixture, component, http } = setup()
    component.toggle()
    http.expectOne(base).flush({ state: 'retry_exhausted', attempts: 8, items: [] })
    fixture.detectChanges()
    expect(fixture.nativeElement.textContent).toContain('Automatic collection stopped')
    expect(fixture.nativeElement.textContent).not.toContain('No files reported')
    component.load()
    const large = { ...item, sizeBytes: 8388609 }
    http.expectOne(base).flush({ state: 'captured', attempts: 1, items: [large] })
    component.download(large)
    http.expectNone(`${base}/${digest}/download`)
    expect(component.canDownload(large)).toBeFalse()
    fixture.destroy()
  })

  it('announces active metadata-only collection distinctly without implying completion or verification', () => {
    const { fixture, component, http } = setup()
    component.toggle()
    http.expectOne(base).flush({ state: 'collecting', attempts: 2, items: [] })
    fixture.detectChanges()

    const status = fixture.nativeElement.querySelector('[role="status"].metadata-collection-active') as HTMLElement | null
    expect(status).not.toBeNull()
    expect(status?.getAttribute('aria-live')).toBe('polite')
    expect(status?.textContent).toContain('Metadata collection in progress')
    expect(status?.textContent).toContain('metadata only')
    expect(status?.textContent).toContain('not been fetched or verified')
    expect(status?.textContent).not.toContain('Automatic collection stopped')
    expect(fixture.nativeElement.textContent).not.toContain('No files reported')
    expect(Array.from(fixture.nativeElement.querySelectorAll('button') as NodeListOf<HTMLButtonElement>).some(button => /retry|collect now/i.test(`${button.textContent} ${button.getAttribute('aria-label') || ''}`))).toBeFalse()
    expect(component.stateLabel('collecting')).not.toBe(component.stateLabel('pending'))
    expect(component.stateLabel('collecting')).not.toBe(component.stateLabel('retry_exhausted'))
    fixture.destroy()
  })

  it('keeps unknown artifact states informational and never treats them as empty success', () => {
    const { fixture, component, http } = setup()
    component.toggle()
    http.expectOne(base).flush({ state: 'future_state', attempts: 0, items: [] })
    fixture.detectChanges()

    const status = fixture.nativeElement.querySelector('[role="status"]') as HTMLElement | null
    expect(status?.textContent).toContain('File collection state unknown')
    expect(fixture.nativeElement.textContent).not.toContain('No files reported')
    expect(fixture.nativeElement.querySelector('.metadata-collection-active')).toBeNull()
    expect(component.stateLabel('future_state')).toBe('File collection state unknown.')
    expect(Array.from(fixture.nativeElement.querySelectorAll('button') as NodeListOf<HTMLButtonElement>).some(button => /retry|collect now/i.test(`${button.textContent} ${button.getAttribute('aria-label') || ''}`))).toBeFalse()
    fixture.destroy()
  })

  it('rejects malformed metadata instead of rendering broken file rows', () => {
    const { fixture, component, http } = setup()
    component.toggle()
    http.expectOne(base).flush({ state: 'captured', attempts: 1, items: [null] })
    fixture.detectChanges()
    expect(component.view).toBeUndefined()
    expect(component.error).toContain('Invalid file metadata')
    fixture.destroy()
  })

  it('stops an in-flight transfer when its controls are collapsed', () => {
    const { fixture, component, http } = setup()
    component.toggle()
    http.expectOne(base).flush({ state: 'captured', attempts: 1, items: [item] })
    component.download(item)
    const request = http.expectOne(`${base}/${digest}/download`)
    component.toggle()
    expect(request.cancelled).toBeTrue()
    fixture.destroy()
  })

  it('does not save a file if the execution changed while reading its bytes', async () => {
    const { fixture, component, http } = setup()
    const save = spyOn(component, 'saveBlob')
    const blob = new Blob(['hi'])
    let resolveBytes!: (value: ArrayBuffer) => void
    spyOn(blob, 'arrayBuffer').and.returnValue(new Promise(resolve => { resolveBytes = resolve }))
    component.toggle()
    http.expectOne(base).flush({ state: 'captured', attempts: 1, items: [item] })
    component.download(item)
    http.expectOne(`${base}/${digest}/download`).flush(blob, { headers: { 'X-Content-SHA256': sha } })
    fixture.componentRef.setInput('eventId', 'event-2')
    fixture.detectChanges()
    resolveBytes(new TextEncoder().encode('hi').buffer)
    await new Promise(resolve => setTimeout(resolve, 0))
    expect(save).not.toHaveBeenCalled()
    expect(component.message).toBe('')
    fixture.destroy()
  })
})
