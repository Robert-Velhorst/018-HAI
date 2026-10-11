import { ChangeDetectionStrategy, Component, NgZone, OnInit } from '@angular/core'
import { IAuthSession } from '../../models/auth-session.model.interface'
import { ISkillGuidancePreview, ISkillInventoryEntry, ISkillSelectionInventory, ISkillSelectionState, MAX_SKILL_GUIDANCE_PREVIEW_BYTES } from '../../models/skills.model.interface'
import { AuthSessionService } from '../../services/auth-session.service'
import { SkillsService } from '../../services/skills.service'

const SKILLS_SOURCE_REPOSITORY = 'anthropics/skills'
const SKILL_ID_PATTERN = /^[a-z0-9]+(?:-[a-z0-9]+)*$/

@Component({
    changeDetection: ChangeDetectionStrategy.Eager,
  selector: 'app-skills',
  templateUrl: './skills.component.html',
  styleUrls: ['./skills.component.scss'],
  standalone: false,
})
export class SkillsComponent implements OnInit {
  inventory?: ISkillSelectionInventory
  session?: IAuthSession
  loading = false
  sessionLoading = false
  sessionUnavailable = false
  approvalAccessDenied = false
  approvalAccessMessage = ''
  unavailable = false
  readonly pendingSkillIds = new Set<string>()
  readonly selectionRequiresRefreshIds = new Set<string>()
  readonly selectionErrors: Record<string, string> = {}
  selectionAnnouncement = ''
  readonly guidancePreviewLoadingIds = new Set<string>()
  readonly guidancePreviewErrors: Record<string, string> = {}
  readonly guidancePreviews: Record<string, ISkillGuidancePreview> = {}
  private readonly verifiedGuidancePreviewHashes = new Map<string, string>()
  private readonly verifiedGuidancePreviewCatalogFingerprints = new Map<string, string>()
  private readonly guidancePreviewRequestIds = new Map<string, number>()

  constructor(
    private skillsService: SkillsService,
    private authSessionService: AuthSessionService,
    private ngZone?: NgZone,
  ) {}

  ngOnInit(): void { this.refresh() }

  refresh(preserveSelectionErrorFor?: string, selectionView: 'overview' | 'inventory' = 'overview'): void {
    if (this.loading || this.sessionLoading || this.pendingSkillIds.size > 0) return
    const recoveringFromUnavailable = this.unavailable
    this.approvalAccessDenied = false
    this.approvalAccessMessage = ''
    this.loadSession()
    this.loading = true
    this.unavailable = false
    this.clearGuidancePreviews()
    this.skillsService.inventory().subscribe({
      next: (inventory) => {
        this.inventory = inventory
        this.selectionRequiresRefreshIds.clear()
        Object.keys(this.selectionErrors).forEach((skillId) => {
          if (skillId !== preserveSelectionErrorFor) delete this.selectionErrors[skillId]
        })
        this.loading = false
        if (preserveSelectionErrorFor) {
          if (this.canApprove) this.focusSelectionControl(preserveSelectionErrorFor, selectionView)
          else this.focusSkillsHeading()
        } else if (recoveringFromUnavailable) {
          this.focusSkillsHeading()
        }
      },
      error: () => {
        this.loading = false
        this.unavailable = true
        this.inventory = undefined
        if (preserveSelectionErrorFor) this.focusRefreshControl()
      },
    })
  }

  get visibleSkills(): ISkillInventoryEntry[] {
    return (this.inventory?.skills ?? []).slice(0, 6)
  }

  get enabledSkills(): number {
    return (this.inventory?.skills ?? []).filter((skill) => this.isEffectivelyEnabled(skill)).length
  }

  get skillsNeedingReapproval(): number {
    return (this.inventory?.skills ?? []).filter((skill) => skill.needsReapproval).length
  }

  get canApprove(): boolean {
    return !this.approvalAccessDenied && this.session?.authenticated === true && this.session.permissions.canApprove === true
  }

