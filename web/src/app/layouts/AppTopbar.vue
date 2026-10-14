<script setup lang="ts">
import { NButton } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";

import GothamIcon from "@/shared/ui/GothamIcon.vue";
import LanguageSelect from "@/shared/ui/LanguageSelect.vue";
import { useAuthStore } from "@/features/auth";
import AccountMenu from "./AccountMenu.vue";
import { useMobileNav } from "./useMobileNav";

const { t } = useI18n();
const authStore = useAuthStore();
const { mobileNavOpen, toggleNav } = useMobileNav();

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

@media (max-width: 1024px) {
  .nav-toggle {
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
