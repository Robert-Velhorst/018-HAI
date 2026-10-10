import json
import io
from pathlib import Path
import re
import shutil
import subprocess
import unittest
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[1]
WORKFLOW = (ROOT / ".github" / "workflows" / "ci.yml").read_text(
    encoding="utf-8"
)


def job_block(job_id: str) -> str:
    marker = f"  {job_id}:\n"
    start = WORKFLOW.index(marker)
    remaining = WORKFLOW[start + len(marker) :]
    next_job_offsets = [
        offset
        for offset, line in enumerate(remaining.splitlines(keepends=True))
        if line.startswith("  ")
        and not line.startswith("    ")
        and line.rstrip().endswith(":")
    ]
    if not next_job_offsets:
        return WORKFLOW[start:]
    lines = remaining.splitlines(keepends=True)
    return WORKFLOW[start : start + len(marker) + sum(
        len(line) for line in lines[: next_job_offsets[0]]
    )]


def compose_service_block(compose: str, service: str) -> str:
    pattern = rf"(?ms)^  {re.escape(service)}:\n(.*?)(?=^  [A-Za-z0-9_-]+:\n|\Z)"
    match = re.search(pattern, compose)
    if not match:
        raise AssertionError(f"missing Compose service {service!r}")
    return match.group(1)


def browser_step(name: str) -> str:
    match = re.search(
        rf"(?ms)^      - name: {re.escape(name)}\n(.*?)(?=^      - |\Z)",
        job_block("browser-acceptance"),
    )
    if not match:
        raise AssertionError(f"missing browser step {name!r}")
    return match.group(1)


def browser_test_program() -> str:
    step = browser_step("Start owned stack and run browser acceptance")
    match = re.search(r"(?ms)^          node <<'NODE'\n(.*?)^          NODE$", step)
    if not match:
        raise AssertionError("browser step must execute its private test process")
    return "\n".join(line.removeprefix("          ") for line in match.group(1).splitlines())


def exercise_browser_program(scenario: str) -> dict:
    # Execute the workflow's actual program with fake OS boundaries, never Docker or a live stack.
    harness = r"""
const vm = require('node:vm');
const path = require('node:path');
const os = require('node:os');
const input = JSON.parse(require('node:fs').readFileSync(0, 'utf8'));
const owner = 'a'.repeat(32), project = `hai-acceptance-${owner.slice(0, 12)}`;
const evidence = path.join(os.tmpdir(), `hai-acceptance-${owner}`);
const password = 'E2eOnly-private-fixture', key = 'private-signing-fixture';
const env = { HAI_ACCEPTANCE_EVIDENCE: evidence, GITHUB_WORKSPACE: path.join(os.tmpdir(), 'checkout') };
const calls = [], output = [], files = {};
const ids = ['1'.repeat(64), '2'.repeat(64)];
let manifest = { version: 1, owner, project, executionMode: 'manual-local', email: 'e2e-owner@example.test', port: 18080, password };
if (input.scenario === 'bad-manifest') manifest.owner = 'foreign-owner';
if (input.scenario === 'wrong-mode') manifest.executionMode = 'paused';
if (input.scenario === 'bad-port') manifest.port = 80;
const filesystem = {
  realpathSync: value => value,
  readFileSync: filename => {
    if (filename === path.join(evidence, 'manifest.json')) return JSON.stringify(manifest);
    if (filename === path.join(evidence, 'compose.json')) return JSON.stringify({ services: { backend: { environment: { JWT_SECRET: key, SMTP_PASSWORD: '' } } } });
    throw new Error('Unexpected private file read');
  },
  mkdirSync: () => {},
  writeFileSync: (filename, content) => { files[filename] = content; },
};
const spawnSync = (command, args, options) => {
  calls.push({ command, args, env: { ...env } });
  const result = { status: 0, stdout: '', stderr: '' };
  if (command === 'pwsh') {
    result.stdout = `starting ${password} ${key}`;
    if (input.scenario === 'start-failure') result.status = 1;
  } else if (command === 'python3') {
    if (input.scenario === 'readiness-failure') result.status = 1;
  } else if (command === 'npm') {
    result.stdout = `browser ${password}`;
    if (input.scenario === 'browser-failure') result.status = 1;
  } else if (command === 'docker') {
    if (args[0] === 'ps') result.stdout = input.scenario === 'empty' ? '' : ids.join('\n');
    else if (args[0] === 'inspect') {
      const foreign = args[1] === ids[1] && input.scenario === 'wrong-owner';
      const mounts = input.scenario === 'persistent-volume' ? [{ Type: 'volume', Source: 'installation' }] :
        input.scenario === 'foreign-bind' ? [{ Type: 'bind', Source: path.join(os.tmpdir(), 'personal'), RW: false }] :
        input.scenario === 'writable-bind' ? [{ Type: 'bind', Source: path.join(evidence, 'sources'), RW: true }] :
        [{ Type: 'bind', Source: path.join(evidence, 'sources'), RW: false }, { Type: 'tmpfs' }];
      result.stdout = JSON.stringify([{ Name: `/${project}-${args[1] === ids[0] ? 'backend' : 'idp'}`,
        Config: { Labels: { 'hai.acceptance.owner': foreign ? 'b'.repeat(32) : owner, 'com.docker.compose.project': project } }, Mounts: mounts }]);
    } else if (args[0] === 'logs') {
      result.stdout = `owned log ${password}`; result.stderr = `stderr ${key}`;
      if (input.scenario === 'logs-failure') result.status = 1;
    } else throw new Error('Unexpected Docker operation');
  } else throw new Error('Unexpected executable');
  return result;
};
let error = null;
try {
  vm.runInNewContext(input.program, {
    require: name => name === 'node:fs' ? filesystem : name === 'node:child_process' ? { spawnSync } : require(name),
    process: { env }, console: { log: value => output.push(String(value)), error: value => output.push(String(value)) },
  });
} catch (failure) { error = failure.message; }
console.log(JSON.stringify({ error, calls, output, files, env, evidence, owner, project, password, key }));
"""
    result = subprocess.run(
        [shutil.which("node"), "-e", harness],
        input=json.dumps({"program": browser_test_program(), "scenario": scenario}),
        text=True, capture_output=True, check=True, timeout=20,
    )
    return json.loads(result.stdout)