  get accessMessage(): string {
    if (this.sessionLoading) return 'Checking account permissions. Selection controls stay hidden until approval access is confirmed.'
    if (this.sessionUnavailable) return 'HAI could not verify your account permissions. For safety, selection controls are hidden. Refresh to try again.'
    if (this.approvalAccessDenied) return this.approvalAccessMessage
    if (this.session?.authenticated && this.session.permissions.canRead && !this.canApprove) {
      return 'This account can inspect the Skills inventory but does not have approval permission. Selection controls are hidden.'
    }
    if (this.session && !this.canApprove) {
      return 'No authenticated approval permission was confirmed. Selection controls are hidden.'
    }
    return ''
  }

  isEffectivelyEnabled(skill: ISkillInventoryEntry): boolean {
    return skill.enabled && !skill.needsReapproval
  }

  isPending(skill: ISkillInventoryEntry): boolean {
    return this.pendingSkillIds.has(skill.id)
  }

  isSelectionDisabled(skill: ISkillInventoryEntry): boolean {
    return !this.canApprove || this.loading || this.isPending(skill) || this.selectionRequiresRefreshIds.has(skill.id) || !this.inventory?.skills.some((current) => current.id === skill.id)
  }

  hasCurrentGuidancePreview(skill: ISkillInventoryEntry): boolean {
    const preview = this.guidancePreviews[skill.id]
    const verifiedHash = this.verifiedGuidancePreviewHashes.get(skill.id)
    const verifiedCatalogFingerprint = this.verifiedGuidancePreviewCatalogFingerprints.get(skill.id)
    return preview?.skillId === skill.id &&
      preview.currentGuidanceSHA256 === skill.guidanceSHA256 &&
      !!this.inventory?.catalogFingerprint &&
      preview.catalogFingerprint === this.inventory.catalogFingerprint &&
      verifiedCatalogFingerprint === this.inventory.catalogFingerprint &&
      preview.currentGuidance.length > 0 &&
      verifiedHash === skill.guidanceSHA256 &&
      new TextEncoder().encode(preview.currentGuidance).byteLength <= MAX_SKILL_GUIDANCE_PREVIEW_BYTES &&
      preview.previousGuidanceTextStored === false &&
      (!skill.needsReapproval || (
        !!preview.previousApprovedGuidanceSHA256 &&
        !!preview.previousGuidanceTextLimitation
      ))
  }

  isGuidancePreviewLoading(skill: ISkillInventoryEntry): boolean {
    return this.guidancePreviewLoadingIds.has(skill.id)
  }

  isGuidanceReviewDisabled(skill: ISkillInventoryEntry): boolean {
    return !this.canApprove || this.loading || this.isPending(skill) || this.isGuidancePreviewLoading(skill)
  }

  reviewGuidance(skill: ISkillInventoryEntry): void {
    const displayedSkill = this.inventory?.skills.find((current) => current.id === skill.id)
    if (!this.canApprove || this.loading || !displayedSkill || this.isPending(displayedSkill) || this.isGuidancePreviewLoading(displayedSkill)) return
    if (this.hasCurrentGuidancePreview(displayedSkill)) {
      delete this.guidancePreviews[skill.id]
      this.verifiedGuidancePreviewHashes.delete(skill.id)
      this.verifiedGuidancePreviewCatalogFingerprints.delete(skill.id)
      delete this.guidancePreviewErrors[skill.id]
      return
    }

    const requestId = (this.guidancePreviewRequestIds.get(skill.id) ?? 0) + 1
    this.guidancePreviewRequestIds.set(skill.id, requestId)
    this.guidancePreviewLoadingIds.add(skill.id)
    delete this.guidancePreviewErrors[skill.id]
    this.skillsService.guidancePreview(skill.id).subscribe({
      next: (preview) => {
        void this.acceptGuidancePreview(preview, skill.id, requestId)
      },
      error: (error) => {
        if (this.guidancePreviewRequestIds.get(skill.id) !== requestId) return
        this.guidancePreviewLoadingIds.delete(skill.id)
        if (error?.status === 401 || error?.status === 403) {
          this.denyApprovalAccess('The server denied access to the guidance preview. Approval controls are hidden. Refresh to re-check your access.')
          return
        }
        this.guidancePreviewErrors[skill.id] = 'HAI could not load the current HAI-authored guidance. The skill cannot be enabled until it can be reviewed.'
      },
    })
  }

