<script setup lang="ts">
import { NAvatar, NButton, NDropdown, NInput, NTooltip } from "naive-ui";
import type { DropdownOption } from "naive-ui";
import { computed, ref, watch } from "vue";
import { RouterLink, RouterView, useRoute, useRouter } from "vue-router";
import { version as appVersion } from "../../package.json";

import GothamIcon from "../components/GothamIcon.vue";
import type { IconName } from "../components/GothamIcon.vue";
import MeCard from "../components/MeCard.vue";
import ServerRail from "../components/ServerRail.vue";
import { useAppStore } from "../stores/app";
import { useAuthStore } from "../stores/auth";
import { useServersStore } from "../stores/servers";

/**
 * Listener ports shown as topbar chips. They mirror the backend defaults in
 * `internal/config/config.go` (`defaultServerPort = 8000`,
 * `defaultGRPCAddr = ":9442"`); the SPA itself is served from the same
 * origin, so these are labels, not live config.
 */
const cpPort = 8000;
const grpcPort = 9442;

/** Sidebar entry: live route when `to` is set, inert stub otherwise. */
interface NavItem {
  key: string;
  label: string;
  icon: IconName;
  to?: string;
  phase?: number;
}

interface NavSection {
  label: string;
  items: NavItem[];
}

// Group order and English labels follow the docs/design shell renderer
// (docs/design/assets/gotham-ui.js SECTIONS). Placeholder entries have no
// route and name the backend phase that will build them.
const navSections: NavSection[] = [
  {
    label: "Operations",
    items: [
      { key: "dashboard", label: "Dashboard", icon: "grid", to: "dashboard" },
      { key: "servers", label: "Servers", icon: "server", to: "servers" },
      { key: "applications", label: "Applications", icon: "box", to: "applications" },
      { key: "services", label: "Services", icon: "layers", phase: 7 },
      { key: "databases", label: "Databases", icon: "db", to: "databases" },
      { key: "files", label: "File manager", icon: "folder", phase: 4 },
      { key: "templates", label: "Template library", icon: "rocket", phase: 7 },
      { key: "domains", label: "Domains & SSL", icon: "globe", phase: 6 },
    ],
  },
  {
    label: "Team",
    items: [
      { key: "members", label: "Members & roles", icon: "users", phase: 8 },
      { key: "notifications", label: "Notification channels", icon: "bell", phase: 8 },
      { key: "tokens", label: "API tokens", icon: "key", phase: 8 },
    ],
  },
  {
    label: "System",
    items: [
      { key: "updates", label: "Updates & settings", icon: "gear", phase: 9 },
    ],
  },
];

const appStore = useAppStore();
const authStore = useAuthStore();
const serversStore = useServersStore();
const route = useRoute();
const router = useRouter();

const accountOptions: DropdownOption[] = [{ label: "Sign out", key: "sign-out" }];

// Live count: the only pill backed by a store. Every other section has no
// backend yet, so no pill is rendered rather than a fabricated number.
const serversCount = computed<number>(() => serversStore.servers.length);

const activeKey = computed<string>(() => String(route.name ?? "dashboard"));

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

/** stubTip names the backend phase behind an inert sidebar entry. */
function stubTip(phase: number): string {
  return `Coming in Phase ${phase}`;
}

async function handleAccountSelect(key: string | number): Promise<void> {
  if (key !== "sign-out") {
    return;
  }
  await authStore.logout();
  await router.push({ name: "login" });
}

/**
 * toggleNav drives the drawer on small screens and the collapsed sidebar
 * on desktop, matching the ≤1024px responsive behavior.
 */
function toggleNav(): void {
  if (window.matchMedia("(max-width: 1024px)").matches) {
    mobileNavOpen.value = !mobileNavOpen.value;
    return;
  }
  appStore.toggleSidebar();
}

const mobileNavOpen = ref(false);

watch(
  () => route.path,
  () => {
    mobileNavOpen.value = false;
  },
);
</script>

