import { ChangeDetectionStrategy, Component, OnInit } from '@angular/core';
import { Router } from '@angular/router';
import { IWorkflowItem } from '../../models/workflow.model.interface';
import { WorkflowService } from '../../services/workflow/workflow.service';

const ATTENTION_STATES = [
  'awaiting_approval',
  'failed',
  'dead_letter',
  'blocked',
  'interrupted',
];

@Component({
  changeDetection: ChangeDetectionStrategy.Eager,
  standalone: false,
  selector: 'app-exceptions',
  templateUrl: './exceptions.component.html',
  styleUrls: ['./exceptions.component.scss'],
})
export class ExceptionsComponent implements OnInit {
  items: IWorkflowItem[] = [];
  loading = false;
  loadError = '';

  constructor(
    private workflowService: WorkflowService,
    private router: Router
  ) {}

  ngOnInit(): void {
    this.refresh();
  }

  refresh(): void {
    this.loading = true;
    this.loadError = '';
    this.workflowService.items(false).subscribe({
      next: (items) => {
        this.items = items || [];
        this.loading = false;
      },
      error: () => {
        this.loading = false;
        this.loadError = 'Could not load workflow exceptions. Refresh to try again.';
      },
    });
  }

  get exceptions(): IWorkflowItem[] {
    return this.items
      .filter((item) => ExceptionsComponent.needsAttention(item.currentState))
      .sort((a, b) => (b.updatedAt ?? '').localeCompare(a.updatedAt ?? ''));
  }

  static needsAttention(state: string): boolean {
    return ATTENTION_STATES.includes((state ?? '').toLowerCase());
  }

  openWorkflow(item: IWorkflowItem): void {
    this.router.navigate(['/workflow-engine'], {
      queryParams: { workflowId: item.id },
    });
  }

  goBack(): void {
    this.router.navigate(['/control-center']);
  }

  tagColor(state: string): string {
    switch ((state ?? '').toLowerCase()) {
      case 'failed':
      case 'dead_letter':
        return 'red';
      case 'blocked':
      case 'interrupted':
        return 'orange';
      case 'awaiting_approval':
        return 'gold';
      default:
        return 'default';
    }
  }
}
