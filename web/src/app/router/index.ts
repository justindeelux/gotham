import { createRouter, createWebHistory } from "vue-router";
import type { RouteRecordRaw } from "vue-router";

import { useAuthStore } from "@/features/auth";
import { i18n, onLocaleChange } from "@/shared/i18n";

const routes: RouteRecordRaw[] = [
  {
    path: "",
    component: () => import("@/app/layouts/AuthLayout.vue"),
    children: [
      { path: "", redirect: { name: "login" } },
      {
        path: "/login",
        name: "login",
        component: () => import("@/features/auth/pages/LoginPage.vue"),
        meta: { titleKey: "titles.login", publicOnly: true },
      },
      {
        path: "/register",
        name: "register",
        component: () => import("@/features/auth/pages/RegisterPage.vue"),
        meta: { titleKey: "titles.register", publicOnly: true },
      },
      {
        path: "/oauth/callback",
        name: "oauth-callback",
        component: () => import("@/features/auth/pages/OAuthCallbackPage.vue"),
        meta: { titleKey: "titles.oauthCallback" },
      },
    ],
  },
  {
    path: "/",
    component: () => import("@/app/layouts/AppLayout.vue"),
    meta: { requiresAuth: true },
    children: [
      { path: "", redirect: { name: "dashboard" } },
      {
        path: "dashboard",
        name: "dashboard",
        component: () => import("@/features/dashboard/pages/DashboardPage.vue"),
        meta: { titleKey: "titles.dashboard", requiresAuth: true },
      },
      {
        path: "servers",
        name: "servers",
        component: () => import("@/features/servers/pages/ServersPage.vue"),
        meta: { titleKey: "titles.servers", requiresAuth: true, requiresOwner: true },
      },
      {
        path: "servers/:id",
        name: "server-detail",
        component: () => import("@/features/servers/pages/ServerDetailPage.vue"),
        meta: { titleKey: "titles.serverDetail", requiresAuth: true, requiresOwner: true },
      },
      {
        path: "servers/:id/containers",
        name: "server-containers",
        component: () => import("@/features/servers/pages/ContainersPage.vue"),
        meta: { titleKey: "titles.serverContainers", requiresAuth: true, requiresOwner: true },
      },
      {
        path: "files",
        name: "files",
        component: () => import("@/features/files/pages/FilesPage.vue"),
        meta: {
          titleKey: "titles.files",
          requiresAuth: true,
          requiresOwner: true,
          wide: true,
        },
      },
      {
        path: "servers/:id/files",
        name: "server-files",
        component: () => import("@/features/files/pages/ServerFilesPage.vue"),
        meta: {
          titleKey: "titles.serverFiles",
          requiresAuth: true,
          requiresOwner: true,
          fillViewport: true,
          wide: true,
        },
      },
      {
        path: "terminal",
        name: "terminal",
        component: () => import("@/features/terminal/pages/TerminalPage.vue"),
        meta: {
          titleKey: "terminal.page.title",
          requiresAuth: true,
          requiresOwner: true,
          fillViewport: true,
        },
      },
      {
        path: "projects",
        name: "projects",
        component: () => import("@/features/projects/pages/ProjectsPage.vue"),
        meta: { titleKey: "titles.projects", requiresAuth: true },
      },
      {
        path: "projects/:projectId",
        name: "project-detail",
        component: () => import("@/features/projects/pages/ProjectDetailPage.vue"),
        meta: { titleKey: "titles.projectDetail", requiresAuth: true },
      },
      {
        path: "projects/:projectId/environments/:environmentId",
        name: "environment-detail",
        component: () => import("@/features/projects/pages/EnvironmentPage.vue"),
        meta: { titleKey: "titles.environmentDetail", requiresAuth: true },
      },
      {
        path: "projects/:projectId/environments/:environmentId/applications/:id",
        name: "application-detail",
        component: () => import("@/features/applications/pages/ApplicationDetailPage.vue"),
        meta: { titleKey: "titles.applicationDetail", requiresAuth: true },
      },
      {
        path: "projects/:projectId/environments/:environmentId/databases/:id",
        name: "database-detail",
        component: () => import("@/features/databases/pages/DatabaseDetailPage.vue"),
        meta: { titleKey: "titles.databaseDetail", requiresAuth: true },
      },
      {
        path: "applications/github-app/callback",
        name: "github-app-callback",
        component: () => import("@/features/applications/pages/GitHubAppCallbackPage.vue"),
        meta: { titleKey: "titles.githubAppCallback", requiresAuth: true },
      },
      { path: "applications", redirect: { name: "projects" } },
      { path: "applications/:id", redirect: { name: "projects" } },
      { path: "databases", redirect: { name: "projects" } },
      { path: "databases/:id", redirect: { name: "projects" } },
      { path: "services", redirect: { name: "projects" } },
      { path: "services/:id", redirect: { name: "projects" } },
      {
        path: "domains",
        name: "domains",
        component: () => import("@/features/domains/pages/DomainsPage.vue"),
        meta: { titleKey: "titles.domains", requiresAuth: true, requiresOwner: true },
      },
      {
        path: "projects/:projectId/environments/:environmentId/services/:id",
        name: "service-detail",
        component: () => import("@/features/services/pages/ServiceDetailPage.vue"),
        meta: { titleKey: "titles.serviceDetail", requiresAuth: true },
      },
      {
        path: "toolchain",
        name: "toolchain",
        component: () => import("@/features/toolchain/pages/ToolchainPage.vue"),
        meta: { titleKey: "titles.toolchain", requiresAuth: true },
      },
      {
        path: "providers/callback",
        name: "provider-callback",
        component: () => import("@/features/applications/pages/ProviderCallbackPage.vue"),
        meta: { titleKey: "titles.providerCallback", requiresAuth: true },
      },
      {
        path: "users",
        name: "users",
        component: () => import("@/features/users/pages/UsersPage.vue"),
        meta: { titleKey: "titles.users", requiresAuth: true, requiresOwner: true },
      },
      {
        path: "settings/notifications",
        name: "notifications",
        component: () => import("@/features/notifications/pages/NotificationsPage.vue"),
        meta: { titleKey: "titles.notifications", requiresAuth: true },
      },
      {
        path: "settings/git-sources",
        name: "git-sources",
        component: () => import("@/features/applications/pages/GitSourcesPage.vue"),
        meta: { titleKey: "titles.gitSources", requiresAuth: true },
      },
      {
        path: "settings/instance",
        name: "instance",
        component: () => import("@/features/instance-settings/pages/InstanceSettingsPage.vue"),
        meta: { titleKey: "titles.instance", requiresAuth: true, requiresOwner: true },
      },
      {
        path: "settings/updates",
        name: "updates",
        component: () => import("@/features/updates/pages/UpdatesPage.vue"),
        meta: { titleKey: "titles.updates", requiresAuth: true, requiresOwner: true },
      },
      { path: "settings", redirect: { name: "notifications" } },
      {
        path: "settings/profile",
        name: "profile",
        component: () => import("@/features/profile/pages/ProfilePage.vue"),
        meta: { titleKey: "titles.profile", requiresAuth: true },
      },
      {
        path: "forbidden",
        name: "forbidden",
        component: () => import("@/app/pages/NoPermissionPage.vue"),
        meta: { titleKey: "titles.forbidden", requiresAuth: true },
      },
      {
        path: ":pathMatch(.*)*",
        name: "not-found",
        component: () => import("@/features/not-found/pages/NotFoundPage.vue"),
        meta: { titleKey: "titles.notFound" },
      },
    ],
  },
];

export const router = createRouter({
  history: createWebHistory(),
  routes,
});

router.beforeEach(async (to) => {
  const authStore = useAuthStore();

  if (to.meta.requiresAuth && !authStore.isAuthenticated) {
    return { name: "login", query: { redirect: to.fullPath } };
  }

  if (to.meta.requiresOwner && !authStore.isOwner) {
    await authStore.fetchMe().catch(() => undefined);
    if (!authStore.isOwner) {
      return { name: "forbidden" };
    }
  }

  if (to.meta.publicOnly && authStore.isAuthenticated) {
    return { name: "dashboard" };
  }

  return true;
});

export function applyRouteTitle(titleKey: unknown): void {
  if (typeof document === "undefined") {
    return;
  }
  document.title =
    typeof titleKey === "string" && titleKey !== ""
      ? `${String(i18n.global.t(titleKey))} — Gotham`
      : "Gotham";
}

router.afterEach((to) => {
  applyRouteTitle(to.meta.titleKey);
});

onLocaleChange(() => {
  applyRouteTitle(router.currentRoute.value.meta.titleKey);
});
