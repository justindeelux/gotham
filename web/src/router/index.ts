import { createRouter, createWebHistory } from "vue-router";
import type { RouteRecordRaw } from "vue-router";

declare module "vue-router" {
  interface RouteMeta {
    title?: string;
  }
}

const routes: RouteRecordRaw[] = [
  {
    path: "/",
    redirect: "/dashboard",
  },
  {
    path: "/dashboard",
    name: "dashboard",
    component: () => import("../pages/DashboardPage.vue"),
    meta: { title: "Dashboard" },
  },
];

export const router = createRouter({
  history: createWebHistory(),
  routes,
});
