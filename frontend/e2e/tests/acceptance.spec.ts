import { expect, Page, test } from '@playwright/test';
import { assertIsolatedAcceptanceTarget } from './support/isolated-stack';

/**
 * Real operator acceptance path:
 *
 * login -> local source registration -> bounded sync -> governed workflow
 * intake -> exact runtime selection -> one bounded, read-only workflow run.
 *
 * The source is the owner-scoped, read-only connected-sources mount. This test
 * never authorizes an external provider or requests an irreversible action.
 */

const email = process.env.E2E_OPERATOR_EMAIL || 'operator@example.com';
const password = process.env.E2E_OPERATOR_PASSWORD || '';
const allowMutation = process.env.E2E_ALLOW_MUTATION === 'true';

async function login(page: Page) {
  await page.goto('/');
  await page.getByTestId('login-submit').waitFor({ state: 'visible' });
  await page.getByTestId('login-email').fill(email);
  await page.getByTestId('login-password').fill(password);
  await page.getByTestId('login-submit').click();
  await expect(page).toHaveURL(/\/(?:control-center|onboarding)(?:\?|$)/);
  if (new URL(page.url()).pathname === '/onboarding') {
    await expect(page.getByRole('heading', { name: 'Getting started with HAI', exact: true })).toBeVisible();
    await page.getByRole('button', { name: 'Skip', exact: true }).click();
  }
  await expect(page).toHaveURL(/\/control-center(?:\?|$)/);
  await expect(page.getByRole('heading', { name: 'Command Center', exact: true }).first()).toBeVisible();
}