class CIWorkflowContractTest(unittest.TestCase):
    def test_workflow_token_is_read_only_for_current_ci_jobs(self) -> None:
        self.assertRegex(WORKFLOW, r"(?m)^permissions:\n  contents: read$")
        self.assertNotRegex(WORKFLOW, r"(?m)^  (?:actions|contents|packages): write$")

    def test_checklist_status_and_audit_use_one_guarded_commit(self) -> None:
        service = (ROOT / "backend/internal/workflow/service.go").read_text(encoding="utf-8")
        method = service.split("func (s *service) UpdateChecklistItem(", 1)[1].split("func (s *service) RecoverStaleClaims(", 1)[0]
        self.assertIn("s.repo.CommitChecklistUpdate(&expected, &item", method)
        self.assertNotIn("s.repo.UpdateChecklistItem(", method)
        self.assertNotIn("s.audit(", method)
        self.assertIn("if !committed", method)
        repository = (ROOT / "backend/internal/workflow/repository.go").read_text(encoding="utf-8")
        commit = repository.split("func (r *GormRepository) CommitChecklistUpdate(", 1)[1].split("func (r *GormRepository) SaveIntakeRecord(", 1)[0]
        for token in ('r.DB.Transaction(', 'clause.Locking{Strength: "UPDATE"}', 'workflow_id = ? AND status = ? AND updated_at = ?', 'NewGormRepository(tx).CreateEvent(&event)', 'workflowAuditPersistenceFailure("checklist event", err)'):
            self.assertIn(token, commit)
        self.assertTrue((ROOT / "backend/internal/workflow/checklist_transaction_test.go").is_file())

    def test_autonomy_http_telemetry_uses_owner_scoped_queries(self) -> None:
        handler = (ROOT / "backend/internal/autonomy/handler.go").read_text(encoding="utf-8")
        service = (ROOT / "backend/internal/autonomy/service.go").read_text(encoding="utf-8")
        self.assertIn("c.GetString(identity.ContextSubjectKey)", handler)
        self.assertIn("h.service.OverviewForOwner(owner)", handler)
        self.assertNotIn("h.service.Overview()", handler)
        self.assertIn("Model(&models.WorkflowItem{})", service)
        self.assertIn('Where("owner_identity = ?", *owner)', service)
        self.assertIn('Where("workflow_id IN (?)", workflows)', service)
        for timestamp, limit, target in (("observed_at", "limit", "states"),
                                         ("started_at", "limit", "actions"),
                                         ("created_at", "500", "evaluations")):
            self.assertIn(f's.db.Scopes(scope).Order("{timestamp} desc").Limit({limit}).Find(&{target})', service)
        self.assertTrue((ROOT / "backend/internal/autonomy/owner_scope_test.go").is_file())

    def test_autonomy_http_telemetry_withholds_raw_execution_evidence(self) -> None:
        handler = (ROOT / "backend/internal/autonomy/handler.go").read_text(encoding="utf-8")
        projection = (ROOT / "backend/internal/autonomy/public_projection.go").read_text(encoding="utf-8")
        self.assertIn("c.JSON(http.StatusOK, publicOverview(result))", handler)
        self.assertIn('state.Snapshot = "[withheld from telemetry]"', projection)
        self.assertIn('action.ActionPayload = ""', projection)
        self.assertIn('result.RecentStressRuns[index].Results = ""', projection)
        self.assertIn("safety.RedactSecrets(value)", projection)
        self.assertLess(projection.index("safety.RedactSecrets(value)"), projection.index("runes[:4096]"))
        self.assertIn("append([]models.AutonomyActionTrace{}, source.RecentActions...)", projection)
        self.assertTrue((ROOT / "backend/internal/autonomy/public_projection_test.go").is_file())

    def test_disposable_acceptance_guard_runs_without_live_stack_mutations(self) -> None:
        compose = job_block("compose")
        self.assertIn("Disposable acceptance isolation and cleanup contracts", compose)
        self.assertIn("shell: pwsh", compose)
        self.assertIn("./scripts/test-isolated-acceptance-stack.ps1", compose)
        runner = (ROOT / "scripts/test-isolated-acceptance-stack.ps1").read_text(encoding="utf-8")
        for contract in ("wrong-owner cleanup refuses all destructive calls", "configuration failure restores caller environment",
                         "backend-ingress-attachment", "missing-scheduler-disable", "anonymous-role-volume"):
            self.assertIn(contract, runner)

    def test_compose_validation_runs_the_fail_closed_truthfulness_audit(self) -> None:
        compose = job_block("compose")
        audit = (ROOT / "scripts" / "no-fake-claims-audit.sh").read_text(
            encoding="utf-8"
        )

        self.assertIn("No-fake claims and tracked-artifact audit", compose)
        self.assertIn("bash scripts/no-fake-claims-audit.sh", compose)
        self.assertIn("command -v git", audit)
        self.assertIn("refusing to report an incomplete audit as passing", audit)

    def test_frontend_audit_fails_closed_on_advisories_and_registry_timeouts(self) -> None:
        frontend = job_block("frontend")
        audit = ROOT / "scripts" / "audit-npm-production-dependencies.sh"

        self.assertTrue(audit.is_file())
        audit_text = audit.read_text(encoding="utf-8")
        self.assertIn("bash ../scripts/audit-frontend-production-dependencies.sh", frontend)
        self.assertIn("npm audit --omit=dev --audit-level=high --json", audit_text)
        self.assertIn("confirmed high or critical production dependency advisory", audit_text)
        self.assertIn("audit infrastructure did not return a trustworthy report", audit_text)
        self.assertRegex(audit_text, r"audit infrastructure did not return a trustworthy report[\s\S]*?exit 2")

        promptfoo = job_block("promptfoo-runner")
        self.assertIn(
            'bash ../../scripts/audit-npm-production-dependencies.sh "Promptfoo runner"',
            promptfoo,
        )

    def test_bootstrap_document_matches_the_dark_first_theme_contract(self) -> None:
        index = (ROOT / "frontend" / "src" / "index.html").read_text(
            encoding="utf-8"
        )

        # Angular loads after the browser has already painted index.html. The
        # bootstrap shell must apply the same persisted dark-first preference as
        # ThemeService so a slow local Windows startup does not flash the light
        # theme or present an unbranded document title.
        self.assertIn("HAI Automation Hub", index)
        self.assertIn("hai-theme-mode", index)
        self.assertIn("document.documentElement.classList.add(themeClass)", index)
        self.assertIn("background:#08111f", index)

    def test_operational_shell_avoids_decorative_compositing_effects(self) -> None:
        styles = (ROOT / "frontend" / "src" / "styles.scss").read_text(
            encoding="utf-8"
        )
        self.assertNotIn("backdrop-filter:", styles)
        self.assertNotIn("linear-gradient(180deg", styles)

    def test_control_center_keeps_its_route_styles_isolated(self) -> None:
        component = (
            ROOT
            / "frontend"
            / "src"
            / "app"
            / "pages"
            / "control-center"
            / "control-center.component.ts"
        ).read_text(encoding="utf-8")
        self.assertIn("encapsulation: ViewEncapsulation.Emulated", component)
        self.assertNotIn("encapsulation: ViewEncapsulation.None", component)

    def test_frontend_uses_one_direct_angular_cdk_dependency(self) -> None:
        frontend = ROOT / "frontend"
        package = json.loads((frontend / "package.json").read_text(encoding="utf-8"))
        package_lock = (frontend / "package-lock.json").read_text(encoding="utf-8")

        self.assertIn("@angular/cdk", package["dependencies"])
        self.assertNotIn("angular-mixed-cdk-drag-drop", package["dependencies"])
        self.assertNotIn("angular-mixed-cdk-drag-drop", package_lock)

    def test_frontend_focused_compiler_keeps_full_release_gates(self) -> None:
        frontend = job_block("frontend")
        package = json.loads((ROOT / "frontend" / "package.json").read_text(encoding="utf-8"))
        command = package["scripts"]["check:control-room"]
        self.assertEqual(
            command,
            "node --max-old-space-size=384 scripts/check-control-room-compiler.mjs && "
            "node --max-old-space-size=384 scripts/check-control-room-compiler.mjs --spec-types && "
            "node --max-old-space-size=384 scripts/check-control-room-compiler.mjs --source-navigation && "
            "node --max-old-space-size=384 scripts/check-control-room-compiler.mjs --workflow && "
            "node --max-old-space-size=384 scripts/check-control-room-compiler.mjs --workflow-spec-types",
        )
        self.assertIn("npm run check:control-room", frontend)
        self.assertLess(frontend.index("npm ci"), frontend.index("npm run check:control-room"))
        self.assertLess(frontend.index("npm run check:control-room"), frontend.index("npm run build"))
        self.assertIn("npx ng test --watch=false --browsers=ChromeHeadlessNoSandbox", frontend)
        self.assertIn("scripts/deep-link-focus.test.mjs", package["scripts"]["test"])
        self.assertIn("scripts/framework-secret-boundary.test.mjs", package["scripts"]["test"])
        self.assertIn("scripts/framework-secret-boundary.test.mjs", frontend)
        self.assertTrue(package["scripts"]["test"].endswith("&& ng test"))

    def test_frontend_theme_excludes_unused_ng_zorro_components(self) -> None:
        theme = (ROOT / "frontend" / "src" / "theme.less").read_text(
            encoding="utf-8"
        )

        # Importing ng-zorro-antd.less expands the complete component catalog
        # into HAI's initial CSS chunk. Keep this explicit list aligned with
        # application module usage so a new UI dependency has a deliberate
        # styling and payload review.
        self.assertNotIn("ng-zorro-antd/ng-zorro-antd.less", theme)
        expected_components = {
            "icon", "alert", "button", "card", "checkbox", "drawer",
            "dropdown", "empty", "form", "input", "input-number",
            "layout", "list", "modal", "radio", "select", "spin",
            "steps", "table", "tag", "timeline", "tooltip", "upload",
        }
        imported_components = set(
            re.findall(r'ng-zorro-antd/([^/]+)/style/entry\.less', theme)
        )
        self.assertEqual(imported_components, expected_components)
        self.assertIn('ng-zorro-antd/style/default.less', theme)
        self.assertIn('ng-zorro-antd/style/patch.less', theme)

    def test_home_menu_uses_the_ng_zorro_menu_module(self) -> None:
        home_module = (ROOT / "frontend" / "src" / "app" / "pages" / "home" / "home.module.ts").read_text(
            encoding="utf-8"
        )
        home_styles = (ROOT / "frontend" / "src" / "app" / "pages" / "home" / "home.component.scss").read_text(
            encoding="utf-8"
        )

        self.assertIn('from "ng-zorro-antd/menu"', home_module)
        self.assertIn("NzMenuModule", home_module)
        self.assertIn(".dropdown-menu {", home_styles)
        self.assertIn(".ant-menu {", home_styles)

    def test_home_automation_images_defer_offscreen_work(self) -> None:
        home_template = (ROOT / "frontend" / "src" / "app" / "pages" / "home" / "home.component.html").read_text(
            encoding="utf-8"
        )

        self.assertIn('loading="lazy"', home_template)
        self.assertIn('decoding="async"', home_template)

    def test_pursuit_reservation_styles_load_with_the_lazy_module(self) -> None:
        global_styles = (ROOT / "frontend" / "src" / "styles.scss").read_text(
            encoding="utf-8"
        )
        pursuit_styles = (
            ROOT
            / "frontend"
            / "src"
            / "app"
            / "pages"
            / "pursuits"
            / "pursuits.component.scss"
        ).read_text(encoding="utf-8")

        self.assertNotIn(".resource-reservations {", global_styles)
        self.assertIn(".resource-reservations {", pursuit_styles)
        self.assertIn(".resource-reservation--stale", pursuit_styles)

    def test_control_center_styles_load_with_the_lazy_module(self) -> None:
        global_styles = (ROOT / "frontend" / "src" / "styles.scss").read_text(
            encoding="utf-8"
        )
        control_center_styles = (
            ROOT
            / "frontend"
            / "src"
            / "app"
            / "pages"
            / "control-center"
            / "control-center.component.scss"
        ).read_text(encoding="utf-8")
        control_center_component = (
            ROOT
            / "frontend"
            / "src"
            / "app"
            / "pages"
            / "control-center"
            / "control-center.component.ts"
        ).read_text(encoding="utf-8")

        self.assertNotIn("/* HAI Control Center design system */", global_styles)
        self.assertNotIn("app-control-center{--bg", global_styles)
        # The Control Center now consumes the shared app-shell tokens from its
        # lazy component host. The legacy app-control-center wrapper was
        # deliberately removed during the shell consolidation.
        normalized_styles = control_center_styles.replace(" ", "").replace("\n", "")
        self.assertIn(":host{--hai-primary:var(--hai-blue);", normalized_styles)
        self.assertNotIn("app-control-center", control_center_styles)
        self.assertIn(".command-board{display:grid;", normalized_styles)
        self.assertIn(".page-content", control_center_styles)
        self.assertIn("ViewEncapsulation.Emulated", control_center_component)
        self.assertNotIn("ViewEncapsulation.None", control_center_component)

    def test_workflow_engine_styles_load_with_the_lazy_module(self) -> None:
        global_styles = (ROOT / "frontend" / "src" / "styles.scss").read_text(
            encoding="utf-8"
        )
        workflow_styles = (
            ROOT
            / "frontend"
            / "src"
            / "app"
            / "pages"
            / "workflow-engine"
            / "workflow-engine.component.scss"
        ).read_text(encoding="utf-8")
        workflow_component = (
            ROOT
            / "frontend"
            / "src"
            / "app"
            / "pages"
            / "workflow-engine"
            / "workflow-engine.component.ts"
        ).read_text(encoding="utf-8")

        self.assertNotIn(
            "/* Workflow Engine follows the command-center rule:", global_styles
        )
        self.assertIn(
            "app-workflow-engine hai-progressive-section.workflow-more-tools {",
            workflow_styles,
        )
        self.assertIn("ViewEncapsulation.Emulated", workflow_component)
        self.assertNotIn("ViewEncapsulation.None", workflow_component)

    def test_pursuits_disclosure_styles_load_with_the_lazy_authenticated_shell(self) -> None:
        global_styles = (ROOT / "frontend" / "src" / "styles.scss").read_text(
            encoding="utf-8"
        )
        shell_styles = (
            ROOT
            / "frontend"
            / "src"
            / "app"
            / "control-room"
            / "app-shell.component.scss"
        ).read_text(encoding="utf-8")
        pursuit_styles = (
            ROOT
            / "frontend"
            / "src"
            / "app"
            / "pages"
            / "pursuits"
            / "pursuits.component.scss"
        ).read_text(encoding="utf-8")
        pursuit_component = (
            ROOT
            / "frontend"
            / "src"
            / "app"
            / "pages"
            / "pursuits"
            / "pursuits.component.ts"
        ).read_text(encoding="utf-8")
        shell_component = (
            ROOT
            / "frontend"
            / "src"
            / "app"
            / "control-room"
            / "app-shell.component.ts"
        ).read_text(encoding="utf-8")

        self.assertNotIn(
            "/* Pursuits uses the same calm, progressive-disclosure pattern", global_styles
        )
        self.assertNotIn("app-pursuits .pursuit-health", pursuit_styles)
        self.assertIn("details.pursuit-health", shell_styles)
        self.assertIn("details.route-intake-panel", shell_styles)
        self.assertIn("encapsulation: ViewEncapsulation.None", shell_component)
        self.assertIn("ViewEncapsulation.Emulated", pursuit_component)
        self.assertNotIn("ViewEncapsulation.None", pursuit_component)

    def test_pursuit_portfolio_planner_styles_load_with_the_lazy_module(self) -> None:
        global_styles = (ROOT / "frontend" / "src" / "styles.scss").read_text(
            encoding="utf-8"
        )
        pursuit_styles = (
            ROOT
            / "frontend"
            / "src"
            / "app"
            / "pages"
            / "pursuits"
            / "pursuits.component.scss"
        ).read_text(encoding="utf-8")

        # The planner opens only from the lazy Pursuits route. Angular retains
        # the component's scoped attributes when the content is rendered in the
        # NG-Zorro overlay, so it remains styled without leaking route rules.
        self.assertNotIn(".portfolio-planner {", global_styles)
        self.assertIn(".portfolio-planner {", pursuit_styles)
        self.assertIn(".portfolio-workflow-settlement__usage", pursuit_styles)

    def test_component_style_budget_allows_a_deferred_complex_workspace(self) -> None:
        angular = json.loads(
            (ROOT / "frontend" / "angular.json").read_text(encoding="utf-8")
        )
        budgets = angular["projects"]["app"]["architect"]["build"][
            "configurations"
        ]["production"]["budgets"]
        component_budget = next(
            budget for budget in budgets if budget["type"] == "anyComponentStyle"
        )

        # Route styles are loaded on demand. The portfolio workspace is an
        # intentional advanced surface, not a reason to ship its CSS in the
        # initial bundle. Keep a ceiling to catch accidental style growth.
        self.assertEqual(component_budget["maximumWarning"], "20kb")
        self.assertEqual(component_budget["maximumError"], "48kb")

    def test_frontend_copies_only_runtime_icon_assets(self) -> None:
        angular = json.loads(
            (ROOT / "frontend" / "angular.json").read_text(encoding="utf-8")
        )
        assets = angular["projects"]["app"]["architect"]["build"]["options"][
            "assets"
        ]
        icon_asset = next(
            asset
            for asset in assets
            if isinstance(asset, dict)
            and asset.get("input")
            == "./node_modules/@ant-design/icons-angular/src/inline-svg/"
        )

        # The package directory also contains source JavaScript modules. They
        # are not browser assets and publishing them inflates every Windows
        # installer and container image without helping nz-icon resolve SVGs.
        self.assertEqual(icon_asset["glob"], "**/*.svg")

    def test_canonical_service_runtime_images_do_not_float_on_latest(
        self,
    ) -> None:
        for relative_path in (
            "backend/Dockerfile",
            "idp/Dockerfile",
            "nginx-config-manager/Dockerfile",
        ):
            with self.subTest(path=relative_path):
                dockerfile = (ROOT / relative_path).read_text(encoding="utf-8")
                self.assertNotRegex(
                    dockerfile,
                    r"(?m)^FROM\s+\S+:latest(?:\s|$)",
                )
                self.assertIn("FROM ubuntu:24.04", dockerfile)

    def test_local_compose_uses_one_lightweight_kafka_protocol_broker(self) -> None:
        compose = (ROOT / "docker-compose.local.yml").read_text(encoding="utf-8")
        self.assertIn("docker.redpanda.com/redpandadata/redpanda:", compose)
        self.assertNotIn("confluentinc/cp-zookeeper", compose)
        self.assertNotIn("confluentinc/cp-kafka", compose)
        self.assertNotIn("  zookeeper:\n", compose)
        self.assertNotIn("  kafka-network:\n", compose)
        self.assertIn("mem_limit: ${KAFKA_MEMORY_LIMIT:-256m}", compose)
        self.assertIn("cpus: ${KAFKA_CPU_LIMIT:-0.5}", compose)
        self.assertIn("--overprovisioned=true", compose)

    def test_default_compose_starts_only_the_core_local_stack(self) -> None:
        compose = (ROOT / "docker-compose.local.yml").read_text(encoding="utf-8")
        services_section = compose.split("services:\n", 1)[1].split(
            "\nnetworks:\n", 1
        )[0]
        services = re.findall(r"(?m)^  ([A-Za-z0-9_-]+):\n", services_section)
        core_services = {
            "idp",
            "backend",
            "backend-migrate",
            "backend-runtime-role",
            "backend-state-permissions",
            "frontend",
            "nginx",
            "postgres-idp",
            "postgres-automation",
            "redis",
        }

        default_services = set()
        optional_services = set()
        for service in services:
            service_block = compose_service_block(compose, service)
            if re.search(r"(?m)^    profiles:", service_block):
                optional_services.add(service)
            else:
                default_services.add(service)

        self.assertEqual(default_services, core_services)
        self.assertEqual(len(services), 44)
        self.assertEqual(len(optional_services), 34)
        self.assertTrue(optional_services)
        for service in optional_services:
            with self.subTest(service=service):
                self.assertIn(
                    "profiles:", compose_service_block(compose, service),
                    "optional integrations must never join the default local startup",
                )

    def test_optional_profile_documentation_covers_every_compose_profile(self) -> None:
        compose = (ROOT / "docker-compose.local.yml").read_text(encoding="utf-8")
        documentation = (ROOT / "docs" / "optional-runtime-profiles.md").read_text(
            encoding="utf-8"
        )
        compose_profiles = set(
            re.findall(r'(?m)^    profiles:\s*\["([^"\]]+)"\]', compose)
        )
        documented_profiles = set(
            re.findall(r"(?m)^\| `([^`]+)` \|", documentation)
        )

        self.assertEqual(len(compose_profiles), 25)
        self.assertEqual(documented_profiles, compose_profiles)

    def test_local_compose_has_one_canonical_runtime_and_unique_container_names(
        self,
    ) -> None:
        compose = (ROOT / "docker-compose.local.yml").read_text(encoding="utf-8")
        runtime_services = (
            "idp",
            "backend",
            "frontend",
            "nginx",
            "postgres-idp",
            "postgres-automation",
            "redis",
        )
        container_names = re.findall(
            r"(?m)^    container_name:\s+(\S+)\s*$", compose
        )

        self.assertEqual(len(container_names), len(set(container_names)))
        self.assertNotRegex(compose, r"(?m)^[ \t]+(?:scale|replicas):")
        for service in runtime_services:
            with self.subTest(service=service):
                block = compose_service_block(compose, service)
                self.assertRegex(block, r"(?m)^    container_name:\s+\S+")
                self.assertEqual(
                    len(re.findall(rf"(?m)^  {re.escape(service)}:\s*$", compose)),
                    1,
                )

        # These run once as backend dependencies. They must not remain as
        # restartable background copies after the schema/user setup completes.
        for service in ("backend-migrate", "backend-runtime-role"):
            with self.subTest(one_shot=service):
                self.assertIn(
                    'restart: "no"', compose_service_block(compose, service)
                )

        idp_database = compose_service_block(compose, "postgres-idp")
        app_database = compose_service_block(compose, "postgres-automation")
        self.assertIn("POSTGRES_DB: ${IDP_DB_NAME}", idp_database)
        self.assertIn("POSTGRES_DB: ${AUTOMATION_DB_NAME}", app_database)
        self.assertNotEqual(idp_database, app_database)

    def test_local_compose_example_resource_ceiling_is_explicit_and_bounded(
        self,
    ) -> None:
        compose = (ROOT / "docker-compose.local.yml").read_text(encoding="utf-8")
        defaults = (ROOT / ".env.example").read_text(encoding="utf-8")
        service_prefixes = {
            "idp": "IDP",
            "backend": "BACKEND",
            "backend-migrate": "BACKEND_MIGRATE",
            "backend-runtime-role": "BACKEND_RUNTIME_ROLE",
            "frontend": "FRONTEND",
            "nginx": "GATEWAY",
            "postgres-idp": "POSTGRES_IDP",
            "postgres-automation": "POSTGRES_AUTOMATION",
            "redis": "REDIS",
        }
        memory_mib = {}
        cpu_limits = {}

        for service, prefix in service_prefixes.items():
            block = compose_service_block(compose, service)
            memory_key = f"{prefix}_MEMORY_LIMIT"
            cpu_key = f"{prefix}_CPU_LIMIT"
            memory_match = re.search(
                rf"(?m)^[ \t]+mem_limit:[ \t]*\$\{{{memory_key}:-([^}}]+)}}[ \t]*$",
                block,
            )
            cpu_match = re.search(
                rf"(?m)^[ \t]+cpus:[ \t]*\$\{{{cpu_key}:-([^}}]+)}}[ \t]*$",
                block,
            )
            env_memory_match = re.search(
                rf"(?m)^{memory_key}=([^\r\n]+)$", defaults
            )
            env_cpu_match = re.search(rf"(?m)^{cpu_key}=([^\r\n]+)$", defaults)
            self.assertIsNotNone(memory_match, memory_key)
            self.assertIsNotNone(cpu_match, cpu_key)
            self.assertIsNotNone(env_memory_match, memory_key)
            self.assertIsNotNone(env_cpu_match, cpu_key)
            self.assertEqual(memory_match.group(1), env_memory_match.group(1))
            self.assertEqual(cpu_match.group(1), env_cpu_match.group(1))

            value_match = re.fullmatch(
                r"(\d+(?:\.\d+)?)([kKmMgG]?)", env_memory_match.group(1)
            )
            self.assertIsNotNone(value_match, memory_key)
            magnitude = float(value_match.group(1))
            unit = value_match.group(2).lower()
            memory_mib[service] = magnitude * {
                "": 1,
                "m": 1,
                "g": 1024,
                "k": 1 / 1024,
            }[unit]
            cpu_limits[service] = float(env_cpu_match.group(1))

        long_running = set(service_prefixes) - {
            "backend-migrate",
            "backend-runtime-role",
        }
        self.assertEqual(sum(memory_mib[name] for name in long_running), 2688)
        self.assertEqual(sum(memory_mib.values()), 3264)
        self.assertEqual(sum(cpu_limits[name] for name in long_running), 4.0)
        self.assertAlmostEqual(sum(cpu_limits.values()), 5.1)

    def test_local_compose_bounds_the_always_on_desktop_services(self) -> None:
        compose = (ROOT / "docker-compose.local.yml").read_text(encoding="utf-8")
        defaults = (ROOT / ".env.example").read_text(encoding="utf-8")

        for service, prefix in {
            "idp": "IDP",
            "backend": "BACKEND",
            "frontend": "FRONTEND",
            "nginx": "GATEWAY",
            "nginxconfigmanager": "NGINX_CONFIG_MANAGER",
            "postgres-idp": "POSTGRES_IDP",
            "postgres-automation": "POSTGRES_AUTOMATION",
            "redis": "REDIS",
        }.items():
            with self.subTest(service=service):
                service_block = compose_service_block(compose, service)
                self.assertIn(f"mem_limit: ${{{prefix}_MEMORY_LIMIT:-", service_block)
                self.assertIn(f"cpus: ${{{prefix}_CPU_LIMIT:-", service_block)
                self.assertIn(f"pids_limit: ${{{prefix}_PIDS_LIMIT:-", service_block)
                self.assertIn(f"{prefix}_MEMORY_LIMIT=", defaults)
                self.assertIn(f"{prefix}_CPU_LIMIT=", defaults)
                self.assertIn(f"{prefix}_PIDS_LIMIT=", defaults)

    def test_every_compose_service_has_finite_resource_caps(self) -> None:
        compose = (ROOT / "docker-compose.local.yml").read_text(encoding="utf-8")
        services_section = compose.split("services:\n", 1)[1].split(
            "\nnetworks:\n", 1
        )[0]
        services = re.findall(r"(?m)^  ([A-Za-z0-9_-]+):\n", services_section)
        self.assertEqual(len(services), 44)

        for service in services:
            block = compose_service_block(compose, service)
            with self.subTest(service=service):
                self.assertRegex(block, r"(?m)^    mem_limit:\s*(?!0(?:\s|$))\S+")
                self.assertRegex(block, r"(?m)^    cpus:\s*(?!0(?:\s|$))\S+")
                self.assertRegex(
                    block,
                    r"(?m)^    pids_limit:\s*(?:\$\{[A-Z0-9_]+:-)?[1-9]\d*\}?\s*$",
                )
                self.assertNotRegex(block, r"(?im)^\s*(?:mem_limit|cpus|pids_limit):\s*(?:unlimited|0)\s*$")

    def test_every_published_local_compose_port_is_loopback_only(self) -> None:
        compose = (ROOT / "docker-compose.local.yml").read_text(encoding="utf-8")
        services_section = compose.split("services:\n", 1)[1].split(
            "\nnetworks:\n", 1
        )[0]
        services = re.findall(r"(?m)^  ([A-Za-z0-9_-]+):\n", services_section)
        published = []

        for service in services:
            block = compose_service_block(compose, service)
            ports = re.search(
                r"(?ms)^    ports:\n(.*?)(?=^    [A-Za-z_][A-Za-z0-9_-]*:|\Z)",
                block,
            )
            if not ports:
                continue
            mappings = re.findall(r'(?m)^      - "([^"]+)"\s*$', ports.group(1))
            self.assertTrue(mappings, f"could not parse published ports for {service}")
            for mapping in mappings:
                with self.subTest(service=service, mapping=mapping):
                    self.assertTrue(mapping.startswith("127.0.0.1:"), mapping)
                    published.append((service, mapping))

        self.assertGreaterEqual(len(published), 5)
        gateway = compose_service_block(compose, "nginx")
        self.assertIn('"127.0.0.1:${GATEWAY_HOST_PORT:-8088}:80"', gateway)

    def test_temporal_profile_does_not_block_default_compose_when_disabled(self) -> None:
        compose = (ROOT / "docker-compose.local.yml").read_text(encoding="utf-8")
        self.assertNotIn("temporal-local-only", compose)
        for service, key in (
            ("temporal-postgres", "POSTGRES_PASSWORD"),
            ("temporal-schema", "POSTGRES_PWD"),
            ("temporal-schema", "SQL_PASSWORD"),
            ("temporal", "POSTGRES_PWD"),
        ):
            with self.subTest(service=service, key=key):
                block = compose_service_block(compose, service)
                self.assertIn(f'{key}: "${{HAI_TEMPORAL_POSTGRES_PASSWORD:-}}"', block)
                self.assertNotIn("Set HAI_TEMPORAL_POSTGRES_PASSWORD", block)

    def test_backend_runtime_is_immutable_and_uses_separate_runtime_db_settings(self) -> None:
        compose = (ROOT / "docker-compose.local.yml").read_text(encoding="utf-8")
        defaults = (ROOT / ".env.example").read_text(encoding="utf-8")
        backend = compose_service_block(compose, "backend")

        # The API must never regain broad host or kernel privileges merely
        # because an execution adapter is configured. Its writable locations
        # are explicit mounts, while the image root remains immutable.
        self.assertIn("read_only: true", backend)
        self.assertIn("/tmp:rw,noexec,nosuid,size=64m", backend)
        self.assertIn("no-new-privileges:true", backend)
        self.assertIn("cap_drop:\n      - ALL", backend)

        # The API uses a DML-only account by default, so startup migrations stay
        # disabled unless an operator explicitly opts into owner-role startup.
        self.assertIn("DB_USER: ${BACKEND_DB_USER:-hai_runtime}", backend)
        self.assertIn("DB_PASSWORD: ${BACKEND_DB_PASSWORD:-${DB_PASSWORD}}", backend)
        self.assertIn("DB_MIGRATIONS_ENABLED: ${DB_MIGRATIONS_ENABLED:-false}", backend)
        self.assertIn("backend-migrate:\n        condition: service_completed_successfully", backend)
        self.assertIn("backend-runtime-role:\n        condition: service_completed_successfully", backend)
        for setting in ("BACKEND_DB_USER=", "BACKEND_DB_PASSWORD=", "DB_MIGRATIONS_ENABLED="):
            self.assertIn(setting, defaults)

        migration = compose_service_block(compose, "backend-migrate")
        runtime_role = compose_service_block(compose, "backend-runtime-role")
        role_script = (ROOT / "services" / "postgres-runtime-role" / "provision-runtime-role.sh").read_text(encoding="utf-8")
        self.assertIn('command: ["/app/app", "migrate", "up"]', migration)
        self.assertIn("DB_USER: ${DB_USER}", migration)
        self.assertIn("DB_MIGRATIONS_ENABLED: \"false\"", migration)
        self.assertIn("HAI_SEMANTIC_RETRIEVAL_ENABLED: ${HAI_SEMANTIC_RETRIEVAL_ENABLED:-false}", migration)
        self.assertIn("backend-migrate:\n        condition: service_completed_successfully", runtime_role)
        self.assertIn("HAI_RUNTIME_DB_USER: ${BACKEND_DB_USER:-hai_runtime}", runtime_role)
        self.assertIn("HAI_RUNTIME_DB_PASSWORD: ${BACKEND_DB_PASSWORD:-${DB_PASSWORD}}", runtime_role)
        for service, prefix in ((migration, "BACKEND_MIGRATE"), (runtime_role, "BACKEND_RUNTIME_ROLE")):
            self.assertIn(f"mem_limit: ${{{prefix}_MEMORY_LIMIT:-", service)
            self.assertIn(f"cpus: ${{{prefix}_CPU_LIMIT:-", service)
            self.assertIn(f"pids_limit: ${{{prefix}_PIDS_LIMIT:-", service)
            self.assertIn(f"{prefix}_MEMORY_LIMIT=", defaults)
            self.assertIn(f"{prefix}_CPU_LIMIT=", defaults)
            self.assertIn(f"{prefix}_PIDS_LIMIT=", defaults)
        for required in ("NOSUPERUSER", "NOCREATEDB", "NOCREATEROLE", "ALTER DEFAULT PRIVILEGES", "GRANT SELECT, INSERT, UPDATE, DELETE"):
            self.assertIn(required, role_script)

    def test_backend_pool_settings_are_forwarded_to_api_and_migration_job(self) -> None:
        compose = (ROOT / "docker-compose.local.yml").read_text(encoding="utf-8")
        defaults = (ROOT / ".env.example").read_text(encoding="utf-8")
        settings = {
            "DB_MAX_OPEN_CONNS": "8",
            "DB_MAX_IDLE_CONNS": "2",
            "DB_CONN_MAX_IDLE_TIME": "2m",
            "DB_CONN_MAX_LIFETIME": "30m",
            "DB_CONNECT_TIMEOUT": "5s",
            "DB_OPEN_TIMEOUT": "10s",
        }
        for key, value in settings.items():
            with self.subTest(key=key):
                self.assertIn(f"{key}={value}", defaults)
                for service in ("backend", "backend-migrate"):
                    self.assertIn(f"{key}: ${{{key}:-{value}}}", compose_service_block(compose, service))
                # The IDP has its own implementation, not these backend caps.
                self.assertNotIn(f"{key}:", compose_service_block(compose, "idp"))

    def test_backend_runtime_cleanup_is_owned_and_has_compose_grace(self) -> None:
        # Wiring checks complement Go lifecycle regressions, not live acceptance.
        compose = (ROOT / "docker-compose.local.yml").read_text(encoding="utf-8")
        router = (ROOT / "backend/internal/router/router.go").read_text(encoding="utf-8")
        routes = (ROOT / "backend/internal/router/routes.go").read_text(encoding="utf-8")
        temporal = (ROOT / "backend/internal/temporalbridge/service.go").read_text(encoding="utf-8")
        self.assertIn("stop_grace_period: 60s", compose_service_block(compose, "backend"))
        self.assertIn("router.Use(runtimeOwnershipMiddleware(workers))", router)
        self.assertIn("server.RegisterOnShutdown(workers.Stop)", router)
        self.assertIn("workers.StopAndWait(drainCtx)", router)
        self.assertIn("infra.CloseDefaultDB()", router)
        self.assertIn("infra.GetDefaultDBContext(ctx)", router)
        self.assertLess(router.index("metrics.NewFromEnv()"), router.index("infra.GetDefaultDBContext(ctx)"))
        self.assertLess(router.index("infra.GetDefaultDBContext(ctx)"), router.index("initializeRoutesWithContext(router, ctx)"))
        self.assertIn("DB_STARTUP_TIMEOUT: ${DB_STARTUP_TIMEOUT:-5m}", compose_service_block(compose, "backend"))
        self.assertIn("source.WithRuntimeContext(sourceService, runtimeCtx)", routes)
        # Published activities use host lifetime, not an HTTP/probe deadline.
        # Actual cancellation/join semantics are exercised by the Go regressions.
        self.assertIn("ownerCtx := s.workerContext", temporal)
        self.assertIn("BackgroundActivityContext: ownerCtx", temporal)
        self.assertNotIn("BackgroundActivityContext: attemptCtx", temporal)
        self.assertIn('lifecycle.Enter(lifecycle.WithOwnership(ctx, ownerCtx), "temporal-worker-start")', temporal)
        self.assertIn("temporalService.StartWorkerEventually(runtimeCtx)", routes)

    def test_backend_command_deadline_is_forwarded_to_api_and_migration_job(self) -> None:
        compose = (ROOT / "docker-compose.local.yml").read_text(encoding="utf-8")
        defaults = (ROOT / ".env.example").read_text(encoding="utf-8")
        self.assertIn("HAI_COMMAND_TIMEOUT=30m", defaults)
        for service in ("backend", "backend-migrate"):
            with self.subTest(service=service):
                self.assertIn(
                    "HAI_COMMAND_TIMEOUT: ${HAI_COMMAND_TIMEOUT:-30m}",
                    compose_service_block(compose, service),
                )
        self.assertNotIn("HAI_COMMAND_TIMEOUT:", compose_service_block(compose, "idp"))

    def test_gateway_waits_for_ready_control_plane_dependencies(self) -> None:
        compose = (ROOT / "docker-compose.local.yml").read_text(encoding="utf-8")
        gateway = compose_service_block(compose, "nginx")

        # The browser gateway is the first endpoint a local or ngrok user sees.
        # Starting it before the IDP or API is healthy creates an intermittent
        # blank/login failure window even though Compose eventually recovers.
        self.assertIn(
            "backend:\n        condition: service_healthy",
            gateway,
        )
        self.assertIn(
            "idp:\n        condition: service_healthy",
            gateway,
        )

        a2a_gateway = compose_service_block(compose, "a2a-gateway")
        self.assertIn(
            "backend:\n        condition: service_healthy",
            a2a_gateway,
        )

        backend = compose_service_block(compose, "backend")
        self.assertIn("idp:\n        condition: service_healthy", backend)
        config_manager = compose_service_block(compose, "nginxconfigmanager")
        self.assertIn("nginx:\n        condition: service_healthy", config_manager)
        fastmcp = compose_service_block(compose, "fastmcp-bridge")
        self.assertIn("healthcheck:", fastmcp)
        self.assertIn("socket.create_connection", fastmcp)

    def test_optional_ngrok_tunnel_is_profiled_and_fail_closed(self) -> None:
        compose = (ROOT / "docker-compose.local.yml").read_text(encoding="utf-8")
        defaults = (ROOT / ".env.example").read_text(encoding="utf-8")
        launcher = (ROOT / "scripts" / "start-ngrok.ps1")
        entrypoint = ROOT / "deploy" / "ngrok" / "start-ngrok.sh"

        service = compose_service_block(compose, "ngrok")
        self.assertIn('profiles: ["cloud-tunnel"]', service)
        self.assertNotIn("ports:", service)
        self.assertIn("read_only: true", service)
        self.assertIn("no-new-privileges:true", service)
        self.assertIn("cap_drop:", service)
        self.assertIn("HAI_A2A_BRIDGE_PUBLIC_NGROK_ENABLED", service)
        self.assertIn("RATE_LIMIT_PER_MINUTE", service)
        self.assertIn("NGROK_AUTHTOKEN=", defaults)
        self.assertIn("HAI_NGROK_URL=", defaults)
        self.assertIn("HAI_A2A_BRIDGE_PUBLIC_NGROK_ENABLED=false", defaults)
        self.assertTrue(launcher.is_file())
        self.assertTrue(entrypoint.is_file())

        launcher_text = launcher.read_text(encoding="utf-8")
        entrypoint_text = entrypoint.read_text(encoding="utf-8")
        for content in (launcher_text, entrypoint_text):
            self.assertIn("RUN_MODE", content)
            self.assertIn("LOCAL_LOGIN_BYPASS_ENABLED", content)
            self.assertIn("IDP_COOKIE_SECURE", content)
            self.assertIn("GATEWAY_HOST_BIND", content)
            self.assertIn("HAI_A2A_BRIDGE_PUBLIC_NGROK_ENABLED", content)
            self.assertIn("RATE_LIMIT_PER_MINUTE", content)
            self.assertIn("NGROK_AUTHTOKEN", content)
            self.assertIn("HAI_NGROK_URL", content)
            self.assertIn("GOOGLE_LOGIN_REDIRECT_URL", content)
            self.assertIn("GOOGLE_OAUTH_REDIRECT_URL", content)

        self.assertIn("GOOGLE_LOGIN_REDIRECT_URL: ${GOOGLE_LOGIN_REDIRECT_URL:-}", service)
        self.assertIn("GOOGLE_OAUTH_REDIRECT_URL: ${GOOGLE_OAUTH_REDIRECT_URL:-}", service)
        self.assertIn("/api/v1/auth/google/callback", launcher_text)
        self.assertIn("/api/v1/sources/oauth/google/callback", launcher_text)
        self.assertIn("/api/v1/auth/google/callback", entrypoint_text)
        self.assertIn("/api/v1/sources/oauth/google/callback", entrypoint_text)

    def test_optional_event_bus_does_not_expand_the_default_local_stack(self) -> None:
        compose = (ROOT / "docker-compose.local.yml").read_text(encoding="utf-8")
        defaults = (ROOT / ".env.example").read_text(encoding="utf-8")

        kafka = compose_service_block(compose, "kafka")
        config_manager = compose_service_block(compose, "nginxconfigmanager")
        backend = compose_service_block(compose, "backend")
        idp = compose_service_block(compose, "idp")

        self.assertIn('profiles: ["event-bus"]', kafka)
        self.assertIn('profiles: ["event-bus"]', config_manager)
        self.assertNotIn("      kafka:\n", backend)
        self.assertNotIn("      kafka:\n", idp)
        self.assertIn("HAI_EVENT_BUS_ENABLED=false", defaults)

    def test_local_a2a_connector_is_loopback_only_and_not_on_the_cloud_gateway(self) -> None:
        compose = (ROOT / "docker-compose.local.yml").read_text(encoding="utf-8")
        defaults = (ROOT / ".env.example").read_text(encoding="utf-8")
        connector_template = ROOT / "nginx-config" / "a2a-local.conf.template"

        service = compose_service_block(compose, "a2a-gateway")
        self.assertIn('profiles: ["local-a2a"]', service)
        self.assertIn('"127.0.0.1:${HAI_A2A_LOCAL_PORT:-8091}:8080"', service)
        self.assertIn("- a2a-local", service)
        self.assertNotIn("- service-hub", service)
        self.assertIn("a2a-local:", compose)
        self.assertIn("internal: true", compose)
        self.assertIn("HAI_A2A_LOCAL_PORT=8091", defaults)
        backend_service = compose_service_block(compose, "backend")
        self.assertIn(
            "HAI_A2A_BRIDGE_URL: ${HAI_A2A_BRIDGE_URL:-http://127.0.0.1:8091/api/v1/a2a}",
            backend_service,
        )
        self.assertTrue(connector_template.is_file())

        template = connector_template.read_text(encoding="utf-8")
        self.assertIn("listen 8080;", template)
        self.assertIn("location = /.well-known/agent-card.json", template)
        self.assertIn("location = /api/v1/a2a", template)
        self.assertIn("X-HAI-Backend-Key", template)
        self.assertIn("return 404", template)
        self.assertNotIn("location /api/v1", template)

    def test_default_connected_source_allowlist_enables_live_trello(self) -> None:
        defaults = (ROOT / ".env.example").read_text(encoding="utf-8")
        compose = (ROOT / "docker-compose.local.yml").read_text(encoding="utf-8")
        match = re.search(
            r"^CONNECTED_SOURCE_HTTP_ALLOWED_HOSTS=(.+)$",
            defaults,
            re.MULTILINE,
        )
        self.assertIsNotNone(match)
        hosts = {host.strip().lower() for host in match.group(1).split(",")}
        self.assertIn("api.trello.com", hosts)
        for setting in ("TRELLO_API_KEY", "TRELLO_READ_TOKEN", "TRELLO_API_BASE_URL"):
            with self.subTest(setting=setting):
                self.assertIn(f"{setting}=", defaults)
                self.assertIn(f"{setting}: ${{{setting}:-}}", compose)

    def test_google_oauth_callback_uses_signed_state_not_session_rbac(self) -> None:
        routes = (ROOT / "backend" / "internal" / "router" / "routes.go").read_text(
            encoding="utf-8"
        )

        # The provider returns through a browser navigation that may not carry
        # HAI's session cookie. Start remains permission-gated; the callback
        # must reach the source service so its signed, short-lived OAuth state
        # can be verified before any code is exchanged.
        self.assertIn(
            'sourceOAuth.GET("/oauth/google/start", requirePermission(rbac.PermWrite), sourceHandler.StartGoogleOAuth)',
            routes,
        )
        self.assertIn(
            'sourceOAuth.GET("/oauth/google/callback", sourceHandler.GoogleOAuthCallback)',
            routes,
        )
        self.assertNotIn(
            'sourceOAuth.GET("/oauth/google/callback", requirePermission(', routes
        )

    def test_command_dashboard_names_all_registered_controlled_runtimes(self) -> None:
        template = (
            ROOT
            / "frontend"
            / "src"
            / "app"
            / "pages"
            / "command-dashboard"
            / "command-dashboard.component.html"
        ).read_text(encoding="utf-8")

        # The registry exposes four controlled runtime families. Do not leave
        # DeepSeek Harness invisible in the operator-facing summary merely
        # because OpenClaw has additional ecosystem-inspection controls.
        self.assertIn("Hermes, DeepSeek Harness, Odysseus, and OpenClaw", template)

    def test_documented_provider_and_local_observability_settings_reach_backend(self) -> None:
        defaults = (ROOT / ".env.example").read_text(encoding="utf-8")
        compose = (ROOT / "docker-compose.local.yml").read_text(encoding="utf-8")
        backend = compose_service_block(compose, "backend")

        for setting in (
            "NOUS_PORTAL_BASE_URL",
            "NOUS_PORTAL_API_KEY",
            "MIXTURE_OF_AGENTS_BASE_URL",
            "MIXTURE_OF_AGENTS_API_KEY",
            "OPENAI_CODEX_BASE_URL",
            "OPENAI_CODEX_API_KEY",
            "HAI_LANGFUSE_ENABLED",
            "HAI_LANGFUSE_BASE_URL",
            "HAI_LANGFUSE_PUBLIC_KEY",
            "HAI_LANGFUSE_SECRET_KEY",
            "HAI_LANGFUSE_TIMEOUT_SECONDS",
            "DB_AUTOMIGRATE",
            "LM_STUDIO_MODEL_ID",
            "SGLANG_BASE_URL",
            "SGLANG_MODEL_ID",
            "DSPARK_ENABLED",
            "DSPARK_BASE_URL",
            "DSPARK_PROBE_PATH",
            "DSPARK_GENERATION_PATH",
            "DSPARK_MODEL_ID",
            "SOURCE_SCHEDULER_DURABLE",
            "SOURCE_WORKER_POLL_SECONDS",
            "WORKFLOW_SCHEDULER_DURABLE",
            "WORKFLOW_WORKER_POLL_SECONDS",
            "WORKFLOW_REMINDER_DELIVERY_ENABLED",
            "AMBIENT_SCHEDULER_DURABLE",
            "AMBIENT_WORKER_POLL_SECONDS",
            "WHATSAPP_EXPORT_CHUNK_MESSAGES",
            "HAI_CATALOG_REVALIDATION_ENABLED",
            "HAI_CATALOG_COLLECTION_REVALIDATION_ENABLED",
            "HAI_CATALOG_REPOSITORY_DISCOVERY_REVALIDATION_ENABLED",
            "HAI_CATALOG_REVALIDATION_INTERVAL_HOURS",
            "HAI_CATALOG_REVALIDATION_BATCH_SIZE",
            "HAI_CATALOG_REVALIDATION_SCHEDULER_ENABLED",
            "HAI_CATALOG_REVALIDATION_SCHEDULER_INTERVAL_MINUTES",
            "LLM_PROVIDER_PROBE_TIMEOUT_SECONDS",
            "OPENCLAW_ECOSYSTEM_ALLOWED_ROOTS",
            "DEEPSEEK_HARNESS_ENABLED",
            "DEEPSEEK_HARNESS_EXECUTION_ENABLED",
            "DEEPSEEK_HARNESS_EXECUTABLE",
            "DEEPSEEK_HARNESS_WORKSPACE",
            "DEEPSEEK_HARNESS_STATE_DIR",
            "DEEPSEEK_HARNESS_TIMEOUT_SECONDS",
            "DEEPSEEK_HARNESS_ENV_ALLOWLIST",
        ):
            with self.subTest(setting=setting):
                self.assertIn(f"{setting}=", defaults)
                self.assertIn(f"{setting}: ${{{setting}", backend)

    def test_provider_fixture_is_opt_in_and_has_no_host_power(self) -> None:
        compose = (ROOT / "docker-compose.local.yml").read_text(encoding="utf-8")
        fixture = compose_service_block(compose, "provider-fixture")

        self.assertIn('profiles: ["provider-fixture"]', fixture)
        self.assertIn("target: provider-fixture", fixture)
        self.assertIn("read_only: true", fixture)
        self.assertIn("mem_limit: 32m", fixture)
        self.assertIn("cpus: 0.10", fixture)
        self.assertIn("pids_limit: 32", fixture)
        self.assertIn("- ALL", fixture)
        self.assertIn("no-new-privileges:true", fixture)
        self.assertIn('"11434"', fixture)
        self.assertNotIn("ports:", fixture)
        self.assertIn("--healthcheck", fixture)

    def test_ci_runs_provider_fixture_http_contract(self) -> None:
        fixture_job = job_block("provider-fixture")
        smoke = ROOT / "scripts" / "smoke-provider-fixture.sh"

        self.assertTrue(smoke.is_file())
        smoke_text = smoke.read_text(encoding="utf-8")
        self.assertIn("bash scripts/smoke-provider-fixture.sh", fixture_job)
        self.assertIn("--target provider-fixture", smoke_text)
        self.assertIn("--read-only", smoke_text)
        self.assertIn("--cap-drop ALL", smoke_text)
        self.assertIn("no-new-privileges", smoke_text)
        self.assertIn("-p 127.0.0.1:0:11434", smoke_text)
        self.assertIn("MSYS_NO_PATHCONV=1 docker exec", smoke_text)
        self.assertNotIn("docker network create --internal", smoke_text)

    def test_phase2_control_state_and_safe_worker_paths_are_durable(self) -> None:
        compose = (ROOT / "docker-compose.local.yml").read_text(encoding="utf-8")
        defaults = (ROOT / ".env.example").read_text(encoding="utf-8")
        backend = compose_service_block(compose, "backend")

        # The background control service persists emergency-stop and autonomy
        # decisions. Its safe-worker output and opt-in feed imports must use
        # distinct paths so a restart neither resets control state nor lets the
        # worker modify its read-only intake folder.
        for setting, value in (
            ("HAI_PHASE2_WORKSPACE_DIR", "/root/agent-workspaces/phase2"),
            ("HAI_PHASE2_FEEDS_DIR", "/root/phase2-feeds"),
            ("HAI_PHASE2_STATE_DIR", "/root/phase2-control-state"),
            ("HAI_PHASE2_FEED_FILES", ""),
        ):
            with self.subTest(setting=setting):
                self.assertIn(f"{setting}=", defaults)
                self.assertIn(f"{setting}: ${{{setting}:-{value}}}", backend)

        self.assertIn(
            "- phase2-control-state:/root/phase2-control-state",
            backend,
        )
        self.assertIn("- ./phase2-feeds:/root/phase2-feeds:ro", backend)
        self.assertIn("phase2-control-state:\n    name: 018-hai-phase2-control-state", compose)

    def test_quick_start_and_google_local_callback_match_gateway_port(self) -> None:
        defaults = (ROOT / ".env.example").read_text(encoding="utf-8")
        readme = (ROOT / "README.md").read_text(encoding="utf-8")
        self.assertIn("GATEWAY_HOST_PORT=8088", defaults)
        self.assertIn("http://localhost:8088/api/v1/sources/oauth/google/callback", defaults)
        self.assertIn("http://localhost:8088", readme)

    def test_directly_invoked_contract_and_smoke_files_exist(self) -> None:
        for relative_path in (
            "nginx-config/test_gateway_contract.py",
            "scripts/test_ci_contract.py",
            "scripts/test_smoke_auth_contract.py",
            "scripts/smoke-all.sh",
            "scripts/two-account-isolation-test.sh",
        ):
            with self.subTest(path=relative_path):
                self.assertTrue((ROOT / relative_path).is_file())

    def test_execution_boundary_race_tests_are_not_served_from_test_cache(
        self,
    ) -> None:
        backend = job_block("backend")
        self.assertIn(
            "go test -count=1 -race ./internal/automation ./internal/task ./internal/source ./internal/llm ./internal/agentruntime ./internal/durablejob ./internal/background",
            backend,
        )

    def test_backend_vulnerability_scan_is_pinned_and_blocking(self) -> None:
        backend = job_block("backend")
        self.assertIn(
            "go install golang.org/x/vuln/cmd/govulncheck@v1.6.0",
            backend,
        )
        self.assertIn("govulncheck ./...", backend)
        self.assertNotIn("continue-on-error", backend)

    def test_idp_vulnerability_scan_is_pinned_and_blocking(self) -> None:
        idp = job_block("idp")
        self.assertIn(
            "go install golang.org/x/vuln/cmd/govulncheck@v1.6.0",
            idp,
        )
        self.assertIn("govulncheck ./...", idp)
        self.assertNotIn("continue-on-error", idp)

    def test_nginx_manager_toolchain_and_scan_match_container(self) -> None:
        go_mod = (ROOT / "nginx-config-manager" / "go.mod").read_text(
            encoding="utf-8"
        )
        dockerfile = (ROOT / "nginx-config-manager" / "Dockerfile").read_text(
            encoding="utf-8"
        )
        job = job_block("nginx-config-manager")

        recommended = re.search(
            r"^toolchain\s+go(\d+\.\d+\.\d+)$",
            go_mod,
            re.MULTILINE,
        )
        container = re.search(
            r"^FROM\s+golang:(\d+\.\d+\.\d+)\s+AS\s+builder$",
            dockerfile,
            re.MULTILINE,
        )
        self.assertIsNotNone(recommended)
        self.assertIsNotNone(container)
        self.assertEqual(recommended.group(1), container.group(1))
        self.assertIn(f'go-version: "{recommended.group(1)}"', job)
        self.assertIn("- run: go vet ./...", job)
        self.assertIn(
            "go install golang.org/x/vuln/cmd/govulncheck@v1.6.0",
            job,
        )
        self.assertIn("govulncheck ./...", job)
        self.assertNotIn("continue-on-error", job)

    def test_nginx_manager_has_no_docker_socket_control_path(self) -> None:
        go_mod = (ROOT / "nginx-config-manager" / "go.mod").read_text(
            encoding="utf-8"
        )
        service = (
            ROOT
            / "nginx-config-manager"
            / "internal"
            / "app"
            / "autoconfig"
            / "auto_config_service.go"
        ).read_text(encoding="utf-8")
        self.assertNotIn("github.com/docker/docker", go_mod)
        self.assertNotIn("github.com/docker/docker", service)
        self.assertIn("Docker socket control is disabled", service)
        for compose_file in ("docker-compose.local.yml", "docker-compose.yml"):
            with self.subTest(compose_file=compose_file):
                content = (ROOT / compose_file).read_text(encoding="utf-8")
                self.assertNotIn("/var/run/docker.sock", content)

    def test_all_compose_entry_points_delegate_to_the_source_built_stack(self) -> None:
        expected = {
            "docker-compose.yml": "./docker-compose.local.yml",
            "backend/docker-compose.yml": "../docker-compose.local.yml",
            "idp/docker-compose.yml": "../docker-compose.local.yml",
            "gate/docker-compose.yml": "../docker-compose.local.yml",
        }
        for compose_file, local_stack_path in expected.items():
            with self.subTest(compose_file=compose_file):
                content = (ROOT / compose_file).read_text(encoding="utf-8")
                self.assertIn("include:", content)
                self.assertIn(local_stack_path, content)
                self.assertNotIn("jacksonbarreto/", content)

    def test_cross_platform_secret_generator_covers_every_required_backend_key(self) -> None:
        generator = (ROOT / "scripts" / "generate-secrets.sh").read_text(encoding="utf-8")
        generated_keys = {
            "BACKEND_API_SHARED_KEY": "backend_api_shared_key",
            "LOCAL_PREVIEW_GATEWAY_SECRET": "local_preview_gateway_secret",
            "HAI_MEMORY_ENCRYPTION_KEY": "memory_encryption_key",
            "JWT_SECRET": "jwt_secret",
            "HAI_APPROVAL_PROOF_SIGNING_KEY": "approval_proof_signing_key",
            "DB_PASSWORD": "db_password",
            "FIRST_RUN_ADMIN_PASSWORD": "first_run_admin_password",
        }
        self.assertIn("FIRST_RUN_ADMIN_EMAIL=$FIRST_RUN_ADMIN_EMAIL", generator)
        self.assertIn("Set FIRST_RUN_ADMIN_EMAIL or run this script interactively", generator)
        self.assertIn("FIRST_RUN_ADMIN_EMAIL must be a valid email address", generator)
        for key, variable in generated_keys.items():
            with self.subTest(key=key):
                self.assertIn(f'{variable}="$(secret)"', generator)
                self.assertIn(f"{key}=${variable}", generator)

    def test_go_container_builds_bound_parallel_work(self) -> None:
        for module in ("backend", "idp"):
            with self.subTest(module=module):
                dockerfile = (ROOT / module / "Dockerfile").read_text(encoding="utf-8")
                self.assertIn("ARG HAI_GO_BUILD_WORKERS=2", dockerfile)
                builder = re.split(r"^FROM\s", dockerfile, flags=re.MULTILINE)[1]
                self.assertIn("ARG HAI_GO_BUILD_WORKERS=2", builder)
                self.assertEqual(dockerfile.count("ARG HAI_GO_BUILD_WORKERS=2"), 1)
                self.assertIn('case "$HAI_GO_BUILD_WORKERS" in 1|2|4|8)', dockerfile)
                self.assertIn('GOMAXPROCS="$HAI_GO_BUILD_WORKERS"', dockerfile)
                self.assertIn('go build -p "$HAI_GO_BUILD_WORKERS"', dockerfile)

    def test_idp_startup_budget_is_forwarded_only_to_identity_service(self) -> None:
        compose = (ROOT / "docker-compose.local.yml").read_text(encoding="utf-8")
        identity = compose_service_block(compose, "idp")
        backend = compose_service_block(compose, "backend")
        example = (ROOT / ".env.example").read_text(encoding="utf-8")
        self.assertIn("IDP_DB_STARTUP_TIMEOUT: ${IDP_DB_STARTUP_TIMEOUT:-5m}", identity)
        self.assertNotIn("IDP_DB_STARTUP_TIMEOUT:", backend)
        self.assertRegex(example, r"(?m)^IDP_DB_STARTUP_TIMEOUT=5m$")

    def test_idp_routes_borrow_one_owned_database(self) -> None:
        router = (ROOT / "idp/internal/app/router/router.go").read_text(encoding="utf-8")
        routes = (ROOT / "idp/internal/app/router/routes.go").read_text(encoding="utf-8")
        self.assertEqual(router.count("infra.GetDefaultDBContext(ctx)"), 1)
        self.assertIn("errors.Join(resources.Close(), pool.Close())", router)
        self.assertIn("initializeRoutes(router, database, resources)", router)
        self.assertIn("initializeAuthRoutes(router, v1, database, resources)", routes)
        for resource in ("authService", "authHandler", "userService"):
            self.assertIn(f"resources.Add({resource})", routes)
        self.assertIn("authentication.NewDefaultAuthServiceWithDatabase(database)", routes)
        self.assertIn("users.NewDefaultUserServiceWithDatabase(database)", routes)
        self.assertNotIn("GetDefaultAuthService()", routes)
        self.assertNotIn("GetDefaultUserService()", routes)

    def test_idp_signal_shutdown_and_container_grace_are_wired(self) -> None:
        main = (ROOT / "idp/cmd/main.go").read_text(encoding="utf-8")
        router = (ROOT / "idp/internal/app/router/router.go").read_text(encoding="utf-8")
        compose = (ROOT / "docker-compose.local.yml").read_text(encoding="utf-8")
        self.assertIn("signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)", main)
        self.assertIn("runIdentity(ctx, config.Setup, router.InitializeContext, os.Stderr)", main)
        self.assertIn("serveIdentity(ctx, newHTTPServer(port, router), 90*time.Second)", router)
        self.assertIn("stop_grace_period: 120s", compose_service_block(compose, "idp"))

    def test_idp_route_recovery_does_not_use_raw_request_dump(self) -> None:
        router = (ROOT / "idp/internal/app/router/router.go").read_text(encoding="utf-8")
        self.assertIn("router.Use(gin.LoggerWithFormatter(formatAccessLog), privateRecovery())", router)
        self.assertIn("gin.CustomRecoveryWithWriter(nil,", router)
        self.assertNotIn("gin.Recovery()", router)
        self.assertIn('errorMessage != "" || containsPasswordResetTokenPath(params.Path)', router)

    def test_idp_toolchain_matches_ci_and_container(self) -> None:
        go_mod = (ROOT / "idp" / "go.mod").read_text(encoding="utf-8")
        dockerfile = (ROOT / "idp" / "Dockerfile").read_text(encoding="utf-8")
        idp = job_block("idp")

        language = re.search(r"^go\s+(\d+\.\d+)(?:\.\d+)?$", go_mod, re.MULTILINE)
        recommended = re.search(
            r"^toolchain\s+go(\d+\.\d+\.\d+)$",
            go_mod,
            re.MULTILINE,
        )
        container = re.search(
            r"^FROM\s+golang:(\d+\.\d+\.\d+)\s+AS\s+builder$",
            dockerfile,
            re.MULTILINE,
        )
        ci = re.search(r'go-version:\s+"(\d+\.\d+\.\d+)"', idp)

        self.assertIsNotNone(language)
        self.assertIsNotNone(recommended)
        self.assertIsNotNone(container)
        self.assertIsNotNone(ci)
        self.assertEqual(recommended.group(1), container.group(1))
        self.assertEqual(recommended.group(1), ci.group(1))
        self.assertEqual(
            ".".join(recommended.group(1).split(".")[:2]),
            language.group(1),
        )
        self.assertIn("- run: go vet ./...", idp)

    def test_backend_toolchain_matches_ci_and_container(self) -> None:
        go_mod = (ROOT / "backend" / "go.mod").read_text(encoding="utf-8")
        dockerfile = (ROOT / "backend" / "Dockerfile").read_text(
            encoding="utf-8"
        )

        language = re.search(r"^go\s+(\d+\.\d+)(?:\.\d+)?$", go_mod, re.MULTILINE)
        recommended = re.search(
            r"^toolchain\s+go(\d+\.\d+\.\d+)$",
            go_mod,
            re.MULTILINE,
        )
        container = re.search(
            r"^FROM\s+golang:(\d+\.\d+\.\d+)\s+AS\s+builder$",
            dockerfile,
            re.MULTILINE,
        )

        self.assertIsNotNone(language)
        self.assertIsNotNone(recommended)
        self.assertIsNotNone(container)
        self.assertEqual(recommended.group(1), container.group(1))
        self.assertEqual(
            ".".join(recommended.group(1).split(".")[:2]),
            language.group(1),
        )
        for job_id in (
            "backend",
            "authenticated-smoke",
            "migrations-integration",
            "isolation-acceptance",
        ):
            with self.subTest(job=job_id):
                self.assertIn(
                    f'go-version: "{recommended.group(1)}"',
                    job_block(job_id),
                )

    def test_authenticated_smoke_requires_each_suite_result(self) -> None:
        smoke = job_block("authenticated-smoke")
        self.assertIn(
            'smoke_log="$RUNNER_TEMP/authenticated-smoke.log"',
            smoke,
        )
        for suite in (
            "smoke-background-operations",
            "smoke-model-intelligence",
            "smoke-runtime-lab",
            "smoke-account-bridges",
            "smoke-windows-runtime",
        ):
            with self.subTest(suite=suite):
                self.assertIn(suite, smoke)
        self.assertIn(
            r'grep -Eq "^  PASS  ${suite}  \(Result: [1-9][0-9]* passed, 0 failed\)$"',
            smoke,
        )
        self.assertIn(
            "grep -qx '==> ALL PHASE 2 SMOKE SUITES PASSED'",
            smoke,
        )

    def test_browser_acceptance_uses_an_isolated_real_compose_stack(self) -> None:
        acceptance = job_block("browser-acceptance")
        prepare = browser_step("Prepare owned browser acceptance stack")
        stop = browser_step("Stop exact owned browser acceptance stack")
        self.assertIn("shell: pwsh", prepare)
        self.assertIn("& ./scripts/isolated-acceptance-stack.ps1 -Action Prepare -ExecutionMode manual-local", prepare)
        self.assertIn("$prepared.Count -ne 1", prepare)
        self.assertIn("Resolve-Path -LiteralPath $prepared[0].Matches[0].Groups[1].Value", prepare)
        self.assertIn("[IO.Path]::GetDirectoryName($evidence) -cne $temp", prepare)
        self.assertIn("'^hai-acceptance-[0-9a-f]{32}$'", prepare)
        self.assertEqual(re.findall(r'^          .*GITHUB_ENV.*$', acceptance, re.M),
                         ['          "HAI_ACCEPTANCE_EVIDENCE=$evidence" >> $env:GITHUB_ENV'])
        self.assertIn("if: ${{ always() && env.HAI_ACCEPTANCE_EVIDENCE != '' }}", stop)
        self.assertIn("shell: pwsh", stop)
        self.assertIn("& ./scripts/isolated-acceptance-stack.ps1 -Action Stop -EvidenceDirectory $env:HAI_ACCEPTANCE_EVIDENCE", stop)
        self.assertLess(acceptance.index("-Action Prepare"), acceptance.index("node <<'NODE'"))
        self.assertLess(acceptance.index("node <<'NODE'"), acceptance.index("-Action Stop"))
        for forbidden in ("docker compose", "docker-compose.local.yml", ".env.browser-acceptance",
                          "down -v", "--remove-orphans", "container_name", "FIRST_RUN_ADMIN_PASSWORD",
                          "connected-sources", "E2eLocalOwnerPassword", "${{ secrets."):
            self.assertNotIn(forbidden, acceptance)
        program = browser_test_program()
        self.assertIn('status in {"ready", "degraded"}', program)
        self.assertEqual(acceptance.count("'manifest.json'"), 1)
        self.assertIn("'manifest.json'", program)
        self.assertIn('node-version: "24"', acceptance)
        self.assertIn("npm install --global npm@10.9.8 --no-audit --no-fund", acceptance)
        self.assertIn('test "$(npm --version)" = "10.9.8"', acceptance)
        dependencies = browser_step("Install browser acceptance dependencies")
        for command in ("npm ci", "npm run typecheck", "npx playwright install --with-deps chromium"):
            self.assertRegex(dependencies, rf"(?m)^          {re.escape(command)}$")

    def test_browser_artifacts_are_explicit_unique_and_exclude_private_configuration(self) -> None:
        upload = browser_step("Retain browser acceptance reports and guarded logs")
        self.assertIn("if: ${{ always() && env.HAI_ACCEPTANCE_EVIDENCE != '' }}", upload)
        self.assertIn("uses: actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9 # v7.0.2", upload)
        self.assertIn("name: hai-browser-acceptance-${{ github.run_id }}-${{ github.run_attempt }}", upload)
        paths = re.search(r"(?ms)^          path: \|\n(.*?)(?=^          \S)", upload)
        self.assertIsNotNone(paths)
        self.assertEqual([line.strip() for line in paths.group(1).splitlines()], [
            "${{ env.HAI_ACCEPTANCE_EVIDENCE }}/browser-report/",
            "${{ env.HAI_ACCEPTANCE_EVIDENCE }}/browser-results/",
            "${{ env.HAI_ACCEPTANCE_EVIDENCE }}/owner-logs/*.log",
        ])

    @unittest.skipUnless(shutil.which("pwsh"), "PowerShell is required to check the runner shell contracts")
    def test_browser_powershell_paths_and_launcher_json_property_guards(self) -> None:
        launcher = (ROOT / "scripts/isolated-acceptance-stack.ps1").read_text(encoding="utf-8")
        prefix = re.search(r"(?m)^\s*(\$prefix = .*DirectorySeparatorChar)$", launcher)
        self.assertIsNotNone(prefix)
        programs = [launcher]
        for name in ("Prepare owned browser acceptance stack", "Stop exact owned browser acceptance stack"):
            step = browser_step(name)
            programs.append("\n".join(line.removeprefix("          ") for line in step.split("run: |\n", 1)[1].splitlines()))
        harness = r"""
$ErrorActionPreference = 'Stop'
$inputData = [Console]::In.ReadToEnd() | ConvertFrom-Json
foreach ($program in $inputData.programs) {
    $tokens = $null
    $parseErrors = $null
    $null = [Management.Automation.Language.Parser]::ParseInput($program, [ref]$tokens, [ref]$parseErrors)
    if ($parseErrors.Count) { throw 'Workflow or launcher PowerShell syntax is invalid.' }
}
$EvidenceDirectory = Join-Path ([IO.Path]::GetTempPath()) 'hai-acceptance-fixture'
Invoke-Expression $inputData.prefix
$expected = [IO.Path]::GetFullPath($EvidenceDirectory) + [IO.Path]::DirectorySeparatorChar
if ($prefix -cne $expected) { throw 'Launcher prefix overload changed.' }
if ('/tmp/'.TrimEnd([char]'/') -cne '/tmp') { throw 'Linux separator trim changed.' }
$config = '{"secrets":{},"services":{"backend":{"env_file":[]}},"networks":{"ingress":{},"isolated":{}}}' | ConvertFrom-Json
if (-not $config.PSObject.Properties['secrets'] -or -not $config.services.backend.PSObject.Properties['env_file']) {
    throw 'Empty external configuration properties must be present, not truth-tested by value.'
}
if ((($config.networks.PSObject.Properties.Name | Sort-Object) -join ',') -cne 'ingress,isolated') {
    throw 'JSON network property enumeration changed.'
}
Write-Output 'PowerShell syntax, path overloads and JSON property checks passed.'
"""
        result = subprocess.run(
            [shutil.which("pwsh"), "-NoProfile", "-NonInteractive", "-Command", harness],
            input=json.dumps({"programs": programs, "prefix": prefix.group(1)}),
            text=True, capture_output=True, check=True, timeout=20,
        )
        self.assertIn("PowerShell syntax, path overloads and JSON property checks passed.", result.stdout)

    @unittest.skipUnless(shutil.which("node"), "Node is required to execute the workflow test-process contract")
    def test_browser_private_process_executes_owned_start_tests_and_redacted_logs(self) -> None:
        result = exercise_browser_program("success")
        self.assertIsNone(result["error"])
        commands = [call["command"] for call in result["calls"]]
        self.assertEqual(commands[:3], ["pwsh", "python3", "npm"])
        start, ready, browser = result["calls"][:3]
        self.assertEqual(start["args"], ["-NoProfile", "-File", str(Path(result["env"]["GITHUB_WORKSPACE"]) / "scripts/isolated-acceptance-stack.ps1"),
                                        "-Action", "Start", "-EvidenceDirectory", result["evidence"]])
        self.assertEqual(ready["args"][-1], "http://127.0.0.1:18080/readyz")
        self.assertEqual(browser["args"], ["test", "--", "--output", str(Path(result["evidence"]) / "browser-results"), "--trace", "off"])
        self.assertEqual(browser["env"]["PLAYWRIGHT_HTML_OUTPUT_DIR"], str(Path(result["evidence"]) / "browser-report"))
        for key, expected in {"E2E_BASE_URL": "http://127.0.0.1:18080", "E2E_OPERATOR_EMAIL": "e2e-owner@example.test",
                              "E2E_OPERATOR_PASSWORD": result["password"], "E2E_ALLOW_MUTATION": "true", "E2E_ISOLATED_STACK": "true"}.items():
            self.assertEqual(browser["env"][key], expected)
        self.assertIn("::add-mask::" + result["password"], result["output"])
        self.assertIn("::add-mask::" + result["key"], result["output"])
        public_output = "\n".join(line for line in result["output"] if not line.startswith("::add-mask::"))
        self.assertNotIn(result["password"], public_output)
        self.assertNotIn(result["key"], public_output)
        docker_calls = [call["args"] for call in result["calls"] if call["command"] == "docker"]
        self.assertEqual(docker_calls[0], ["ps", "-aq", "--filter", "label=hai.acceptance.owner=" + result["owner"]])
        self.assertEqual([call[0] for call in docker_calls], ["ps", "inspect", "inspect", "logs", "logs"])
        self.assertEqual(len(result["files"]), 2)
        for filename, content in result["files"].items():
            self.assertEqual(Path(filename).parent, Path(result["evidence"]) / "owner-logs")
            self.assertNotIn(result["password"], content)
            self.assertNotIn(result["key"], content)
            self.assertIn("[REDACTED]", content)

    @unittest.skipUnless(shutil.which("node"), "Node is required to execute the workflow test-process contract")
    def test_browser_private_process_fails_closed_on_foreign_resources_and_unsafe_mounts(self) -> None:
        for scenario in ("bad-manifest", "wrong-mode", "bad-port", "wrong-owner", "persistent-volume", "foreign-bind", "writable-bind"):
            with self.subTest(scenario=scenario):
                result = exercise_browser_program(scenario)
                self.assertIsNotNone(result["error"])
                self.assertEqual(result["files"], {})
                self.assertFalse(any(call["command"] == "docker" and call["args"][0] == "logs" for call in result["calls"]))
                if scenario in ("bad-manifest", "wrong-mode", "bad-port"):
                    self.assertEqual(result["calls"], [])

    @unittest.skipUnless(shutil.which("node"), "Node is required to execute the workflow test-process contract")
    def test_browser_private_process_retains_failures_and_collects_only_owned_logs(self) -> None:
        for scenario in ("start-failure", "readiness-failure", "browser-failure", "logs-failure"):
            with self.subTest(scenario=scenario):
                result = exercise_browser_program(scenario)
                self.assertIsNotNone(result["error"])
                self.assertTrue(any(call["command"] == "docker" and call["args"][0] == "inspect" for call in result["calls"]))
                if scenario == "start-failure":
                    self.assertFalse(any(call["command"] in ("python3", "npm") for call in result["calls"]))
                elif scenario == "readiness-failure":
                    self.assertFalse(any(call["command"] == "npm" for call in result["calls"]))
        result = exercise_browser_program("empty")
        self.assertIsNone(result["error"])
        self.assertEqual(result["files"], {})

    @unittest.skipUnless(shutil.which("node"), "Node is required to extract the executed readiness program")
    def test_browser_readiness_accepts_only_serving_gateway_states(self) -> None:
        result = exercise_browser_program("success")
        ready = next(call for call in result["calls"] if call["command"] == "python3")
        code = compile(ready["args"][1], "browser-ci-readiness", "exec")
        for status in ("ready", "degraded", "not_ready", "invalid-json", "unreachable"):
            with self.subTest(status=status):
                def response(*args, **kwargs):
                    if status == "unreachable":
                        raise OSError("Synthetic connection failure")
                    return io.StringIO("{" if status == "invalid-json" else json.dumps({"status": status}))

                with patch("sys.argv", ["-c", ready["args"][-1]]), patch("urllib.request.urlopen", side_effect=response) as request, patch("time.sleep"):
                    expected = SystemExit if status != "not_ready" else AssertionError
                    with self.assertRaises(expected) as raised:
                        exec(code, {})
                    if status in ("ready", "degraded"):
                        self.assertEqual(raised.exception.code, 0)
                    elif status != "not_ready":
                        self.assertNotEqual(raised.exception.code, 0)
                    self.assertEqual(request.call_count, 60 if status in ("invalid-json", "unreachable") else 1)
                    request.assert_called_with("http://127.0.0.1:18080/readyz", timeout=5)

    def test_browser_acceptance_environment_file_is_not_trackable(self) -> None:
        gitignore = (ROOT / ".gitignore").read_text(encoding="utf-8")
        self.assertIn(
            ".env.browser-acceptance",
            gitignore,
            "a local reproduction of browser acceptance must not expose its generated credentials to git status",
        )

    def test_browser_acceptance_requires_an_explicit_mutation_opt_in(self) -> None:
        acceptance_test = (
            ROOT / "frontend" / "e2e" / "tests" / "acceptance.spec.ts"
        ).read_text(encoding="utf-8")
        self.assertIn(
            "process.env.E2E_ALLOW_MUTATION === 'true'", acceptance_test
        )
        self.assertIn("!password || !allowMutation", acceptance_test)
        self.assertIn("only for an isolated acceptance stack", acceptance_test)
        self.assertIn("assertIsolatedAcceptanceTarget(baseURL", acceptance_test)

    def test_browser_route_checks_cover_shared_progressive_controls_without_writes(self) -> None:
        route_tests = (ROOT / "frontend" / "e2e" / "tests" / "control-room.spec.ts").read_text(encoding="utf-8")
        for contract in ("HAI_MODULES", "[375, 768, 1024, 1440]", "assertIsolatedAcceptanceTarget(baseURL",
                         "route.abort('blockedbyclient')", "unexpectedWrites", "runtimeErrors",
                         "aria-pressed", "Skip to content", "toBeFocused", "#product-stack"):
            self.assertIn(contract, route_tests)
        self.assertIn("npm run typecheck", job_block("browser-acceptance"))

    def test_idp_rotation_races_require_marked_real_redis_and_pass_markers(self) -> None:
        identity = job_block("idp")
        for contract in ("redis:7-alpine", "HAI_TEST_REDIS_ADDR: 127.0.0.1:6379",
                         "HAI_TEST_REDIS_INSTANCE:", "hai-disposable-test-instance",
                         "set -euo pipefail", "go test -count=3 -race -run '^TestRedis'",
                         "TestRedisSessionRefreshConcurrentCookiesAndLogout",
                         "TestRedisHTTPSessionCookieChain", "TestRedisHTTPSessionAuthorityLoss",
                         "TestRedisHTTPSessionStoreUnavailable",
                         "TestRedisSessionLogoutRacesRotation", "TestRedisRefreshReplayAfterGraceRevokesDescendants",
                         "TestRedisSessionStoreLossDoesNotReviveLoggedOutJWTs",
                         "TestRedisSessionStoreLossDoesNotReviveRotatedJWTs",
                         "TestRedisSessionStoreLossOfPositiveAuthorityDeniesCurrentAndCachedJWTs",
                         "TestRedisSessionStoreLossFreshPasswordLoginRestoresOnlyNewFamily",
                         "TestRedisSessionScriptPartialWriteFailureFailsClosed",
                         "TestRedisSessionScriptLaterWriteFailureFailsClosed",
                         "TestRedisRotationCannotExtendShorterParentAuthority",
                         '^--- PASS: ${test_name} ('):
            self.assertIn(contract, identity)

    def test_idp_durable_sessions_require_disposable_postgres_redis_and_all_real_passes(self) -> None:
        identity = job_block("idp")
        self.assertIn("runs-on: ubuntu-latest", identity)
        self.assertIn("working-directory: idp", identity)
        self.assertNotRegex(identity, r"(?m)^    container:")
        service = re.search(
            r"(?ms)^      postgres:\n(.*?)(?=^      [A-Za-z0-9_-]+:|^    defaults:)",
            identity,
        )
        self.assertIsNotNone(service)
        for contract in (
            "image: postgres:17-alpine",
            "POSTGRES_USER: postgres",
            "POSTGRES_PASSWORD: postgres",
            "POSTGRES_DB: hai_idp_auth_test",
            "- 127.0.0.1:5432:5432",
            '--health-cmd "pg_isready -U postgres -d hai_idp_auth_test"',
            "--health-interval 5s", "--health-timeout 5s", "--health-retries 12",
        ):
            with self.subTest(service_contract=contract):
                self.assertIn(contract, service.group(1))
        self.assertNotIn("volumes:", service.group(1))
        self.assertNotIn("${{ secrets.", service.group(1))
        regression = re.search(
            r"(?ms)^      - name: Durable session PostgreSQL and Redis real-store regressions\n(.*?)(?=^      - |\Z)",
            identity,
        )
        rotation = re.search(
            r"(?ms)^      - name: Redis session rotation and revocation races\n(.*?)(?=^      - |\Z)",
            identity,
        )
        self.assertIsNotNone(regression)
        self.assertIsNotNone(rotation)
        step = regression.group(1)
        instance = "HAI_TEST_REDIS_INSTANCE: hai-idp-ci-${{ github.run_id }}-${{ github.run_attempt }}"
        marker = 'docker exec "${{ job.services.redis.id }}" redis-cli SET hai-disposable-test-instance "$HAI_TEST_REDIS_INSTANCE"'
        command = "go test -count=1 -race -p=1 -parallel=1 -timeout=300s -run '^TestDurableSession' ./internal/app/authentication -v"
        rejection = "if grep -Eq -- '^[[:space:]]*--- (SKIP|FAIL):|^FAIL([[:space:]]|$)' \"$durable_log\"; then"
        for contract in (
            "shell: bash",
            'HAI_IDP_TEST_DATABASE_URL: "postgres://postgres:postgres@127.0.0.1:5432/hai_idp_auth_test?sslmode=disable"',
            'HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS: "true"',
            "HAI_TEST_REDIS_ADDR: 127.0.0.1:6379", instance,
            "set -euo pipefail", marker,
            'durable_log="$(mktemp "$RUNNER_TEMP/idp-durable-session.XXXXXX.log")"',
            command + ' 2>&1 | tee "$durable_log"',
            rejection,
            'echo "Durable session regressions reported SKIP or FAIL" >&2\n            exit 1\n          fi',
            'grep -q -- "^--- PASS: ${test_name} (" "$durable_log"',
        ):
            with self.subTest(step_contract=contract):
                self.assertIn(contract, step)
        self.assertIn(instance, rotation.group(1))
        self.assertIn(marker, rotation.group(1))
        self.assertIn("go test -count=3 -race -run '^TestRedis'", rotation.group(1))
        self.assertLess(rotation.start(), regression.start())
        self.assertLess(step.index(marker), step.index(command))
        self.assertLess(step.index(command), step.index(rejection))
        self.assertLess(step.index(rejection), step.index("for test_name in"))
        for forbidden in ("continue-on-error", "|| true", "tee -a", "${{ secrets."):
            self.assertNotIn(forbidden, step)
        # Gate the real-store names, not the total PASS count: unit tests may also run.
        names = (
            "TestDurableSessionOldSnapshotCannotUndoLogout",
            "TestDurableSessionOldSnapshotCannotBranchConsumedRefresh",
            "TestDurableSessionRestoredGraceCannotUndoReplayRevocation",
            "TestDurableSessionLoginRegistrationFaultCannotBeRestored",
            "TestDurableSessionCommittedRefreshRecoversOnlyCanonicalPair",
            "TestDurableSessionPostgresWriteFaultDoesNotConsumeOrCallRedis",
            "TestDurableSessionConcurrentStoresReturnOneCommittedPair",
            "TestDurableSessionLogoutAndResetAfterRedisRotationDenyResult",
            "TestDurableSessionLegacyRedisFamilyCannotBackfillPostgres",
            "TestDurableSessionMigrationPreservesRevocationsAndReceipts",
            "TestDurableSessionSchemaFaultNeverRelaxesAuthority",
            "TestDurableSessionClosedPostgresPoolDeniesBothTokenTypes",
            "TestDurableSessionUserLockPrecedesFamilyAndSeesReset",
        )
        source = (ROOT / "idp/internal/app/authentication/durable_session_postgres_redis_test.go").read_text(encoding="utf-8")
        self.assertEqual(tuple(re.findall(r"^func (TestDurableSession\w+)\(t \*testing.T\)", source, re.M)), names)
        self.assertEqual(tuple(re.findall(r"^            (TestDurableSession\w+)(?: \\)?$", step, re.M)), names)
        self.assertRegex(step, r'(?s)for test_name in \\\n.*?\n          do\n            grep -q -- "\^--- PASS: \$\{test_name\} \(" "\$durable_log"\n          done')

    def test_native_runtime_gate_requires_actual_windows_execution_and_evidence(self) -> None:
        native = job_block("windows-native-runtime")
        for contract in ("runs-on: windows-latest", 'go-version: "1.27.2"',
                         "go mod download", "./scripts/test-windows-native-runtime.ps1",
                         "if: always()", "acceptance-evidence.json", "native-tests.log"):
            self.assertIn(contract, native)
        self.assertNotIn("-CompileOnly", native)
        self.assertNotIn("-CrossCompileDocker", native)
        runner = (ROOT / "scripts/test-windows-native-runtime.ps1").read_text(encoding="utf-8")
        for contract in ("EnvironmentVariables.Clear()", "nativeExecuted", "nativePassed",
                         "TestWindowsNativeAcceptance", "--- SKIP:", "--- PASS:"):
            self.assertIn(contract, runner)

    def test_postgres_reminder_revocation_is_not_silently_skipped(self) -> None:
        migrations = job_block("migrations-integration")
        self.assertNotIn("host=localhost", migrations)
        self.assertIn("-run '^TestPostgresReminderRevocationAfterPreparationExpiryAndSourceClosure$'", migrations)
        self.assertIn("grep -q -- '^--- PASS: TestPostgresReminderRevocationAfterPreparationExpiryAndSourceClosure ('", migrations)

    def test_historical_outcome_evaluations_require_three_real_postgres_passes(self) -> None:
        migrations = job_block("migrations-integration")
        for contract in ("hai_outcome_evaluation_test", "HAI_OUTCOME_EVALUATION_TEST_DATABASE_DSN:",
                         "migrations/pre/0023_outcome_resilience_ledgers.up.sql", "--set=ON_ERROR_STOP=1",
                         "go test -count=3 -race -run '^TestPostgres' ./internal/outcomeevaluation/",
                         "TestPostgresPinnedHistoricalEvaluationReplay",
                         "TestPostgresRowDecodersRevalidateScopeMetadataAndAuditDigests",
                         'test "$(grep -c -- "^--- PASS: ${test_name} (" "$outcome_log")" -eq 3',
                         "! grep -q -- '^--- SKIP:'"):
            self.assertIn(contract, migrations)

    def test_atomic_reminders_require_real_postgres_pass(self) -> None:
        migrations = job_block("migrations-integration")
        for contract in ("HAI_TEST_DATABASE_DSN=\"$migration_dsn\" go test -count=1 -race",
                         "-run '^TestPostgresReminderDeliveryAtomicReplay$'",
                         "grep -q -- '^--- PASS: TestPostgresReminderDeliveryAtomicReplay ('"):
            self.assertIn(contract, migrations)

    def test_atomic_claim_recovery_requires_guarded_real_postgres_pass(self) -> None:
        migrations = job_block("migrations-integration")
        for contract in ('HAI_TEST_DATABASE_DSN="$migration_dsn" go test -count=1 -race -p 1 -parallel 1 -timeout 120s',
                         "-run '^TestPostgresClaimRecoveryStateAndHistoryAtomic$'",
                         "grep -q -- '^--- PASS: TestPostgresClaimRecoveryStateAndHistoryAtomic ('"):
            self.assertIn(contract, migrations)
        source = (ROOT / "backend/internal/workflow/recovery_atomic_postgres_test.go").read_text(encoding="utf-8")
        self.assertLess(source.index("pgtestguard.RequireDedicatedPostgresTestDSN"), source.index("gorm.Open"))
        self.assertLess(source.index("pgtestguard.RequireDedicatedPostgresTestDSN"), source.index("infra.RunMigrations"))
        for contract in ('"hai_migration_runner_test"', '"history_failure"', '"cancel_after_history_write"',
                         '"live_lease"', '"archived_parent"', '"workflow_transitions"', '"workflow_events"', '"workflow_decisions"'):
            self.assertIn(contract, source)

    def test_followup_atomicity_requires_owned_database_and_all_pass_markers(self) -> None:
        migrations = job_block("migrations-integration")
        for contract in ("hai_workflow_followup_test",
                         'HAI_WORKFLOW_FOLLOWUP_TEST_DATABASE_OWNED: "true"',
                         "go test -count=1 -race -run '^TestPostgresFollowUpProjection'",
                         "TestPostgresFollowUpProjectionRollbackAtEveryWriteAndReplay",
                         "TestPostgresFollowUpProjectionConcurrentWorkersAndClaimRecovery",
                         "TestPostgresFollowUpProjectionConcurrentSchedulerClaims",
                         "TestPostgresFollowUpProjectionLeaseExpiryAfterLockWait",
                         "TestPostgresFollowUpProjectionExpiryDuringWritesRollsBack",
                         "TestPostgresFollowUpProjectionSerializesWithRetractionAndApproval",
                         "TestPostgresFollowUpProjectionRejectsAmbiguousLegacyAdoption",
                         "TestPostgresFollowUpProjectionPreservesAdoptedLegacyArtifactsForLaterLoop",
                         "TestPostgresFollowUpProjectionDistinctLoopsWithIdenticalText",
                         "TestPostgresFollowUpProjectionEmergencyStopAfterClaim",
                         "TestPostgresFollowUpProjectionRejectsNestedTransaction",
                         '^--- PASS: ${test_name} ('):
            self.assertIn(contract, migrations)

    def test_browser_acceptance_waits_for_real_source_ingestion(self) -> None:
        prepare = browser_step("Prepare owned browser acceptance stack")
        self.assertIn("-Action Prepare -ExecutionMode manual-local", prepare)
        launcher = (ROOT / "scripts/isolated-acceptance-stack.ps1").read_text(encoding="utf-8")
        for contract in ("$values[$key] = 'false'", "SOURCE_SCHEDULER_ENABLED = 'false'",
                         "$values['SOURCE_MANUAL_WORKER_ENABLED'] = if ($ExecutionMode -eq 'manual-local') { 'true' } else { 'false' }",
                         "$values['SOURCE_WORKER_POLL_SECONDS'] = '15'",
                         "if ($ExecutionMode -eq 'manual-local') { $values['HAI_PHASE2_MODE'] = 'autonomous_safe' }",
                         "LLM_PROVIDERS_JSON = '[]'", "Join-Path $EvidenceDirectory 'sources/acceptance.txt'"):
            self.assertIn(contract, launcher)
        browser = (ROOT / "frontend" / "e2e" / "tests" / "acceptance.spec.ts").read_text(encoding="utf-8")
        for contract in ('submitted.status()', 'manual_async_sync', 'await expect.poll',
                         'current.sourceId', ").toBe('completed')", 'fixture?.text',
                         'Synthetic HAI acceptance source. No personal records.'):
            self.assertIn(contract, browser)

    def test_workflow_claim_lease_expiry_postgres_is_dedicated_guarded_and_requires_pass(self) -> None:
        migrations = job_block("migrations-integration")
        creation = re.search(
            r"(?ms)^      - name: Create isolated integration databases\n(.*?)(?=^      - |\Z)",
            migrations,
        )
        self.assertIsNotNone(creation)
        self.assertIn("hai_workflow_lease_expiry_test", creation.group(1))
        self.assertIn("createdb", creation.group(1))
        self.assertIn("--host=127.0.0.1", creation.group(1))

        regression = re.search(
            r"(?ms)^      - name: Workflow claim lease-expiry PostgreSQL regression\n(.*?)(?=^      - |\Z)",
            migrations,
        )
        self.assertIsNotNone(regression)
        step = regression.group(1)
        for contract in (
            'HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS: "true"',
            'HAI_WORKFLOW_LEASE_EXPIRY_TEST_DSN: "host=127.0.0.1 user=postgres password=postgres dbname=hai_workflow_lease_expiry_test port=5432 sslmode=disable TimeZone=UTC"',
            "set -euo pipefail",
            'lease_expiry_log="$RUNNER_TEMP/workflow-claim-lease-expiry-postgres.log"',
            "go test -count=1 -run '^TestPostgresClaimLeaseExpiryRenewal$'",
            './internal/workflow/ -v | tee "$lease_expiry_log"',
            "grep -q -- '^--- PASS: TestPostgresClaimLeaseExpiryRenewal (' \"$lease_expiry_log\"",
        ):
            with self.subTest(contract=contract):
                self.assertIn(contract, step)
        self.assertLess(creation.start(), regression.start())
        self.assertNotIn("HAI_TEST_DATABASE_DSN", step)
        self.assertNotIn("migrate", step)
        self.assertNotIn("DB_AUTOMIGRATE", step)

        source = (ROOT / "backend/internal/workflow/claim_lease_expiry_postgres_test.go").read_text(encoding="utf-8")
        self.assertIn("RequireDedicatedPostgresTestDSN(t,", source)
        self.assertIn('"HAI_WORKFLOW_LEASE_EXPIRY_TEST_DSN", "hai_workflow_lease_expiry_test"', source)
        self.assertIn('SET LOCAL search_path = pg_temp', source)
        self.assertIn("CREATE TEMP TABLE workflow_items", source)
        self.assertIn("CREATE TEMP TABLE workflow_open_loops", source)
        self.assertIn("ON COMMIT DROP", source)
        self.assertIn("defer tx.Rollback()", source)

    def test_postgres_jobs_cannot_silently_skip_or_match_no_tests(self) -> None:
        migrations = job_block("migrations-integration")
        for contract in (
            "hai_migration_runner_test",
            "hai_framework_registry_test",
            "hai_task_state_test",
            "hai_agentregistry_test",
            "hai_resilience_test",
            "hai_brain_skill_selection_test",
            "hai_evaluation_test",
            "hai_durablejob_test",
            "createdb",
            'HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS: "true"',
            'HAI_REQUIRE_POSTGRES_INTEGRATION: "true"',
            'HAI_TEST_DATABASE_DSN="$migration_dsn" go test -count=1 -tags integration',
            'HAI_TEST_DATABASE_DSN="$registry_dsn" go test -count=1 -tags integration',
            'HAI_TEST_DATABASE_DSN="$task_dsn" go test -count=1 -tags integration',
            "^--- PASS: TestRunMigrationsAppliesAndIsIdempotent",
            "^--- PASS: TestRollbackMigrationReversesPostMigration",
            "^--- PASS: TestConcurrentMigrationRunnersSerializeAndRecheck",
            "^--- PASS: TestLegacyBaselineRejectsDifferentExistingPrimaryKey",
            "^--- PASS: TestFrameworkRegistryPostgresIntegrationRequiredEnvironment",
            "^--- PASS: TestFrameworkRegistryPostgresMigrationApplyRollbackAndRerun",
            "^--- PASS: TestFrameworkRegistryPostgresConstraintsAndImmutability",
            "^--- PASS: TestPostgresTaskStateRepositoryDurabilityOwnerScopeAndImmutability",
            "^--- PASS: TestPostgresRepositoryRoundTripOwnerIsolationCASAndImmutableLedgers",
            "^--- PASS: TestPostgresAgentRegistryMigrationCanReplayAgainstExistingSchema",
        ):
            with self.subTest(contract=contract):
                self.assertIn(contract, migrations)
        self.assertIn(
            "-run '^TestPostgresTaskStateRepositoryDurabilityOwnerScopeAndImmutability$'",
            migrations,
        )
        database_assignments = dict(
            re.findall(
                r'^\s*(migration|registry|task|agent_registry|resilience|brain_skill_selection|evaluation|durablejob)_dsn="[^"]*dbname=([^ "\n]+)',
                migrations,
                re.MULTILINE,
            )
        )
        self.assertEqual(
            database_assignments,
            {
                "migration": "hai_migration_runner_test",
                "registry": "hai_framework_registry_test",
                "task": "hai_task_state_test",
                "agent_registry": "hai_agentregistry_test",
                "resilience": "hai_resilience_test",
                "brain_skill_selection": "hai_brain_skill_selection_test",
                "evaluation": "hai_evaluation_test",
                "durablejob": "hai_durablejob_test",
            },
        )
        self.assertEqual(len(set(database_assignments.values())), 8)

    def test_brain_skill_selection_database_is_migrated_before_tests(self) -> None:
        migrations = job_block("migrations-integration")
        preparation = 'DB_NAME=hai_brain_skill_selection_test DB_AUTOMIGRATE=false'
        execution = 'HAI_BRAIN_SKILL_SELECTION_TEST_DATABASE_DSN="$brain_skill_selection_dsn" go test'
        self.assertIn(preparation, migrations)
        self.assertIn(f'{execution} -count=1 -tags integration', migrations)
        self.assertIn('go run ./cmd/main.go migrate up', migrations)
        self.assertLess(migrations.index(preparation), migrations.index(execution))

    def test_durable_recovery_runs_on_dedicated_database_with_required_passes(self) -> None:
        migrations = job_block("migrations-integration")
        self.assertIn('HAI_DURABLEJOB_TEST_DATABASE_DSN="$durablejob_dsn" go test -count=1 -tags integration', migrations)
        for name in (
            'TestRecurringReaperCreatesOneSuccessorAndRollsBackAtomically',
            'TestExpiredLeaseOnFinalDeliveryDeadLettersWithoutReexecution',
            'TestReclaimedLeaseFencesStaleWorkerCompletion',
            'TestSingletonSchedulingIsAtomicPerQueueAndKind',
        ):
            self.assertIn(name, migrations)
        self.assertIn('grep -q -- "^--- PASS: ${test_name} (" "$durablejob_log"', migrations)
        for file_name in ('integration_test.go', 'reaper_recurring_integration_test.go'):
            source = (ROOT / 'backend/internal/durablejob' / file_name).read_text(encoding='utf-8')
            self.assertIn('RequireDedicatedPostgresTestDSN(t, "HAI_DURABLEJOB_TEST_DATABASE_DSN", "hai_durablejob_test")', source)
            self.assertNotIn('os.Getenv("HAI_TEST_DATABASE_DSN")', source)

    def test_trello_reconciliation_0109_runs_both_integration_tests_and_requires_pass(self) -> None:
        migrations = job_block("migrations-integration")
        test_names = (
            "TestTrelloWebhookReconciliationGenerationMigrationBackfillAndRollback",
            "TestTrelloWebhookReconciliationGenerationMigrationLocksOutConcurrentPendingReceipt",
        )
        selection = "|".join(test_names)
        self.assertIn(
            'HAI_TEST_DATABASE_DSN="$migration_dsn" go test -count=1 -tags integration \\\n'
            f"            -run '^({selection})$' \\\n"
            '            ./migrations/ -v | tee "$trello_reconciliation_log"',
            migrations,
        )
        self.assertIn(
            'trello_reconciliation_log="$RUNNER_TEMP/trello-reconciliation-0109-postgres.log"',
            migrations,
        )
        source = (
            ROOT / "backend" / "migrations" /
            "trello_webhook_reconciliation_generations_integration_test.go"
        ).read_text(encoding="utf-8")
        self.assertTrue(source.startswith("//go:build integration\n"))
        for name in test_names:
            with self.subTest(test=name):
                self.assertIn(f"func {name}(t *testing.T)", source)
                self.assertIn(
                    f"grep -q -- '^--- PASS: {name} (' "
                    '"$trello_reconciliation_log"',
                    migrations,
                )
        self.assertIn("set -euo pipefail", migrations)

    def test_source_configuration_postgres_runs_all_enrollment_races_without_skips(self) -> None:
        migrations = job_block("migrations-integration")
        self.assertIn('HAI_TEST_DATABASE_DSN="$migration_dsn" go test -count=1 -p 1 -parallel 1 -timeout 5m', migrations)
        self.assertIn("-run '^TestSourceConfigurationPostgres'", migrations)
        self.assertIn('./migrations/ -v | tee "$source_configuration_log"', migrations)
        self.assertIn('grep -q -- "^--- PASS: ${test_name} (" "$source_configuration_log"', migrations)
        self.assertIn('grep -q -- "^[[:space:]]*--- PASS: ${case_name} (" "$source_configuration_log"', migrations)
        for contract in (
            'TestSourceConfigurationPostgresCanonicalRevocationAndRawMutationRefusal',
            'TestSourceConfigurationPostgresEnrollmentConcurrency',
            'for isolation in read_committed repeatable_read',
            'for enrollment in insert update_unmanaged',
            'for schedule in mutation_first enrollment_first',
            'for mutation in rename disable delete',
            'for truncate_case in standalone_feed feed_and_audits',
            'TestSourceConfigurationPostgresEnrollmentConcurrency/repeatable_read_truncate/${truncate_case}',
            'TestSourceConfigurationPostgresEnrollmentConcurrency/${isolation}/${enrollment}/${schedule}/${mutation}',
            'set -euo pipefail',
            'HAI_ALLOW_DESTRUCTIVE_DATABASE_TESTS: "true"',
        ):
            self.assertIn(contract, migrations)
        source = (ROOT / 'backend/migrations/source_configuration_concurrency_postgres_test.go').read_text(encoding='utf-8')
        self.assertIn('RequireDedicatedPostgresTestDSN(t, "HAI_TEST_DATABASE_DSN", "hai_migration_runner_test")', source)
        self.assertIn('sql.LevelReadCommitted', source)
        self.assertIn('sql.LevelRepeatableRead', source)
        self.assertIn('pg_blocking_pids', source)
        self.assertNotIn('t.Skip', source)
        self.assertNotIn('t.Parallel', source)

    def test_account_feed_registry_durability_runs_on_its_dedicated_database(self) -> None:
        migrations = job_block("migrations-integration")
        for contract in (
            'hai_account_feed_registry_test',
            'HAI_ACCOUNT_FEED_TEST_DSN="$account_feed_dsn" go test -count=1 -p 1 -parallel 1 -timeout 5m -tags integration',
            "-run '^TestPostgresRegistryCommittedReconstructionAndAtomicFailure$'",
            './internal/accountfeed/ -v | tee "$account_feed_log"',
            'grep -q -- \'^--- PASS: TestPostgresRegistryCommittedReconstructionAndAtomicFailure (\' "$account_feed_log"',
        ):
            self.assertIn(contract, migrations)
        source = (ROOT / 'backend/internal/accountfeed/registry_repository_postgres_integration_test.go').read_text(encoding='utf-8')
        guard = 'RequireDedicatedPostgresTestDSN(t, "HAI_ACCOUNT_FEED_TEST_DSN", "hai_account_feed_registry_test")'
        self.assertLess(source.index(guard), source.index('stdlib.OpenDB(*config)'))
        self.assertIn('infra.ApplyMigrations', source)
        self.assertIn('ownedDatabaseOID', source)
        self.assertNotIn('WITH (FORCE)', source)

    def test_source_configuration_checks_commit_and_immediate_dml_firing(self) -> None:
        migrations = job_block("migrations-integration")
        for contract in (
            'TestSourceConfigurationPostgresEnrollmentFiringModes',
            'for firing in commit immediate_dml',
            'TestSourceConfigurationPostgresEnrollmentFiringModes/${isolation}/${enrollment}/${firing}/${mutation}',
            'grep -q -- "^[[:space:]]*--- PASS: ${case_name} (" "$source_configuration_log"',
        ):
            self.assertIn(contract, migrations)
        source = (ROOT / 'backend/migrations/source_configuration_firing_modes_postgres_test.go').read_text(encoding='utf-8')
        for contract in (
            'func TestSourceConfigurationPostgresEnrollmentFiringModes(t *testing.T)',
            'RequireDedicatedPostgresTestDSN(t, "HAI_TEST_DATABASE_DSN", "hai_migration_runner_test")',
            'sql.LevelReadCommitted',
            'sql.LevelRepeatableRead',
            '"commit", "immediate_dml"',
            'sourceConfigurationRaceAssertFinal',
        ):
            self.assertIn(contract, source)
        self.assertNotIn('t.Skip', source)
        self.assertNotIn('t.Parallel', source)

    def test_source_authority_staged_migrations_require_real_passes(self) -> None:
        migrations = job_block("migrations-integration")
        for contract in (
            "-run '^(TestOperationSourceIdentityPostgres|TestSourceObservationPostgres|TestSourceHeadPostgres)'",
            './migrations/ -v | tee "$source_authority_log"',
            'grep -q -- "^--- PASS: ${test_name} (" "$source_authority_log"',
        ):
            self.assertIn(contract, migrations)
        for filename, name in (
            ('operation_source_identity_postgres_test.go', 'TestOperationSourceIdentityPostgresUpgradeImmutabilityAndRollback'),
            ('source_observation_postgres_test.go', 'TestSourceObservationPostgresUpgradeMintConstraintsAndRollback'),
            ('source_head_postgres_test.go', 'TestSourceHeadPostgresPublicationEpochSupersessionAndRollback'),
        ):
            source = (ROOT / 'backend/migrations' / filename).read_text(encoding='utf-8')
            self.assertIn(f'func {name}(t *testing.T)', source)
            self.assertIn(name, migrations)
            self.assertIn('openIsolatedMigrationDatabase(t)', source)

    def test_trello_repository_requires_guard_before_connection_and_real_passes(self) -> None:
        migrations = job_block("migrations-integration")
        self.assertIn('HAI_TEST_DATABASE_DSN="$migration_dsn" go test -count=1 -race', migrations)
        self.assertIn("-run '^TestTrelloPostgres'", migrations)
        self.assertIn('./internal/source/ -v | tee "$trello_repository_log"', migrations)
        for name in (
            "TestTrelloPostgresFindStaleCardsAfterOrdersPaginatesAndFilters",
            "TestTrelloPostgresOwnerManualRequestResumesCancelledCheckpoint",
            "TestTrelloPostgresInventoryCheckpointUpdatesFetchedAtAndCursorAtomically",
            "TestTrelloPostgresRejectsSourcePausedBeforeDurableOperations",
        ):
            self.assertIn(name, migrations)
        self.assertIn('grep -q -- "^--- PASS: ${test_name} (" "$trello_repository_log"', migrations)
        source = (ROOT / "backend/internal/source/trello_sync_repository_postgres_integration_test.go").read_text(encoding="utf-8")
        guard = 'RequireDedicatedPostgresTestDSN(t, "HAI_TEST_DATABASE_DSN", "hai_migration_runner_test")'
        self.assertLess(source.index(guard), source.index("stdlib.OpenDB(*config)"))
        self.assertLess(source.index("t.Cleanup(func() { _ = sqlDB.Close() })"), source.index("sqlDB.PingContext(ctx)"))
        self.assertIn('databaseName != "hai_migration_runner_test"', source)
        self.assertNotIn("t.Skip", source)
        self.assertNotIn("postgres.Open(dsn)", source)

    def test_running_stack_must_be_live_before_acceptance_test(self) -> None:
        isolation = job_block("isolation-acceptance")
        for contract in (
            'backend_live=""',
            'if ! kill -0 "$(cat backend.pid)" 2>/dev/null; then',
            '[ -n "${backend_live}" ] || {',
            'readyz_http="$(curl -sS -o readyz.json',
            '[ "${readyz_http}" = "200" ] || {',
            'status not in {"ready", "degraded"}',
        ):
            with self.subTest(contract=contract):
                self.assertIn(contract, isolation)
        self.assertNotIn(
            "curl -s http://localhost:8080/readyz || true",
            isolation,
        )

    def test_windows_contract_executes_from_a_space_containing_path(self) -> None:
        windows = job_block("windows-contract")
        for contract in (
            "runs-on: windows-latest",
            r'C:\Program Files\Git\bin\bash.exe',
            '"HAI smoke path with spaces"',
            "cygpath -u",
            'python3() { python "$@"; }',
            "hai_smoke_mint_jwt owner ci-secret windows-owner",
            "python scripts/test_ci_contract.py",
            "python scripts/test_smoke_auth_contract.py",
        ):
            with self.subTest(contract=contract):
                self.assertIn(contract, windows)

    def test_windows_contract_fails_on_each_native_python_exit_code(self) -> None:
        windows = job_block("windows-contract")
        commands = (
            ("python scripts/test_ci_contract.py", "CI workflow contract"),
            ("python scripts/test_smoke_auth_contract.py", "Smoke authentication contract"),
            ("python nginx-config/test_gateway_contract.py", "Gateway contract"),
        )
        for command, message in commands:
            with self.subTest(command=command):
                self.assertRegex(
                    windows,
                    rf"(?m)^\s*{re.escape(command)}\s*\n\s*if \(\$LASTEXITCODE -ne 0\) \{{ throw \"{re.escape(message)} failed with exit code \$LASTEXITCODE\" \}}",
                )

    def test_smoke_aggregator_rejects_zero_or_missing_assertions(self) -> None:
        aggregator = (ROOT / "scripts" / "smoke-all.sh").read_text(
            encoding="utf-8"
        )
        self.assertIn(
            r"grep -E '^==> Result: [1-9][0-9]* passed, 0 failed$'",
            aggregator,
        )
        self.assertIn(
            'if [ "${code}" -eq 0 ] && [ -n "${valid_line}" ]; then',
            aggregator,
        )
        self.assertIn(
            'line="${reported_line:-==> Result: missing or invalid}"',
            aggregator,
        )

    def test_ci_never_uploads_generated_runtime_or_secret_artifacts(self) -> None:
        self.assertIn("actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9 # v7.0.2", WORKFLOW)
        self.assertIn("name: hai-windows-installer-preview", WORKFLOW)
        self.assertIn("path: installer/release/HAI-Setup-*.exe", WORKFLOW)
        self.assertNotIn("installer/release/payload", WORKFLOW)
        self.assertNotIn("payload-manifest.json", WORKFLOW)
        self.assertNotRegex(WORKFLOW, r"(?i)\bupload[\w -]*(?:log|env|secret)")

    def test_ci_checkout_does_not_persist_tokens_for_executed_source(self) -> None:
        checkout_ref = r"actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1"
        blocks = re.findall(
            rf"(?ms)^      - uses: {re.escape(checkout_ref)}\n(.*?)(?=^      - |\Z)",
            WORKFLOW,
        )
        self.assertGreater(len(blocks), 0)
        for block in blocks:
            self.assertIn(
                "persist-credentials: false",
                block,
                "every checkout runs source and must not persist GITHUB_TOKEN",
            )

    def test_installer_preview_build_and_upload_are_clean_and_trusted_only(self) -> None:
        installer = job_block("windows-installer")
        build = re.search(
            r"(?ms)^      - name: Build unsigned Windows preview\n(.*?)(?=^      - |\Z)",
            installer,
        )
        self.assertIsNotNone(build)
        build_body = build.group(1)
        self.assertIn("git status --porcelain=v1 --untracked-files=all", build_body)
        self.assertIn("$LASTEXITCODE -ne 0 -or $status.Count -ne 0", build_body)
        self.assertLess(build_body.index("git status"), build_body.index("build-windows-installer.ps1"))

        upload = re.search(
            r"(?ms)^      - name: Upload unsigned Windows preview\n(.*?)(?=^      - |\Z)",
            installer,
        )
        self.assertIsNotNone(upload)
        upload_body = upload.group(1)
        self.assertIn("if: github.event_name == 'push' && github.ref == 'refs/heads/main'", upload_body)
        self.assertIn("if-no-files-found: error", upload_body)
        self.assertIn("test-windows-installer-install.ps1", installer)
        self.assertNotIn("-AllowDirtyWorktree", installer)

    def test_windows_installer_ci_compiles_the_distributable(self) -> None:
        installer = job_block("windows-installer")
        for contract in (
            "runs-on: windows-latest",
            "choco install innosetup --yes --no-progress",
            "build-windows-installer.ps1 -Version",
            "actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9 # v7.0.2",
            "retention-days: 14",
        ):
            with self.subTest(contract=contract):
                self.assertIn(contract, installer)

    def test_windows_installer_ci_checks_signing_and_labels_unsigned_preview(self) -> None:
        installer = job_block("windows-installer")
        self.assertIn("name: Windows installer preview and signing guards", installer)
        self.assertIn("name: hai-windows-installer-preview\n", installer)
        self.assertIn("name: Upload unsigned Windows preview", installer)
        guard = re.search(
            r"(?ms)^      - name: Production signing guards on both PowerShell engines\n(.*?)(?=^      - |\Z)",
            installer,
        )
        self.assertIsNotNone(guard)
        body = guard.group(1)
        self.assertIn("shell: pwsh", body)
        self.assertIn("./scripts/test-windows-installer-signing.ps1", body)
        self.assertIn("powershell -NoProfile -ExecutionPolicy Bypass -File ./scripts/test-windows-installer-signing.ps1", body)
        self.assertIn("if ($LASTEXITCODE -ne 0)", body)
        self.assertNotIn("-Production", installer)
        self.assertNotIn("SigningCertificateThumbprint", installer)
        self.assertLess(installer.index("Production signing guards"), installer.index("Build unsigned Windows preview"))

    def test_every_job_has_an_explicit_timeout(self) -> None:
        for job_id in (
            "python-runners",
            "secret-scan",
            "windows-installer",
            "backend",
            "idp",
            "nginx-config-manager",
            "frontend",
            "browser-acceptance",
            "compose",
            "authenticated-smoke",
            "migrations-integration",
            "isolation-acceptance",
            "windows-contract",
        ):
            with self.subTest(job=job_id):
                self.assertIn("timeout-minutes:", job_block(job_id))

    def test_external_actions_are_pinned_to_reviewed_full_shas(self) -> None:
        uses_lines = re.findall(r"(?m)^\s*uses:\s*(.+)$", WORKFLOW)
        for raw_reference in uses_lines:
            reference = raw_reference.strip()
            if reference.startswith(("./", "docker://")):
                continue
            self.assertRegex(
                reference,
                r"^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+@[0-9a-f]{40}\s+#\s+v\d+\.\d+\.\d+$",
                f"external action must use a reviewed full commit SHA: {reference}",
            )

        reviewed = {
            "actions/checkout": "3d3c42e5aac5ba805825da76410c181273ba90b1",
            "actions/setup-go": "b7ad1dad31e06c5925ef5d2fc7ad053ef454303e",
            "actions/setup-node": "949feb2413d6458794dcd2491c4babbbce0c15c1",
            "actions/setup-python": "5fda3b95a4ea91299a34e894583c3862153e4b97",
            "actions/upload-artifact": "cf430e030ddbb5b0abf93d22962f4752f3646cd9",
        }
        for action, sha in reviewed.items():
            self.assertIn(f"{action}@{sha}", WORKFLOW)

    def test_redacted_secret_scan_is_a_read_only_ci_job(self) -> None:
        job = job_block("secret-scan")
        self.assertIn("permissions:\n      contents: read", job)
        self.assertIn("fetch-depth: 0", job)
        self.assertIn("scripts/run-secret-scan.py", job)
        self.assertNotIn("secrets.", job)
        scanner = (ROOT / "scripts" / "run-secret-scan.py").read_text(encoding="utf-8")
        self.assertIn("--redact=100", scanner)
        self.assertIn("capture_output=True", scanner)
        self.assertNotIn("stdout=", scanner)


if __name__ == "__main__":
    unittest.main()
