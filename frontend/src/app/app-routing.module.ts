import { NgModule } from "@angular/core";
import { RouterModule, Routes } from "@angular/router";
import { authGuard } from "./services/auth/guards/auth.guard";
import { RedirectIfLoggedGuard } from "./services/auth/guards/login.guard";

const authenticatedRoutes: Routes = [
  {
    path: "onboarding",
    loadChildren: () =>
      import("./pages/onboarding/onboarding.module").then(
        (m) => m.OnboardingModule
      ),
    canActivate: [authGuard],
  },
  {
    path: "home",
    loadChildren: () =>
      import("./pages/home/home.module").then((m) => m.HomeModule),
    canActivate: [authGuard],
  },
  {
    path: "command-dashboard",
    loadChildren: () =>
      import("./pages/command-dashboard/command-dashboard.module").then(
        (m) => m.CommandDashboardModule
      ),
    canActivate: [authGuard],
  },
  {
    path: "ambient-brain",
    loadChildren: () =>
      import("./pages/ambient-brain/ambient-brain.module").then(
        (m) => m.AmbientBrainModule
      ),
    canActivate: [authGuard],
  },
  {
    path: "hai-os",
    loadChildren: () =>
      import("./pages/hai-os/hai-os.module").then((m) => m.HAIOSModule),
    canActivate: [authGuard],
  },
  {
    path: "life-ops",
    loadChildren: () =>
      import("./pages/life-ops/life-ops.module").then(
        (m) => m.LifeOpsModule
      ),
    canActivate: [authGuard],
  },
  {
    path: "framework-registry",
    loadChildren: () =>
      import("./pages/framework-registry/framework-registry.module").then(
        (m) => m.FrameworkRegistryModule
      ),
    canActivate: [authGuard],
  },
  {
    path: "control-center",
    loadChildren: () =>
      import("./pages/control-center/control-center.module").then(
        (m) => m.ControlCenterModule
      ),
    canActivate: [authGuard],
  },
  {
    path: "llm-policy",
    loadChildren: () =>
      import("./pages/llm-policy/llm-policy.module").then(
        (m) => m.LLMPolicyModule
      ),
    canActivate: [authGuard],
  },
  {
    path: "memory",
    loadChildren: () =>
      import("./pages/memory/memory.module").then((m) => m.MemoryModule),
    canActivate: [authGuard],
  },
  {
    path: "task-blueprint",
    loadChildren: () =>
      import("./pages/task-blueprint/task-blueprint.module").then(
        (m) => m.TaskBlueprintModule
      ),
    canActivate: [authGuard],
  },
  {
    path: "connected-sources",
    loadChildren: () =>
      import("./pages/connected-sources/connected-sources.module").then(
        (m) => m.ConnectedSourcesModule
      ),
    canActivate: [authGuard],
  },
  {
    path: "grounded-answers",
    loadChildren: () =>
      import("./pages/grounded-answers/grounded-answers.module").then(
        (m) => m.GroundedAnswersModule
      ),
    canActivate: [authGuard],
  },
  {
    path: "workflow-engine",
    loadChildren: () =>
      import("./pages/workflow-engine/workflow-engine.module").then(
        (m) => m.WorkflowEngineModule
      ),
    canActivate: [authGuard],
  },
  {
    path: "pursuits",
    loadChildren: () =>
      import("./pages/pursuits/pursuits.module").then((m) => m.PursuitsModule),
    canActivate: [authGuard],
  },
  {
    path: "background-operations",
    loadChildren: () =>
      import("./pages/background-operations/background-operations.module").then(
        (m) => m.BackgroundOperationsModule
      ),
    canActivate: [authGuard],
  },
  {
    path: "model-intelligence",
    loadChildren: () =>
      import("./pages/model-intelligence/model-intelligence.module").then(
        (m) => m.ModelIntelligenceModule
      ),
    canActivate: [authGuard],
  },
  {
    path: "runtime-lab",
    loadChildren: () =>
      import("./pages/runtime-lab/runtime-lab.module").then(
        (m) => m.RuntimeLabModule
      ),
    canActivate: [authGuard],
  },
  {
    path: "account-bridges",
    loadChildren: () =>
      import("./pages/account-bridges/account-bridges.module").then(
        (m) => m.AccountBridgesModule
      ),
    canActivate: [authGuard],
  },
  {
    path: "runtime-control",
    loadChildren: () =>
      import("./pages/runtime-control/runtime-control.module").then(
        (m) => m.RuntimeControlModule
      ),
    canActivate: [authGuard],
  },
  {
    path: "plans",
    loadChildren: () =>
      import("./pages/plan-coordination/plan-coordination.module").then(
        (m) => m.PlanCoordinationModule
      ),
    canActivate: [authGuard],
  },
  {
    path: "agent-teams",
    loadChildren: () =>
      import("./pages/agent-teams/agent-teams.module").then(
        (m) => m.AgentTeamsModule
      ),
    canActivate: [authGuard],
  },
  {
    path: "knowledge-claims",
    loadChildren: () =>
      import("./pages/knowledge-claims/knowledge-claims.module").then(
        (m) => m.KnowledgeClaimsModule
      ),
    canActivate: [authGuard],
  },
  {
    path: "governance-control",
    loadChildren: () =>
      import("./pages/governance-control/governance-control.module").then(
        (m) => m.GovernanceControlModule
      ),
    canActivate: [authGuard],
  },
  {
    path: "exceptions",
    loadChildren: () =>
      import("./pages/exceptions/exceptions.module").then(
        (m) => m.ExceptionsModule
      ),
    canActivate: [authGuard],
  },
  {
    path: "quick-capture",
    loadChildren: () =>
      import("./pages/quick-capture/quick-capture.module").then(
        (m) => m.QuickCaptureModule
      ),
    canActivate: [authGuard],
  },
  {
    path: "system-status",
    loadChildren: () =>
      import("./pages/system-status/system-status.module").then(
        (m) => m.SystemStatusModule
      ),
    canActivate: [authGuard],
  },
  {
    path: "brain-catalog",
    loadChildren: () =>
      import("./pages/brain-catalog/brain-catalog.module").then(
        (m) => m.BrainCatalogModule
      ),
    canActivate: [authGuard],
  },
  {
    path: "skills",
    loadChildren: () =>
      import("./pages/skills/skills.module").then((m) => m.SkillsModule),
    canActivate: [authGuard],
  },
]

export const AUTHENTICATED_PAGE_PATHS = authenticatedRoutes
  .map((route) => route.path)
  .filter((path): path is string => typeof path === 'string' && path.length > 0)
  .map((path) => `/${path}`)

export const APP_ROUTES: Routes = [
  {
    path: "login",
    loadChildren: () =>
      import("./pages/login/login.module").then((m) => m.LoginModule),
    canActivate: [RedirectIfLoggedGuard],
  },
  {
    path: '',
    loadComponent: () => import('./control-room/app-shell.component').then((m) => m.AppShellComponent),
    children: [
      ...authenticatedRoutes,
      { path: "", redirectTo: "control-center", pathMatch: "full" },
      {
        path: "**",
        loadComponent: () =>
          import("./pages/not-found/not-found.component").then(
            (m) => m.NotFoundComponent
          ),
        canActivate: [authGuard],
      },
    ],
  },
];

@NgModule({
  imports: [RouterModule.forRoot(APP_ROUTES)],
  exports: [RouterModule],
})
export class AppRoutingModule {}
