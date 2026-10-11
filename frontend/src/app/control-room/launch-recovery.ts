const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i
const nilUuid = '00000000-0000-0000-0000-000000000000'

export function isConfirmedLaunchResult(result: unknown, automationId: string): boolean {
  return !!result && typeof result === 'object' &&
    'automationId' in result && result.automationId === automationId &&
    'launchEventId' in result && typeof result.launchEventId === 'string' &&
    uuidPattern.test(result.launchEventId) && result.launchEventId !== nilUuid &&
    'status' in result && (result.status === 'completed' || result.status === 'ready') &&
    'requiresApproval' in result && result.requiresApproval === false
}

export function launchRecoveryNotice(error: unknown, automationId: string): string | undefined {
  if (!error || typeof error !== 'object' || !('error' in error)) return undefined
  const body = error.error
  if (!body || typeof body !== 'object' || !('recovery' in body)) return undefined
  const recovery = body.recovery
  if (!recovery || typeof recovery !== 'object' ||
      !('automationId' in recovery) || recovery.automationId !== automationId ||
      !('launchEventId' in recovery) || typeof recovery.launchEventId !== 'string' ||
      !uuidPattern.test(recovery.launchEventId) || recovery.launchEventId === nilUuid ||
      !('reconciliationRequired' in recovery) || recovery.reconciliationRequired !== true ||
      !('retryAllowed' in recovery) || recovery.retryAllowed !== false) return undefined
  return `Request not completed. Recovery reference: ${recovery.launchEventId}. This reference is not proof of completion or permission to retry. Open diagnostics and reconcile the existing attempt before starting again.`
}