  isEnableSelectionDisabled(skill: ISkillInventoryEntry): boolean {
    return this.isSelectionDisabled(skill) || !this.hasCurrentGuidancePreview(skill)
  }

  updateSelection(skill: ISkillInventoryEntry, enabled: boolean, view: 'overview' | 'inventory' = 'overview'): void {
    const displayedSkill = this.inventory?.skills.find((current) => current.id === skill.id)
    if (!this.canApprove || this.loading || !displayedSkill || this.isPending(displayedSkill) || this.selectionRequiresRefreshIds.has(skill.id)) return
    if (enabled && !this.hasCurrentGuidancePreview(displayedSkill)) {
      this.selectionErrors[skill.id] = 'Review the exact current HAI-authored guidance before enabling this skill.'
      return
    }

    this.pendingSkillIds.add(skill.id)
    delete this.selectionErrors[skill.id]
    this.selectionAnnouncement = ''
    const reviewedGuidanceSHA256 = enabled ? this.guidancePreviews[skill.id]?.currentGuidanceSHA256 : undefined
    const reviewedCatalogFingerprint = enabled ? this.guidancePreviews[skill.id]?.catalogFingerprint : undefined
    const selectionRequest = enabled
      ? this.skillsService.setEnabled(skill.id, true, reviewedGuidanceSHA256, reviewedCatalogFingerprint)
      : this.skillsService.setEnabled(skill.id, false)
    selectionRequest.subscribe({
      next: (state) => {
        if (!isSkillSelectionState(state)) {
          this.pendingSkillIds.delete(skill.id)
          this.selectionRequiresRefreshIds.add(skill.id)
          this.selectionErrors[skill.id] = 'HAI returned an invalid selection result. Refresh the Skills inventory before taking another action.'
          this.focusRefreshControl()
          return
        }
        const currentDisplayedSkill = this.inventory?.skills.find((current) => current.id === skill.id)
        if (currentDisplayedSkill) {
          currentDisplayedSkill.enabled = state.enabled
          currentDisplayedSkill.needsReapproval = state.needsReapproval
          currentDisplayedSkill.selectionDecision = state.selectionDecision ?? undefined
        }
        const effectivelyEnabled = state.enabled && !state.needsReapproval
        const effectiveState = effectivelyEnabled ? 'enabled' : (state.needsReapproval ? 'disabled pending version review' : 'disabled')
        if (effectivelyEnabled !== enabled) {
          this.selectionAnnouncement = state.supersededByConcurrentDecision
            ? `A newer concurrent decision won for ${displayedSkill.name}. The effective state is now ${effectiveState}.`
            : `HAI reports ${displayedSkill.name} is ${effectiveState}, which differs from the state you requested.`
        } else {
          this.selectionAnnouncement = `HAI confirmed ${displayedSkill.name} is ${effectiveState}.`
        }
        if (state.selectionDecision) {
          this.pendingSkillIds.delete(skill.id)
          this.focusSelectionControl(skill.id, view)
          return
        }

        // Older servers return only the selection flags. Read the inventory
        // once so the row never keeps audit metadata from the previous choice.
        this.skillsService.inventory().subscribe({
          next: (inventory) => {
            const currentInventory = this.inventory
            const updatedRow = currentInventory?.skills.find((current) => current.id === skill.id)
            const latestRow = inventory.skills.find((current) => current.id === skill.id)
            if (currentInventory && updatedRow && latestRow) {
              const catalogChanged = currentInventory.catalogFingerprint !== inventory.catalogFingerprint
              const currentRows = new Map(currentInventory.skills.map((current) => [current.id, current]))
              const refreshedRows = inventory.skills.map((latest) => {
                const current = currentRows.get(latest.id)
                if (!current) return latest
                Object.assign(current, latest)
                return current
              })
              currentInventory.sourceRepository = inventory.sourceRepository
              currentInventory.sourceCommit = inventory.sourceCommit
              currentInventory.sourceCommitDate = inventory.sourceCommitDate
              currentInventory.catalogFingerprint = inventory.catalogFingerprint
              currentInventory.skills.splice(0, currentInventory.skills.length, ...refreshedRows)
              if (catalogChanged) {
                const savedAnnouncement = this.selectionAnnouncement
                this.invalidateGuidancePreviews()
                this.selectionAnnouncement = `${savedAnnouncement} The skill catalog changed after this save. HAI loaded the latest catalog and cleared previous previews. Review current guidance before enabling another skill.`
              }
            } else {
              this.selectionRequiresRefreshIds.add(skill.id)
              this.selectionErrors[skill.id] = 'Selection saved, but HAI could not find its latest audit details. Refresh the Skills inventory to review them.'
            }
            this.pendingSkillIds.delete(skill.id)
            if (this.selectionRequiresRefreshIds.has(skill.id)) this.focusRefreshControl()
            else this.focusSelectionControl(skill.id, view)
          },
          error: () => {
            this.selectionRequiresRefreshIds.add(skill.id)
            this.selectionErrors[skill.id] = 'Selection saved, but the latest audit details could not be refreshed. Refresh the Skills inventory to review them.'
            this.pendingSkillIds.delete(skill.id)
            this.focusRefreshControl()
          },
        })
      },
      error: (error) => {
        this.pendingSkillIds.delete(skill.id)
        if (error?.status === 401 || error?.status === 403) {
          this.denyApprovalAccess('The server denied this selection change, so it was not applied. Approval controls are hidden. Refresh to re-check your access.')
          return
        }
        if (enabled && error?.status === 409) {
          delete this.guidancePreviews[skill.id]
          this.verifiedGuidancePreviewHashes.delete(skill.id)
          this.verifiedGuidancePreviewCatalogFingerprints.delete(skill.id)
          delete this.guidancePreviewErrors[skill.id]
          this.selectionRequiresRefreshIds.add(skill.id)
          this.selectionErrors[skill.id] = 'HAI rejected this choice because the guidance or catalog/source identity changed after review. No selection change was applied. The latest inventory is being loaded; review the current version before enabling.'
          this.refresh(skill.id, view)
          return
        }
        this.selectionRequiresRefreshIds.add(skill.id)
        this.selectionErrors[skill.id] = 'HAI could not confirm whether this selection was saved. Refresh the Skills inventory to check its current state before trying again.'
        this.focusRefreshControl()
      },
    })
  }

