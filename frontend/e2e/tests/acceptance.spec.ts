import { expect, Page, test } from '@playwright/test';
import type { APIResponse } from '@playwright/test';

/**
 * Real operator acceptance path:
 *
 * login -> local source registration -> bounded sync -> governed workflow
 * intake -> durable human approval -> one exact controlled workflow run.
 *
 * The source is the owner-scoped, read-only connected-sources mount. This test
 * never authorizes an external provider or requests an irreversible action.
 */

const email = process.env.E2E_OPERATOR_EMAIL || 'operator@example.com';
const password = process.env.E2E_OPERATOR_PASSWORD || '';
const allowMutation = process.env.E2E_ALLOW_MUTATION === 'true';

interface AcceptanceArtifacts {
  automationId?: string;
  sourceId?: string;
  pursuitId?: string;
  workflowId?: string;
}

async function requireResponse(response: APIResponse, label: string, expectedStatuses: number[]): Promise<void> {
  if (expectedStatuses.includes(response.status())) {
    return;
  }
  throw new Error(`${label} failed with HTTP ${response.status()}: ${await response.text()}`);
}

async function transitionWorkflow(page: Page, workflowId: string, targetState: string, message: string): Promise<void> {
  const response = await page.request.post(`/api/v1/workflow/${workflowId}/transition`, {
    data: { targetState, message },
  });
  await requireResponse(response, `workflow transition to ${targetState}`, [200]);
}

async function cleanupWorkflow(page: Page, workflowId: string): Promise<void> {
  const lookup = await page.request.get(`/api/v1/workflow/${workflowId}`);
  if (lookup.status() === 404) {
    return;
  }
  await requireResponse(lookup, 'workflow cleanup lookup', [200]);
  const record = await lookup.json();
  let state = String(record?.item?.currentState || record?.item?.state || '');

  if (state === 'archived') {
    return;
  }
  if (state === 'needs_approval') {
    const rejection = await page.request.post(`/api/v1/workflow/${workflowId}/approval`, {
      data: {
        approved: false,
        note: 'Acceptance-test cleanup: reject unfinished test work before archival.',
      },
    });
    await requireResponse(rejection, 'workflow cleanup rejection', [200]);
    state = 'blocked';
  }
  if (state !== 'blocked' && state !== 'completed') {
    await transitionWorkflow(
      page,
      workflowId,
      'blocked',
      'Acceptance-test cleanup: stop unfinished test work before archival.'
    );
  }
  await transitionWorkflow(
    page,
    workflowId,
    'archived',
    'Acceptance-test cleanup: archive the disposable test workflow.'
  );
}

async function cleanupAcceptanceArtifacts(page: Page, artifacts: AcceptanceArtifacts): Promise<void> {
  const failures: string[] = [];
  const cleanup = async (label: string, action: () => Promise<void>) => {
    try {
      await action();
    } catch (error) {
      failures.push(`${label}: ${error instanceof Error ? error.message : String(error)}`);
    }
  };

  if (artifacts.workflowId) {
    await cleanup('workflow', () => cleanupWorkflow(page, artifacts.workflowId!));
  }
  if (artifacts.pursuitId) {
    await cleanup('pursuit', async () => {
      const response = await page.request.post(`/api/v1/pursuits/${artifacts.pursuitId}/archive`, {
        data: { archived: true },
      });
      await requireResponse(response, 'pursuit cleanup archive', [200]);
    });
  }
  if (artifacts.sourceId) {
    await cleanup('source', async () => {
      const response = await page.request.post(`/api/v1/sources/${artifacts.sourceId}/pause`, { data: {} });
      await requireResponse(response, 'source cleanup pause', [200]);
    });
  }
  if (artifacts.automationId) {
    await cleanup('automation', async () => {
      const response = await page.request.delete(`/api/v1/automation/${artifacts.automationId}`);
      await requireResponse(response, 'automation cleanup delete', [204, 404]);
    });
  }

  if (failures.length) {
    throw new Error(`Acceptance-test cleanup failed:\n${failures.join('\n')}`);
  }
}

async function login(page: Page) {
  await page.goto('/');
  await page.getByTestId('login-submit').waitFor({ state: 'visible' });
  await page.getByTestId('login-email').fill(email);
  await page.getByTestId('login-password').fill(password);
  await page.getByTestId('login-submit').click();
  await expect(page).toHaveURL(/\/control-center(?:\?|$)/);
  await expect(page.getByRole('heading', { name: 'Command Center', exact: true }).first()).toBeVisible();
}

