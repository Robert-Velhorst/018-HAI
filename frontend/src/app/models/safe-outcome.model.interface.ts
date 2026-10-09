export interface ISafeWorkerProgress {
  authorization?: '' | 'consumed' | 'unknown'
  effectStarted: boolean
  artifactCreated: boolean
  bytesWritten: number
  writeComplete: boolean
  syncComplete: boolean
  readBytes: number
  readComplete: boolean
  identityVerified: boolean
  fileClosed: boolean
}

export interface ISafeExecutionReceipt {
  runtimeId?: string
  ok?: boolean
  output?: { artifactHash?: string; boundedOutput?: string; progress?: ISafeWorkerProgress }
  verification?: { passed?: boolean }
}

function readSafeWorkerProgress(value: unknown): ISafeWorkerProgress | undefined {
  const progress = outcomeRecord(value)
  if (!progress || (progress['authorization'] !== undefined &&
    !['', 'consumed', 'unknown'].includes(progress['authorization'] as string))) return undefined
  const flags = ['effectStarted', 'artifactCreated', 'writeComplete', 'syncComplete',
    'readComplete', 'identityVerified', 'fileClosed'] as const
  if (flags.some((flag) => typeof progress[flag] !== 'boolean') ||
    ['bytesWritten', 'readBytes'].some((key) => !Number.isSafeInteger(progress[key]) ||
      (progress[key] as number) < 0 || (progress[key] as number) > 65536)) return undefined
  return {
    authorization: progress['authorization'] as ISafeWorkerProgress['authorization'],
    effectStarted: progress['effectStarted'] as boolean,
    artifactCreated: progress['artifactCreated'] as boolean,
    bytesWritten: progress['bytesWritten'] as number,
    writeComplete: progress['writeComplete'] as boolean,
    syncComplete: progress['syncComplete'] as boolean,
    readBytes: progress['readBytes'] as number,
    readComplete: progress['readComplete'] as boolean,
    identityVerified: progress['identityVerified'] as boolean,
    fileClosed: progress['fileClosed'] as boolean,
  }
}

export interface ISafeOutcomeState {
  receipt?: ISafeExecutionReceipt
  interrupted?: boolean
  reconciliationRequired?: boolean
  outcomeRecorded?: boolean
}

export function outcomeRecord(value: unknown): Record<string, unknown> | undefined {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
    ? value as Record<string, unknown> : undefined
}

export function safeReceiptMalformed(value: unknown): boolean {
  if (value === undefined || value === null) return false
  const receipt = outcomeRecord(value)
  const output = outcomeRecord(receipt?.['output'])
  const verification = outcomeRecord(receipt?.['verification'])
  return !receipt || typeof receipt['runtimeId'] !== 'string' || typeof receipt['ok'] !== 'boolean' ||
    !output || typeof output['artifactHash'] !== 'string' || typeof output['boundedOutput'] !== 'string' ||
    !verification || typeof verification['passed'] !== 'boolean' ||
    (output['progress'] !== undefined && !readSafeWorkerProgress(output['progress']))
}

export function receiptConsistentWithCompletion(receipt?: ISafeExecutionReceipt): boolean {
  const progress = receipt?.output?.progress
  const progressComplete = !progress || (progress.authorization === 'consumed' && progress.effectStarted &&
    progress.artifactCreated && progress.writeComplete && progress.syncComplete && progress.readComplete &&
    progress.identityVerified && progress.fileClosed && progress.bytesWritten > 0 &&
    progress.bytesWritten === progress.readBytes)
  return !receipt || (!safeReceiptMalformed(receipt) && !!receipt.runtimeId && receipt.ok === true &&
    receipt.verification?.passed === true && !!receipt.output?.artifactHash && progressComplete)
}

export function safeOutcomeText(value: unknown, limit = 512): string {
  if (typeof value !== 'string') return ''
  const redacted = value
    .replace(/\b(authorization)\s*[:=]\s*(?:Bearer|Basic)\s+[^\s,;]+/gi, '$1=[redacted]')
    .replace(/\bBearer\s+[A-Za-z0-9._~+\/-]+=*/gi, 'Bearer [redacted]')
    .replace(/\b(authorization|api[_-]?key|access[_-]?token|refresh[_-]?token|client[_-]?secret|password|token)\s*[:=]\s*("[^"]*"|'[^']*'|[^\s,;]+)/gi, '$1=[redacted]')
    .replace(/[\u0000-\u001f\u007f]/g, ' ')
  return redacted.length > limit ? `${redacted.slice(0, limit - 3)}...` : redacted
}

export function readSafeOutcomeState(value: Record<string, unknown>): ISafeOutcomeState {
  const receipt = outcomeRecord(value['receipt'])
  const output = outcomeRecord(receipt?.['output'])
  const verification = outcomeRecord(receipt?.['verification'])
  const malformedReceipt = safeReceiptMalformed(value['receipt'])
  const malformedState = ['interrupted', 'reconciliationRequired', 'outcomeRecorded'].some((key) =>
    value[key] !== undefined && typeof value[key] !== 'boolean'
  )
  return {
    interrupted: typeof value['interrupted'] === 'boolean' ? value['interrupted'] : undefined,
    reconciliationRequired: malformedState || malformedReceipt || value['reconciliationRequired'] === true ? true :
      typeof value['reconciliationRequired'] === 'boolean' ? false : undefined,
    outcomeRecorded: typeof value['outcomeRecorded'] === 'boolean' ? value['outcomeRecorded'] : undefined,
    receipt: receipt ? {
      runtimeId: safeOutcomeText(receipt['runtimeId'], 128),
      ok: typeof receipt['ok'] === 'boolean' ? receipt['ok'] : undefined,
      output: output ? {
        artifactHash: safeOutcomeText(output['artifactHash'], 128),
        boundedOutput: safeOutcomeText(output['boundedOutput']),
        progress: readSafeWorkerProgress(output['progress']),
      } : undefined,
      verification: verification ? { passed: typeof verification['passed'] === 'boolean' ? verification['passed'] : undefined } : undefined,
    } : undefined,
  }
}

export function safeReceiptSummary(state: ISafeOutcomeState): string {
  if (!state.receipt) return ''
  const parts = ['Receipt retained; artifact evidence is not operation completion.']
  const hash = safeOutcomeText(state.receipt.output?.artifactHash, 128)
  const output = safeOutcomeText(state.receipt.output?.boundedOutput)
  if (hash) parts.push(`Artifact hash: ${hash}.`)
  if (output) parts.push(`Output: ${output}`)
  const progress = state.receipt.output?.progress
  if (progress?.authorization === 'consumed') parts.push('Execution authorization consumed; do not retry without reconciliation.')
  if (progress?.authorization === 'unknown') parts.push('Execution authorization consumption is uncertain; review is required.')
  if (progress?.effectStarted) parts.push(`Filesystem effect entered; ${progress.bytesWritten} bytes written and ${progress.readBytes} bytes read.`)
  return parts.join(' ')
}