  selectionActionsId(skillId: string, view: 'overview' | 'inventory'): string {
    return `skills-selection-${view}-${encodeURIComponent(skillId)}`
  }

  guidancePreviewId(skillId: string, view: 'overview' | 'inventory'): string {
    return `skills-guidance-${view}-${encodeURIComponent(skillId)}`
  }

  licenseHref(skill: ISkillInventoryEntry): string | undefined {
    return this.pinnedSkillLinks(skill)?.licenseURL
  }

  sourceHref(skill: ISkillInventoryEntry): string | undefined {
    return this.pinnedSkillLinks(skill)?.sourceURL
  }

  sourceProvenanceLabel(skill: ISkillInventoryEntry): string {
    const inventory = this.inventory
    if (!inventory || !this.pinnedSkillLinks(skill)) return 'Pinned source unavailable'
    return `${inventory.sourceRepository} @ ${inventory.sourceCommit.slice(0, 7)}`
  }

  sourceLinkAriaLabel(skill: ISkillInventoryEntry): string {
    const commit = this.inventory?.sourceCommit
    return `View pinned source for ${skill.name}${commit ? ` at commit ${commit}` : ''} (opens in a new tab)`
  }

  private loadSession(): void {
    this.session = undefined
    this.sessionUnavailable = false
    this.sessionLoading = true
    this.authSessionService.session().subscribe({
      next: (session) => {
        this.session = session
        this.sessionLoading = false
      },
      error: () => {
        this.session = undefined
        this.sessionUnavailable = true
        this.sessionLoading = false
      },
    })
  }

  private clearGuidancePreviews(): void {
    this.invalidateGuidancePreviews()
    this.selectionAnnouncement = ''
  }

