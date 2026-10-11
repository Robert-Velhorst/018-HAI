import { HttpErrorResponse } from '@angular/common/http';
import { ChangeDetectionStrategy, Component, OnDestroy, OnInit } from '@angular/core';
import { Router } from '@angular/router';
import {
  Subject,
  Subscription,
  catchError,
  finalize,
  forkJoin,
  of,
  takeUntil,
} from 'rxjs';
import { NzNotificationService } from 'ng-zorro-antd/notification';
import {
  AuthActorRole,
  IAuthSession,
} from '../../models/auth-session.model.interface';
import {
  FrameworkPreferenceState,
  FrameworkViewMode,
  IConstitution,
  IConstitutionDraftRequest,
  IConstitutionHistoryEntry,
  IFrameworkModuleViewPreferences,
  IFrameworkPreferencePatch,
  IFrameworkRegistryOverview,
  IFrameworkSelectionDecision,
  IFrameworkSelectionRequest,
  IFrameworkView,
  isProtectedFrameworkId,
} from '../../models/framework-registry.model.interface';
import { AuthSessionService } from '../../services/auth-session.service';
import { FrameworkRegistryService } from '../../services/framework-registry.service';

const VIEW_STORAGE_KEY = 'hai.module-view.v1.framework-registry';

const DEFAULT_OPEN_SECTIONS: Record<string, boolean> = {
  'selection-context': true,
  'selection-history': false,
  'constitution-history': false,
  'constitution-governance': false,
};

interface ISelectionDraft {
  request: string;
  projectKey: string;
  pursuitId: string;
  taskType: string;
  difficulty: number | null;
  requiredReasoning: string;
  successCriteriaText: string;
  needsMemory: boolean;
  needsTools: boolean;
  needsDocuments: boolean;
  needsWebAccess: boolean;
  needsLocalExecution: boolean;
  executeRequested: boolean;
}

interface IFrameworkPreferenceEditor {
  state: FrameworkPreferenceState;
  pinned: boolean;
  maximumAutonomyLevel: number | null;
  adaptationsText: string;
}

interface IConstitutionEditor {
  values: string;
  prohibitions: string;
  standingPermissions: string;
  preferences: string;
  relationshipRules: string;
  financialBoundaries: string;
  communicationRules: string;
  escalationRules: string;
  changeSummary: string;
}

type ConstitutionRuleField =
  | 'values'
  | 'prohibitions'
  | 'standingPermissions'
  | 'preferences'
  | 'relationshipRules'
  | 'financialBoundaries'
  | 'communicationRules'
  | 'escalationRules';

interface IConstitutionRuleSection {
  field: ConstitutionRuleField;
  label: string;
}

@Component({
  changeDetection: ChangeDetectionStrategy.Eager,
  standalone: false,
  selector: 'app-framework-registry',
  templateUrl: './framework-registry.component.html',
  styleUrls: ['./framework-registry.component.scss'],
})
export class FrameworkRegistryComponent implements OnInit, OnDestroy {
  overview?: IFrameworkRegistryOverview;
  authSession?: IAuthSession;
  frameworks: IFrameworkView[] = [];
  selections: IFrameworkSelectionDecision[] = [];
  constitution?: IConstitution;
  constitutionHistory: IConstitutionHistoryEntry[] = [];
  constitutionHistoryTruncated = false;
  constitutionSource = '';
  constitutionDraft?: IConstitution;

  viewMode: FrameworkViewMode = 'basic';
  openSections: Record<string, boolean> = { ...DEFAULT_OPEN_SECTIONS };

  searchText = '';
  familyFilter = 'all';
  statusFilter = 'all';

  loading = false;
  selectionBusy = false;
  selectionError = '';
  currentSelection?: IFrameworkSelectionDecision;

  inspectorVisible = false;
  inspectorLoading = false;
  inspectorError = '';
  selectedFramework?: IFrameworkView;
  preferenceSaving = false;
  private inspectorReturnFocus?: HTMLElement;
  private inspectedFrameworkId = '';
  private inspectorSubscription?: Subscription;
  private refreshSubscription?: Subscription;
  private overviewSubscription?: Subscription;
  private readonly destroy$ = new Subject<void>();
  private readonly preferenceStateHints = new Map<string, FrameworkPreferenceState>();

  constitutionEditing = false;
  constitutionSaving = false;
  constitutionActivating = false;
  activationConfirmation = '';
  activationNote = '';
  constitutionError = '';

  loadErrors: Record<string, string> = {};

