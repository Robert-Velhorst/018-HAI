import { operationRunVerified, readOperationRunResult } from './background-operations.model.interface'
import { readRuntimeAttempt, runtimeAttemptUncertain, runtimeAttemptVerified } from './runtime-lab.model.interface'
import { safeOutcomeText, safeReceiptSummary } from './safe-outcome.model.interface'

describe('Safe outcome display projections', () => {
  const receipt = {
    runtimeId: 'local-safe-worker', ok: true,
    output: { artifactHash: 'hash', boundedOutput: 'bounded evidence' }, verification: { passed: true },
  }
  const completed = {
    verified: true, failed: false, operation: { id: 'id', status: 'completed', verificationStatus: 'passed' },
  }
  const succeeded = {
    runtimeId: 'local-safe-worker', status: 'succeeded', operationId: 'id', operationStatus: 'completed', verificationPassed: true,
  }

  for (const supplied of [
    { ...receipt, ok: false }, { ...receipt, verification: { passed: false } },
    'invalid-receipt', [], {}, { ...receipt, output: null },
  ]) {
    it(`rejects verified UI and retry eligibility for contradictory/malformed supplied receipt (${JSON.stringify(supplied)})`, () => {
      const result = readOperationRunResult({ ...completed, receipt: supplied })!
      const attempt = readRuntimeAttempt({ ...succeeded, receipt: supplied })!
      expect(operationRunVerified(result)).toBeFalse()
      expect(runtimeAttemptVerified(attempt)).toBeFalse()
      expect(runtimeAttemptUncertain(attempt)).toBeTrue()
    })
  }

  it('keeps absent historical receipts compatible without inventing receipt-level verification', () => {
    for (const absent of [undefined, null]) {
      const result = readOperationRunResult({ ...completed, receipt: absent })!
      const attempt = readRuntimeAttempt({ ...succeeded, receipt: absent })!
      expect(operationRunVerified(result)).toBeTrue()
      expect(runtimeAttemptVerified(attempt)).toBeTrue()
      expect(result.receipt).toBeUndefined()
      expect(attempt.receipt).toBeUndefined()
    }
    expect(operationRunVerified(readOperationRunResult({ ...completed, receipt })!)).toBeTrue()
  })

  it('fences future/unknown effect-correlated statuses without converting discovery-only evidence into execution', () => {
    for (const status of ['unknown', 'indeterminate', 'future_result']) {
      expect(runtimeAttemptUncertain(readRuntimeAttempt({ ...succeeded, status, verificationPassed: false })!)).toBeTrue()
    }
    const discovery = readRuntimeAttempt({ runtimeId: 'openclaw', status: 'succeeded', discoveryRecovered: true, verificationPassed: false })!
    expect(runtimeAttemptUncertain(discovery)).toBeFalse()
    expect(runtimeAttemptVerified(discovery)).toBeFalse()
    expect(runtimeAttemptUncertain(readRuntimeAttempt({ runtimeId: 'openclaw', status: 'inconclusive', verificationPassed: false })!)).toBeFalse()
    expect(runtimeAttemptUncertain(readRuntimeAttempt({
      ...succeeded, status: 'failed', operationStatus: 'failed', verificationPassed: false,
      receipt: { ...receipt, ok: false, output: { artifactHash: '', boundedOutput: '' }, verification: { passed: false } },
    })!)).toBeFalse()
  })

  it('keeps correlation and minimal/null snapshots without retaining arbitrary payload fields', () => {
    const result = readOperationRunResult({
      operationId: 'operation-id', operationSnapshot: true,
      operation: { id: 'operation-id', status: 'running', verificationStatus: 'pending', worldModelStateJSON: 'private-payload' },
      verified: false, failed: false, reconciliationRequired: true, outcomeRecorded: false,
      receipt: {
        output: { artifactPath: 'private/path', artifactHash: 'observed-hash', boundedOutput: 'api_key=synthetic-secret' },
        verification: { passed: true }, extra: 'private-extra',
      },
    })!
    expect(result.operationId).toBe('operation-id')
    expect(result.operationSnapshot).toBeTrue()
    expect(result.operation?.status).toBe('running')
    expect(result.operation as unknown as Record<string, unknown>).not.toEqual(jasmine.objectContaining({ worldModelStateJSON: jasmine.anything() }))
    expect(operationRunVerified(result)).toBeFalse()
    const summary = safeReceiptSummary(result)
    expect(summary).toContain('observed-hash')
    expect(summary).toContain('[redacted]')
    expect(summary).not.toContain('private/path')
    expect(summary).not.toContain('synthetic-secret')
    expect(readOperationRunResult({ verified: false, operation: null })?.operation).toBeNull()
    expect(readOperationRunResult({ error: 'Pre-effect policy refusal.' })).toBeUndefined()
  })

  it('bounds and redacts text without allowing receipt-level verification to claim operation completion', () => {
    expect(safeOutcomeText(`Authorization: Bearer synthetic-credential ${'x'.repeat(5000)}`).length).toBeLessThanOrEqual(512)
    expect(safeOutcomeText('password="synthetic-secret"')).toBe('password=[redacted]')
    const result = readOperationRunResult({
      verified: true, failed: false, operation: { id: 'id', status: 'verifying', verificationStatus: 'passed' },
      receipt: { ok: true, verification: { passed: true }, output: { artifactHash: 'x'.repeat(5000), boundedOutput: 'y'.repeat(5000) } },
    })!
    expect(operationRunVerified(result)).toBeFalse()
    expect(safeReceiptSummary(result).length).toBeLessThan(800)
  })

  it('fails closed on malformed safety flags and preserves absent optional fields when projected twice', () => {
    const malformed = readOperationRunResult({
      verified: true, operation: { id: 'id', status: 'completed', verificationStatus: 'passed' },
      reconciliationRequired: 'false',
    })!
    expect(operationRunVerified(malformed)).toBeFalse()
    expect(malformed.reconciliationRequired).toBeTrue()
    const legacy = readRuntimeAttempt({ runtimeId: 'local-safe-worker', status: 'succeeded', verificationPassed: true, operationStatus: 'completed' })!
    expect(runtimeAttemptVerified(readRuntimeAttempt(legacy)!)).toBeTrue()
    expect(readRuntimeAttempt(null)).toBeUndefined()
  })

  it('retains bounded worker progress without turning consumed authority into completion', () => {
    const progress = {
      authorization: 'consumed' as const, effectStarted: true, artifactCreated: false,
      bytesWritten: 0, writeComplete: false, syncComplete: false, readBytes: 0,
      readComplete: false, identityVerified: false, fileClosed: false,
    }
    const result = readOperationRunResult({ ...completed, receipt: {
      ...receipt, output: { ...receipt.output, progress },
    } })!
    expect(operationRunVerified(result)).toBeFalse()
    expect(safeReceiptSummary(result)).toContain('authorization consumed')
    expect(safeReceiptSummary(result)).toContain('0 bytes written')
    expect(readOperationRunResult(result)?.receipt?.output?.progress).toEqual(progress)
    const successful = { ...progress, artifactCreated: true, bytesWritten: 4,
      writeComplete: true, syncComplete: true, readBytes: 4, readComplete: true,
      identityVerified: true, fileClosed: true }
    expect(operationRunVerified(readOperationRunResult({ ...completed, receipt: {
      ...receipt, output: { ...receipt.output, progress: successful },
    } })!)).toBeTrue()
  })

  for (const progress of [null, [], {}, { authorization: 'token=private' },
    { bytesWritten: -1 }, { bytesWritten: 65537 }, { effectStarted: 'false' }]) {
    it(`fences malformed progress instead of dropping it (${JSON.stringify(progress)})`, () => {
      const attempt = readRuntimeAttempt({ ...succeeded, receipt: {
        ...receipt, output: { ...receipt.output, progress },
      } })!
      expect(runtimeAttemptVerified(attempt)).toBeFalse()
      expect(runtimeAttemptUncertain(attempt)).toBeTrue()
      expect(safeReceiptSummary(attempt)).not.toContain('private')
    })
  }
})
