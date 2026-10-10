import { createRouter, createWebHistory } from "vue-router";
import type { RouteRecordRaw } from "vue-router";

import { useAuthStore } from "@/features/auth";
import { i18n, onLocaleChange } from "@/shared/i18n";

const routes: RouteRecordRaw[] = [
  {
    path: "",
    component: () => import("@/app/layouts/AuthLayout.vue"),
    children: [
      // The bare root must land on the auth form. Without an index record this
      // parent matches "/" and its empty router-view renders no form at all
      // (the catch-all never fires), so `http://<host>:8000/` showed a blank
      // right pane with no way to sign in or register.
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
        meta: { titleKey: "titles.servers", requiresAuth: true },
      },
      {
        path: "servers/:id",
        name: "server-detail",
        component: () => import("@/features/servers/pages/ServerDetailPage.vue"),
        meta: { titleKey: "titles.serverDetail", requiresAuth: true },
      },
      {
        path: "servers/:id/containers",
        name: "server-containers",
        component: () => import("@/features/servers/pages/ContainersPage.vue"),
        meta: { titleKey: "titles.serverContainers", requiresAuth: true },
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
      // The flat list pages are gone since PE-5: old bookmarks land on Projects.
      // The GitHub App callback landing (GS-5) is a static route above the
      // applications redirect, so manifest and setup redirects resolve here.
      // GS-10 owns the Connect/Install/Disconnect controls; this page only
      // shows the callback result.
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
        meta: { titleKey: "titles.domains", requiresAuth: true },
      },
      {
        path: "projects/:projectId/environments/:environmentId/services/:id",
        name: "service-detail",
        component: () => import("@/features/services/pages/ServiceDetailPage.vue"),
        meta: { titleKey: "titles.serviceDetail", requiresAuth: true },
      },
      {
        path: "templates",
        name: "templates",
        component: () => import("@/features/templates/pages/TemplatesPage.vue"),
        meta: { titleKey: "titles.templates", requiresAuth: true },
      },
      {
        path: "providers/callback",
        name: "provider-callback",
        component: () => import("@/features/applications/pages/ProviderCallbackPage.vue"),
        meta: { titleKey: "titles.providerCallback", requiresAuth: true },
      },
      {
        path: "teams",
        name: "teams",
        component: () => import("@/features/teams/pages/TeamsPage.vue"),
        meta: { titleKey: "titles.teams", requiresAuth: true },
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
        meta: { titleKey: "titles.instance", requiresAuth: true },
      },
      {
        path: "settings/updates",
        name: "updates",
        component: () => import("@/features/updates/pages/UpdatesPage.vue"),
        meta: { titleKey: "titles.updates", requiresAuth: true },
      },
      // The bare settings path keeps landing on notifications now that the
      // settings section holds more than one page.
      { path: "settings", redirect: { name: "notifications" } },
      {
        path: "settings/profile",
        name: "profile",
        component: () => import("@/features/profile/pages/ProfilePage.vue"),
        meta: { titleKey: "titles.profile", requiresAuth: true },
      },
      {
        path: "invite/accept",
        name: "invite-accept",
        component: () => import("@/features/auth/pages/InviteAcceptPage.vue"),
        meta: { titleKey: "titles.inviteAccept", requiresAuth: true },
      },
    ],
  },
  { path: "/:pathMatch(.*)*", redirect: { name: "dashboard" } },
];

export const router = createRouter({
  history: createWebHistory(),
  routes,
});

router.beforeEach((to) => {
  const authStore = useAuthStore();

  if (to.meta.requiresAuth && !authStore.isAuthenticated) {
    return { name: "login", query: { redirect: to.fullPath } };
  }

  if (to.meta.publicOnly && authStore.isAuthenticated) {
    return { name: "dashboard" };
  }

  return true;
});

/**
 * applyRouteTitle mirrors the active route's titleKey onto document.title in
 * the current locale ("<page> — Gotham"). Keys (not English strings) live in
 * meta so a language switch re-renders the title without navigating; route
 * names, URLs, redirects and active-nav keys are unchanged.
 */
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

// A language switch re-applies the current title without navigating,
// reloading or touching session state.
onLocaleChange(() => {
  applyRouteTitle(router.currentRoute.value.meta.titleKey);
});
