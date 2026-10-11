import { ChangeDetectionStrategy, Component, OnInit } from '@angular/core';
import { Router } from '@angular/router';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import {
  IAmbientNeed,
  IAmbientOpportunity,
  IAmbientOverview,
} from '../../models/ambient.model.interface';
import { IAutonomyOverview } from '../../models/autonomy.model.interface';
import { AmbientService } from '../../services/ambient.service';
import { AutonomyService } from '../../services/autonomy.service';

@Component({
  changeDetection: ChangeDetectionStrategy.Eager,
  standalone: false,
  selector: 'app-ambient-brain',
  templateUrl: './ambient-brain.component.html',
  styleUrls: ['./ambient-brain.component.scss'],
})
export class AmbientBrainComponent implements OnInit {
  overview?: IAmbientOverview;
  autonomyOverview?: IAutonomyOverview;
  loading = false;
  scanning = false;
  stressTesting = false;
  savingNeed = '';
  resolving = '';
  statusFilter = 'proposed';
  selectedNeed?: IAmbientNeed;
  selectedOpportunity?: IAmbientOpportunity;
  detailsMode: 'none' | 'autonomy' | 'policy' = 'none';

  constructor(
    private ambient: AmbientService,
    private autonomy: AutonomyService,
    private notification: NzNotificationService,
    private router: Router
  ) {}

  ngOnInit(): void {
    this.refresh();
    this.refreshAutonomy();
  }

  refreshAutonomy(): void {
    this.autonomy.overview().subscribe({
      next: (overview) => {
        this.autonomyOverview = overview;
      },
      error: () => {
        this.notification.warning(
          'Autonomy telemetry unavailable',
          'Workflow execution metrics could not be loaded.'
        );
      },
    });
  }

  runStressSuite(): void {
    this.stressTesting = true;
    this.autonomy.runStressSuite().subscribe({
      next: (result) => {
        this.stressTesting = false;
        this.notification.success(
          'Autonomy guards tested',
          `${result.run.passed} passed; ${result.run.failed} failed.`
        );
        this.refreshAutonomy();
      },
      error: (error) => {
        this.stressTesting = false;
        this.notification.error(
          'Stress suite failed',
          error?.error?.error || 'The deterministic guard suite did not complete.'
        );
      },
    });
  }

  percent(value: number): string {
    return `${(value * 100).toFixed(1)}%`;
  }

  visibleNeeds(): IAmbientNeed[] {
    return (this.overview?.needs || []).filter((need) => need.enabled);
  }

  disabledNeedCount(): number {
    return (this.overview?.needs || []).filter((need) => !need.enabled).length;
  }

  proposedCount(): number {
    return (this.overview?.opportunities || []).filter((item) => item.status === 'proposed').length;
  }

  acceptedCount(): number {
    return (this.overview?.opportunities || []).filter((item) => item.status === 'accepted').length;
  }

  needGap(need: IAmbientNeed): number {
    return Math.max(0, need.targetLevel - need.currentLevel);
  }

  needTone(need: IAmbientNeed): string {
    const gap = this.needGap(need);
    if (gap >= 30) return 'critical';
    if (gap >= 15) return 'attention';
    return 'stable';
  }

  topOpportunity(): IAmbientOpportunity | undefined {
    return this.filteredOpportunities()[0];
  }

  openTopOpportunity(): void {
    const opportunity = this.topOpportunity();
    if (opportunity) {
      this.openOpportunityDetails(opportunity);
    }
  }

  openNeed(need: IAmbientNeed): void {
    this.selectedNeed = need;
  }

  closeNeed(): void {
    this.selectedNeed = undefined;
  }

  openOpportunityDetails(item: IAmbientOpportunity): void {
    this.selectedOpportunity = item;
  }

  closeOpportunityDetails(): void {
    this.selectedOpportunity = undefined;
  }

  acceptSelectedOpportunity(): void {
    if (!this.selectedOpportunity) return;
    const opportunity = this.selectedOpportunity;
    this.closeOpportunityDetails();
    this.accept(opportunity);
  }

  openDetails(mode: 'autonomy' | 'policy'): void {
    this.detailsMode = mode;
  }

