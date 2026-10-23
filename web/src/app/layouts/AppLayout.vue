<script setup lang="ts">
import { useMediaQuery } from "@vueuse/core";
import { computed, onMounted, onUnmounted } from "vue";
import { RouterView, useRoute } from "vue-router";

import { TaskProgressCards } from "@/features/tasks";
import FilesEditor from "@/features/files/components/FilesEditor.vue";
import ServerRail from "@/features/servers/components/ServerRail.vue";
import { useServersStore } from "@/features/servers";
import { TerminalPageDock, TerminalWindow } from "@/features/terminal";
import AppSidebar from "./AppSidebar.vue";
import AppTopbar from "./AppTopbar.vue";
import { provideMobileNav } from "./useMobileNav";
import { useTerminalAccessBoundary } from "./useTerminalAccessBoundary";

const { mobileNavOpen, closeMobileNav } = provideMobileNav();
const serversStore = useServersStore();

useTerminalAccessBoundary();

const isMobile = useMediaQuery("(max-width: 1024px)");

onMounted(() => {
  if (serversStore.servers.length === 0 && !serversStore.loading) {
    void serversStore.fetchServers().catch(() => {
    });
  }
  serversStore.pollServers();
});
onUnmounted(() => serversStore.stopPolling());

const route = useRoute();
const fillViewport = computed(() => route.meta.fillViewport === true);
const widePage = computed(() => route.meta.wide === true);
</script>

<template>
  <div
    class="app"
    :class="{ 'is-open': mobileNavOpen }"
  >
    <ServerRail v-if="!isMobile" />

    <AppSidebar />

    <div class="main">
      <AppTopbar />

      <main class="view">
        <div class="page" :class="{ 'page--fill': fillViewport, 'page--wide': widePage }">
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

    <FilesEditor />

    <TerminalWindow />

    <TerminalPageDock />
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

.page.page--fill {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
  padding-bottom: var(--space-6);
}

.page.page--fill > * {
  min-height: 0;
}

.page.page--wide {
  max-width: none;
}

.backdrop {
  position: fixed;
  inset: 0;
  z-index: 80;
  background: color-mix(in oklab, black 62%, transparent);
}

@media (max-width: 1024px) {
  .app {
    grid-template-columns: minmax(0, 1fr);
  }

  .page {
    padding: var(--space-5) var(--space-5) var(--space-8);
  }
}

@media (max-width: 640px) {
  .page {
    padding: var(--space-4) var(--space-4) var(--space-8);
  }

  .page.page--fill {
    padding-top: var(--space-2);
    padding-bottom: var(--space-2);
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
