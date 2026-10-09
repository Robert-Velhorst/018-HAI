import { ChangeDetectionStrategy, ChangeDetectorRef, Component, OnDestroy, OnInit } from '@angular/core';
import { Router } from '@angular/router';
import { Subscription } from 'rxjs';
import { IWorkflowItem } from '../../models/workflow.model.interface';
import { ModuleViewPreferencesService } from '../../control-room/module-view-preferences.service';
import { WorkflowService } from '../../services/workflow/workflow.service';

const ATTENTION_STATES = new Set([
  'awaiting_approval',
  'needs_approval',
  'failed',
  'dead_letter',
  'blocked',
  'interrupted',
]);

const CLOSED_STATES = new Set(['completed', 'done', 'archived']);
const APPROVED_STATES = new Set(['approved', 'not_required']);

function normalized(value?: string): string {
  return (value ?? '').trim().toLowerCase();
}

function hasText(value?: string): value is string {
  return typeof value === 'string' && value.trim().length > 0;
}

@Component({
  selector: 'app-exceptions',
  templateUrl: './exceptions.component.html',
  styleUrls: ['./exceptions.component.scss'],
  standalone: false,
  changeDetection: ChangeDetectionStrategy.Default,
})
export class ExceptionsComponent implements OnInit, OnDestroy {
  readonly moduleId = 'exceptions';
  items: IWorkflowItem[] = [];
  loading = false;
  loadError = '';
  lastRefreshedAt = '';
  searchQuery = '';
  stateFilter = 'all';
  riskFilter = 'all';

  private loadSubscription?: Subscription;

  constructor(
    private workflowService: WorkflowService,
    private router: Router,
    private preferences: ModuleViewPreferencesService,
    private changeDetectorRef: ChangeDetectorRef,
  ) {}

  ngOnInit(): void {
    this.refresh();
  }

  ngOnDestroy(): void {
    this.loadSubscription?.unsubscribe();
  }

  get isAdvancedView(): boolean {
    return this.preferences.get(this.moduleId).mode === 'advanced';
  }

  refresh(): void {
    this.loadSubscription?.unsubscribe();
    this.loading = true;
    this.loadError = '';
    this.loadSubscription = this.workflowService.items(false).subscribe({
      next: (items) => {
        if (!Array.isArray(items)) {
          this.loading = false;
          this.loadError = 'The workflow queue returned an unreadable response. The last successful results, if any, are still shown.';
          this.loadSubscription = undefined;
          this.changeDetectorRef.markForCheck();
          return;
        }
        this.items = items;
        this.lastRefreshedAt = new Date().toISOString();
        this.loading = false;
        this.loadSubscription = undefined;
        this.changeDetectorRef.markForCheck();
      },
      error: () => {
        this.loading = false;
        this.loadError = 'The exception queue could not be refreshed. The last successful results, if any, are still shown.';
        this.loadSubscription = undefined;
        this.changeDetectorRef.markForCheck();
      },
    });
  }

  get exceptions(): IWorkflowItem[] {
    return this.items
      .filter((item) => this.isOpenException(item))
      .sort((a, b) => {
        const priorityDifference = this.priorityRank(a) - this.priorityRank(b);
        if (priorityDifference !== 0) return priorityDifference;

        const freshnessDifference = this.timestampValue(b.updatedAt) - this.timestampValue(a.updatedAt);
        return freshnessDifference || (a.title ?? '').localeCompare(b.title ?? '');
      });
  }

  get displayedExceptions(): IWorkflowItem[] {
    const exceptions = this.exceptions;
    if (!this.isAdvancedView) return exceptions;

    const query = normalized(this.searchQuery);
    return exceptions.filter((item) => {
      const state = normalized(item.currentState);
      const risk = normalized(item.riskLevel) || 'not_recorded';
      const searchText = [
        item.title,
        item.description,
        item.projectKey,
        item.nextAction,
        item.blockedReason,
        item.recoveryNote,
        item.approvalReason,
        item.lastWorkerError,
        item.sourceLabel,
        item.taskType,
      ].filter(hasText).join(' ').toLowerCase();

      return (this.stateFilter === 'all' || state === this.stateFilter)
        && (this.riskFilter === 'all' || risk === this.riskFilter)
        && (!query || searchText.includes(query));
    });
  }

  get stateOptions(): string[] {
    return Array.from(new Set(this.exceptions.map((item) => normalized(item.currentState))))
      .sort((a, b) => a.localeCompare(b));
  }

  get riskOptions(): string[] {
    return Array.from(new Set(this.exceptions.map((item) => normalized(item.riskLevel) || 'not_recorded')))
      .sort((a, b) => a.localeCompare(b));
  }