  private invalidateGuidancePreviews(): void {
    for (const [skillId, requestId] of this.guidancePreviewRequestIds) {
      this.guidancePreviewRequestIds.set(skillId, requestId + 1)
    }
    Object.keys(this.guidancePreviews).forEach((skillId) => delete this.guidancePreviews[skillId])
    this.verifiedGuidancePreviewHashes.clear()
    this.verifiedGuidancePreviewCatalogFingerprints.clear()
    Object.keys(this.guidancePreviewErrors).forEach((skillId) => delete this.guidancePreviewErrors[skillId])
    this.guidancePreviewLoadingIds.clear()
  }

  private async acceptGuidancePreview(preview: ISkillGuidancePreview, skillId: string, requestId: number): Promise<void> {
    try {
      const current = this.inventory?.skills.find((entry) => entry.id === skillId)
      const valid = !!current && await this.matchesCurrentGuidance(preview, current)
      if (this.guidancePreviewRequestIds.get(skillId) !== requestId) return
      this.guidancePreviewLoadingIds.delete(skillId)
      const latest = this.inventory?.skills.find((entry) => entry.id === skillId)
      if (!this.canApprove) return
      if (!valid || !latest || latest.guidanceSHA256 !== preview.currentGuidanceSHA256) {
        delete this.guidancePreviews[skillId]
        this.verifiedGuidancePreviewHashes.delete(skillId)
        this.verifiedGuidancePreviewCatalogFingerprints.delete(skillId)
        this.guidancePreviewErrors[skillId] = 'HAI returned guidance or catalog identity that could not be verified against the current inventory hash. Refresh, then review the current version before enabling.'
        return
      }
      this.guidancePreviews[skillId] = { ...preview }
      this.verifiedGuidancePreviewHashes.set(skillId, latest.guidanceSHA256)
      this.verifiedGuidancePreviewCatalogFingerprints.set(skillId, preview.catalogFingerprint)
    } catch {
      if (this.guidancePreviewRequestIds.get(skillId) !== requestId) return
      this.guidancePreviewLoadingIds.delete(skillId)
      delete this.guidancePreviews[skillId]
      this.verifiedGuidancePreviewHashes.delete(skillId)
      this.verifiedGuidancePreviewCatalogFingerprints.delete(skillId)
      this.guidancePreviewErrors[skillId] = 'HAI could not verify the guidance text integrity. The skill cannot be enabled until it can be verified.'
    }
  }

  private async matchesCurrentGuidance(preview: ISkillGuidancePreview, skill: ISkillInventoryEntry): Promise<boolean> {
    if (!preview || typeof preview !== 'object' || typeof preview.currentGuidance !== 'string' ||
      preview.skillId !== skill.id || !/^[a-f0-9]{64}$/.test(skill.guidanceSHA256) ||
      preview.currentGuidanceSHA256 !== skill.guidanceSHA256 ||
      !/^[a-f0-9]{64}$/.test(this.inventory?.catalogFingerprint ?? '') ||
      !/^[a-f0-9]{64}$/.test(preview.catalogFingerprint) ||
      preview.catalogFingerprint !== this.inventory?.catalogFingerprint ||
      preview.previousGuidanceTextStored !== false) return false

    const bytes = new TextEncoder().encode(preview.currentGuidance)
    if (bytes.byteLength === 0 || bytes.byteLength > MAX_SKILL_GUIDANCE_PREVIEW_BYTES) return false

    if (skill.needsReapproval) {
      if (typeof preview.previousApprovedGuidanceSHA256 !== 'string' ||
        !/^[a-f0-9]{64}$/.test(preview.previousApprovedGuidanceSHA256) ||
        preview.previousApprovedGuidanceSHA256 === preview.currentGuidanceSHA256 ||
        typeof preview.previousGuidanceTextLimitation !== 'string' ||
        preview.previousGuidanceTextLimitation.length === 0 ||
        preview.previousGuidanceTextLimitation.length > 500) return false
    } else if (preview.previousApprovedGuidanceSHA256 !== undefined || preview.previousGuidanceTextLimitation !== undefined) {
      return false
    }

    const subtle = globalThis.crypto?.subtle
    if (!subtle) return false
    const digest = await subtle.digest('SHA-256', bytes)
    const actualHash = Array.from(new Uint8Array(digest), (value) => value.toString(16).padStart(2, '0')).join('')
    return actualHash === skill.guidanceSHA256
  }

