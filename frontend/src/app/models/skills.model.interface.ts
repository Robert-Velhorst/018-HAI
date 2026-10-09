export const MAX_SKILL_GUIDANCE_PREVIEW_BYTES = 500

export interface ISkillInventoryEntry {
  id: string
  name: string
  description: string
  license: string
  licenseURL?: string
  licensePath?: string
  licenseSHA256?: string
  sourcePath: string
  sourceURL: string
  sourceSHA256: string
  guidanceSHA256: string
  scope: string
  status: string
  enabled: boolean
  needsReapproval: boolean
  selectionDecision?: ISkillSelectionDecision | null
}

export interface ISkillSelectionDecision {
  id?: number | string
  actorIdentity?: string
  decidedAt?: string
}

export interface ISkillInventory {
  sourceRepository: string
  sourceCommit: string
  sourceCommitDate: string
  catalogFingerprint?: string
  skills: ISkillInventoryEntry[]
}

export interface ISkillSelectionInventory extends ISkillInventory {
  catalogFingerprint: string
}

export interface ISkillSelectionState {
  enabled: boolean
  needsReapproval: boolean
  selectionDecision?: ISkillSelectionDecision | null
  supersededByConcurrentDecision?: boolean
}

export interface ISkillGuidancePreview {
  skillId: string
  currentGuidance: string
  currentGuidanceSHA256: string
  catalogFingerprint: string
  previousApprovedGuidanceSHA256?: string
  previousGuidanceTextStored: false
  previousGuidanceTextLimitation?: string
}
