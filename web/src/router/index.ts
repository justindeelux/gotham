import { createRouter, createWebHistory } from "vue-router";
import type { RouteRecordRaw } from "vue-router";

import { useAuthStore } from "../stores/auth";

declare module "vue-router" {
  interface RouteMeta {
    title?: string;
    requiresAuth?: boolean;
    publicOnly?: boolean;
  }
}

const routes: RouteRecordRaw[] = [
  {
    path: "",
    component: () => import("../layouts/AuthLayout.vue"),
    children: [
      {
        path: "/login",
        name: "login",
        component: () => import("../pages/LoginPage.vue"),
        meta: { title: "Sign in", publicOnly: true },
      },
      {
        path: "/register",
        name: "register",
        component: () => import("../pages/RegisterPage.vue"),
        meta: { title: "Create account", publicOnly: true },
      },
      {
        path: "/oauth/callback",
        name: "oauth-callback",
        component: () => import("../pages/OAuthCallbackPage.vue"),
        meta: { title: "Signing in" },
      },
    ],
  },
  {
    path: "/",
    component: () => import("../layouts/AppLayout.vue"),
    meta: { requiresAuth: true },
    children: [
      { path: "", redirect: { name: "dashboard" } },
      {
        path: "dashboard",
        name: "dashboard",
        component: () => import("../pages/DashboardPage.vue"),
        meta: { title: "Dashboard", requiresAuth: true },
      },
      {
        path: "servers",
        name: "servers",
        component: () => import("../pages/ServersPage.vue"),
        meta: { title: "Servers", requiresAuth: true },
      },
      {
        path: "servers/:id",
        name: "server-detail",
        component: () => import("../pages/ServerDetailPage.vue"),
        meta: { title: "Server detail", requiresAuth: true },
      },
      {
        path: "servers/:id/containers",
        name: "server-containers",
        component: () => import("../pages/ContainersPage.vue"),
        meta: { title: "Containers", requiresAuth: true },
      },
      {
        path: "applications",
        name: "applications",
        component: () => import("../pages/ApplicationsPage.vue"),
        meta: { title: "Applications", requiresAuth: true },
      },
      {
        path: "applications/:id",
        name: "application-detail",
        component: () => import("../pages/ApplicationDetailPage.vue"),
        meta: { title: "Application detail", requiresAuth: true },
      },
      {
        path: "databases",
        name: "databases",
        component: () => import("../pages/DatabasesPage.vue"),
        meta: { title: "Databases", requiresAuth: true },
      },
      {
        path: "databases/:id",
        name: "database-detail",
        component: () => import("../pages/DatabaseDetailPage.vue"),
        meta: { title: "Database detail", requiresAuth: true },
      },
      {
        path: "domains",
        name: "domains",
        component: () => import("../pages/DomainsPage.vue"),
        meta: { title: "Domains & SSL", requiresAuth: true },
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
