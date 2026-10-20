import { ChangeDetectionStrategy, ChangeDetectorRef, Component, OnDestroy, OnInit } from '@angular/core'
import { FormBuilder, FormGroup } from '@angular/forms'
import { NzNotificationService } from 'ng-zorro-antd/notification'
import { Subject } from 'rxjs'
import { catchError, finalize, takeUntil, timeout } from 'rxjs/operators'
import { IHireAICursor, IHireAIFilters, IHireAIJob, IHireAISearchResult, IHireAIStatus } from '../../models/hire-ai.model.interface'
import { HttpTimeoutPolicy } from '../../shared/http-timeout-policy'
import { HireAIService } from '../../services/hire-ai.service'

@Component({
  changeDetection: ChangeDetectionStrategy.OnPush,
  selector: 'app-hire-ai',
  templateUrl: './hire-ai.component.html',
  styleUrls: ['./hire-ai.component.scss'],
  standalone: false,
})
export class HireAIComponent implements OnInit, OnDestroy {
  readonly moduleId = 'hire-ai'
  readonly filters: FormGroup
  status?: IHireAIStatus
  result?: IHireAISearchResult
  statusLoading = false
  searchLoading = false
  statusError = ''
  searchError = ''
  private cursor?: IHireAICursor
  private activeFilters?: IHireAIFilters
  private readonly seenCursors = new Set<string>()
  private readonly destroyed$ = new Subject<void>()

  constructor(private formBuilder: FormBuilder, private service: HireAIService, private notification: NzNotificationService, private cdr: ChangeDetectorRef) {
    this.filters = this.formBuilder.group({
      query: [''], location: [''], jobType: ['all'], experienceLevel: ['all'],
      applicationProcess: ['all'], postedWithin: ['all'], salaryCurrency: [''],
      salaryMin: [''], salaryMax: [''], platformId: [''],
      visaSponsorshipOnly: [false], openHiringSupportOnly: [false],
      diversityFriendlyOnly: [false], salaryDisclosedOnly: [false],
    })
  }

  ngOnInit(): void { this.refreshStatus() }

  ngOnDestroy(): void {
    this.destroyed$.next()
    this.destroyed$.complete()
  }

  refreshStatus(): void {
    if (this.statusLoading) return
    this.statusLoading = true
    this.statusError = ''
    this.service.status().pipe(
      timeout(8000),
      takeUntil(this.destroyed$),
      catchError(() => {
        this.status = undefined
        this.statusError = 'HAI could not read Hire.AI connector status. No search was started.'
        return []
      }),
      finalize(() => { this.statusLoading = false; this.cdr.markForCheck() }),
    ).subscribe((status) => { if (status) this.status = status; this.cdr.markForCheck() })
  }

  search(): void { this.runSearch(undefined, true) }

  loadMore(): void { if (this.cursor && !this.searchLoading) this.runSearch(this.cursor, false) }

  canOpen(url?: string | null): boolean {
    if (!url) return false
    try { return new URL(url).protocol === 'https:' } catch { return false }
  }

  trackJob(_index: number, job: IHireAIJob): number { return job.id }