  selectionDraft: ISelectionDraft = this.emptySelectionDraft();
  preferenceEditor: IFrameworkPreferenceEditor = this.emptyPreferenceEditor();
  constitutionEditor: IConstitutionEditor = this.emptyConstitutionEditor();

  readonly reasoningLevels = ['minimal', 'low', 'medium', 'high', 'xhigh'];
  readonly constitutionRuleSections: IConstitutionRuleSection[] = [
    { field: 'values', label: 'Values' },
    { field: 'prohibitions', label: 'Prohibitions' },
    { field: 'standingPermissions', label: 'Standing permissions' },
    { field: 'preferences', label: 'Preferences' },
    { field: 'relationshipRules', label: 'Relationship rules' },
    { field: 'financialBoundaries', label: 'Financial boundaries' },
    { field: 'communicationRules', label: 'Communication rules' },
    { field: 'escalationRules', label: 'Escalation rules' },
  ];

  private readonly typedRuleCapabilities = new Set([
    'memory-read',
    'document-read',
    'web-access',
    'tool-execution',
    'local-execution',
    'execution',
    'external-communication',
    'legal-government-action',
    'financial-action',
    'account-change',
    'destructive-action',
    'public-posting',
    'consequential-action',
  ]);

  constructor(
    private service: FrameworkRegistryService,
    private authSessionService: AuthSessionService,
    private notification: NzNotificationService,
    private router: Router
  ) {}

  ngOnInit(): void {
    this.restoreViewPreference();
    this.refresh();
  }

  ngOnDestroy(): void {
    this.refreshSubscription?.unsubscribe();
    this.inspectorSubscription?.unsubscribe();
    this.overviewSubscription?.unsubscribe();
    this.destroy$.next();
    this.destroy$.complete();
  }

  get isAdvanced(): boolean {
    return this.viewMode === 'advanced';
  }

  get actorRole(): AuthActorRole {
    return this.authSession?.role ?? 'unknown';
  }

  get canManageOwnerControls(): boolean {
    return Boolean(
      this.authSession?.authenticated &&
      this.authSession.role === 'owner' &&
      this.authSession.permissions.canAdminister
    );
  }

  get canRequestSelection(): boolean {
    return Boolean(
      this.authSession?.authenticated &&
      (this.authSession.role === 'owner' || this.authSession.role === 'operator') &&
      this.authSession.permissions.canOperate
    );
  }

  get selectionAccessExplanation(): string {
    switch (this.actorRole) {
      case 'viewer':
        return 'Viewer sessions can inspect the registry but cannot create recorded framework recommendations.';
      case 'operator':
      case 'owner':
        return 'Framework selection is unavailable because this session does not include operating permission.';
      default:
        return 'Framework selection is disabled because HAI could not verify an authorized local session.';
    }
  }

  get ownerControlExplanation(): string {
    if (!this.authSession?.authenticated) {
      return 'Owner-only changes are disabled because HAI could not verify an authenticated local session.';
    }
    switch (this.actorRole) {
      case 'owner':
        return 'This owner session does not include administration permission, so framework preferences and Constitution controls remain read-only.';
      case 'operator':
        return 'Only the owner can change framework preferences or the Constitution. Your operator session remains able to inspect and plan.';
      case 'viewer':
        return 'Viewer sessions are read-only. Only the owner can change framework preferences or the Constitution.';
      default:
        return 'Owner-only changes are disabled because HAI could not verify owner authority for this session.';
    }
  }

  get familyOptions(): string[] {
    const families = new Set(this.frameworks.map((framework) => framework.family));
    Object.keys(this.overview?.families ?? {}).forEach((family) => families.add(family));
    return Array.from(families).sort((left, right) => this.stableCompare(left, right));
  }

  get filteredFrameworks(): IFrameworkView[] {
    const query = this.searchText.trim().toLowerCase();
    return this.frameworks.filter((framework) => {
      if (this.familyFilter !== 'all' && framework.family !== this.familyFilter) {
        return false;
      }
      if (this.statusFilter !== 'all' && framework.status !== this.statusFilter) {
        return false;
      }
      if (!query) {
        return true;
      }
      const searchable = [
        framework.name,
        framework.id,
        framework.family,
        framework.purpose,
        ...framework.suitableProblemTypes,
        ...framework.triggerConditions,
      ].join(' ').toLowerCase();
      return searchable.includes(query);
    });
  }

