import { ChangeDetectionStrategy, ChangeDetectorRef, Component, Input, OnChanges, OnDestroy } from '@angular/core'
import { CommonModule } from '@angular/common'
import { HttpClient } from '@angular/common/http'
import { Subscription, timeout } from 'rxjs'
import { NzButtonModule } from 'ng-zorro-antd/button'
import { NzIconModule, NzIconService } from 'ng-zorro-antd/icon'
import { DownOutline, RightOutline, ReloadOutline } from '@ant-design/icons-angular/icons'

interface UsageTotals {
  input: number; output: number; cacheRead: number; cacheWrite: number; totalTokens: number
  estimatedCostUsd: number | null; missingCostEntries: number | null
}
interface UsageView {
  state: 'pending' | 'captured' | 'unavailable' | 'retry_exhausted'
  attempts: number; nextAttemptAt?: string; capturedAt?: string
  snapshot?: {
    source: string; scope: string; startDate: string; endDate: string; observedAt: string
    totals: UsageTotals; modelsReported: boolean
    models?: { provider: string; model: string; count: number; totals: UsageTotals }[]
  }
}

@Component({
  selector: 'app-openclaw-usage', standalone: true,
  imports: [CommonModule, NzButtonModule, NzIconModule],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <button nz-button nzType="link" type="button" (click)="toggle()" [attr.aria-expanded]="expanded" title="Inspect stored session tokens and estimated cost">
      <span nz-icon [nzType]="expanded ? 'down' : 'right'" aria-hidden="true"></span>OpenClaw usage
    </button>
    <section *ngIf="expanded" aria-label="OpenClaw session usage" [attr.aria-busy]="loading">
      <header><strong>Session usage</strong><button nz-button nzType="text" type="button" (click)="load()" [disabled]="loading" aria-label="Refresh usage" title="Reload stored usage"><span nz-icon nzType="reload" aria-hidden="true"></span></button></header>
      <p role="status" *ngIf="loading">Loading usage...</p>
      <p role="alert" *ngIf="error">{{ error }}</p>
      <ng-container *ngIf="view as data">
        <p role="status" *ngIf="data.state !== 'captured'">{{ stateLabel(data.state) }}</p>
        <small *ngIf="data.state === 'pending' && data.nextAttemptAt">Next collection eligible {{ data.nextAttemptAt | date:'short' }}; subject to runtime availability.</small>
        <ng-container *ngIf="data.snapshot as s">
          <p class="scope">{{ s.startDate }} to {{ s.endDate }} UTC &middot; Observed {{ s.observedAt | date:'short':'UTC' }} UTC</p>
          <dl class="totals">
            <div><dt>Input tokens</dt><dd>{{ s.totals.input | number }}</dd></div>
            <div><dt>Output tokens</dt><dd>{{ s.totals.output | number }}</dd></div>
            <div><dt>Cache read</dt><dd>{{ s.totals.cacheRead | number }}</dd></div>
            <div><dt>Cache write</dt><dd>{{ s.totals.cacheWrite | number }}</dd></div>
            <div><dt>Total tokens</dt><dd>{{ s.totals.totalTokens | number }}</dd></div>
            <div><dt>Estimated USD</dt><dd>{{ s.totals.estimatedCostUsd === null ? 'Unavailable' : (s.totals.estimatedCostUsd | number:'1.0-8') }}</dd></div>
          </dl>
          <p class="scope">Session snapshot, not a per-run bill or budget debit.</p>
          <details>
            <summary>Reported models and source</summary>
            <p class="scope">Source: {{ s.source }} &middot; {{ s.scope }}</p>
            <p *ngIf="!s.modelsReported">Not reported</p>
            <p *ngIf="s.modelsReported && !s.models?.length">No model groups reported</p>
            <article *ngFor="let model of s.models">
              <strong>{{ model.provider || 'Unknown provider' }} / {{ model.model || 'Unknown model' }}</strong>
              <dl class="totals">
                <div><dt>Observations</dt><dd>{{ model.count | number }}</dd></div>
                <div><dt>Input / output</dt><dd>{{ model.totals.input | number }} / {{ model.totals.output | number }}</dd></div>
                <div><dt>Cache read / write</dt><dd>{{ model.totals.cacheRead | number }} / {{ model.totals.cacheWrite | number }}</dd></div>
                <div><dt>Total tokens</dt><dd>{{ model.totals.totalTokens | number }}</dd></div>
                <div><dt>Estimated USD</dt><dd>{{ model.totals.estimatedCostUsd === null ? 'Unavailable' : (model.totals.estimatedCostUsd | number:'1.0-8') }}</dd></div>
              </dl>
            </article>
            <p class="scope">Model groups do not establish fallback order. Cost estimates are not provider invoices.</p>
          </details>
        </ng-container>
      </ng-container>
    </section>
  `,
  styles: [`
    :host { display:block; min-width:0; color:var(--hai-text); }
    section { border-block:1px solid var(--hai-border); padding:12px 0; margin-top:8px; }
    header { display:flex; align-items:center; justify-content:space-between; gap:12px; }
    header button { color:var(--hai-text); }
    p { margin:8px 0; } .scope, dt, small { color:var(--hai-text-soft); font-size:12px; }
    .totals { display:grid; grid-template-columns:repeat(auto-fit,minmax(min(130px,100%),1fr)); gap:12px; margin:12px 0; }
    dt, dd, strong, p { overflow-wrap:anywhere; } dd { margin:4px 0 0; font-variant-numeric:tabular-nums; }
    summary { cursor:pointer; padding:8px 0; } article { border-top:1px solid var(--hai-border); padding:12px 0; }
    button:focus-visible, summary:focus-visible { outline:2px solid currentColor; outline-offset:3px; }
  `],
})
export class OpenclawUsageComponent implements OnChanges, OnDestroy {
  @Input() eventId = ''
  expanded = false
  loading = false
  error = ''
  view?: UsageView
  private request?: Subscription
  constructor(private http: HttpClient, private cdr: ChangeDetectorRef, icons: NzIconService) {
    icons.addIcon(DownOutline, RightOutline, ReloadOutline)
  }
  ngOnChanges(): void {
    this.request?.unsubscribe()
    this.view = undefined
    this.expanded = this.loading = false
    this.error = ''
  }
  toggle(): void {
    this.expanded = !this.expanded
    if (this.expanded && !this.view) this.load()
  }
  load(): void {
    if (this.loading || !this.eventId) return
    this.loading = true
    this.error = ''
    this.request = this.http.get<UsageView>(`/api/v1/openclaw-usage/${encodeURIComponent(this.eventId)}`).pipe(timeout(10000)).subscribe({
      next: view => { this.view = view; this.loading = false; this.cdr.markForCheck() },
      error: response => {
        this.view = undefined
        this.loading = false
        this.error = response.status === 404 ? 'No usage record available for this execution.' : 'Usage is temporarily unavailable. Refresh to try again.'
        this.cdr.markForCheck()
      },
    })
  }
  stateLabel(state: UsageView['state']): string {
    return ({ pending: 'Usage has not been collected yet.', captured: 'Captured', unavailable: 'Source usage is unavailable for this execution.', retry_exhausted: 'Automatic collection stopped after eight attempts. Runtime review is required.' })[state] || 'Usage state unknown.'
  }
  ngOnDestroy(): void { this.request?.unsubscribe() }
}
