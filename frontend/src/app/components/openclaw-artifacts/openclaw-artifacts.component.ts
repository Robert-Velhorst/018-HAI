import { ChangeDetectionStrategy, ChangeDetectorRef, Component, Input, OnChanges, OnDestroy, Renderer2 } from '@angular/core'
import { CommonModule } from '@angular/common'
import { HttpClient, HttpResponse } from '@angular/common/http'
import { Subscription, timeout } from 'rxjs'
import { NzButtonModule } from 'ng-zorro-antd/button'
import { NzIconModule, NzIconService } from 'ng-zorro-antd/icon'
import { NzModalModule, NzModalRef, NzModalService } from 'ng-zorro-antd/modal'
import { DownOutline, RightOutline, ReloadOutline, DownloadOutline, SaveOutline, DeleteOutline } from '@ant-design/icons-angular/icons'
import { ArtifactMutationFailureCode, ArtifactMutationState, OpenclawArtifactOperationsService, RetainedArtifactCopy } from '../../services/openclaw-artifact-operations.service'

interface Artifact {
  artifactDigest: string; artifactType: string; mimeType?: string
  sizeBytes: number | null; createdAt: string
}
interface ArtifactView {
  state: string; attempts: number; nextAttemptAt?: string; capturedAt?: string; items: Artifact[]
  retentionAvailable?: boolean; retentionStatus?: string; retained?: RetainedArtifactCopy[]; storageUsage?: ArtifactStorageUsage
}
interface ArtifactStorageUsage { bytesUsed: number; filesUsed: number; bytesLimit: number; filesLimit: number }
const contentLimit = 8 * 1024 * 1024
const retentionStatuses = new Set(['available', 'weak_key', 'key_unavailable', 'unavailable'])
const unreadableRetainedCopyMessage = 'HAI could not authenticate this encrypted copy with the configured key. The copy was preserved; restore the correct key before trying again.'