  get displayedFrameworks(): IFrameworkView[] {
    const ordered = [...this.filteredFrameworks].sort((left, right) => {
      if (left.pinned !== right.pinned) {
        return left.pinned ? -1 : 1;
      }
      if (left.enabled !== right.enabled) {
        return left.enabled ? -1 : 1;
      }
      const byName = this.stableCompare(left.name, right.name);
      return byName !== 0 ? byName : this.stableCompare(left.id, right.id);
    });
    return this.isAdvanced ? ordered : ordered.slice(0, 10);
  }

  get hasPartialLoadFailure(): boolean {
    return Object.keys(this.loadErrors).length > 0;
  }

  get loadErrorItems(): Array<{ label: string; message: string }> {
    const labels: Record<string, string> = {
      overview: 'Overview',
      frameworks: 'Catalogue',
      selections: 'Selection history',
      constitution: 'Constitution',
      constitutionHistory: 'Constitution history',
      authorization: 'Authorization',
    };
    return Object.entries(this.loadErrors).map(([key, message]) => ({
      label: labels[key] ?? this.humanize(key),
      message,
    }));
  }

  refresh(): void {
    this.refreshSubscription?.unsubscribe();
    this.preferenceStateHints.clear();
    this.loading = true;
    this.loadErrors = {};
    this.authSession = undefined;

    this.refreshSubscription = forkJoin({
      session: this.authSessionService.session().pipe(
        catchError(() => {
          this.loadErrors['authorization'] =
            'Session authority could not be verified. Mutating controls remain disabled.';
          return of(undefined);
        })
      ),
      overview: this.service.overview().pipe(
        catchError((error: unknown) => {
          this.loadErrors['overview'] = this.errorMessage(error, 'Registry overview is unavailable.');
          return of(undefined);
        })
      ),
      frameworks: this.service.frameworks().pipe(
        catchError((error: unknown) => {
          this.loadErrors['frameworks'] = this.errorMessage(error, 'Framework records are unavailable.');
          return of(undefined);
        })
      ),
      selections: this.service.selections().pipe(
        catchError((error: unknown) => {
          this.loadErrors['selections'] = this.errorMessage(error, 'Selection history is unavailable.');
          return of(undefined);
        })
      ),
      constitution: this.service.constitution().pipe(
        catchError((error: unknown) => {
          this.loadErrors['constitution'] = this.errorMessage(error, 'The active Constitution is unavailable.');
          return of(undefined);
        })
      ),
      constitutionHistory: this.service.constitutionHistory().pipe(
        catchError((error: unknown) => {
          this.loadErrors['constitutionHistory'] = this.errorMessage(
            error,
            'Constitution history is unavailable.'
          );
          return of(undefined);
        })
      ),
    }).pipe(
      takeUntil(this.destroy$),
      finalize(() => (this.loading = false))
    ).subscribe({
      next: ({
        session,
        overview,
        frameworks,
        selections,
        constitution,
        constitutionHistory,
      }) => {
        if (session) {
          this.authSession = session;
        }
        if (overview) {
          this.overview = overview;
        }
        if (frameworks) {
          this.frameworks = frameworks;
        }
        if (selections) {
          const orderedSelections = [...selections].sort((left, right) => {
            const byTime = right.createdAt.localeCompare(left.createdAt);
            return byTime !== 0 ? byTime : this.stableCompare(left.id, right.id);
          });
          this.selections = orderedSelections;
          if (!this.currentSelection && orderedSelections.length > 0) {
            this.currentSelection = orderedSelections[0];
          }
        }
        if (constitution) {
          this.constitution = constitution.constitution;
          this.constitutionSource = constitution.source;
          this.constitutionEditor = this.editorFromConstitution(constitution.constitution);
        }
        if (constitutionHistory) {
          this.constitutionHistory = constitutionHistory.history;
          this.constitutionHistoryTruncated = constitutionHistory.truncated;
        }
        if (!this.canManageOwnerControls && this.constitutionEditing) {
          this.cancelConstitutionDraft();
        }
      },
    });
  }

  setViewMode(mode: FrameworkViewMode): void {
    this.viewMode = mode;
    if (mode === 'basic') {
      this.statusFilter = 'all';
    }
    this.persistViewPreference();
  }

  resetView(): void {
    this.viewMode = 'basic';
    this.openSections = { ...DEFAULT_OPEN_SECTIONS };
    this.statusFilter = 'all';
    try {
      window.localStorage.removeItem(VIEW_STORAGE_KEY);
    } catch {
      // Hardened browser contexts may disable local storage.
    }
  }

  sectionOpen(sectionId: string): boolean {
    return this.openSections[sectionId] ?? false;
  }

