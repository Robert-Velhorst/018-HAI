import { ChangeDetectionStrategy, Component, EventEmitter, Input, Output } from '@angular/core';
import {
  IFrameworkSelectionDecision,
  ISelectedFramework,
} from '../../models/framework-registry.model.interface';

@Component({
  changeDetection: ChangeDetectionStrategy.Eager,
  standalone: false,
  selector: 'app-framework-registry-recommendation',
  templateUrl: './framework-registry-recommendation.component.html',
  styleUrls: ['./framework-registry-recommendation.component.scss'],
})
export class FrameworkRegistryRecommendationComponent {
  @Input() selection?: IFrameworkSelectionDecision;
  @Input() advanced = false;
  @Input() loading = false;
  @Input() unavailableMessage = '';
  @Input() staleMessage = '';
  @Output() inspectFramework = new EventEmitter<string>();

  get visibleFrameworks(): ISelectedFramework[] {
    const selected = this.selection?.selected ?? [];
    return this.advanced ? selected : selected.slice(0, 3);
  }

  get primaryFrameworkName(): string {
    return this.selection?.selected[0]?.name ?? 'No suitable framework';
  }

  get hiddenFrameworkCount(): number {
    if (this.advanced) {
      return 0;
    }
    return Math.max(0, (this.selection?.selected.length ?? 0) - this.visibleFrameworks.length);
  }

  humanize(value: string): string {
    if (!value) {
      return 'Unknown';
    }
    const normalized = value.replace(/[_-]+/g, ' ');
    return normalized.charAt(0).toUpperCase() + normalized.slice(1);
  }

  trackFramework(_index: number, framework: ISelectedFramework): string {
    return `${framework.id}:${framework.version}`;
  }

  trackString(_index: number, value: string): string {
    return value;
  }

  trackConflict(
    _index: number,
    conflict: { selectedId: string; skippedId: string; reason: string }
  ): string {
    return `${conflict.selectedId}:${conflict.skippedId}:${conflict.reason}`;
  }

  trackDomain(_index: number, domain: { id: string }): string {
    return domain.id;
  }

  trackNeed(_index: number, need: { id: string }): string {
    return need.id;
  }

  trackAgent(_index: number, agent: { id: string }): string {
    return agent.id;
  }

  trackAutonomy(_index: number, action: { action: string }): string {
    return action.action;
  }
}
