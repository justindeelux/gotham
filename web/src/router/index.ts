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