  toggleSection(sectionId: string): void {
    this.openSections = {
      ...this.openSections,
      [sectionId]: !this.sectionOpen(sectionId),
    };
    this.persistViewPreference();
  }

  selectFrameworks(): void {
    if (this.selectionBusy) {
      return;
    }
    if (!this.canRequestSelection) {
      this.notification.warning(
        'Framework selection is not available',
        this.selectionAccessExplanation
      );
      return;
    }
    const requestText = this.selectionDraft.request.trim();
    if (!requestText) {
      this.selectionError = 'Describe the decision, task, or situation before asking HAI to select a framework.';
      this.focusElement('framework-selection-request');
      return;
    }

    this.selectionBusy = true;
    this.selectionError = '';
    const request = this.buildSelectionRequest(requestText);

    this.service.select(request).pipe(
      takeUntil(this.destroy$),
      finalize(() => (this.selectionBusy = false))
    ).subscribe({
      next: (selection) => {
        this.currentSelection = selection;
        this.selections = [
          selection,
          ...this.selections.filter((item) => item.id !== selection.id),
        ];
        this.notification.success(
          'Framework recommendation ready',
          selection.selected.length > 0
            ? `${selection.selected[0].name} is the primary decision discipline.`
            : 'No suitable enabled framework was found.'
        );
      },
      error: (error: unknown) => {
        this.selectionError = this.errorMessage(
          error,
          'HAI could not create a framework recommendation. No execution was started.'
        );
        this.notification.error('Selection failed', this.selectionError);
      },
    });
  }

  clearSelection(): void {
    this.selectionDraft = this.emptySelectionDraft();
    this.currentSelection = undefined;
    this.selectionError = '';
  }

  openFramework(framework: IFrameworkView): void {
    this.openFrameworkById(framework.id);
  }

  openFrameworkById(frameworkId: string): void {
    const normalizedId = frameworkId.trim();
    if (!normalizedId) {
      return;
    }
    if (!this.inspectorVisible) {
      const activeElement = document.activeElement;
      this.inspectorReturnFocus = activeElement instanceof HTMLElement
        ? activeElement
        : undefined;
    }
    this.inspectorVisible = true;
    this.inspectedFrameworkId = normalizedId;
    this.inspectorLoading = true;
    this.inspectorError = '';
    this.selectedFramework = undefined;
    this.inspectorSubscription?.unsubscribe();

    this.inspectorSubscription = this.service.framework(normalizedId).pipe(
      takeUntil(this.destroy$),
      finalize(() => (this.inspectorLoading = false))
    ).subscribe({
      next: (detail) => {
        this.selectedFramework = detail;
        this.preferenceEditor = this.preferenceEditorFromFramework(detail);
      },
      error: (error: unknown) => {
        this.inspectorError = this.errorMessage(
          error,
          'The current framework record is unavailable.'
        );
        this.notification.error('Framework could not be opened', this.inspectorError);
      },
    });
  }

  retryFrameworkInspector(): void {
    if (this.inspectedFrameworkId && !this.inspectorLoading) {
      this.openFrameworkById(this.inspectedFrameworkId);
    }
  }

  closeInspector(): void {
    const returnFocus = this.inspectorReturnFocus;
    this.inspectorSubscription?.unsubscribe();
    this.inspectorSubscription = undefined;
    this.inspectorVisible = false;
    this.inspectorLoading = false;
    this.selectedFramework = undefined;
    this.inspectorError = '';
    this.inspectedFrameworkId = '';
    this.inspectorReturnFocus = undefined;
    window.setTimeout(() => {
      if (returnFocus?.isConnected) {
        returnFocus.focus();
      }
    });
  }

  savePreference(): void {
    if (this.preferenceSaving) {
      return;
    }
    if (!this.requireOwnerAuthority('Framework preference changes')) {
      return;
    }
    if (!this.selectedFramework) {
      return;
    }
    const preferenceError = this.preferenceValidationError(this.selectedFramework);
    if (preferenceError) {
      this.notification.warning('Check the preference', preferenceError);
      return;
    }

    const patch: IFrameworkPreferencePatch = {
      state: this.preferenceEditor.state,
      pinned: this.preferenceEditor.pinned,
      adaptations: this.lines(this.preferenceEditor.adaptationsText),
    };
    if (this.preferenceEditor.maximumAutonomyLevel === null) {
      patch.clearAutonomyOverride = true;
    } else {
      patch.maximumAutonomyLevel = this.preferenceEditor.maximumAutonomyLevel;
    }

    const frameworkId = this.selectedFramework.id;
    this.preferenceSaving = true;
    this.service.updatePreference(frameworkId, patch).pipe(
      takeUntil(this.destroy$),
      finalize(() => (this.preferenceSaving = false))
    ).subscribe({
      next: (updated) => {
        this.preferenceStateHints.set(updated.id, patch.state);
        this.selectedFramework = updated;
        this.preferenceEditor = this.preferenceEditorFromFramework(updated);
        this.frameworks = this.frameworks.map((framework) =>
          framework.id === updated.id ? updated : framework
        );
        this.notification.success('Preference saved', `${updated.name} was updated for this owner.`);
        this.refreshOverview();
      },
      error: (error: unknown) => {
        this.notification.error(
          'Preference not saved',
          this.errorMessage(error, 'The existing framework preference was not changed.')
        );
      },
    });
  }

