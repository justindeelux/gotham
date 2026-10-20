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

// JUS-247: the shell is the single owner of container-tab disposal at a team
// role switch/demotion; the app page and tab host only leave their pane.
useTerminalAccessBoundary();

// JUS-135: below the tablet breakpoint the rail leaves the grid column and
// moves into the rail drawer (AppTopbar). Only one ServerRail is mounted at a
// time (the grid copy above 1024px, the drawer copy below).
const isMobile = useMediaQuery("(max-width: 1024px)");

// JUS-135: the shared server poll belongs to the shell, not the rail. The rail
// unmounts (its drawer closes, or the viewport crosses the breakpoint), so it
// must never stop a poll the topbar drawer and every page still rely on.
onMounted(() => {
  // JUS-143: seed the list once so the sidebar count and the rail are correct
  // on first paint, even on a cold load of a page that neither fetches servers
  // nor opens the rail (a mobile page, where the rail is not mounted). The
  // guard skips when the rail or the page has already started a load; the
  // store's listSync ordering keeps any overlapping loads correct.
  if (serversStore.servers.length === 0 && !serversStore.loading) {
    void serversStore.fetchServers().catch(() => {
      // The servers page surfaces load errors; the shell stays quiet.
    });
  }
  serversStore.pollServers();
});
onUnmounted(() => serversStore.stopPolling());

// A route with `fillViewport` owns its own scrolling region (see the file
// browser, JUS-118): the page spans the viewport and never scrolls itself.
const route = useRoute();
const fillViewport = computed(() => route.meta.fillViewport === true);
// A wide page (the file manager, JUS-206) skips the centred max-width column.
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

    <!-- JUS-232: the code editor window mounts once in the app shell, so open
         files and unsaved buffers survive route and page navigation. -->
    <FilesEditor />

    <!-- JUS-245: the floating terminal window mounts beside the editor and
         collapses with v-show, so every live PTY survives navigation, tab
         switches and minimize/restore. -->
    <TerminalWindow />

    <!-- JUS-246: page sessions outlive their route. The dock shows a paused
         card for every page tab without a mounted pane, so a live shell is
         never invisible, unreachable or unclosable. -->
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

/* A fill page (JUS-118) spans the viewport as a flex column whose single
 * child owns the internal scroll, so the page itself never scrolls. The
 * bottom padding is trimmed so the panel reaches near the viewport edge. */
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

/* A wide page skips the centred max-width column and uses the full width. */
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
  /* JUS-135: the rail has left the grid (it is a drawer now), so the single
     remaining in-flow column is the content; the fixed sidebar overlays it. */
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

  /* JUS-135: on a phone the fill page keeps a tighter band so the file list
     (the only scrolling region) gets more of the viewport. */
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
