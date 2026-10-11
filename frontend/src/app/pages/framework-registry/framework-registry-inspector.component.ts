import { ChangeDetectionStrategy, Component, EventEmitter, Input, Output } from '@angular/core';
import { AuthActorRole } from '../../models/auth-session.model.interface';
import {
  FrameworkPreferenceState,
  IFrameworkView,
  isProtectedFrameworkId,
} from '../../models/framework-registry.model.interface';

export interface IFrameworkPreferenceEditor {
  state: FrameworkPreferenceState;
  pinned: boolean;
  maximumAutonomyLevel: number | null;
  adaptationsText: string;
}

@Component({
  changeDetection: ChangeDetectionStrategy.Eager,
  standalone: false,
  selector: 'app-framework-registry-inspector',
  templateUrl: './framework-registry-inspector.component.html',
  styleUrls: ['./framework-registry-inspector.component.scss'],
})
export class FrameworkRegistryInspectorComponent {
  @Input() loading = false;
  @Input() errorMessage = '';
  private inspectedFramework?: IFrameworkView;

  @Input()
  set framework(value: IFrameworkView | undefined) {
    if (value?.id !== this.inspectedFramework?.id) {
      this.fullContractExpanded = false;
    }
    this.inspectedFramework = value;
  }

  get framework(): IFrameworkView | undefined {
    return this.inspectedFramework;
  }

  @Input() editor: IFrameworkPreferenceEditor = {
    state: 'default',
    pinned: false,
    maximumAutonomyLevel: null,
    adaptationsText: '',
  };
  @Input() saving = false;
  @Input() actorRole: AuthActorRole = 'unknown';
  @Input() canManagePreferences = false;
  @Output() save = new EventEmitter<void>();
  @Output() retry = new EventEmitter<void>();

  fullContractExpanded = false;

  readonly preferenceStates: Array<{ value: FrameworkPreferenceState; label: string }> = [
    { value: 'default', label: 'Use catalog default' },
    { value: 'enabled', label: 'Enable for this owner' },
    { value: 'disabled', label: 'Disable for this owner' },
  ];

  get preferenceAccessExplanation(): string {
    switch (this.actorRole) {
      case 'owner':
        return 'This owner session does not include administration permission, so framework preferences remain read-only.';
      case 'operator':
        return 'Only the owner can change framework preferences. Your operator session can inspect frameworks and request recommendations.';
      case 'viewer':
        return 'Viewer sessions are read-only. Only the owner can change framework preferences.';
      default:
        return 'Owner-only changes are disabled because HAI could not verify owner authority for this session.';
    }
  }

  get isProtectedSafetyOverlay(): boolean {
    return Boolean(this.framework && isProtectedFrameworkId(this.framework.id));
  }

  get effectivePreferenceState(): FrameworkPreferenceState {
    const item = this.framework;
    if (!item?.preferenceUpdatedAt) {
      return 'default';
    }
    if (!item.enabled) {
      return 'disabled';
    }
    return item.status === 'active' ? 'default' : 'enabled';
  }

  get lifecycleGuidance(): string {
    switch (this.framework?.status) {
      case 'experimental':
        return 'Experimental frameworks are disabled by default. Owner opt-in can make this framework eligible, but does not grant execution authority.';
      case 'deprecated':
        return 'Deprecated frameworks are retained for audit and inspection but are never eligible for new selections.';
      default:
        return '';
    }
  }

  humanize(value: string): string {
    if (!value) {
      return 'Unknown';
    }
    const normalized = value.replace(/[_-]+/g, ' ');
    return normalized.charAt(0).toUpperCase() + normalized.slice(1);
  }

  statusColor(status: string): string {
    switch (status) {
      case 'active':
        return 'green';
      case 'experimental':
        return 'gold';
      case 'deprecated':
        return 'red';
      default:
        return 'blue';
    }
  }

  riskColor(risk: string): string {
    switch (risk) {
      case 'high':
        return 'red';
      case 'medium':
        return 'gold';
      default:
        return 'green';
    }
  }

  toggleFullContract(): void {
    this.fullContractExpanded = !this.fullContractExpanded;
  }
}
