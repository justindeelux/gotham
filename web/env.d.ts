/// <reference types="vite/client" />

export {};

// RouteMeta augmentation for the sidebar/nav guards (title, auth gating).
// Ambient declarations live here rather than in a linted source file.
declare module "vue-router" {
  interface RouteMeta {
    title?: string;
    requiresAuth?: boolean;
    publicOnly?: boolean;
  }
}