test.describe('HAI operator acceptance flow', () => {
  let artifacts: AcceptanceArtifacts;

  test.skip(!password, 'Set E2E_OPERATOR_PASSWORD and a seeded account to run against a live stack.');
  test.skip(!allowMutation, 'Set E2E_ALLOW_MUTATION=true to acknowledge that this suite creates temporary records.');

  test.beforeEach(() => {
    artifacts = {};
  });

  // Hooks receive their own Playwright timeout. Cleanup therefore still runs
  // when the main UI flow times out or an assertion fails near its deadline.
  test.afterEach(async ({ page }) => {
    await cleanupAcceptanceArtifacts(page, artifacts);
  });

  test('login -> source -> sync -> workflow -> approval -> exact bounded execution', async ({ page }) => {
    let sourceName = '';
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
      artifacts.automationId = automation.id;
    });

    await test.step('connect a local, read-only source', async () => {
      await page.goto('/connected-sources');
      await expect(page.getByTestId('source-connect-form')).toBeVisible();
      sourceName = `E2E local source ${Date.now()}`;
      await page.getByTestId('source-name').fill(sourceName);
      await page.getByTestId('source-target').fill('e2e-fixture');
      const createResponse = page.waitForResponse((response) =>
        response.request().method() === 'POST'
        && new URL(response.url()).pathname === '/api/v1/sources/'
      );
      await page.getByTestId('source-connect').click();
      const response = await createResponse;
      await requireResponse(response, 'source registration', [201]);
      const source = await response.json();
      expect(source.id).toBeTruthy();
      artifacts.sourceId = source.id;

      const sourceRow = page.getByTestId('source-row').filter({ hasText: sourceName });
      await expect(sourceRow).toBeVisible();
      await sourceRow.click();
      await expect(page.getByText(sourceName, { exact: true }).last()).toBeVisible();
    });

    await test.step('run a bounded source sync', async () => {
      const sourceRow = page.getByTestId('source-row').filter({ hasText: sourceName });
      await sourceRow.getByTestId('source-sync').click();
      await expect(page.getByText(/source sync/i).first()).toBeVisible();
    });

    await test.step('create an explicit pursuit for governed work', async () => {
      await page.goto('/pursuits');
      await page.getByTestId('pursuit-new').click();
      await expect(page.getByTestId('pursuit-create-form')).toBeVisible();
      await page.getByTestId('pursuit-title').fill(pursuitName);
      await page.getByTestId('pursuit-project-key').fill(projectKey);
      const createResponse = page.waitForResponse((response) =>
        response.request().method() === 'POST'
        && new URL(response.url()).pathname === '/api/v1/pursuits/'
      );
      await page.getByTestId('pursuit-create').click();
      const response = await createResponse;
      await requireResponse(response, 'pursuit creation', [201]);
      const pursuit = await response.json();
      expect(pursuit.id).toBeTruthy();
      artifacts.pursuitId = pursuit.id;
      await expect(page.getByTestId('pursuit-row').filter({ hasText: pursuitName })).toBeVisible();
    });

    await test.step('create an explicitly approval-gated workflow', async () => {
      await page.goto('/workflow-engine');
      await page.locator('#workflow-intake-input').fill(
        `Run the HAI backend readiness ${capabilityMarker} only after explicit human approval and attach the source-grounded result.`
      );
      await page.getByTestId('workflow-project-key').fill(projectKey);
      await page.getByRole('button', { name: /Basic$/ }).click();
      await expect(page.getByRole('button', { name: /Advanced$/ })).toBeVisible();
      await page.locator('details.advanced-block > summary').click();
      await page.getByTestId('workflow-source-id').fill(`e2e-source-${runId}`);
      await page.getByTestId('workflow-source-uri').fill(`local://e2e/source/${runId}`);
      await page.getByTestId('workflow-source-label').fill(sourceName);
      const intakeResponse = page.waitForResponse((response) => {
        const path = new URL(response.url()).pathname;
        return response.request().method() === 'POST'
          && (path === '/api/v1/pursuits/intake' || /^\/api\/v1\/pursuits\/[^/]+\/intake$/.test(path));
      }, { timeout: 20_000 });
      await page.getByTestId('workflow-create').click();
      const response = await intakeResponse;
      await requireResponse(response, 'workflow intake', [200, 201]);
      const routed = await response.json();
      const workflows = Array.isArray(routed?.detail?.workflows)
        ? routed.detail.workflows
        : Array.isArray(routed?.workflows) ? routed.workflows : [];
      const workflow = [...workflows].reverse().find((item) => item?.projectKey === projectKey)
        || workflows[workflows.length - 1];
      expect(workflow?.id).toBeTruthy();
      artifacts.workflowId = workflow.id;
      await expect(
        page.getByTestId('workflow-approval-controls').or(page.getByTestId('workflow-runtime-selection'))
      ).toBeVisible();
    });

    await test.step('select the exact runtime and approve through the durable boundary', async () => {
      const runtimeSelection = page.getByTestId('workflow-runtime-selection');
      if (await runtimeSelection.isVisible()) {
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
      } else {
        await page.getByTestId('workflow-approve').click();
      }
      await expect(page.getByTestId('workflow-approval-controls')).toHaveCount(0);
      await expect(page.getByTestId('workflow-runtime-selection')).toHaveCount(0);
      await expect(page.getByText(/approved/i).first()).toBeVisible();
    });

    await test.step('run only the selected safe, approved workflow', async () => {
      const exactRun = page.getByTestId('workflow-run-selected');
      await expect(exactRun).toBeVisible();
      await exactRun.click();
      const confirmation = page.getByRole('dialog');
      await expect(confirmation.getByText('Run this approved workflow?')).toBeVisible();
      const runResponse = page.waitForResponse((response) =>
        response.request().method() === 'POST'
        && /\/api\/v1\/workflow\/[^/]+\/run$/.test(new URL(response.url()).pathname)
      );
      await confirmation.getByRole('button', { name: 'Run this workflow', exact: true }).click();
      const response = await runResponse;
      expect(response.ok(), await response.text()).toBeTruthy();
      const result = await response.json();
      expect(result.status).toBe('completed');
      expect(result.state).toBe('completed');
      await expect(page.getByText(/workflow completed/i).first()).toBeVisible();
      await expect(page.getByText(/last operation/i).first()).toBeVisible();
      await expect(page.getByTestId('workflow-selected-state')).toHaveText('completed');
      await expect(page.getByTestId('workflow-selected-verification')).not.toHaveText('-');
    });
  });
});
