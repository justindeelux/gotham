<script setup lang="ts">
import { useMediaQuery } from "@vueuse/core";
import { NButton, NDrawer } from "naive-ui";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink, useRoute } from "vue-router";

import GothamIcon from "@/shared/ui/GothamIcon.vue";
import LanguageSelect from "@/shared/ui/LanguageSelect.vue";
import { useAuthStore } from "@/features/auth";
import ServerRail from "@/features/servers/components/ServerRail.vue";
import AccountMenu from "./AccountMenu.vue";
import { useMobileNav } from "./useMobileNav";

const { t } = useI18n();
const authStore = useAuthStore();
const route = useRoute();
const { mobileNavOpen, toggleNav } = useMobileNav();

/**
 * JUS-135: below the tablet breakpoint the server rail is a Naive UI drawer
 * (focus trap, Escape and aria come from NDrawer) instead of an always-on
 * column. The drawer closes on navigation and when the viewport grows back.
 */
const isMobile = useMediaQuery("(max-width: 1024px)");
const railOpen = ref(false);
watch(isMobile, (value) => {
  if (!value) {
    railOpen.value = false;
  }
});
// Close on any navigation (the query included) ...
watch(
  () => route.fullPath,
  () => {
    railOpen.value = false;
  },
);

/**
 * Listener ports shown as topbar chips. They mirror the backend defaults in
 * `internal/config/config.go` (`defaultServerPort = 8000`,
 * `defaultGRPCAddr = ":9442"`); the SPA itself is served from the same
 * origin, so these are labels, not live config.
 */
const cpPort = 8000;
const grpcPort = 9442;

// The control plane exposes no environment endpoint, so the chip reflects
// where the SPA itself is served from: loopback means a local setup.
const envLabel = computed<string>(() => {
  const host = window.location.hostname;
  return host === "localhost" || host === "127.0.0.1" || host === "::1" || host === ""
    ? "local"
    : "production";
});

</script>

<template>
  <header class="topbar">
    <!-- JUS-135: the server rail becomes this drawer below the tablet
         breakpoint; the trigger replaces the rail column on phones/tablets. -->
    <NButton
      quaternary
      circle
      class="rail-toggle"
      :aria-label="t('shell.serversToggle')"
      aria-haspopup="dialog"
      :aria-expanded="railOpen"
      @click="railOpen = true"
    >
      <template #icon>
        <GothamIcon name="server" />
      </template>
    </NButton>
    <NButton
      quaternary
      circle
      class="nav-toggle"
      :aria-label="t('shell.navToggle')"
      aria-controls="app-nav"
      :aria-expanded="mobileNavOpen"
      @click="toggleNav"
    >
      <template #icon>
        <GothamIcon name="grid" />
      </template>
    </NButton>
    <span class="status-line" :title="t('shell.envTitle', { env: envLabel })">
      <GothamIcon name="shield" class="status-icon" />
      {{ envLabel }}
    </span>
    <span class="channel-chip" :title="t('shell.cpPortTitle')">CP :{{ cpPort }}</span>
    <span class="channel-chip" :title="t('shell.grpcPortTitle')">gRPC :{{ grpcPort }}</span>
    <div class="topbar-right">
      <LanguageSelect />
      <AccountMenu v-if="authStore.isAuthenticated" compact />
      <RouterLink v-else to="/login">
        <NButton quaternary type="primary">{{ t("shell.signIn") }}</NButton>
      </RouterLink>
    </div>
  </header>

  <!-- JUS-135: the phone/tablet server rail. Naive's NDrawer supplies the
       focus trap, Escape-to-close and dialog semantics; the content only
       mounts while open, so the rail's server poll is not duplicated. -->
  <NDrawer
    v-model:show="railOpen"
    placement="left"
    :width="72"
    :aria-label="t('servers.rail.navLabel')"
  >
    <!-- A tap on any rail link/button selects its target and closes the
         drawer — including a link whose route is the current one, where the
         route watcher above never fires (JUS-135). -->
    <div class="rail-drawer" @click="railOpen = false">
      <ServerRail />
    </div>
  </NDrawer>
</template>

<style scoped>
.topbar {
  position: sticky;
  top: 0;
  flex: 0 0 auto;
  height: var(--topbar-h);
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: 0 var(--space-4);
  border-bottom: 1px solid var(--border);
  background: color-mix(in oklab, var(--bg) 94%, transparent);
  backdrop-filter: blur(12px);
  z-index: 20;
}

.status-line {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--text-xs);
  color: var(--muted);
  white-space: nowrap;
}

.status-icon {
  width: 15px;
  height: 15px;
  flex: 0 0 auto;
}

.channel-chip {
  font-family: var(--font-mono);
  font-size: 10px;
  color: var(--muted);
  background: var(--surface-warm);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 3px 6px;
  white-space: nowrap;
}

.topbar-right {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  margin-left: auto;
}

.nav-toggle {
  display: none;
}

.rail-toggle {
  display: none;
}

/* JUS-135: the rail drawer. The drawer body owns no padding, so it just needs
   to hand the full height to the rail (whose foot sticks to the bottom). */
.rail-drawer {
  height: 100%;
  display: flex;
  flex-direction: column;
  background: var(--surface-warm);
}

.rail-drawer :deep(.rail) {
  flex: 1 1 auto;
  min-height: 0;
}

@media (max-width: 1024px) {
  .nav-toggle,
  .rail-toggle {
    display: inline-flex;
  }
}

@media (max-width: 860px) {
  .topbar .channel-chip,
  .topbar .status-line {
    display: none;
  }
}

/* Phone: the language selector and the account control are the only
   controls left in the top bar; tighten the spacing around them. */
@media (max-width: 640px) {
  .topbar {
    gap: var(--space-2);
    padding: 0 var(--space-3);
  }
}
</style>