@Component({
  selector: 'app-openclaw-artifacts', standalone: true,
  imports: [CommonModule, NzButtonModule, NzIconModule, NzModalModule],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <button nz-button nzType="link" type="button" (click)="toggle()" [attr.aria-expanded]="expanded" [attr.aria-controls]="panelId" title="Inspect files reported by this OpenClaw execution">
      <span nz-icon [nzType]="expanded ? 'down' : 'right'" aria-hidden="true"></span>OpenClaw files
    </button>
    <div class="download-status" *ngIf="downloading" [attr.aria-busy]="!!downloading">
      <span role="status" aria-live="polite">{{ operationLabel() }}</span>
      <span *ngIf="retaining || removing">This operation continues if the section is closed.</span>
      <button *ngIf="!retaining && !removing" nz-button type="button" (click)="cancelDownload()">Cancel download</button>
    </div>
    <p class="download-feedback" *ngIf="downloadError" role="alert">{{ downloadError }}</p>
    <p class="download-feedback" *ngIf="message" role="status" aria-live="polite">{{ message }}</p>
    <div class="artifact-panel" [id]="panelId" [hidden]="!expanded">
    <section *ngIf="expanded" aria-label="OpenClaw files" [attr.aria-busy]="loading || !!downloading">
      <header><strong>Execution files</strong><button nz-button nzType="text" type="button" (click)="load()" [disabled]="loading || !!downloading" aria-label="Refresh files" title="Reload stored file metadata"><span nz-icon nzType="reload" aria-hidden="true"></span></button></header>
      <p class="privacy-note">Files are scoped to the authenticated account that owns this execution. Opening this section loads metadata only; HAI fetches file contents only after you choose Download or Retain. Saved copies are encrypted and remain unverified.</p>
      <p *ngIf="loading" role="status">Loading files...</p>
      <p *ngIf="error" role="alert">{{ error }}</p>
      <ng-container *ngIf="view as data">
        <p *ngIf="data.state !== 'captured'" role="status" aria-live="polite" [class.metadata-collection-active]="data.state === 'collecting'">
          <strong *ngIf="data.state === 'collecting'">Metadata collection in progress. </strong>{{ stateLabel(data.state) }}
        </p>
        <p *ngIf="data.state === 'captured' && !data.items.length">No files reported by this execution.</p>
        <p *ngIf="data.retentionStatus === 'weak_key'" role="status">New HAI copies are disabled because the configured retention key does not meet policy. Existing copies can still be downloaded or removed.</p>
        <p *ngIf="data.retentionStatus === 'key_unavailable'" role="status">HAI can show retained-file metadata, but its encryption key is unavailable. Existing retained copies cannot be opened or removed until the key is restored.</p>
        <p *ngIf="data.retentionStatus === 'unavailable'" role="status">Encrypted retention is not available on this HAI instance. No automatic copy is being saved; an on-demand download may still be available under runtime policy.</p>
        <p *ngIf="data.retentionAvailable" class="retention-scope">New encrypted copies are stored under the execution owner’s account. HAI checks account permissions when saving.</p>
        <small *ngIf="data.capturedAt">Metadata collected {{ data.capturedAt | date:'short' }}</small>
        <small *ngIf="data.state === 'pending' && data.nextAttemptAt">Next collection eligible {{ data.nextAttemptAt | date:'short' }}; subject to runtime availability.</small>
        <ul>
          <li *ngFor="let item of data.items; let index = index">
            <div class="file-summary"><strong>File {{ index + 1 }} <span class="muted">{{ item.artifactType }}</span></strong>
              <small>{{ item.mimeType || 'Type unknown' }} &middot; {{ item.sizeBytes === null ? 'Size unknown' : (item.sizeBytes | number) + ' bytes' }}</small>
              <small *ngIf="item.sizeBytes !== null && item.sizeBytes > limit">Exceeds the 8 MiB download limit.</small>
              <small *ngIf="retained(item) as saved">Retained in HAI {{ saved.retainedAt | date:'short' }}; not a verified deliverable.</small>
              <small *ngIf="retained(item) && data.retentionStatus === 'key_unavailable'" role="status">This encrypted copy cannot be opened or removed until the retention key is restored.</small>
            </div>
            <button nz-button type="button" (click)="download(item)" [disabled]="loading || !!downloading || !canDownload(item)" [attr.aria-label]="'Download file ' + (index + 1)" [title]="retained(item) && data.retentionStatus === 'key_unavailable' ? 'Unavailable until the retention key is restored' : 'Retrieve through HAI, check integrity, and download without opening the file'"><span nz-icon nzType="download" aria-hidden="true"></span>Download</button>
            <button *ngIf="data.retentionAvailable && !retained(item)" nz-button type="button" (click)="retain(item)" [disabled]="loading || !!downloading || !canDownload(item)" [attr.aria-label]="'Retain file ' + (index + 1) + ' in HAI'" title="Fetch this file only now and keep an encrypted copy for the authenticated execution owner. This does not verify the result."><span nz-icon nzType="save" aria-hidden="true"></span>Retain in HAI</button>
            <button *ngIf="retained(item)" nz-button nzType="default" nzDanger type="button" (click)="confirmForget(item)" [disabled]="!!downloading || loading || !canForget(item)" [attr.aria-label]="'Remove HAI copy for file ' + (index + 1)" [title]="canForget(item) ? 'Remove only HAI’s encrypted copy after confirmation' : 'Unavailable until the retention key is restored'"><span nz-icon nzType="delete" aria-hidden="true"></span>Remove HAI copy</button>
            <details><summary>Source and reference</summary><dl><dt>Source</dt><dd>OpenClaw execution</dd><dt>Recorded</dt><dd>{{ item.createdAt | date:'short' }}</dd><dt>Reference fingerprint</dt><dd>{{ item.artifactDigest }}</dd></dl></details>
          </li>
        </ul>
        <p *ngIf="data.storageUsage as usage" class="storage-usage" title="Owner-scoped retained content quota reported by HAI; this is not physical database storage.">This account’s retained file content quota: {{ usage.bytesUsed | number }} of {{ usage.bytesLimit | number }} bytes of retained content · {{ usage.filesUsed }} of {{ usage.filesLimit }} files</p>
      </ng-container>
      <small *ngIf="view?.items?.length">Runtime output, not a verified deliverable. Downloads up to 8 MiB; external origins require configuration.</small>
    </section>
    </div>
  `,
  styles: [`
    :host { display:block; min-width:0; color:var(--hai-text); }
    section { border-top:1px solid var(--hai-border); padding:12px 0; margin-top:8px; }
    header, .download-status { display:flex; align-items:center; justify-content:space-between; gap:12px; flex-wrap:wrap; }
    .download-feedback { margin:8px 0; }
    .privacy-note, .retention-scope { padding:8px 10px; border-left:3px solid var(--hai-border); background:var(--hai-surface); }
    .metadata-collection-active { padding:8px 10px; border-left:3px solid var(--hai-info, var(--hai-border)); background:var(--hai-surface); }
    header button { color:var(--hai-text); }
    ul { list-style:none; padding:0; margin:8px 0; }
    li { display:flex; flex-wrap:wrap; align-items:center; gap:12px; padding:12px 0; border-bottom:1px solid var(--hai-border); }
    .file-summary { flex:1 1 180px; min-width:0; } small { display:block; } .muted, small, dt { color:var(--hai-text-soft); }
    .muted { font-weight:400; margin-left:6px; } details { flex-basis:100%; min-width:0; font-size:12px; }
    summary { cursor:pointer; padding:8px 0; } dd { margin:4px 0 8px; } p { margin:8px 0; }
    strong, small, p, dd { overflow-wrap:anywhere; } button { min-height:32px; }
    button:focus-visible, summary:focus-visible { outline:2px solid currentColor; outline-offset:3px; }
  `],
})
export class OpenclawArtifactsComponent implements OnChanges, OnDestroy {
  private static nextPanelId = 0
  @Input() eventId = ''
  readonly panelId = `openclaw-artifacts-panel-${OpenclawArtifactsComponent.nextPanelId++}`
  readonly limit = contentLimit
  expanded = false
  loading = false
  error = ''
  downloading = ''
  operationEventId = ''
  retaining = false
  removing = false
  private locallyStartedMutation = false
  private destroyed = false
  downloadError = ''
  message = ''
  view?: ArtifactView
  private read?: Subscription
  private transfer?: Subscription
  private operationState?: Subscription
  private generation = 0
  private objectURLs = new Map<string, ReturnType<typeof setTimeout>>()
  private confirmation?: NzModalRef
  private confirmationGeneration = 0
  private staleVersionRefreshEventId = ''
  private reconcileAfterRefresh = new Map<string, { action: 'retain' | 'remove'; digest: string; failureCode?: ArtifactMutationFailureCode }>()
  constructor(private http: HttpClient, private cdr: ChangeDetectorRef, private modal: NzModalService, private operations: OpenclawArtifactOperationsService, private renderer: Renderer2, icons: NzIconService) {
    icons.addIcon(DownOutline, RightOutline, ReloadOutline, DownloadOutline, SaveOutline, DeleteOutline)
  }
  ngOnChanges(): void {
    this.closeConfirmation()
    this.staleVersionRefreshEventId = ''
    this.read?.unsubscribe()
    this.operationState?.unsubscribe()
    const mutationInFlight = this.retaining || this.removing
    if (!mutationInFlight) this.cancelDownload()
    this.view = undefined
    this.expanded = this.loading = false
    this.error = ''
    if (!mutationInFlight) {
      this.downloadError = ''
      this.message = this.reconcileAfterRefresh.size
        ? 'A previous execution still needs a file-state check. Reopen it and refresh the file list.'
        : ''
    }
    this.operationState = this.operations.watch(this.eventId).subscribe(state => this.applyOperationState(state))
  }
  toggle(): void {
    if (this.expanded) {
      this.expanded = false
      this.closeConfirmation()
      if (!this.retaining && !this.removing) this.cancelDownload()
      return
    }
    this.expanded = true
    if (!this.view) this.load()
  }
  load(): void {
    if (this.loading || this.downloading || !this.eventId) return
    this.loading = true
    this.error = ''
    this.read = this.http.get<ArtifactView>(`/api/v1/openclaw-artifacts/${encodeURIComponent(this.eventId)}`).pipe(timeout(10000)).subscribe({
      next: view => {
        this.loading = false
        if (!view || !Array.isArray(view.items) || view.items.length > 20 || !view.items.every(item => this.validMetadata(item)) || new Set(view.items.map(item => item.artifactDigest)).size !== view.items.length || (view.retentionAvailable !== undefined && typeof view.retentionAvailable !== 'boolean') || (view.retentionStatus !== undefined && !retentionStatuses.has(view.retentionStatus)) || (view.retentionAvailable === true && view.retentionStatus !== undefined && view.retentionStatus !== 'available') || (view.retained !== undefined && (!Array.isArray(view.retained) || view.retained.length > 20 || !view.retained.every(saved => this.validRetained(saved, view.items)) || new Set(view.retained.map(saved => saved.artifactDigest)).size !== view.retained.length)) || (view.storageUsage !== undefined && !this.validUsage(view.storageUsage))) {
          this.view = undefined
          this.error = 'Invalid file metadata. Refresh to try again.'
        } else {
          this.view = view
          this.reconcileMutationAfterRefresh(view)
          if (this.staleVersionRefreshEventId === this.eventId) {
            this.staleVersionRefreshEventId = ''
            this.downloadError = 'This retained version changed. HAI refreshed the file list; review the current copy before deciding whether to remove it.'
          }
        }
        this.cdr.markForCheck()
      },
      error: response => {
        this.view = undefined
        this.loading = false
        this.error = response.status === 404 ? 'No file record available for this execution.' : 'Files are temporarily unavailable. Refresh to try again.'
        if (this.staleVersionRefreshEventId === this.eventId) {
          this.downloadError = 'This retained version changed, but HAI could not refresh the file list. Refresh it before deciding whether to remove the copy.'
        }
        this.cdr.markForCheck()
      },
    })
  }
  canDownload(item: Artifact): boolean {
    if (this.retained(item) && this.view?.retentionStatus === 'key_unavailable') return false
    return /^[a-f0-9]{64}$/.test(item.artifactDigest) &&
      (item.sizeBytes === null || (Number.isSafeInteger(item.sizeBytes) && item.sizeBytes >= 0 && item.sizeBytes <= contentLimit))
  }
  private validMetadata(item: Artifact): boolean {
    return !!item && typeof item.artifactDigest === 'string' && /^[a-f0-9]{64}$/.test(item.artifactDigest) &&
      typeof item.artifactType === 'string' && item.artifactType.length <= 120 &&
      (item.mimeType === undefined || (typeof item.mimeType === 'string' && item.mimeType.length <= 160)) &&
      typeof item.createdAt === 'string' && Number.isFinite(Date.parse(item.createdAt)) &&
      (item.sizeBytes === null || (Number.isSafeInteger(item.sizeBytes) && item.sizeBytes >= 0))
  }
  retained(item: Artifact): RetainedArtifactCopy | undefined { return this.view?.retained?.find(saved => saved.artifactDigest === item.artifactDigest) }
  canForget(item: Artifact): boolean {
    return !!this.retained(item) && (this.view?.retentionAvailable === true || this.view?.retentionStatus === 'weak_key' || this.view?.retentionStatus === 'available')
  }
  private validRetained(saved: RetainedArtifactCopy, items: Artifact[]): boolean {
    const item = saved && items.find(item => item.artifactDigest === saved.artifactDigest)
    return !!item && /^[a-f0-9]{64}$/.test(saved.contentSHA256) && Number.isSafeInteger(saved.sizeBytes) && saved.sizeBytes >= 0 && saved.sizeBytes <= contentLimit &&
      (item.sizeBytes === null || item.sizeBytes === saved.sizeBytes) && saved.verification === 'unverified' && typeof saved.retainedAt === 'string' && Number.isFinite(Date.parse(saved.retainedAt))
  }
  private validUsage(usage: ArtifactStorageUsage): boolean {
    return !!usage && Number.isSafeInteger(usage.bytesUsed) && usage.bytesUsed >= 0 && Number.isSafeInteger(usage.filesUsed) && usage.filesUsed >= 0 && Number.isSafeInteger(usage.bytesLimit) && usage.bytesLimit > 0 && Number.isSafeInteger(usage.filesLimit) && usage.filesLimit > 0 && usage.bytesUsed <= usage.bytesLimit && usage.filesUsed <= usage.filesLimit
  }
  private reconcileMutationAfterRefresh(view: ArtifactView): void {
    const pending = this.reconcileAfterRefresh.get(this.eventId)
    if (!pending) return
    this.reconcileAfterRefresh.delete(this.eventId)
    const retained = (view.retained || []).some(saved => saved.artifactDigest === pending.digest)
    const reachedExpectedState = pending.action === 'retain' ? retained : !retained
    if (reachedExpectedState) {
      this.downloadError = ''
      this.message = pending.action === 'retain'
        ? 'HAI confirms the encrypted copy is retained; deliverable correctness has not been verified.'
        : 'The refreshed list confirms HAI no longer holds this copy; the source and audit records remain.'
      return
    }
    this.message = ''
    if (pending.failureCode === 'retained_copy_unreadable') {
      this.downloadError = unreadableRetainedCopyMessage
      return
    }
    this.downloadError = pending.action === 'retain'
      ? 'The refreshed list does not show a retained copy. Review the current state before retrying.'
      : view.retentionStatus === 'key_unavailable'
        ? 'HAI still lists this copy, but the encryption key is unavailable. Restore the key before trying to remove it.'
        : 'The refreshed list still shows the retained copy. Review the current state before retrying.'
  }
  operationLabel(): string {
    const action = this.retaining ? 'Retaining encrypted file...' : this.removing ? 'Removing HAI copy...' : 'Retrieving and checking file...'
    return this.operationEventId && this.operationEventId !== this.eventId ? `${action} for a previous execution.` : action
  }
  private applyOperationState(state: ArtifactMutationState | null): void {
    if (this.destroyed) return
    if (!state) {
      this.message = ''
      this.downloadError = ''
      this.reconcileAfterRefresh.delete(this.eventId)
      this.view = undefined
      if (this.expanded) this.load()
      this.cdr.markForCheck()
      return
    }
    if (state.eventId !== this.eventId) return
    const localOperation = this.locallyStartedMutation && this.operationEventId === state.eventId && this.downloading === state.digest
    if (localOperation) return
    if (state.status === 'running') {
      this.downloading = state.digest
      this.operationEventId = state.eventId
      this.retaining = state.action === 'retain'
      this.removing = state.action === 'remove'
      this.downloadError = ''
      this.message = state.action === 'retain' ? 'Retention is continuing from the previous view.' : 'Removal is continuing from the previous view.'
    } else {
      this.downloading = ''
      this.operationEventId = ''
      this.retaining = this.removing = false
      if (state.status === 'succeeded') {
        this.downloadError = ''
        this.message = 'Checking the latest HAI file state before confirming the previous operation.'
        this.reconcileAfterRefresh.set(state.eventId, { action: state.action, digest: state.digest })
      } else if (state.httpStatus === 412 && state.action === 'remove') {
        this.staleVersionRefreshEventId = state.eventId
        this.downloadError = 'This retained version changed. HAI is refreshing the file list; review the updated copy before deciding what to do.'
      } else if (state.httpStatus === 409) {
        this.downloadError = state.action === 'retain'
          ? 'Retention limit or version conflict. Existing copies were preserved.'
          : 'The retained copy changed or is unavailable. Refresh before trying again.'
      } else if (state.httpStatus === 401) {
        this.downloadError = 'Your HAI session has expired. Sign in again, then refresh this execution.'
      } else if (state.httpStatus === 403) {
        this.downloadError = `This account is not authorized to ${state.action === 'retain' ? 'save' : 'remove'} this copy.`
      } else if (state.httpStatus === 404) {
        this.downloadError = 'HAI could not find a matching file for this execution. Refresh before retrying.'
      } else if (state.failureCode === 'retained_copy_unreadable') {
        this.downloadError = unreadableRetainedCopyMessage
        this.reconcileAfterRefresh.set(state.eventId, { action: state.action, digest: state.digest, failureCode: state.failureCode })
      } else if (state.httpStatus === 422) {
        this.downloadError = 'HAI rejected this file under the runtime origin, format, or size policy. Existing copies were preserved.'
      } else if (state.httpStatus === 429) {
        this.downloadError = 'HAI is busy with another artifact transfer. Wait briefly, then refresh before retrying.'
      } else if (state.httpStatus === 503) {
        this.downloadError = state.action === 'retain'
          ? 'Encrypted retention is unavailable or its key is not ready. Checking the saved-copy list.'
          : 'HAI retention is unavailable or its encryption key is not ready. Checking the saved-copy list.'
        this.reconcileAfterRefresh.set(state.eventId, { action: state.action, digest: state.digest })
      } else {
        this.downloadError = state.action === 'retain'
          ? 'Retention could not be confirmed. Checking the current retained-file list.'
          : 'Removal could not be confirmed. Checking the current retained-file list.'
        this.reconcileAfterRefresh.set(state.eventId, { action: state.action, digest: state.digest })
      }
      if (this.expanded) this.load()
    }
    this.cdr.markForCheck()
  }
  confirmForget(item: Artifact): void {
    const saved = this.retained(item)
    if (!saved || this.loading || this.downloading || !this.expanded || !this.canForget(item) || !this.eventId) return
    const eventId = this.eventId
    const contentSHA256 = saved.contentSHA256
    const fileNumber = (this.view?.items.findIndex(candidate => candidate.artifactDigest === item.artifactDigest) ?? -1) + 1
    const generation = this.generation
    const confirmationGeneration = ++this.confirmationGeneration
    this.closeConfirmation(false)
    this.confirmation = this.modal.confirm({
      nzTitle: 'Remove the retained copy from HAI?',
      nzContent: `File ${fileNumber}. Only this account’s encrypted HAI copy will be removed. The OpenClaw source file and HAI source and audit records will remain.`,
      nzOkText: 'Remove HAI copy', nzOkDanger: true, nzCancelText: 'Keep copy', nzAutofocus: 'cancel',
      nzOnOk: () => this.forget(item, eventId, contentSHA256, generation, confirmationGeneration),
    })
    const confirmation = this.confirmation
    const titleId = `${this.panelId}-forget-title-${confirmationGeneration}`
    const labelConfirmation = () => {
      const dialog = confirmation.getElement?.()
      const title = dialog?.querySelector('.ant-modal-confirm-title')
      if (!dialog || !title) return
      this.renderer.setAttribute(dialog, 'aria-modal', 'true')
      this.renderer.setAttribute(title, 'id', titleId)
      this.renderer.setAttribute(dialog, 'aria-labelledby', titleId)
    }
    labelConfirmation()
    confirmation.afterOpen?.subscribe(labelConfirmation)
  }
  private forget(item: Artifact, eventId: string, contentSHA256: string, generation: number, confirmationGeneration: number): Promise<void> {
    const current = this.view?.retained?.find(saved => saved.artifactDigest === item.artifactDigest)
    if (generation !== this.generation || confirmationGeneration !== this.confirmationGeneration || !this.expanded || eventId !== this.eventId || !this.canForget(item) || this.loading || this.downloading || current?.contentSHA256 !== contentSHA256) return Promise.resolve()
    this.downloading = item.artifactDigest
    this.removing = true
    this.locallyStartedMutation = true
    this.operationEventId = eventId
    this.downloadError = this.message = ''
    this.cdr.markForCheck()
    return new Promise(resolve => {
      this.transfer = this.operations.forget(eventId, item.artifactDigest, contentSHA256).subscribe({
        next: response => {
          if (generation !== this.generation) { resolve(); return }
          if (this.destroyed) { resolve(); return }
          this.downloading = ''; this.removing = false
          this.locallyStartedMutation = false
          this.operationEventId = ''
          const sameEvent = eventId === this.eventId
          if (response.status === 204) {
            this.message = sameEvent
              ? 'HAI copy removed. The source file and audit records remain.'
              : 'HAI confirms the copy was removed from a previous execution. Its source file and audit records remain.'
            if (sameEvent || this.expanded) this.load()
          }
          else {
            this.downloadError = sameEvent
              ? 'The removal could not be confirmed. Checking the current retained-file list.'
              : 'The removal from a previous execution could not be confirmed. Reopen that execution and refresh its file list.'
            this.reconcileAfterRefresh.set(eventId, { action: 'remove', digest: item.artifactDigest })
            if (sameEvent || this.expanded) this.load()
          }
          this.cdr.markForCheck(); resolve()
        },
        error: response => {
          if (generation === this.generation) {
            if (this.destroyed) { resolve(); return }
            this.downloading = ''; this.removing = false
            this.locallyStartedMutation = false
            this.operationEventId = ''
            const sameEvent = eventId === this.eventId
            const failureCode: ArtifactMutationFailureCode | undefined = response.error?.code === 'retained_copy_unreadable' ? 'retained_copy_unreadable' : undefined
            this.downloadError = failureCode ? unreadableRetainedCopyMessage : response.status === 412
              ? sameEvent ? 'This retained version changed. HAI is refreshing the file list; review the updated copy before deciding what to do.' : 'A retained version on a previous execution changed. Reopen it so HAI can refresh the file list before you decide what to do.'
              : response.status === 401 ? 'Your HAI session has expired. Sign in again, then refresh this execution.'
                : response.status === 403 ? 'This account is not authorized to remove the retained copy.'
                  : response.status === 404 ? 'The retained copy is no longer available to this account. Refresh the execution to confirm its state.'
                    : response.status === 429 ? 'HAI is busy with another artifact transfer. Wait briefly, then refresh before retrying.'
              : response.status === 503 ? 'HAI retention is unavailable or its encryption key is not ready. Checking the current saved-copy list.'
                        : sameEvent ? 'Removal could not be confirmed. Checking the current retained-file list.' : 'Removal on a previous execution could not be confirmed. Reopen it and refresh its file list.'
            if (response.status === 412) this.staleVersionRefreshEventId = eventId
            else this.reconcileAfterRefresh.set(eventId, { action: 'remove', digest: item.artifactDigest, failureCode })
            this.cdr.markForCheck()
            if (sameEvent || this.expanded) this.load()
          }
          resolve()
        },
      })
    })
  }
  private basePath(eventId = this.eventId): string { return `/api/v1/openclaw-artifacts/${encodeURIComponent(eventId)}` }
  private closeConfirmation(increment = true): void {
    if (increment) this.confirmationGeneration++
    const confirmation = this.confirmation
    this.confirmation = undefined
    confirmation?.destroy()
  }
  retain(item: Artifact): void {
    const stored = this.view?.items.find(candidate => candidate.artifactDigest === item.artifactDigest)
    if (!this.view?.retentionAvailable || this.downloading || this.loading || !this.eventId || !stored || !this.canDownload(stored) || this.retained(stored)) return
    const generation = ++this.generation
    const eventId = this.eventId
    this.downloading = stored.artifactDigest
    this.retaining = true
    this.locallyStartedMutation = true
    this.operationEventId = eventId
    this.downloadError = this.message = ''
    this.transfer = this.operations.retain(eventId, stored.artifactDigest).subscribe({
      next: saved => {
        if (generation !== this.generation) return
        if (this.destroyed) return
        this.downloading = ''; this.retaining = false
        this.locallyStartedMutation = false
        this.operationEventId = ''
        const sameEvent = eventId === this.eventId
        if (sameEvent && this.view && this.validRetained(saved, [stored])) {
          this.view = { ...this.view, retained: [...(this.view.retained || []), saved] }
          this.message = 'Encrypted copy retained. Deliverable correctness has not been verified.'
        } else if (!sameEvent && this.validRetained(saved, [stored])) {
          this.message = 'HAI confirms the encrypted copy was retained for a previous execution. Deliverable correctness has not been verified.'
        } else {
          this.downloadError = sameEvent
            ? 'Retention result could not be confirmed. Checking the current retained-file list.'
            : 'Retention on a previous execution could not be confirmed. Reopen it and refresh its file list.'
          this.reconcileAfterRefresh.set(eventId, { action: 'retain', digest: stored.artifactDigest })
        }
        this.cdr.markForCheck()
        if (sameEvent || this.expanded) this.load()
      },
      error: response => {
        if (generation !== this.generation) return
        if (this.destroyed) return
        this.downloading = ''; this.retaining = false
        this.locallyStartedMutation = false
        this.operationEventId = ''
        const sameEvent = eventId === this.eventId
        const context = sameEvent ? '' : ' on a previous execution'
        this.downloadError = response.error?.code === 'retained_copy_unreadable' ? `${unreadableRetainedCopyMessage}${context}`
          : response.status === 409 ? `Retention limit or version conflict${context}. Existing copies were preserved.`
          : response.status === 401 ? `Your HAI session has expired${context}. Sign in again, then refresh.`
            : response.status === 403 ? `This account is not authorized to save a copy${context}.`
              : response.status === 404 ? `The source file is no longer available to this account${context}. Refresh before retrying.`
                : response.status === 422 ? `HAI could not save this file under the runtime download policy${context}. The existing copy, if any, was not replaced.`
                  : response.status === 429 ? `HAI is busy with another artifact transfer${context}. Wait briefly, then retry.`
                    : response.status === 503 ? `Encrypted retention is unavailable or its key is not ready${context}. Checking the current saved-copy list before confirming state.`
                      : sameEvent ? 'Retention could not be confirmed. Checking the current retained-file list.' : 'Retention on a previous execution could not be confirmed. Reopen it and refresh its file list.'
        if (![401, 403, 409, 422, 429].includes(response.status)) {
          this.reconcileAfterRefresh.set(eventId, { action: 'retain', digest: stored.artifactDigest })
        }
        this.cdr.markForCheck()
        if (sameEvent || this.expanded) this.load()
      },
    })
  }
  download(item: Artifact): void {
    const stored = this.view?.items.find(candidate => candidate.artifactDigest === item.artifactDigest)
    if (this.downloading || this.loading || !this.eventId || !stored || !this.canDownload(stored)) return
    const generation = ++this.generation
    this.operationEventId = this.eventId
    this.downloading = stored.artifactDigest
    this.downloadError = this.message = ''
    this.transfer = this.http.get(`/api/v1/openclaw-artifacts/${encodeURIComponent(this.eventId)}/${stored.artifactDigest}/download`, { observe: 'response', responseType: 'blob' }).pipe(timeout(60000)).subscribe({
      next: response => { void this.acceptContent(response, stored, generation) },
      error: response => {
        if (generation !== this.generation) return
        this.downloading = ''
        this.operationEventId = ''
        this.downloadError = response.status === 422 ? 'This file cannot be downloaded under the runtime origin, format, or 8 MiB size policy. Nothing was opened in HAI.'
          : response.status === 404 ? 'This file is no longer available for this execution. Refresh the file list.'
          : response.status === 429 ? 'Downloads are busy. Wait a moment and try again.'
          : response.status === 401 ? 'Your HAI session has expired. Sign in again, then refresh this execution.'
            : response.status === 403 ? 'This account is not authorized to read this execution.'
          : 'Download unavailable. Check runtime readiness and try again.'
        this.cdr.markForCheck()
      },
    })
  }
  private async acceptContent(response: HttpResponse<Blob>, item: Artifact, generation: number): Promise<void> {
    try {
      const body = response.body
      const expected = response.headers.get('X-Content-SHA256') || ''
      if (!body || body.size > contentLimit || (item.sizeBytes !== null && body.size !== item.sizeBytes) || !/^[a-f0-9]{64}$/.test(expected) || !globalThis.crypto?.subtle) throw new Error('integrity')
      const bytes = await body.arrayBuffer()
      if (generation !== this.generation) return
      const hash = await crypto.subtle.digest('SHA-256', bytes)
      if (generation !== this.generation) return
      const actual = Array.from(new Uint8Array(hash), byte => byte.toString(16).padStart(2, '0')).join('')
      if (actual !== expected) throw new Error('integrity')
      // Never render runtime content, trust an upstream filename, or navigate to a supplied URL.
      this.saveBlob(new Blob([bytes], { type: 'application/octet-stream' }), `openclaw-${item.artifactDigest.slice(0, 12)}.bin`)
      this.message = 'Download requested; file integrity checked. Saving and deliverable correctness are not verified.'
    } catch {
      if (generation === this.generation) this.downloadError = 'File integrity could not be verified. Nothing was downloaded; refresh and try again.'
    } finally {
      if (generation === this.generation) { this.downloading = ''; this.operationEventId = ''; this.cdr.markForCheck() }
    }
  }
  saveBlob(blob: Blob, filename: string): void {
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = filename
    link.hidden = true
    document.body.appendChild(link)
    try { link.click() } finally {
      link.remove()
      this.objectURLs.set(url, setTimeout(() => { URL.revokeObjectURL(url); this.objectURLs.delete(url) }, 1000))
    }
  }
  cancelDownload(): void {
    this.generation++
    this.transfer?.unsubscribe()
    // A cancelled HTTP observation cannot roll back an already committed write.
    if (this.retaining || this.removing) this.view = undefined
    this.retaining = false
    this.removing = false
    this.downloading = ''
    this.operationEventId = ''
    this.message = ''
  }
  stateLabel(state: string): string {
    const labels: Record<string, string> = {
      pending: 'File metadata has not been collected yet.', waiting_for_completion: 'Waiting for this execution to finish.',
      collecting: 'HAI is reading available file metadata only. File contents have not been fetched or verified.',
      unavailable: 'Files are unavailable for this execution outcome.', retry_exhausted: 'Automatic collection stopped after eight attempts. Runtime review is required.',
      metadata_available: 'Stored file metadata is available; collection completeness is not confirmed.',
    }
    return labels[state] || 'File collection state unknown.'
  }
  ngOnDestroy(): void {
    this.destroyed = true
    this.closeConfirmation()
    this.read?.unsubscribe()
    this.operationState?.unsubscribe()
    if (this.retaining || this.removing) this.transfer?.unsubscribe()
    else this.cancelDownload()
    for (const [url, timer] of this.objectURLs) { clearTimeout(timer); URL.revokeObjectURL(url) }
    this.objectURLs.clear()
  }
}