  closeDetails(): void {
    this.detailsMode = 'none';
  }

  refresh(): void {
    this.loading = true;
    this.ambient.overview().subscribe({
      next: (overview) => {
        this.overview = overview;
        this.loading = false;
      },
      error: (error) => {
        this.loading = false;
        this.notification.error(
          'Proactive brain unavailable',
          error?.error?.error || 'Failed to load the ambient planning engine.'
        );
      },
    });
  }

  runScan(): void {
    this.scanning = true;
    this.ambient.scan().subscribe({
      next: (scan) => {
        this.scanning = false;
        this.notification.success(
          'Ambient scan completed',
          `${scan.opportunitiesFound} opportunities found; ${scan.deduplicated} reused without duplicating source content.`
        );
        this.refresh();
      },
      error: (error) => {
        this.scanning = false;
        this.notification.error(
          'Ambient scan failed',
          error?.error?.error || 'The scan did not complete.'
        );
      },
    });
  }

  saveNeed(need: IAmbientNeed): void {
    this.savingNeed = need.key;
    this.ambient
      .updateNeed(need.key, {
        currentLevel: need.currentLevel,
        targetLevel: need.targetLevel,
        priorityWeight: need.priorityWeight,
        enabled: need.enabled,
        notes: need.notes,
      })
      .subscribe({
        next: () => {
          this.savingNeed = '';
          this.notification.success('Need profile updated', `${need.name} was saved.`);
        },
        error: () => {
          this.savingNeed = '';
          this.notification.error('Save failed', 'The need profile could not be saved.');
        },
      });
  }

  accept(item: IAmbientOpportunity): void {
    this.resolve(item, true);
  }

  dismiss(item: IAmbientOpportunity): void {
    this.resolve(item, false);
  }

  filteredOpportunities(): IAmbientOpportunity[] {
    return (this.overview?.opportunities || []).filter(
      (item) => this.statusFilter === 'all' || item.status === this.statusFilter
    );
  }

  needName(key: string): string {
    return this.overview?.needs.find((item) => item.key === key)?.name || key;
  }

  riskColor(risk: number): string {
    if (risk >= 70) {
      return 'red';
    }
    if (risk >= 45) {
      return 'orange';
    }
    return 'green';
  }

  openSource(uri?: string): void {
    const safeUri = this.safeExternalUri(uri);
    if (safeUri) {
      window.open(safeUri, '_blank', 'noopener,noreferrer');
      return;
    }
    this.notification.warning(
      'Source link not opened',
      'Only absolute HTTP and HTTPS source links can be opened from the dashboard.'
    );
  }

  openWorkflow(id?: string): void {
    this.router.navigate(['/workflow-engine'], {
      queryParams: id ? { workflowId: id } : undefined,
    });
  }

  goHome(): void {
    this.router.navigate(['/home']);
  }

  canOpenSource(uri?: string): boolean {
    return this.safeExternalUri(uri) !== '';
  }

  private safeExternalUri(uri?: string): string {
    if (!uri) {
      return '';
    }
    try {
      const parsed = new URL(uri);
      return parsed.protocol === 'http:' || parsed.protocol === 'https:'
        ? parsed.toString()
        : '';
    } catch {
      return '';
    }
  }

  private resolve(item: IAmbientOpportunity, accept: boolean): void {
    this.resolving = item.id;
    const request = accept
      ? this.ambient.accept(item.id)
      : this.ambient.dismiss(item.id);
    request.subscribe({
      next: (resolved) => {
        this.resolving = '';
        this.notification.success(
          accept ? 'Opportunity accepted' : 'Opportunity dismissed',
          accept
            ? resolved.workflowId
              ? 'The item is linked to controlled workflow work and its pursuit context.'
              : 'The item is stored as reviewable pursuit context; explicit candidate acceptance is still required before workflow work.'
            : 'The item is hidden until its cooldown expires.'
        );
        this.refresh();
      },
      error: (error) => {
        this.resolving = '';
        this.notification.error(
          'Action failed',
          error?.error?.error || 'The opportunity could not be updated.'
        );
      },
    });
  }
}
