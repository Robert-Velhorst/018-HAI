import { NgModule } from "@angular/core";
import { RouterModule, Routes } from "@angular/router";
import { authGuard } from "./services/auth/guards/auth.guard";
import { RedirectIfLoggedGuard } from "./services/auth/guards/login.guard";
import { AppShellComponent } from './control-room/app-shell.component';

export const AUTHENTICATED_ROUTES: Routes = [
  {
    path: "home",
    loadChildren: () =>
      import("./pages/home/home.module").then((m) => m.HomeModule),
  },
  {
    path: "command-dashboard",
    loadChildren: () =>
      import("./pages/command-dashboard/command-dashboard.module").then(
        (m) => m.CommandDashboardModule
      ),
  },
  {
    path: "ambient-brain",
    loadChildren: () =>
      import("./pages/ambient-brain/ambient-brain.module").then(
        (m) => m.AmbientBrainModule
      ),
  },
  {
    path: "hai-os",
    loadChildren: () =>
      import("./pages/hai-os/hai-os.module").then((m) => m.HAIOSModule),
  },
  {
    path: "life-ops",
    loadChildren: () =>
      import("./pages/life-ops/life-ops.module").then(
        (m) => m.LifeOpsModule
      ),
  },
  {
    path: "framework-registry",
    loadChildren: () =>
      import("./pages/framework-registry/framework-registry.module").then(
        (m) => m.FrameworkRegistryModule
      ),
  },
  {
    path: "control-center",
    loadChildren: () =>
      import("./pages/control-center/control-center.module").then(
        (m) => m.ControlCenterModule
      ),
  },
  {
    path: "llm-policy",
    loadChildren: () =>
      import("./pages/llm-policy/llm-policy.module").then(
        (m) => m.LLMPolicyModule
      ),
  },
  {
    path: "memory",
    loadChildren: () =>
      import("./pages/memory/memory.module").then((m) => m.MemoryModule),
  },
  {
    path: "operational-brain",
    loadChildren: () =>
      import("./pages/operational-brain/operational-brain.module").then(
        (m) => m.OperationalBrainModule
      ),
  },
  {
    path: "task-blueprint",
    loadChildren: () =>
      import("./pages/task-blueprint/task-blueprint.module").then(
        (m) => m.TaskBlueprintModule
      ),
  },
  {
    path: "connected-sources",
    loadChildren: () =>
      import("./pages/connected-sources/connected-sources.module").then(
        (m) => m.ConnectedSourcesModule
      ),
  },
  {
    path: "grounded-answers",
    loadChildren: () =>
      import("./pages/grounded-answers/grounded-answers.module").then(
        (m) => m.GroundedAnswersModule
      ),
  },
  {
    path: "workflow-engine",
    loadChildren: () =>
      import("./pages/workflow-engine/workflow-engine.module").then(
        (m) => m.WorkflowEngineModule
      ),
  },
  {
    path: "pursuits",
    loadChildren: () =>
      import("./pages/pursuits/pursuits.module").then((m) => m.PursuitsModule),
  },
  {
    path: "background-operations",
    loadChildren: () =>
      import("./pages/background-operations/background-operations.module").then(
        (m) => m.BackgroundOperationsModule
      ),
  },
  {
    path: "model-intelligence",
    loadChildren: () =>
      import("./pages/model-intelligence/model-intelligence.module").then(
        (m) => m.ModelIntelligenceModule
      ),
  },
  {
    path: "runtime-lab",
    loadChildren: () =>
      import("./pages/runtime-lab/runtime-lab.module").then(
        (m) => m.RuntimeLabModule
      ),
  },
  {
    path: "account-bridges",
    loadChildren: () =>
      import("./pages/account-bridges/account-bridges.module").then(
        (m) => m.AccountBridgesModule
      ),
  },
  {
    path: "runtime-control",
    loadChildren: () =>
      import("./pages/runtime-control/runtime-control.module").then(
        (m) => m.RuntimeControlModule
      ),
  },
  {
    path: "plans",
    loadChildren: () =>
      import("./pages/plan-coordination/plan-coordination.module").then(
        (m) => m.PlanCoordinationModule
      ),
  },
  {
    path: "agent-teams",
    loadChildren: () =>
      import("./pages/agent-teams/agent-teams.module").then(
        (m) => m.AgentTeamsModule
      ),
  },
  {
    path: "knowledge-claims",
    loadChildren: () =>
      import("./pages/knowledge-claims/knowledge-claims.module").then(
        (m) => m.KnowledgeClaimsModule
      ),
  },
  {
    path: "governance-control",
    loadChildren: () =>
      import("./pages/governance-control/governance-control.module").then(
        (m) => m.GovernanceControlModule
      ),
  },
  {
    path: "exceptions",
    loadChildren: () =>
      import("./pages/exceptions/exceptions.module").then(
        (m) => m.ExceptionsModule
      ),
  },
  {
    path: "quick-capture",
    loadChildren: () =>
      import("./pages/quick-capture/quick-capture.module").then(
        (m) => m.QuickCaptureModule
      ),
  },
  {
    path: "system-status",
    loadChildren: () =>
      import("./pages/system-status/system-status.module").then(
        (m) => m.SystemStatusModule
      ),
  },
  {
    path: "brain-catalog",
    loadChildren: () =>
      import("./pages/brain-catalog/brain-catalog.module").then(
        (m) => m.BrainCatalogModule
      ),
  },
]

export const APP_ROUTES: Routes = [
  {
    path: "login",
    loadChildren: () =>
      import("./pages/login/login.module").then((m) => m.LoginModule),
    canActivate: [RedirectIfLoggedGuard],
  },
  {
    path: "onboarding",
    loadChildren: () =>
      import("./pages/onboarding/onboarding.module").then(
        (m) => m.OnboardingModule
      ),
    canActivate: [authGuard],
  },
  {
    path: '',
    component: AppShellComponent,
    canActivate: [authGuard],
    children: [
      ...AUTHENTICATED_ROUTES,
      { path: "", redirectTo: "control-center", pathMatch: "full" },
      { path: "**", redirectTo: "control-center" },
    ],
  },
];

@NgModule({
  imports: [RouterModule.forRoot(APP_ROUTES)],
  exports: [RouterModule],
})
export class AppRoutingModule {}