  private denyApprovalAccess(message: string): void {
    this.approvalAccessDenied = true
    this.approvalAccessMessage = message
    this.clearGuidancePreviews()
    this.focusRefreshControl()
  }

  private pinnedSkillLinks(skill: ISkillInventoryEntry): { sourceURL: string; licenseURL: string } | undefined {
    const inventory = this.inventory
    const commit = inventory?.sourceCommit
    if (inventory?.sourceRepository !== SKILLS_SOURCE_REPOSITORY ||
      typeof commit !== 'string' || !/^[a-f0-9]{40}$/.test(commit) ||
      !SKILL_ID_PATTERN.test(skill.id)) return undefined

    const skillPath = `skills/${skill.id}`
    const sourcePath = `${skillPath}/SKILL.md`
    const licensePath = `${skillPath}/LICENSE.txt`
    if (skill.sourcePath !== sourcePath || skill.licensePath !== licensePath) return undefined

    const sourceURL = `https://github.com/${SKILLS_SOURCE_REPOSITORY}/blob/${commit}/${sourcePath}`
    const licenseURL = `https://github.com/${SKILLS_SOURCE_REPOSITORY}/blob/${commit}/${licensePath}`
    if (skill.sourceURL !== sourceURL || skill.licenseURL !== licenseURL) return undefined

    return { sourceURL, licenseURL }
  }

  private focusSelectionControl(skillId: string, view: 'overview' | 'inventory'): void {
    this.focusAfterRender(() => {
      const targetFor = (targetView: 'overview' | 'inventory') =>
        document.getElementById(this.selectionActionsId(skillId, targetView))?.querySelector<HTMLButtonElement>('button:not(:disabled)') ?? null
      const target = targetFor(view)
      if (this.isVisible(target)) return target

      const alternate = targetFor(view === 'overview' ? 'inventory' : 'overview')
      if (this.isVisible(alternate)) return alternate

      const disclosure = document.querySelector<HTMLButtonElement>('.skills-page hai-progressive-section .hai-progressive-section__summary')
      if (this.isVisible(disclosure)) return disclosure

      return document.getElementById('skills-heading')
    })
  }

  private isVisible(element: HTMLElement | null): element is HTMLElement {
    return !!element && element.isConnected && element.getClientRects().length > 0
  }

  private focusRefreshControl(): void {
    this.focusAfterRender(() => document.getElementById('skills-refresh') as HTMLButtonElement | null)
  }

  private focusSkillsHeading(): void {
    this.focusAfterRender(() => document.getElementById('skills-heading'))
  }

  private focusAfterRender(target: () => HTMLElement | null): void {
    const focus = () => target()?.focus()
    const schedule = () => {
      if (typeof globalThis.requestAnimationFrame === 'function') globalThis.requestAnimationFrame(() => focus())
      else globalThis.setTimeout(focus, 0)
    }
    if (this.ngZone) this.ngZone.runOutsideAngular(schedule)
    else schedule()
  }
}

function isSkillSelectionState(value: unknown): value is ISkillSelectionState {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return false
  const state = value as Record<string, unknown>
  if (typeof state['enabled'] !== 'boolean' || typeof state['needsReapproval'] !== 'boolean' ||
    (state['supersededByConcurrentDecision'] !== undefined && typeof state['supersededByConcurrentDecision'] !== 'boolean')) return false
  const decision = state['selectionDecision']
  if (decision === undefined || decision === null) return true
  if (typeof decision !== 'object' || Array.isArray(decision)) return false
  const fields = decision as Record<string, unknown>
  return (fields['id'] === undefined || typeof fields['id'] === 'string' || (typeof fields['id'] === 'number' && Number.isFinite(fields['id']))) &&
    (fields['actorIdentity'] === undefined || typeof fields['actorIdentity'] === 'string') &&
    (fields['decidedAt'] === undefined || typeof fields['decidedAt'] === 'string')
}
