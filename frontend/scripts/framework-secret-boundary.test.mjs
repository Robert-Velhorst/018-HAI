import assert from 'node:assert/strict'
import { test } from 'node:test'
import { firstValueFrom, of } from 'rxjs'
import { loadComponentLogic } from './load-component-logic.mjs'

const { FrameworkRegistryService } = await loadComponentLogic(new URL('../src/app/services/framework-registry.service.ts', import.meta.url))
const openSshPrivateKeyHeader = ['-----BEGIN OPENSSH ', 'PRIVATE ', 'KEY-----'].join('')
const rsaPrivateKeyHeader = ['-----BEGIN RSA ', 'PRIVATE ', 'KEY-----'].join('')
const rsaPrivateKeyFooter = ['-----END RSA ', 'PRIVATE ', 'KEY-----'].join('')

// Exercise the actual service with a synthetic HTTP boundary, not a live provider or browser.
test('registry inbound cookie and incomplete private-key values are redacted without mutating input', () => {
  const service = new FrameworkRegistryService({})
  const input = {
    name: 'Ordinary framework',
    'Set-Cookie': 'session=synthetic-cookie; csrf=synthetic-csrf',
    diagnostics: 'Cookie: session=synthetic-cookie; csrf=synthetic-csrf\nstatus=keep',
    source: `${openSshPrivateKeyHeader}\nsynthetic-private-body`,
    nested: [{ cookie: 'synthetic-cookie' }],
  }
  const original = structuredClone(input)
  const output = service.sanitizeInbound(input)
  assert.equal(output.name, input.name)
  assert.ok(output.diagnostics.includes('status=keep'))
  assert.ok(!JSON.stringify(output).includes('synthetic-'))
  assert.deepEqual(service.sanitizeInbound(output), output)
  assert.deepEqual(input, original)
})

test('completed private-key blocks retain trailing public context and public certificates remain intact', () => {
  const service = new FrameworkRegistryService({})
  const complete = `${rsaPrivateKeyHeader}\nsynthetic-private-body\n${rsaPrivateKeyFooter}\nstatus=keep`
  assert.equal(service.sanitizeInbound(complete), '[redacted]\nstatus=keep')
  const certificate = '-----BEGIN CERTIFICATE-----\npublic-body\n-----END CERTIFICATE-----'
  assert.equal(service.sanitizeInbound(certificate), certificate)
})

test('retained generic and escaped private-key cases complement RSA and OpenSSH fixtures', () => {
  const service = new FrameworkRegistryService({})
  for (const kind of ['', 'RSA ', 'OPENSSH ']) {
    const header = ['-----BEGIN ', kind, 'PRIVATE ', 'KEY-----'].join('')
    const footer = ['-----END ', kind, 'PRIVATE ', 'KEY-----'].join('')
    for (const separator of ['\n', '\\n']) {
      const complete = `public=before${separator}${header}${separator}synthetic-private-body${separator}${footer}${separator}status=keep`
      const input = { diagnostic: complete, incomplete: `${header}${separator}synthetic-private-body` }
      const original = structuredClone(input)
      const output = service.sanitizeInbound(input)
      assert.equal(output.diagnostic, `public=before${separator}[redacted]${separator}status=keep`)
      assert.equal(output.incomplete, '[redacted]')
      assert.doesNotMatch(JSON.stringify(output), /synthetic-private-body/)
      assert.deepEqual(service.sanitizeInbound(output), output)
      assert.deepEqual(input, original)
    }
  }
})

test('public framework read sanitizes inspector records and preserves operational contract fields', async () => {
  const record = {
    id: 'truth/evidence', version: '1', name: 'Truth', family: 'knowledge', purpose: 'Verify evidence',
    authorityRequirement: 'draft only', maximumAutonomyLevel: 2, riskCeiling: 'high',
    source: 'HAI specification', provenance: 'Set-Cookie: session=synthetic-cookie; csrf=synthetic-csrf',
    status: 'active', effectiveStatus: 'active', enabled: true, pinned: false, effectiveAutonomyLevel: 2,
  }
  for (const key of ['suitableProblemTypes', 'triggerConditions', 'requiredInputs', 'producedOutputs',
    'requiredAgents', 'workflowTemplate', 'decisionRules', 'safetyInvariants', 'evidenceRequirements',
    'evaluationMethod', 'conflictsWith', 'userSpecificAdaptations', 'adaptations']) record[key] = []
  const calls = []
  const service = new FrameworkRegistryService({ get: url => { calls.push(url); return of({ framework: record }) } })
  const output = await firstValueFrom(service.framework(record.id))
  assert.deepEqual(calls, ['/api/v1/framework-registry/frameworks/truth%2Fevidence'])
  assert.equal(output.enabled, true)
  assert.equal(output.effectiveAutonomyLevel, 2)
  assert.equal(output.provenance, 'Set-Cookie: [redacted]')
  assert.equal(record.provenance.includes('synthetic-cookie'), true)
})
