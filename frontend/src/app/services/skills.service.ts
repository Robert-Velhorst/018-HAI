import { HttpClient } from '@angular/common/http'
import { Injectable } from '@angular/core'
import { Observable, map, throwError } from 'rxjs'
import { ISkillGuidancePreview, ISkillSelectionInventory, ISkillSelectionState } from '../models/skills.model.interface'

const sha256Pattern = /^[a-f0-9]{64}$/
const sourceCommitPattern = /^[a-f0-9]{40}$/
const skillIdPattern = /^[a-z0-9]+(?:-[a-z0-9]+)*$/
const sourceRepository = 'anthropics/skills'

@Injectable({ providedIn: 'root' })
export class SkillsService {
  constructor(private http: HttpClient) {}

  inventory(): Observable<ISkillSelectionInventory> {
    return this.http.get<unknown>('/api/v1/brain-skills/').pipe(
      map((response) => parseSkillInventory(response)),
    )
  }

  guidancePreview(id: string): Observable<ISkillGuidancePreview> {
    return this.http.get<ISkillGuidancePreview>(
      `/api/v1/brain-skills/${encodeURIComponent(id)}/guidance-preview`,
    )
  }

  setEnabled(id: string, enabled: boolean, reviewedGuidanceSHA256?: string, reviewedCatalogFingerprint?: string): Observable<ISkillSelectionState> {
    if (enabled && (!reviewedGuidanceSHA256 || !sha256Pattern.test(reviewedGuidanceSHA256) ||
      !reviewedCatalogFingerprint || !sha256Pattern.test(reviewedCatalogFingerprint))) {
      return throwError(() => new Error('Enabling a skill requires the reviewed guidance hash and current catalog fingerprint.'))
    }

    return this.http.put<ISkillSelectionState>(
      `/api/v1/brain-skills/${encodeURIComponent(id)}/selection`,
      enabled
        ? { enabled, reviewedGuidanceSHA256, reviewedCatalogFingerprint }
        : { enabled },
    )
  }
}

function parseSkillInventory(value: unknown): ISkillSelectionInventory {
  if (!isRecord(value) ||
    value['sourceRepository'] !== sourceRepository ||
    typeof value['sourceCommit'] !== 'string' || !sourceCommitPattern.test(value['sourceCommit']) ||
    typeof value['sourceCommitDate'] !== 'string' || !Number.isFinite(Date.parse(value['sourceCommitDate'])) ||
    typeof value['catalogFingerprint'] !== 'string' || !sha256Pattern.test(value['catalogFingerprint']) ||
    !Array.isArray(value['skills']) ||
    !value['skills'].every(isSkillInventoryEntry)) {
    throw new Error('Skills inventory response did not match the expected schema.')
  }
  const skillIds = value['skills'].map((skill) => skill.id)
  if (new Set(skillIds).size !== skillIds.length) {
    throw new Error('Skills inventory response did not match the expected schema.')
  }
  return value as unknown as ISkillSelectionInventory
}

function isSkillInventoryEntry(value: unknown): boolean {
  if (!isRecord(value)) return false

  const requiredStrings = [
    'id', 'name', 'description', 'license', 'sourcePath', 'sourceURL',
    'sourceSHA256', 'guidanceSHA256', 'scope', 'status',
  ]
  if (!requiredStrings.every((key) => typeof value[key] === 'string' && value[key].trim().length > 0) ||
    typeof value['enabled'] !== 'boolean' ||
    typeof value['needsReapproval'] !== 'boolean') return false
  if (!skillIdPattern.test(value['id'] as string) ||
    !sha256Pattern.test(value['sourceSHA256'] as string) ||
    !sha256Pattern.test(value['guidanceSHA256'] as string)) return false

  const optionalStrings = ['licenseURL', 'licensePath', 'licenseSHA256']
  if (!optionalStrings.every((key) => value[key] === undefined || typeof value[key] === 'string')) return false
  const licenseSHA256 = value['licenseSHA256'] as string | undefined
  if (licenseSHA256 && !sha256Pattern.test(licenseSHA256)) return false

  const decision = value['selectionDecision']
  if (decision === undefined || decision === null) return true
  if (!isRecord(decision)) return false
  return (decision['id'] === undefined || typeof decision['id'] === 'string' || (typeof decision['id'] === 'number' && Number.isFinite(decision['id']))) &&
    (decision['actorIdentity'] === undefined || typeof decision['actorIdentity'] === 'string') &&
    (decision['decidedAt'] === undefined || typeof decision['decidedAt'] === 'string')
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