  salary(job: IHireAIJob): string {
    if (job.salaryMin == null && job.salaryMax == null) return 'Salary not listed'
    const amount = new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 })
    const low = job.salaryMin == null ? '' : amount.format(job.salaryMin)
    const high = job.salaryMax == null ? '' : amount.format(job.salaryMax)
    return `${job.salaryCurrency || ''} ${low}${low && high ? '–' : ''}${high}`.trim()
  }

  private runSearch(cursor: IHireAICursor | undefined, reset: boolean): void {
    if (!this.status?.configured || this.searchLoading) return
    const filters = reset ? this.readFilters() : this.activeFilters
    if (!filters) {
      this.cdr.markForCheck()
      return
    }
    if (reset) {
      this.result = undefined
      this.cursor = undefined
      this.activeFilters = filters
      this.seenCursors.clear()
    }
    this.searchLoading = true
    this.service.search(filters, cursor).pipe(
      timeout(HttpTimeoutPolicy.hireAISearchMs),
      takeUntil(this.destroyed$),
      catchError((error: unknown) => {
        const filtersRejected = typeof error === 'object' && error !== null && 'status' in error && error.status === 400
        this.searchError = filtersRejected
          ? 'Hire.AI rejected one or more search filters. Check the currency and filter values, then retry.'
          : 'Hire.AI search failed or returned an unavailable response. Your filters are unchanged; retry when the connector is available.'
        return []
      }),
      finalize(() => { this.searchLoading = false; this.cdr.markForCheck() }),
    ).subscribe((result) => {
      if (!result || !result.policy?.remoteOnly || result.policy.listingSafety !== 'clear' || !result.policy.canonicalListingsOnly) {
        this.searchError = 'HAI rejected a result that did not satisfy the remote-only, canonical, safety-cleared policy.'
        this.cdr.markForCheck()
        return
      }
      const nextCursor = result.nextCursor || undefined
      if (nextCursor) {
        const cursorKey = this.cursorKey(nextCursor)
        if (this.seenCursors.has(cursorKey)) {
          this.searchError = 'Hire.AI repeated a pagination cursor. Search stopped to avoid repeating listings.'
          this.cursor = undefined
          this.cdr.markForCheck()
          return
        }
        this.seenCursors.add(cursorKey)
      }
      const jobs = reset || !this.result ? result.jobs : [...this.result.jobs, ...result.jobs]
      const uniqueJobs = new Map<number, IHireAIJob>()
      for (const job of jobs) uniqueJobs.set(job.id, job)
      this.result = { ...result, jobs: [...uniqueJobs.values()] }
      this.cursor = nextCursor
      this.notification.success('Hire.AI search complete', `${result.jobs.length} listing${result.jobs.length === 1 ? '' : 's'} received.`)
      this.cdr.markForCheck()
    })
  }

  private readFilters(): IHireAIFilters | undefined {
    this.searchError = ''
    const value = this.filters.getRawValue()
    const minimumInput = String(value.salaryMin ?? '').trim()
    const maximumInput = String(value.salaryMax ?? '').trim()
    const salaryCurrency = String(value.salaryCurrency ?? '').trim()
    const hasMinimum = minimumInput.length > 0
    const hasMaximum = maximumInput.length > 0
    if (hasMinimum !== hasMaximum) {
      this.searchError = 'Enter both salary bounds, or clear both.'
      return undefined
    }
    const minimum = hasMinimum ? Number(minimumInput) : undefined
    const maximum = hasMaximum ? Number(maximumInput) : undefined
    if (minimum !== undefined && (!Number.isFinite(minimum) || minimum < 0 || minimum > 10_000_000) || maximum !== undefined && (!Number.isFinite(maximum) || maximum < 0 || maximum > 10_000_000) || minimum !== undefined && maximum !== undefined && minimum > maximum) {
      this.searchError = 'Enter a valid non-negative salary range with the minimum no greater than the maximum.'
      return undefined
    }
    if (salaryCurrency && !this.isKnownSalaryCurrency(salaryCurrency)) {
      this.searchError = 'Choose a recognized ISO 4217 currency when filtering by salary.'
      return undefined
    }
    if (minimum !== undefined && !salaryCurrency) {
      this.searchError = 'Choose a recognized ISO 4217 currency when filtering by salary.'
      return undefined
    }
    const platformId = value.platformId?.trim() ? Number(value.platformId) : undefined
    if (platformId !== undefined && (!Number.isSafeInteger(platformId) || platformId <= 0)) {
      this.searchError = 'Source platform ID must be a positive whole number.'
      return undefined
    }
    const filters = {
      query: value.query?.trim() || undefined,
      location: value.location?.trim() || undefined,
      jobType: value.jobType === 'all' ? undefined : value.jobType || undefined,
      platformId,
      salaryRange: minimum !== undefined && maximum !== undefined ? [minimum, maximum] as [number, number] : undefined,
      experienceLevel: value.experienceLevel === 'all' ? undefined : value.experienceLevel || undefined,
      applicationProcess: value.applicationProcess === 'all' ? undefined : value.applicationProcess || undefined,
      postedWithin: value.postedWithin === 'all' ? undefined : value.postedWithin || undefined,
      salaryCurrency: salaryCurrency.toUpperCase() || undefined,
      visaSponsorshipOnly: value.visaSponsorshipOnly || undefined,
      openHiringSupportOnly: value.openHiringSupportOnly || undefined,
      diversityFriendlyOnly: value.diversityFriendlyOnly || undefined,
      salaryDisclosedOnly: value.salaryDisclosedOnly || undefined,
    }
    return filters
  }

  private cursorKey(cursor: IHireAICursor): string {
    return JSON.stringify([cursor.postedDate ?? null, cursor.createdAt, cursor.id])
  }

  private isKnownSalaryCurrency(value: unknown): boolean {
    if (typeof value !== 'string') return false
    const currency = value.trim().toUpperCase()
    if (!/^[A-Z]{3}$/.test(currency) || currency === 'XXX') return false
    try {
      const displayName = new Intl.DisplayNames(['en'], { type: 'currency' }).of(currency)
      return Boolean(displayName && displayName.toUpperCase() !== currency)
    } catch {
      return false
    }
  }
}
