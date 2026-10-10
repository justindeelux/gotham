<script setup lang="ts">
import { RouterView } from "vue-router";

import { TaskProgressCards } from "@/features/tasks";
import ServerRail from "@/features/servers/components/ServerRail.vue";
import AppSidebar from "./AppSidebar.vue";
import AppTopbar from "./AppTopbar.vue";
import { provideMobileNav } from "./useMobileNav";

const { mobileNavOpen, closeMobileNav } = provideMobileNav();
</script>

<template>
  <div
    class="app"
    :class="{ 'is-open': mobileNavOpen }"
  >
    <ServerRail />

    <AppSidebar />

    <div class="main">
      <AppTopbar />

      <main class="view">
        <div class="page">
          <RouterView />
        </div>
      </main>
    </div>

    <div
      v-if="mobileNavOpen"
      class="backdrop"
      aria-hidden="true"
      @click="closeMobileNav"
    ></div>

    <TaskProgressCards />
  </div>
</template>

<style scoped>
.app {
  display: grid;
  grid-template-columns: var(--rail-w) var(--sidebar-w) minmax(0, 1fr);
  grid-template-rows: minmax(0, 1fr);
  height: 100vh;
  height: 100dvh;
  overflow: hidden;
}

.main {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  background: var(--bg);
}

.view {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  overflow-x: hidden;
  overscroll-behavior: contain;
}

.page {
  padding: var(--space-6) var(--space-6) var(--space-12);
  max-width: 1360px;
}

.backdrop {
  position: fixed;
  inset: 0;
  z-index: 80;
  background: color-mix(in oklab, black 62%, transparent);
}

@media (max-width: 1024px) {
  .app {
    grid-template-columns: var(--rail-w) minmax(0, 1fr);
  }

  .page {
    padding: var(--space-5) var(--space-5) var(--space-8);
  }
}

@media (max-width: 640px) {
  .app {
    --rail-w: 60px;
  }

  .page {
    padding: var(--space-4) var(--space-4) var(--space-8);
  }
}

@media (prefers-reduced-motion: reduce) {
  *,
  *::before,
  *::after {
    animation-duration: 0.001ms !important;
    transition-duration: 0.001ms !important;
  }
}
</style>