  get approvalCount(): number {
    return this.exceptions.filter((item) => this.needsHumanApproval(item)).length;
  }

  get highRiskCount(): number {
    return this.exceptions.filter((item) => this.isHighRisk(item)).length;
  }

  get recoveryCount(): number {
    return this.exceptions.filter((item) => this.needsRecovery(item)).length;
  }

  get hasActiveFilters(): boolean {
    return !!this.searchQuery.trim() || this.stateFilter !== 'all' || this.riskFilter !== 'all';
  }

  isOpenException(item: IWorkflowItem): boolean {
    const state = normalized(item.currentState);
    if (CLOSED_STATES.has(state)) return false;

    return ATTENTION_STATES.has(state)
      || normalized(item.recoveryStatus) === 'needs_review'
      || this.needsHumanApproval(item)
      || this.isHighRisk(item);
  }

  static needsAttention(state: string): boolean {
    return ATTENTION_STATES.has(normalized(state));
  }

  needsHumanApproval(item: IWorkflowItem): boolean {
    const state = normalized(item.currentState);
    const approvalStatus = normalized(item.approvalStatus);
    return state === 'needs_approval'
      || state === 'awaiting_approval'
      || approvalStatus === 'pending'
      || (item.requiresApproval === true && !APPROVED_STATES.has(approvalStatus));
  }

  isHighRisk(item: IWorkflowItem): boolean {
    return normalized(item.riskLevel) === 'high';
  }

  needsRecovery(item: IWorkflowItem): boolean {
    const state = normalized(item.currentState);
    return ['failed', 'dead_letter', 'blocked', 'interrupted'].includes(state)
      || normalized(item.recoveryStatus) === 'needs_review';
  }

  recoveryReason(item: IWorkflowItem): string {
    const reason = [item.blockedReason, item.recoveryNote, item.approvalReason, item.lastWorkerError]
      .find(hasText);
    return reason?.trim() || 'No recovery reason is recorded.';
  }

  stateLabel(state: string): string {
    const normalizedState = normalized(state);
    const knownLabels: Record<string, string> = {
      awaiting_approval: 'Needs approval',
      needs_approval: 'Needs approval',
      dead_letter: 'Retry limit reached',
      waiting_external_input: 'Waiting for an external reply',
      in_progress: 'In progress',
      new_input: 'New input',
    };
    if (knownLabels[normalizedState]) return knownLabels[normalizedState];
    if (!normalizedState) return 'State not recorded';
    return normalizedState.replace(/[_-]+/g, ' ').replace(/\b\w/g, (letter) => letter.toUpperCase());
  }

  riskLabel(riskLevel?: string): string {
    const risk = normalized(riskLevel);
    if (!risk) return 'Not recorded';
    return risk === 'high' ? 'High' : risk.replace(/[_-]+/g, ' ').replace(/\b\w/g, (letter) => letter.toUpperCase());
  }

  tagColor(state: string): string {
    switch (normalized(state)) {
      case 'failed':
      case 'dead_letter':
        return 'red';
      case 'blocked':
      case 'interrupted':
        return 'orange';
      case 'awaiting_approval':
      case 'needs_approval':
        return 'gold';
      default:
        return 'default';
    }
  }

  formatTimestamp(value?: string): string {
    if (!hasText(value)) return 'Not recorded';
    const timestamp = new Date(value);
    return Number.isNaN(timestamp.getTime()) ? 'Time not recognized' : timestamp.toLocaleString();
  }

  timestampAttribute(value?: string): string | null {
    if (!hasText(value)) return null;
    const timestamp = new Date(value);
    return Number.isNaN(timestamp.getTime()) ? null : timestamp.toISOString();
  }

  clearFilters(): void {
    this.searchQuery = '';
    this.stateFilter = 'all';
    this.riskFilter = 'all';
  }

  trackById(_index: number, item: IWorkflowItem): string {
    return item.id;
  }

  openWorkflow(item: IWorkflowItem): void {
    this.router.navigate(['/workflow-engine'], {
      queryParams: { workflowId: item.id },
    });
  }

  goBack(): void {
    this.router.navigate(['/control-center']);
  }

  private priorityRank(item: IWorkflowItem): number {
    if (this.needsHumanApproval(item)) return 0;
    if (this.isHighRisk(item)) return 1;
    return 2;
  }

  private timestampValue(value?: string): number {
    if (!hasText(value)) return 0;
    const timestamp = new Date(value).getTime();
    return Number.isNaN(timestamp) ? 0 : timestamp;
  }
}