  beginConstitutionDraft(): void {
    if (!this.requireOwnerAuthority('Constitution amendments')) {
      return;
    }
    if (!this.constitution) {
      return;
    }
    this.constitutionEditor = this.editorFromConstitution(this.constitution);
    this.constitutionEditing = true;
    this.constitutionDraft = undefined;
    this.activationConfirmation = '';
    this.activationNote = '';
    this.constitutionError = '';
  }

  openConstitutionGovernance(): void {
    this.viewMode = 'advanced';
    this.openSections = {
      ...this.openSections,
      'constitution-governance': true,
    };
    this.persistViewPreference();
    window.setTimeout(() => {
      const summary = document.getElementById('constitution-governance-summary');
      summary?.scrollIntoView({
        behavior: this.scrollBehavior(),
        block: 'start',
      });
      summary?.focus({ preventScroll: true });
    });
  }

  cancelConstitutionDraft(): void {
    this.constitutionEditing = false;
    this.constitutionDraft = undefined;
    this.activationConfirmation = '';
    this.activationNote = '';
    this.constitutionError = '';
    if (this.constitution) {
      this.constitutionEditor = this.editorFromConstitution(this.constitution);
    }
  }

  createConstitutionDraft(): void {
    if (this.constitutionSaving) {
      return;
    }
    if (!this.requireOwnerAuthority('Constitution amendments')) {
      return;
    }
    if (!this.constitution) {
      return;
    }
    if (this.constitutionDraft) {
      this.constitutionError =
        `Draft v${this.constitutionDraft.version} is already immutable. Close this editor before preparing another version.`;
      this.notification.warning('Draft already created', this.constitutionError);
      return;
    }
    const changeSummary = this.constitutionEditor.changeSummary.trim();
    if (!changeSummary) {
      this.constitutionError = 'Explain why this Constitution draft is needed.';
      this.notification.warning('Change summary required', this.constitutionError);
      this.focusElement('constitution-change-summary');
      return;
    }
    this.constitutionError = '';

    const request: IConstitutionDraftRequest = {
      baseVersion: this.constitution.version,
      values: this.lines(this.constitutionEditor.values),
      prohibitions: this.lines(this.constitutionEditor.prohibitions),
      standingPermissions: this.lines(this.constitutionEditor.standingPermissions),
      preferences: this.lines(this.constitutionEditor.preferences),
      relationshipRules: this.lines(this.constitutionEditor.relationshipRules),
      financialBoundaries: this.lines(this.constitutionEditor.financialBoundaries),
      communicationRules: this.lines(this.constitutionEditor.communicationRules),
      escalationRules: this.lines(this.constitutionEditor.escalationRules),
      changeSummary,
    };

    this.constitutionSaving = true;
    this.service.createConstitutionDraft(request).pipe(
      takeUntil(this.destroy$),
      finalize(() => (this.constitutionSaving = false))
    ).subscribe({
      next: (draft) => {
        this.constitutionDraft = draft;
        this.constitutionEditor = this.editorFromConstitution(
          draft,
          draft.changeSummary ?? changeSummary
        );
        this.notification.success(
          'Constitution draft created',
          `Version ${draft.version} remains inactive until explicitly approved.`
        );
      },
      error: (error: unknown) => {
        this.constitutionError = this.constitutionMutationError(
          error,
          'The active Constitution was not changed.',
          'The active Constitution changed before this draft was saved. Refresh and review the current version before drafting again.'
        );
        this.notification.error(
          'Draft not created',
          this.constitutionError
        );
      },
    });
  }