test.describe('HAI operator acceptance flow', () => {
  test.skip(
    !password || !allowMutation,
    'Set E2E_OPERATOR_PASSWORD and E2E_ALLOW_MUTATION=true only for an isolated acceptance stack.',
  );

  test.beforeAll(async ({ baseURL }) => {
    assertIsolatedAcceptanceTarget(baseURL, process.env.E2E_ISOLATED_STACK, email);
  });

  test('login -> source -> sync -> workflow -> exact bounded execution', async ({ page }) => {
    test.setTimeout(180_000);
    let sourceName = '';
    let sourceId = '';
    const runId = Date.now();
    const pursuitName = `E2E governed pursuit ${runId}`;
    const projectKey = `e2e-governed-${runId}`;
    const capabilityMarker = `probe-${runId}`;

    await test.step('login', async () => {
      await login(page);
    });

    await test.step('register a unique read-only execution capability', async () => {
      const response = await page.request.post('/api/v1/automation/', {
        multipart: {
          name: `E2E backend readiness ${capabilityMarker}`,
          host: 'backend',
          port: '80',
          position: '0',
          removeImage: 'false',
          launchType: 'api',
		  launchTarget: 'GET http://backend/readyz',
          dependencyNotes: `Read-only HAI backend readiness check for ${capabilityMarker}.`,
          healthCheckType: 'http',
		  healthCheckUrl: 'http://backend/readyz',
          expectedHttpStatus: '200',
        },
      });
      expect(response.status(), await response.text()).toBe(201);
      const automation = await response.json();
      expect(automation.id).toBeTruthy();
    });

    await test.step('connect a local, read-only source', async () => {
      await page.goto('/connected-sources');
      await page.getByTestId('configure-source-basic').click();
      await expect(page.getByRole('button', { name: 'Advanced view for Sources', exact: true }))
        .toHaveAttribute('aria-pressed', 'true');
      await expect(page.getByTestId('source-connect-form')).toBeVisible();
      sourceName = `E2E local source ${Date.now()}`;
      await page.getByTestId('source-name').fill(sourceName);
      await page.getByTestId('source-target').fill('.');
      await expect(page.getByTestId('source-connect')).toBeEnabled();
      const createResponse = page.waitForResponse(
        (response) => response.url().includes('/api/v1/sources/')
          && response.request().method() === 'POST'
      );
      await page.getByTestId('source-connect').click();
      const createdSource = await (await createResponse).json();
      expect(createdSource.name).toBe(sourceName);
      sourceId = createdSource.id;
      expect(sourceId).toMatch(/^[0-9a-f-]{36}$/i);
      const sourceListResponse = await page.request.get('/api/v1/sources/?includeDisabled=true');
      expect(sourceListResponse.status()).toBe(200);
      const listedSources = await sourceListResponse.json();
      expect(listedSources.map((source: { name: string }) => source.name)).toContain(sourceName);
      const sourceRow = page.getByTestId('source-row').filter({ hasText: sourceName });
      await expect(sourceRow).toBeVisible();
      const selectSource = sourceRow.getByRole('button', { name: new RegExp(`^Select ${sourceName},`) });
      await selectSource.click();
      await expect(selectSource).toHaveAttribute('aria-pressed', 'true');
      await expect(page.getByText(sourceName, { exact: true }).last()).toBeVisible();
    });

    await test.step('run a bounded source sync', async () => {
      const sourceRow = page.getByTestId('source-row').filter({ hasText: sourceName });
      const submittedResponse = page.waitForResponse((response) =>
        new URL(response.url()).pathname === `/api/v1/sources/${sourceId}/sync-jobs`
        && response.request().method() === 'POST');
      await sourceRow.getByTestId('source-sync').click();
      const submitted = await submittedResponse;
      expect(submitted.status(), await submitted.text()).toBe(202);
      const job = await submitted.json();
      expect(job.sourceId).toBe(sourceId);
      expect(job.mode).toBe('manual_async_sync');
      expect(job.id).toMatch(/^[0-9a-f-]{36}$/i);

      // Queue acceptance is not evidence of ingestion. Inspect the persisted
      // job and the exact fixture's owner-scoped extraction before proceeding.
      await expect.poll(async () => {
        const response = await page.request.get(`/api/v1/sources/sync-jobs/${job.id}`);
        expect(response.status()).toBe(200);
        const current = await response.json();
        expect(current.id).toBe(job.id);
        expect(current.sourceId).toBe(sourceId);
        return current.status;
      }, { timeout: 120_000, intervals: [1000, 2000, 5000] }).toBe('completed');

      const extractionResponse = await page.request.get('/api/v1/sources/extractions?limit=100');
      expect(extractionResponse.status()).toBe(200);
      const extractions = await extractionResponse.json() as Array<{ sourceId: string; text: string; sourceUri?: string }>;
      const fixture = extractions.find((item) => item.sourceId === sourceId
        && item.sourceUri?.endsWith('/acceptance.txt'));
      expect(fixture?.text).toContain('Synthetic HAI acceptance source. No personal records.');
    });

    await test.step('create an explicit pursuit for governed work', async () => {
      await page.goto('/pursuits');
      await page.getByTestId('pursuit-new').click();
      await expect(page.getByTestId('pursuit-create-form')).toBeVisible();
      await page.getByTestId('pursuit-title').fill(pursuitName);
      await page.getByTestId('pursuit-project-key').fill(projectKey);
      await page.getByTestId('pursuit-create').click();
      await expect(page.getByTestId('pursuit-row').filter({ hasText: pursuitName })).toBeVisible();
    });

    await test.step('create a low-risk workflow for the read-only probe', async () => {
      await page.goto('/workflow-engine');
      await page.locator('#workflow-intake-input').fill(
        `Run the selected HAI backend readiness ${capabilityMarker} for ${pursuitName} (${projectKey}) and record its read-only verification result. Do not send anything externally.`
      );
      await page.getByTestId('workflow-project-key').fill(projectKey);
      const matchResponse = page.waitForResponse(
        (response) => response.url().includes('/api/v1/pursuits/match')
          && response.request().method() === 'POST'
      );
      await page.getByTestId('workflow-match-pursuit').click();
      const matches = await (await matchResponse).json() as Array<{ pursuit?: { title?: string } }>;
      expect(matches.some((match) => match.pursuit?.title === pursuitName)).toBeTruthy();
      const pursuitMatch = page.locator('.pursuit-match-card').filter({ hasText: pursuitName });
      await expect(pursuitMatch).toBeVisible();
      await pursuitMatch.click();
      await page.getByTestId('workflow-create').click();
      const inspectIntake = page.getByRole('button', { name: 'Inspect intake workflow', exact: true });
      await expect(inspectIntake).toBeVisible();
      await inspectIntake.click();
      await expect(page.getByTestId('workflow-runtime-selection')).toBeVisible();
    });

    await test.step('select the exact runtime through the durable boundary', async () => {
      const runtimeSelection = page.getByTestId('workflow-runtime-selection');
      const exactRuntime = runtimeSelection.getByRole('button', {
        name: `E2E backend readiness ${capabilityMarker}`,
        exact: true,
      });
      await expect(exactRuntime).toBeVisible();
      const proposalResponse = page.waitForResponse((response) =>
        response.request().method() === 'POST'
        && /\/api\/v1\/workflow\/[^/]+\/proposals\/[^/]+\/resolve$/.test(new URL(response.url()).pathname)
      );
      await exactRuntime.click();
      const response = await proposalResponse;
      expect(response.ok(), await response.text()).toBeTruthy();
      await expect(page.getByTestId('workflow-approval-controls')).toHaveCount(0);
      await expect(page.getByTestId('workflow-runtime-selection')).toHaveCount(0);
      await expect(page.getByTestId('workflow-selected-state')).toHaveText('ready');
    });

    await test.step('require action-bound approval before executing the selected API workflow', async () => {
      const exactRun = page.getByTestId('workflow-run-selected');
      await expect(exactRun).toBeVisible();
      await exactRun.click();
      const confirmation = page.getByRole('dialog');
      await expect(confirmation.getByText('Run this selected workflow?')).toBeVisible();
      const runResponse = page.waitForResponse((response) =>
        response.request().method() === 'POST'
        && /\/api\/v1\/workflow\/[^/]+\/run$/.test(new URL(response.url()).pathname)
      );
      await confirmation.getByRole('button', { name: 'Run this workflow', exact: true }).click();
      const response = await runResponse;
      expect(response.ok(), await response.text()).toBeTruthy();
      const result = await response.json();
      const resultContext = JSON.stringify(result, null, 2);
      expect(result.status, resultContext).toBe('blocked');
      expect(result.state, resultContext).toBe('needs_approval');
      expect(result.attempts, resultContext).toBe(0);
      expect(result.message).toContain('action-bound approval proof is required');
      await expect(page.getByTestId('workflow-selected-state')).toHaveText('needs approval');
      await expect(page.getByTestId('workflow-approval-controls')).toBeVisible();
      const approvalResponse = page.waitForResponse((response) =>
        response.request().method() === 'POST'
        && new URL(response.url()).pathname === `/api/v1/workflow/${result.workflowId}/approval`);
      await page.getByTestId('workflow-approve').click();
      const approved = await approvalResponse;
      expect(approved.ok(), await approved.text()).toBeTruthy();
      const approvedRecord = await approved.json();
      expect(approvedRecord.item.id).toBe(result.workflowId);
      expect(approvedRecord.item.approvalStatus).toBe('approved');
      await expect(page.getByTestId('workflow-approval-controls')).toHaveCount(0);
      await expect(page.getByTestId('workflow-selected-state')).toHaveText('ready');
    });

    await test.step('run only the selected owner-approved workflow', async () => {
      await page.getByTestId('workflow-run-selected').click();
      const confirmation = page.getByRole('dialog');
      await expect(confirmation.getByText('Run this selected workflow?')).toBeVisible();
      const runResponse = page.waitForResponse((response) =>
        response.request().method() === 'POST'
        && /\/api\/v1\/workflow\/[^/]+\/run$/.test(new URL(response.url()).pathname));
      await confirmation.getByRole('button', { name: 'Run this workflow', exact: true }).click();
      const response = await runResponse;
      expect(response.ok(), await response.text()).toBeTruthy();
      const result = await response.json();
      const resultContext = JSON.stringify(result, null, 2);
      expect(result.status, resultContext).toBe('completed');
      expect(result.state, resultContext).toBe('completed');
      await expect(page.getByText(/workflow completed/i).first()).toBeVisible();
      await expect(page.getByText(/last operation/i).first()).toBeVisible();
      await expect(page.getByTestId('workflow-selected-state')).toHaveText('completed');
      await expect(page.getByTestId('workflow-selected-verification')).not.toHaveText('-');
    });
  });
});
