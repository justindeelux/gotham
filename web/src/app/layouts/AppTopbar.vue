<script setup lang="ts">
import { NAvatar, NButton, NDropdown, NInput, NTooltip } from "naive-ui";
import type { DropdownOption } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink, useRouter } from "vue-router";

import GothamIcon from "@/shared/ui/GothamIcon.vue";
import LanguageSelect from "@/shared/ui/LanguageSelect.vue";
import { useAuthStore } from "@/features/auth";
import { useMobileNav } from "./useMobileNav";

const { t } = useI18n();
const authStore = useAuthStore();
const router = useRouter();
const { mobileNavOpen, toggleNav } = useMobileNav();

/** accountOptions keeps stable keys; labels render in the active locale. */
const accountOptions = computed<DropdownOption[]>(() => [
  { label: t("shell.signOut"), key: "sign-out" },
]);

/**
 * Listener ports shown as topbar chips. They mirror the backend defaults in
 * `internal/config/config.go` (`defaultServerPort = 8000`,
 * `defaultGRPCAddr = ":9442"`); the SPA itself is served from the same
 * origin, so these are labels, not live config.
 */
const cpPort = 8000;
const grpcPort = 9442;

const userInitial = computed<string>(() =>
  (authStore.user?.email?.[0] ?? "?").toUpperCase(),
);

// The control plane exposes no environment endpoint, so the chip reflects
// where the SPA itself is served from: loopback means a local setup.
const envLabel = computed<string>(() => {
  const host = window.location.hostname;
  return host === "localhost" || host === "127.0.0.1" || host === "::1" || host === ""
    ? "local"
    : "production";
});

async function handleAccountSelect(key: string | number): Promise<void> {
  if (key !== "sign-out") {
    return;
  }
  try {
    await authStore.logout();
  } catch {
    // logout clears the local session in `finally`; a failed revoke must not
    // block the redirect or surface as an unhandled rejection (B3-3).
  }
  await router.push({ name: "login" });
}
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
    <span class="status-line" :title="`Serving environment: ${envLabel}`">
      <GothamIcon name="shield" class="status-icon" />
      {{ envLabel }}
    </span>
    <span class="channel-chip" :title="`Control-plane HTTP port`">CP :{{ cpPort }}</span>
    <span class="channel-chip" :title="`Agent gRPC port`">gRPC :{{ grpcPort }}</span>
    <NTooltip trigger="hover">
      <template #trigger>
        <div class="search" role="search" :aria-label="t('shell.searchLabel')">
          <GothamIcon name="search" />
          <NInput disabled :placeholder="t('shell.searchPlaceholder')" :aria-label="t('shell.searchLabel')" />
          <span class="kbd">⌘K</span>
        </div>
      </template>
      {{ t("shell.searchSoon") }}
    </NTooltip>
    <div class="topbar-right">
      <LanguageSelect />
      <NTooltip trigger="hover">
        <template #trigger>
          <NButton quaternary circle :aria-label="t('shell.notifications')" class="is-stub">
            <template #icon>
              <GothamIcon name="bell" />
            </template>
          </NButton>
        </template>
        {{ t("shell.notifications") }}
      </NTooltip>
      <NTooltip trigger="hover">
        <template #trigger>
          <NButton quaternary circle :aria-label="t('shell.docs')" class="is-stub">
            <template #icon>
              <GothamIcon name="doc" />
            </template>
          </NButton>
        </template>
        {{ t("shell.docsSoon") }}
      </NTooltip>
      <NDropdown
        v-if="authStore.isAuthenticated"
        trigger="click"
        :options="accountOptions"
        @select="handleAccountSelect"
      >
        <NButton quaternary circle :aria-label="t('shell.account')">
          <template #icon>
            <NAvatar round :size="24" :src="authStore.user?.avatar">
              <!-- NAvatar prefers the default slot over `src`, so the
                   initial is rendered only when there is no avatar image;
                   `#fallback` keeps it for a failed image load. -->
              <template v-if="!authStore.user?.avatar">{{ userInitial }}</template>
              <template #fallback>{{ userInitial }}</template>
            </NAvatar>
          </template>
        </NButton>
      </NDropdown>
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

.search {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  background: var(--surface-warm);
  border-radius: var(--radius-sm);
  padding: 0 var(--space-2);
  height: 28px;
  width: min(360px, 38vw);
  color: var(--muted);
  margin-left: auto;
  cursor: not-allowed;
}

.search :deep(svg) {
  width: 15px;
  height: 15px;
  flex: 0 0 auto;
}

.search :deep(.n-input) {
  background: none;
  cursor: not-allowed;
}

.search :deep(.n-input .n-input__input-el) {
  cursor: not-allowed;
}

.kbd {
  font-family: var(--font-mono);
  font-size: 10px;
  color: var(--muted);
  background: var(--surface-warm);
  border: 1px solid var(--border);
  border-bottom-width: 2px;
  border-radius: 3px;
  padding: 1px 5px;
  flex: 0 0 auto;
}

.topbar-right {
  display: flex;
  align-items: center;
  gap: var(--space-1);
}

.is-stub {
  opacity: 0.65;
  cursor: default;
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

  .search {
    width: auto;
  }

  .topbar .search :deep(.n-input) {
    width: 0;
    padding: 0;
  }
}
</style>