  activateConstitutionDraft(): void {
    if (this.constitutionActivating) {
      return;
    }
    if (!this.requireOwnerAuthority('Constitution activation')) {
      return;
    }
    if (!this.constitutionDraft) {
      return;
    }
    if (this.activationConfirmation !== 'ACTIVATE CONSTITUTION') {
      this.constitutionError = 'Type ACTIVATE CONSTITUTION exactly.';
      this.notification.warning(
        'Exact confirmation required',
        this.constitutionError
      );
      this.focusElement('constitution-activation-confirmation');
      return;
    }
    if (this.activationNote.trim().length < 10) {
      this.constitutionError = 'Explain the approval in at least 10 characters.';
      this.notification.warning(
        'Approval note required',
        this.constitutionError
      );
      this.focusElement('constitution-activation-note');
      return;
    }

    this.constitutionError = '';
    this.constitutionActivating = true;
    this.service.activateConstitution(this.constitutionDraft.id, {
      confirmation: this.activationConfirmation,
      approvalNote: this.activationNote.trim(),
    }).pipe(
      takeUntil(this.destroy$),
      finalize(() => (this.constitutionActivating = false))
    ).subscribe({
      next: (active) => {
        this.constitution = active;
        this.constitutionSource = `${active.id}:v${active.version}`;
        this.constitutionDraft = undefined;
        this.constitutionEditing = false;
        this.activationConfirmation = '';
        this.activationNote = '';
        this.constitutionError = '';
        this.constitutionEditor = this.editorFromConstitution(active);
        this.notification.success('Constitution activated', `Version ${active.version} is now active.`);
        this.refresh();
      },
      error: (error: unknown) => {
        this.constitutionError = this.constitutionMutationError(
          error,
          'The draft remains inactive and the current Constitution is unchanged.',
          'This draft is stale or no longer activatable. Refresh the active Constitution and prepare a new reviewed draft.'
        );
        this.notification.error(
          'Activation blocked',
          this.constitutionError
        );
      },
    });
  }

  chooseSelection(selection: IFrameworkSelectionDecision): void {
    this.currentSelection = selection;
    window.setTimeout(() => {
      const heading = document.getElementById('recommendation-heading');
      heading?.scrollIntoView({ behavior: this.scrollBehavior(), block: 'start' });
      heading?.focus({ preventScroll: true });
    });
  }

  clearFilters(): void {
    this.searchText = '';
    this.familyFilter = 'all';
    this.statusFilter = 'all';
  }