<template>
  <div
    class="app"
    :class="{
      'is-collapsed': appStore.sidebarCollapsed,
      'is-open': mobileNavOpen,
    }"
  >
    <ServerRail />

    <aside class="sidebar" aria-label="Product navigation">
      <div class="sidebar-head">
        <span class="brand">Gotham</span>
        <span class="tag" :title="`Web build ${appVersion}`">v{{ appVersion }}</span>
      </div>
      <nav class="sidebar-body">
        <template v-for="section in navSections" :key="section.label">
          <p class="nav-label">{{ section.label }}</p>
          <template v-for="item in section.items" :key="item.key">
            <RouterLink
              v-if="item.to"
              class="nav-item"
              :class="{ 'is-active': activeKey === item.key }"
              :to="{ name: item.to }"
            >
              <GothamIcon :name="item.icon" />
              <span>{{ item.label }}</span>
              <span v-if="item.key === 'servers'" class="nav-count">{{ serversCount }}</span>
            </RouterLink>
            <NTooltip v-else trigger="hover" :tooltip-style="{ maxWidth: '240px' }">
              <template #trigger>
                <span class="nav-item is-disabled" role="link" aria-disabled="true">
                  <GothamIcon :name="item.icon" />
                  <span>{{ item.label }}</span>
                </span>
              </template>
              {{ item.phase !== undefined ? stubTip(item.phase) : "Coming soon" }}
            </NTooltip>
          </template>
        </template>
      </nav>
      <div class="sidebar-foot">
        <MeCard />
      </div>
    </aside>

    <div class="main">
      <header class="topbar">
        <NButton
          quaternary
          circle
          class="nav-toggle"
          aria-label="Toggle navigation"
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
            <div class="search" role="search" aria-label="Search (coming soon)">
              <GothamIcon name="search" />
              <NInput disabled placeholder="Search apps, servers, databases…" aria-label="Search" />
              <span class="kbd">⌘K</span>
            </div>
          </template>
          Search is coming soon
        </NTooltip>
        <div class="topbar-right">
          <NTooltip trigger="hover">
            <template #trigger>
              <NButton quaternary circle aria-label="Notifications (coming soon)" class="is-stub">
                <template #icon>
                  <GothamIcon name="bell" />
                </template>
              </NButton>
            </template>
            Notifications — coming in Phase 8
          </NTooltip>
          <NTooltip trigger="hover">
            <template #trigger>
              <NButton quaternary circle aria-label="Docs (coming soon)" class="is-stub">
                <template #icon>
                  <GothamIcon name="doc" />
                </template>
              </NButton>
            </template>
            Docs — coming soon
          </NTooltip>
          <NDropdown
            v-if="authStore.isAuthenticated"
            trigger="click"
            :options="accountOptions"
            @select="handleAccountSelect"
          >
            <NButton quaternary circle aria-label="Account">
              <template #icon>
                <NAvatar round :size="24" :src="authStore.user?.avatar">
                  {{ userInitial }}
                </NAvatar>
              </template>
            </NButton>
          </NDropdown>
          <RouterLink v-else to="/login">
            <NButton quaternary type="primary">Sign in</NButton>
          </RouterLink>
        </div>
      </header>

      <div class="view">
        <div class="page">
          <RouterView />
        </div>
      </div>
    </div>

    <div
      v-if="mobileNavOpen"
      class="backdrop"
      aria-hidden="true"
      @click="mobileNavOpen = false"
    ></div>
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

.app.is-collapsed {
  grid-template-columns: var(--rail-w) 0 minmax(0, 1fr);
}

.sidebar {
  background: var(--surface);
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
}

.is-collapsed .sidebar {
  visibility: hidden;
}

.sidebar-head {
  position: sticky;
  top: 0;
  z-index: 3;
  height: var(--topbar-h);
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: 0 var(--space-4);
  border-bottom: 1px solid var(--border);
  font-family: var(--font-display);
  font-size: var(--text-sm);
  font-weight: 600;
  color: var(--fg-2);
}

.brand {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tag {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 2px 8px;
  border-radius: var(--radius-pill);
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.02em;
  background: var(--surface);
  border: 1px solid var(--border);
  color: var(--muted);
  margin-left: auto;
  flex: 0 0 auto;
}

.sidebar-body {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  overscroll-behavior: contain;
  padding: var(--space-3) var(--space-2) var(--space-4);
}

.nav-label {
  font-family: var(--font-mono);
  font-size: 10px;
  letter-spacing: 0.07em;
  text-transform: uppercase;
  color: var(--muted);
  padding: var(--space-3) var(--space-2) 3px;
  margin: 0;
}

.nav-item {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  width: 100%;
  padding: 6px var(--space-2);
  min-height: 30px;
  border-radius: var(--radius-sm);
  color: var(--muted);
  font-size: var(--text-sm);
  font-weight: 500;
  text-decoration: none;
  transition:
    background var(--motion-base) var(--ease-standard),
    color var(--motion-base) var(--ease-standard);
}

.nav-item :deep(svg) {
  width: 18px;
  height: 18px;
  flex: 0 0 auto;
  color: var(--meta);
}

a.nav-item:hover {
  background: var(--hover-row);
  color: var(--fg-2);
}

a.nav-item:hover :deep(svg) {
  color: var(--fg);
}

a.nav-item.is-active {
  background: var(--selected-row);
  color: var(--fg-2);
}

a.nav-item.is-active :deep(svg) {
  color: var(--fg-2);
}

.nav-item .nav-count {
  margin-left: auto;
  font-family: var(--font-mono);
  font-size: 10px;
  color: var(--muted);
  background: var(--surface-warm);
  border-radius: var(--radius-pill);
  padding: 1px 6px;
}

a.nav-item.is-active .nav-count {
  color: var(--fg);
}

.nav-item.is-disabled {
  opacity: 0.55;
  cursor: not-allowed;
}

.sidebar-foot {
  flex: 0 0 auto;
  position: sticky;
  bottom: 0;
  z-index: 2;
  padding: var(--space-2);
  border-top: 1px solid var(--border);
  background: var(--surface-warm);
}

.main {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  background: var(--bg);
}

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
  .app,
  .app.is-collapsed {
    grid-template-columns: var(--rail-w) minmax(0, 1fr);
  }

  .sidebar {
    position: fixed;
    left: var(--rail-w);
    top: 0;
    bottom: 0;
    width: var(--sidebar-w);
    z-index: 90;
    border-right: 1px solid var(--border-soft);
    box-shadow: var(--elev-raised);
    visibility: hidden;
  }

  .is-collapsed .sidebar {
    visibility: hidden;
  }

  .app.is-open .sidebar {
    visibility: visible;
  }

  .nav-toggle {
    display: inline-flex;
  }

  .page {
    padding: var(--space-5) var(--space-5) var(--space-8);
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