  statusColor(status: string): string {
    switch (status) {
      case 'active':
      case 'enabled':
        return 'green';
      case 'experimental':
        return 'gold';
      case 'deprecated':
      case 'disabled':
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

  isProtectedFramework(framework: IFrameworkView): boolean {
    return isProtectedFrameworkId(framework.id);
  }

  constitutionValues(
    constitution: IConstitution,
    section: IConstitutionRuleSection
  ): string[] {
    return constitution[section.field];
  }

  isMachineEnforcedConstitutionRule(value: string): boolean {
    const fields = value.trim().toLowerCase().split(/\s+/);
    if (fields.length !== 4 || fields[0] !== 'hai-rule' || fields[1] !== 'v1') {
      return false;
    }
    if (fields[2] === 'authority-ceiling') {
      if (!fields[3].startsWith('level=')) {
        return false;
      }
      const rawLevel = fields[3].slice('level='.length);
      if (!/^\+?\d+$/.test(rawLevel)) {
        return false;
      }
      const level = Number(rawLevel);
      return Number.isInteger(level) && level >= 0 && level <= 10;
    }
    if (!['deny-capability', 'require-approval'].includes(fields[2])) {
      return false;
    }
    if (!fields[3].startsWith('capability=')) {
      return false;
    }
    return this.typedRuleCapabilities.has(
      fields[3].slice('capability='.length)
    );
  }

  humanize(value: string): string {
    if (!value) {
      return 'Unknown';
    }
    const normalized = value.replace(/[_-]+/g, ' ');
    return normalized.charAt(0).toUpperCase() + normalized.slice(1);
  }

  selectionRiskCeilingLabel(selection: IFrameworkSelectionDecision): string {
    const riskCeiling = selection.selected[0]?.riskCeiling;
    return riskCeiling
      ? `${this.humanize(riskCeiling)} risk`
      : 'Not recorded (legacy)';
  }

  trackFramework(_index: number, framework: IFrameworkView): string {
    return framework.id;
  }

  trackSelection(_index: number, selection: IFrameworkSelectionDecision): string {
    return selection.id;
  }

  trackConstitutionHistory(
    _index: number,
    entry: IConstitutionHistoryEntry
  ): string {
    return `${entry.id}:${entry.status}:${entry.digest}`;
  }

  trackConstitutionSection(
    _index: number,
    section: IConstitutionRuleSection
  ): ConstitutionRuleField {
    return section.field;
  }

  trackString(_index: number, value: string): string {
    return value;
  }

  goBack(): void {
    this.router.navigate(['/control-center']);
  }

  private refreshOverview(): void {
    this.overviewSubscription?.unsubscribe();
    this.overviewSubscription = this.service.overview().pipe(
      takeUntil(this.destroy$)
    ).subscribe({
      next: (overview) => (this.overview = overview),
      error: () => {
        this.loadErrors['overview'] = 'Registry overview is stale. Refresh to retry.';
      },
    });
  }

  private requireOwnerAuthority(action: string): boolean {
    if (this.canManageOwnerControls) {
      return true;
    }
    this.notification.warning(`${action} are owner-only`, this.ownerControlExplanation);
    return false;
  }

  private focusElement(id: string): void {
    window.setTimeout(() => {
      document.getElementById(id)?.focus();
    });
  }

  private scrollBehavior(): ScrollBehavior {
    try {
      return window.matchMedia('(prefers-reduced-motion: reduce)').matches
        ? 'auto'
        : 'smooth';
    } catch {
      return 'auto';
    }
  }

  private buildSelectionRequest(requestText: string): IFrameworkSelectionRequest {
    const request: IFrameworkSelectionRequest = {
      request: requestText,
      needsMemory: this.selectionDraft.needsMemory,
      needsTools: this.selectionDraft.needsTools,
      needsDocuments: this.selectionDraft.needsDocuments,
      needsWebAccess: this.selectionDraft.needsWebAccess,
      needsLocalExecution: this.selectionDraft.needsLocalExecution,
      executeRequested: this.selectionDraft.executeRequested,
    };

    this.assignIfPresent(request, 'projectKey', this.selectionDraft.projectKey);
    this.assignIfPresent(request, 'pursuitId', this.selectionDraft.pursuitId);
    this.assignIfPresent(request, 'taskType', this.selectionDraft.taskType);
    this.assignIfPresent(request, 'requiredReasoning', this.selectionDraft.requiredReasoning);

    if (this.selectionDraft.difficulty !== null) {
      request.difficulty = this.selectionDraft.difficulty;
    }
    const successCriteria = this.lines(this.selectionDraft.successCriteriaText);
    if (successCriteria.length > 0) {
      request.successCriteria = successCriteria;
    }
    return request;
  }

  private assignIfPresent(
    request: IFrameworkSelectionRequest,
    key: 'projectKey' | 'pursuitId' | 'taskType' | 'requiredReasoning',
    value: string
  ): void {
    const trimmed = value.trim();
    if (trimmed) {
      request[key] = trimmed;
    }
  }

  private restoreViewPreference(): void {
    try {
      const raw = window.localStorage.getItem(VIEW_STORAGE_KEY);
      if (!raw) {
        return;
      }
      const parsed = JSON.parse(raw) as Partial<IFrameworkModuleViewPreferences>;
      if (
        parsed.version !== 1 ||
        (parsed.mode !== 'basic' && parsed.mode !== 'advanced') ||
        !parsed.openSections ||
        typeof parsed.openSections !== 'object'
      ) {
        return;
      }
      this.viewMode = parsed.mode;
      this.openSections = {
        ...DEFAULT_OPEN_SECTIONS,
        ...parsed.openSections,
      };
    } catch {
      this.viewMode = 'basic';
      this.openSections = { ...DEFAULT_OPEN_SECTIONS };
    }
  }

  private persistViewPreference(): void {
    const preference: IFrameworkModuleViewPreferences = {
      version: 1,
      mode: this.viewMode,
      openSections: this.openSections,
    };
    try {
      window.localStorage.setItem(VIEW_STORAGE_KEY, JSON.stringify(preference));
    } catch {
      // View preferences are optional and never block registry operations.
    }
  }

  private preferenceEditorFromFramework(framework: IFrameworkView): IFrameworkPreferenceEditor {
    const preferenceState = this.preferenceStateHints.get(framework.id) ??
      (!framework.preferenceUpdatedAt
      ? 'default'
      : !framework.enabled
        ? 'disabled'
        : framework.status === 'active'
          ? 'default'
          : 'enabled');
    return {
      state: preferenceState,
      pinned: framework.pinned,
      maximumAutonomyLevel: framework.preferenceUpdatedAt
        ? framework.effectiveAutonomyLevel
        : null,
      adaptationsText: framework.adaptations.join('\n'),
    };
  }

  private editorFromConstitution(
    constitution: IConstitution,
    changeSummary = ''
  ): IConstitutionEditor {
    return {
      values: constitution.values.join('\n'),
      prohibitions: constitution.prohibitions.join('\n'),
      standingPermissions: constitution.standingPermissions.join('\n'),
      preferences: constitution.preferences.join('\n'),
      relationshipRules: constitution.relationshipRules.join('\n'),
      financialBoundaries: constitution.financialBoundaries.join('\n'),
      communicationRules: constitution.communicationRules.join('\n'),
      escalationRules: constitution.escalationRules.join('\n'),
      changeSummary,
    };
  }

  private lines(value: string): string[] {
    return value
      .split(/\r?\n/)
      .map((line) => line.trim())
      .filter((line, index, all) => Boolean(line) && all.indexOf(line) === index);
  }

  private stableCompare(left: string, right: string): number {
    const normalizedLeft = left.trim().toLowerCase();
    const normalizedRight = right.trim().toLowerCase();
    if (normalizedLeft < normalizedRight) {
      return -1;
    }
    if (normalizedLeft > normalizedRight) {
      return 1;
    }
    return left < right ? -1 : left > right ? 1 : 0;
  }

  private errorMessage(error: unknown, fallback: string): string {
    const response = error as HttpErrorResponse;
    if (!(response instanceof HttpErrorResponse)) {
      return fallback;
    }
    if (response.status === 0) {
      return 'HAI could not reach the registry service. Refresh after the local backend is available.';
    }
    if (response.status === 401) {
      return 'Your local session has expired. Sign in again before retrying.';
    }
    if (response.status === 403) {
      return 'Your account does not have permission to perform this registry action.';
    }
    if (![400, 409, 422].includes(response.status)) {
      return fallback;
    }

    const body = response?.error as { error?: unknown; message?: unknown } | string | null;
    let candidate = '';
    if (typeof body === 'string' && body.trim()) {
      candidate = body.trim();
    } else if (body && typeof body === 'object') {
      if (typeof body.error === 'string' && body.error.trim()) {
        candidate = body.error.trim();
      } else if (typeof body.message === 'string' && body.message.trim()) {
        candidate = body.message.trim();
      }
    }
    if (
      !candidate ||
      candidate.length > 240 ||
      /(?:sql|database|gorm|stack|trace|panic|dial tcp|internal server|exception|password|passwd|secret|api[-_ ]?key|access[-_ ]?token|refresh[-_ ]?token|authorization|cookie|bearer)/i.test(candidate)
    ) {
      return fallback;
    }
    return candidate.replace(/\s+/g, ' ');
  }

  private constitutionMutationError(
    error: unknown,
    fallback: string,
    conflictMessage: string
  ): string {
    return error instanceof HttpErrorResponse && error.status === 409
      ? conflictMessage
      : this.errorMessage(error, fallback);
  }

  private preferenceValidationError(framework: IFrameworkView): string {
    if (
      !['default', 'enabled', 'disabled'].includes(this.preferenceEditor.state)
    ) {
      return 'Choose a valid framework availability state.';
    }
    if (
      isProtectedFrameworkId(framework.id) &&
      this.preferenceEditor.state === 'disabled'
    ) {
      return 'Protected safety overlays cannot be disabled.';
    }
    const level = this.preferenceEditor.maximumAutonomyLevel;
    if (
      level !== null &&
      (!Number.isInteger(level) ||
        level < 0 ||
        level > framework.maximumAutonomyLevel)
    ) {
      return `The autonomy override must be a whole number from 0 to ${framework.maximumAutonomyLevel}.`;
    }
    return '';
  }

  private emptySelectionDraft(): ISelectionDraft {
    return {
      request: '',
      projectKey: '',
      pursuitId: '',
      taskType: '',
      difficulty: null,
      requiredReasoning: '',
      successCriteriaText: '',
      needsMemory: false,
      needsTools: false,
      needsDocuments: false,
      needsWebAccess: false,
      needsLocalExecution: false,
      executeRequested: false,
    };
  }

  private emptyPreferenceEditor(): IFrameworkPreferenceEditor {
    return {
      state: 'default',
      pinned: false,
      maximumAutonomyLevel: null,
      adaptationsText: '',
    };
  }

  private emptyConstitutionEditor(): IConstitutionEditor {
    return {
      values: '',
      prohibitions: '',
      standingPermissions: '',
      preferences: '',
      relationshipRules: '',
      financialBoundaries: '',
      communicationRules: '',
      escalationRules: '',
      changeSummary: '',
    };
  }
}
